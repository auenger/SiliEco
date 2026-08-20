# Runtimes and repos source map

- `core/cmd/silieco/cmd_runtime.go` registers `runtime list`, `usage`, `activity`, `update`, and `delete`.
- `runtime list` reads `/api/runtimes` and prints `id`, `name`, `runtime_mode`, `provider`, `status`, and `last_seen_at`.
- `runtime update` posts to `/api/runtimes/{runtime-id}/update`; with `--wait` it polls update status. Initiation enforces runtime-owner or workspace-owner/admin access through `canEditRuntime`; status polling additionally permits that request's immutable initiator so an in-flight poll survives an admin-role change (`core/internal/handler/runtime_update.go` and `runtime.go`).
- `runtime delete` deletes `/api/runtimes/{runtime-id}`; with `--cascade`, it first reads the `runtime_has_active_agents` conflict payload and posts those ids to `/api/runtimes/{runtime-id}/archive-agents-and-delete`.
- `core/cmd/silieco/cmd_repo.go` registers `repo checkout <url> [--ref]`.
- `repo checkout` requires `SILIECO_DAEMON_PORT` and `SILIECO_TOKEN`, sends a Bearer task credential plus compatibility fields to local daemon `/repo/checkout`, then prints the checked-out path.
- `core/internal/daemon/health.go` resolves workspace, task, agent, and allowed workdir from the active token registry rather than trusting request-body identity. Request `ref` wins; otherwise it asks `core/internal/daemon/daemon.go` for the token-bound task's project repo default ref. It forwards the validated isolated-checkout mode into `repocache.WorktreeParams` and rejects missing, expired, mismatched, or out-of-root requests.
- `core/internal/handler/runtime.go` is the runtime permission source: public runtimes are workspace-usable, private runtimes are owner-only, ownerless runtimes fail closed, and only the runtime owner may change visibility.
- `core/internal/daemon/daemon.go` injects `SILIECO_REPO_CHECKOUT_MODE=isolated` only for Linux Codex tasks. `core/internal/daemon/repocache/cache.go` implements that mode as a same-filesystem local clone with task-local Git metadata and the real repository as `origin`; other runtimes keep the linked-worktree path.
- When the bare cache is a partial clone, that isolated checkout must have `remote.origin.promisor` / `partialclonefilter` restored before its first `checkout`: `git clone --local` neither inherits them nor errors on the missing objects it leaves behind, so the checkout would otherwise succeed with an empty working tree. The linked-worktree path shares the cache's own config and needs no such repair.
- `core/cmd/core/router.go` registers daemon APIs under `/api/daemon`, including workspace repos and task claim.
- `core/internal/daemon/daemon.go` claims tasks, prepares workdirs, launches provider CLIs, and reports completion.
- `core/internal/daemon/execenv/runtime_config.go` injects task/project/repo context into agent workdirs.
