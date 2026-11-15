CREATE INDEX CONCURRENTLY idx_webhook_delivery_queue ON public.webhook_delivery USING btree (available_at, created_at) WHERE (status = 'queued'::text);
