CREATE INDEX CONCURRENTLY workflow_gate_decision_instance_index ON public.workflow_gate_decision USING btree (workflow_instance_id, created_at DESC);
