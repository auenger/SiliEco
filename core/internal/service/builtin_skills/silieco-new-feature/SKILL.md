---
name: silieco-new-feature
description: "Use in the Feature SOP intake stage to turn the current large Task and its source material into a small, testable requirement brief without starting implementation."
user-invocable: false
---

# Feature Requirement Intake

## Purpose

Convert the current large Task, comments, attachments, and Project context into enough shared understanding for a first implementation. Keep Silieco as the workflow state source.

## Input

- The current Task and its parent/related Task context.
- User feedback, business goal, attachments, and relevant Project resources.

## Procedure

1. Identify the target user, problem, desired outcome, dependencies, and hard constraints.
2. Separate in-scope behavior from explicit out-of-scope behavior.
3. State a small set of observable acceptance conditions, including the main failure path when relevant.
4. List open questions only when their answers change scope, data, permissions, or acceptance.
5. Flag likely independent delivery slices for the later split Stage; do not create child Tasks yet.

## Required output

Produce `feature-brief.md`, or an equivalent Markdown section in the current Task when a separate file adds no value. Include: goal, users, in/out of scope, constraints, acceptance conditions, dependencies, risks, open questions, and likely slices.

Post the artifact or its location plus unresolved questions to the current Task. This Stage is complete when scope and acceptance are usable by the PRD Stage. Do not implement code, create child Tasks, or advance the SOP.
