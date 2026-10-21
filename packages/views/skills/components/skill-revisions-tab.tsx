"use client";

import { useRef, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { Skill } from "@silieco/core/types";
import { skillRevisionDetailOptions, skillRevisionListOptions, useCreateSkillRevision } from "@silieco/core/skills";
import { Button } from "@silieco/ui/components/ui/button";
import { Input } from "@silieco/ui/components/ui/input";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@silieco/ui/components/ui/dialog";
import { useT } from "../../i18n";

export function SkillRevisionsTab({ workspaceId, skill, canEdit }: { workspaceId: string; skill: Skill; canEdit: boolean }) {
  const { t } = useT("skills");
  const list = useInfiniteQuery(skillRevisionListOptions(workspaceId, skill.id));
  const [selected, setSelected] = useState("");
  const [creating, setCreating] = useState(false);
  const detail = useQuery(skillRevisionDetailOptions(workspaceId, skill.id, selected));
  return (
    <section className="mx-auto max-w-[1440px] space-y-4 p-4 sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-2xl text-body text-muted-foreground">{t(($) => $.revisions.description)}</p>
        {canEdit && <Button variant="outline" onClick={() => setCreating(true)}>{t(($) => $.revisions.create)}</Button>}
      </div>
      {list.isPending && <p role="status">{t(($) => $.revisions.loading)}</p>}
      {list.isError && <div role="alert" className="space-y-2"><p>{t(($) => $.revisions.error)}</p><Button variant="outline" onClick={() => void list.refetch()}>{t(($) => $.revisions.retry)}</Button></div>}
      {list.isSuccess && list.data.pages.every((page) => page.revisions.length === 0) && <p className="rounded-lg border p-6 text-muted-foreground">{t(($) => $.revisions.empty)}</p>}
      <div className="divide-y rounded-lg border empty:hidden">
        {list.data?.pages.flatMap((page) => page.revisions).map((revision) => (
          <div key={revision.id} className="flex flex-wrap items-center justify-between gap-3 p-3">
            <div className="min-w-0 flex-1 space-y-1"><p className="break-all font-mono text-caption">{revision.revision_hash}</p><p className="text-caption text-muted-foreground">{revision.created_at}</p></div>
            <Button variant="outline" size="sm" onClick={() => setSelected(revision.id)}>{t(($) => $.revisions.inspect)}</Button>
          </div>
        ))}
      </div>
      {list.hasNextPage && <Button variant="outline" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{t(($) => $.revisions.more)}</Button>}
      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected("")}>
        <DialogContent className="w-[calc(100vw_-_2rem)] max-w-none sm:max-w-4xl">
          <DialogHeader><DialogTitle>{t(($) => $.revisions.inspect)}</DialogTitle><DialogDescription>{t(($) => $.revisions.description)}</DialogDescription></DialogHeader>
          <div className="max-h-[65vh] space-y-4 overflow-y-auto">
            {detail.isPending && <p role="status">{t(($) => $.revisions.loading)}</p>}
            {detail.isError && <div role="alert"><p>{t(($) => $.revisions.error)}</p><Button variant="outline" onClick={() => void detail.refetch()}>{t(($) => $.revisions.retry)}</Button></div>}
            {detail.data && <>
              <p className="break-all font-mono text-caption">{detail.data.revision_hash}</p>
              <label className="block space-y-2"><span>{t(($) => $.revisions.content)}</span><Textarea readOnly value={detail.data.bundle.content} className="min-h-64 font-mono text-caption" /></label>
              <details><summary className="cursor-pointer">{t(($) => $.revisions.bundle)}</summary><pre className="mt-2 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted p-3 text-caption">{JSON.stringify({ bundle: detail.data.bundle, manifest: detail.data.manifest }, null, 2)}</pre></details>
            </>}
          </div>
          <DialogFooter><Button variant="outline" onClick={() => setSelected("")}>{t(($) => $.revisions.close)}</Button></DialogFooter>
        </DialogContent>
      </Dialog>
      {creating && <CandidateDialog workspaceId={workspaceId} skill={skill} onClose={() => setCreating(false)} onCreated={(id) => { setCreating(false); setSelected(id); }} />}
    </section>
  );
}

