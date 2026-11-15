import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Link from "@tiptap/extension-link";
import { Markdown } from "@tiptap/markdown";
import { EditorView } from "@tiptap/pm/view";
import type { RefObject } from "react";
import { BaseMentionExtension } from "./mention-extension";
import { createMarkdownPasteExtension } from "./markdown-paste";
import {
  createTaskIdentifierAutolinkExtension,
  type TaskIdentifierResolver,
} from "./task-identifier-autolink";

const LinkExtension = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: false,
});

/** Count canonical task mentions in serialised markdown. */
function mentionCount(markdown: string): number {
  return markdown.match(/mention:\/\/task\//g)?.length ?? 0;
}

// The real mention extension renders via a React NodeView (TaskChip → workspace
// hooks) that needs providers we don't mount here. Swap in a plain-DOM NodeView
// so the node still serialises through the inherited renderMarkdown, without
// pulling React into the jsdom editor.
const TestMention = BaseMentionExtension.extend({
  addNodeView() {
    return () => ({ dom: document.createElement("span") });
  },
});

const resolveMock = vi.fn<TaskIdentifierResolver>();
const resolveRef: RefObject<TaskIdentifierResolver | undefined> = {
  current: (identifier) => resolveMock(identifier),
};

let editor: Editor | null = null;

function makeEditor(): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ link: false }),
      LinkExtension,
      TestMention,
      createTaskIdentifierAutolinkExtension({ resolveRef }),
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
      // The extension the real editor pastes through. It is a catch-all
      // `handlePaste` that dispatches its own transaction, so it — not
      // ProseMirror — decides whether this extension can tell a paste from
      // typing. Leaving it out is what let the paste cases below pass against a
      // hand-stamped meta the product never produced (SILI-5429).
      createMarkdownPasteExtension(),
    ],
  });
}

/** Flush the resolver microtask + the follow-up replacement dispatch. */
function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

/** Fire a real paste event, so the whole handlePaste chain runs. */
function paste(ed: Editor, text: string): void {
  ed.commands.focus("start");
  const event = new Event("paste", { bubbles: false, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      files: [],
      getData: (type: string) => (type === "text/plain" ? text : ""),
    },
  });
  ed.view.dom.dispatchEvent(event);
}

/** Simulate user typing `text` at position 1 (empty paragraph start). */
function typeAt1(ed: Editor, text: string): void {
  const tr = ed.state.tr.insertText(text, 1);
  ed.view.dispatch(tr);
}

beforeEach(() => {
  resolveMock.mockReset();
  // jsdom has no layout; ProseMirror's post-dispatch scroll needs client rects.
  (EditorView.prototype as unknown as { scrollToSelection: () => void }).scrollToSelection =
    () => {};
  Element.prototype.scrollIntoView = () => {};
});

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

