import { expect, it } from "vitest";
import { WorkflowAgentReviewStatusSchema, WorkflowAgentReviewRecoverySchema } from "./schemas";
const id="11111111-1111-4111-8111-111111111111";
const status={execution_id:id,phase:"failed",request_id:id,reviewer_id:id,result_revision:2,basis_hash:"a".repeat(64),call:{id,status:"failed",failure_reason:null,attempt:3,max_attempts:3},can_recover:true,recovery_attempts:3,retry_at:null};
it("fails closed on malformed review identity and missing fields",()=>{
 for(const value of [{},{...status,execution_id:"wrong"},{...status,basis_hash:"wrong"},{...status,call:{id,status:"failed"}}]) expect(WorkflowAgentReviewStatusSchema.safeParse(value).success).toBe(false);
 expect(WorkflowAgentReviewStatusSchema.parse({...status,can_recover:"true",recovery_attempts:99})).toMatchObject({can_recover:false,recovery_attempts:0});
 expect(WorkflowAgentReviewStatusSchema.parse({...status,phase:"future_phase",future:1}).phase).toBe("future_phase");
});
it("requires an explicit bounded recovery result",()=>{
 expect(WorkflowAgentReviewRecoverySchema.safeParse({}).success).toBe(false);
 const result={recovery_id:id,instance_id:id,execution_id:id,request_id:id,failed_task_id:id,task_id:id,allowed_attempts:3,max_attempts:6};
 expect(WorkflowAgentReviewRecoverySchema.safeParse(result).success).toBe(true);
 expect(WorkflowAgentReviewRecoverySchema.safeParse({...result,allowed_attempts:10}).success).toBe(false);
});
