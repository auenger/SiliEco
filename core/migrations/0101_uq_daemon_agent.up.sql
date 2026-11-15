CREATE UNIQUE INDEX CONCURRENTLY uq_daemon_agent ON public.daemon_connection (agent_id, daemon_id);
