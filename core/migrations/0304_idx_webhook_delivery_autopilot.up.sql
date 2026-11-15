CREATE INDEX CONCURRENTLY idx_webhook_delivery_autopilot ON public.webhook_delivery USING btree (autopilot_id, created_at DESC);
