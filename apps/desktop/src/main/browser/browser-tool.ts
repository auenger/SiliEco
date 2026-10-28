import { app, BrowserWindow, WebContentsView, ipcMain } from "electron";
import { createHash, randomUUID } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import {
  BROWSER_COMMAND, BROWSER_STATE, browserCommandSchema, normalizeBrowserUrl, pageTargetSchema,
  type BrowserContext, type BrowserState, type PageCapture,
} from "@silieco/core/page-feedback/protocol";
import { AUTH_SESSION_STATE_CHANNEL, parseAuthSessionUserId } from "../../shared/auth-session";
import { pickPageTarget } from "./page-picker";

interface Entry { view: WebContentsView; context: BrowserContext; cancel?: () => void; error?: string }
const guestPreferences = { sandbox: true, contextIsolation: true, nodeIntegration: false, webSecurity: true, webviewTag: false };

/** Owns only the page-feedback guests. Never changes the application's session. */
export function setupBrowserTool(getWindow: () => BrowserWindow | null): void {
  const entries = new Map<string, Entry>();
  let owner: BrowserWindow | null = null;
  let userId: string | null = null;
  let active: string | null = null;
  const close = (id: string) => {
    const entry = entries.get(id);
    if (!entry) return;
    entries.delete(id);
    entry.cancel?.();
    if (owner && !owner.isDestroyed()) owner.contentView.removeChildView(entry.view);
    if (!entry.view.webContents.isDestroyed()) entry.view.webContents.close();
  };
  const reset = () => { for (const id of [...entries.keys()]) close(id); active = null; };
  const bindOwner = (win: BrowserWindow) => {
    if (owner === win) return;
    reset(); owner = win;
    win.on("closed", () => { if (owner === win) { reset(); owner = null; userId = null; } });
    win.webContents.on("did-start-navigation", (_event, _url, inPlace, mainFrame) => { if (mainFrame && !inPlace) reset(); });
    win.webContents.on("render-process-gone", reset);
    win.on("resize", () => { for (const entry of entries.values()) { entry.cancel?.(); entry.view.setVisible(false); } });
  };
  const trusted = (event: Electron.IpcMainEvent | Electron.IpcMainInvokeEvent) => {
    const win = getWindow();
    if (!win || win.isDestroyed() || event.sender !== win.webContents || event.senderFrame !== win.webContents.mainFrame) throw new Error("Untrusted browser tool caller");
    bindOwner(win); return win;
  };
  ipcMain.on(AUTH_SESSION_STATE_CHANNEL, (event, value: unknown) => {
    if (event.sender !== getWindow()?.webContents || event.senderFrame !== event.sender.mainFrame) return;
    const next = parseAuthSessionUserId(value);
    if (next === undefined) return;
    if (next !== userId) reset();
    userId = next;
  });
  const state = (entry: Entry): BrowserState => {
    const wc = entry.view.webContents;
    return { url: wc.getURL(), title: wc.getTitle(), loading: wc.isLoading(), canGoBack: wc.navigationHistory.canGoBack(), canGoForward: wc.navigationHistory.canGoForward(), error: entry.error };
  };
  const publish = (entry: Entry) => {
    if (owner && !owner.isDestroyed() && !entry.view.webContents.isDestroyed()) owner.webContents.send(BROWSER_STATE, { sessionId: entry.context.sessionId, state: state(entry) });
  };
  const directory = (context: BrowserContext) => join(app.getPath("userData"), "page-feedback", createHash("sha256").update(`${context.userId}:${context.workspaceId}`).digest("hex"), context.sessionId);
  const sameContext = (a: BrowserContext, b: BrowserContext) => a.userId === b.userId && a.workspaceId === b.workspaceId && a.sessionId === b.sessionId;
  ipcMain.handle(BROWSER_COMMAND, async (event, raw: unknown) => {
    const win = trusted(event);
    const request = browserCommandSchema.parse(raw);
    if (!userId) throw new Error("Sign in before opening a website");
    if (request.type === "sync") {
      active = request.activeSessionId;
      for (const [id, entry] of entries) {
        if (!request.sessionIds.includes(id)) close(id);
        else if (id !== active) { entry.cancel?.(); entry.view.setVisible(false); }
      }
      return null;
    }
    const { context } = request;
    if (context.userId !== userId) throw new Error("Browser account changed");
    let entry = entries.get(context.sessionId);
    if (entry && !sameContext(entry.context, context)) throw new Error("Browser session belongs to another workspace");
    if (request.type === "image") {
      const bytes = await readFile(join(directory(context), `${request.captureId}.png`));
      if (context.userId !== userId) throw new Error("Browser account changed");
      return `data:image/png;base64,${bytes.toString("base64")}`;
    }
    if (request.type === "open") {
      if (!entry) {
        if (entries.size >= 12) throw new Error("Close a browser tab before opening another (maximum 12)");
        const partition = `persist:page-feedback-${createHash("sha256").update(`${userId}:${context.workspaceId}`).digest("hex")}`;
        const view = new WebContentsView({ webPreferences: { ...guestPreferences, partition } });
        entry = { view, context }; entries.set(context.sessionId, entry);
        win.contentView.addChildView(view); view.setVisible(false);
        const current = entry, wc = view.webContents;
        wc.on("before-input-event", (inputEvent, input) => {
          if (input.type !== "keyDown") return;
          const primary = process.platform === "darwin" ? input.meta : input.control;
          if (primary && !input.shift && !input.alt && input.key.toLowerCase() === "w") {
            inputEvent.preventDefault();
            if (!input.isAutoRepeat) win.webContents.send("tab:close-active");
          } else if ((primary && input.key.toLowerCase() === "r") || input.key === "F5") {
            inputEvent.preventDefault(); current.cancel?.(); current.error = undefined; wc.reload();
          }
        });
        wc.session.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));
        wc.session.setPermissionCheckHandler(() => false);
        // Each partition installs this once even if several guests share it.
        if (!blockedDownloads.has(wc.session)) { blockedDownloads.add(wc.session); wc.session.on("will-download", (downloadEvent) => downloadEvent.preventDefault()); }
        wc.setWindowOpenHandler(({ url }) => {
          try { void wc.loadURL(normalizeBrowserUrl(url)).catch(() => {}); } catch { /* Reject privileged schemes. */ }
          return { action: "deny" };
        });
        const guardNavigation = (navigationEvent: Electron.Event, url: string) => {
          try { normalizeBrowserUrl(url); } catch { navigationEvent.preventDefault(); }
        };
        wc.on("will-navigate", guardNavigation);
        wc.on("will-redirect", guardNavigation);
        const invalidate = () => { current.cancel?.(); publish(current); };
        wc.on("did-start-loading", invalidate);
        wc.on("did-navigate-in-page", invalidate);
        wc.on("did-stop-loading", () => publish(current));
        wc.on("page-title-updated", () => publish(current));
        wc.on("did-navigate", () => publish(current));
        wc.on("did-fail-load", (_event, code, description, _url, mainFrame) => { if (mainFrame && code !== -3) { current.error = description; publish(current); } });
        wc.on("render-process-gone", () => { current.cancel?.(); current.error = "The website stopped responding. Reload to try again."; publish(current); });
        if (request.url) void wc.loadURL(normalizeBrowserUrl(request.url)).catch(() => {});
      }
      return state(entry);
    }
    if (!entry) throw new Error("Browser session is closed");
    const current = entry, wc = entry.view.webContents;
    if (request.type === "bounds") {
      if (!request.bounds || active !== context.sessionId) { entry.cancel?.(); entry.view.setVisible(false); return null; }
      const zoom = win.webContents.getZoomFactor();
      const b = { x: request.bounds.x * zoom, y: request.bounds.y * zoom, width: request.bounds.width * zoom, height: request.bounds.height * zoom }, [width, height] = win.getContentSize();
      const x = Math.max(0, Math.round(b.x)), y = Math.max(0, Math.round(b.y));
      entry.view.setBounds({ x, y, width: Math.max(1, Math.min(Math.round(b.width), width - x)), height: Math.max(1, Math.min(Math.round(b.height), height - y)) });
      entry.view.setVisible(true); return null;
    }
    if (active !== context.sessionId) throw new Error("Browser tab is no longer active");
    if (request.type === "navigate") { entry.error = undefined; entry.cancel?.(); void wc.loadURL(normalizeBrowserUrl(request.url)).catch(() => {}); return null; }
    if (request.type === "action") {
      entry.cancel?.();
      if (request.action === "reload") { entry.error = undefined; wc.reload(); }
      if (request.action === "back" && wc.navigationHistory.canGoBack()) wc.navigationHistory.goBack();
      if (request.action === "forward" && wc.navigationHistory.canGoForward()) wc.navigationHistory.goForward();
      return null;
    }
    if (request.type === "capture") {
      if (entry.cancel || wc.isLoading() || !wc.getURL().startsWith("http")) throw new Error("Wait for the website to finish loading");
      let cancelled = false;
      let resolveCancel: (value: null) => void = () => {};
      const cancellation = new Promise<null>((resolve) => { resolveCancel = resolve; });
      const clear = () => { if (!wc.isDestroyed()) void wc.executeJavaScriptInIsolatedWorld(999, [{ code: "window.__siliecoCancelPick?.()" }]).catch(() => {}); };
      current.cancel = () => { cancelled = true; resolveCancel(null); clear(); };
      const url = wc.getURL(), title = wc.getTitle();
      try {
        wc.focus();
        const rawTarget = await Promise.race([cancellation, wc.executeJavaScriptInIsolatedWorld(999, [{ code: `(${pickPageTarget.toString()})(${JSON.stringify(request.mode)},${JSON.stringify(request.hint)})` }])]);
        if (!rawTarget || cancelled) return null;
        const target = pageTargetSchema.parse(rawTarget);
        const stable = async () => {
          if (cancelled || entries.get(context.sessionId) !== current || context.userId !== userId || active !== context.sessionId || wc.isDestroyed() || wc.getURL() !== url) throw new Error("Page changed; capture again");
          const frame = await wc.executeJavaScriptInIsolatedWorld(999, [{ code: "({width:innerWidth,height:innerHeight,scrollX,scrollY,devicePixelRatio,active:!!window.__siliecoCancelPick})" }]);
          if (Object.entries(target.viewport).some(([key, value]) => frame[key] !== value) || (request.mode !== "page" && !frame.active)) throw new Error("Page changed; capture again");
        };
        await stable();
        const image = await wc.capturePage();
        await stable();
        if (image.isEmpty()) throw new Error("The website did not produce a screenshot");
        const capture: PageCapture = { id: randomUUID(), url, title, capturedAt: new Date().toISOString(), target };
        await mkdir(directory(context), { recursive: true, mode: 0o700 });
        await writeFile(join(directory(context), `${capture.id}.png`), image.toPNG(), { flag: "wx", mode: 0o600 });
        if (cancelled || context.userId !== userId) return null;
        return capture;
      } finally { current.cancel = undefined; clear(); }
    }
    return null;
  });
}
const blockedDownloads = new WeakSet<Electron.Session>();
