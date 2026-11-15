# Projects and resources source map

- `core/cmd/silieco/cmd_project.go` registers project `list`, `get`, `create`, `update`, `delete`, and `status`.
- The same file registers `project resource list/add/update/remove`.
- `project create --repo` attaches `github_repo` resources during project creation.
- `project create` / `project update` accept `--start-date` / `--due-date` (calendar days, `YYYY-MM-DD`), mapping to the project `start_date` / `due_date` columns (migration `166_project_dates`); an empty `--start-date ""`/`--due-date ""` on update clears the date, mirroring the task date flags in `cmd_task.go`.
- `project resource add` supports shortcuts for `github_repo` (`--url`, non-JSON `--ref` for checkout ref, `--default-branch-hint`, `--primary`) and `local_directory` (`--local-path`, `--daemon-id`, optional `--repository-resource-id`, `--ref-label`), or generic JSON `--ref '<json>'`.
- `project resource update` merges shortcut edits with existing `resource_ref` so a partial edit does not clobber required fields; non-JSON `--ref` updates `github_repo.resource_ref.ref`.
- `core/cmd/server/router.go` exposes `/api/projects` plus `/api/projects/{projectId}/resources` routes.
- `core/pkg/db/queries/project_resource.sql` is the CRUD query surface for `project_resource` rows.
- Project resources are written into `.silieco/project/resources.json` for agent workdirs.
- `core/internal/daemon/local_directory.go` resolves the current daemon's mapping beneath the primary repository and validates the selected directory is the matching Git root before launching the agent. Foreign-daemon local paths are filtered from generated task context.
- `github_repo.resource_ref.ref` is lifted into daemon `RepoData.Ref` by `core/internal/handler/daemon.go`; `core/internal/daemon/daemon.go` stores it per task, and `core/internal/daemon/health.go` uses it as the default `/repo/checkout` ref when the checkout request does not explicitly pass one.
- A project's `description` is injected as durable context for every task in the project. The claim handler (`core/internal/handler/daemon.go`) reads `proj.Description` onto the claim response (`ProjectDescription`, `core/internal/handler/agent.go`); the daemon carries it through `Task` (`core/internal/daemon/types.go`) and `RunContextForEnv` (`core/internal/daemon/execenv/execenv.go`) into the brief's `## Project Context` section (`core/internal/daemon/execenv/runtime_config.go`) and into `.silieco/project/resources.json` as `project_description` (`core/internal/daemon/execenv/context.go`).

- `core/internal/handler/project_directory.go` exposes project-scoped directory validation and reads; `core/internal/daemonws/project_files.go` binds replies to the authenticated daemon connection.
- `core/internal/daemon/project_files.go` enforces the filesystem root, directory pages, and bounded preview chunks.
- `packages/views/projects/components/project-directory-browser.tsx` and `project-directory-binding.tsx` implement shared Web/Desktop browsing and binding. HTML reuses `HtmlBlockPreview`; Markdown reuses `ReadonlyContent`.

- `packages/views/projects/components/project-files-page.tsx` is the shared full-page file browser; `apps/web/app/[workspaceSlug]/(dashboard)/projects/[id]/files/page.tsx` and the desktop `ProjectFilesRoute` wire the platforms.
- `packages/views/common/file-type-icon.tsx` maps file extensions to statically imported Lucide icons.
- `packages/core/paths/tab-subject.ts` gives project files a distinct tab identity.
