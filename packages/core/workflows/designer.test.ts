import { describe, expect, it } from "vitest";
import { parseWorkflowDraft, stripWorkflowDraft, workflowDesignerPrompt, workflowChatDisplayContent } from "./designer";

const draft = { name: "Requirements", description: "## Goal", stages: [{ id: "analysis", name: "Analyze", description: "Read sources", input: "Originals", output: "Cited requirements" }] };
const block = (value: unknown) => `<sop_draft>${JSON.stringify(value)}</sop_draft>`;

describe("SOP designer drafts", () => {
  it("accepts a complete text draft and new stages without IDs", () => {
    expect(parseWorkflowDraft(`Proposed changes\n${block(draft)}`)).toEqual(draft);
    expect(parseWorkflowDraft(block({ ...draft, stages: [{ name: "Review", description: "", input: "", output: "" }] }))).not.toBeNull();
  });
  it("rejects partial, ambiguous and oversized output instead of applying it", () => {
    expect(parseWorkflowDraft("<sop_draft>{")).toBeNull();
    expect(parseWorkflowDraft(block({ name: "Only a title" }))).toBeNull();
    expect(parseWorkflowDraft(block(draft) + block(draft))).toBeNull();
    expect(parseWorkflowDraft(block({ ...draft, stages: [draft.stages[0], draft.stages[0]] }))).toBeNull();
    expect(parseWorkflowDraft(block({ ...draft, description: "x".repeat(30001) }))).toBeNull();
  });
  it("rejects execution and permission changes in a text proposal", () => {
    expect(parseWorkflowDraft(block({ ...draft, stages: [{ ...draft.stages[0], executor_id: "other-agent", gate: {type:"none"} }] }))).toBeNull();
  });
  it("keeps the explanation visible while removing protocol content", () => {
    expect(stripWorkflowDraft(`Use two stages.\n${block(draft)}`)).toBe("Use two stages.");
    expect(stripWorkflowDraft("Working\n<sop_draft>{\"name\":")).toBe("Working");
  });
  it("provides the latest editor contents and prohibits publishing", () => {
    const prompt = workflowDesignerPrompt("Add acceptance criteria", draft);
    expect(prompt).toContain(JSON.stringify(draft));
    expect(prompt).toContain("USER REQUEST:\nAdd acceptance criteria");
    expect(prompt).toContain("do not use tools to create, publish");
  });
});

it("hides protocol payloads when reopening saved conversations", () => {
  expect(workflowChatDisplayContent(workflowDesignerPrompt("Add criteria\n\nSelected stage: analysis", draft))).toBe("Add criteria");
  expect(workflowChatDisplayContent(`Updated.\n${block(draft)}`)).toBe("Updated.");
  expect(workflowChatDisplayContent("Ordinary chat")).toBe("Ordinary chat");
});

it("accepts fenced replies with literal Markdown newlines without relaxing the schema", () => {
  const multiline = { ...draft, description: '## Goal\n\n- A\n- B\tC', stages: [{ ...draft.stages[0], output: 'A "quoted" title\nC:\\path' }] };
  const invalidJSON = JSON.stringify(multiline).replaceAll('\\n', '\n').replaceAll('\\t', '\t');
  expect(parseWorkflowDraft('<sop_draft>\n```json\n' + invalidJSON + '\n```\n</sop_draft>')).toEqual(multiline);
  expect(parseWorkflowDraft('<sop_draft>\n```json\n{"name":"bad",}\n```\n</sop_draft>')).toBeNull();
});
