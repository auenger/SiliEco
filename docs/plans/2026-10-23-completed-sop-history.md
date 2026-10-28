# Preserve completed SOP run history

## Problem

Completing JAMOM_Develop preserved its run, Tasks and artifacts on the server, but
removed its sidebar link and excluded it from the Project Stage selector. The
Project SOP list still showed the completed run but hid its board action.

## Behavior

- Keep unarchived completed runs in the Project sidebar and Stage selector.
- Allow the Project SOP list to open a completed run's existing board.
- Preserve completed status until new work is successfully added.
- Task-driven plans (all Stages use manual execution) can accept follow-up Tasks.
  Creation or new membership atomically reactivates the same Run at the selected
  Stage. Earlier-stage additions rewind the review cursor; later-stage additions
  do not skip the current gate. New membership refreshes the review state token.
- Default follow-up requirements to the first Stage, not the last completed one.
- Preserve previous Tasks and gate decisions. New work must pass fresh review.
- Keep accepted agent-stage executions, archived Runs and cancelled Runs locked.
- Expose `accepts_new_tasks` in Run detail/list responses. Older servers do not
  advertise continuation, so clients keep their existing read-only behavior.
- Invalidate Run detail/list caches after Task creation, attachment and websocket
  updates so the board and sidebar show the reopened state.
- Archived and cancelled runs remain excluded from the sidebar and Stage selector.

No existing Tasks, artifacts, or gate decisions are deleted or recreated.
The reopen and task write share a transaction and Run lock; failed creation or
assignment rolls back the state change. No database migration is required.

## Validation

- Five view suites: 61 tests passed.
- Core schema, issue mutations and realtime suites: 151 tests passed.
- Go workflow service and handler suites passed against a dedicated PostgreSQL 17
  database (all migrations applied). Coverage includes two rounds in the same
  Run, new unfinished work blocking review, both assignment endpoints, preserved
  gate decisions, archived/cancelled/agent-stage exclusions and existing locking.
- Shared core and views TypeScript checks passed.

## Deployment

Both the 6688 API and Web client need updating. The local desktop renderer picks
up the source change, but continuation remains unavailable until the API exposes
the new capability. Never patch production Run statuses to simulate this fix.

### Local release validation

Linux AMD64 backend/CLI and production Web images built as
`v0.0.1-24-g6b41604` from the current uncommitted working tree. The backend
container health endpoint returned OK. Web containers returned HTTP 200 for
home, login, download and the Task route shell; all 73 referenced static assets
returned 200. This is an HTTP smoke check, not an authenticated browser test.

Artifacts and checksums: `dist/sop-continuation-2026-10-23/`.
Remote deployment completed using the explicit SSH identity
`/Users/ryan/Downloads/ryanlocal.pem`. The default identity was not accepted.
Both 6688 application containers were replaced; the database and all other
application containers were preserved. Desktop installers remain unchanged in a
read-only host mount; the deployed Web image uses the `-runtime` tag.
Production health, 73 static assets, authenticated JAMOM preservation/capability
checks and the actual local desktop create-Task affordance passed. No production
Task was created for verification. See `服务器连接与6688发布.md` and the release
folder's `deployment-result.json` for backup and rollback details.
