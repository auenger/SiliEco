# Project directory browsing and mini deployment

## Behavior

Projects retain the existing `local_directory` resource model. Web and desktop can select a runtime machine, validate an absolute directory, and bind it to the project or an attached repository. Existing desktop directory selection remains available.

Project Resources now includes a directory browser. It reads a single level only when expanded, pages directories in batches of 200 entries, and fetches file content only after selection. Queries are workspace/project/resource scoped, cancel when unused, and use short caches. Refresh invalidates active directory and preview queries.

Markdown renders through `ReadonlyContent`. HTML renders through the existing `HtmlBlockPreview` / `HtmlPreviewBody` path, including sandboxing, preview/source switching, fullscreen, and browser/desktop tab actions. HTML and Markdown are not subject to a fixed total-size limit: transport reads 256 KiB chunks and preserves UTF-8 across chunk boundaries. The client assembles the document before rendering; very large documents still consume browser memory. Common raster images and plain text are also previewable. This is a single-file preview, not a hosted site with automatic relative-asset routing.

The file API resolves the project and stored resource inside the selected workspace. Only authenticated workspace members can browse. Binding uses existing manager/own-runtime permissions. The server dispatches reads to a connected runtime belonging to that daemon in the same workspace. Replies are correlated to the exact connection, have a timeout, and are bounded to 16 concurrent server requests. The daemon opens an OS-enforced root; path traversal, symlink escapes and special files are rejected. Symlinks are omitted from directory listings.

Files remain on the execution machine. Binding a directory makes it browsable to workspace members; choose a directory intended for sharing. Offline machines do not fall back to another host.

## Validation

- Full monorepo TypeScript checks passed.
- Next.js production standalone build passed.
- Go filesystem tests cover pagination, traversal/symlink escape, and complete multi-megabyte Markdown/HTML reads.
- WebSocket tests cover connection identity, cancellation and offline machines; race detector passed for the new transport tests.
- Frontend tests cover malformed responses, lazy expansion, selection-triggered preview, shared HTML rendering, and UTF-8 split across chunks.
- On mini, an isolated disposable database passed project-directory scope/offline tests, existing directory lifecycle/validation/conflict tests, and password registration/login without a verification code. The temporary database was removed afterwards.
- Browser inspection confirmed the deployed email/password login and registration UI. No real user account or workspace was created.
