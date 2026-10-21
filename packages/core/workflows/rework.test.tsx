/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api } from "../api";
import { useReworkWorkflowExecution } from "./mutations";
import { issueKeys } from "../issues/queries";
import { workflowKeys } from "./queries";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "unused" }));
vi.mock("../api", () => ({ api: { reworkWorkflowExecution: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const data = {expected_revision:1,target_stage_id:"stage",reason:"Revise requirements"};

describe("workflow rework mutation", () => {
  it.each(["conflict", "malformed", "mismatched", "success", "manual"])("handles %s without retry or optimistic stage writes", async (outcome) => {
    const client = new QueryClient({defaultOptions:{mutations:{retry:3,retryDelay:0}}});
    const key = workflowKeys.instance("ws", "run");
    const other = workflowKeys.instance("other", "run");
    const issue = issueKeys.detail("ws", "issue");
    const otherIssue = issueKeys.detail("other", "issue");
    client.setQueryData(issue, {title:"before rework"});
    client.setQueryData(otherIssue, {title:"other workspace"});
    client.setQueryData(key,{current_stage_id:"current"});
    client.setQueryData(other,{current_stage_id:"other"});
    if (outcome === "conflict") vi.mocked(api.reworkWorkflowExecution).mockRejectedValue(new Error("409 changed"));
    else if (outcome === "malformed") vi.mocked(api.reworkWorkflowExecution).mockResolvedValue(null);
    else if (outcome === "manual") vi.mocked(api.reworkWorkflowExecution).mockResolvedValue({rework_id:"record",instance_id:"run",execution_id:null,source_execution_id:"execution",stage_id:"stage",status:"awaiting_work",attempt:null});
    else vi.mocked(api.reworkWorkflowExecution).mockResolvedValue({instance_id:"run",execution_id:"new",source_execution_id:outcome === "mismatched" ? "other" : "execution",stage_id:"stage",status:"pending",attempt:2});
    const wrapper = ({children}:{children:ReactNode}) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const {result} = renderHook(() => useReworkWorkflowExecution("ws"),{wrapper});
    await act(async () => {
      const response = result.current.mutateAsync({instanceId:"run",sourceExecutionId:"execution",data});
      if (outcome === "success" || outcome === "manual") await expect(response).resolves.toMatchObject({status:outcome === "manual" ? "awaiting_work" : "pending"});
      else await expect(response).rejects.toThrow();
    });
    expect(api.reworkWorkflowExecution).toHaveBeenCalledExactlyOnceWith("run","execution",data);
    expect(client.getQueryData(key)).toEqual({current_stage_id:"current"});
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryState(other)?.isInvalidated).toBe(false);
    expect(client.getQueryState(issue)?.isInvalidated).toBe(true);
    expect(client.getQueryState(otherIssue)?.isInvalidated).toBe(false);
    client.clear();
  });
});
