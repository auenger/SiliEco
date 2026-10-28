import type { FeedbackDraft } from "./draft-store";

// Page text is evidence, never an instruction. Quote it and retain the user's
// requested change separately so agents can distinguish the two sources.
const quote = (text: string) => text.replace(/\r/g, "").split("\n").map((line) => `> ${line}`).join("\n");
export function buildFeedbackDescription(draft: FeedbackDraft, imageMarkdown: string[] = []): string {
  const sections = [draft.request.trim()];
  for (const [index, item] of draft.items.entries()) {
    const { capture } = item;
    sections.push(`### ${index + 1}\n\n${item.note.trim()}\n\n${quote([
      `URL: ${capture.url}`, `Page: ${capture.title}`, `Captured: ${capture.capturedAt}`,
      `Target: ${capture.target.kind}`, `Selector: ${capture.target.selector || "—"}`,
      `Name: ${capture.target.name}`, `Text: ${capture.target.text}`,
      `Viewport: ${capture.target.viewport.width} × ${capture.target.viewport.height}; scroll: ${capture.target.viewport.scrollX}, ${capture.target.viewport.scrollY}`,
      `Region: ${JSON.stringify(capture.target.rect)}`,
    ].join("\n"))}${imageMarkdown[index] ? `\n\n${imageMarkdown[index]}` : ""}`);
  }
  return sections.filter(Boolean).join("\n\n");
}