describe("createTaskIdentifierAutolinkExtension", () => {
  it("converts a completed identifier into a canonical task mention", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-1", identifier: "SILI-1" });
    editor = makeEditor();

    // Typing "SILI-1 " leaves the caret right after the boundary space, which
    // completes the previous token.
    typeAt1(editor, "SILI-1 ");
    await flush();

    expect(resolveMock).toHaveBeenCalledWith("SILI-1");
    expect(editor.getMarkdown().trim()).toBe(
      "[SILI-1](mention://task/uuid-1)",
    );
  });

  it("leaves an unresolvable identifier as plain text", async () => {
    resolveMock.mockResolvedValue(null);
    editor = makeEditor();

    typeAt1(editor, "SILI-9 ");
    await flush();

    expect(resolveMock).toHaveBeenCalledWith("SILI-9");
    const md = editor.getMarkdown();
    expect(md).toContain("SILI-9");
    expect(md).not.toContain("mention://task");
  });

  it("converts identifiers found inside pasted text", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-2", identifier: "SILI-2" });
    editor = makeEditor();

    paste(editor, "See SILI-2 now");
    await flush();

    expect(resolveMock).toHaveBeenCalledWith("SILI-2");
    expect(editor.getMarkdown().trim()).toBe(
      "See [SILI-2](mention://task/uuid-2) now",
    );
  });

  it("does not convert content set programmatically (open ≠ rewrite)", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-3", identifier: "SILI-3" });
    editor = makeEditor();

    // setContent uses emitUpdate:false (preventUpdate) — the same path the real
    // editor uses on mount and WS-driven resets.
    editor.commands.setContent("SILI-3 stays", {
      emitUpdate: false,
      contentType: "markdown",
    });
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    expect(editor.getMarkdown()).toContain("SILI-3");
    expect(editor.getMarkdown()).not.toContain("mention://task");
  });

  it("does not convert an identifier inside inline code", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-4", identifier: "SILI-4" });
    editor = makeEditor();

    const codeMark = editor.schema.marks.code!.create();
    const tr = editor.state.tr.insert(
      1,
      editor.schema.text("SILI-4 ", [codeMark]),
    );
    editor.view.dispatch(tr);
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    expect(editor.getMarkdown()).not.toContain("mention://task");
  });

  it("does not fire while the identifier is still being typed (no boundary yet)", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-5", identifier: "SILI-5" });
    editor = makeEditor();

    typeAt1(editor, "SILI-5");
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    expect(editor.getMarkdown()).not.toContain("mention://task");
  });

  // --- blocker regressions: replace ONLY the captured range ---------------

  it("converts only the newly-typed occurrence, not a pre-existing identical one", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-1", identifier: "SILI-1" });
    editor = makeEditor();

    // Pre-existing SILI-1 arrives via a programmatic (preventUpdate) set — the
    // path the real editor uses on mount / WS resets — so it is not captured.
    editor.commands.setContent("old SILI-1 here", {
      emitUpdate: false,
      contentType: "markdown",
    });
    await flush();
    expect(resolveMock).not.toHaveBeenCalled();

    // User now types a brand-new SILI-1 at the end of the paragraph.
    const endOfPara = editor.state.doc.content.size - 1;
    editor.view.dispatch(editor.state.tr.insertText(" SILI-1 ", endOfPara));
    await flush();

    const md = editor.getMarkdown();
    // Exactly the new occurrence became a mention; the old one stays text.
    expect(mentionCount(md)).toBe(1);
    expect(md).toContain("old SILI-1 here");
    expect(md).toContain("mention://task/uuid-1");
  });

  it("paste converts identifiers inside the paste range but not identical ones outside it", async () => {
    resolveMock.mockImplementation(async (identifier) => {
      const map: Record<string, string> = {
        "SILI-2": "uuid-2",
        "SILI-3": "uuid-3",
        "SILI-9": "uuid-9",
      };
      const id = map[identifier];
      return id ? { id, identifier } : null;
    });
    editor = makeEditor();

    // Pre-existing (programmatic) SILI-9 outside any future paste range.
    editor.commands.setContent("keep SILI-9 outside", {
      emitUpdate: false,
      contentType: "markdown",
    });
    await flush();

    // Paste two identifiers at the very start of the doc.
    paste(editor, "SILI-2 plus SILI-3 x ");
    await flush();

    const md = editor.getMarkdown();
    // Both pasted identifiers converted; the outside SILI-9 did NOT.
    expect(md).toContain("mention://task/uuid-2");
    expect(md).toContain("mention://task/uuid-3");
    expect(md).not.toContain("mention://task/uuid-9");
    expect(md).toContain("SILI-9");
    expect(mentionCount(md)).toBe(2);
    expect(resolveMock).not.toHaveBeenCalledWith("SILI-9");
  });

  it("does not replace an identifier that already carries an explicit link mark", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-1", identifier: "SILI-1" });
    editor = makeEditor();

    // A link-marked "SILI-1" (e.g. an existing markdown link label) followed by
    // plain text, then the user types a boundary after it.
    const linkMark = editor.schema.marks.link!.create({ href: "https://x.test" });
    const linked = editor.schema.text("SILI-1", [linkMark]);
    const trailing = editor.schema.text(" ");
    const paragraph = editor.schema.nodes.paragraph!.create(null, [
      linked,
      trailing,
    ]);
    editor.view.dispatch(
      editor.state.tr.replaceWith(0, editor.state.doc.content.size, paragraph),
    );
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    const md = editor.getMarkdown();
    expect(md).not.toContain("mention://task");
    expect(md).toContain("https://x.test");
  });
});
