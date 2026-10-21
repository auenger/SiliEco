"use client";

import { useRef, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { ApiError, type CreateSkillImprovementProposalInput, type SkillProposalCategory } from "@silieco/core/api";
import { skillProposalListOptions, skillProposalDetailOptions, skillRevisionListOptions, skillRevisionDetailOptions, useCreateSkillImprovementProposal } from "@silieco/core/skills";
import { Button } from "@silieco/ui/components/ui/button";
import { Input } from "@silieco/ui/components/ui/input";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@silieco/ui/components/ui/select";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@silieco/ui/components/ui/dialog";
import { useT } from "../../i18n";

const dialogClass = "w-[calc(100vw_-_2rem)] max-w-none sm:max-w-4xl";
const categories: SkillProposalCategory[] = ["skill_instruction", "insufficient_input", "tool_failure", "contract_issue", "model_capability", "human_correction", "positive_example"];
function useCategoryLabel() {
  const { t } = useT("skills");
  return (category: string) => categories.includes(category as SkillProposalCategory) ? t(($) => $.proposals.categories[category as SkillProposalCategory]) : category;
}

function ProposalSelect({ label, value, options, onChange, disabled }: { label: string; value: string; options: {value: string; label: string}[]; onChange: (value: string) => void; disabled?: boolean }) {
  const { t } = useT("skills");
  const [open, setOpen] = useState(false);
  return <div className="min-w-0 space-y-2"><span className="text-body font-medium">{label}</span>
    <Select items={options} value={value || null} open={open} onOpenChange={setOpen} disabled={disabled} onValueChange={(next) => { if (next) onChange(next); setOpen(false); }}>
      <SelectTrigger aria-label={label} className="w-full"><SelectValue placeholder={t(($) => $.proposals.choose)}>{options.find((option) => option.value === value)?.label}</SelectValue></SelectTrigger>
      {open && <SelectContent alignItemWithTrigger={false}>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>}
    </Select>
  </div>;
}

function RevisionComparison({ workspaceId, skillId, baselineId, candidateId }: { workspaceId: string; skillId: string; baselineId: string; candidateId: string }) {
  const { t } = useT("skills");
  const baseline = useQuery(skillRevisionDetailOptions(workspaceId, skillId, baselineId));
  const candidate = useQuery(skillRevisionDetailOptions(workspaceId, skillId, candidateId));
  if (!baselineId || !candidateId) return null;
  return <div className="space-y-2"><h3 className="font-medium">{t(($) => $.proposals.compare)}</h3>
    <div className="grid gap-3 md:grid-cols-2">{[{query: baseline, label: t(($) => $.proposals.baseline)}, {query: candidate, label: t(($) => $.proposals.candidate)}].map(({query, label}) => <div key={label} className="min-w-0 space-y-2">
      <p className="text-caption font-medium">{label}</p>
      {query.isPending && <p role="status">{t(($) => $.revisions.loading)}</p>}
      {query.isError && <div role="alert"><p>{t(($) => $.revisions.error)}</p><Button type="button" variant="outline" onClick={() => void query.refetch()}>{t(($) => $.revisions.retry)}</Button></div>}
      {query.data && <><p className="break-all font-mono text-caption text-muted-foreground">{query.data.revision_hash}</p><pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg border p-3 text-caption">{query.data.bundle.content}</pre><details><summary className="cursor-pointer text-caption">{t(($) => $.revisions.bundle)}</summary><pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted p-3 text-caption">{JSON.stringify({bundle: query.data.bundle, manifest: query.data.manifest}, null, 2)}</pre></details></>}
    </div>)}</div>
  </div>;
}

export function SkillProposalsTab({ workspaceId, skillId, canEdit }: { workspaceId: string; skillId: string; canEdit: boolean }) {
  const { t } = useT("skills");
  const categoryLabel = useCategoryLabel();
  const list = useInfiniteQuery(skillProposalListOptions(workspaceId, skillId));
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState("");
  const detail = useQuery(skillProposalDetailOptions(workspaceId, skillId, selected));
  return <section className="mx-auto max-w-[1440px] space-y-4 p-4 sm:p-6">
    <div className="flex flex-wrap items-start justify-between gap-3"><p className="max-w-2xl text-muted-foreground">{t(($) => $.proposals.description)}</p>{canEdit && <Button variant="outline" onClick={() => setCreating(true)}>{t(($) => $.proposals.create)}</Button>}</div>
    {list.isPending && <p role="status">{t(($) => $.proposals.loading)}</p>}
    {list.isError && <div role="alert"><p>{t(($) => $.proposals.error)}</p><Button variant="outline" onClick={() => void list.refetch()}>{t(($) => $.revisions.retry)}</Button></div>}
    {list.isSuccess && list.data.pages.every((page) => page.proposals.length === 0) && <p className="rounded-lg border p-6 text-muted-foreground">{t(($) => $.proposals.empty)}</p>}
    <div className="divide-y rounded-lg border empty:hidden">{list.data?.pages.flatMap((page) => page.proposals).map((proposal) => <div key={proposal.id} className="flex flex-wrap items-center justify-between gap-3 p-3">
      <div className="min-w-0 flex-1 space-y-1"><p className="line-clamp-2 break-words">{proposal.reason}</p><p className="text-caption text-muted-foreground">{categoryLabel(proposal.category)} · {proposal.created_at}</p></div>
      <Button variant="outline" size="sm" onClick={() => setSelected(proposal.id)}>{t(($) => $.proposals.inspect)}</Button>
    </div>)}</div>
    {list.hasNextPage && <Button variant="outline" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{t(($) => $.revisions.more)}</Button>}
    <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected("")}><DialogContent className={dialogClass}>
      <DialogHeader><DialogTitle>{t(($) => $.proposals.inspect)}</DialogTitle><DialogDescription>{t(($) => $.proposals.description)}</DialogDescription></DialogHeader>
      <div className="max-h-[65vh] space-y-4 overflow-y-auto">
        {detail.isPending && <p role="status">{t(($) => $.proposals.loading)}</p>}
        {detail.isError && <div role="alert"><p>{t(($) => $.proposals.error)}</p><Button variant="outline" onClick={() => void detail.refetch()}>{t(($) => $.revisions.retry)}</Button></div>}
        {detail.data && <>
          <p className="text-caption text-muted-foreground">{categoryLabel(detail.data.category)} · {detail.data.created_at}</p>
          {[{label: t(($) => $.proposals.reason), value: detail.data.reason}, {label: t(($) => $.proposals.hypothesis), value: detail.data.hypothesis}, {label: t(($) => $.proposals.scope), value: detail.data.applicable_scope}].map(({label,value}) => <div key={label} className="space-y-1"><h3 className="font-medium">{label}</h3><p className="whitespace-pre-wrap break-words">{value}</p></div>)}
          <div className="rounded-lg border p-3 text-caption">{detail.data.evidence.execution_id ? <dl className="space-y-2"><dt>{t(($) => $.proposals.execution)}</dt><dd className="break-all font-mono">{detail.data.evidence.execution_id}</dd>{detail.data.evidence.result_revision !== undefined && <><dt>{t(($) => $.proposals.result)}</dt><dd>{detail.data.evidence.result_revision}</dd><dd className="break-all font-mono">{detail.data.evidence.result_hash}</dd></>}</dl> : t(($) => $.proposals.manual)}</div>
          <RevisionComparison workspaceId={workspaceId} skillId={skillId} baselineId={detail.data.baseline_revision_id} candidateId={detail.data.candidate_revision_id} />
        </>}
      </div>
      <DialogFooter><Button variant="outline" onClick={() => setSelected("")}>{t(($) => $.revisions.close)}</Button></DialogFooter>
    </DialogContent></Dialog>
    {creating && <SkillProposalDialog workspaceId={workspaceId} skillId={skillId} onClose={() => setCreating(false)} onCreated={(id) => { setCreating(false); setSelected(id); }} />}
  </section>;
}