function CandidateDialog({ workspaceId, skill, onClose, onCreated }: { workspaceId: string; skill: Skill; onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useT("skills");
  const mutation = useCreateSkillRevision();
  const submitting = useRef(false);
  // Freeze the saved baseline when opening; live refreshes must not replace a draft.
  const [baseline] = useState(skill.content ?? "");
  const [content, setContent] = useState(baseline);
  const [files, setFiles] = useState(() => (skill.files ?? []).map(({ path, content }) => ({ path, content })));
  const [contracts, setContracts] = useState("");
  const [tools, setTools] = useState<{ name: string; version: string }[]>([]);
  const [failed, setFailed] = useState(false);
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (submitting.current) return;
    submitting.current = true;
    setFailed(false);
    try {
      const revision = await mutation.mutateAsync({ workspaceId, skillId: skill.id, data: { content, files, compatible_contracts: contracts.split("\n").map((line) => line.trim()).filter(Boolean), tool_dependencies: tools } });
      onCreated(revision.id);
    } catch { setFailed(true); } finally { submitting.current = false; }
  };
  return <Dialog open onOpenChange={(open) => { if (!open && !submitting.current) onClose(); }}>
    <DialogContent className="w-[calc(100vw_-_2rem)] max-w-none sm:max-w-4xl" showCloseButton={!mutation.isPending}>
      <DialogHeader><DialogTitle>{t(($) => $.revisions.create)}</DialogTitle><DialogDescription>{t(($) => $.revisions.description)}</DialogDescription></DialogHeader>
      <form onSubmit={submit} className="min-w-0 space-y-4">
        <fieldset disabled={mutation.isPending} className="max-h-[60vh] min-w-0 space-y-5 overflow-y-auto pr-1">
          <div className="grid gap-3 md:grid-cols-2">
            <label className="min-w-0 space-y-2"><span>{t(($) => $.revisions.baseline)}</span><Textarea readOnly value={baseline} className="min-h-64 font-mono text-caption" /></label>
            <label className="min-w-0 space-y-2"><span>{t(($) => $.revisions.candidate)}</span><Textarea required value={content} onChange={(event) => setContent(event.target.value)} className="min-h-64 font-mono text-caption" /></label>
          </div>
          <div className="space-y-3"><h3 className="font-medium">{t(($) => $.revisions.files)}</h3>
            {files.map((file, index) => <div key={index} className="space-y-2 rounded-lg border p-3">
              <label className="block space-y-1"><span>{t(($) => $.revisions.path)}</span><Input required value={file.path} onChange={(event) => setFiles(files.map((entry, i) => i === index ? { ...entry, path: event.target.value } : entry))} /></label>
              <label className="block space-y-1"><span>{t(($) => $.revisions.file_content)}</span><Textarea value={file.content} onChange={(event) => setFiles(files.map((entry, i) => i === index ? { ...entry, content: event.target.value } : entry))} className="font-mono text-caption" /></label>
              <Button type="button" variant="ghost" onClick={() => setFiles(files.filter((_, i) => i !== index))}>{t(($) => $.revisions.remove)}</Button>
            </div>)}
            <Button type="button" variant="outline" onClick={() => setFiles([...files, { path: "", content: "" }])}>{t(($) => $.revisions.add_file)}</Button>
          </div>
          <label className="block space-y-2"><span>{t(($) => $.revisions.contracts)}</span><Textarea value={contracts} onChange={(event) => setContracts(event.target.value)} /><span className="block text-caption text-muted-foreground">{t(($) => $.revisions.contracts_hint)}</span></label>
          <div className="space-y-3"><h3 className="font-medium">{t(($) => $.revisions.tools)}</h3>
            {tools.map((tool, index) => <div key={index} className="grid items-end gap-2 rounded-lg border p-3 sm:grid-cols-[1fr_1fr_auto]">
              <label className="space-y-1"><span>{t(($) => $.revisions.tool_name)}</span><Input required value={tool.name} onChange={(event) => setTools(tools.map((entry, i) => i === index ? { ...entry, name: event.target.value } : entry))} /></label>
              <label className="space-y-1"><span>{t(($) => $.revisions.tool_version)}</span><Input required value={tool.version} onChange={(event) => setTools(tools.map((entry, i) => i === index ? { ...entry, version: event.target.value } : entry))} /></label>
              <Button type="button" variant="ghost" onClick={() => setTools(tools.filter((_, i) => i !== index))}>{t(($) => $.revisions.remove)}</Button>
            </div>)}
            <Button type="button" variant="outline" onClick={() => setTools([...tools, { name: "", version: "" }])}>{t(($) => $.revisions.add_tool)}</Button>
          </div>
        </fieldset>
        {failed && <p role="alert" className="text-destructive">{t(($) => $.revisions.create_error)}</p>}
        <DialogFooter><Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>{t(($) => $.revisions.cancel)}</Button><Button type="submit" disabled={mutation.isPending || !content.trim()}>{mutation.isPending ? t(($) => $.revisions.saving) : t(($) => $.revisions.save)}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>;
}
