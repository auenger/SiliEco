import type { Attachment } from "./attachment";

export interface FileResource extends Attachment {
  source_task_title: string | null;
  source_task_number: number | null;
  source_project_id: string | null;
  source_project_title: string | null;
  reference_count: number;
}

export interface ListFileResourcesResponse {
  files: FileResource[];
  total: number;
}

export type AttachmentReferenceTargetType = "task" | "project";

export interface AttachmentReference {
  id: string;
  workspace_id: string;
  attachment_id: string;
  target_type: AttachmentReferenceTargetType;
  target_id: string;
  created_by: string;
  created_at: string;
  target_title: string | null;
  target_task_number: number | null;
  target_project_id: string | null;
  target_project_title: string | null;
}

export interface ListAttachmentReferencesResponse {
  references: AttachmentReference[];
  total: number;
}
