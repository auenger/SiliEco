---
name: silieco-autopilots
description: "Use when creating, updating, inspecting, triggering, or debugging Silieco autopilots. Covers the full chain: schedule/webhook/manual trigger, create_task vs run_only execution, agent/squad leader admission, runs, created tasks/tasks, webhook URL rotation, and side-effect boundaries."
user-invocable: false
allowed-tools: Bash(silieco *)
---

# Silieco Autopilots

## Quick start

Autopilots are durable automations. Read before mutating:

```bash
silieco autopilot list --output json
silieco autopilot get <autopilot-id> --output json
silieco autopilot runs <autopilot-id> --output json
```

Do not run `trigger`, `delete`, `trigger-delete`, or `trigger-rotate-url` to test. Those are real side effects.

## Core model

An autopilot is not an agent. It is a rule that dispatches work to an agent, or to a squad's leader agent.

The chain is: trigger fires (`schedule`, `webhook`, or `manual`) -> `autopilot_run` row -> `execution_mode` decides output -> assignee readiness check -> task/task execution -> run status sync. Webhooks have a durable admission step in front: HTTP ingress stores a queued `webhook_delivery`, synchronously creates or reuses its idempotent run, and returns `200` with `status=accepted|skipped` plus `run_id`; a database-leased worker then resumes accepted runs and owns recoverable task/task dispatch.

Execution modes:

- `create_task` creates a Silieco task, making the run visible as task state.
- `run_only` creates an agent task directly. No task is created; any durable
  report location has to come from other task context or instructions.

`task-title-template` only supports `{{date}}`. Do not invent `{{trigger_id}}`, `{{branch}}`, or other variables.

## CLI

```bash
silieco autopilot list --output json
silieco autopilot get <autopilot-id> --output json
silieco autopilot create --title "<title>" --description "<task prompt>" --agent <agent-name-or-id> --mode create_task|run_only --output json
silieco autopilot update <autopilot-id> --status active|paused --output json
silieco autopilot runs <autopilot-id> --output json
silieco autopilot trigger-add <autopilot-id> --kind schedule --cron "0 9 * * *" --timezone Asia/Shanghai --output json
silieco autopilot trigger-add <autopilot-id> --kind webhook --label "ci" --output json
silieco autopilot trigger <autopilot-id> --output json
silieco autopilot trigger-rotate-url <autopilot-id> <trigger-id> --yes --output json
```

Use `trigger` only when the user explicitly asks for a manual run. Use `trigger-rotate-url` only when rotating a webhook URL; the old URL stops being valid.

Webhook trigger output can include a URL/token. Do not paste webhook tokens or signing material into comments, logs, docs, or PRs. Redact secrets.

## Debugging

For "why didn't it run":

1. `silieco autopilot get <id> --output json` — status, mode, assignee, triggers.
2. `silieco autopilot runs <id> --output json` — run status and failure reason.
3. If assigned to a squad, inspect the squad: `silieco squad get <squad-id> --output json`; execution goes to the leader.
4. Inspect the target agent/runtime: `silieco agent get <agent-id> --output json` and `silieco runtime list --output json`.
5. For webhooks, inspect delivery status: `queued` means the worker has not completed dispatch; `failed` carries the worker error. A provider retry with the same `X-GitHub-Delivery` / `Idempotency-Key` reuses the original delivery.
6. For `create_task`, inspect the created task if the run records one.

## Side effects

These mutate durable state or start work: `create`, `update`, `delete`, trigger add/update/delete/rotate, `trigger`, and webhook calls to `/api/webhooks/autopilots/{token}`.

More source-backed details: `references/autopilots-source-map.md`.
