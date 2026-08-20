---
name: silieco-implement-feature
description: "Use inside the Feature SOP development run to implement exactly one ready child Task with focused tests and a concise handoff for verification."
user-invocable: false
---

# Implement Feature

## Input

- One preflight-approved development child Task and its explicit acceptance boundary.
- Parent PRD requirement IDs, completed dependency results, Project context, and repository instructions.

## Procedure

1. Read the current child Task, its parent, requirement IDs, approved PRD, prototype findings, completed dependency results, and repository instructions.
2. Inspect existing implementation patterns before introducing new abstractions.
3. Implement only this child's scope; do not absorb sibling work for convenience.
4. Add or update focused tests with each behavioral change.
5. Update documentation and generated artifacts required by repository policy.
6. Run focused checks that give fast feedback, leaving full acceptance verification to the next SOP Stage.

## Constraints

Do not mark incomplete work complete or weaken tests, security checks, types, or validation. When the PRD conflicts with the codebase or an external dependency is unavailable, set the child to `blocked` and explain the required decision in the Task.

## Required output

Produce code, focused tests, necessary documentation, and a concise Task handoff listing changed areas, commands/results, requirement IDs, and remaining risks. A separate implementation report file is optional. Move the child to `in_review` only when its acceptance boundary is implemented. Do not merge, advance the SOP, or mark the parent complete.
