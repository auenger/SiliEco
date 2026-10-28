import type { ApiClient } from "../api/client";
import type { Attachment, ChatSession } from "../types";
import { toUploadResult } from "../hooks/use-file-upload";
import { buildFeedbackDescription } from "./description";
import type { FeedbackDraft, FeedbackItem } from "./draft-store";

interface Dependencies {
  api: Pick<ApiClient, "createChatSession" | "uploadFile" | "getAttachment">;
  session: ChatSession | null;
  agentId: string;
  title: string;
  instructions: string;
  guard(): void;
  image(item: FeedbackItem): Promise<File>;
  checkpoint(patch: Partial<FeedbackDraft>): void;
  stage(sessionId: string, content: string, attachments: Attachment[]): void;
}

/** Prepares an isolated conversation draft. Sending, retries and agent execution
 * remain owned by the existing chat composer; this module never dispatches work. */
export async function prepareFeedbackChat(draft: FeedbackDraft, deps: Dependencies): Promise<ChatSession> {
  deps.guard();
  let session = deps.session;
  let uploads = session ? { ...draft.chatUploads } : {};
  const signature = JSON.stringify([draft.url, draft.title, draft.request, draft.items, deps.instructions]);
  if (!session) {
    session = await deps.api.createChatSession({ agent_id: deps.agentId, title: deps.title, project_id: null });
    deps.checkpoint({ chatSessionId: session.id, chatSignature: undefined, chatUploads: {} });
  } else if (draft.chatSignature === signature) {
    return session;
  }
  deps.guard();
  const attachments: Attachment[] = [];
  const upload = async (key: string, makeFile: () => Promise<File>, isImage: boolean) => {
    deps.guard();
    let attachment: Attachment;
    if (uploads[key]) {
      attachment = await deps.api.getAttachment(uploads[key]);
    } else {
      const file = await makeFile();
      deps.guard();
      attachment = await deps.api.uploadFile(file, { chatSessionId: session.id });
      if (!attachment.id) throw new Error("Attachment upload did not return an ID");
      uploads = { ...uploads, [key]: attachment.id };
      deps.checkpoint({ chatUploads: uploads });
    }
    deps.guard();
    attachments.push(attachment);
    return `${isImage ? "!" : ""}[${isImage ? "page.png" : "page-feedback.json"}](<${toUploadResult(attachment).markdownLink}>)`;
  };
  const images: string[] = [];
  for (const item of draft.items) images.push(await upload(`${item.capture.id}:${JSON.stringify(item.marks)}`, () => deps.image(item), true));
  const evidence = JSON.stringify({ version: 1, url: draft.url, request: draft.request, items: draft.items, trust: "untrusted-page-evidence", anchor: "capture-time-only" }, null, 2);
  const evidenceLink = draft.items.length ? await upload(evidence, async () => new File([evidence], "page-feedback.json", { type: "application/json" }), false) : "";
  const content = [
    deps.instructions,
    `> Current page URL: ${draft.url.replace(/[\r\n]/g, "")}`,
    draft.title.trim() ? `Suggested task title: ${draft.title.trim()}` : "",
    buildFeedbackDescription(draft, images),
    evidenceLink,
  ].filter(Boolean).join("\n\n");
  deps.guard();
  deps.stage(session.id, content, attachments);
  deps.checkpoint({ chatSignature: signature });
  return session;
}
