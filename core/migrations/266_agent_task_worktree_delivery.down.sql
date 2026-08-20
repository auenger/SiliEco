ALTER TABLE agent_task_queue
    DROP COLUMN worktree_delivery_status,
    DROP COLUMN worktree_final_sha,
    DROP COLUMN worktree_base_sha,
    DROP COLUMN worktree_branch;
