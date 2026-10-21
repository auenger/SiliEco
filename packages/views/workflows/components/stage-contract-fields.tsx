"use client";

import { useId, useState } from "react";
import type { WorkflowObject } from "@silieco/core/types";
import { Button } from "@silieco/ui/components/ui/button";
import { Checkbox } from "@silieco/ui/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@silieco/ui/components/ui/dialog";
import { Input } from "@silieco/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@silieco/ui/components/ui/select";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { validStageContractDraft } from "./stage-contract-validation";
import { workflowConfigurationDialogWidth } from "./workflow-dialog";

export type StageContractDraft = {
  inputSpec?: WorkflowObject;
  inputBindings?: WorkflowObject;
  outputSpec?: WorkflowObject;
};
type SourceStage = { stableKey: string; name: string };
const starter = { schema_version: "silieco.stage/v1", data: { type: "object" } };
const fieldTypes = ["string", "number", "integer", "boolean", "object", "array"] as const;

function object(value: unknown): WorkflowObject | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as WorkflowObject : null;
}
function parse(text: string): WorkflowObject | null {
  try { return object(JSON.parse(text)); } catch { return null; }
}
function format(value: unknown) { return JSON.stringify(value, null, 2); }
function contract(text: string): WorkflowObject | null {
  const value = parse(text);
  return value?.schema_version === "silieco.stage/v1" && object(value.data)?.type === "object" ? value : null;
}
function withContract(spec: WorkflowObject | undefined, value: WorkflowObject | null): WorkflowObject {
  const next = { ...spec };
  if (value) next.contract = value;
  else delete next.contract;
  return next;
}

export function StageContractFields({ value, precedingStages, disabled = false, onChange }: {
  value: StageContractDraft;
  precedingStages: readonly SourceStage[];
  disabled?: boolean;
  onChange: (patch: StageContractDraft) => void;
}) {
  const { t } = useT("workflows");
  const [open, setOpen] = useState(false);
  return <>
    <Button type="button" size="sm" variant="outline" disabled={disabled} onClick={() => setOpen(true)}>{t(($) => $.contract_config.open)}</Button>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className={`flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 ${workflowConfigurationDialogWidth}`}>
        <DialogHeader className="border-b p-4 pr-12">
          <DialogTitle>{t(($) => $.contract_config.open)}</DialogTitle>
          <DialogDescription>{t(($) => $.contract_config.description)}</DialogDescription>
        </DialogHeader>
        {open && <ContractForm key={JSON.stringify(value)} value={value} precedingStages={precedingStages} disabled={disabled} onApply={(patch) => { if (!disabled) { onChange(patch); setOpen(false); } }} onClose={() => setOpen(false)} />}
      </DialogContent>
    </Dialog>
  </>;
}

