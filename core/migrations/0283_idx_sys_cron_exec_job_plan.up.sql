CREATE INDEX CONCURRENTLY idx_sys_cron_exec_job_plan ON public.sys_cron_executions USING btree (job_name, scope_kind, scope_id, plan_time DESC);
