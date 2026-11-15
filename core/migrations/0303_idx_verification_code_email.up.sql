CREATE INDEX CONCURRENTLY idx_verification_code_email ON public.verification_code USING btree (email, used, expires_at);