function ContractForm({ value, precedingStages, disabled, onApply, onClose }: {
  value: StageContractDraft; precedingStages: readonly SourceStage[]; disabled: boolean;
  onApply: (patch: StageContractDraft) => void; onClose: () => void;
}) {
  const { t } = useT("workflows");
  const [inputEnabled, setInputEnabled] = useState(value.inputSpec?.contract !== undefined);
  const [input, setInput] = useState(() => format(value.inputSpec?.contract ?? starter));
  const [output, setOutput] = useState(() => format(value.outputSpec?.contract ?? starter));
  const [bindings, setBindings] = useState(() => format(value.inputBindings ?? {}));
  const parsedInput = inputEnabled ? contract(input) : null;
  const parsedOutput = contract(output);
  const parsedBindings = parse(bindings);
  const bindingRows = Array.isArray(parsedBindings?.data) ? parsedBindings.data : [];
  const inputProperties = object(object(parsedInput?.data)?.properties) ?? {};
  const bindingTargets = bindingRows.map((row: unknown) => object(row)?.target);
  const validBindings = parsedBindings !== null && (Object.keys(parsedBindings).length === 0 || (
    inputEnabled && parsedBindings.schema_version === "silieco.input-bindings/v1" && Array.isArray(parsedBindings.data) && bindingRows.length <= 128 &&
    new Set(bindingTargets).size === bindingTargets.length && bindingRows.every((row: unknown) => {
      const binding = object(row); const source = object(binding?.source);
      return typeof binding?.target === "string" && binding.target.length > 0 && new TextEncoder().encode(binding.target).length <= 128 && Object.hasOwn(inputProperties, binding.target) &&
        typeof source?.pointer === "string" && new TextEncoder().encode(source.pointer).length <= 4096 && source.pointer.split("/").length <= 33 && (source.pointer === "" || source.pointer.startsWith("/")) && !/~(?![01])/u.test(source.pointer) &&
        (source.kind === "run_input" && source.stage_key === undefined || source.kind === "stage_result" && precedingStages.some((stage) => stage.stableKey === source.stage_key));
    })
  ));
  const valid = (!inputEnabled || validStageContractDraft(parsedInput, false)) && validStageContractDraft(parsedOutput, true) && validBindings;
  const apply = () => {
    if (disabled || !valid || !parsedOutput || !parsedBindings) return;
    onApply({ inputSpec: withContract(value.inputSpec, parsedInput), outputSpec: withContract(value.outputSpec, parsedOutput), inputBindings: parsedBindings });
  };
  const changeBinding = (index: number, patch: WorkflowObject) => setBindings(format({ ...parsedBindings, data: bindingRows.map((row: unknown, i: number) => i === index ? { ...object(row), ...patch } : row) }));
  return <>
    <div data-slot="contract-scroll" className="min-h-0 overflow-y-auto">
    <fieldset disabled={disabled} className="min-w-0 space-y-5 p-4">
      <section className="space-y-3 rounded-lg border p-3">
        <label className="flex items-center gap-2 text-caption font-medium"><Checkbox checked={inputEnabled} onCheckedChange={(checked) => { if (!disabled) setInputEnabled(checked === true); }} />{t(($) => $.contract_config.input)}</label>
        {inputEnabled && <ContractEditor text={input} onChange={setInput} artifacts={false} />}
      </section>
      <section className="space-y-3 rounded-lg border p-3">
        <h3 className="text-caption font-medium">{t(($) => $.contract_config.bindings)}</h3>
        <p className="text-micro text-muted-foreground">{t(($) => $.contract_config.bindings_hint)}</p>
        {bindingRows.map((row: unknown, index: number) => {
          const binding = object(row); const source = object(binding?.source) ?? {};
          const choices = [{ value: "run_input", label: t(($) => $.contract_config.run_input) }, ...precedingStages.map((stage) => ({ value: `stage:${stage.stableKey}`, label: stage.name }))];
          const selected = source.kind === "run_input" ? "run_input" : `stage:${source.stage_key}`;
          return <div key={index} className="grid gap-2 rounded-md bg-muted/20 p-3 sm:grid-cols-2">
            <Choice label={t(($) => $.contract_config.target)} value={typeof binding?.target === "string" ? binding.target : null} items={Object.keys(inputProperties).map((key) => ({ value: key, label: key }))} onChange={(target) => changeBinding(index, { target })} />
            <Choice label={t(($) => $.contract_config.source)} value={choices.some((choice) => choice.value === selected) ? selected : null} items={choices} onChange={(selected) => { if (selected) changeBinding(index, { source: { kind: selected === "run_input" ? "run_input" : "stage_result", ...(selected === "run_input" ? {} : { stage_key: selected.slice(6) }), pointer: source.pointer ?? "" } }); }} />
            <label className="space-y-1 text-caption"><span>{t(($) => $.contract_config.pointer)}</span><Input value={typeof source.pointer === "string" ? source.pointer : ""} placeholder="/summary" onChange={(event) => changeBinding(index, { source: { ...source, pointer: event.target.value } })} /></label>
            <div className="flex items-end justify-end"><Button type="button" variant="ghost" size="sm" onClick={() => setBindings(format({ ...parsedBindings, data: bindingRows.filter((_: unknown, i: number) => i !== index) }))}>{t(($) => $.contract_config.remove)}</Button></div>
          </div>;
        })}
        <Button type="button" size="sm" variant="outline" disabled={!parsedInput || !Object.keys(inputProperties).some((key) => !bindingTargets.includes(key))} onClick={() => setBindings(format({ schema_version: "silieco.input-bindings/v1", data: [...bindingRows, { target: Object.keys(inputProperties).find((key) => !bindingTargets.includes(key)), source: { kind: "run_input", pointer: "" } }] }))}>{t(($) => $.contract_config.add_binding)}</Button>
        <AdvancedJSON text={bindings} onChange={setBindings} />
      </section>
      <section className="space-y-3 rounded-lg border p-3">
        <h3 className="text-caption font-medium">{t(($) => $.contract_config.output)}</h3>
        <ContractEditor text={output} onChange={setOutput} artifacts />
      </section>
      {!valid && <p role="alert" className="text-caption text-destructive">{t(($) => $.contract_config.invalid)}</p>}
    </fieldset>
    </div>
    <div className="flex shrink-0 justify-end gap-2 border-t bg-surface-raised p-4">
      <Button type="button" variant="outline" onClick={onClose}>{t(($) => $.contract_config.cancel)}</Button>
      <Button type="button" disabled={disabled || !valid} onClick={apply}>{t(($) => $.contract_config.apply)}</Button>
    </div>
  </>;
}

