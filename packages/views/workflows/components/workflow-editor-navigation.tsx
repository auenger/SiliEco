"use client";

import { FileText, Layers, Plus, LockKeyhole } from "lucide-react";
import { Button } from "@silieco/ui/components/ui/button";
import { cn } from "@silieco/ui/lib/utils";
import { useT } from "../../i18n";

export function WorkflowEditorNavigation({ stages, selected, onSelect, onAdd, disabled = false }: {
  stages: readonly { id: string; name: string; locked?: boolean }[];
  selected: string;
  onSelect: (id: string) => void;
  onAdd: () => void;
  disabled?: boolean;
}) {
  const { t } = useT("workflows");
  return (
    <nav aria-label={t(($) => $.editor.navigation)} className="flex shrink-0 flex-col border-b bg-muted/15 md:w-56 md:border-b-0 md:border-r">
      <div className="flex gap-1 overflow-x-auto p-3 md:min-h-0 md:flex-1 md:flex-col md:overflow-y-auto md:overflow-x-hidden">
        <button type="button" aria-current={selected === "overview" ? "step" : undefined} onClick={() => onSelect("overview")}
          className={cn("flex shrink-0 items-center gap-2 rounded-md px-3 py-2 text-left text-body transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", selected === "overview" ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-accent/50")}>
          <FileText className="size-4 shrink-0" />{t(($) => $.editor.overview)}
        </button>
        <div className="hidden items-center gap-2 px-3 pb-1 pt-5 text-caption text-muted-foreground md:flex"><Layers className="size-3.5" />{t(($) => $.designer.stage_list, { count: stages.length })}</div>
        {stages.map((stage, index) => (
          <button key={stage.id} type="button" title={stage.name} aria-current={selected === stage.id ? "step" : undefined} onClick={() => onSelect(stage.id)}
            className={cn("flex min-w-0 shrink-0 items-center gap-2 rounded-md px-3 py-2 text-left text-body transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", selected === stage.id ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-accent/50")}>
            <span className="flex size-5 shrink-0 items-center justify-center rounded border text-micro tabular-nums">{index + 1}</span>
            <span className="max-w-48 truncate">{stage.name || t(($) => $.create.stage_placeholder)}</span>
            {stage.locked && <LockKeyhole className="ml-auto size-3 shrink-0" />}
          </button>
        ))}
      </div>
      <div className="hidden border-t p-3 md:block"><Button type="button" variant="ghost" size="sm" className="w-full justify-start" disabled={disabled} onClick={onAdd}><Plus className="size-3.5" />{t(($) => $.create.add_stage)}</Button></div>
    </nav>
  );
}

export type WorkflowEditorTab = "content" | "execution" | "review";
export function WorkflowEditorTabs({ value, onChange }: { value: WorkflowEditorTab; onChange: (value: WorkflowEditorTab) => void }) {
  const { t } = useT("workflows");
  return <div className="flex gap-1 border-b pb-3" aria-label={t(($) => $.editor.sections)}>
    {(["content", "execution", "review"] as const).map((tab) => <Button key={tab} type="button" size="sm" variant={value === tab ? "secondary" : "ghost"} aria-pressed={value === tab} onClick={() => onChange(tab)}>{t(($) => $.editor[tab])}</Button>)}
  </div>;
}
