"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useWorkflowChatEditorStore } from "@silieco/core/workflows";
import { cn } from "@silieco/ui/lib/utils";
import { ContentEditor, ReadonlyContent } from "../../editor";
import { Button } from "@silieco/ui/components/ui/button";
import { useT } from "../../i18n";

export function WorkflowMarkdownEditor({ label, value, onChange, placeholder, disabled = false, helper }: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  helper?: string;
}) {
  const { t } = useT("workflows");
  const id = useId();
  const applied = useWorkflowChatEditorStore((s) => s.undo);
  const previousValue = useRef(value);
  const [highlighted, setHighlighted] = useState(false);
  useEffect(() => {
    const changed = previousValue.current !== value;
    previousValue.current = value;
    if (!applied || !changed) return;
    setHighlighted(true);
    const timer = setTimeout(() => setHighlighted(false), 1800);
    return () => { clearTimeout(timer); setHighlighted(false); };
  }, [applied, value]);
  const [preview, setPreview] = useState(false);
  return (
    <section className="min-w-0 space-y-2" aria-labelledby={id}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 id={id} className="text-body font-medium">{label}</h3>
        {!disabled && <div className="flex gap-1" aria-label={label}>
          <Button type="button" size="sm" variant={!preview ? "secondary" : "ghost"} aria-pressed={!preview} onClick={() => setPreview(false)}>{t(($) => $.editor.write)}</Button>
          <Button type="button" size="sm" variant={preview ? "secondary" : "ghost"} aria-pressed={preview} onClick={() => setPreview(true)}>{t(($) => $.editor.preview)}</Button>
        </div>}
      </div>
      <div className={cn("h-40 overflow-y-auto rounded-lg border bg-background transition-shadow focus-within:ring-1 focus-within:ring-ring", highlighted && "ring-2 ring-brand/50")}>
        {preview || disabled ? (
          <div className="px-4 py-3"><ReadonlyContent content={value || placeholder || ""} /></div>
        ) : (
          <ContentEditor value={value} onUpdate={onChange} debounceMs={0} placeholder={placeholder}
            showBubbleMenu disableMentions
            className="min-h-32 px-4 py-3 text-body" />
        )}
      </div>
      <p className="text-caption text-muted-foreground">{helper ?? t(($) => $.editor.markdown_hint)}</p>
    </section>
  );
}
