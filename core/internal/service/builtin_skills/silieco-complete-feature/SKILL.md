---
name: silieco-complete-feature
description: "Use in the Feature SOP completion stage to summarize verified delivery, reconcile child Tasks, and prepare the final human completion decision without assuming merge or release authority."
user-invocable: false
---

# Complete Feature

## Input

- The parent Feature Task, its child Task list, the approved PRD, and the passing verification result.
- Existing PR/commit/release references and repository delivery policy, when applicable.

## Preconditions

Begin only with a passing verification result, all required child Tasks terminal, and explicit accounting for cancelled or deferred scope. The human Gate happens after this Stage's work; do not claim it is already approved.

## Procedure

1. Reconcile the parent and child Tasks against PRD requirement IDs; list completed, cancelled, blocked, and deferred scope.
2. Confirm the verification result still matches the delivered revision.
3. Summarize user-visible behavior, migration/configuration needs, known limitations, and rollback notes where relevant.
4. Link the PRD, review, prototype decision, implementation handoffs, verification result, and PR/commit identifiers that already exist.
5. Commit, merge/rebase, push, tag, publish, delete branches, or remove worktrees only when the user and repository policy explicitly authorize those actions. Never guess through conflicts.
6. Record reusable project knowledge only when it will help later work.

## Required output

Produce `completion-summary.md` or an equivalent parent Task comment with delivery status, child Task reconciliation, verification reference, release notes, known limitations, follow-ups, and any Git/release actions actually performed.

If a precondition fails, return `block` with the exact missing item. Otherwise return `ready-for-human-completion`. Do not approve the Gate, advance the SOP, or mark unfinished children complete.
