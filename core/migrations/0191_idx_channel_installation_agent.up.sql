CREATE INDEX CONCURRENTLY idx_channel_installation_agent ON public.channel_installation USING btree (agent_id);
