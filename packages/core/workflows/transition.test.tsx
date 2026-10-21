/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api } from "../api";
import { EMPTY_WORKFLOW_INSTANCE } from "../api/schemas";
import { issueKeys } from "../issues/queries";
import { workflowKeys } from "./queries";
import { useTransitionWorkflowInstance } from "./mutations";

vi.mock("../hooks",()=>({useWorkspaceId:()=>"ws"}));
vi.mock("../api",()=>({api:{transitionWorkflowInstance:vi.fn()}}));
afterEach(()=>{cleanup();vi.resetAllMocks();});

it.each([false,true])("manual rollback refreshes Run and issue caches without retry (failure=%s)",async(failure)=>{
 const client=new QueryClient({defaultOptions:{mutations:{retry:3,retryDelay:0}}});
 const key=workflowKeys.instance("ws","run");
 const issue=issueKeys.detail("ws","issue");
 const other=issueKeys.detail("other","issue");
 for(const entry of [key,issue,other])client.setQueryData(entry,{before:true});
 if(failure)vi.mocked(api.transitionWorkflowInstance).mockRejectedValue(new Error("409 changed"));
 else vi.mocked(api.transitionWorkflowInstance).mockResolvedValue({...EMPTY_WORKFLOW_INSTANCE,id:"run"});
 const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>;
 const {result}=renderHook(()=>useTransitionWorkflowInstance(),{wrapper});
 const data={expected_state_token:"a".repeat(64),outcome:"rejected" as const,target_stage_id:"build",note:"Revise delivery"};
 await act(async()=>{
  const response=result.current.mutateAsync({instanceId:"run",data});
  if(failure)await expect(response).rejects.toThrow("409 changed");
  else await response;
 });
 expect(api.transitionWorkflowInstance).toHaveBeenCalledExactlyOnceWith("run",data);
 expect(client.getQueryState(key)?.isInvalidated).toBe(true);
 expect(client.getQueryState(issue)?.isInvalidated).toBe(true);
 expect(client.getQueryState(other)?.isInvalidated).toBe(false);
 expect(client.getQueryData(key)).toEqual({before:true});
 client.clear();
});
