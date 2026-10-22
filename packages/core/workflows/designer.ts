import { z } from "zod";
const text = z.string().max(30000);
const StageTextSchema = z.object({
  id: z.string().max(128).optional(), name: z.string().trim().min(1).max(200),
  description: text, input: text, output: text,
}).strict();
export const WorkflowTextDraftSchema = z.object({
  name: z.string().trim().min(1).max(200), description: text,
  stages: z.array(StageTextSchema).min(1).max(40),
}).strict().refine((draft) => {
  const ids = draft.stages.flatMap((stage) => stage.id ? [stage.id] : []);
  return new Set(ids).size === ids.length;
});
export type WorkflowTextDraft = z.infer<typeof WorkflowTextDraftSchema>;

export function parseWorkflowDraft(content: string): WorkflowTextDraft | null {
  if (content.length > 1_000_000) return null;
  const blocks = [...content.matchAll(/<sop_draft>\s*([\s\S]*?)\s*<\/sop_draft>/g)];
  if (blocks.length !== 1) return null;
  try {
    const parsed = WorkflowTextDraftSchema.safeParse(JSON.parse(blocks[0]![1]!));
    return parsed.success ? parsed.data : null;
  } catch { return null; }
}

export function stripWorkflowDraft(content: string): string {
  return content.replace(/<sop_draft>[\s\S]*?(?:<\/sop_draft>|$)/g, "").trim();
}

export function workflowDesignerPrompt(request: string, draft: WorkflowTextDraft): string {
  return `Help design this SOP through conversation. Only propose text changes; do not use tools to create, publish, update resources, or modify project files. Preserve existing stage IDs and settings; omit id only for a genuinely new stage. Return a short explanation followed by exactly one <sop_draft> JSON block with {"name":"...","description":"Markdown","stages":[{"id":"existing id, omitted for new stages","name":"...","description":"Markdown goal and instructions","input":"Markdown input requirements","output":"Markdown output and acceptance standards"}]}. Every string must be valid JSON. Do not include execution, permission, reviewer, Skill or MCP settings. Do not delete existing stages. Reply in the user's language. The draft is for review in the editor, never claim it has been published.\n\nCURRENT DRAFT:\n${JSON.stringify(draft)}\n\nUSER REQUEST:\n${request}`;
}

/** Keep protocol payloads out of both the floating chat and saved chat history. */
export function workflowChatDisplayContent(content: string): string {
  if (content.startsWith("Help design this SOP through conversation.") && content.includes("\n\nCURRENT DRAFT:\n")) {
    return content.split("\n\nUSER REQUEST:\n").at(-1)?.split("\n\nSelected stage:")[0] ?? content;
  }
  return stripWorkflowDraft(content);
}
