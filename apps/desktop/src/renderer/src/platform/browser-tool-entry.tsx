import { useEffect } from "react";
import { Globe } from "lucide-react";
import { useAuthStore } from "@silieco/core/auth";
import { browserSessionFromPath } from "@silieco/core/page-feedback/protocol";
import { useT } from "@silieco/views/i18n";
import { useTabStore } from "@/stores/tab-store";

export function BrowserToolTabButton() {
  const { t } = useT("page-feedback");
  return <button title={t(($) => $.new_tab)} aria-label={t(($) => $.new_tab)} style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties} className="mb-1 flex size-7 shrink-0 items-center justify-center self-end rounded-md text-muted-foreground hover:bg-muted hover:text-foreground" onClick={() => {
    const state = useTabStore.getState(), slug = state.activeWorkspaceSlug;
    if (!slug) return;
    const id = state.addTab(`/${slug}/browser/${crypto.randomUUID()}`, t(($) => $.title));
    if (id) state.setActiveTab(id);
  }}><Globe className="size-3.5" /></button>;
}

/** Reconciles native guests with all tab groups, including close-other-tabs and
 * workspace removal. It does not change the application's routing or history. */
export function syncBrowserTool() {
  const state = useTabStore.getState();
  const group = state.activeWorkspaceSlug
    ? state.byWorkspace[state.activeWorkspaceSlug]
    : undefined;
  const current = group?.tabs.find((tab) => tab.id === group.activeTabId);
  const sessionIds = Object.values(state.byWorkspace).flatMap((group) =>
    group.tabs.flatMap((tab) => {
      const id = browserSessionFromPath(tab.url);
      return id ? [id] : [];
    }),
  );
  return window.desktopAPI.browser.command({
    type: "sync",
    sessionIds,
    activeSessionId: current ? browserSessionFromPath(current.url) : null,
  }).catch(() => {});
}

export function BrowserToolLifecycle() {
  useEffect(() => {
    void syncBrowserTool();
    const unsubscribeTabs = useTabStore.subscribe(() => { void syncBrowserTool(); });
    const unsubscribeAuth = useAuthStore.subscribe(() => { void syncBrowserTool(); });
    return () => { unsubscribeTabs(); unsubscribeAuth(); };
  }, []);
  return null;
}
