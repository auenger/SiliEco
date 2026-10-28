import { syncBrowserTool } from "./browser-tool-entry";
import { browserBoundsBesideChat } from "./browser-tool-bounds";
import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { useWorkspaceId } from "@silieco/core";
import { useChatStore } from "@silieco/core/chat";
import { useAuthStore } from "@silieco/core/auth";
import { useNavigation } from "@silieco/views/navigation";
import { useWorkspacePaths } from "@silieco/core/paths";
import { PageFeedbackPage } from "@silieco/views/page-feedback";
import { usePageFeedbackStore, feedbackDraftKey, browserStateSchema, browserSessionFromPath, type BrowserContext, type BrowserState } from "@silieco/core/page-feedback";
import { useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

const EMPTY_STATE: BrowserState = { url: "", title: "", loading: false, canGoBack: false, canGoForward: false };
export function BrowserToolRoute() {
  const { sessionId } = useParams<{ sessionId: string }>();
  const userId = useAuthStore((s) => s.user?.id);
  const workspaceId = useWorkspaceId();
  const context = useMemo(() => ({ userId: userId ?? "", workspaceId, sessionId: sessionId ?? "" }), [userId, workspaceId, sessionId]);
  return userId && sessionId ? <BrowserToolSession key={feedbackDraftKey(context)} context={context} /> : null;
}

function BrowserToolSession({ context }: { context: BrowserContext }) {
  const [state, setState] = useState<BrowserState>(EMPTY_STATE);
  const [ready, setReady] = useState(false);
  const [covered, setCovered] = useState(false);
  const surface = useRef<HTMLDivElement>(null);
  const navigation = useNavigation(), paths = useWorkspacePaths();
  const bridge = window.desktopAPI.browser;
  const chatOpen = useChatStore((s) => s.isOpen && s.floatingChatEnabled);
  const overlay = useWindowOverlayStore((s) => s.overlay);
  useEffect(() => {
    if (!state.url.startsWith("http")) return;
    const tabs = useTabStore.getState();
    const tab = Object.values(tabs.byWorkspace).flatMap((group) => group.tabs).find((item) => browserSessionFromPath(item.url) === context.sessionId);
    const title = (state.title || new URL(state.url).hostname).slice(0, 200);
    if (tab && tab.title !== title) tabs.updateTab(tab.id, { title });
  }, [context.sessionId, state.title, state.url]);
  useEffect(() => {
    let disposed = false;
    const unsubscribe = bridge.subscribe((event) => { if (!disposed && event.sessionId === context.sessionId) { const parsed = browserStateSchema.safeParse(event.state); if (parsed.success) setState(parsed.data); } });
    const url = usePageFeedbackStore.getState().drafts[feedbackDraftKey(context)]?.url ?? "";
    window.desktopAPI.reportAuthSession(context.userId);
    void syncBrowserTool().then(() => disposed ? null : bridge.command({ type: "open", context, url })).then((value) => { if (!disposed) { setState(browserStateSchema.parse(value)); setReady(true); } }).catch((e: unknown) => { if (!disposed) setState({ ...EMPTY_STATE, error: e instanceof Error ? e.message : String(e) }); });
    return () => { disposed = true; unsubscribe(); void bridge.command({ type: "bounds", context, bounds: null }).catch(() => {}); };
  }, [bridge, context]);
  useEffect(() => {
    if (!ready) return;
    let frame = 0, last = "";
    const update = () => {
      const element = surface.current;
      // Native guests sit above DOM portals. Hide the guest while a host
      // overlay is visible. Polling also follows animated sidebar geometry.
      const overlayVisible = [...document.querySelectorAll<HTMLElement>('[role="dialog"], [role="alertdialog"], [role="menu"], [data-slot="popover-content"], [data-slot="select-content"], [data-browser-occlusion]')].some((el) => el.getClientRects().length > 0 && getComputedStyle(el).visibility !== "hidden");
      const b = element?.getBoundingClientRect();
      const chat = document.querySelector<HTMLElement>(chatOpen ? "[data-chat-window]" : "[data-chat-launcher]");
      const chatRect = chat?.getClientRects().length ? chat.getBoundingClientRect() : null;
      const bounds = !covered && !overlay && !overlayVisible && state.url.startsWith("http") && b && b.width > 0 && b.height > 0
        ? browserBoundsBesideChat({ x: b.x, y: b.y, width: b.width, height: b.height }, chatRect)
        : null;
      const next = JSON.stringify(bounds);
      if (next !== last) { last = next; void bridge.command({ type: "bounds", context, bounds }).catch(() => {}); }
      frame = requestAnimationFrame(update);
    };
    update();
    return () => { cancelAnimationFrame(frame); };
  }, [bridge, context, ready, covered, overlay, chatOpen, state.url]);
  return <PageFeedbackPage context={context} bridge={bridge} state={state} surface={<div ref={surface} className="h-full w-full" />} onCovered={setCovered} onTaskCreated={(id) => navigation.push(paths.taskDetail(id))} onChatPrepared={({ sessionId, agentId }) => {
    const chat = useChatStore.getState();
    chat.setActiveSession(sessionId);
    chat.setSelectedAgentId(agentId);
    chat.setOpen(true);
    if (!chat.floatingChatEnabled) navigation.push(paths.chat());
  }} />;
}
