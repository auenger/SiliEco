import { useEffect, useRef, useState, type ReactNode } from "react";
import { ArrowLeft, ArrowRight, CornerDownLeft, RotateCw, Globe, MousePointer2, Scan, Camera, PanelRightOpen, PanelRightClose, MessageCircle, X, Loader2 } from "lucide-react";
import { Button } from "@silieco/ui/components/ui/button";
import { Input } from "@silieco/ui/components/ui/input";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { useT } from "../i18n";
import {
  usePageFeedbackStore, feedbackDraftKey, emptyFeedbackDraft, pageCaptureSchema,
  normalizeBrowserUrl, useSubmitFeedback, useFeedbackChat, buildFeedbackDescription,
  type BrowserBridge, type BrowserContext, type BrowserState, type FeedbackItem,
} from "@silieco/core/page-feedback";
import { ScreenshotMarkup, renderFeedbackImage } from "./screenshot";

export function PageFeedbackPage({ context, bridge, state, surface, onCovered, onTaskCreated, onChatPrepared }: {
  context: BrowserContext; bridge: BrowserBridge; state: BrowserState; surface: ReactNode;
  onCovered: (covered: boolean) => void; onTaskCreated: (id: string) => void;
  onChatPrepared: (target: { sessionId: string; agentId: string }) => void;
}) {
  const { t } = useT("page-feedback");
  const key = feedbackDraftKey(context);
  const draft = usePageFeedbackStore((s) => s.drafts[key] ?? emptyFeedbackDraft);
  const pending = usePageFeedbackStore((s) => s.pending[key] === true);
  const patch = (value: Parameters<ReturnType<typeof usePageFeedbackStore.getState>["patch"]>[1]) => usePageFeedbackStore.getState().patch(key, value);
  const [address, setAddress] = useState(draft.url);
  const [error, setError] = useState("");
  const [picking, setPicking] = useState<"element" | "region" | "page" | null>(null);
  const [panelOpen, setPanelOpen] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [image, setImage] = useState("");
  const [preview, setPreview] = useState(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const readImage = async (item: FeedbackItem) => {
    const result = await bridge.command({ type: "image", context, captureId: item.capture.id });
    if (typeof result !== "string" || !result.startsWith("data:image/png;base64,")) throw new Error(t(($) => $.image_missing));
    return result;
  };
  const submit = useSubmitFeedback(context, async (item) => renderFeedbackImage(item, await readImage(item)));
  const prepareChat = useFeedbackChat(context, async (item) => renderFeedbackImage(item, await readImage(item)));
  const locked = pending || !!draft.taskId;
  const editingItem = draft.items.find((item) => item.capture.id === editing);
  useEffect(() => { if (state.url && state.url !== "about:blank") { setAddress(state.url); usePageFeedbackStore.getState().patch(key, { url: state.url }); } }, [state.url, key]);
  useEffect(() => { onCovered(!!editing || preview); return () => onCovered(false); }, [editing, preview, onCovered]);
  useEffect(() => {
    setImage(""); let cancelled = false;
    if (editingItem) void readImage(editingItem).then((value) => { if (!cancelled) setImage(value); }).catch((e: unknown) => { if (!cancelled) setError(e instanceof Error ? e.message : t(($) => $.failed)); });
    return () => { cancelled = true; };
    // The original image is immutable; marks and notes do not reload it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing, key]);
  const perform = async (operation: () => Promise<unknown>) => { setError(""); try { await operation(); } catch (e) { if (mounted.current) setError(e instanceof Error && e.message ? e.message : t(($) => $.failed)); } };
  const capture = async (mode: "element" | "region" | "page") => {
    if (picking || locked || draft.items.length >= 30) return;
    setPicking(mode);
    await perform(async () => {
      // Let the host commit transient UI (including a cleared error message)
      // and synchronize native bounds before recording the guest viewport.
      await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      if (!mounted.current) return;
      const raw = await bridge.command({ type: "capture", context, mode, hint: t(($) => $.pick_hint) });
      if (!raw) return;
      const captured = pageCaptureSchema.parse(raw);
      const { rect, viewport } = captured.target;
      const item: FeedbackItem = { capture: captured, note: "", marks: mode === "page" ? [] : [{ x: rect.x / viewport.width, y: rect.y / viewport.height, width: rect.width / viewport.width, height: rect.height / viewport.height }] };
      const current = usePageFeedbackStore.getState().drafts[key] ?? emptyFeedbackDraft;
      patch({ items: [...current.items, item] });
      if (mounted.current) { setEditing(captured.id); setPanelOpen(true); }
    });
    if (mounted.current) setPicking(null);
  };
  const discuss = () => void perform(async () => {
    let current = usePageFeedbackStore.getState().drafts[key] ?? emptyFeedbackDraft;
    if (!current.items.length && !current.chatSessionId) {
      await capture("page");
      current = usePageFeedbackStore.getState().drafts[key] ?? emptyFeedbackDraft;
      if (!current.items.length) return;
    }
    const target = await prepareChat.mutateAsync({ draft: current, instructions: t(($) => $.agent_instructions), title: t(($) => $.agent_chat_title), noAgent: t(($) => $.no_agent) });
    if (mounted.current) { setEditing(null); setPreview(false); setPanelOpen(false); onChatPrepared(target); }
  });
  const updateItem = (id: string, change: Partial<FeedbackItem>) => patch({ items: draft.items.map((item) => item.capture.id === id ? { ...item, ...change } : item) });
  return <div className="flex min-h-0 flex-1 flex-col bg-background text-foreground [&_input]:text-foreground [&_textarea]:text-foreground">
    {/* Capture controls retain their dimensions while picking. A toolbar reflow
        resizes the native viewport and invalidates the selection/screenshot. */}
    <form className="flex h-12 shrink-0 items-center gap-1 border-b px-2" onSubmit={(event) => { event.preventDefault(); void perform(async () => { const url = normalizeBrowserUrl(address); await bridge.command({ type: "navigate", context, url }); patch({ url }); setEditing(null); setPreview(false); }); }}>
      <Button type="button" variant="ghost" size="icon" title={t(($) => $.back)} aria-label={t(($) => $.back)} disabled={!state.canGoBack || !!picking} onClick={() => void perform(() => bridge.command({ type: "action", context, action: "back" }))}><ArrowLeft /></Button>
      <Button type="button" variant="ghost" size="icon" title={t(($) => $.forward)} aria-label={t(($) => $.forward)} disabled={!state.canGoForward || !!picking} onClick={() => void perform(() => bridge.command({ type: "action", context, action: "forward" }))}><ArrowRight /></Button>
      <Button type="button" variant="ghost" size="icon" title={t(($) => $.reload)} aria-label={t(($) => $.reload)} onClick={() => void perform(() => bridge.command({ type: "action", context, action: "reload" }))}>{state.loading ? <Loader2 className="animate-spin" /> : <RotateCw />}</Button>
      <Input value={address} onChange={(event) => setAddress(event.target.value)} aria-label={t(($) => $.address)} placeholder={t(($) => $.address)} className="min-w-0 flex-1" spellCheck={false} disabled={!!picking} />
      <Button type="submit" variant="ghost" size="icon" title={t(($) => $.open)} aria-label={t(($) => $.open)} disabled={!!picking}><CornerDownLeft /></Button>
      <div className="mx-1 h-5 w-px shrink-0 bg-border" />
      {([
        { mode: "element", label: t(($) => $.element), Icon: MousePointer2 },
        { mode: "region", label: t(($) => $.region), Icon: Scan },
        { mode: "page", label: t(($) => $.screenshot), Icon: Camera },
      ] as const).map(({ mode, label, Icon }) => <Button
        key={mode} type="button" variant={picking === mode ? "secondary" : "ghost"} size="icon"
        title={picking === mode ? t(($) => $.cancel) : label}
        aria-label={picking === mode ? t(($) => $.cancel) : label} aria-pressed={picking === mode}
        disabled={locked || draft.items.length >= 30 || state.loading || !state.url || !!editing || preview || (!!picking && picking !== mode)}
        onClick={() => { if (picking === mode) void perform(() => bridge.command({ type: "action", context, action: "cancel" })); else void capture(mode); }}
      >{picking === mode ? <X /> : <Icon />}</Button>)}
      <div className="mx-1 h-5 w-px shrink-0 bg-border" />
      <Button type="button" variant="ghost" size="icon" title={panelOpen ? t(($) => $.collapse_panel) : t(($) => $.expand_panel)} aria-label={panelOpen ? t(($) => $.collapse_panel) : t(($) => $.expand_panel)} aria-expanded={panelOpen} aria-controls="feedback-panel" disabled={!!picking} onClick={() => setPanelOpen(!panelOpen)}>{panelOpen ? <PanelRightClose /> : <PanelRightOpen />}</Button>
      <Button type="button" variant="ghost" size="icon" title={t(($) => $.discuss)} aria-label={t(($) => $.discuss)} disabled={locked || !!picking || state.loading || !state.url} onClick={discuss}>{prepareChat.isPending ? <Loader2 className="animate-spin" /> : <MessageCircle />}</Button>
    </form>
    {(error || state.error) && <div role="alert" className="shrink-0 border-b px-3 py-2 text-sm text-destructive">{error || state.error}</div>}
    <div className="flex min-h-0 flex-1">
      <div className="relative min-h-0 min-w-0 flex-1">
        {surface}
        {!state.url && !editing && !preview && <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground"><Globe className="size-10" /><p>{t(($) => $.empty)}</p></div>}
        {(editing || preview) && <div className="absolute inset-0 overflow-auto bg-background p-4">
          <div className="mb-4 flex items-center justify-between"><span className="font-medium">{preview ? t(($) => $.preview) : t(($) => $.screenshot)}</span><Button variant="outline" size="sm" onClick={() => { setEditing(null); setPreview(false); }}>{t(($) => $.return_page)}</Button></div>
          {preview ? <pre className="whitespace-pre-wrap break-words font-sans text-sm leading-relaxed">{buildFeedbackDescription(draft)}</pre> : image && editingItem ? <ScreenshotMarkup image={image} marks={editingItem.marks} disabled={locked} onChange={(marks) => updateItem(editingItem.capture.id, { marks })} /> : <p className="text-muted-foreground">{t(($) => $.loading_image)}</p>}
        </div>}
      </div>
      {panelOpen && <aside id="feedback-panel" className="flex w-80 shrink-0 flex-col border-l xl:w-96">
        <div className="min-h-0 flex-1 space-y-4 overflow-auto p-4">
          <div className="space-y-2"><p className="text-sm font-medium">{t(($) => $.agent_heading)}</p><p className="text-xs leading-relaxed text-muted-foreground">{t(($) => $.agent_hint)}</p></div>
          <div><label htmlFor="feedback-request" className="mb-2 block text-sm font-medium">{t(($) => $.optional_request)}</label><Textarea id="feedback-request" value={draft.request} maxLength={20000} disabled={locked} onChange={(event) => patch({ request: event.target.value })} className="min-h-16" placeholder={t(($) => $.optional_request_placeholder)} /></div>
          {draft.items.map((item, index) => <div key={item.capture.id} className="space-y-2 rounded-lg border p-3">
            <div className="flex items-start gap-2"><Button variant="ghost" size="sm" className="h-auto min-w-0 flex-1 justify-start whitespace-normal break-all text-left" onClick={() => { setEditing(item.capture.id); setPreview(false); }}>{index + 1}. {item.capture.target.text.slice(0, 55) || item.capture.title || item.capture.url}</Button><Button variant="ghost" size="icon-sm" aria-label={t(($) => $.remove)} disabled={locked} onClick={() => { patch({ items: draft.items.filter((i) => i !== item) }); if (editing === item.capture.id) setEditing(null); }}><X /></Button></div>
            <Textarea aria-label={t(($) => $.note)} placeholder={t(($) => $.note)} value={item.note} maxLength={4000} disabled={locked} onChange={(event) => updateItem(item.capture.id, { note: event.target.value })} />
          </div>)}
        </div>
        <div className="space-y-2 border-t p-3">
          <Button className="w-full" disabled={locked || !!picking || state.loading || !state.url} onClick={discuss}>{prepareChat.isPending ? <Loader2 className="animate-spin" /> : <MessageCircle />}{t(($) => $.discuss)}</Button>
          <details open={draft.taskId ? true : undefined} className="rounded-lg border p-3">
            <summary className="cursor-pointer text-xs text-muted-foreground">{t(($) => $.manual_create)}</summary>
            <div className="mt-3 space-y-3">
              <div><label htmlFor="feedback-title" className="mb-2 block text-sm font-medium">{t(($) => $.task_title)}</label><Input id="feedback-title" value={draft.title} maxLength={250} disabled={locked} onChange={(event) => patch({ title: event.target.value })} placeholder={t(($) => $.title_placeholder)} /></div>
              <p className="text-xs text-muted-foreground">{draft.taskId ? t(($) => $.retry_hint) : t(($) => $.submit_hint)}</p>
              <div className="flex gap-2"><Button variant="outline" disabled={!!picking} onClick={() => { setEditing(null); setPreview(!preview); }}>{t(($) => $.preview)}</Button><Button className="flex-1" disabled={pending || !!picking || !draft.title.trim() || (!draft.request.trim() && !draft.items.length)} onClick={() => void perform(async () => { const id = await submit.mutateAsync(draft); if (mounted.current) { usePageFeedbackStore.getState().clear(key); onTaskCreated(id); } })}>{pending && <Loader2 className="animate-spin" />}{draft.taskId ? t(($) => $.retry) : t(($) => $.create_task)}</Button></div>
            </div>
          </details>
        </div>
      </aside>}
    </div>
  </div>;
}