export function SkillProposalDialog({ workspaceId, skillId, onClose, onCreated }: { workspaceId: string; skillId: string; onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useT("skills");
  const categoryLabel = useCategoryLabel();
  const revisions = useInfiniteQuery(skillRevisionListOptions(workspaceId, skillId));
  const mutation = useCreateSkillImprovementProposal();
  const [baseline, setBaseline] = useState(""); const [candidate, setCandidate] = useState("");
  const [category, setCategory] = useState<SkillProposalCategory>("human_correction");
  const [reason, setReason] = useState(""); const [hypothesis, setHypothesis] = useState(""); const [scope, setScope] = useState("");
  const [execution, setExecution] = useState(""); const [resultRevision, setResultRevision] = useState("");
  const [failure, setFailure] = useState<"uncertain" | "rejected" | null>(null);
  const [requestId] = useState(() => crypto.randomUUID());
  const submitted = useRef<CreateSkillImprovementProposalInput | null>(null);
  const inFlight = useRef(false);
  const locked = mutation.isPending || failure === "uncertain";
  const options = (revisions.data?.pages.flatMap((page) => page.revisions) ?? []).map((revision) => ({value: revision.id, label: `${revision.revision_hash.slice(7, 23)} · ${revision.created_at}`}));
  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); if (inFlight.current || !baseline || !candidate || baseline === candidate) return;
    inFlight.current = true;
    const data = submitted.current ?? {request_id: requestId, baseline_revision_id: baseline, candidate_revision_id: candidate, category, reason, hypothesis, applicable_scope: scope, ...(execution ? {source_execution_id: execution, ...(resultRevision ? {source_result_revision: Number(resultRevision)} : {})} : {})};
    submitted.current = data;
    try { const proposal = await mutation.mutateAsync({workspaceId, skillId, data}); onCreated(proposal.id); }
    catch (error) {
      if (error instanceof ApiError && [400, 403, 404].includes(error.status)) { submitted.current = null; setFailure("rejected"); }
      else setFailure("uncertain");
    } finally { inFlight.current = false; }
  };
  return <Dialog open onOpenChange={(open) => { if (!open && !inFlight.current) onClose(); }}><DialogContent className={dialogClass} showCloseButton={!mutation.isPending}>
    <DialogHeader><DialogTitle>{t(($) => $.proposals.create)}</DialogTitle><DialogDescription>{t(($) => $.proposals.description)}</DialogDescription></DialogHeader>
    <form onSubmit={submit} className="min-w-0 space-y-4">
      <fieldset disabled={locked} className="max-h-[60vh] min-w-0 space-y-4 overflow-y-auto pr-1">
        {revisions.isPending && <p role="status">{t(($) => $.revisions.loading)}</p>}
        {revisions.isError && <div role="alert"><p>{t(($) => $.revisions.error)}</p><Button type="button" variant="outline" onClick={() => void revisions.refetch()}>{t(($) => $.revisions.retry)}</Button></div>}
        {revisions.isSuccess && options.length < 2 && <p>{t(($) => $.proposals.need_revisions)}</p>}
        <div className="grid gap-3 sm:grid-cols-2"><ProposalSelect label={t(($) => $.proposals.baseline)} value={baseline} options={options.filter((option) => option.value !== candidate)} onChange={setBaseline} disabled={locked} /><ProposalSelect label={t(($) => $.proposals.candidate)} value={candidate} options={options.filter((option) => option.value !== baseline)} onChange={setCandidate} disabled={locked} /></div>
        {revisions.hasNextPage && <Button type="button" variant="outline" disabled={revisions.isFetchingNextPage} onClick={() => void revisions.fetchNextPage()}>{t(($) => $.revisions.more)}</Button>}
        <ProposalSelect label={t(($) => $.proposals.category)} value={category} options={categories.map((value) => ({value, label: categoryLabel(value)}))} onChange={(value) => setCategory(value as SkillProposalCategory)} disabled={locked} />
        {[{label: t(($) => $.proposals.reason), value: reason, change: setReason, limit: 8000}, {label: t(($) => $.proposals.hypothesis), value: hypothesis, change: setHypothesis, limit: 8000}, {label: t(($) => $.proposals.scope), value: scope, change: setScope, limit: 4000}].map(({label,value,change,limit}) => <label key={label} className="block space-y-2"><span>{label}</span><Textarea required maxLength={limit} value={value} onChange={(event) => change(event.target.value)} /></label>)}
        <details className="rounded-lg border p-3"><summary className="cursor-pointer font-medium">{t(($) => $.proposals.evidence)}</summary><div className="mt-3 space-y-3"><p className="text-caption text-muted-foreground">{t(($) => $.proposals.evidence_hint)}</p><label className="block space-y-1"><span>{t(($) => $.proposals.execution)}</span><Input value={execution} pattern="[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}" onChange={(event) => { setExecution(event.target.value); if (!event.target.value) setResultRevision(""); }} /></label><label className="block space-y-1"><span>{t(($) => $.proposals.result)}</span><Input type="number" min={1} max={2147483647} step={1} disabled={!execution} value={resultRevision} onChange={(event) => setResultRevision(event.target.value)} /></label></div></details>
        <RevisionComparison workspaceId={workspaceId} skillId={skillId} baselineId={baseline} candidateId={candidate} />
      </fieldset>
      {failure && <p role="alert" className="text-destructive">{failure === "uncertain" ? t(($) => $.proposals.uncertain) : t(($) => $.proposals.rejected)}</p>}
      <DialogFooter><Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>{t(($) => $.revisions.cancel)}</Button><Button type="submit" disabled={mutation.isPending || !baseline || !candidate || baseline === candidate || !reason.trim() || !hypothesis.trim() || !scope.trim()}>{mutation.isPending ? t(($) => $.proposals.submitting) : failure === "uncertain" ? t(($) => $.proposals.retry_submission) : t(($) => $.proposals.submit)}</Button></DialogFooter>
    </form>
  </DialogContent></Dialog>;
}
