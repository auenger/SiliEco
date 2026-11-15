CREATE INDEX CONCURRENTLY idx_webhook_delivery_run ON public.webhook_delivery USING btree (autopilot_run_id) WHERE (autopilot_run_id IS NOT NULL);
