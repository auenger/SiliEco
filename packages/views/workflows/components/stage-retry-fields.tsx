"use client";

import {useId} from "react";
import type {WorkflowObject} from "@silieco/core/types";
import {Input} from "@silieco/ui/components/ui/input";
import {useT} from "../../i18n";

const fields = [
 {key:"max_attempts",min:1,max:10},
 {key:"timeout_seconds",min:1,max:86400},
 {key:"backoff_seconds",min:0,max:3600},
 {key:"backoff_multiplier",min:1,max:10},
 {key:"max_backoff_seconds",min:0,max:3600},
] as const;

export function validRetryPolicyDraft(policy?: WorkflowObject) {
 for(const {key,min,max} of fields) {
  const value=policy?.[key];
  if(value!==undefined && (typeof value!=="number" || !Number.isInteger(value) || value<min || value>max)) return false;
 }
 if(policy?.backoff_seconds===undefined && (policy?.backoff_multiplier!==undefined || policy?.max_backoff_seconds!==undefined)) return false;
 return !(typeof policy?.backoff_seconds==="number" && typeof policy?.max_backoff_seconds==="number" && policy.max_backoff_seconds<policy.backoff_seconds);
}

export function StageRetryFields({value,disabled=false,onChange}:{value?:WorkflowObject;disabled?:boolean;onChange:(value:WorkflowObject)=>void}) {
 const {t}=useT("workflows");const id=useId();
 const labels={max_attempts:t($=>$.retry_config.max_attempts),timeout_seconds:t($=>$.retry_config.timeout_seconds),backoff_seconds:t($=>$.retry_config.backoff_seconds),backoff_multiplier:t($=>$.retry_config.backoff_multiplier),max_backoff_seconds:t($=>$.retry_config.max_backoff_seconds)};
 const valid=validRetryPolicyDraft(value);
 return <fieldset className="space-y-3 border-t border-border pt-3" disabled={disabled} aria-describedby={`${id}-hint`}>
  <legend className="px-1 text-caption font-medium">{t($=>$.retry_config.title)}</legend>
  <p id={`${id}-hint`} className="text-micro text-muted-foreground">{t($=>$.retry_config.hint)}</p>
  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
   {fields.map(({key,min,max})=><div key={key} className="min-w-0 space-y-1.5">
    <label htmlFor={`${id}-${key}`} className="block text-caption">{labels[key]} <span className="text-muted-foreground">({min}–{max})</span></label>
    <Input id={`${id}-${key}`} aria-label={labels[key]} type="number" min={min} max={max} step={1} disabled={disabled} value={typeof value?.[key]==="number" ? value[key] as number : ""} placeholder={t($=>$.retry_config.default_value)} onChange={event=>{
     const next={...value};
     if(event.target.value==="") {delete next[key];if(key==="backoff_seconds") {delete next.backoff_multiplier;delete next.max_backoff_seconds;}}
     else next[key]=Number(event.target.value);
     onChange(next);
    }}/>
   </div>)}
  </div>
  <p className="text-micro text-muted-foreground">{t($=>$.retry_config.backoff_hint)}</p>
  {!valid && <p role="alert" className="text-micro text-destructive">{t($=>$.retry_config.invalid)}</p>}
 </fieldset>;
}
