CREATE INDEX CONCURRENTLY workflow_review_claim_candidates_idx ON workflow_review_request(reviewer_id) WHERE status='queued';
