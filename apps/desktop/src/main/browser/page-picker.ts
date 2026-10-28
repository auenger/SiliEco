/// <reference lib="dom" />
import type { PageTarget } from "@silieco/core/page-feedback/protocol";

/** Serialized into an isolated world. No closures, imports, or page bridges. */
export function pickPageTarget(mode: "element" | "region" | "page", hint: string): Promise<PageTarget | null> {
  const scope = window as typeof window & { __siliecoCancelPick?: () => void };
  scope.__siliecoCancelPick?.();
  const viewport = () => ({ width: innerWidth, height: innerHeight, scrollX, scrollY, devicePixelRatio });
  if (mode === "page") return Promise.resolve({ kind: "page", selector: "", text: "", name: "", rect: { x: 0, y: 0, width: innerWidth, height: innerHeight }, viewport: viewport() });
  return new Promise((resolve) => {
    const host = document.createElement("div");
    host.style.cssText = "position:fixed!important;inset:0!important;z-index:2147483647!important;cursor:crosshair!important;pointer-events:auto!important";
    const shadow = host.attachShadow({ mode: "closed" });
    const box = document.createElement("div");
    box.style.cssText = "position:fixed;border:2px solid #2563eb;background:#2563eb20;pointer-events:none;display:none;box-sizing:border-box";
    const label = document.createElement("div");
    label.textContent = hint;
    label.style.cssText = "position:fixed;top:12px;left:50%;transform:translateX(-50%);padding:10px 16px;background:#fff;color:#111;border:1px solid #aaa;border-radius:8px;font:14px system-ui;pointer-events:none";
    shadow.append(box, label);
    document.documentElement.append(host);
    const controller = new AbortController();
    let start: { x: number; y: number } | null = null;
    let finished = false;
    const clean = () => { controller.abort(); host.remove(); delete scope.__siliecoCancelPick; };
    const cancel = () => { clean(); if (!finished) { finished = true; resolve(null); } };
    scope.__siliecoCancelPick = cancel;
    const under = (x: number, y: number) => {
      host.style.setProperty("pointer-events", "none", "important");
      const element = document.elementFromPoint(x, y);
      host.style.setProperty("pointer-events", "auto", "important");
      return element;
    };
    const clip = (r: { x: number; y: number; width: number; height: number }) => {
      const x = Math.max(0, r.x), y = Math.max(0, r.y);
      return { x, y, width: Math.max(0, Math.min(innerWidth, r.x + r.width) - x), height: Math.max(0, Math.min(innerHeight, r.y + r.height) - y) };
    };
    const region = (x: number, y: number) => ({ x: Math.min(start!.x, x), y: Math.min(start!.y, y), width: Math.abs(x - start!.x), height: Math.abs(y - start!.y) });
    const draw = (r: ReturnType<typeof clip>) => Object.assign(box.style, { display: "block", left: `${r.x}px`, top: `${r.y}px`, width: `${r.width}px`, height: `${r.height}px` });
    const selector = (element: Element) => {
      const parts: string[] = [];
      for (let node: Element | null = element; node && parts.length < 8; node = node.parentElement) {
        if (node.id) { parts.unshift(`#${CSS.escape(node.id)}`); break; }
        const position = node.parentElement ? Array.from(node.parentElement.children).indexOf(node) + 1 : 1;
        parts.unshift(`${node.tagName.toLowerCase()}:nth-child(${position})`);
      }
      return parts.join(" > ").slice(0, 1500);
    };
    const block = (event: Event) => { event.preventDefault(); event.stopImmediatePropagation(); };
    const options = { capture: true, signal: controller.signal };
    window.addEventListener("pointerdown", (event) => { block(event); if (event.isTrusted && event.button === 0) start = { x: event.clientX, y: event.clientY }; }, options);
    window.addEventListener("pointermove", (event) => {
      block(event);
      const r = mode === "region" ? start && region(event.clientX, event.clientY) : under(event.clientX, event.clientY)?.getBoundingClientRect();
      if (r) draw(clip(r));
    }, options);
    window.addEventListener("pointerup", (event) => {
      block(event);
      if (!event.isTrusted || event.button !== 0 || !start || finished) return;
      const element = mode === "element" ? under(event.clientX, event.clientY) : null;
      const rect = clip(element ? element.getBoundingClientRect() : region(event.clientX, event.clientY));
      if (rect.width < 2 || rect.height < 2) { start = null; return; }
      const target: PageTarget = {
        kind: mode, rect, viewport: viewport(), selector: element ? selector(element) : "",
        text: element && !element.matches("input,textarea,select") ? (element.textContent ?? "").replace(/\s+/g, " ").slice(0, 1000) : "",
        name: (element?.getAttribute("aria-label") ?? element?.getAttribute("title") ?? "").slice(0, 500),
      };
      finished = true;
      box.style.display = "none"; label.style.display = "none";
      // Keep input blocked until the host finishes its capture. Cancel handlers
      // remain installed so navigation/scroll invalidates an in-flight capture.
      requestAnimationFrame(() => requestAnimationFrame(() => resolve(target)));
    }, options);
    for (const type of ["click", "dblclick", "contextmenu"]) window.addEventListener(type, block, options);
    window.addEventListener("wheel", block, { ...options, passive: false });
    window.addEventListener("keydown", (event) => { block(event); if (event.key === "Escape") cancel(); }, options);
    window.addEventListener("scroll", cancel, options);
    window.addEventListener("resize", cancel, options);
  });
}
