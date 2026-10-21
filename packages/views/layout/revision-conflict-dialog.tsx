"use client";

import { useMemo, useState } from "react";
import { Copy, Loader2, RefreshCw } from "lucide-react";
import { useRevisionConflictStore } from "@silieco/core/revisions";
import { Button } from "@silieco/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@silieco/ui/components/ui/dialog";
import { Label } from "@silieco/ui/components/ui/label";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { toast } from "sonner";
import { useT } from "../i18n";

function editableText(value: Record<string, unknown>): string {
  for (const key of ["content", "description", "title"] as const) {
    if (typeof value[key] === "string" && value[key].trim()) return value[key];
  }
  const changedFields = Object.fromEntries(
    Object.entries(value).filter(
      ([key, fieldValue]) =>
        key !== "expected_revision" &&
        fieldValue !== null &&
        fieldValue !== undefined,
    ),
  );
  return JSON.stringify(changedFields, null, 2);
}

export function RevisionConflictDialog() {
  const { t } = useT("common");
  const conflict = useRevisionConflictStore((state) => state.conflict);
  const close = useRevisionConflictStore((state) => state.close);
  const [working, setWorking] = useState<"reload" | "overwrite" | null>(null);
  const currentText = useMemo(() => conflict ? editableText(conflict.current) : "", [conflict]);
  const attemptedText = useMemo(() => conflict ? editableText(conflict.attempted) : "", [conflict]);

  const reload = async () => {
    if (!conflict) return;
    setWorking("reload");
    try {
      await conflict.refresh();
      close();
    } finally {
      setWorking(null);
    }
  };

  const overwrite = async () => {
    if (!conflict) return;
    setWorking("overwrite");
    try {
      await conflict.retry();
      await conflict.refresh();
      toast.success(t(($) => $.revision_conflict.overwrite_success));
      close();
    } catch (error) {
      toast.error(t(($) => $.revision_conflict.overwrite_failed));
    } finally {
      setWorking(null);
    }
  };

  const copyMine = async () => {
    try {
      await navigator.clipboard.writeText(attemptedText);
      toast.success(t(($) => $.revision_conflict.copied));
    } catch {
      toast.error(t(($) => $.revision_conflict.copy_failed));
    }
  };

  return (
    <Dialog open={conflict !== null} onOpenChange={(open) => !open && working === null && close()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {conflict?.kind === "issue"
              ? t(($) => $.revision_conflict.task_title)
              : t(($) => $.revision_conflict.title)}
          </DialogTitle>
          <DialogDescription>
            {conflict?.kind === "issue"
              ? t(($) => $.revision_conflict.task_description)
              : t(($) => $.revision_conflict.description)}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2"><Label htmlFor="revision-current">{t(($) => $.revision_conflict.server_version)}</Label><Textarea id="revision-current" readOnly rows={10} value={currentText} className="resize-none" /></div>
          <div className="space-y-2"><Label htmlFor="revision-attempted">{t(($) => $.revision_conflict.your_version)}</Label><Textarea id="revision-attempted" readOnly rows={10} value={attemptedText} className="resize-none" /></div>
        </div>
        <DialogFooter className="flex-wrap sm:justify-between">
          <Button variant="outline" onClick={copyMine} disabled={working !== null}><Copy className="size-3.5" />{t(($) => $.revision_conflict.copy_mine)}</Button>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={reload} disabled={working !== null}>{working === "reload" ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}{t(($) => $.revision_conflict.reload)}</Button>
            <Button onClick={overwrite} disabled={working !== null}>{working === "overwrite" && <Loader2 className="size-3.5 animate-spin" />}{t(($) => $.revision_conflict.overwrite)}</Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
