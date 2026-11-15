# CLI and Agent Daemon Guide

The `silieco` CLI connects your local machine to Silieco. It handles authentication, Space and Task management, and runs the Agent Daemon that executes AI work locally.

> **Task** is the business work item; **AgentRun** is one agent execution.
> Use `silieco task ...`, `/api/tasks`, and `/api/runs`. This development
> reset requires the same protocol generation across the server, CLI,
> daemon, and apps; retired commands and wire formats are not supported.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install silieco-ai/tap/silieco
```

### Build from Source

```bash
git clone https://github.com/auenger/SiliEco.git
cd SiliEco
make build
cp core/bin/silieco /usr/local/bin/silieco
```

### Update

```bash
brew upgrade silieco-ai/tap/silieco
```

For install script or manual installs, use:

```bash
silieco update
```

`silieco update` auto-detects your installation method and upgrades accordingly.

## Quick Start

```bash
# One-command setup: configure, authenticate, and start the daemon
silieco setup

# For self-hosted (local) deployments:
silieco setup self-host
```

Or step by step:

```bash
# 1. Authenticate (opens browser for login)
silieco login

# 2. Start the agent daemon
silieco daemon start

# 3. Done — agents in your watched workspaces can now execute tasks on your machine
```

`silieco login` automatically discovers all workspaces you belong to and adds them to the daemon watch list.

## Authentication

### Browser Login

```bash
silieco login
```

Opens your browser for OAuth authentication, creates a 90-day personal access token, and auto-configures your workspaces.

### Token Login

```bash
silieco login --token <sili_...>
```

Authenticate using a personal access token directly. Useful for headless environments. Pass `--token=` with an empty value to be prompted interactively (so the token never lands in shell history).

### Check Status

```bash
silieco auth status
```

Shows your current server, user, and token validity.

### Logout

```bash
silieco auth logout
```

Removes the stored authentication token.

## Agent Daemon

The daemon is the local agent runtime. It detects available AI CLIs on your machine, registers them with the Silieco server, and executes tasks when agents are assigned work.

### Start

```bash
silieco daemon start
```

By default, the daemon runs in the background and logs to `~/.silieco/daemon.log`.

To run in the foreground (useful for debugging):

```bash
silieco daemon start --foreground
```

### Stop

```bash
silieco daemon stop
```

### Status

```bash
silieco daemon status
silieco daemon status --output json
```

Shows PID, uptime, detected agents, and watched workspaces.

### Logs

```bash
silieco daemon logs              # Last 50 lines
silieco daemon logs -f           # Follow (tail -f)
silieco daemon logs -n 100       # Last 100 lines
```

### Supported Agents

The daemon auto-detects these AI CLIs on your PATH:

| CLI | Command | Description |
|-----|---------|-------------|
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | `claude` | Anthropic's coding agent |
| [Codex](https://github.com/openai/codex) | `codex` | OpenAI's coding agent |
| [GitHub Copilot CLI](https://docs.github.com/en/copilot) | `copilot` | GitHub's coding agent (model routed by your GitHub entitlement) |
| OpenCode | `opencode` | Open-source coding agent |
| OpenClaw | `openclaw` | Open-source coding agent |
| Hermes | `hermes` | Nous Research coding agent |
| Gemini | `gemini` | Google's coding agent |
| [Pi](https://pi.dev/) | `pi` | Pi coding agent |
| [Cursor Agent](https://cursor.com/) | `cursor-agent` | Cursor's headless coding agent |
| Kimi | `kimi` | Moonshot coding agent |
| Kiro CLI | `kiro-cli` | Kiro ACP coding agent |
| [Qoder CLI](https://docs.qoder.com/) | `qodercli` | Qoder ACP coding agent |
| [Trae](https://docs.trae.cn/cli) | `traecli` | ByteDance TRAE CLI (ACP via `traecli acp serve`) |
| [Grok Build CLI](https://docs.x.ai/) | `grok` | xAI Grok Build CLI (ACP via `grok agent stdio`) |
| [Qwen Code](https://github.com/QwenLM/qwen-code) | `qwen` | Alibaba Qwen Code (`qwen -p` with stream-json) |

You need at least one installed. The daemon registers each detected CLI as an available runtime.

### How It Works

1. On start, the daemon detects installed agent CLIs and registers a runtime for each agent in each watched workspace
2. It polls the server at a configurable interval (default: 3s) for claimed tasks
3. When a task arrives, it creates an isolated workspace directory, spawns the agent CLI, and streams results back
4. Heartbeats are sent periodically (default: 15s) so the server knows the daemon is alive
5. On shutdown, all runtimes are deregistered

### Configuration

Daemon behavior is configured via flags or environment variables:

| Setting | Flag | Env Variable | Default |
|---------|------|--------------|---------|
| Poll interval | `--poll-interval` | `SILIECO_DAEMON_POLL_INTERVAL` | `3s` |
| Heartbeat interval | `--heartbeat-interval` | `SILIECO_DAEMON_HEARTBEAT_INTERVAL` | `15s` |
| Agent timeout | `--agent-timeout` | `SILIECO_AGENT_TIMEOUT` | `0` (no cap; bounded by the watchdogs) |
| Codex semantic inactivity timeout | `--codex-semantic-inactivity-timeout` | `SILIECO_CODEX_SEMANTIC_INACTIVITY_TIMEOUT` | `10m` |
| OpenCode idle watchdog | — | `SILIECO_OPENCODE_IDLE_WATCHDOG` | `10m` (`0` falls back to the generic idle watchdog; cannot extend it) |
| Max concurrent runs | `--max-concurrent-runs` | `SILIECO_DAEMON_MAX_CONCURRENT_RUNS` | `20` |
| Daemon ID | `--daemon-id` | `SILIECO_DAEMON_ID` | hostname |
| Device name | `--device-name` | `SILIECO_DAEMON_DEVICE_NAME` | hostname |
| Runtime name | `--runtime-name` | `SILIECO_AGENT_RUNTIME_NAME` | `Local Agent` |
| Workspaces root | — | `SILIECO_WORKSPACES_ROOT` | `~/silieco_workspaces` |
| GC enabled | — | `SILIECO_GC_ENABLED` | `true` (set `false`/`0` to disable) |
| GC scan interval | — | `SILIECO_GC_INTERVAL` | `2h` |
| GC TTL (done/cancelled tasks) | — | `SILIECO_GC_TTL` | `24h` |
| GC orphan TTL (no `.gc_meta.json`) | — | `SILIECO_GC_ORPHAN_TTL` | `72h` |
| GC artifact TTL (open tasks) | — | `SILIECO_GC_ARTIFACT_TTL` | `12h` (set `0` to disable) |
| GC artifact patterns | — | `SILIECO_GC_ARTIFACT_PATTERNS` | `node_modules,.next,.turbo` |

#### Workspace garbage collection

The daemon periodically scans `SILIECO_WORKSPACES_ROOT` and reclaims disk space in three modes:

- **Full run cleanup** — when a task's status is `done` or `cancelled` and has been idle for `SILIECO_GC_TTL`, the entire run directory is removed.
- **Orphan cleanup** — run directories with no `.gc_meta.json` (e.g. left over from a daemon crash) are removed once they exceed `SILIECO_GC_ORPHAN_TTL`.
- **Artifact-only cleanup** — when a run has been completed for at least `SILIECO_GC_ARTIFACT_TTL` but the task is still open, regenerable build outputs whose directory basename matches `SILIECO_GC_ARTIFACT_PATTERNS` are removed. The daemon also reclaims the exact managed path `codex-home/.sandbox-bin`; old run metadata without `completed_at` becomes eligible for this managed-only cleanup after its `.gc_meta.json` file has been idle for `SILIECO_GC_ORPHAN_TTL`. The rest of the run (source, `.git`, `output/`, `logs/`, `.gc_meta.json`, Codex auth/config/session state) is preserved so the agent can resume it.

Configured patterns are basename-only — entries containing `/` or `\` are silently dropped — and `.git` subtrees are never descended into. The managed Codex cache is matched by its exact relative path, so a repository's own `.sandbox-bin` is not removed unless an operator explicitly adds that basename to `SILIECO_GC_ARTIFACT_PATTERNS`. The default list (`node_modules`, `.next`, `.turbo`) is intentionally narrow; extend it per deployment if your repos consistently produce other regenerable directories (for example, `SILIECO_GC_ARTIFACT_PATTERNS=node_modules,.next,.turbo,target,__pycache__`). To disable artifact cleanup entirely, including the managed Codex cache, set `SILIECO_GC_ARTIFACT_TTL=0`.

Agent-specific overrides:

| Variable | Description |
|----------|-------------|
| `SILIECO_CLAUDE_PATH` | Custom path to the `claude` binary |
| `SILIECO_CLAUDE_MODEL` | Override the Claude model used |
| `SILIECO_CLAUDE_ARGS` | Default extra arguments for Claude Code runs |
| `SILIECO_CODEX_PATH` | Custom path to the `codex` binary |
| `SILIECO_CODEX_MODEL` | Override the Codex model used |
| `SILIECO_CODEX_ARGS` | Default extra arguments for Codex runs |
| `SILIECO_COPILOT_PATH` | Custom path to the `copilot` binary |
| `SILIECO_COPILOT_MODEL` | Override the Copilot model used (note: GitHub Copilot routes models through your account entitlement, so this may not be honoured) |
| `SILIECO_OPENCODE_PATH` | Custom path to the `opencode` binary |
| `SILIECO_OPENCODE_MODEL` | Override the OpenCode model used |
| `SILIECO_OPENCLAW_PATH` | Custom path to the `openclaw` binary |
| `SILIECO_OPENCLAW_MODEL` | Override the OpenClaw model used |
| `SILIECO_HERMES_PATH` | Custom path to the `hermes` binary |
| `SILIECO_HERMES_MODEL` | Override the Hermes model used |
| `SILIECO_GEMINI_PATH` | Custom path to the `gemini` binary |
| `SILIECO_GEMINI_MODEL` | Override the Gemini model used |
| `SILIECO_PI_PATH` | Custom path to the `pi` binary |
| `SILIECO_PI_MODEL` | Override the Pi model used |
| `SILIECO_CURSOR_PATH` | Custom path to the `cursor-agent` binary |
| `SILIECO_CURSOR_MODEL` | Override the Cursor Agent model used |
| `SILIECO_KIMI_PATH` | Custom path to the `kimi` binary |
| `SILIECO_KIMI_MODEL` | Override the Kimi model used |
| `SILIECO_KIRO_PATH` | Custom path to the `kiro-cli` binary |
| `SILIECO_KIRO_MODEL` | Override the Kiro model used |
| `SILIECO_QODER_PATH` | Custom path to the `qodercli` binary |
| `SILIECO_QODER_MODEL` | Override the Qoder model used |
| `SILIECO_TRAECLI_PATH` | Custom path to the `traecli` binary |
| `SILIECO_TRAECLI_MODEL` | Override the Trae model used (a model id from your logged-in traecli catalog, e.g. `Doubao-Seed-2.1-Pro`) |
| `SILIECO_GROK_PATH` | Custom path to the `grok` binary (defaults to `grok` on PATH; often `~/.grok/bin/grok`) |
| `SILIECO_GROK_MODEL` | Override the Grok model used (e.g. `grok-4.5`) |
| `SILIECO_QWEN_PATH` | Custom path to the `qwen` binary |
| `SILIECO_QWEN_MODEL` | Override the Qwen Code model used |
| `SILIECO_QWEN_ARGS` | Daemon-wide extra Qwen arguments (POSIX shellword parsing; managed protocol flags are filtered) |

If a previously generated `~/.silieco/hooks` wrapper is first on `PATH` and calls the same command name again, the daemon skips that hooks directory during built-in agent discovery and records the real binary path behind it. If your interactive shell still recurses when you run `claude`, `codex`, or `hermes` manually, remove the hooks entry from your shell startup file or replace the wrapper body with an absolute `exec /path/to/real-binary "$@"`.

The daemon launches Qoder as `qodercli --yolo --acp`, matching Qoder’s ACP “bypass permissions” mode so tool runs do not block on interactive approval in headless runs.
The daemon launches Qwen Code as `qwen -p <prompt> --output-format stream-json`. It writes the task brief to `QWEN.md`; when an agent has managed `mcp_config`, the daemon writes a 0600 per-run JSON file and passes it through `--mcp-config <path>`, then removes it after the process exits. A null config preserves Qwen Code native MCP settings.


`SILIECO_CLAUDE_ARGS`, `SILIECO_CODEX_ARGS`, and `SILIECO_QWEN_ARGS` are parsed with POSIX shellword quoting, so values such as `--model "gpt-5.1 codex" --sandbox read-only` are split like a shell command line. Agent arguments are applied in this order: hardcoded Silieco defaults, daemon-wide env defaults, then per-agent `custom_args` from the task.

### Self-Hosted Server

When connecting to a self-hosted Silieco instance, the easiest approach is:

```bash
# One command — configures for localhost, authenticates, starts daemon
silieco setup self-host

