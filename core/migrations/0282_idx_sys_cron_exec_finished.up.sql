CREATE INDEX CONCURRENTLY idx_sys_cron_exec_finished ON public.sys_cron_executions USING btree (finished_at) WHERE (status = ANY (ARRAY['SUCCESS'::text, 'FAILED'::text]));
