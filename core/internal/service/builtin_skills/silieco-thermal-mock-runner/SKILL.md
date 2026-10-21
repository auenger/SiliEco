---
name: silieco-thermal-mock-runner
description: "Deterministic helper for thermal simulation POC Stages; validates a model file and emulates asynchronous MCP job, verdict, candidate, and report contracts without Icepak."
user-invocable: false
---

# Thermal Mock Runner

This helper is used only when a thermal POC Stage also requires a stage-specific thermal skill. It is not a real solver and must never be used as engineering evidence.

## Commands

Locate this skill directory, then run `python3 scripts/thermal_mock.py --help`. The supported operations are:

- `validate-model --input <model> --output model-validation.json`
- `start-simulation --kind baseline --requirements requirements-snapshot.json --model-validation model-validation.json --job-dir <dir> --idempotency-key <key>`
- `start-simulation --kind candidate --requirements requirements-snapshot.json --model-validation model-validation.json --baseline baseline-result.json --candidate approved-candidate.json --job-dir <dir> --idempotency-key <key>`
- `get-job-status --job-dir <dir>`; call twice to emulate `accepted → running → succeeded`
- `evaluate-result --requirements <json> --result <json> --output <json> --candidates-output <json>`
- `generate-report --requirements <json> --model-validation <json> --baseline <json> --baseline-verdict <json> --approval approved-candidate.json --candidate <json> --candidate-verdict <json> --output <md> --artifact-index <json>`

## Contract

The runner writes JSON atomically, refuses unconfirmed requirements, invalid models, and unapproved candidates, keeps `execution_status` distinct from `thermal_verdict`, and reuses an existing job for the same idempotency key. Exit code `0` means the requested Mock operation completed; exit code `2` means a business/input validation failure.

Always retain the runner's `simulation_mode:mock` and `solver:mock-thermal-poc-v1` markers in downstream artifacts. Do not edit generated metrics to force a desired verdict.