# Or for on-premise with custom domains:
silieco setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

Or configure manually:

```bash
# Set URLs individually
silieco config set server_url http://localhost:8080
silieco config set app_url http://localhost:3000

# For production with TLS:
# silieco config set server_url https://api.example.com
# silieco config set app_url https://app.example.com

silieco login
silieco daemon start
```

### Profiles

Profiles let you run multiple daemons on the same machine — for example, one for production and one for a staging server.

```bash
# Set up a staging profile
silieco setup self-host --profile staging --server-url https://api-staging.example.com --app-url https://staging.example.com

# Start its daemon
silieco daemon start --profile staging

# Default profile runs separately
silieco daemon start
```

Each profile gets its own config directory (`~/.silieco/profiles/<name>/`), daemon state, health port, and workspace root.

## Workspaces

### Working with multiple workspaces

Every command runs against a single workspace. The CLI resolves which one in this order (highest priority first):

1. `--workspace-id <id>` flag on the command
2. `SILIECO_WORKSPACE_ID` environment variable
3. The default workspace stored in your current profile (set by `silieco workspace switch` or `silieco login`)

`silieco workspace switch <id|slug>` is the day-to-day way to change the default workspace. For scripting and headless setups where you don't want any stored state, prefer the `--workspace-id` flag or the env variable. `silieco config set workspace_id <id>` is the low-level equivalent of `switch` (it writes the same setting but skips the access check).

