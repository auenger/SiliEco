---
name: silieco-thermal-requirements-review
description: "Use in the thermal simulation POC confirmation Stage to validate user answers and freeze a unit-aware requirement and acceptance snapshot for the Mock run."
user-invocable: false
---

# Confirm Thermal Requirements

## Input

- `requirements-draft.json`, `missing-information.md`, and the user's explicit answers.
- The selected model attachment and any authoritative component specification.

## Review

Reject the handoff when the model is missing, `target_tmax_c` is absent or has no unit/source, the operating condition is ambiguous, or a critical component lacks an identity. Keep business limits separate from Mock assumptions.

## Required output

On success, produce:

- `requirements-snapshot.json`, retaining sources and assumptions, with `confirmed:true` and an empty `missing_fields` array;
- `acceptance-profile.json`, containing the deterministic temperature limit and any confirmed airflow, pressure-drop, convergence, or improvement constraints.

On failure, produce `requirements-review.json` with `status:block` and the unresolved fields. Post the result to the current Task for the human Gate. Do not approve the Gate, run the model, or relax a limit to make the POC pass.
