CREATE INDEX CONCURRENTLY agent_to_label_label_idx ON public.agent_to_label USING btree (label_id);
