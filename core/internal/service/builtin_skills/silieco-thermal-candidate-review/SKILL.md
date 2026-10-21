---
name: silieco-thermal-candidate-review
description: "Use in the thermal simulation POC candidate-confirmation Stage to record one explicit engineering choice or a structured rejection before any rerun."
user-invocable: false
---

# Confirm Thermal Candidate

## Input

- `baseline-verdict.json`, `candidates.json`, and the engineer's explicit decision.

## Review

Ensure the selected ID exists, its exact action is unchanged, risks are visible, and the approver understands that expected improvement is simulated rather than measured. Do not infer approval from silence, task completion, or an earlier general requirement confirmation.

## Required output

For approval, produce `approved-candidate.json` with `candidate_id`, the immutable action, approver identity, decision note, and timestamp. For rejection, produce `rejection.json` and leave the Stage blocked or waiting. Post the decision record for the human Gate. Do not approve the Gate, execute the candidate, or alter the original model.
