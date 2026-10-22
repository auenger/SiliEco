import { InfiniteQueryObserver, QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { fileResourcesInfiniteOptions } from "./queries";

vi.mock("../api", () => ({ api: { listFileResources: vi.fn(), listProjectFiles: vi.fn() } }));

describe("file resource pagination", () => {
  it("loads the next offset only on request and continues past 100 files", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    vi.mocked(api.listFileResources).mockImplementation(async ({ offset = 0 } = {}) => ({
      files: Array.from({ length: offset < 100 ? 50 : 1 }, (_, i) => ({ id: String(offset + i) })) as Awaited<ReturnType<typeof api.listFileResources>>["files"],
      total: 101,
    }));
    const observer = new InfiniteQueryObserver(client, fileResourcesInfiniteOptions("ws"));
    await observer.refetch();
    expect(api.listFileResources).toHaveBeenCalledTimes(1);
    expect(api.listFileResources).toHaveBeenLastCalledWith({ limit: 50, offset: 0 });
    await observer.fetchNextPage();
    expect(api.listFileResources).toHaveBeenLastCalledWith({ limit: 50, offset: 50 });
    await observer.fetchNextPage();
    expect(api.listFileResources).toHaveBeenLastCalledWith({ limit: 50, offset: 100 });
    expect(observer.getCurrentResult().hasNextPage).toBe(false);
    expect(observer.getCurrentResult().data?.pages.flatMap((page) => page.files)).toHaveLength(101);
    client.clear();
  });

  it("isolates project pagination and stops on an empty response", async () => {
    const client = new QueryClient();
    vi.mocked(api.listProjectFiles).mockResolvedValue({ files: [], total: 100 });
    const observer = new InfiniteQueryObserver(client, fileResourcesInfiniteOptions("ws", "project"));
    await observer.refetch();
    expect(api.listProjectFiles).toHaveBeenCalledWith("project", { limit: 50, offset: 0 });
    expect(observer.getCurrentResult().hasNextPage).toBe(false);
    expect(fileResourcesInfiniteOptions("other", "project").queryKey).not.toEqual(fileResourcesInfiniteOptions("ws", "project").queryKey);
    client.clear();
  });
});
