CREATE UNIQUE INDEX CONCURRENTLY uq_autopilot_run_webhook_delivery ON public.autopilot_run USING btree (webhook_delivery_id) WHERE (webhook_delivery_id IS NOT NULL);
