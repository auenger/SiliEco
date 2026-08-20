---
name: silieco-verify-feature
description: "Use in the Feature SOP local-verification stage to validate the integrated first version against PRD acceptance conditions with proportionate local evidence."
user-invocable: false
---

# Verify Feature

## Procedure

## Input

- The PRD acceptance conditions, implementation handoffs, changed code, and current child/parent Task state.

## Procedure

1. Confirm required development children are terminal or explicitly excluded and identify the repository's authoritative verification commands.
2. Run focused lint, type, unit and integration checks appropriate to changed surfaces.
3. Validate every in-scope PRD acceptance condition. Use browser checks for user-facing flows when practical.
4. Capture commands, results, failures, and only the evidence needed to reproduce the conclusion.
5. Retry fixes only when the root cause is understood; do not change expected behavior to hide a defect.

## Required output

Produce `verification-report.md` or an equivalent structured Task comment with requirement-level pass/fail, commands/results, untested items, limitations, and final `pass` or `block`. Attach screenshots/traces only when they materially support acceptance or failure analysis.

Any failing required check or acceptance condition blocks completion. Verification failure blocks completion and must never be downgraded merely to continue the SOP. Post the result for the human Gate; do not approve it or advance the SOP yourself.