If you need full isolation between organizations or accounts — separate tokens, separate daemons, separate config dirs — use `--profile <name>` instead. Each profile keeps its own default workspace.

### List Workspaces

```bash
silieco workspace list
silieco workspace list --full-id
silieco workspace list --output json
```

The current default workspace is marked with `*`. Table output shows short UUID prefixes — pass `--full-id` when you need the canonical UUIDs.

### Switch Default Workspace

```bash
silieco workspace switch <workspace-id>
silieco workspace switch <slug>
```

Verifies you have access to the workspace, then sets it as the default for the current profile. Subsequent commands without `--workspace-id` and `SILIECO_WORKSPACE_ID` target this workspace. Pair `--profile` if you want to change a non-default profile's workspace.

### Get Details

```bash
silieco workspace get <workspace-id>
silieco workspace get <workspace-id> --output json
```

Passing no `<workspace-id>` resolves to the current default workspace, so `silieco workspace get` doubles as "what workspace am I on?".

### List Members

```bash
silieco workspace member list <workspace-id>
```

## Tasks

### List Tasks

```bash
silieco task list
silieco task list --status in_progress
silieco task list --priority urgent --assignee "Agent Name"
silieco task list --assignee-id 5fb87ac7-23b5-4a7a-81fa-ed295a54545d
silieco task list --full-id
silieco task list --limit 20 --output json
silieco task list --status todo --sort position       # board order (the default)
silieco task list --sort created_at --direction desc  # newest first
```

