---
name: silieco-run-feature
description: "Use in the Feature SOP development stage to coordinate preflight and implementation for one ready child Task without duplicating the later local-verification Stage."
user-invocable: false
---

# Run Feature

## Input

- One ready development child Task from the current barrier group.
- Its parent/PRD context and any valid evidence from an earlier interrupted attempt.

## Sequence

For the current ready child Task, load and follow these built-in skills in order:

1. `silieco-start-feature`
2. `silieco-implement-feature`

Resume from the first incomplete activity when prior evidence remains valid. This skill runs focused developer checks only; `silieco-verify-feature` belongs to the next SOP Stage and must not be invoked here.

## Failure policy

- A preflight or implementation blocker stops the run and records the reason.
- A failed preflight or implementation check leaves the child `blocked` with a concrete reason.
- Never invoke `silieco-verify-feature` or `silieco-complete-feature`, merge, tag, archive, or transition the SOP from this skill.

## Required output

Return the preflight result and implementation handoff in the current child Task. When the child is ready for review, leave later barrier groups in `backlog`; the parent coordinator promotes them only after the current barrier closes. Silieco owns scheduling and Stage transitions; do not create a parallel queue or autonomous batch loop.
