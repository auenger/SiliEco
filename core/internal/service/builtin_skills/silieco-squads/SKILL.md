---
name: silieco-squads
description: "Use when creating, inspecting, updating, assigning, mentioning, or debugging Silieco squads. Explains what squads are, squad/member fields, CLI commands, leader routing, task assignment, comments, mentions, autopilot behavior, leader briefing, side effects, and product-gap handling."
user-invocable: false
allowed-tools: Bash(silieco *)
---

# Silieco Squads

## Quick start

If debugging why a squad did or did not run, inspect first:

```bash
silieco task get <task-id> --output json
silieco squad get <squad-id> --output json
silieco squad member list <squad-id> --output json
silieco task comment list <task-id> --recent 10 --output json
```

If the command shape is unclear, check help instead of guessing:

```bash
silieco squad --help
silieco squad member --help
silieco task update --help
silieco task comment add --help
```

Do not assign, comment, mention, update, delete, or record squad activity just
to test. These can mutate workspace state or trigger agent runs.

## Core model

A Silieco squad is a workspace routing and coordination object.

A squad is not an agent. It does not run work by itself. Current behavior:
squad-routed work runs through the squad's `leader_id` agent.

Important consequences:

- assigning a task to a squad routes to the leader;
- mentioning a squad routes to the leader;
- squad-assigned autopilot resolves to the leader;
- squad members are not automatically fanned out;
- squad `instructions` are leader briefing content, not member prompts.

## CLI

Squad commands:

```bash
silieco squad list --output json
silieco squad get <squad-id> --output json
silieco squad create --name <name> --leader <agent-name-or-id> --output json
silieco squad update <squad-id> --instructions "<leader coordination policy>" --output json
silieco squad delete <squad-id>
```

Member commands:

```bash
silieco squad member list <squad-id> --output json
silieco squad member add <squad-id> --member-id <id> --type agent|member --role <role> --output json
silieco squad member remove <squad-id> --member-id <id> --type agent|member
silieco squad member set-role <squad-id> --member-id <id> --member-type agent|member --role <role> --output json
```

Squad leader evaluation command:

```bash
silieco squad activity <task-id> action|no_action|failed --reason "<why>" --output json
```

`activity` is a write: it records the leader's evaluation decision on a task.
Use it only when acting as the squad leader after evaluating a trigger.

Task/comment commands often needed with squads:

```bash
silieco task get <task-id> --output json
silieco task update <task-id> --help
silieco task comment list <task-id> --output json
silieco task comment add <task-id> --help
```

Prefer `--output json` for reads. Use `--help` before writes.

## Squad fields

- `id` — squad UUID.
- `workspace_id` — workspace the squad belongs to.
- `name` — display name; unique per workspace.
- `description` — human-facing metadata/display text. Do not assume runtime
  prompt impact unless source proves a consumer.
- `instructions` — squad-level instructions added to the squad leader briefing.
  They are not directly injected into every squad member.
- `avatar_url` — optional squad avatar URL.
- `leader_id` — agent ID of the squad leader; the runtime target for
  squad-routed work.
- `creator_id` — creator of the squad.
- `archived_at` / `archived_by` — archive metadata. Archived squads are rejected
  by assignment/autopilot routing paths.
- `member_count` — list response count of squad members.
- `member_preview` — list response preview of squad members.

Use `instructions` for leader-facing coordination policy: squad responsibility,
delegation expectations, when to ask humans, and review/handoff rules. Do not
write it as if every member automatically receives it.

## Squad member fields

- `member_type` — `agent` or `member`.
- `member_id` — ID of the agent or workspace member.
- `role` — roster role label. Current behavior: non-empty `role` appears in the
  leader briefing roster. Do not assume it creates scheduling, permissions, or
  routing behavior.

## Creation and leader membership

Creating a squad requires `leader_id`. The leader must be a workspace agent.
Create/update does not reject an archived leader: the lookup only checks the
agent exists in the workspace. An archived leader fails closed later, at
routing/dispatch — assignment, autopilot admission, and the comment/mention
readiness gate all reject an archived leader before any task is enqueued.

On create, the backend attempts to add the leader as a squad member with role
`leader`. When updating `leader_id`, if the new leader is not already a member,
the backend adds the new leader as a squad member with role `leader`.

## Leader briefing

For squad leader tasks, Silieco appends a squad leader briefing to the leader
agent instructions. The briefing includes:

- Squad Operating Protocol;
- Squad Roster;
- Squad Instructions, only when `instructions` is non-empty.