Table output shows a routable Task `KEY` such as `SILI-123`; copy that key into follow-up commands like `task get`, `task comment list`, `task status`, or `--parent`. Add `--full-id` when you need canonical UUIDs. Available filters: `--status`, `--priority`, `--assignee` / `--assignee-id`, `--project`, `--metadata`, `--limit`. Use `--assignee-id <uuid>` for unambiguous filtering when names overlap.

Results come back in board order (`position`, ascending) by default. Pass `--sort` to change the column (`position`, `title`, `created_at`, `start_date`, `due_date`, `priority`) and `--direction asc|desc` to flip the order. `position` is always ascending (it is the manual drag order), so `--direction` is rejected when `--sort` is `position` or omitted — use it only with `title`, `created_at`, `start_date`, `due_date`, or `priority`.

Use `--metadata key=value` (repeatable; combined with AND) to filter by per-task metadata. The value is JSON-parsed: `true`/`false` become bool, numbers become numbers, anything else is a string. Wrap as `'"42"'` to force a string when the value would otherwise sniff as a number:

```bash
silieco task list --metadata pipeline_status=waiting_review
silieco task list --metadata pr_number=482 --metadata is_blocked=true
```

### Get a Task

```bash
silieco task get <id>
silieco task get <id> --output json
```

### Create a Task

