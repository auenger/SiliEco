import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api";
import {
  isRevisionConflictError,
  revisionConflictBody,
  useRevisionConflictStore,
} from "./conflict-store";

describe("revision conflicts", () => {
  beforeEach(() => useRevisionConflictStore.getState().close());

  it("accepts only structured revision conflict responses", () => {
    const error = new ApiError("conflict", 409, "Conflict", {
      code: "revision_conflict",
      current: { revision: "server", description: "server value" },
      attempted: { expected_revision: "old", description: "my value" },
    });
    expect(revisionConflictBody(error)).toEqual({
      current: { revision: "server", description: "server value" },
      attempted: { expected_revision: "old", description: "my value" },
    });
    expect(revisionConflictBody(new ApiError("bad", 409, "Conflict", {}))).toBeNull();
    expect(revisionConflictBody(new Error("offline"))).toBeNull();
    expect(isRevisionConflictError(error)).toBe(true);
    expect(isRevisionConflictError(new Error("offline"))).toBe(false);
  });

  it("stores explicit retry and refresh actions", async () => {
    const retry = vi.fn(async () => undefined);
    const refresh = vi.fn(async () => undefined);
    useRevisionConflictStore.getState().open({
      kind: "issue",
      current: { description: "server" },
      attempted: { description: "mine" },
      retry,
      refresh,
    });
    const conflict = useRevisionConflictStore.getState().conflict;
    await conflict?.retry();
    await conflict?.refresh();
    expect(retry).toHaveBeenCalledOnce();
    expect(refresh).toHaveBeenCalledOnce();
  });
});