Roster entries include member name, member type, mention markdown, and non-empty
role. For agent members the roster also lists their assigned skills
(`skills: a, b`, or `no skills assigned` when the agent has none) so the leader
can delegate by capability instead of guessing from the role label; human
members carry no skills segment. Builtin `silieco-*` skills are not listed —
only the workspace skills explicitly attached to the agent. Archived agent
members are skipped from the briefing roster.

## Task assignment behavior

Tasks can be assigned to squads with:

```text
assignee_type = "squad"
assignee_id = <squad-id>
```

Current behavior:

- assignment routes work to `squad.leader_id`;
- it does not enqueue every squad member;
- assignment while status is `backlog` does not immediately start work;
- moving a squad-assigned task out of `backlog` can trigger the leader;
- changing assignee cancels existing tasks for the task before enqueueing the
  new assignee path;
- parent task status is agent-managed (same model as direct agent assignment):
  the leader's first assignment turn should move the parent to `in_progress`
  and keep it there while members work; the leader moves the parent to
  `in_review` only when a later re-trigger confirms the overall goal is met.
  Completing a leader `task` (including the first dispatch) does not itself
  change task status;
- that status authority is granted only when the task's `assignee_type` /
  `assignee_id` point at THIS squad. The leader briefing is injected on every
  leader path, including an `@squad` mention on a task owned by a plain agent
  — on those paths the protocol instead carries an explicit "do not change this
  task's status".

Assignment validation rejects a missing type/id pair, non-existent squad,
archived squad, archived leader, and private leader when the actor cannot access
it.

## Comment and mention behavior

If a task is assigned to a squad, a new comment can wake the squad leader. This
is leader routing, not member fan-out.

Squad mention format:

```md
[@Squad Name](mention://squad/<squad-id>)
```

Current behavior: resolve the squad, read `leader_id`, enqueue a leader task,
and use the current comment as the trigger comment. It does not enqueue every
squad member.

## Autopilot behavior

Autopilots can be assigned to squads. For `assignee_type = "squad"`:

- executable agent resolves from `squad.leader_id`;
- admission/readiness checks run against the leader;
- archived squads fail closed / skip dispatch;
- run attribution records squad id where applicable.

For `create_task` autopilots, the created task keeps `assignee_type = "squad"`
and `assignee_id = <squad-id>`, while the actual executing agent is the resolved
leader. For `run_only` autopilots, no task is created; the task is created
directly for the resolved leader agent.

## Handling complaints or product gaps

When the user says squad behavior is wrong, confusing, or disappointing, do not
immediately assume code is broken and do not defend current behavior just because
it exists. Classify first:

- expected current behavior;
- configuration task;
- product limitation;
- actual bug.

Explain the current source-backed behavior. If the behavior is technically
correct but product-wise bad, say so and propose a scoped product/code change.

Do not silently change squad routing, member fan-out, leader briefing, autopilot
behavior, or comment-trigger behavior without confirmation. These are product
contract changes with side effects.

## Side effects

These actions can trigger agent work or mutate durable state:

- creating a squad;
- updating squad fields;
- changing `leader_id`;
- adding/removing members;
- changing member roles;
- assigning a task to a squad;
- moving a squad-assigned task out of backlog;
- commenting on a squad-assigned task;
- mentioning a squad;
- creating or triggering squad-assigned autopilots;
- recording squad activity with `silieco squad activity`;
- deleting/archive squad.

Do not perform side-effecting actions as tests unless the user explicitly
authorizes them.

## Common wrong assumptions

- A squad is not an agent.
- Squad work routes to `leader_id`, not every member.
- Squad mention routes to the leader, not every member.
- Squad assignment routes to the leader, not every member.
- Squad autopilot resolves to the leader as executable agent.
- `instructions` are leader briefing content, not automatic member prompts.
- `description` is not proven runtime prompt content.
- `role` is roster context, not automatic scheduling.
- Backlog assignment does not immediately start work.
- First leader dispatch is not parent completion — parent stays `in_progress`
  until the leader later confirms the overall goal and moves it to `in_review`.
- The server does not auto-flip parent status when child tasks finish; it only
  wakes the leader with an explicit ask (including `in_review` when wrapping up).
- Getting the leader briefing does NOT imply status authority. A squad
  `@`-mentioned into a task assigned to someone else is a guest: roster and
  delegation rules yes, `silieco task status` no.

## References

For source paths, tests, edge cases, and exact routing details, see:

```text
references/squad-source-map.md
```
