---
name: silieco-split-feature
description: "Use in the Feature SOP planning stage to turn the current large Task into small, parent-linked Silieco child Tasks, grouped into safe execution barriers and kept parked until development starts."
user-invocable: false
---

# Split Feature into Silieco Child Tasks

## Input

- The current large Task, approved `PRD.md`, `spec-review.md`, and prototype decision/result.
- The current SOP run ID and, when available, the immutable `开发实现` Stage ID for that run.

## Rules

- Prefer vertical, verifiable slices; use technical-only children when shared foundation or migration work is genuinely required.
- Each child must be small enough for one focused implementation cycle and have an objective done condition.
- Preserve the parent as the scope and progress index.
- Express order with Silieco child `--stage N` barrier groups; describe exact dependencies in each child.
- Avoid overlapping ownership of one acceptance scenario across children.
- Create new development children as `backlog` so planning does not start all agents early.

## Procedure

1. Map every PRD requirement ID to exactly one owning child, with supporting children referenced where needed.
2. Define each child with: goal, input, in/out of scope, expected output, acceptance checks, dependencies, and requirement IDs.
3. Put parallel children in the same `--stage N`; put dependent work in later numbered groups. Check for gaps, overlap, and cycles.
4. Create the children under the current Task. Example shape:

   ```bash
   silieco task create --title "..." --description-file <child.md> \
     --parent <current-task-id> --stage 1 --status backlog \
     --workflow-instance <run-uuid> --workflow-stage <development-stage-uuid>
   ```

5. If the development Stage UUID is unavailable, never guess it. Create parent-linked `backlog` children without the workflow flags, report that they still need UI attachment to the `开发实现` Stage, and keep the planning Task open until that handoff is confirmed.
6. Verify with `silieco task children <current-task-id>`. Keep the large parent as the scope and progress coordinator; do not replace it with a Markdown queue.

## Required output

Produce real Silieco child Tasks plus a concise parent comment listing child IDs by barrier group and requirement coverage. All children remain `backlog`; after the SOP enters `开发实现`, a human or parent coordinator promotes only the first ready group to `todo`.

This Stage is complete when every requirement is covered, every child is parent-linked, ordering is visible, and SOP Stage attachment is confirmed. Do not create `queue.yaml`, `workflow-state.json`, worktrees, branches, or implementation code here. `--stage N` is a child barrier group; `--workflow-stage <uuid>` is the SOP Stage assignment.
