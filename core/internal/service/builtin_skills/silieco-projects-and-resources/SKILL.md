---
name: silieco-projects-and-resources
description: "Use when creating, inspecting, updating, or debugging Silieco projects and project resources. Covers durable project context, github_repo and local_directory resources, how resources affect future agent task context, when to bind repos, and when not to mutate resources."
user-invocable: false
allowed-tools: Bash(silieco *)
---

# Silieco Projects and Resources

## Quick start

Projects are durable context containers. Resources attached to a project can affect future agent tasks.

```bash
silieco project list --output json
silieco project get <project-id> --output json
silieco project resource list <project-id> --output json
```

Project resources are mutated through project resource commands/endpoints. Task
comments do not create durable project resources.

## Core model

A project groups work and carries durable resources. A resource is not just display metadata; it is context later injected into task briefs and `.silieco/project/resources.json`.

A project's `description` is also durable context: when a task (or a quick-create task) is bound to a project, the project description is injected into the agent's brief under `## Project Context` and written to `.silieco/project/resources.json` as `project_description`. Use it for project-wide rules/context that should apply to every task in the project.

Common resource types:

- `github_repo` — durable remote Git context, with `resource_ref.url`, optional checkout `ref`, optional prompt-only `default_branch_hint`, and `primary` when it is the project's default execution repository;
- `local_directory` — a private daemon-local project working directory, with `resource_ref.local_path`, `daemon_id`, and optional label. When it represents a clone of an attached repository, `repository_resource_id` maps it beneath that `github_repo`; projects without Git keep it standalone. Files remain on the machine. Workspace members can browse bound project directories in the project Resources tab while the daemon is online. Only bind directories intended for workspace sharing.

## CLI

```bash
silieco project list --output json
silieco project get <project-id> --output json
silieco project create --title "<title>" --repo <github-url> --output json
silieco project create --title "<title>" --start-date 2026-03-01 --due-date 2026-03-31 --output json
silieco project update <project-id> --title "<title>" --output json
silieco project update <project-id> --due-date 2026-04-15 --output json
silieco project update <project-id> --start-date "" --output json   # clear the start date
silieco project status <project-id> in_progress --output json
silieco project resource list <project-id> --output json
silieco project resource add <project-id> --type github_repo --url <github-url> --output json
silieco project resource add <project-id> --type github_repo --url <github-url> --primary --output json
silieco project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json
silieco project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --repository-resource-id <github-resource-id> --output json
silieco project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --output json
silieco project resource update <project-id> <resource-id> --url <new-github-url> --output json
silieco project resource update <project-id> <resource-id> --ref <branch-or-sha> --output json
silieco project resource remove <project-id> <resource-id> --output json
```

For `github_repo`, non-JSON `--ref` sets `resource_ref.ref`, the default checkout branch/tag/SHA for future tasks in that project. JSON `--ref '<json>'` remains the escape hatch for full payloads or resource types not covered by shortcuts.

`--start-date` / `--due-date` are optional calendar days (`YYYY-MM-DD`, like task dates). On `project update`, pass an empty string (`--start-date ""`) to clear a date; an unset flag leaves it untouched.

## When to add a resource

Add/update a project resource when the user asks for durable project context: "把这个 GitHub repo 绑到项目上", "以后都用这个 repo", "agent 总是拿不到这个项目的仓库", or "这个项目要在我的本地目录里跑".

Project resources are durable and affect future tasks. `silieco repo checkout`
is task-local checkout state.

## Debugging wrong context

1. `silieco project get <project-id> --output json`.
2. `silieco project resource list <project-id> --output json`.
3. Check `github_repo.resource_ref.url`, optional `ref`, `default_branch_hint`, and `primary`; then verify the current daemon's `local_directory.repository_resource_id` points to that repository.
4. Updating resources is a durable mutation. After an update, listing the
   resource is the verification path.
5. If resources match the expected task context, inspect runtime/repo checkout
   path next.

## Side effects

Project create/update/delete/status and project resource add/update/remove mutate durable workspace state and affect future tasks. Ask before changing `local_directory` unless the user explicitly requested that exact local path.

More source-backed details: `references/projects-and-resources-source-map.md`.

## Project directory browser

Web and desktop can bind a machine directory from Project Resources. The server validates access via the selected daemon. Directory expansion reads one page (200 entries), never recursively walks the tree. File previews load only on selection, transferring 256 KiB chunks. Markdown uses the shared readonly renderer; HTML uses the existing sandboxed preview with source/fullscreen/new-tab actions. HTML and Markdown have no fixed total preview size limit. Directory traversal and escaping symlinks are rejected by an OS-enforced root. Offline machines show an explicit error; files are not replicated to the server.

A single bound directory is displayed as a path, not a selector. Multiple mappings use the shared select control. The browser offers an independent `/{workspace}/projects/{id}/files` page via Open in new tab (desktop app tab or web browser tab), with resizable tree/preview panels and file-type icons. The standalone page uses the same lazy directory and preview queries.
