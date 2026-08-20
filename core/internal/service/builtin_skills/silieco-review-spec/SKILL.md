---
name: silieco-review-spec
description: "Use in the Feature SOP PRD-review stage to find only the specification problems that would block a safe first implementation or objective acceptance."
user-invocable: false
---

# Review Feature Specification

## Input

- `PRD.md`, the source requirement brief, and current Project/repository context.

## Review

Check that scope is unambiguous, each requirement is testable, the primary and material failure paths are covered, and the proposal does not conflict with known architecture, security, migration, or external dependency constraints. Do not demand enterprise-level detail for a low-risk first version.

## Required output

Produce `spec-review.md` with:

- status: `pass` or `block`;
- blocking findings with requirement IDs and a concrete correction;
- non-blocking notes kept short;
- a Gate recommendation.

Any unresolved issue that changes scope, data safety, permissions, or acceptance means `block`. Post the report to the current Task. Do not approve the human Gate, advance the SOP, or silently rewrite product decisions.
