CREATE UNIQUE INDEX CONCURRENTLY idx_autopilot_trigger_webhook_token ON public.autopilot_trigger USING btree (webhook_token) WHERE ((kind = 'webhook'::text) AND (webhook_token IS NOT NULL));
