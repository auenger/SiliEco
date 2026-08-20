ALTER TABLE agent_task_queue
    ADD COLUMN worktree_branch TEXT NULL,
    ADD COLUMN worktree_base_sha TEXT NULL,
    ADD COLUMN worktree_final_sha TEXT NULL,
    ADD COLUMN worktree_delivery_status TEXT NULL;
