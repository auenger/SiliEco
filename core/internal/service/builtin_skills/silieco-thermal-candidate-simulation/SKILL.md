---
name: silieco-thermal-candidate-simulation
description: "Use in the thermal simulation POC candidate Stage to rerun only an explicitly approved Mock action and compare it deterministically with the baseline."
user-invocable: false
---

# Run Approved Mock Candidate

## Input

- `candidates.json`, `approved-candidate.json`, `baseline-result.json`, `requirements-snapshot.json`, and VALID `model-validation.json`.
- The required `silieco-thermal-mock-runner` skill.

## Procedure

Verify that the approval references a candidate from `candidates.json`. The runner also requires the immutable action, approver identity, and approval timestamp. Start a `candidate` Mock job with the approved record and baseline, preserve the accepted envelope, then call `get-job-status` until succeeded. Evaluate the immutable candidate result against the same confirmed acceptance profile.

## Required output

Produce `candidate-job.json`, `candidate-result.json`, and `candidate-verdict.json`. Include baseline temperature, candidate temperature, delta, execution status, thermal verdict, and rollback recommendation. Post the comparison with the Mock disclaimer. Do not claim the source AEDT file changed, approve the final Gate, or advance the SOP.
