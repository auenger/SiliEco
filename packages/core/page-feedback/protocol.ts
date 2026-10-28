import { z } from "zod";

const id = z.string().regex(/^[a-zA-Z0-9_-]{1,128}$/);
export const browserContextSchema = z.object({ userId: id, workspaceId: id, sessionId: id }).strict();
export type BrowserContext = z.infer<typeof browserContextSchema>;
export const rectangleSchema = z.object({ x: z.number().finite(), y: z.number().finite(), width: z.number().positive().max(16384), height: z.number().positive().max(16384) }).strict();
export const pageTargetSchema = z.object({
  kind: z.enum(["element", "region", "page"]),
  selector: z.string().max(1500), text: z.string().max(1000), name: z.string().max(500),
  rect: rectangleSchema, viewport: z.object({ width: z.number().positive(), height: z.number().positive(), scrollX: z.number().finite(), scrollY: z.number().finite(), devicePixelRatio: z.number().positive() }).strict(),
}).strict();
export type PageTarget = z.infer<typeof pageTargetSchema>;
export interface PageCapture { id: string; url: string; title: string; capturedAt: string; target: PageTarget }
export const pageCaptureSchema = z.object({ id, url: z.string().max(8192), title: z.string().max(2000), capturedAt: z.string(), target: pageTargetSchema }).strict();
export const browserStateSchema = z.object({ url: z.string(), title: z.string(), loading: z.boolean(), canGoBack: z.boolean(), canGoForward: z.boolean(), error: z.string().optional() });
export interface BrowserState { url: string; title: string; loading: boolean; canGoBack: boolean; canGoForward: boolean; error?: string }
export const browserCommandSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("sync"), sessionIds: z.array(id).max(500), activeSessionId: id.nullable() }).strict(),
  z.object({ type: z.literal("open"), context: browserContextSchema, url: z.string().max(8192) }).strict(),
  z.object({ type: z.literal("bounds"), context: browserContextSchema, bounds: rectangleSchema.nullable() }).strict(),
  z.object({ type: z.literal("navigate"), context: browserContextSchema, url: z.string().max(8192) }).strict(),
  z.object({ type: z.literal("action"), context: browserContextSchema, action: z.enum(["back", "forward", "reload", "cancel"]) }).strict(),
  z.object({ type: z.literal("capture"), context: browserContextSchema, mode: z.enum(["element", "region", "page"]), hint: z.string().max(300) }).strict(),
  z.object({ type: z.literal("image"), context: browserContextSchema, captureId: id }).strict(),
]);
export type BrowserCommand = z.infer<typeof browserCommandSchema>;
export interface BrowserBridge {
  command(command: BrowserCommand): Promise<unknown>;
  subscribe(listener: (event: { sessionId: string; state: BrowserState }) => void): () => void;
}
export const BROWSER_COMMAND = "page-feedback:command";
export const BROWSER_STATE = "page-feedback:state";

export function normalizeBrowserUrl(input: string): string {
  const value = input.trim();
  if (!value) throw new Error("Enter a website address");
  const local = /^(localhost|127(?:\.\d{1,3}){3}|\[::1\])(?::\d+)?(?:\/|$)/i.test(value);
  const url = new URL(/^[a-z][a-z\d+.-]*:/i.test(value) && !/^[\w.-]+:\d+(?:\/|$)/.test(value) ? value : `${local ? "http" : "https"}://${value}`);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw new Error("Only HTTP and HTTPS website addresses are supported");
  return url.href;
}

export function browserSessionFromPath(path: string): string | null {
  return /^\/[^/]+\/browser\/([a-zA-Z0-9_-]{1,128})(?:[?#]|$)/.exec(path)?.[1] ?? null;
}
