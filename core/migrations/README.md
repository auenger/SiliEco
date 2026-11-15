# Task domain database baseline

`0001_task_domain_baseline` starts schema generation 2. This migration set is for
fresh databases. The runner rejects a database containing a prior generation or
untracked application tables; it never resets data automatically.

Business work is stored in `task`; execution records are stored in `agent_run`.
Business references use `task_id`, and execution references use `run_id`.
Relationships and dependent cleanup belong to application queries and transactions.
The schema intentionally contains no foreign keys or cascading actions.

The baseline was derived from the complete effective predecessor schema, not just
its original initialization migration. It retains its columns, defaults, checks,
unique guarantees, SQL functions, triggers, extensions and rollup-state seed.
Historical migrations and upgrade-only hooks are available in Git history.

Every index, including the backing indexes for primary and unique keys, is built
in its own concurrent migration. The final key-constraint migration attaches
those existing indexes without rebuilding them. Reverse migration removes key
constraints before indexes, then removes application tables and functions.

When adding migrations, use a new unique four-digit prefix and include both up
and down directions. Keep each concurrent index build in a single-statement file.
Run `make sqlc` after changing schema or queries. Migration concurrency, generation
guards and relational cleanup have focused tests under `core/cmd/migrate`,
`core/internal/migrations` and `core/pkg/db`.
