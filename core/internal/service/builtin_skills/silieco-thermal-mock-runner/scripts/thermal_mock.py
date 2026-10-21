#!/usr/bin/env python3
"""Deterministic local runner for the built-in thermal simulation POC."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


SOLVER = "mock-thermal-poc-v1"
ALLOWED_MODEL_EXTENSIONS = {".aedt", ".step", ".stp"}


class BusinessError(Exception):
    """Input or business-contract failure reported with exit code 2."""


def now_utc() -> str:
    return datetime.now(timezone.utc).isoformat()


def read_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise BusinessError(f"cannot read JSON {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise BusinessError(f"expected a JSON object: {path}")
    return value


def write_text_atomic(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    temporary.write_text(content, encoding="utf-8")
    os.replace(temporary, path)


def write_json_atomic(path: Path, value: dict[str, Any]) -> None:
    write_text_atomic(path, json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def print_json(value: dict[str, Any]) -> None:
    print(json.dumps(value, ensure_ascii=False, indent=2))


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def validate_model(input_path: Path, output_path: Path) -> dict[str, Any]:
    source = input_path.expanduser().resolve()
    status = "VALID"
    errors: list[str] = []
    if not source.is_file():
        status = "INVALID"
        errors.append("model file does not exist")
    elif source.stat().st_size == 0:
        status = "INVALID"
        errors.append("model file is empty")
    elif source.suffix.lower() not in ALLOWED_MODEL_EXTENSIONS:
        status = "INVALID"
        errors.append("POC accepts only .aedt, .step, or .stp model files")

    result: dict[str, Any] = {
        "schema_version": "thermal.model-validation.v1",
        "simulation_mode": "mock",
        "status": status,
        "source_name": source.name,
        "source_path": str(source),
        "size_bytes": source.stat().st_size if source.is_file() else None,
        "sha256": file_sha256(source) if status == "VALID" else None,
        "errors": errors,
        "warnings": [
            "Mock precheck did not open AEDT or validate Design, Setup, materials, boundaries, mesh, monitors, or licenses."
        ],
        "checked_at": now_utc(),
    }
    write_json_atomic(output_path, result)
    return result


def confirmed_requirements(path: Path) -> dict[str, Any]:
    requirements = read_json(path)
    if requirements.get("confirmed") is not True:
        raise BusinessError("requirements must have confirmed=true")
    target = requirements.get("target_tmax_c")
    if not isinstance(target, (int, float)):
        raise BusinessError("requirements.target_tmax_c must be numeric")
    return requirements


def model_seed(validation: dict[str, Any]) -> int:
    digest = str(validation.get("sha256") or "sha256:0").split(":", 1)[-1]
    try:
        return int(digest[:8], 16)
    except ValueError:
        return 0


def job_envelope(job: dict[str, Any]) -> dict[str, Any]:
    return {
        "execution_id": job["execution_id"],
        "status": job["status"],
        "output": job.get("output", {}),
        "output_refs": job.get("output_refs", []),
        "poll_after_seconds": 0 if job["status"] in {"accepted", "running"} else None,
        "error": job.get("error"),
        "simulation_mode": "mock",
    }


def start_simulation(args: argparse.Namespace) -> dict[str, Any]:
    requirements = confirmed_requirements(args.requirements)
    validation = read_json(args.model_validation)
    if validation.get("status") != "VALID":
        raise BusinessError("model-validation status must be VALID")
    if args.kind == "candidate" and (args.baseline is None or args.candidate is None):
        raise BusinessError("candidate simulation requires --baseline and --candidate")
    candidate = read_json(args.candidate) if args.candidate else None
    if args.kind == "candidate" and (
        not candidate.get("candidate_id")
        or not isinstance(candidate.get("action"), dict)
        or not candidate.get("approver")
        or not candidate.get("approved_at")
    ):
        raise BusinessError("candidate simulation requires a structured approval record")

    job_dir: Path = args.job_dir.expanduser().resolve()
    job_path = job_dir / "job.json"
    if job_path.exists():
        existing = read_json(job_path)
        if existing.get("idempotency_key") != args.idempotency_key:
            raise BusinessError("job directory belongs to a different idempotency key")
        return job_envelope(existing)

    identity = json.dumps(
        {
            "kind": args.kind,
            "idempotency_key": args.idempotency_key,
            "requirements": requirements,
            "model_sha256": validation.get("sha256"),
        },
        sort_keys=True,
        separators=(",", ":"),
    )
    execution_id = "mock-" + hashlib.sha256(identity.encode()).hexdigest()[:16]
    job: dict[str, Any] = {
        "schema_version": "thermal.external-job.v1",
        "execution_id": execution_id,
        "idempotency_key": args.idempotency_key,
        "kind": args.kind,
        "status": "accepted",
        "simulation_mode": "mock",
        "solver": SOLVER,
        "created_at": now_utc(),
        "requirements": requirements,
        "model_validation": validation,
        "baseline": read_json(args.baseline) if args.baseline else None,
        "candidate": candidate,
        "output": {},
        "output_refs": [],
        "error": None,
    }
    write_json_atomic(job_path, job)
    return job_envelope(job)


def simulation_result(job: dict[str, Any]) -> dict[str, Any]:
    requirements = job["requirements"]
    target = float(requirements["target_tmax_c"])
    ambient = float((requirements.get("operating_conditions") or {}).get("ambient_temperature_c", 25.0))
    seed = model_seed(job["model_validation"])
    jitter = (seed % 21) / 10.0
    baseline_tmax = round(target + 8.0 + jitter, 3)
    is_candidate = job["kind"] == "candidate"
    tmax = round(baseline_tmax - (10.5 + jitter / 2.0), 3) if is_candidate else baseline_tmax
    airflow = round(0.0935 if is_candidate else 0.085, 4)
    pressure_drop = 91.0 if is_candidate else 82.0
    verdict = "PASS" if tmax <= target else "FAIL"
    baseline = job.get("baseline") or {}
    baseline_metrics = baseline.get("metrics") if isinstance(baseline, dict) else None
    result: dict[str, Any] = {
        "schema_version": "thermal.solver-result.v1",
        "simulation_mode": "mock",
        "solver": SOLVER,
        "execution_id": job["execution_id"],
        "execution_status": "SUCCEEDED",
        "thermal_verdict": verdict,
        "metrics": {
            "Tmax_c": tmax,
            "target_tmax_c": target,
            "ambient_temperature_c": ambient,
            "airflow_m3_s": airflow,
            "pressure_drop_pa": pressure_drop,
            "converged": True,
        },
        "candidate_id": (job.get("candidate") or {}).get("candidate_id") if is_candidate else None,
        "completed_at": now_utc(),
    }
    if isinstance(baseline_metrics, dict) and isinstance(baseline_metrics.get("Tmax_c"), (int, float)):
        result["comparison"] = {
            "baseline_tmax_c": baseline_metrics["Tmax_c"],
            "candidate_tmax_c": tmax,
            "delta_tmax_c": round(tmax - float(baseline_metrics["Tmax_c"]), 3),
            "rollback_recommended": tmax >= float(baseline_metrics["Tmax_c"]),
        }
    return result


def get_job_status(job_dir: Path) -> dict[str, Any]:
    directory = job_dir.expanduser().resolve()
    job_path = directory / "job.json"
    job = read_json(job_path)
    if job.get("status") == "accepted":
        job["status"] = "running"
        job["started_at"] = now_utc()
    elif job.get("status") == "running":
        result = simulation_result(job)
        result_path = directory / "result.json"
        write_json_atomic(result_path, result)
        job["status"] = "succeeded"
        job["completed_at"] = now_utc()
        job["output"] = result
        job["output_refs"] = [{"kind": "file", "path": str(result_path)}]
    write_json_atomic(job_path, job)
    return job_envelope(job)


def evaluate_result(args: argparse.Namespace) -> dict[str, Any]:
    requirements = confirmed_requirements(args.requirements)
    result = read_json(args.result)
    metrics = result.get("metrics")
    if result.get("execution_status") != "SUCCEEDED" or not isinstance(metrics, dict):
        verdict = "INVALID"
        reason = "solver_result_missing_or_unsuccessful"
    elif metrics.get("converged") is not True:
        verdict = "DIVERGED"
        reason = "solver_not_converged"
    elif not isinstance(metrics.get("Tmax_c"), (int, float)):
        verdict = "INVALID"
        reason = "missing_temperature_metric"
    else:
        verdict = "PASS" if float(metrics["Tmax_c"]) <= float(requirements["target_tmax_c"]) else "FAIL"
        reason = "target_met" if verdict == "PASS" else "temperature_above_target"

    decision = {
        "schema_version": "thermal.verdict.v1",
        "simulation_mode": "mock",
        "execution_id": result.get("execution_id"),
        "execution_status": result.get("execution_status"),
        "thermal_verdict": verdict,
        "reason": reason,
        "target_tmax_c": requirements["target_tmax_c"],
        "actual_tmax_c": metrics.get("Tmax_c") if isinstance(metrics, dict) else None,
        "evaluated_at": now_utc(),
    }
    candidates = {
        "schema_version": "thermal.candidates.v1",
        "simulation_mode": "mock",
        "candidates": [],
    }
    if verdict == "FAIL":
        candidates["candidates"] = [
            {
                "candidate_id": "fan-curve-speed-110",
                "title": "Increase native fan curve speed by 10%",
                "action": {"type": "fan_curve_speed_ratio", "value": 1.1},
                "expected_direction": "lower Tmax_c with higher airflow and pressure drop",
                "risks": ["higher acoustic noise", "higher fan power", "POC estimate is not measured"],
                "requires_human_approval": True,
            }
        ]
    write_json_atomic(args.output, decision)
    write_json_atomic(args.candidates_output, candidates)
    return decision


def generate_report(args: argparse.Namespace) -> dict[str, Any]:
    requirements = confirmed_requirements(args.requirements)
    validation = read_json(args.model_validation)
    baseline = read_json(args.baseline)
    baseline_verdict = read_json(args.baseline_verdict)
    approval = read_json(args.approval)
    candidate = read_json(args.candidate)
    candidate_verdict = read_json(args.candidate_verdict)
    baseline_metrics = baseline.get("metrics") or {}
    candidate_metrics = candidate.get("metrics") or {}
    comparison = candidate.get("comparison") or {}
    report = f"""# Thermal Simulation POC Report

