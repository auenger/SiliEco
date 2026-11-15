CREATE INDEX CONCURRENTLY idx_contact_sales_inquiry_email_created ON public.contact_sales_inquiry USING btree (business_email, created_at DESC);
