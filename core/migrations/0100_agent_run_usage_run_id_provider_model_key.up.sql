CREATE UNIQUE INDEX CONCURRENTLY agent_run_usage_run_id_provider_model_key ON public.agent_run_usage (run_id, provider, model);