```bash
silieco task create --title "Fix login bug" --description "..." --priority high --assignee "Lambda"
silieco task create --title "Fix login bug" --assignee-id 5fb87ac7-23b5-4a7a-81fa-ed295a54545d
```

Flags: `--title` (required), `--description`, `--status`, `--priority`, `--assignee` / `--assignee-id`, `--parent`, `--project`, `--due-date`. Pass `--assignee-id <uuid>` (mutually exclusive with `--assignee`) when scripting against the IDs returned by `silieco workspace member list --output json` / `silieco agent list --output json`.

### Update a Task

```bash
silieco task update <id> --title "New title" --priority urgent
silieco task update <id> --position 4.5
```

`--position` sets the raw ordering value within the board column (lower sorts first). For relative moves, `task reorder` is easier because it works out the value for you.

### Reorder Task

Move a task within its current status column. The new ordering value is computed the same way the board's drag-and-drop computes it, so the CLI and UI agree on where the task lands.

```bash
silieco task reorder <id> --top              # top of its status column
silieco task reorder <id> --bottom           # bottom of its status column
silieco task reorder <id> --before <other>   # directly above another task in the same column
silieco task reorder <id> --after  <other>   # directly below another task in the same column
```

Pick exactly one of `--top`, `--bottom`, `--before`, or `--after`. Reorder stays inside the task's current column, so `--before` / `--after` must name a task in that same column. To move a task to a different column, change its status first with `task status`, then reorder within the new column.

### Assign Task

```bash
silieco task assign <id> --to "Lambda"
silieco task assign <id> --to-id 5fb87ac7-23b5-4a7a-81fa-ed295a54545d
silieco task assign <id> --unassign
```

Pass `--to-id <uuid>` to assign by canonical UUID (mutually exclusive with `--to`); useful when names overlap across members and agents.

### Change Status

```bash
silieco task status <id> in_progress
```

Valid statuses: `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`.

### Comments

```bash
# List comments — flat timeline, chronological. Hard cap of 2000 rows; on
# long-running tasks prefer one of the thread-aware reads below to keep
# context windows tight.
silieco task comment list <task-id>

# Single thread (root + every descendant). Anchor may be the root itself
# or any reply inside the thread — the server walks up to the root.
silieco task comment list <task-id> --thread <comment-id>

# Single thread, capped to the N most recent replies. The thread root is
# always included (even with --tail 0), so an agent landing on a long
# thread keeps the "what is this about" context without dragging hundreds
# of replies into its prompt.
silieco task comment list <task-id> --thread <comment-id> --tail 30

# Scroll older replies inside the same thread. --before / --before-id are
# the reply cursor that the previous response emitted on stderr as
# `Next reply cursor: --before <ts> --before-id <reply-id>`.
silieco task comment list <task-id> --thread <comment-id> --tail 30 \
    --before <ts> --before-id <reply-id>

# Most recently active threads (root + every descendant), grouped by
# thread. Returns N complete conversational arcs, oldest-active first so
# the freshest thread sits closest to "now" in an agent prompt.
silieco task comment list <task-id> --recent 10

# Scroll older threads. Under --recent, --before / --before-id are a
# THREAD cursor (thread last_activity_at + root id), emitted on stderr as
# `Next thread cursor: --before <ts> --before-id <root-id>`.
silieco task comment list <task-id> --recent 10 \
    --before <ts> --before-id <root-id>

# Incremental polling. Combines with --thread or --recent; filters out
# replies created on or before <ts> from the page (the thread root is
# exempt so the agent always gets context).
silieco task comment list <task-id> --thread <comment-id> --tail 30 \
    --since <RFC3339-timestamp>

# Add a comment
silieco task comment add <task-id> --content "Looks good, merging now"

# Reply to a specific comment
silieco task comment add <task-id> --parent <comment-id> --content "Thanks!"

# Delete a comment
silieco task comment delete <comment-id>
```