function Choice({ label, value, items, onChange }: { label: string; value: string | null; items: { value: string; label: string }[]; onChange: (value: string | null) => void }) {
  const id = useId();
  return <div className="space-y-1"><label id={id} className="text-caption">{label}</label><Select items={items} value={value} onValueChange={onChange}><SelectTrigger className="w-full" aria-labelledby={id}><SelectValue /></SelectTrigger><SelectContent>{items.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent></Select></div>;
}
function AdvancedJSON({ text, onChange }: { text: string; onChange: (text: string) => void }) {
  const { t } = useT("workflows");
  return <details className="rounded-md border p-3"><summary className="cursor-pointer text-caption text-muted-foreground">{t(($) => $.contract_config.advanced)}</summary><label className="mt-3 block space-y-2 text-caption"><span>{t(($) => $.contract_config.json)}</span><Textarea className="min-h-40 font-mono" value={text} onChange={(event) => onChange(event.target.value)} /></label></details>;
}

function ContractEditor({ text, onChange, artifacts }: { text: string; onChange: (text: string) => void; artifacts: boolean }) {
  const { t } = useT("workflows");
  const [newField, setNewField] = useState("");
  const value = contract(text);
  const data = object(value?.data) ?? {};
  const properties = object(data.properties) ?? {};
  const required = Array.isArray(data.required) ? data.required : [];
  const artifactRows = Array.isArray(value?.artifacts) ? value.artifacts : [];
  const updateData = (patch: WorkflowObject) => onChange(format({ ...value, data: { ...data, ...patch } }));
  return <div className="space-y-3">
    {Object.entries(properties).map(([name, schema]) => <div key={name} className="grid items-center gap-2 rounded-md bg-muted/20 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto]">
      <span className="break-all font-mono text-caption">{name}</span>
      <Choice label={t(($) => $.contract_config.type)} value={typeof object(schema)?.type === "string" ? object(schema)!.type as string : null} items={fieldTypes.map((type) => ({ value: type, label: type }))} onChange={(type) => { if (type) updateData({ properties: { ...properties, [name]: { type, ...(type === "array" ? { items: { type: "string" } } : {}) } } }); }} />
      <label className="flex items-center gap-2 text-caption"><Checkbox checked={required.includes(name)} onCheckedChange={(checked) => updateData({ required: checked === true ? [...required.filter((item) => item !== name), name] : required.filter((item) => item !== name) })} />{t(($) => $.contract_config.required)}</label>
      <Button type="button" size="sm" variant="ghost" onClick={() => { const next = { ...properties }; delete next[name]; updateData({ properties: next, required: required.filter((item) => item !== name) }); }}>{t(($) => $.contract_config.remove)}</Button>
    </div>)}
    <div className="flex items-end gap-2">
      <label className="min-w-0 flex-1 space-y-1 text-caption"><span>{t(($) => $.contract_config.field_name)}</span><Input value={newField} onChange={(event) => setNewField(event.target.value)} /></label>
      <Button type="button" size="sm" variant="outline" disabled={!value || !newField.trim() || Object.hasOwn(properties, newField.trim())} onClick={() => { if (newField.trim() && !Object.hasOwn(properties, newField.trim())) { updateData({ properties: { ...properties, [newField.trim()]: { type: "string" } } }); setNewField(""); } }}>{t(($) => $.contract_config.add_field)}</Button>
    </div>
    {artifacts && <div className="space-y-3">
      <h4 className="text-caption font-medium">{t(($) => $.contract_config.artifacts)}</h4>
      {artifactRows.map((row: unknown, index: number) => <div key={index} className="grid gap-2 rounded-md bg-muted/20 p-3 sm:grid-cols-2">
        {(["name", "kind", "media_type", "schema_version"] as const).map((key) => <label key={key} className="space-y-1 text-caption"><span>{t(($) => $.contract_config[key])}</span><Input value={typeof object(row)?.[key] === "string" ? object(row)![key] as string : ""} onChange={(event) => onChange(format({ ...value, artifacts: artifactRows.map((item: unknown, i: number) => i === index ? { ...object(item), [key]: event.target.value } : item) }))} /></label>)}
        <label className="flex items-center gap-2 text-caption"><Checkbox checked={object(row)?.required === true} onCheckedChange={(checked) => onChange(format({ ...value, artifacts: artifactRows.map((item: unknown, i: number) => i === index ? { ...object(item), required: checked === true } : item) }))} />{t(($) => $.contract_config.required)}</label>
        <div className="flex justify-end"><Button type="button" size="sm" variant="ghost" onClick={() => onChange(format({ ...value, artifacts: artifactRows.filter((_: unknown, i: number) => i !== index) }))}>{t(($) => $.contract_config.remove)}</Button></div>
      </div>)}
      <Button type="button" variant="outline" size="sm" disabled={!value} onClick={() => onChange(format({ ...value, artifacts: [...artifactRows, { name: "", kind: "file", media_type: "text/plain", schema_version: "v1", required: true }] }))}>{t(($) => $.contract_config.add_artifact)}</Button>
    </div>}
    <AdvancedJSON text={text} onChange={onChange} />
  </div>;
}
