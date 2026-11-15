# Silieco

**Human + Agent Work OS**

Share your Agents and let them represent you inside a working organization.

[![CI](https://github.com/auenger/SiliEco/actions/workflows/ci.yml/badge.svg)](https://github.com/auenger/SiliEco/actions/workflows/ci.yml)

[Website](https://silieco.ai) · [中文](README.zh-CN.md) · [Self-hosting](SELF_HOSTING.md) · [Contributing](CONTRIBUTING.md)

> Silieco is currently a development preview. Do not use it with irreplaceable
> production data. The repository does not yet declare a license; all rights are
> reserved until a license is added.

## What Silieco is

Silieco is a collaborative operating system for work performed by people and AI
Agents together. A team can describe an objective, turn it into Tasks, assign
those Tasks to a person, an Agent, or a Swarm, and keep execution, discussion,
decisions, reviews, and results in one shared record.

The product supports both lightweight work and governed processes:

- create a Task directly in a Space and use the normal linear lifecycle; or
- place the Task in a Project, start a published SOP, and manage it through a
  versioned Workflow Run and ordered Stages.

The product hierarchy is:

```text
Space
├── direct Tasks
├── people, Agents, Swarms, Skills, chats and settings
└── Project
    ├── Project Tasks
    ├── reusable Space SOP assets
    └── SOP run or Project-specific SOP variant
        └── Workflow Run
            └── Stage
                └── Task
```

An SOP is the reusable definition. A Workflow Run is one named execution of a
published SOP. Tasks keep their own lifecycle status while also belonging to a
Stage, so the board can switch between status view and Stage view without losing
either dimension.

## What is implemented

### Space and Project organization

- Multiple isolated Spaces with members, roles, repositories, Agents, Skills,
  Tasks, chats, and settings.
- Accordion navigation for Spaces and Projects in the Desktop App.
- Multiple Projects per Space, each with its own Tasks, SOP variants,
  automation, Agents, people, and context. Reusable SOP definitions live at
  Space level and can run directly inside any Project.
- Project and Space Tasks can coexist; an SOP is optional.

### Task as the core work object

- Tasks can be created for a person, an Agent, or a Swarm.
- Status lifecycle: backlog, todo, in progress, in review, done, blocked, and
  cancelled.
- List, personal, Project, status-board, and Workflow Stage-board views.
- Priority, assignee, creator, Project, labels, dates, parent/child Tasks,
  dependencies, acceptance criteria, attachments, comments, reactions,
  subscribers, activity timeline, and runtime usage.
- Real-time state changes from queued Agent work through execution, review, and
  completion.

> **Task** is the business work item and **AgentRun** is one agent execution.
> The code, database, API, and CLI use these names directly. A Task can have
> multiple runs; chat and automation runs may have no associated Task.

### Shared file resources

- Files uploaded to a Task are durable Space resources; temporary files
  uploaded only inside a conversation are intentionally excluded.
- The Space resource page groups files by their source Project and shows every
  Project or Task that currently references each file.
- Each Project has an independent file view combining files from its Tasks with
  files referenced directly by the Project.
- A Task can reference an existing Space file without creating a duplicate.
  The comment composer provides a searchable, multi-select reference picker and
  inserts the selected files as file cards in the draft.
- Image, PDF, audio, video, Markdown, HTML, and text resources can be opened in
  a dedicated preview tab; other files remain directly downloadable.
- Reference creation and removal follow Space membership and Project management
  permissions, while referenced source files are protected from accidental
  deletion.

### SOP, Workflow, and Stage

- Space-scoped reusable SOP definitions with draft, published, and archived
  states. A Project can run the shared definition directly or inherit it into
  an independently adjustable Project variant.
- Immutable published versions and separately named Workflow Runs.
- Active or waiting Workflow Runs can be safely adjusted without mutating the
  published SOP version: managers may edit run metadata, artifacts, Skills,
  gates, and unused future Stages. Completed Stages stay locked, revisions
  prevent concurrent overwrites, and each change keeps before/after snapshots.
- Ordered Stages with input/output descriptions, required Skills, allowed Task
  statuses, rollback targets, and human/Agent/hybrid gates.
- Preset SOP templates for bid delivery, software development, bug fixing,
  document collaboration, and a blank workflow.
- Task creation can select Project → SOP → Workflow Run → Stage. Selecting a
  published SOP can create a run when none exists.
- Existing Project Tasks can be attached to a Workflow and moved across Stages.
- Stage boards show the same Task metadata and lifecycle status as the normal
  Task board, with status filtering, todo-first ordering, and independent
  vertical scrolling per Stage column.
- The current Stage exposes its review state and lets authorized reviewers
  approve progression, complete the run, or reject back to its rollback Stage.

### Project working directories and Git repositories

- A Project can map a private local working directory for each Runtime without
  requiring a Git repository.
- When a remote Git repository is attached, each Runtime can optionally map its
  own prepared local clone beneath that repository. Repository-specific mappings
  take precedence over the Project's standalone working directory.
- Desktop validates repository mappings against the selected Git root and its
  `origin`; standalone directories only need to be readable and writable.
- Local paths and files are never synchronized to other Space members. Other
  people see machine ownership and availability, while Agents receive only the
  path belonging to the Runtime executing their Task.

### Human–Agent collaboration

- Direct persistent chats with an Agent and resumable execution context.
- Space group chats containing current Space members and Agents.
- Human messages remain ordinary conversation; an Agent is invoked only when it
  is explicitly `@mentioned`.
- Mentioned Agents receive speaker-labelled group history as shared context, and
  multiple mentioned Agents can be queued from the same message.
- Agent profiles, visibility, runtime/provider selection, model, thinking level,
  environment variables, arguments, MCP configuration, concurrency, and Skills.
- Swarms provide a stable routing layer for groups of people and Agents.
- Autopilots can schedule or trigger recurring Agent work.

### Runtime and integrations

- Local daemon discovery, runtime registration, WebSocket heartbeats, run
  claiming, streaming, cancellation, recovery, and garbage collection.
- Runtime adapters for Claude Code, Codex, CodeBuddy, GitHub Copilot CLI,
  OpenCode, OpenClaw, Hermes, Pi, Cursor Agent, Kimi, Kiro, Qwen, Qoder, Trae,
  and other configured providers.
- GitHub/VCS, Slack, Lark, object storage, email, metrics, and self-hosting
  integration points.
- Web, Electron Desktop, Expo Mobile, and documentation apps share the same
  TypeScript SDK, UI system, and business views where applicable.

## System boundaries

```text
┌──────────────────────────────────────────────────────────────┐
│ App                                                          │
│ Next.js Web · Electron Desktop · Expo Mobile · shared views  │
└──────────────────────────────┬───────────────────────────────┘
                               │ REST + WebSocket
┌──────────────────────────────▼───────────────────────────────┐
│ Core Service                                                 │
│ identity · Spaces · Projects · Workflows · Tasks · chat      │
│ scheduling · realtime coordination · PostgreSQL              │
└──────────────────────────────┬───────────────────────────────┘
                               │ daemon protocol
┌──────────────────────────────▼───────────────────────────────┐
│ Daemon / CLI                                                 │
│ local runtime discovery · Agent process execution · recovery │
└──────────────────────────────────────────────────────────────┘
```

| Boundary | Current implementation | Direction |
| --- | --- | --- |
| Core Service | Go, Chi, sqlc, pgx, WebSocket | Evaluate gradual Rust/Axum migration |
| Daemon / CLI | Go, Cobra, local process supervision | First Rust migration candidate |
| App | Next.js, React, Electron, Expo | Keep TypeScript and `@silieco/*` packages |
| Data | PostgreSQL 17 + pgvector, Redis, S3-compatible storage | Preserve schema and protocol compatibility |

The current Go implementation is the executable reference. See the
[Rust migration analysis](docs/architecture/RUST-MIGRATION.md) before replacing
either the Core Service or daemon.

## Repository layout

```text
silieco/
├── core/                 # Go Core Service, CLI, current daemon, migrations
├── daemon/               # Independent daemon boundary and future Rust home
├── apps/
│   ├── web/              # Next.js web app and public website
│   ├── desktop/          # Electron desktop app
│   ├── mobile/           # Expo mobile app
│   └── docs/             # Documentation site
├── packages/
│   ├── core/             # Shared TypeScript SDK and state
│   ├── ui/               # Design system
│   └── views/            # Shared product views
├── deploy/               # Docker and Helm deployment resources
├── docs/                 # Product and architecture documents
└── e2e/                  # Playwright end-to-end tests
```

## Local development

Requirements:

- Node.js 22+
- pnpm 10.28.2+
- Go 1.26+
- Docker with the Compose plugin
- optionally, at least one authenticated Agent CLI

Bootstrap and start Core + Web:

```bash
cp .env.example .env
pnpm install
make dev
```

Default endpoints:

- Web: <http://localhost:3000>
- Core API: <http://localhost:8080>
- Health check: <http://localhost:8080/health>

Start the Desktop App in another terminal:

```bash
pnpm dev:desktop
```

Useful commands:

```bash
make setup             # dependencies, PostgreSQL, and migrations
make start             # Core + Web, running migrations first
make server            # Core Service only
make daemon            # restart the authenticated local daemon
make silieco ARGS=...  # run the CLI from Go source
pnpm dev:desktop       # Electron Desktop development app
pnpm dev:mobile        # Expo Mobile development app
pnpm typecheck         # TypeScript type checking
pnpm test              # TypeScript tests
make test              # Go tests
make check             # full local verification pipeline
```

For deployment and operator details, read
[Self-hosting](SELF_HOSTING.md),
[CLI and daemon](CLI_AND_DAEMON.md), and
[Contributing](CONTRIBUTING.md).

## Current naming contract

- npm scope: `@silieco/*`
- Go module: `github.com/silieco-ai/silieco/core`
- CLI: `silieco`
- environment variables: `SILIECO_*`
- deep links: `silieco://`
- local state: `~/.silieco`
- protocol headers: `X-Silieco-*`

Silieco does not provide a compatibility layer for the pre-migration product
brand.

## License

No license has been selected yet. Until a license file is added, all rights are
reserved.
