CREATE UNIQUE INDEX CONCURRENTLY uq_agent_run_usage_hourly_key ON public.agent_run_usage_hourly (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model) NULLS NOT DISTINCT;
