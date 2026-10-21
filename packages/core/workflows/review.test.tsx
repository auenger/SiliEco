/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api } from "../api";
import { useRecordWorkflowHumanDecision } from "./mutations";
import { workflowHumanReviewOptions, workflowKeys } from "./queries";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "unused" }));
vi.mock("../api", () => ({ api: { recordWorkflowHumanDecision: vi.fn(), getWorkflowHumanReview: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });

const data = {expected_result_revision: 3, expected_basis_hash: "a".repeat(64), outcome: "approved" as const};

describe("workflow human review query lifecycle", () => {
  it("separates workspace, Run and execution identities", () => {
    const first = workflowHumanReviewOptions("ws", "run", "execution");
    expect(first.queryKey).not.toEqual(workflowHumanReviewOptions("other", "run", "execution").queryKey);
    expect(first.queryKey).not.toEqual(workflowHumanReviewOptions("ws", "other", "execution").queryKey);
    expect(first.queryKey).not.toEqual(workflowHumanReviewOptions("ws", "run", "other").queryKey);
    expect(workflowHumanReviewOptions("", "run", "execution").enabled).toBe(false);
    expect(first.retry).toBe(false);
  });

  it.each(["conflict", "malformed", "mismatched"])("does not retry %s decisions or replace their reviewed basis", async (failure) => {
    const client = new QueryClient({defaultOptions: {mutations: {retry: 3, retryDelay: 0}}});
    const key = workflowKeys.review("ws", "run", "execution");
    const unrelated = workflowKeys.review("other", "run", "execution");
    client.setQueryData(key, {basis_hash: "old"});
    client.setQueryData(unrelated, {basis_hash: "other"});
    const wrapper = ({children}: {children: ReactNode}) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    if (failure === "conflict") vi.mocked(api.recordWorkflowHumanDecision).mockRejectedValue(new Error("409 evidence changed"));
    else if (failure === "malformed") vi.mocked(api.recordWorkflowHumanDecision).mockResolvedValue(null);
    else vi.mocked(api.recordWorkflowHumanDecision).mockResolvedValue({id:"d", workflow_instance_id:"run",execution_id:"execution",result_revision:3,basis_hash:"b".repeat(64),review_step:"human",outcome:"approved",actor_type:"member",actor_id:"u",created_at:"now",from_stage_id:"stage",to_stage_id:null,note:null});
    const {result} = renderHook(() => useRecordWorkflowHumanDecision("ws"), {wrapper});
    await act(async () => {
      await expect(result.current.mutateAsync({instanceId: "run", executionId: "execution", data})).rejects.toThrow();
    });
    expect(api.recordWorkflowHumanDecision).toHaveBeenCalledExactlyOnceWith("run", "execution", data);
    expect(api.getWorkflowHumanReview).not.toHaveBeenCalled();
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    expect(client.getQueryState(unrelated)?.isInvalidated).toBe(false);
    client.clear();
  });
});
