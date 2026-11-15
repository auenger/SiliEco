CREATE INDEX CONCURRENTLY workflow_review_claim_candidates_idx ON public.workflow_review_request USING btree (reviewer_id) WHERE (status = 'queued'::text);
