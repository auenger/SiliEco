CREATE INDEX CONCURRENTLY idx_sys_cron_exec_running_stale ON public.sys_cron_executions USING btree (stale_after) WHERE (status = 'RUNNING'::text);
