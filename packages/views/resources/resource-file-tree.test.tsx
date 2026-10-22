import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { FileResource } from "@silieco/core/types";
import enResources from "../locales/en/resources.json";
import { ResourceFileTree } from "./resource-file-tree";

function file(id: string, project: string, task: string): FileResource {
  return {
    id, workspace_id: "ws", filename: `${id}.png`, content_type: "image/png",
    size_bytes: 1, created_at: "2026-01-01", url: "", download_url: "", markdown_url: "",
    issue_id: task, comment_id: null, chat_session_id: null, chat_message_id: null,
    uploader_id: "user", uploader_type: "user", reference_count: 0,
    source_project_id: project, source_project_title: project,
    source_issue_title: task, source_issue_number: 1,
  };
}

const renderFiles = (files: FileResource[]) => <div>{files.map((item) => <span key={item.id}>{item.filename}</span>)}</div>;

function tree(files: FileResource[]) {
  return <I18nProvider locale="en" resources={{ en: { resources: enResources } }}>
    <ResourceFileTree files={files} renderFiles={renderFiles} />
  </I18nProvider>;
}

describe("ResourceFileTree", () => {
  it("mounts files only after expanding their project and Task, and unmounts on collapse", () => {
    render(tree([file("first", "Project A", "Design"), file("second", "Project B", "Build")]));
    expect(screen.queryByText("first.png")).toBeNull();
    expect(screen.queryByRole("button", { name: /Design/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Project A" }));
    expect(screen.queryByText("first.png")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Design/ }));
    expect(screen.getByText("first.png")).toBeInTheDocument();
    expect(screen.queryByText("second.png")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Design/ }));
    expect(screen.queryByText("first.png")).toBeNull();
  });

  it("keeps branches open as another page adds files to the same Task", () => {
    const first = file("first", "Project A", "Design");
    const { rerender } = render(tree([first]));
    fireEvent.click(screen.getByRole("button", { name: "Project A" }));
    fireEvent.click(screen.getByRole("button", { name: /Design/ }));
    rerender(tree([first, file("second", "Project A", "Design")]));
    expect(screen.getByText("first.png")).toBeInTheDocument();
    expect(screen.getByText("second.png")).toBeInTheDocument();
  });
});
