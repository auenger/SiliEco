---
name: silieco-start-feature
description: "Use inside the Feature SOP development run for one ready child Task to perform repository and dependency preflight before implementation."
user-invocable: false
---

# Start Feature

## Input

- One `todo` or `in_progress` development child Task with parent, barrier group, and SOP assignment.
- Its dependency descriptions, Project repository resources, and repository instructions.

## Preflight

1. Confirm the current child Task belongs to the intended parent, Project, SOP run, and `开发实现` Stage.
2. Read `silieco task children <parent-id>` and confirm this child's described dependencies and earlier barrier groups are terminal.
3. Read repository instructions and inspect the worktree for existing user changes.
4. Resolve the primary repository and local directory from Project resources.
5. Reuse the provided checkout when safe. Create a branch or worktree only when the Task or repository workflow requires isolation.

## Safety

Never discard, overwrite, stash, or commit unrelated user changes. Fetching, pulling, branching, or creating a worktree must stay within the user's authorization and repository policy.

## Required output

Return a short preflight result: repository/path, base revision, working location, dependency status, authoritative local commands, and blockers. This is an internal handoff to `silieco-implement-feature`, not a separate required Markdown file. Stop before implementation when preflight fails.
