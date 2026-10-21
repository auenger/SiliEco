/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api } from "../api";
import { useRecoverWorkflowAgentReview } from "./mutations";
import { workflowKeys } from "./queries";
vi.mock("../hooks",()=>({useWorkspaceId:()=>"unused"}));
vi.mock("../api",()=>({api:{recoverWorkflowAgentReview:vi.fn()}}));
afterEach(()=>{cleanup();vi.resetAllMocks();});
it.each(["conflict","malformed","mismatched","success"])("handles %s without automatic resubmission",async outcome=>{
 const client=new QueryClient({defaultOptions:{mutations:{retry:3,retryDelay:0}}});
 const key=workflowKeys.instance("ws","run"),other=workflowKeys.instance("other","run");client.setQueryData(key,{status:"active"});client.setQueryData(other,{status:"active"});
 if(outcome==="conflict") vi.mocked(api.recoverWorkflowAgentReview).mockRejectedValue(new Error("409"));
 else if(outcome==="malformed") vi.mocked(api.recoverWorkflowAgentReview).mockResolvedValue(null);
 else vi.mocked(api.recoverWorkflowAgentReview).mockResolvedValue({recovery_id:"record",instance_id:"run",execution_id:"execution",request_id:"request",failed_task_id:outcome==="mismatched"?"other":"failed",task_id:"new",allowed_attempts:3,max_attempts:6});
 const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>;
 const {result}=renderHook(()=>useRecoverWorkflowAgentReview("ws"),{wrapper});
 const data={failed_task_id:"failed",expected_result_revision:2,expected_basis_hash:"a".repeat(64),reason:"Restored"};
 await act(async()=>{const response=result.current.mutateAsync({instanceId:"run",executionId:"execution",requestId:"request",data});if(outcome==="success")await expect(response).resolves.toMatchObject({task_id:"new"});else await expect(response).rejects.toThrow();});
 expect(api.recoverWorkflowAgentReview).toHaveBeenCalledExactlyOnceWith("run","execution","request",data);expect(client.getQueryState(key)?.isInvalidated).toBe(true);expect(client.getQueryState(other)?.isInvalidated).toBe(false);expect(client.getQueryData(key)).toEqual({status:"active"});client.clear();
});
