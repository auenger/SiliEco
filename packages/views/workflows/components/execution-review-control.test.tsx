import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { WorkflowInstance } from "@silieco/core/types";
import enWorkflows from "../../locales/en/workflows.json";
import { ExecutionReviewControl } from "./execution-review-control";

const mocks=vi.hoisted(() => ({review:vi.fn(),result:vi.fn(),decision:vi.fn(),download:vi.fn()}));
vi.mock("@silieco/core/auth",()=>({useAuthStore:(selector:(state:{user:{id:string}})=>unknown)=>selector({user:{id:"u"}})}));
vi.mock("@silieco/core/api",()=>({api:{getWorkflowArtifactBlob:mocks.download}}));
vi.mock("@silieco/core/workflows",()=>({
  workflowExecutionsOptions:()=>({queryKey:["executions"],queryFn:async()=>[{id:"e",stage_id:"s",attempt:1,plan_revision:1,status:"awaiting_review",result_revision:1}]}),
  workflowHumanReviewOptions:()=>({queryKey:["review"],queryFn:mocks.review,retry:false}),
  workflowResultOptions:(_ws:string,_run:string,_execution:string,revision:number)=>({queryKey:["result",revision],queryFn:mocks.result,retry:false}),
  useRecordWorkflowHumanDecision:()=>({mutateAsync:mocks.decision,isPending:false}),
}));

const run={id:"r",workspace_id:"ws",revision:1,status:"active",archived_at:null,current_stage_id:"s",stages:[{id:"s",execution_mode:"agent",gate:{type:"human",decider:"u"}}]} as unknown as WorkflowInstance;
const basis=(hash:string)=>({execution_id:"e",result_revision:1,basis_hash:hash,issues:[{id:"i",title:"Validated delivery",status:"done",updated_at:"now"}],calls:[{id:"c",issue_id:"i",agent_id:"a",status:"completed"}]});

function setup(instance=run){
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 render(<QueryClientProvider client={client}><I18nProvider locale="en" resources={{en:{workflows:enWorkflows}}}><ExecutionReviewControl instance={instance}/></I18nProvider></QueryClientProvider>);
 return client;
}
async function open(){const button=screen.getByRole("button",{name:"Review delivery"});await waitFor(()=>expect(button).toBeEnabled());fireEvent.click(button);await screen.findByText("Validated delivery");}

beforeEach(()=>{
 vi.clearAllMocks();mocks.review.mockResolvedValue(basis("a".repeat(64)));mocks.result.mockResolvedValue({id:"result",execution_id:"e",revision:1,data:{summary:"Tests passed"},artifacts:[]});mocks.decision.mockResolvedValue({id:"decision"});
});

