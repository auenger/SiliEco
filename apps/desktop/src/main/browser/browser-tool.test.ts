import { EventEmitter } from "node:events";
import { beforeEach, describe, expect, it, vi } from "vitest";
const registry = vi.hoisted(() => ({ handlers: new Map<string, (...args: any[]) => any>(), listeners: new Map<string, (...args: any[]) => any>(), views: [] as any[] }));
vi.mock("electron", () => ({
  app: { getPath: () => "/tmp/page-feedback-test" }, BrowserWindow: class {},
  ipcMain: { handle: (name: string, handler: (...args: any[]) => any) => registry.handlers.set(name, handler), on: (name: string, handler: (...args: any[]) => any) => registry.listeners.set(name, handler) },
  WebContentsView: class {
    webContents = Object.assign(new EventEmitter(), { mainFrame: {}, session: Object.assign(new EventEmitter(), { setPermissionRequestHandler: vi.fn(), setPermissionCheckHandler: vi.fn() }), getURL: () => "https://example.com/", getTitle: () => "Example", isLoading: () => false, isDestroyed: () => false, getZoomFactor: () => 1, navigationHistory: { canGoBack: () => false, canGoForward: () => false }, setWindowOpenHandler: vi.fn(), loadURL: vi.fn(async () => {}), close: vi.fn(), focus: vi.fn(), executeJavaScriptInIsolatedWorld: vi.fn(() => new Promise(() => {})) });
    setVisible = vi.fn(); setBounds = vi.fn();
    constructor(public options: unknown) { registry.views.push(this); }
  },
}));
import { setupBrowserTool } from "./browser-tool";
import { BROWSER_COMMAND } from "@silieco/core/page-feedback/protocol";

describe("native browser tool boundary", () => {
  let win: any, event: any;
  const context = { userId: "user", workspaceId: "workspace", sessionId: "session" };
  const command = (request: unknown, source = event) => registry.handlers.get(BROWSER_COMMAND)!(source, request);
  beforeEach(() => {
    registry.handlers.clear(); registry.listeners.clear(); registry.views.length = 0;
    const wc = Object.assign(new EventEmitter(), { mainFrame: {}, send: vi.fn(), getZoomFactor: () => 1 });
    win = Object.assign(new EventEmitter(), { webContents: wc, contentView: { addChildView: vi.fn(), removeChildView: vi.fn() }, getContentSize: () => [1000, 800], isDestroyed: () => false });
    event = { sender: wc, senderFrame: wc.mainFrame };
    setupBrowserTool(() => win);
    registry.listeners.get("auth:session-state")!(event, "user");
  });
  it("rejects guest callers, subframes, other accounts and unknown commands", async () => {
    const request = { type: "open", context, url: "https://example.com" };
    await expect(command(request, { sender: {}, senderFrame: {} })).rejects.toThrow("Untrusted");
    await expect(command(request, { ...event, senderFrame: {} })).rejects.toThrow("Untrusted");
    await expect(command({ ...request, context: { ...context, userId: "other" } })).rejects.toThrow("account");
    await expect(command({ type: "eval", script: "1" })).rejects.toThrow();
  });
  it("creates isolated guests and releases them on close and account switch", async () => {
    await command({ type: "sync", sessionIds: ["session"], activeSessionId: "session" });
    await command({ type: "open", context, url: "https://example.com" });
    const view = registry.views[0];
    expect(view.options.webPreferences).toMatchObject({ sandbox: true, contextIsolation: true, nodeIntegration: false, webSecurity: true });
    expect(view.options.webPreferences).not.toHaveProperty("preload");
    await command({ type: "bounds", context, bounds: { x: 10, y: 50, width: 500, height: 500 } });
    expect(view.setVisible).toHaveBeenLastCalledWith(true);
    await command({ type: "sync", sessionIds: ["session"], activeSessionId: null });
    expect(view.setVisible).toHaveBeenLastCalledWith(false);
    expect(view.webContents.close).not.toHaveBeenCalled();
    registry.listeners.get("auth:session-state")!(event, "other");
    expect(view.webContents.close).toHaveBeenCalledTimes(1);
  });
  it("cancels pending selection when switching away, and destroys closed tabs", async () => {
    await command({ type: "sync", sessionIds: ["session"], activeSessionId: "session" });
    await command({ type: "open", context, url: "https://example.com" });
    const pending = command({ type: "capture", context, mode: "element", hint: "Select" });
    await command({ type: "sync", sessionIds: ["session"], activeSessionId: null });
    await expect(pending).resolves.toBeNull();
    await command({ type: "sync", sessionIds: [], activeSessionId: null });
    expect(registry.views[0].webContents.close).toHaveBeenCalledOnce();
  });
  it("blocks privileged navigation and rejects cross-workspace reuse", async () => {
    await command({ type: "open", context, url: "https://example.com" });
    const navigation = { preventDefault: vi.fn() };
    registry.views[0].webContents.emit("will-navigate", navigation, "file:///tmp/private");
    expect(navigation.preventDefault).toHaveBeenCalledOnce();
    await expect(command({ type: "open", context: { ...context, workspaceId: "other" }, url: "" })).rejects.toThrow("workspace");
  });
});
