/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api } from "../api";
import { useCorrectWorkflowExecutionInputs } from "./mutations";
import { workflowKeys } from "./queries";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "unused" }));
vi.mock("../api", () => ({ api: { correctWorkflowExecutionInputs: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const data = {expected_inputs: {goal:"old"}, inputs:{goal:"new"}};

describe("workflow input recovery mutation", () => {
  it.each(["conflict", "malformed", "mismatched", "success"])("handles %s without retry or optimistic input writes", async (outcome) => {
    const client = new QueryClient({defaultOptions:{mutations:{retry:3,retryDelay:0}}});
    const key = workflowKeys.instance("ws", "run");
    const other = workflowKeys.instance("other", "run");
    client.setQueryData(key,{inputs:data.expected_inputs});
    client.setQueryData(other,{inputs:{other:true}});
    if (outcome === "conflict") vi.mocked(api.correctWorkflowExecutionInputs).mockRejectedValue(new Error("409 changed"));
    else if (outcome === "malformed") vi.mocked(api.correctWorkflowExecutionInputs).mockResolvedValue(null);
    else vi.mocked(api.correctWorkflowExecutionInputs).mockResolvedValue({instance_id:"run",execution_id:outcome === "mismatched" ? "other" : "execution",status:"pending",attempt:1});
    const wrapper = ({children}:{children:ReactNode}) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const {result} = renderHook(() => useCorrectWorkflowExecutionInputs("ws"),{wrapper});
    await act(async () => {
      const response = result.current.mutateAsync({instanceId:"run",executionId:"execution",data});
      if (outcome === "success") await expect(response).resolves.toMatchObject({status:"pending"});
      else await expect(response).rejects.toThrow();
    });
    expect(api.correctWorkflowExecutionInputs).toHaveBeenCalledExactlyOnceWith("run","execution",data);
    expect(client.getQueryData(key)).toEqual({inputs:data.expected_inputs});
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryState(other)?.isInvalidated).toBe(false);
    client.clear();
  });
});
