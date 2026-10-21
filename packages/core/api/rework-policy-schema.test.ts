import {expect, it} from "vitest";
import {WorkflowStageSchema, WorkflowInstanceStageSchema} from "./schemas";
const stage={id:"stage",workspace_id:"workspace",workflow_version_id:"version",stable_key:"build",name:"Build",description:null,position:0,created_at:"2026-09-09"};
for(const schema of [WorkflowStageSchema,WorkflowInstanceStageSchema]) {
 it("preserves bounded rework policy and accepts omitted policy",()=>{
  expect(schema.parse({...stage,rework_policy:{max_reworks:0}}).rework_policy).toEqual({max_reworks:0});
  expect(schema.parse(stage).rework_policy).toBeUndefined();
 });
 it.each([null,[],{max_reworks:-1},{max_reworks:101},{max_reworks:1.5},{max_reworks:"3"}])("rejects malformed rework policy %j",rework_policy=>{
  expect(schema.safeParse({...stage,rework_policy}).success).toBe(false);
 });
}
