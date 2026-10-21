"use client";

import { StageRetryFields, validRetryPolicyDraft } from "./stage-retry-fields";
export { validRetryPolicyDraft };

import { StageContractFields, type StageContractDraft } from "./stage-contract-fields";
import { useId, useState } from "react";
import type { Agent, WorkflowObject } from "@silieco/core/types";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@silieco/ui/components/ui/select";
import { Input } from "@silieco/ui/components/ui/input";
import { useT } from "../../i18n";

type ExecutionDraft = StageContractDraft & {retryPolicy?: WorkflowObject;reworkPolicy?: WorkflowObject;rollbackStageKey?: string | null; executionMode?: string; executorId?: string | null; outputSpec?: WorkflowObject};
type ExecutionPatch = StageContractDraft & {retryPolicy?: WorkflowObject;reworkPolicy?: WorkflowObject;rollbackStageKey?: string | null; executionMode?: string; executorId?: string | null; completionRule?: WorkflowObject; outputSpec?: WorkflowObject};

export function validReworkPolicyDraft(policy?: WorkflowObject) {
 const max=policy?.max_reworks;
 return max === undefined || (typeof max === "number" && Number.isInteger(max) && max >= 0 && max <= 100);
}

export function StageExecutionFields({value, agents, disabled = false, precedingStages = [], rollbackStages = precedingStages, onChange}: {value: ExecutionDraft; agents: readonly Agent[]; disabled?: boolean; precedingStages?: readonly {stableKey: string; name: string}[]; rollbackStages?: readonly {stableKey: string; name: string}[]; onChange: (patch: ExecutionPatch) => void}) {
  const {t} = useT("workflows");
  const id = useId();
  const [executorOpen,setExecutorOpen]=useState(false);
  const [rollbackOpen,setRollbackOpen]=useState(false);
  const mode = value.executionMode ?? "manual";
  const available = agents.filter((agent) => !agent.archived_at);
  const rollbackUnavailable = !!value.rollbackStageKey && !rollbackStages.some((stage) => stage.stableKey === value.rollbackStageKey);
  const executorAvailable = available.some((agent) => agent.id === value.executorId);
  const selectMode = (executionMode: "manual" | "agent") => {
    if (disabled || executionMode === mode) return;
    if (executionMode === "manual") {onChange({executionMode, executorId:null, completionRule:{type:"all_tasks_terminal"}});return;}
    onChange({executionMode, completionRule:{type:"validated_result"}, outputSpec: value.outputSpec?.contract
      ? value.outputSpec : {...value.outputSpec, contract:{schema_version:"silieco.stage/v1",data:{type:"object"}}}});
  };
  return <div className="space-y-3 rounded-lg border border-border bg-muted/20 p-3">
    <fieldset disabled={disabled}>
      <legend className="mb-2 text-caption font-medium">{t(($) => $.execution_config.mode)}</legend>
      <div className="grid gap-2 sm:grid-cols-2">
        {(["manual", "agent"] as const).map((item) => <label key={item} className={`flex cursor-pointer items-start gap-2 rounded-md border p-3 ${mode === item ? "border-primary/40 bg-background" : "border-border"}`}>
          <input className="mt-0.5 accent-primary" type="radio" name={`execution-${id}`} checked={mode === item} onChange={() => selectMode(item)} />
          <span className="min-w-0"><span className="block text-caption font-medium">{item === "manual" ? t(($) => $.execution_config.manual) : t(($) => $.execution_config.agent)}</span><span className="text-micro text-muted-foreground">{item === "manual" ? t(($) => $.execution_config.manual_hint) : t(($) => $.execution_config.agent_hint)}</span></span>
        </label>)}
      </div>
    </fieldset>
    {mode === "agent" && <div className="space-y-1.5">
      <label id={`${id}-label`} className="text-caption font-medium">{t(($) => $.execution_config.executor)}</label>
      <Select open={executorOpen} onOpenChange={setExecutorOpen} items={available.map((agent) => ({value:agent.id,label:agent.name}))} value={executorAvailable ? value.executorId : null} disabled={disabled} onValueChange={(executorId) => {if (!disabled) {onChange({executorId});setExecutorOpen(false);}}}>
        <SelectTrigger className="w-full" aria-labelledby={`${id}-label`}><SelectValue placeholder={t(($) => $.execution_config.select_executor)} /></SelectTrigger>
        {executorOpen && <SelectContent alignItemWithTrigger={false}>{available.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.name}</SelectItem>)}</SelectContent>}
      </Select>
      {!executorAvailable && <p role="status" className="text-micro text-muted-foreground">{t(($) => $.execution_config.executor_required)}</p>}
      <p className="text-micro text-muted-foreground">{t(($) => $.execution_config.review_independent)}</p>

      <p className="text-micro text-muted-foreground">{t(($) => $.execution_config.completion)}</p>
      <StageContractFields value={value} precedingStages={precedingStages} disabled={disabled} onChange={onChange} />
    </div>}
      <StageRetryFields value={value.retryPolicy} disabled={disabled} onChange={(retryPolicy) => onChange({retryPolicy})} />
      <label className="block space-y-1.5 text-caption">
        <span>{t(($) => $.execution_config.rework_limit)}</span>
        <Input aria-label={t(($) => $.execution_config.rework_limit)} aria-describedby={`${id}-rework-hint`} type="number" min={0} max={100} step={1} value={typeof value.reworkPolicy?.max_reworks === "number" ? value.reworkPolicy.max_reworks : ""} disabled={disabled} placeholder={t(($) => $.execution_config.rework_unlimited)} onChange={(event) => onChange({reworkPolicy:event.target.value === "" ? {} : {max_reworks:Number(event.target.value)}})} />
        <span id={`${id}-rework-hint`} className="block text-micro text-muted-foreground">{t(($) => $.execution_config.rework_limit_hint)}</span>
        {!validReworkPolicyDraft(value.reworkPolicy) && <span role="alert" className="block text-micro text-destructive">{t(($) => $.execution_config.rework_limit_invalid)}</span>}
      </label>
      <div className="space-y-1.5">
        <label id={`${id}-rollback`} className="text-caption font-medium">{t(($) => $.execution_config.rollback)}</label>
        <Select open={rollbackOpen} onOpenChange={setRollbackOpen} items={[{value:"none",label:t(($) => $.execution_config.no_rollback)}, ...rollbackStages.map((stage) => ({value:`stage:${stage.stableKey}`,label:stage.name}))]} value={!value.rollbackStageKey ? "none" : rollbackStages.some((stage) => stage.stableKey === value.rollbackStageKey) ? `stage:${value.rollbackStageKey}` : null} disabled={disabled} onValueChange={(key) => {if (!disabled && key) {onChange({rollbackStageKey:key === "none" ? null : key.slice(6)});setRollbackOpen(false);}}}>
          <SelectTrigger className="w-full" aria-labelledby={`${id}-rollback`}><SelectValue placeholder={t(($) => $.execution_config.invalid_rollback)} /></SelectTrigger>
          {rollbackOpen && <SelectContent alignItemWithTrigger={false}><SelectItem value="none">{t(($) => $.execution_config.no_rollback)}</SelectItem>{rollbackStages.map((stage) => <SelectItem key={stage.stableKey} value={`stage:${stage.stableKey}`}>{stage.name}</SelectItem>)}</SelectContent>}
        </Select>
        <p className="text-micro text-muted-foreground">{t(($) => $.execution_config.rollback_hint)}</p>
        {rollbackUnavailable && <p role="alert" className="text-micro text-destructive">{t(($) => $.execution_config.invalid_rollback)}</p>}
      </div>
  </div>;
}
