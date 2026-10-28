import { useRef, useState, type PointerEvent } from "react";
import type { FeedbackItem, FeedbackMark } from "@silieco/core/page-feedback";
import { Button } from "@silieco/ui/components/ui/button";
import { useT } from "../i18n";

export async function renderFeedbackImage(item: FeedbackItem, dataUrl: string): Promise<File> {
  const image = new Image(); image.src = dataUrl; await image.decode();
  const canvas = document.createElement("canvas"); canvas.width = image.naturalWidth; canvas.height = image.naturalHeight;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("Screenshot export unavailable");
  ctx.drawImage(image, 0, 0);
  ctx.strokeStyle = "#ef4444"; ctx.lineWidth = Math.max(2, canvas.width / 500);
  for (const mark of item.marks) ctx.strokeRect(mark.x * canvas.width, mark.y * canvas.height, mark.width * canvas.width, mark.height * canvas.height);
  const blob = await new Promise<Blob>((resolve, reject) => canvas.toBlob((value) => value ? resolve(value) : reject(new Error("Screenshot export failed")), "image/png"));
  return new File([blob], `page-${item.capture.id}.png`, { type: "image/png" });
}

export function ScreenshotMarkup({ image, marks, onChange, disabled }: { image: string; marks: FeedbackMark[]; onChange: (marks: FeedbackMark[]) => void; disabled: boolean }) {
  const { t } = useT("page-feedback");
  const start = useRef<{ x: number; y: number } | null>(null);
  const [drawing, setDrawing] = useState<FeedbackMark | null>(null);
  const point = (event: PointerEvent<SVGSVGElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    return { x: Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)), y: Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height)) };
  };
  return <div className="space-y-3">
    <div className="flex items-center justify-between gap-2 text-sm text-muted-foreground"><span>{t(($) => $.draw_hint)}</span><Button variant="outline" size="sm" disabled={disabled || !marks.length} onClick={() => onChange(marks.slice(0, -1))}>{t(($) => $.undo)}</Button></div>
    <div className="relative overflow-hidden rounded-lg border bg-muted">
      <img src={image} alt={t(($) => $.screenshot)} className="block h-auto w-full" draggable={false} />
      <svg className="absolute inset-0 h-full w-full touch-none" viewBox="0 0 1000 1000" preserveAspectRatio="none" aria-label={t(($) => $.draw_hint)}
        onPointerDown={(event) => { if (disabled || event.button !== 0 || marks.length >= 50) return; start.current = point(event); event.currentTarget.setPointerCapture(event.pointerId); }}
        onPointerMove={(event) => { if (!start.current) return; const p = point(event), a = start.current; setDrawing({ x: Math.min(a.x, p.x), y: Math.min(a.y, p.y), width: Math.abs(p.x - a.x), height: Math.abs(p.y - a.y) }); }}
        onPointerUp={(event) => { if (!start.current) return; const p = point(event), a = start.current; const mark = { x: Math.min(a.x, p.x), y: Math.min(a.y, p.y), width: Math.abs(p.x - a.x), height: Math.abs(p.y - a.y) }; start.current = null; setDrawing(null); if (mark.width > 0.003 && mark.height > 0.003) onChange([...marks, mark]); }}
        onPointerCancel={() => { start.current = null; setDrawing(null); }}>
        {[...marks, ...(drawing ? [drawing] : [])].map((mark, index) => <rect key={index} x={mark.x * 1000} y={mark.y * 1000} width={mark.width * 1000} height={mark.height * 1000} fill="none" stroke="#ef4444" strokeWidth="2" vectorEffect="non-scaling-stroke" />)}
      </svg>
    </div>
  </div>;
}
