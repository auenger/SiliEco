CREATE INDEX CONCURRENTLY idx_user_created_at ON public."user" USING btree (created_at);