**`--before` / `--before-id` semantics depend on the paging mode**, by
design — same flag, different scope:

| Mode | What the cursor walks | stderr label |
| --- | --- | --- |
| `--recent N` | Older *threads* (last_activity_at, root_id) | `Next thread cursor` |
| `--thread <id> --tail N` | Older *replies* inside that thread (created_at, id) | `Next reply cursor` |

Outside those two modes (`--thread` without `--tail`, or no `--thread`
and no `--recent`) the cursor flags are rejected so they cannot silently
no-op. The server emits the cursor headers (`X-Silieco-Next-Before` /
`X-Silieco-Next-Before-Id`) only when an older page actually exists —
exact-boundary pages (e.g. `--tail 3` on a thread with exactly 3
replies) intentionally return no cursor so callers stop paginating.

When `--since` is combined with `--recent` or `--thread --tail`, the
server additionally suppresses the cursor once the cursor target itself
is older than `since`. Older pages walk strictly older rows, so they
cannot satisfy `> since` either — emitting a cursor there would just
hand back root-only pages until the caller reaches the start of the
thread / task. Incremental polling stops at the first page whose
cursor target falls before the watermark.

### Metadata

Per-task metadata is a small KV map agents use to track pipeline state (PR number, pipeline status, waiting_on, ...). Keys match `^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`, values are primitives (string / number / bool), max 50 keys per task, blob capped at 8KB.

