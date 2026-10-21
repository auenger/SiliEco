import {z} from "zod";
const digest=z.string().regex(/^sha256:[0-9a-f]{64}$/);
export const SkillRevisionSchema=z.object({
 id:z.string().uuid(),workspace_id:z.string().uuid(),source_kind:z.string(),source_id:z.string().min(1),revision_hash:digest,
 bundle:z.object({id:z.string(),name:z.string(),content:z.string(),hash:digest}).loose(),
 manifest:z.object({schema_version:z.literal("silieco.skill-revision/v1"),hash:digest,bundle_hash:digest,size_bytes:z.number().int().nonnegative(),compatible_contracts:z.array(z.string()),tool_dependencies:z.array(z.object({name:z.string(),version:z.string()}).loose())}).loose(),
 created_by:z.string().uuid(),created_at:z.string(),
}).loose().refine(value=>value.revision_hash===value.manifest.hash && value.bundle.hash===value.manifest.bundle_hash && value.source_id===value.bundle.id,"Revision identity does not match its manifest");
export type SkillRevision=z.infer<typeof SkillRevisionSchema>;
export interface CreateSkillRevisionInput {
 content:string;
 files?:{path:string;content:string}[];
 compatible_contracts?:string[];
 tool_dependencies?:{name:string;version:string}[];
}

export const SkillRevisionSummarySchema=z.object({
 id:SkillRevisionSchema.shape.id,workspace_id:SkillRevisionSchema.shape.workspace_id,
 source_kind:SkillRevisionSchema.shape.source_kind,source_id:SkillRevisionSchema.shape.source_id,
 revision_hash:SkillRevisionSchema.shape.revision_hash,manifest:SkillRevisionSchema.shape.manifest,
 created_by:SkillRevisionSchema.shape.created_by,created_at:SkillRevisionSchema.shape.created_at,
}).loose().refine(value=>value.revision_hash===value.manifest.hash,"Revision summary digest mismatch");
export const SkillRevisionPageSchema=z.object({revisions:z.array(SkillRevisionSummarySchema).max(100),next_cursor:z.string().uuid().nullable()}).loose().refine(value=>new Set(value.revisions.map(item=>item.id)).size===value.revisions.length && (value.next_cursor===null || value.revisions.at(-1)?.id===value.next_cursor),"Invalid revision page cursor");
export type SkillRevisionSummary=z.infer<typeof SkillRevisionSummarySchema>;
export type SkillRevisionPage=z.infer<typeof SkillRevisionPageSchema>;
