CREATE UNIQUE INDEX CONCURRENTLY idx_channel_installation_type_appid ON public.channel_installation USING btree (channel_type, ((config ->> 'app_id'::text)));