describe("ExecutionReviewControl",()=>{
 it("requires acknowledgement and submits the exact displayed basis",async()=>{
  setup();await open();
  const approve=screen.getByRole("button",{name:"Approve and advance"});expect(approve).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox"));fireEvent.click(approve);
  await waitFor(()=>expect(mocks.decision).toHaveBeenCalledExactlyOnceWith({instanceId:"r",executionId:"e",data:{expected_result_revision:1,expected_basis_hash:"a".repeat(64),outcome:"approved",note:""}}));
 });
 it("resets acknowledgement when the evidence changes",async()=>{
  const client=setup();await open();fireEvent.click(screen.getByRole("checkbox"));
  mocks.review.mockResolvedValue(basis("b".repeat(64)));
  await act(async()=>{await client.invalidateQueries({queryKey:["review"]});});
  await waitFor(() => expect(screen.getByRole("checkbox")).not.toBeChecked());
  expect(screen.getByRole("button",{name:"Approve and advance"})).toBeDisabled();
  expect(mocks.decision).not.toHaveBeenCalled();
 });
 it("blocks another decision after an uncertain or conflicting response",async()=>{
  mocks.decision.mockRejectedValue(new Error("409 changed"));setup();await open();fireEvent.click(screen.getByRole("checkbox"));fireEvent.click(screen.getByRole("button",{name:"Approve and advance"}));
  await screen.findByRole("alert");expect(screen.getByRole("button",{name:"Approve and advance"})).toBeDisabled();expect(screen.getByRole("checkbox")).toBeDisabled();expect(mocks.decision).toHaveBeenCalledTimes(1);
 });
 it("never offers approval for an invalid result response",async()=>{
  mocks.result.mockResolvedValue(null);setup();
  const button=screen.getByRole("button",{name:"Review delivery"});await waitFor(()=>expect(button).toBeEnabled());fireEvent.click(button);
  await screen.findByText("Could not load this delivery.");expect(screen.queryByRole("button",{name:"Approve and advance"})).not.toBeInTheDocument();
 });
 it.each(["none", "agent", "hybrid"] as const)("shows %s deliveries without offering the human-only action", async (type) => {
  setup({...run, stages:[{...run.stages![0]!,gate:{type}}]});
  const button=screen.getByRole("button",{name:"Review delivery"});await waitFor(()=>expect(button).toBeEnabled());fireEvent.click(button);
  await screen.findByText(/Tests passed/);
  expect(screen.queryByRole("button",{name:"Approve and advance"})).not.toBeInTheDocument();
  expect(mocks.review).not.toHaveBeenCalled();
 });
 it("waits for server-confirmed agent approval before hybrid human review", async () => {
  mocks.review.mockRejectedValue(new Error("409 agent review incomplete"));
  setup({...run, stages:[{...run.stages![0]!,gate:{type:"hybrid",decider:"agent",human_decider:"u"}}]});
  const button=screen.getByRole("button",{name:"Review delivery"});
  await waitFor(()=>expect(button).toBeEnabled());fireEvent.click(button);
  await screen.findByText("This delivery is not ready for your review, or the evidence has changed.");
  expect(screen.getByText(enWorkflows.execution_review.hybrid_sequence)).toBeInTheDocument();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByRole("button",{name:"Approve and advance"})).not.toBeInTheDocument();
  expect(mocks.review).toHaveBeenCalledTimes(1);
  expect(mocks.decision).not.toHaveBeenCalled();
  mocks.review.mockResolvedValue(basis("b".repeat(64)));
  fireEvent.click(screen.getByRole("button",{name:"Refresh delivery"}));
  const checkbox=await screen.findByRole("checkbox");
  expect(checkbox).not.toBeChecked();
  fireEvent.click(checkbox);fireEvent.click(screen.getByRole("button",{name:"Approve and advance"}));
  await waitFor(()=>expect(mocks.decision).toHaveBeenCalledExactlyOnceWith({instanceId:"r",executionId:"e",data:{expected_result_revision:1,expected_basis_hash:"b".repeat(64),outcome:"approved",note:""}}));
 });
 it.each([{type:"hybrid",decider:"u"},{type:"hybrid",decider:"u",human_decider:"other"}] as const)("does not use the agent decider as the hybrid human reviewer (%j)",async(gate)=>{
  setup({...run,stages:[{...run.stages![0]!,gate}]});
  const button=screen.getByRole("button",{name:"Review delivery"});await waitFor(()=>expect(button).toBeEnabled());fireEvent.click(button);
  await screen.findByText(/Tests passed/);
  expect(mocks.review).not.toHaveBeenCalled();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
 });
 it("removes hybrid approval when refreshed evidence is no longer reviewable",async()=>{
  const client=setup({...run,stages:[{...run.stages![0]!,gate:{type:"hybrid",decider:"agent",human_decider:"u"}}]});
  await open();fireEvent.click(screen.getByRole("checkbox"));
  mocks.review.mockRejectedValue(new Error("409 evidence changed"));
  await act(async()=>{await client.invalidateQueries({queryKey:["review"]});});
  await screen.findByText("This delivery is not ready for your review, or the evidence has changed.");
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(mocks.decision).not.toHaveBeenCalled();
 });
 it("removes approval when result refresh fails despite cached delivery",async()=>{
  const client=setup();await open();fireEvent.click(screen.getByRole("checkbox"));
  mocks.result.mockRejectedValue(new Error("result inaccessible"));
  await act(async()=>{await client.invalidateQueries({queryKey:["result"]});});
  await screen.findByText("Could not load this delivery.");
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(mocks.decision).not.toHaveBeenCalled();
 });
 it("downloads the exact artifact revision through the authenticated client", async () => {
  mocks.result.mockResolvedValue({id:"result",execution_id:"e",revision:1,data:{summary:"Tests passed"},artifacts:[{id:"ar",artifact_id:"a",revision:2,name:"report.md",media_type:"text/markdown",size_bytes:12,checksum:"digest"}]});
  mocks.download.mockRejectedValue(new Error("download unavailable"));
  setup();await open();fireEvent.click(screen.getByRole("button",{name:"Download report.md"}));
  await waitFor(()=>expect(mocks.download).toHaveBeenCalledExactlyOnceWith("r","e",1,"ar"));
 });

});

vi.mock("./agent-review-recovery-control", () => ({AgentReviewRecoveryControl: () => null}));
