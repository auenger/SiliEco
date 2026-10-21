import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { WorkflowExecution, WorkflowInstance } from "@silieco/core/types";
import enWorkflows from "../../locales/en/workflows.json";
import { InputRecoveryControl } from "./input-recovery-control";
const recover = vi.hoisted(() => vi.fn());
vi.mock("@silieco/core/auth", () => ({useAuthStore:(selector:(state:{user:{id:string}})=>unknown)=>selector({user:{id:"u"}})}));
vi.mock("@silieco/core/workflows", () => ({useCorrectWorkflowExecutionInputs:()=>({mutateAsync:recover,isPending:false})}));
const run = {id:"r",workspace_id:"ws",created_by:"u",status:"active",revision:1,current_stage_id:"s",inputs:{goal:"old"}} as unknown as WorkflowInstance;
const execution = {id:"e",stage_id:"s",plan_revision:1,status:"blocked",failure_code:"input_unavailable",failure_detail:"Missing goal"} as WorkflowExecution;
function setup(instance=run) {render(<I18nProvider locale="en" resources={{en:{workflows:enWorkflows}}}><InputRecoveryControl instance={instance} execution={execution}/></I18nProvider>);}
beforeEach(()=>{recover.mockReset();});
it("keeps malformed inputs local and submits exact old/new documents",async()=>{
 recover.mockResolvedValue({status:"pending"});setup();fireEvent.click(screen.getByRole("button",{name:"Correct inputs"}));
 const input=screen.getByRole("textbox");fireEvent.change(input,{target:{value:"[]"}});expect(screen.getByRole("button",{name:"Save and resume"})).toBeDisabled();
 fireEvent.change(input,{target:{value:'{"goal":"new"}'}});fireEvent.click(screen.getByRole("button",{name:"Save and resume"}));
 expect(recover).toHaveBeenCalledExactlyOnceWith({instanceId:"r",executionId:"e",data:{expected_inputs:{goal:"old"},inputs:{goal:"new"}}});
});
it("disables retry after an uncertain response",async()=>{
 recover.mockRejectedValue(new Error("409"));setup();fireEvent.click(screen.getByRole("button",{name:"Correct inputs"}));fireEvent.click(screen.getByRole("button",{name:"Save and resume"}));
 await screen.findByRole("alert");expect(screen.getByRole("button",{name:"Save and resume"})).toBeDisabled();expect(screen.getByRole("textbox")).toBeDisabled();expect(recover).toHaveBeenCalledTimes(1);
});
it("shows the diagnostic without correction access to another member",()=>{
 setup({...run,created_by:"other"});expect(screen.getByText("Missing goal")).toBeInTheDocument();expect(screen.queryByRole("button",{name:"Correct inputs"})).not.toBeInTheDocument();
});
