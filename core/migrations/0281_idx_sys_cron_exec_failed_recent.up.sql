CREATE INDEX CONCURRENTLY idx_sys_cron_exec_failed_recent ON public.sys_cron_executions USING btree (job_name, plan_time DESC) WHERE (status = 'FAILED'::text);
