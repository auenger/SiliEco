import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { WorkspaceMCPServer } from "@silieco/core/types";
import enWorkflows from "../../locales/en/workflows.json";
import { StageMCPSelector } from "./stage-mcp-selector";

const servers: WorkspaceMCPServer[] = [
  {
    id: "mcp-1",
    workspace_id: "ws-1",
    name: "Documentation",
    configured: true,
    created_by: null,
    created_at: "2026-08-20T00:00:00Z",
    updated_at: "2026-08-20T00:00:00Z",
  },
  {
    id: "mcp-2",
    workspace_id: "ws-1",
    name: "Browser",
    configured: true,
    created_by: null,
    created_at: "2026-08-20T00:00:00Z",
    updated_at: "2026-08-20T00:00:00Z",
  },
];

describe("StageMCPSelector", () => {
  it("narrows a selected Stage allowlist without expanding agent access", () => {
    const onChange = vi.fn();
    render(
      <I18nProvider locale="en" resources={{ en: { workflows: enWorkflows } }}>
        <StageMCPSelector servers={servers} selectedIds={["mcp-1"]} onChange={onChange} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("checkbox", { name: "Allow MCP server Browser in this Stage" }));
    expect(onChange).toHaveBeenCalledWith(["mcp-1", "mcp-2"]);
    fireEvent.click(screen.getByRole("checkbox", { name: "Allow MCP server Documentation in this Stage" }));
    expect(onChange).toHaveBeenCalledWith([]);
  });
});
