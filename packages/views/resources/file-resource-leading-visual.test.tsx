import { act, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Attachment } from "@silieco/core/types";
import { FileResourceLeadingVisual } from "./file-resource-leading-visual";

function makeFile(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: "att-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "user",
    uploader_id: "user-1",
    filename: "photo.png",
    url: "",
    download_url: "https://cdn.example.test/photo.png",
    markdown_url: "",
    content_type: "image/png",
    size_bytes: 10,
    created_at: "2026-08-04T00:00:00Z",
    ...overrides,
  };
}

describe("FileResourceLeadingVisual", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("does not request a thumbnail until its row intersects the viewport", () => {
    let notify: IntersectionObserverCallback = () => {};
    const disconnect = vi.fn();
    vi.stubGlobal("IntersectionObserver", class {
      constructor(callback: IntersectionObserverCallback) { notify = callback; }
      observe() {}
      disconnect = disconnect;
    });
    const { container } = render(<FileResourceLeadingVisual file={makeFile()} />);
    expect(container.querySelector("img")).toBeNull();
    act(() => notify([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver));
    expect(container.querySelector("img")?.src).toBe("https://cdn.example.test/photo.png");
    expect(disconnect).toHaveBeenCalled();
  });

  it("renders an image thumbnail for image files", () => {
    const { container } = render(<FileResourceLeadingVisual file={makeFile()} />);

    expect(container.querySelector('[data-file-visual="thumbnail"]')).not.toBeNull();
    expect(container.querySelector("img")?.getAttribute("src")).toBe(
      "https://cdn.example.test/photo.png",
    );
  });

  it("renders a type icon without an image element for non-image files", () => {
    const { container } = render(
      <FileResourceLeadingVisual
        file={makeFile({ filename: "report.xlsx", content_type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" })}
      />,
    );

    expect(container.querySelector('[data-file-visual="icon"]')).not.toBeNull();
    expect(container.querySelector("img")).toBeNull();
  });

  it("selects distinct icons for common file families", () => {
    const { container, rerender } = render(
      <FileResourceLeadingVisual
        file={makeFile({ filename: "archive.zip", content_type: "application/zip" })}
      />,
    );
    expect(container.querySelector(".lucide-file-archive")).not.toBeNull();

    rerender(
      <FileResourceLeadingVisual
        file={makeFile({ filename: "sheet.xlsx", content_type: "application/octet-stream" })}
      />,
    );
    expect(container.querySelector(".lucide-file-spreadsheet")).not.toBeNull();
  });
});
