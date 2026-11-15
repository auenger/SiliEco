import { it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@silieco/core/i18n/react";
import enResources from "../../locales/en/resources.json";
vi.mock("@silieco/core/paths", () => ({ useWorkspacePaths: () => ({ projectFiles: (id: string) => `/workspace/projects/${id}/files` }) }));
vi.mock("../../navigation", () => ({ AppLink: ({ children, newTabTitle: _title, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { newTabTitle?: string }) => <a {...props}>{children}</a> }));
const mocks = vi.hoisted(() => ({ browse: vi.fn() }));
vi.mock("@silieco/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@silieco/core/api", () => ({
  api: {
    listProjectResources: async () => ({
      resources: [
        {
          id: "mapping",
          resource_type: "local_directory",
          resource_ref: { local_path: "/project", daemon_id: "mini" },
        },
      ],
    }),
    browseProjectDirectory: (...args: unknown[]) => mocks.browse(...args),
  },
}));
vi.mock("../../editor/html-block-preview", () => ({
  HtmlBlockPreview: ({ html }: { html: string }) => (
    <div data-testid="shared-html-preview">{html}</div>
  ),
}));
vi.mock("../../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="shared-markdown-preview">{content}</div>
  ),
}));
import { ProjectDirectoryBrowser } from "./project-directory-browser";
it("loads only expanded folders and previews only the selected file", async () => {
  mocks.browse.mockImplementation(
    async (
      _project: string,
      _resource: string,
      path: string,
      operation: string,
    ) => {
      if (operation === "preview")
        return {
          entries: [],
          next_offset: 0,
          mime: "text/html",
          content: btoa("<h1>Report</h1>"),
        };
      return {
        entries:
          path === ""
            ? [{ name: "docs", directory: true, size: 0 }]
            : [{ name: "report.html", directory: false, size: 16 }],
        next_offset: 0,
        content: "",
        mime: "",
      };
    },
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { resources: enResources } }}>
        <ProjectDirectoryBrowser projectId="project" />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await screen.findByText("docs");
  expect(screen.queryByRole("combobox")).toBeNull();
  expect(screen.getByRole("link", { name: "Open in new tab" }).getAttribute("href")).toBe("/workspace/projects/project/files");
  expect(mocks.browse).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByText("docs"));
  await screen.findByText("report.html");
  expect(mocks.browse).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByText("report.html"));
  await screen.findByTestId("shared-html-preview");
  expect(screen.getByTestId("shared-html-preview").textContent).toBe(
    "<h1>Report</h1>",
  );
  await waitFor(() => expect(mocks.browse).toHaveBeenCalledTimes(3));
  client.clear();
});
