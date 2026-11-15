# Skill-importing source map

Evidence layer for `silieco-skill-importing`. Every behavioral claim in `SKILL.md`
maps to a real code path below with `file:line`. Paths are relative to the repo
root (`silieco/`).

Re-derive before trusting: line numbers drift. To re-verify a single anchor,
`grep` the symbol and read its surroundings, e.g.:

```bash
grep -n "func (h \*Handler) ImportSkill" core/internal/handler/skill.go
grep -n "func runSkillImport"           core/cmd/silieco/cmd_skill.go
grep -n "func IsReservedContentPath"    core/internal/skill/reserved.go
```

## Import endpoint and route

| Behavior | File:line |
|---|---|
| `ImportSkill` handler (`POST /api/skills/import`) | `core/internal/handler/skill.go:1882` |
| Decodes `ImportSkillRequest` (`{ "url": ..., "on_conflict": ... }`) | `core/internal/handler/skill.go:1895-1899`, struct at `:553` |
| Validates `on_conflict` (`fail`, `overwrite`, `rename`, `skip`) | `core/internal/handler/skill.go:1900-1908`, helper `validImportOnConflict` at `:566` |
| Detects source family + normalizes URL | `core/internal/handler/skill.go:1910` (calls `detectImportSource`) |
| Persists provenance into `config.origin` | `core/internal/handler/skill.go:1944-1948` — set only when `imported.origin != nil`; otherwise `config` stays `{}` and `origin` is absent |
| Structured conflict dispatcher | `core/internal/handler/skill.go:1813-1878` |
| Builds skill + files via `createSkillWithFiles` (def `core/internal/handler/skill_create.go:77`, tx body `:29`) | wrapped by `createImportedSkillWithName` at `core/internal/handler/skill.go:1774` |
| Structured success: `201 Created` with `{status:"created", skill}` when `on_conflict` was sent | `core/internal/handler/skill.go:1985-1988` |
| Legacy success: `201 Created` with bare `SkillWithFilesResponse` when `on_conflict` was omitted | `core/internal/handler/skill.go:1990` |
| Route registration `r.Post("/import", h.ImportSkill)` | `core/cmd/server/router.go:874` |

Note: `ImportSkill` now branches on content type. A multipart body routes to the
archive path (below); a JSON body keeps the URL flow. Both converge on the shared
`finishSkillImport` tail. Line numbers in this table predate that split — re-grep
`func (h *Handler) ImportSkill` / `finishSkillImport` to re-derive.

## Local archive import (`.skill` / `.zip`)

| Behavior | File:line |
|---|---|
| `ImportSkill` branches to the archive path on multipart bodies | `core/internal/handler/skill.go:1924` (`if isMultipartForm(r)`) |
| Shared create + conflict tail `finishSkillImport` (URL and archive) | `core/internal/handler/skill.go:1974` |
| `isMultipartForm` content-type check | `core/internal/handler/skill_import_archive.go:26` |
| `importSkillFromArchive` (multipart parse + `MaxBytesReader` + `on_conflict` + `file`) | `core/internal/handler/skill_import_archive.go:36` |
| Upload cap `maxImportArchiveUploadSize` (16 MiB compressed) | `core/internal/handler/skill_import_archive.go:22` |
| `parseSkillArchive` (zip decode, shallowest-`SKILL.md` root, frontmatter name, zip-slip + reserved + ignore filters) | `core/internal/handler/skill_import_archive.go:95` |
| Reuses per-file / per-bundle / count caps via `importedSkill.addFile` | `core/internal/handler/skill.go:618` (`maxImportFileSize`/`maxImportTotalSize`/`maxImportFileCount` at `:579-583`) |
| Name fallback (wrapper dir, then filename) | `core/internal/handler/skill_import_archive.go:201` |
| Ignore filter (dotfiles, `__MACOSX`, license) | `core/internal/handler/skill_import_archive.go:218` |
| Per-entry size-capped read | `core/internal/handler/skill_import_archive.go:234` |
| Tests (parser units + handler multipart create/skip/reject) | `core/internal/handler/skill_import_archive_test.go` |

## CLI: `silieco skill import --url` / `--file`

| Behavior | File:line |
|---|---|
| `skill import` command def | `core/cmd/silieco/cmd_skill.go:59-63` |
| `--url` flag | `core/cmd/silieco/cmd_skill.go:143` |
| `--file` flag (local `.skill` / `.zip`; mutually exclusive with `--url`) | `core/cmd/silieco/cmd_skill.go:144` |
| `--on-conflict` flag (default `fail`) | `core/cmd/silieco/cmd_skill.go:145` |
| `--output` flag (default `json`) | `core/cmd/silieco/cmd_skill.go:146` |
| `runSkillImport` | `core/cmd/silieco/cmd_skill.go:412` |
| Requires exactly one of `--url` / `--file` | `core/cmd/silieco/cmd_skill.go:420-427` |
| `--file` reads the archive and posts multipart via `ImportSkillFile` | `core/cmd/silieco/cmd_skill.go:436-447`, client method `core/internal/cli/client.go:535` |
| `POST /api/skills/import` (URL, JSON body) | `core/cmd/silieco/cmd_skill.go:455` |
| Structured HTTP error body handling | `core/cmd/silieco/cmd_skill.go:437-440`, `handleSkillImportError` at `:454` |
| Prints structured result (`json` or table) | `core/cmd/silieco/cmd_skill.go:443`, helper at `:497` |

## Same-name conflict handling

