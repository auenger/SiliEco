CREATE INDEX CONCURRENTLY idx_contact_sales_inquiry_created ON public.contact_sales_inquiry USING btree (created_at DESC);
