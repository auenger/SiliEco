import { expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
const mockBrowse = vi.hoisted(() => vi.fn());
vi.mock("../api", () => ({ api: { browseProjectDirectory: mockBrowse } }));
import { projectPreviewOptions } from "./directory-queries";
it("preserves UTF-8 characters split across preview chunks", async () => {
 mockBrowse.mockResolvedValueOnce({mime:"text/markdown",content:btoa(String.fromCharCode(0xe4)),next_offset:1});
 mockBrowse.mockResolvedValueOnce({mime:"text/markdown",content:btoa(String.fromCharCode(0xbd,0xa0)),next_offset:0});
 const client=new QueryClient();
 const result=await client.fetchQuery(projectPreviewOptions("ws","project","resource","readme.md"));
 expect(result.content).toBe("你");
 expect(mockBrowse.mock.calls[1]?.[4]).toBe(1);
 client.clear();
});
