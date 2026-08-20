---
name: silieco-prototype-feature
description: "Use in the Feature SOP prototype stage to decide whether a prototype is needed and, only when useful, validate the highest-risk assumption with a minimal mock, MVP, or spike."
user-invocable: false
---

# Prototype Feature

## Purpose

Avoid making every Feature pay a prototype tax. First decide whether the approved PRD contains an interaction, integration, data, or technical uncertainty that is cheaper to validate now than during development.

## Input

- Approved `PRD.md` and `spec-review.md`.
- Existing product behavior and repository context needed to judge feasibility.

## Procedure

1. Record `needed` or `not-needed` and the reason.
2. If needed, test only the smallest high-risk flow or assumption.
3. Keep disposable prototype code isolated unless the Task explicitly makes it production-bound.
4. Record what is simulated, omitted, unsafe, or not production-ready and whether the PRD must change.

## Required output

Always produce `prototype-decision.md`. When needed, also produce a reviewable prototype/MVP/spike and concise run instructions. When not needed, the decision record is the complete output.

Post the result to the current Task for the human Gate. Do not treat a visual mock as proof of backend, security, performance, or integration feasibility, and do not advance the SOP yourself.
