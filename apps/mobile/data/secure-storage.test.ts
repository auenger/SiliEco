import { beforeEach, describe, expect, it, vi } from "vitest";

const storage = vi.hoisted(() => new Map<string, string>());
vi.mock("expo-secure-store", () => ({
  getItemAsync: vi.fn(async (key: string) => storage.get(key) ?? null),
  setItemAsync: vi.fn(async (key: string, value: string) => {
    storage.set(key, value);
  }),
  deleteItemAsync: vi.fn(async (key: string) => {
    storage.delete(key);
  }),
}));

import { clearToken, getToken, setToken } from "./secure-storage";
import { useWorkspaceStore } from "./workspace-store";

beforeEach(() => {
  storage.clear();
  useWorkspaceStore.setState({
    currentWorkspaceId: null,
    currentWorkspaceSlug: null,
  });
});

describe("Task / AgentRun storage generation", () => {
  it("does not restore credentials or workspace from discarded test data", async () => {
    storage.set("silieco_token", "old-token");
    storage.set("silieco_current_workspace_slug", "old-space");
    expect(await getToken()).toBeNull();
    expect(await useWorkspaceStore.getState().restoreSlug()).toBeNull();
  });

  it("restores and clears credentials written by the current generation", async () => {
    await setToken("new-token");
    await useWorkspaceStore.getState().setCurrentWorkspace("space-id", "new-space");
    expect(await getToken()).toBe("new-token");
    expect(await useWorkspaceStore.getState().restoreSlug()).toBe("new-space");
    await clearToken();
    await useWorkspaceStore.getState().clear();
    expect(await getToken()).toBeNull();
    expect(await useWorkspaceStore.getState().restoreSlug()).toBeNull();
  });
});
