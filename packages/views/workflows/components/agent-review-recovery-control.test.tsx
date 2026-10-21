import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { WorkflowAgentReviewStatus, WorkflowExecution, WorkflowInstance } from "@silieco/core/types";
import enWorkflows from "../../locales/en/workflows.json";
import { AgentReviewRecoveryControl } from "./agent-review-recovery-control";
const state = vi.hoisted(() => ({recover:vi.fn(),refetch:vi.fn(),data:null as WorkflowAgentReviewStatus | null}));
vi.mock("@silieco/core/auth", () => ({useAuthStore:(selector:(s:{user:{id:string}})=>unknown)=>selector({user:{id:"owner"}})}));
vi.mock("@tanstack/react-query", () => ({useQuery:()=>({data:state.data,isError:false,isFetching:false,refetch:state.refetch})}));
vi.mock("@silieco/core/workflows", () => ({workflowAgentReviewStatusOptions:()=>({}),useRecoverWorkflowAgentReview:()=>({mutateAsync:state.recover,isPending:false})}));
const run = {id:"run",workspace_id:"ws",created_by:"owner",status:"active",current_stage_id:"stage"} as WorkflowInstance;
const execution = {id:"execution",stage_id:"stage",status:"awaiting_review",result_revision:2} as WorkflowExecution;
function view(instance=run) {return <I18nProvider locale="en" resources={{en:{workflows:enWorkflows}}}><AgentReviewRecoveryControl instance={instance} execution={execution}/></I18nProvider>;}
function setup() {const result=render(view());fireEvent.click(screen.getByRole("button",{name:"Recover review"}));return result;}
function confirm() {fireEvent.change(screen.getByRole("textbox"),{target:{value:"Connection restored"}});fireEvent.click(screen.getByRole("checkbox"));}
beforeEach(()=>{state.recover.mockReset();state.recover.mockResolvedValue({task_id:"new"});state.data={execution_id:"execution",phase:"failed",request_id:"request",reviewer_id:"agent",result_revision:2,basis_hash:"a".repeat(64),call:{id:"failed",status:"failed",failure_reason:"agent_error.provider_network",attempt:3,max_attempts:3},can_recover:true,recovery_attempts:3,retry_at:null};});
it("requires a reason and confirmation before recovering the reviewed call",async()=>{
 setup();expect(screen.getByRole("dialog")).toHaveClass("sm:max-w-2xl");expect(screen.getByRole("button",{name:"Start review again"})).toBeDisabled();confirm();fireEvent.click(screen.getByRole("button",{name:"Start review again"}));
 await waitFor(()=>expect(state.recover).toHaveBeenCalledExactlyOnceWith({instanceId:"run",executionId:"execution",requestId:"request",data:{failed_task_id:"failed",expected_result_revision:2,expected_basis_hash:"a".repeat(64),reason:"Connection restored"}}));
 await waitFor(()=>expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});
it("locks a reviewed snapshot when a new call arrives",()=>{
 const result=setup();confirm();state.data={...state.data!,call:{...state.data!.call!,id:"replacement"}};result.rerender(view());expect(screen.getByRole("alert")).toBeInTheDocument();expect(screen.getByRole("button",{name:"Start review again"})).toBeDisabled();expect(state.recover).not.toHaveBeenCalled();
});
it("requires reopening after uncertain submission",async()=>{
 state.recover.mockRejectedValue(new Error("network"));const result=setup();confirm();fireEvent.click(screen.getByRole("button",{name:"Start review again"}));await screen.findByRole("alert");result.rerender(view());expect(screen.getByRole("button",{name:"Start review again"})).toBeDisabled();expect(state.recover).toHaveBeenCalledTimes(1);
});
it("blocks synchronous duplicate submissions",async()=>{
 let resolve!:()=>void;state.recover.mockImplementation(()=>new Promise<void>(done=>{resolve=done;}));setup();confirm();fireEvent.click(screen.getByRole("button",{name:"Start review again"}));fireEvent.click(screen.getByRole("button",{name:"Start review again"}));expect(state.recover).toHaveBeenCalledTimes(1);await act(async()=>resolve());
});
it.each(["retry_scheduled","queued","running","approved","rejected","unknown"])("does not recover %s calls even with a stale capability",phase=>{
 state.data={...state.data!,phase};render(view());expect(screen.queryByRole("button",{name:"Recover review"})).not.toBeInTheDocument();
});
it("hides recovery from other members",()=>{render(view({...run,created_by:"other"}));expect(screen.queryByRole("button",{name:"Recover review"})).not.toBeInTheDocument();});
it("keeps oversized reasons editable and prevents submission",()=>{setup();fireEvent.change(screen.getByRole("textbox"),{target:{value:"返".repeat(1400)}});fireEvent.click(screen.getByRole("checkbox"));expect(screen.getByRole("textbox")).toBeEnabled();expect(screen.getByRole("button",{name:"Start review again"})).toBeDisabled();expect(state.recover).not.toHaveBeenCalled();});

it("allows explicit recovery after completion without a review decision",async()=>{
 state.data={...state.data!,phase:"missing_decision",call:{...state.data!.call!,status:"completed"}};
 setup();confirm();fireEvent.click(screen.getByRole("button",{name:"Start review again"}));await waitFor(()=>expect(state.recover).toHaveBeenCalledTimes(1));
});
