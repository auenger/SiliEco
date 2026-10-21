---
name: silieco-thermal-result-review
description: "Use in the thermal simulation POC result-review Stage to deterministically classify baseline metrics and create bounded candidate options without applying them."
user-invocable: false
---

# Review Mock Thermal Result

## Input

- `requirements-snapshot.json`, `acceptance-profile.json`, and `baseline-result.json`.
- The required `silieco-thermal-mock-runner` skill.

## Procedure

Run the Mock Runner `evaluate-result` command. Keep external execution success separate from `PASS`, `FAIL`, `INVALID`, or `DIVERGED`. For the expected FAIL path, retain only candidates emitted by the deterministic runner and explain that their expected effect is a POC assumption.

## Required output

Produce `baseline-verdict.json` and `candidates.json`. Each candidate must include an ID, exact action, expected direction, risks, and `requires_human_approval:true`. Post the evidence to the current Task. Do not select, apply, or simulate a candidate and do not advance the SOP.
