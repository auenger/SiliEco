import type { ApiClient } from "../api/client";
import { toUploadResult } from "../hooks/use-file-upload";
import type { FeedbackDraft, FeedbackItem } from "./draft-store";
import { buildFeedbackDescription } from "./description";

interface SubmitDependencies {
  api: Pick<ApiClient, "uploadFile" | "createTask" | "updateTask" | "listAttachments">;
  guard: () => void;
  image: (item: FeedbackItem) => Promise<File>;
  checkpoint: (patch: Partial<FeedbackDraft>) => void;
}
/** Checkpoint a created task before any further await, including workspace
 * changes. A retry binds missing evidence to that task instead of creating two. */
export async function submitFeedbackTask(draft: FeedbackDraft, deps: SubmitDependencies): Promise<string> {
  if (!draft.title.trim() || (!draft.request.trim() && !draft.items.length)) throw new Error("Add a title and a requested change");
  const uploads = { ...draft.uploads };
  const attachmentIds: string[] = [];
  const imageMarkdown: string[] = [];
  const upload = async (key: string, makeFile: () => Promise<File>, image: boolean) => {
    deps.guard();
    if (!uploads[key]) {
      const file = await makeFile();
      deps.guard();
      const result = await deps.api.uploadFile(file);
      if (!result.id) throw new Error("Attachment upload did not return an ID");
      const link = toUploadResult(result).markdownLink;
      uploads[key] = { id: result.id, markdown: `${image ? "!" : ""}[${file.name}](<${link}>)` };
      deps.checkpoint({ uploads });
    }
    attachmentIds.push(uploads[key].id);
    return uploads[key].markdown;
  };
  for (const item of draft.items) {
    imageMarkdown.push(await upload(`${item.capture.id}:${JSON.stringify(item.marks)}`, () => deps.image(item), true));
  }
  const evidence = JSON.stringify({ version: 1, request: draft.request, items: draft.items, trust: "untrusted-page-evidence", anchor: "capture-time-only" }, null, 2);
  const evidenceLink = draft.items.length ? await upload(evidence, async () => new File([evidence], "page-feedback.json", { type: "application/json" }), false) : "";
  let taskId = draft.taskId;
  if (!taskId) {
    deps.guard();
    const task = await deps.api.createTask({ title: draft.title.trim(), description: [buildFeedbackDescription(draft, imageMarkdown), evidenceLink].filter(Boolean).join("\n\n"), status: "backlog", attachment_ids: attachmentIds });
    taskId = task.id;
    deps.checkpoint({ taskId });
  }
  deps.guard();
  if (attachmentIds.length) {
    let attached = await deps.api.listAttachments(taskId);
    if (attachmentIds.some((id) => !attached.some((a) => a.id === id))) {
      deps.guard();
      await deps.api.updateTask(taskId, { attachment_ids: attachmentIds });
      deps.guard();
      attached = await deps.api.listAttachments(taskId);
      if (attachmentIds.some((id) => !attached.some((a) => a.id === id))) throw new Error("Task created; some attachments are still missing. Retry to finish attaching them.");
    }
  }
  return taskId;
}
