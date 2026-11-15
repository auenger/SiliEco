CREATE INDEX CONCURRENTLY idx_autopilot_rule_version_active ON public.autopilot_rule_version USING btree (workspace_id, autopilot_id, created_at DESC);
