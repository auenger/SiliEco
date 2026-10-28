/** Standalone native smoke test. Bundle with Electron external and run with a
 * temporary userData directory; never starts the daemon or an agent runtime. */
import { app, BrowserWindow, webContents } from "electron";
import { createServer } from "node:http";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import assert from "node:assert/strict";
import { setupBrowserTool } from "../src/main/browser/browser-tool";
import type { BrowserCommand, PageCapture } from "@silieco/core/page-feedback/protocol";

async function main() {
const testRoot = await mkdtemp(join(tmpdir(), "silieco-browser-smoke-"));
app.setPath("userData", testRoot);
const server = createServer((_req, response) => {
  response.setHeader("Content-Type", "text/html");
  response.end('<!doctype html><title>Feedback fixture</title><body style="padding:40px;font:20px sans-serif"><h1>Employee settings</h1><button id="save" style="width:180px;height:70px" onclick="window.clicked=(window.clicked||0)+1">Save changes</button><input type="password" value="secret"><p>Local test fixture</p></body>');
});
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const port = (server.address() as { port: number }).port;
const context = { userId: "smoke-user", workspaceId: "smoke-workspace", sessionId: "smoke-session" };
let win: BrowserWindow | null = null;
const deadline = setTimeout(() => { console.error("Native browser smoke timed out"); app.exit(1); }, 60000);
try {
  await app.whenReady();
  const preload = join(testRoot, "preload.cjs");
  await writeFile(preload, 'const {contextBridge,ipcRenderer}=require("electron");contextBridge.exposeInMainWorld("smoke",{command:r=>ipcRenderer.invoke("page-feedback:command",r),auth:()=>ipcRenderer.send("auth:session-state","smoke-user"),subscribe:cb=>{const h=(_,event)=>cb(event);ipcRenderer.on("page-feedback:state",h);return()=>ipcRenderer.removeListener("page-feedback:state",h)}});');
  win = new BrowserWindow({ width: 1100, height: 800, show: true, webPreferences: { preload, contextIsolation: true, sandbox: true } });
  setupBrowserTool(() => win);
  await win.loadURL("data:text/html,<title>Browser tool smoke</title><p>Native browser test</p>");
  await win.webContents.executeJavaScript("window.smoke.auth()");
  const command = (request: BrowserCommand) => win!.webContents.executeJavaScript(`window.smoke.command(${JSON.stringify(request)})`);
  await command({ type: "sync", sessionIds: [context.sessionId], activeSessionId: context.sessionId });
  await command({ type: "open", context, url: `http://127.0.0.1:${port}` });
  const guest = webContents.getAllWebContents().find((wc) => wc !== win!.webContents)!;
  if (guest.isLoading()) await new Promise<void>((resolve) => guest.once("did-stop-loading", resolve));
  await command({ type: "bounds", context, bounds: { x: 10, y: 50, width: 850, height: 650 } });
  assert.deepEqual(await guest.executeJavaScript("({node:typeof require,bridge:typeof window.desktopAPI})"), { node: "undefined", bridge: "undefined" });
  const full = await command({ type: "capture", context, mode: "page", hint: "" }) as PageCapture;
  assert.equal(full.target.kind, "page");
  const dataUrl = await command({ type: "image", context, captureId: full.id });
  assert.match(dataUrl, /^data:image\/png;base64,/);
  const pending = command({ type: "capture", context, mode: "element", hint: "Select the save button" });
  // Wait for the actual isolated-world picker, not a fixed animation delay.
  for (let i = 0; i < 100; i++) {
    if (await guest.executeJavaScriptInIsolatedWorld(999, [{ code: "!!window.__siliecoCancelPick" }])) break;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  const rect = await guest.executeJavaScript("(()=>{const r=document.querySelector('#save').getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()");
  guest.sendInputEvent({ type: "mouseMove", x: Math.round(rect.x), y: Math.round(rect.y) });
  guest.sendInputEvent({ type: "mouseDown", x: Math.round(rect.x), y: Math.round(rect.y), button: "left", clickCount: 1 });
  guest.sendInputEvent({ type: "mouseUp", x: Math.round(rect.x), y: Math.round(rect.y), button: "left", clickCount: 1 });
  const selected = await pending as PageCapture;
  assert.equal(selected.target.selector, "#save");
  assert.equal(selected.target.text, "Save changes");
  assert.equal(await guest.executeJavaScript("window.clicked||0"), 0);
  const cancelled = command({ type: "capture", context, mode: "region", hint: "Select a region" });
  await command({ type: "sync", sessionIds: [context.sessionId], activeSessionId: null });
  assert.equal(await cancelled, null);
  await command({ type: "sync", sessionIds: [], activeSessionId: null });
  assert.equal(guest.isDestroyed(), true);

  // Exercise the production React toolbar and editor, including their native
  // bounds updates. Direct IPC alone misses layout-triggered picker cancellation.
  const waitFor = async (check: () => Promise<unknown>, description: string) => {
    for (let i = 0; i < 200; i++) {
      if (await check()) return;
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
    throw new Error(`Timed out: ${description}`);
  };
  const ui = (code: string) => win!.webContents.executeJavaScript(code);
  const button = (label: string) => `Array.from(document.querySelectorAll('button')).find(b=>b.getAttribute('aria-label')===${JSON.stringify(label)}||b.textContent===${JSON.stringify(label)})`;
  const click = async (label: string) => {
    const point = await ui(`(()=>{const b=${button(label)};if(!b||b.disabled)throw Error('Unavailable button: '+${JSON.stringify(label)});const r=b.getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)}})()`);
    win!.webContents.sendInputEvent({ type: "mouseDown", ...point, button: "left", clickCount: 1 });
    win!.webContents.sendInputEvent({ type: "mouseUp", ...point, button: "left", clickCount: 1 });
  };
  const surfaceSize = () => ui("(()=>{const r=document.querySelector('[data-testid=browser-surface]').getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height}})()");
  await win.loadFile(join(__dirname, "ui/index.html"), { query: { website: `http://127.0.0.1:${port}` } });
  await waitFor(() => ui(`!!(${button("Screenshot")}&&!${button("Screenshot")}.disabled)`), "toolbar ready");
  const embedded = webContents.getAllWebContents().find((wc) => wc !== win!.webContents)!;
  await waitFor(async () => (await embedded.executeJavaScript("innerWidth")) === (await surfaceSize()).width, "native bounds ready");
  assert.equal(await ui("!!document.querySelector('#feedback-panel')"), false);
  const expandedBrowser = await surfaceSize();
  await click("Expand requirements");
  await waitFor(async () => (await surfaceSize()).width < expandedBrowser.width, "panel opened");
  await click("Collapse requirements");
  await waitFor(async () => (await surfaceSize()).width === expandedBrowser.width, "panel collapsed");

  for (const mode of ["element", "region", "page"] as const) {
    await waitFor(async () => (await embedded.executeJavaScript("innerWidth")) === (await surfaceSize()).width, "native viewport settled");
    const before = await surfaceSize();
    await click({ element: "Element", region: "Region", page: "Screenshot" }[mode]);
    if (mode !== "page") {
      await waitFor(() => embedded.executeJavaScriptInIsolatedWorld(999, [{ code: "!!window.__siliecoCancelPick" }]), `${mode} picker visible`);
      assert.deepEqual(await surfaceSize(), before, "Starting selection must not resize the browser");
      const point = await embedded.executeJavaScript("(()=>{const r=document.querySelector('#save').getBoundingClientRect();return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)}})()");
      embedded.sendInputEvent({ type: "mouseMove", ...point });
      embedded.sendInputEvent({ type: "mouseDown", ...point, button: "left", clickCount: 1 });
      if (mode === "region") embedded.sendInputEvent({ type: "mouseMove", x: point.x + 100, y: point.y + 80 });
      embedded.sendInputEvent({ type: "mouseUp", x: point.x + (mode === "region" ? 100 : 0), y: point.y + (mode === "region" ? 80 : 0), button: "left", clickCount: 1 });
    }
    await waitFor(() => ui(`!!${button("Return to website")}`), `${mode} captured into editor`);
    await waitFor(() => ui("!!Array.from(document.images).find(i=>i.alt==='Screenshot'&&i.complete&&i.naturalWidth>0)"), `${mode} image loaded`);
    assert.equal(await ui("document.querySelector('[role=alert]')?.textContent||''"), "");
    assert.equal(await ui("!!document.querySelector('#feedback-panel')"), true);
    assert.equal(await embedded.executeJavaScript("window.clicked||0"), 0);
    if (mode === "page") await writeFile(join(tmpdir(), "silieco-browser-feedback-editor.png"), (await win.capturePage()).toPNG());
    await click("Return to website");
    await waitFor(() => ui(`!${button("Return to website")}`), "returned to website");
  }
  assert.equal(await ui("document.querySelectorAll('#feedback-panel textarea').length"), 4);
  // Clearing an earlier navigation error also changes host layout. Capturing
  // immediately afterwards must wait for the native bounds to catch up.
  await ui("(()=>{const input=document.querySelector('input');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'file:///not-allowed');input.dispatchEvent(new Event('input',{bubbles:true}));})()");
  await ui("document.querySelector('form').requestSubmit()");
  await waitFor(() => ui("!!document.querySelector('[role=alert]')"), "invalid URL reported");
  await click("Screenshot");
  await waitFor(() => ui(`!!${button("Return to website")}`), "screenshot after clearing error");
  assert.equal(await ui("document.querySelector('[role=alert]')?.textContent||''"), "");
  await click("Return to website");
  await click("Collapse requirements");
  await waitFor(async () => (await embedded.executeJavaScript("innerWidth")) === (await surfaceSize()).width, "collapsed viewport settled");
  await writeFile(join(tmpdir(), "silieco-browser-feedback-collapsed.png"), (await win.capturePage()).toPNG());
  await ui("window.dispatchEvent(new Event('smoke:dispose'))");
  await command({ type: "sync", sessionIds: [], activeSessionId: null });
  console.log("PASS native browser: isolated access, capture, element/region toolbar clicks, screenshot editor, stable viewport, collapsible panel, cancellation and destruction");
} catch (error) { console.error(error); process.exitCode = 1; }
finally {
  clearTimeout(deadline);
  win?.destroy();
  server.close();
  await rm(testRoot, { recursive: true, force: true });
  app.exit(process.exitCode || 0);
}

}
void main();
