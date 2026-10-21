import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { WorkflowExecution, WorkflowInstance } from "@silieco/core/types";
import enWorkflows from "../../locales/en/workflows.json";
import { ExecutionReworkControl } from "./execution-rework-control";
const rework = vi.hoisted(() => vi.fn());
vi.mock("@silieco/core/auth", () => ({useAuthStore:(selector:(state:{user:{id:string}})=>unknown)=>selector({user:{id:"u"}})}));
vi.mock("@silieco/core/workflows", () => ({useReworkWorkflowExecution:()=>({mutateAsync:rework,isPending:false})}));
const run = {id:"r",workspace_id:"ws",created_by:"u",status:"active",revision:1,current_stage_id:"s",stages:[
 {id:"before",stable_key:"requirements",name:"Requirements",position:0,execution_mode:"agent"},
 {id:"s",stable_key:"build",name:"Build",position:1,execution_mode:"agent",rollback_stage_key:"requirements"},
 {id:"after",stable_key:"report",name:"Report",position:2,execution_mode:"agent"},
]} as unknown as WorkflowInstance;
const execution = {id:"e",stage_id:"s",plan_revision:1,status:"awaiting_review",attempt:1} as WorkflowExecution;
function view(instance=run, disabled=false) {return <I18nProvider locale="en" resources={{en:{workflows:enWorkflows}}}><ExecutionReworkControl instance={instance} execution={execution} disabled={disabled}/></I18nProvider>;}
function setup() {const result=render(view());fireEvent.click(screen.getByRole("button",{name:"Rework"}));return result;}
function confirm() {fireEvent.change(screen.getByRole("textbox",{name:"Reason for rework"}),{target:{value:"Correct requirements"}});fireEvent.click(screen.getByRole("checkbox"));}
beforeEach(()=>{rework.mockReset();rework.mockResolvedValue({execution_id:"new"});});
it("shows the downstream impact and submits the explicitly reviewed target",async()=>{
 setup();expect(screen.getByRole("button",{name:"Start rework"})).toBeDisabled();
 expect(screen.getByRole("list")).toHaveTextContent("BuildReport");confirm();
 fireEvent.click(screen.getByRole("radio",{name:/Requirements/}));
 expect(screen.getByRole("checkbox")).not.toBeChecked();expect(screen.getByRole("list")).toHaveTextContent("RequirementsBuildReport");
 fireEvent.click(screen.getByRole("checkbox"));fireEvent.click(screen.getByRole("button",{name:"Start rework"}));
 await waitFor(()=>expect(rework).toHaveBeenCalledExactlyOnceWith({instanceId:"r",sourceExecutionId:"e",data:{expected_revision:1,target_stage_id:"before",reason:"Correct requirements"}}));
 await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});
it("locks target, reason and dismissal while submitting",async()=>{
 let resolve!:()=>void;rework.mockImplementation(()=>new Promise<void>((done)=>{resolve=done;}));setup();confirm();fireEvent.click(screen.getByRole("button",{name:"Start rework"}));
 expect(screen.getByRole("button",{name:"Cancel"})).toBeDisabled();expect(screen.getByRole("textbox")).toBeDisabled();expect(screen.getByRole("radio",{name:/Requirements/})).toBeDisabled();
 fireEvent.keyDown(screen.getByRole("dialog"),{key:"Escape"});expect(screen.getByRole("dialog")).toBeInTheDocument();expect(rework).toHaveBeenCalledTimes(1);
 await act(async()=>resolve());
});
it("keeps conflict failure locked across query refreshes until reopened",async()=>{
 rework.mockRejectedValue(new Error("409"));const result=setup();confirm();fireEvent.click(screen.getByRole("button",{name:"Start rework"}));
 await screen.findByRole("alert");
 result.rerender(view(run,true));result.rerender(view());
 expect(screen.getByRole("textbox")).toBeDisabled();expect(screen.getByRole("button",{name:"Start rework"})).toBeDisabled();expect(rework).toHaveBeenCalledTimes(1);
 fireEvent.click(screen.getByRole("button",{name:"Cancel"}));fireEvent.click(screen.getByRole("button",{name:"Rework"}));
 expect(screen.getByRole("textbox")).toHaveValue("");expect(screen.getByRole("checkbox")).not.toBeChecked();
});
it("requires acknowledgement again when the plan changes",()=>{
 const result=setup();confirm();result.rerender(view({...run,revision:2}));expect(screen.queryByRole("dialog")).not.toBeInTheDocument();expect(rework).not.toHaveBeenCalled();
});
it.each([{...run,created_by:"other"},{...run,archived_at:"now"},{...run,status:"cancelled" as const},{...run,current_stage_id:"after"}])("hides rework without current initiator access",(instance)=>{
 render(view(instance));expect(screen.queryByRole("button",{name:"Rework"})).not.toBeInTheDocument();
});
it("rejects an oversized reason without sending a request",()=>{
 setup();fireEvent.change(screen.getByRole("textbox"),{target:{value:"返".repeat(1400)}});fireEvent.click(screen.getByRole("checkbox"));expect(screen.getByRole("alert")).toBeInTheDocument();expect(screen.getByRole("button",{name:"Start rework"})).toBeDisabled();expect(rework).not.toHaveBeenCalled();
});
it("allows the configured earlier manual stage as a rework target",async()=>{
 const mixed={...run,stages:(run.stages ?? []).map(stage=>stage.id==="before"?{...stage,execution_mode:"manual" as const}:stage)};
 render(view(mixed));fireEvent.click(screen.getByRole("button",{name:"Rework"}));
 fireEvent.click(screen.getByRole("radio",{name:/Requirements/}));confirm();
 fireEvent.click(screen.getByRole("button",{name:"Start rework"}));
 await waitFor(()=>expect(rework).toHaveBeenCalledExactlyOnceWith({instanceId:"r",sourceExecutionId:"e",data:{expected_revision:1,target_stage_id:"before",reason:"Correct requirements"}}));
});
it("does not offer another rework when the source stage budget is exhausted",()=>{
 render(view({...run,stages:(run.stages??[]).map(stage=>stage.id==="s"?{...stage,rework_policy:{max_reworks:0}}:stage)}));
 expect(screen.queryByRole("button",{name:"Rework"})).not.toBeInTheDocument();expect(screen.getByText("Rework limit reached (0 / 0).")).toBeInTheDocument();expect(rework).not.toHaveBeenCalled();
});
