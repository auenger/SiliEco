"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, Pencil, Plus, Server, Trash2 } from "lucide-react";
import { ApiError } from "@silieco/core/api";
import {
  useCreateWorkspaceMCPServer,
  useDeleteWorkspaceMCPServer,
  useUpdateWorkspaceMCPServer,
  workspaceMCPServersOptions,
} from "@silieco/core/mcp";
import { useCurrentMember } from "@silieco/core/permissions";
import { useWorkspaceId } from "@silieco/core/hooks";
import type { WorkspaceMCPServer } from "@silieco/core/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@silieco/ui/components/ui/alert-dialog";
import { Badge } from "@silieco/ui/components/ui/badge";
import { Button } from "@silieco/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@silieco/ui/components/ui/dialog";
import { Input } from "@silieco/ui/components/ui/input";
import { Label } from "@silieco/ui/components/ui/label";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { toast } from "sonner";
import { useT } from "../../i18n";

const NEW_SERVER_EXAMPLE = `{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-example"]
}`;

function parseConfig(value: string): Record<string, unknown> | null {
  try {
    const parsed: unknown = JSON.parse(value);
    return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

export function WorkspaceMCPSection() {
  const { t } = useT("settings");
  const workspaceId = useWorkspaceId();
  const { role } = useCurrentMember(workspaceId);
  const canManage = role === "owner" || role === "admin";
  const catalog = useQuery(workspaceMCPServersOptions(workspaceId));
  const createServer = useCreateWorkspaceMCPServer();
  const updateServer = useUpdateWorkspaceMCPServer();
  const deleteServer = useDeleteWorkspaceMCPServer();
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<WorkspaceMCPServer | null>(null);
  const [name, setName] = useState("");
  const [configText, setConfigText] = useState(NEW_SERVER_EXAMPLE);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceMCPServer | null>(null);
  const config = useMemo(() => parseConfig(configText), [configText]);
  const pending = createServer.isPending || updateServer.isPending;

  useEffect(() => {
    if (!editorOpen) return;
    setName(editing?.name ?? "");
    setConfigText(NEW_SERVER_EXAMPLE);
  }, [editing, editorOpen]);

  const openCreate = () => {
    setEditing(null);
    setEditorOpen(true);
  };

  const openEdit = (server: WorkspaceMCPServer) => {
    setEditing(server);
    setEditorOpen(true);
  };

  const save = async () => {
    if (!name.trim() || !config || pending) return;
    try {
      if (editing) await updateServer.mutateAsync({ id: editing.id, data: { name: name.trim(), config } });
      else await createServer.mutateAsync({ name: name.trim(), config });
      toast.success(t(($) => $.mcp.saved));
      setEditorOpen(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.mcp.save_failed));
    }
  };

  const remove = async () => {
    if (!deleteTarget) return;
    try {
      await deleteServer.mutateAsync(deleteTarget.id);
      toast.success(t(($) => $.mcp.deleted));
      setDeleteTarget(null);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.mcp.delete_failed));
    }
  };

  if (catalog.error instanceof ApiError && catalog.error.status === 503) {
    return <p className="rounded-lg border border-dashed px-4 py-3 text-caption text-muted-foreground">{t(($) => $.mcp.unavailable)}</p>;
  }

  const servers = catalog.data?.servers ?? [];
  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <p className="max-w-2xl text-body leading-6 text-muted-foreground">{t(($) => $.mcp.description)}</p>
        {canManage && <Button size="sm" variant="outline" onClick={openCreate}><Plus className="size-3.5" />{t(($) => $.mcp.add)}</Button>}
      </div>
      {catalog.isLoading ? (
        <p className="flex items-center gap-2 text-caption text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t(($) => $.mcp.loading)}</p>
      ) : servers.length === 0 ? (
        <div className="rounded-lg border border-dashed px-4 py-6 text-caption text-muted-foreground">{t(($) => $.mcp.empty)}</div>
      ) : (
        <ul className="divide-y rounded-lg border bg-surface-raised/40">
          {servers.map((server) => (
            <li key={server.id} className="flex items-center gap-3 p-3">
              <span className="flex size-9 items-center justify-center rounded-md bg-muted text-muted-foreground"><Server className="size-4" /></span>
              <div className="min-w-0 flex-1"><p className="truncate text-body font-medium">{server.name}</p><p className="text-caption text-muted-foreground">{t(($) => $.mcp.encrypted_hint)}</p></div>
              <Badge variant="outline">{server.configured ? t(($) => $.mcp.configured) : t(($) => $.mcp.not_configured)}</Badge>
              {canManage && <div className="flex gap-1"><Button size="icon-sm" variant="ghost" aria-label={t(($) => $.mcp.edit)} onClick={() => openEdit(server)}><Pencil className="size-3.5" /></Button><Button size="icon-sm" variant="ghost" aria-label={t(($) => $.mcp.delete)} onClick={() => setDeleteTarget(server)}><Trash2 className="size-3.5" /></Button></div>}
            </li>
          ))}
        </ul>
      )}

      <Dialog open={editorOpen} onOpenChange={(open) => !pending && setEditorOpen(open)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader><DialogTitle>{editing ? t(($) => $.mcp.edit_title) : t(($) => $.mcp.add_title)}</DialogTitle><DialogDescription>{editing ? t(($) => $.mcp.replace_description) : t(($) => $.mcp.dialog_description)}</DialogDescription></DialogHeader>
          <div className="space-y-2"><Label htmlFor="workspace-mcp-name">{t(($) => $.mcp.name)}</Label><Input id="workspace-mcp-name" value={name} onChange={(event) => setName(event.target.value)} /></div>
          <div className="space-y-2"><Label htmlFor="workspace-mcp-config">{t(($) => $.mcp.config)}</Label><Textarea id="workspace-mcp-config" className="min-h-64 font-mono text-caption" value={configText} onChange={(event) => setConfigText(event.target.value)} aria-invalid={config === null || undefined} />{config === null && <p className="text-caption text-destructive">{t(($) => $.mcp.invalid_json)}</p>}</div>
          <DialogFooter><Button variant="ghost" onClick={() => setEditorOpen(false)} disabled={pending}>{t(($) => $.mcp.cancel)}</Button><Button onClick={save} disabled={pending || !name.trim() || config === null}>{pending && <Loader2 className="size-3.5 animate-spin" />}{t(($) => $.mcp.save)}</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => !open && !deleteServer.isPending && setDeleteTarget(null)}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>{t(($) => $.mcp.delete_title)}</AlertDialogTitle><AlertDialogDescription>{t(($) => $.mcp.delete_description, { name: deleteTarget?.name ?? "" })}</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>{t(($) => $.mcp.cancel)}</AlertDialogCancel><AlertDialogAction variant="destructive" onClick={remove} disabled={deleteServer.isPending}>{t(($) => $.mcp.delete)}</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