> Mock-only evidence. No AEDT, PyAEDT, Icepak license, mesh, or physical thermal solve was used.

## Case

- Case: {requirements.get('case_name', 'Unnamed case')}
- Model: {validation.get('source_name')}
- Model hash: `{validation.get('sha256')}`
- Solver: `{SOLVER}`

## Acceptance

- Target Tmax: {requirements.get('target_tmax_c')} °C

## Baseline

- Execution status: {baseline.get('execution_status')}
- Thermal verdict: {baseline_verdict.get('thermal_verdict')}
- Tmax: {baseline_metrics.get('Tmax_c')} °C

## Approved candidate result

- Approved by: {approval.get('approver')}
- Approved at: {approval.get('approved_at')}
- Candidate: {candidate.get('candidate_id')}
- Execution status: {candidate.get('execution_status')}
- Thermal verdict: {candidate_verdict.get('thermal_verdict')}
- Tmax: {candidate_metrics.get('Tmax_c')} °C
- Delta Tmax: {comparison.get('delta_tmax_c')} °C
- Rollback recommended: {comparison.get('rollback_recommended')}

## Limitations

This report validates the Silieco SOP, Stage skill, artifact, Gate, and external-job-shaped Mock flow only. It is not an engineering sign-off.
"""
    write_text_atomic(args.output, report)
    artifacts = {
        "schema_version": "thermal.artifact-index.v1",
        "simulation_mode": "mock",
        "artifacts": [
            {"kind": "report", "path": str(args.output.resolve())},
            {"kind": "model_validation", "path": str(args.model_validation.resolve())},
            {"kind": "baseline_result", "path": str(args.baseline.resolve())},
            {"kind": "candidate_approval", "path": str(args.approval.resolve())},
            {"kind": "candidate_result", "path": str(args.candidate.resolve())},
        ],
        "created_at": now_utc(),
    }
    write_json_atomic(args.artifact_index, artifacts)
    return artifacts


def self_test() -> dict[str, Any]:
    with tempfile.TemporaryDirectory(prefix="thermal-mock-") as raw_dir:
        root = Path(raw_dir)
        model = root / "demo.aedt"
        model.write_bytes(b"mock-aedt-project")
        requirements_path = root / "requirements.json"
        write_json_atomic(requirements_path, {
            "schema_version": "thermal.requirements.v1",
            "case_name": "self-test",
            "confirmed": True,
            "target_tmax_c": 85.0,
            "operating_conditions": {"ambient_temperature_c": 30.0},
            "missing_fields": [],
        })
        validation_path = root / "validation.json"
        validation = validate_model(model, validation_path)
        args = argparse.Namespace(
            kind="baseline",
            requirements=requirements_path,
            model_validation=validation_path,
            baseline=None,
            candidate=None,
            job_dir=root / "baseline-job",
            idempotency_key="self-test-baseline",
        )
        accepted = start_simulation(args)
        running = get_job_status(args.job_dir)
        succeeded = get_job_status(args.job_dir)
        result = read_json(args.job_dir / "result.json")
        baseline_verdict_path = root / "baseline-verdict.json"
        candidates_path = root / "candidates.json"
        evaluate_result(argparse.Namespace(
            requirements=requirements_path,
            result=args.job_dir / "result.json",
            output=baseline_verdict_path,
            candidates_output=candidates_path,
        ))
        candidates = read_json(candidates_path)
        approved_path = root / "approved-candidate.json"
        approved = dict(candidates["candidates"][0])
        approved.update({"approver": "self-test", "approved_at": now_utc()})
        write_json_atomic(approved_path, approved)

        candidate_args = argparse.Namespace(
            kind="candidate",
            requirements=requirements_path,
            model_validation=validation_path,
            baseline=args.job_dir / "result.json",
            candidate=approved_path,
            job_dir=root / "candidate-job",
            idempotency_key="self-test-candidate",
        )
        start_simulation(candidate_args)
        get_job_status(candidate_args.job_dir)
        candidate_succeeded = get_job_status(candidate_args.job_dir)
        candidate_result_path = candidate_args.job_dir / "result.json"
        candidate_result = read_json(candidate_result_path)
        candidate_verdict_path = root / "candidate-verdict.json"
        evaluate_result(argparse.Namespace(
            requirements=requirements_path,
            result=candidate_result_path,
            output=candidate_verdict_path,
            candidates_output=root / "unused-candidates.json",
        ))
        report_path = root / "thermal-poc-report.md"
        artifact_index_path = root / "artifact-index.json"
        generate_report(argparse.Namespace(
            requirements=requirements_path,
            model_validation=validation_path,
            baseline=args.job_dir / "result.json",
            baseline_verdict=baseline_verdict_path,
            approval=approved_path,
            candidate=candidate_result_path,
            candidate_verdict=candidate_verdict_path,
            output=report_path,
            artifact_index=artifact_index_path,
        ))
        if validation["status"] != "VALID" or accepted["status"] != "accepted":
            raise RuntimeError("validation or accepted state failed")
        if running["status"] != "running" or succeeded["status"] != "succeeded":
            raise RuntimeError("job state progression failed")
        if result["execution_status"] != "SUCCEEDED" or result["thermal_verdict"] != "FAIL":
            raise RuntimeError("baseline result contract failed")
        if candidate_succeeded["status"] != "succeeded" or candidate_result["thermal_verdict"] != "PASS":
            raise RuntimeError("candidate result contract failed")
        if not report_path.is_file() or not artifact_index_path.is_file():
            raise RuntimeError("report contract failed")
        return {
            "status": "ok",
            "solver": SOLVER,
            "baseline_verdict": result["thermal_verdict"],
            "candidate_verdict": candidate_result["thermal_verdict"],
            "report_generated": True,
            "workdir_cleaned": True,
        }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    validate = subparsers.add_parser("validate-model")
    validate.add_argument("--input", type=Path, required=True)
    validate.add_argument("--output", type=Path, required=True)

    start = subparsers.add_parser("start-simulation")
    start.add_argument("--kind", choices=("baseline", "candidate"), required=True)
    start.add_argument("--requirements", type=Path, required=True)
    start.add_argument("--model-validation", type=Path, required=True)
    start.add_argument("--baseline", type=Path)
    start.add_argument("--candidate", type=Path)
    start.add_argument("--job-dir", type=Path, required=True)
    start.add_argument("--idempotency-key", required=True)

    status = subparsers.add_parser("get-job-status")
    status.add_argument("--job-dir", type=Path, required=True)

    evaluate = subparsers.add_parser("evaluate-result")
    evaluate.add_argument("--requirements", type=Path, required=True)
    evaluate.add_argument("--result", type=Path, required=True)
    evaluate.add_argument("--output", type=Path, required=True)
    evaluate.add_argument("--candidates-output", type=Path, required=True)

    report = subparsers.add_parser("generate-report")
    report.add_argument("--requirements", type=Path, required=True)
    report.add_argument("--model-validation", type=Path, required=True)
    report.add_argument("--baseline", type=Path, required=True)
    report.add_argument("--baseline-verdict", type=Path, required=True)
    report.add_argument("--approval", type=Path, required=True)
    report.add_argument("--candidate", type=Path, required=True)
    report.add_argument("--candidate-verdict", type=Path, required=True)
    report.add_argument("--output", type=Path, required=True)
    report.add_argument("--artifact-index", type=Path, required=True)

    subparsers.add_parser("self-test")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        if args.command == "validate-model":
            result = validate_model(args.input, args.output)
        elif args.command == "start-simulation":
            result = start_simulation(args)
        elif args.command == "get-job-status":
            result = get_job_status(args.job_dir)
        elif args.command == "evaluate-result":
            result = evaluate_result(args)
        elif args.command == "generate-report":
            result = generate_report(args)
        else:
            result = self_test()
        print_json(result)
        return 0
    except BusinessError as exc:
        print_json({"status": "error", "error_type": "business_validation", "error": str(exc)})
        return 2


if __name__ == "__main__":
    sys.exit(main())
