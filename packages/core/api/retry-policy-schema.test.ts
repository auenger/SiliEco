import {expect,it} from "vitest";
import {WorkflowStageSchema,WorkflowInstanceStageSchema} from "./schemas";
const stage={id:"stage",workspace_id:"workspace",workflow_version_id:"version",stable_key:"build",name:"Build",description:null,position:0,created_at:"2026-09-09"};
for(const schema of [WorkflowStageSchema,WorkflowInstanceStageSchema]) {
 it("preserves retry policy including explicit zero delay",()=>{const retry_policy={max_attempts:2,backoff_seconds:0};expect(schema.parse({...stage,retry_policy}).retry_policy).toEqual(retry_policy);expect(schema.parse(stage).retry_policy).toBeUndefined();});
 it.each([null,[],{max_attempts:0},{max_attempts:11},{max_attempts:"2"},{timeout_seconds:0},{backoff_multiplier:1.5}])("rejects malformed retry policy %j",retry_policy=>{expect(schema.safeParse({...stage,retry_policy}).success).toBe(false);});
}
