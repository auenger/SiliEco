import {expect,it} from "vitest";
import {SkillRevisionSchema,SkillRevisionPageSchema} from "./skill-revision-schema";
const id="11111111-1111-4111-8111-111111111111";const hash=`sha256:${"a".repeat(64)}`;
const revision={id,workspace_id:id,source_kind:"workspace",source_id:id,revision_hash:hash,bundle:{id,name:"builder",content:"Candidate",hash},manifest:{schema_version:"silieco.skill-revision/v1",hash,bundle_hash:hash,size_bytes:9,compatible_contracts:[],tool_dependencies:[]},created_by:id,created_at:"2026-09-09"};
it("preserves validated revision content and future fields",()=>expect(SkillRevisionSchema.parse({...revision,future:true})).toMatchObject(revision));
it.each([{},null,{...revision,id:"wrong"},{...revision,revision_hash:"forged"},{...revision,source_id:"other"},{...revision,manifest:{...revision.manifest,hash:`sha256:${"b".repeat(64)}`}}, {...revision,bundle:{...revision.bundle,hash:`sha256:${"c".repeat(64)}`}}])("rejects malformed or mismatched revision %j",value=>expect(SkillRevisionSchema.safeParse(value).success).toBe(false));

const {bundle: _bundle,...summary}=revision;
it("accepts summary pages without distributing candidate content",()=>{expect(SkillRevisionPageSchema.parse({revisions:[summary],next_cursor:id}).revisions[0]).not.toHaveProperty("bundle");});
it.each([{revisions:[],next_cursor:id},{revisions:[summary,summary],next_cursor:null},{revisions:[summary],next_cursor:"22222222-2222-4222-8222-222222222222"}])("rejects malformed revision pagination %j",value=>expect(SkillRevisionPageSchema.safeParse(value).success).toBe(false));
