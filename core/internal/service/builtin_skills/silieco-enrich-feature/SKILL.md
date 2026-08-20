---
name: silieco-enrich-feature
description: "Use in the Feature SOP PRD stage to turn an approved requirement brief into one lightweight, implementation-ready PRD grounded in the current Project."
user-invocable: false
---

# Enrich Feature

## Purpose

Turn the approved requirement brief into one PRD that is detailed enough for the first version. Avoid documentation that does not change implementation or acceptance.

## Input

- `feature-brief.md` or the equivalent approved Task description.
- Current Project resources, repository guidance, and relevant existing behavior.

## Procedure

1. Load the current Project description, resources, repository guidance, applicable instructions, and related completed work.
2. Preserve the approved scope. Make assumptions and unresolved decisions explicit instead of silently inventing requirements.
3. Define actors, primary flow, important failure flow, data/permission rules, and non-functional constraints only where relevant.
4. Give every requirement a stable ID (`R1`, `R2`, ...) and an objective acceptance condition.
5. Add candidate development slices and their likely order as planning input; do not create Tasks in this Stage.

## Required output

Produce one `PRD.md` containing: context, goals/non-goals, requirements, flows, constraints, acceptance conditions, open decisions, and candidate development slices. Separate `task.md` and `checklist.md` are not required in v1.

Link or attach the PRD to the current Task and summarize unresolved decisions. This Stage is complete when each in-scope behavior has an acceptance condition. Do not begin implementation or advance the SOP.
