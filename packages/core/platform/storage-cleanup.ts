import type { StorageAdapter } from "../types/storage";
import { clearRegisteredWorkspaceDrafts } from "../drafts/cleanup-registry";
// Ensure every module-level draft store has registered its key before cleanup
// runs, so the registry is never partially populated at logout/delete time.
import "../drafts/register-all-drafts";

/**
 * Non-draft workspace-scoped keys (stored as `${key}:${slug}`).
 *
 * Draft stores no longer live here: they self-register via
 * `registerDraftCleanup` (see `drafts/cleanup-registry`) and are cleared by
 * `clearRegisteredWorkspaceDrafts` below. This list is only for the remaining
 * view/navigation keys that are not drafts.
 *
 * IMPORTANT: When adding a new non-draft workspace-scoped persist store, add
 * its key here; for draft stores, prefer `createDraftStore` (auto-registers)
 * or call `registerDraftCleanup` directly.
 */
const WORKSPACE_SCOPED_KEYS = [
  "silieco_tr1_task_surface_views",
  "silieco_tr1_tasks_view",
  "silieco_tr1_tasks_scope",
  "silieco_tr1_my_tasks_view",
  "silieco:tr1:chat:selectedAgentId",
  "silieco:tr1:chat:selectedProjectId",
  "silieco:tr1:chat:activeSessionId",
  "silieco:tr1:chat:expanded",
  "silieco_tr1_navigation",
];

/** Remove all workspace-scoped storage entries for the given workspace slug. */
export function clearWorkspaceStorage(
  adapter: StorageAdapter,
  slug: string,
) {
  for (const key of WORKSPACE_SCOPED_KEYS) {
    adapter.removeItem(`${key}:${slug}`);
  }
  // Draft stores self-register their keys; clear them from the registry so a
  // new draft store can never be silently skipped by an out-of-date list.
  clearRegisteredWorkspaceDrafts(adapter, slug);
}
