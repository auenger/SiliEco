CREATE UNIQUE INDEX CONCURRENTLY uq_sys_cron_execution ON public.sys_cron_executions (job_name, scope_kind, scope_id, plan_time);
