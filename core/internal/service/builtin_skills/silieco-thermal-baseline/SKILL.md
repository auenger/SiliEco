---
name: silieco-thermal-baseline
description: "Use in the thermal simulation POC baseline Stage to exercise an accepted-to-succeeded external-job contract and produce deterministic failing baseline metrics."
user-invocable: false
---

# Run Mock Thermal Baseline

## Input

- Confirmed `requirements-snapshot.json` and `acceptance-profile.json`.
- VALID `model-validation.json`.
- The required `silieco-thermal-mock-runner` skill.

## Procedure

1. Start a `baseline` Mock job in a dedicated artifact directory with an idempotency key derived from the current SOP run and Stage.
2. Record the returned `accepted` envelope as `baseline-job.json`.
3. Call `get-job-status` until the local POC job reaches `succeeded`; the runner advances `accepted → running → succeeded` without sleeping.
4. Copy its immutable result to `baseline-result.json`.

The POC intentionally generates a converged result above the confirmed temperature target so the SOP exercises the FAIL, candidate approval, and rerun path. This is simulated evidence, never an Icepak result.

## Required output

Produce `baseline-job.json` and `baseline-result.json`, preserving `execution_status` separately from `thermal_verdict`. Post the metrics and Mock disclaimer to the current Task. Do not apply a candidate or advance the SOP.