| Behavior | File:line |
|---|---|
| `SkillImportResult` (`status`, `reason`, `skill`, `existing_skill`) | `core/internal/handler/skill.go:104-109` |
| `ExistingSkillIdentity` (`id`, `name`, `created_by`, `can_overwrite`) | `core/internal/handler/skill.go:112-117` |
| Pre-create lookup for structured conflict flow | `core/internal/handler/skill.go:1951-1962` |
| Race-safe unique-violation fallback into structured conflict flow | `core/internal/handler/skill.go:1966-1971` |
| Default `fail`: `status:"conflict"` and HTTP 409 | `core/internal/handler/skill.go:1872-1877` |
| `overwrite`: creator-only update, preserves skill identity/bindings via `overwriteSkillWithFiles` | `core/internal/handler/skill.go:1823-1852`, tx helper at `core/internal/handler/skill_create.go:133` |
| `rename`: creates suffixed name with bounded attempts | `core/internal/handler/skill.go:1854-1870`, helper at `:1786` |
| `skip`: returns `status:"skipped"` and leaves existing skill untouched | `core/internal/handler/skill.go:1816-1821` |
| Legacy duplicate branch when `on_conflict` was omitted | `core/internal/handler/skill.go:1973-1978` |
| Legacy duplicate response `{error, existing_skill}` | `core/internal/handler/skill.go:118-123` |
| CLI normalizes legacy `{existing_skill}` body into `status:"conflict"` | `core/cmd/silieco/cmd_skill.go:454-482`, helper at `:484` |

## Response shape: `SkillWithFilesResponse`

| Behavior | File:line |
|---|---|
| `SkillWithFilesResponse` = embedded `SkillResponse` + `Files []SkillFileResponse` | `core/internal/handler/skill.go:99-102` |
| `SkillResponse` fields (`id, workspace_id, name, description, content, config, created_by, created_at, updated_at`) | `core/internal/handler/skill.go:41-51` |
| `SkillFileResponse` fields | `core/internal/handler/skill.go:80-87` |
| `createSkillWithFilesInTx` returns `SkillWithFilesResponse{SkillResponse, Files}` | `core/internal/handler/skill_create.go:66-69` |
| `config.origin` set on import | `core/internal/handler/skill.go:1947` |

For current CLI imports, `SkillWithFilesResponse` appears under
`SkillImportResult.skill` when status is `created` or `updated`. Legacy clients
that omit `on_conflict` still receive a bare `SkillWithFilesResponse`.

## URL source families (`detectImportSource`)

| Behavior | File:line |
|---|---|
| `detectImportSource` | `core/internal/handler/skill.go:773-804` |
| `skills.sh` / `www.skills.sh` | `core/internal/handler/skill.go:791-792` |
| `clawhub.ai` / `www.clawhub.ai` | `core/internal/handler/skill.go:793-794` |
| `github.com` / `www.github.com` | `core/internal/handler/skill.go:795-796` |
| Bare slug (no host) defaults to ClawHub | `core/internal/handler/skill.go:798-800` |
| `parseGitHubURL` handles `/tree/{ref}/...` and `/blob/{ref}/.../SKILL.md` | `core/internal/handler/skill.go:1450-1503` (tree/blob check `:1463-1480`) |

## Additive add vs replace-all set

| Behavior | File:line |
|---|---|
| `AddAgentSkills` (additive: AddAgentSkill loop, no RemoveAll) | `core/internal/handler/skill.go:2161`; loop `:2192-2200` |
| Route `POST /api/agents/{id}/skills/add` | `core/cmd/server/router.go:851` |
| `SetAgentSkills` (replace-all: RemoveAllAgentSkills then re-add) | `core/internal/handler/skill.go:2106`; `RemoveAllAgentSkills` `:2138`; re-add `:2143-2151` |
| Route `PUT /api/agents/{id}/skills` | `core/cmd/server/router.go:850` |
| CLI `agent skills add` def ("without replacing existing assignments") | `core/cmd/silieco/cmd_agent.go:125-130` |
| `runAgentSkillsAdd` → `POST .../skills/add` | `core/cmd/silieco/cmd_agent.go:797`; POST `:818` |
| CLI `agent skills set` def ("replaces all current assignments") | `core/cmd/silieco/cmd_agent.go:118-123` |
| `runAgentSkillsSet` → `PUT .../skills` | `core/cmd/silieco/cmd_agent.go:772`; PUT `:790` |
| CLI `agent skills list` | `core/cmd/silieco/cmd_agent.go:740`; GET `:750` |

## Reserved primary-content filename (`SKILL.md`)

| Behavior | File:line |
|---|---|
| `ContentFilename = "SKILL.md"` | `core/internal/skill/reserved.go:12` |
| `IsReservedContentPath` (cleans path, case-insensitive compare) | `core/internal/skill/reserved.go:25-27` |
| Import/create path: reserved supporting file is **silently skipped** (`continue`) | `core/internal/handler/skill_create.go:50-54` |
| `UpdateSkill` (PUT `/api/skills/{id}`) replace-files path: also silently skips | `core/internal/handler/skill.go:490-494` |
| `UpsertSkillFile` (PUT `/api/skills/{id}/files`): **rejects 400** "SKILL.md is reserved for the primary skill content" | `core/internal/handler/skill.go:2014`; reserved check `:2034-2036` |

Reason `SKILL.md` is reserved: the daemon writes the skill's `Content` to that path
itself when preparing the execution environment, so a supporting file may not also
claim it (`core/internal/skill/reserved.go:8-24`).

Behavior is path-shape-dependent. On **import or create** a manifest's `SKILL.md`
supporting file is dropped (it will not appear in the returned `files`), so the
import still succeeds — it does not 400. The hard 400 rejection fires only on the
dedicated single-file endpoint `PUT /api/skills/{id}/files`.
