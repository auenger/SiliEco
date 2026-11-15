/**
 * Per-task "last time the user looked at the timeline" timestamp. Drives
 * the "New since last view" divider in `timeline-list.tsx`. In-memory only
 * (matches comment-drafts-store trade-off — no native AsyncStorage
 * available, expo-secure-store's 2KB cap is overkill anyway for a single
 * ISO string per task but the cold-start loss is acceptable).
 *
 * Lifecycle:
 *   - First read for an unseen task → undefined → divider draws at the
 *     very first new entry after the user lands.
 *   - After the user scrolls past the divider, `markViewed(taskId)` is
 *     called with `now`, so next mount draws the divider forward (or no
 *     divider if there's nothing new).
 *   - If the user opens then leaves without scrolling past the divider,
 *     `markViewed` is NOT called — next mount preserves the same divider
 *     position so the user doesn't lose their "where I left off" marker.
 */
import { create } from "zustand";

interface LastViewedState {
  /** taskId → ISO timestamp of the last "the user scrolled past everything" event. */
  lastViewed: Record<string, string>;
  getLastViewed: (taskId: string) => string | undefined;
  markViewed: (taskId: string, when?: string) => void;
}

export const useLastViewedStore = create<LastViewedState>((set, get) => ({
  lastViewed: {},
  getLastViewed: (taskId) => get().lastViewed[taskId],
  markViewed: (taskId, when) => {
    const ts = when ?? new Date().toISOString();
    set({ lastViewed: { ...get().lastViewed, [taskId]: ts } });
  },
}));