The bar for writing is high: pin a value only when it is materially important to the task AND likely to be re-read by future runs on this same task (the PR URL, the deploy URL, what we're blocked on). Most runs write zero new keys — that's the expected case. Don't pin runtime bookkeeping like `attempts`, single-run investigation notes, large logs, secrets/tokens, or description/comment copies — see the agent runtime prompt for the full anti-pattern list.

```bash
# List every key on a task
silieco task metadata list <task-id>

# Read a single key
silieco task metadata get <task-id> --key pipeline_status

# Write a single key — value auto-typed (true/false → bool, numbers → number, else string)
silieco task metadata set <task-id> --key pipeline_status --value waiting_review
silieco task metadata set <task-id> --key pr_number --value 482
silieco task metadata set <task-id> --key is_blocked --value true

# Force a specific type when sniffing would pick the wrong one
silieco task metadata set <task-id> --key code --value 42 --type string

# Remove a key
silieco task metadata delete <task-id> --key pipeline_status
```

All writes are single-key atomic — concurrent agents writing different keys do not lose each other's updates. To query, use `silieco task list --metadata key=value` (see *List Tasks* above).

### Subscribers

```bash
# List subscribers of a task
silieco task subscriber list <task-id>

# Subscribe yourself to a task
silieco task subscriber add <task-id>

# Subscribe another member or agent by name
silieco task subscriber add <task-id> --user "Lambda"

# Unsubscribe yourself
silieco task subscriber remove <task-id>

# Unsubscribe another member or agent
silieco task subscriber remove <task-id> --user "Lambda"
```

Subscribers receive notifications about task activity (new comments, status changes, etc.). Without `--user`, the command acts on the caller.

### Execution History

```bash
# List all execution runs for a task
silieco task runs <task-id>
silieco task runs <task-id> --full-id
silieco task runs <task-id> --output json

# View messages for a specific execution run
silieco task run-messages <run-id>
silieco task run-messages <short-run-id> --task <task-id>
silieco task run-messages <run-id> --output json

# Incremental fetch (only messages after a given sequence number)
silieco task run-messages <run-id> --since 42 --output json

# Aggregated token usage for a task (sum across all its agent runs)
silieco task usage <task-id>
silieco task usage <task-id> --output json
```

The `usage` command returns the aggregated token usage for a task, summed across all of its agent runs: input tokens, output tokens, cache read/write tokens, and the run count (`run_count`). It wraps `GET /api/tasks/<id>/usage` — the same figures the task detail view shows. Use `--output json` to feed billing/cost tooling.

The `runs` command shows all past and current executions for a task, including running executions. Table output uses short run UUID prefixes by default; pass `--full-id` to print canonical run UUIDs. The `run-messages` command accepts full run UUIDs directly; copied short run prefixes must be scoped with `--task <task-id>` so the CLI only checks that task's runs. It shows the detailed message log (tool calls, thinking, text, errors) for a single run. Use `--since` for efficient polling of in-progress runs.

## Projects

Projects group related Tasks (e.g. a sprint, an epic, a workstream). Every project
belongs to a workspace and can optionally have a lead (member or agent).

### List Projects

```bash
silieco project list
silieco project list --status in_progress
silieco project list --output json
```

Available filters: `--status`.

### Get Project

```bash
silieco project get <id>
silieco project get <id> --output json
```

### Create Project

```bash
silieco project create --title "2026 Week 16 Sprint" --icon "🏃" --lead "Lambda"
```

Flags: `--title` (required), `--description`, `--status`, `--icon`, `--lead`, `--start-date`, `--due-date`. Dates are calendar days (`YYYY-MM-DD`).

### Update Project

```bash
silieco project update <id> --title "New title" --status in_progress
silieco project update <id> --lead "Lambda"
silieco project update <id> --due-date 2026-04-15
```

Flags: `--title`, `--description`, `--status`, `--icon`, `--lead`, `--start-date`, `--due-date`. For the date flags, pass an empty string (e.g. `--start-date ""`) to clear the date.

### Change Status

```bash
silieco project status <id> in_progress
```

Valid statuses: `planned`, `in_progress`, `paused`, `completed`, `cancelled`.

### Delete Project

```bash
silieco project delete <id>
```

### Associating Tasks with Projects

Use the `--project` flag on `task create` / `task update` to attach a Task to a
Project, or on `task list` to filter Tasks by Project:

```bash
silieco task create --title "Login bug" --project <project-id>
silieco task update <task-id> --project <project-id>
silieco task list --project <project-id>
```

## Setup

```bash
# One-command setup for Silieco Cloud: configure, authenticate, and start the daemon
silieco setup

# For local self-hosted deployments
silieco setup self-host

# Custom ports
silieco setup self-host --port 9090 --frontend-port 4000

# On-premise with custom domains
silieco setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

`silieco setup` configures the CLI, opens your browser for authentication, and starts the daemon — all in one step. Use `silieco setup self-host` to connect to a self-hosted server instead of Silieco Cloud.

## Configuration

### View Config

```bash
silieco config show
```

Shows config file path, server URL, app URL, and default workspace.

### Set Values

```bash
silieco config set server_url https://api.example.com
silieco config set app_url https://app.example.com
silieco config set workspace_id <workspace-id>
```

`config set workspace_id <id>` is the low-level interface — it writes the value verbatim without checking that the workspace exists or that you have access. Prefer `silieco workspace switch <id|slug>` for day-to-day workspace changes; it does both checks before saving.

## Autopilot Commands

Autopilots are scheduled/triggered automations that dispatch agent runs (either by creating a task or by running an agent directly).

### List Autopilots

```bash
silieco autopilot list
silieco autopilot list --full-id
silieco autopilot list --status active --output json
```

Autopilot table IDs are short UUID prefixes; follow-up autopilot commands accept copied prefixes when they are unique in the current workspace. Use `--full-id` to print canonical UUIDs.

### Get Autopilot Details

```bash
silieco autopilot get <id>
silieco autopilot get <id> --output json   # includes triggers
```

### Create / Update / Delete

```bash
silieco autopilot create \
  --title "Nightly bug triage" \
  --description "Scan todo tasks and prioritize." \
  --agent "Lambda" \
  --mode create_task \
  --subscriber "Alice"

silieco autopilot update <id> --status paused
silieco autopilot update <id> --description "New prompt"
silieco autopilot update <id> --subscriber "Alice" --subscriber "Bob"
silieco autopilot update <id> --clear-subscribers
silieco autopilot delete <id>
```

`--mode` accepts `create_task` (creates a new task on each run and assigns it to the agent) or `run_only` (enqueues a direct agent run without creating a task). `--agent` accepts either a name or UUID.
`--subscriber` accepts a workspace member name or user ID and may be repeated; on update it replaces the autopilot's subscriber template. Subscribers receive inbox notifications for tasks created by a `create_task` autopilot. Use `--clear-subscribers` to remove all autopilot subscribers.

### Manual Trigger

```bash
silieco autopilot trigger <id>            # Fires the autopilot once, returns the run
```

### Run History

```bash
silieco autopilot runs <id>
silieco autopilot runs <id> --limit 50 --output json
```

### Schedule Triggers

```bash
silieco autopilot trigger-add <autopilot-id> --cron "0 9 * * 1-5" --timezone "America/New_York"
silieco autopilot trigger-update <autopilot-id> <trigger-id> --enabled=false
silieco autopilot trigger-delete <autopilot-id> <trigger-id>
```

Only cron-based `schedule` triggers are currently exposed via the CLI. The data model also defines `webhook` and `api` kinds, but there is no server endpoint that fires them yet, so they're not surfaced here.

## Other Commands

```bash
silieco version              # Show CLI version and commit hash
silieco update               # Update to latest version
silieco agent list           # List agents in the current workspace
```

## Output Formats

Most commands support `--output` with two formats:

- `table` — human-readable table (default for list commands)
- `json` — structured JSON (useful for scripting and automation)

```bash
silieco task list --output json
silieco daemon status --output json
```

## Error Messages

The CLI funnels command errors returned to the top-level handler through a
single user-facing translation layer (`core/internal/cli/errors.go`) so that
what you see on the terminal is a short, actionable sentence rather than a raw
Go error, an HTTP status line, or an internal `resolve task: ...` chain. (A
few commands print their own output or run deliberate fast probes — for example
`setup`'s short `/health` reachability check — and don't go through this
layer.) The underlying detail is still available on demand (see `--debug`).

### What you see

- **Friendly, single-line message.** Transport failures (timeout, DNS,
  connection refused, TLS) and HTTP status failures (401/403/404/409/400·422/
  429/5xx) are each rendered as one clear sentence with a next step — for
  example a timeout suggests checking the network or raising
  `SILIECO_HTTP_TIMEOUT`, and a 401 tells you to run `silieco login`.
- **Server-provided validation messages are preserved.** For a 400/422 that
  carries a message from the server, that message is shown verbatim
  (`Invalid request: <server message>`); only when there is none do you get the
  generic "check your values / run with --help" hint.
- **No leaked internals by default.** Raw URLs, status lines, JSON bodies, and
  the internal verb chain are hidden unless you ask for them.

### Language

Messages default to **English**, matching the rest of the CLI's help output.
If a Chinese locale is detected in `LC_ALL`, `LC_MESSAGES`, or `LANG` (in that
precedence order), messages switch to **Chinese**. No flag is needed; set the
locale as usual:

```bash
LANG=zh_CN.UTF-8 silieco task get SILI-9999   # 错误信息显示为中文
```

### Exit codes

The process exit code is tiered so scripts can branch on the failure class:

| Exit code | Meaning |
| --- | --- |
| `0` | success |
| `1` | generic / unclassified error |
| `2` | network error (timeout, DNS, connection refused, TLS, offline) |
| `3` | authentication / authorization (HTTP 401, 403) |
| `4` | not found (HTTP 404) |
| `5` | validation (HTTP 400, 422) |

```bash
silieco task get SILI-9999
if [ $? -eq 4 ]; then echo "no such task"; fi
```

### Seeing the full detail (`--debug`)

Pass the global `--debug` flag (or set `SILIECO_DEBUG=1`) to print the complete
original error chain — the internal verb chain, the request method/path/status,
and the raw server body — underneath the friendly message. Use it when you need
to file a bug or understand exactly what the server returned:

```bash
silieco task list --debug
SILIECO_DEBUG=1 silieco task update SILI-1234 --title "x"
```

### Request timeout

API requests use a default timeout of 30 seconds. Override it with
`SILIECO_HTTP_TIMEOUT` when you are on a slow network; it accepts a Go duration
(`45s`, `2m`) or a plain number of seconds (`45`). Command-level deadlines are
always at least this value, so raising it takes effect across all commands.

```bash
SILIECO_HTTP_TIMEOUT=60s silieco task list
```
