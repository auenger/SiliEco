import { expect,it } from "vitest";
import { WorkflowExecutionReworkSchema } from "./schemas";
const manual={rework_id:"record",execution_id:null,source_execution_id:"source",instance_id:"run",stage_id:"manual",attempt:null,status:"awaiting_work"};
it("accepts explicit manual rework without an agent execution",()=>{expect(WorkflowExecutionReworkSchema.parse(manual)).toEqual(manual);});
it.each([{...manual,rework_id:undefined},{...manual,execution_id:"fake"},{...manual,attempt:1},{...manual,status:"pending"}])("rejects incomplete or fabricated manual rework",value=>{expect(WorkflowExecutionReworkSchema.safeParse(value).success).toBe(false);});
