import { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@silieco/core/i18n/react";
import { PageFeedbackPage } from "@silieco/views/page-feedback";
import { RESOURCES } from "@silieco/views/locales";
import { browserStateSchema, type BrowserBridge, type BrowserState } from "@silieco/core/page-feedback/protocol";
import "../../src/renderer/src/globals.css";

// The real feedback UI talks to the real native browser. Task submission is
// never invoked, and this fixture has no credentials or backend connection.
const bridge = (window as unknown as { smoke: BrowserBridge & { auth(): void } }).smoke;
const context = { userId: "smoke-user", workspaceId: "smoke-workspace", sessionId: "smoke-session" };

function Fixture() {
  const surface = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<BrowserState>({ url: "", title: "", loading: false, canGoBack: false, canGoForward: false });
  const [covered, setCovered] = useState(false);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const unsubscribe = bridge.subscribe((event) => setState(browserStateSchema.parse(event.state)));
    bridge.auth();
    void bridge.command({ type: "sync", sessionIds: [context.sessionId], activeSessionId: context.sessionId })
      .then(() => bridge.command({ type: "open", context, url: new URLSearchParams(location.search).get("website")! }))
      .then((value) => { setState(browserStateSchema.parse(value)); setReady(true); });
    return unsubscribe;
  }, []);
  useEffect(() => {
    if (!ready) return;
    let frame = 0, last = "";
    const update = () => {
      const rect = surface.current!.getBoundingClientRect();
      const bounds = !covered && state.url.startsWith("http") ? { x: rect.x, y: rect.y, width: rect.width, height: rect.height } : null;
      const next = JSON.stringify(bounds);
      if (last !== next) { last = next; void bridge.command({ type: "bounds", context, bounds }); }
      frame = requestAnimationFrame(update);
    };
    update();
    return () => cancelAnimationFrame(frame);
  }, [covered, ready, state.url]);
  return <div className="flex h-screen flex-col"><PageFeedbackPage context={context} bridge={bridge} state={state} surface={<div ref={surface} data-testid="browser-surface" className="h-full w-full" />} onCovered={setCovered} onTaskCreated={() => { throw new Error("Smoke must never create tasks"); }} onChatPrepared={() => { throw new Error("Smoke must never contact an agent"); }} /></div>;
}

const root = createRoot(document.getElementById("root")!);
root.render(<QueryClientProvider client={new QueryClient()}><I18nProvider locale="en" resources={RESOURCES}><Fixture /></I18nProvider></QueryClientProvider>);
window.addEventListener("smoke:dispose", () => root.unmount(), { once: true });
