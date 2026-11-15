-- Task domain schema generation 2. Fresh databases only.
-- Relationships and dependent cleanup are enforced by the application.

SET check_function_bodies = false;

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

CREATE FUNCTION public.clear_runtime_mcp_overlay_on_terminal_state() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.status IN ('completed', 'failed', 'cancelled')
       AND OLD.status IS DISTINCT FROM NEW.status
       AND (NEW.runtime_mcp_overlay IS NOT NULL OR NEW.runtime_connected_apps IS NOT NULL) THEN
        NEW.runtime_mcp_overlay := NULL;
        NEW.runtime_connected_apps := NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_atq() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.runtime_id IS DISTINCT FROM NEW.runtime_id
           OR OLD.task_id IS DISTINCT FROM NEW.task_id THEN
            -- OLD side. NULL runtime_id rows are not aggregated (no
            -- runtime → no bucket); skip those.
            IF OLD.runtime_id IS NOT NULL THEN
                INSERT INTO agent_run_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, provider, model
                )
                SELECT DISTINCT
                    agent_run_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    OLD.runtime_id,
                    OLD.agent_id,
                    i_old.project_id,
                    tu.provider,
                    tu.model
                  FROM agent_run_usage tu
                  JOIN agent a ON a.id = OLD.agent_id
                  LEFT JOIN task i_old ON i_old.id = OLD.task_id
                 WHERE tu.run_id = OLD.id
                ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_dirty_key DO UPDATE
                    SET enqueued_at = GREATEST(agent_run_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;

            IF NEW.runtime_id IS NOT NULL THEN
                INSERT INTO agent_run_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, provider, model
                )
                SELECT DISTINCT
                    agent_run_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    NEW.runtime_id,
                    NEW.agent_id,
                    i_new.project_id,
                    tu.provider,
                    tu.model
                  FROM agent_run_usage tu
                  JOIN agent a ON a.id = NEW.agent_id
                  LEFT JOIN task i_new ON i_new.id = NEW.task_id
                 WHERE tu.run_id = NEW.id
                ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_dirty_key DO UPDATE
                    SET enqueued_at = GREATEST(agent_run_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.runtime_id IS NOT NULL THEN
            INSERT INTO agent_run_usage_hourly_dirty (
                bucket_hour, workspace_id, runtime_id, agent_id,
                project_id, provider, model
            )
            SELECT DISTINCT
                agent_run_usage_hour_bucket(tu.created_at),
                a.workspace_id,
                OLD.runtime_id,
                OLD.agent_id,
                i.project_id,
                tu.provider,
                tu.model
              FROM agent_run_usage tu
              JOIN agent a ON a.id = OLD.agent_id
              LEFT JOIN task i ON i.id = OLD.task_id
             WHERE tu.run_id = OLD.id
            ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_dirty_key DO UPDATE
                SET enqueued_at = GREATEST(agent_run_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_task_delete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO agent_run_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, provider, model
    )
    SELECT DISTINCT
        agent_run_usage_hour_bucket(tu.created_at),
        OLD.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        OLD.project_id,
        tu.provider,
        tu.model
      FROM agent_run atq
      JOIN agent_run_usage tu ON tu.run_id = atq.id
     WHERE atq.task_id = OLD.id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(agent_run_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_task_project() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        -- OLD project buckets.
        INSERT INTO agent_run_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model
        )
        SELECT DISTINCT
            agent_run_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            OLD.project_id,
            tu.provider,
            tu.model
          FROM agent_run atq
          JOIN agent_run_usage tu ON tu.run_id = atq.id
         WHERE atq.task_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(agent_run_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);

        -- NEW project buckets.
        INSERT INTO agent_run_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model
        )
        SELECT DISTINCT
            agent_run_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            NEW.project_id,
            tu.provider,
            tu.model
          FROM agent_run atq
          JOIN agent_run_usage tu ON tu.run_id = atq.id
         WHERE atq.task_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(agent_run_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_tu() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO agent_run_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, provider, model
    )
    SELECT
        agent_run_usage_hour_bucket(OLD.created_at),
        a.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        i.project_id,
        OLD.provider,
        OLD.model
      FROM agent_run atq
      JOIN agent a ON a.id = atq.agent_id
      LEFT JOIN task i ON i.id = atq.task_id
     WHERE atq.id = OLD.run_id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(agent_run_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE FUNCTION public.prune_agent_run_usage_hourly_dirty(p_retention interval DEFAULT '7 days'::interval) RETURNS bigint
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    DELETE FROM agent_run_usage_hourly_dirty
     WHERE enqueued_at < now() - p_retention;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN v_rows;
END;
$$;

CREATE FUNCTION public.reject_skill_evaluation_suite_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'skill evaluation suites are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE FUNCTION public.reject_skill_improvement_proposal_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'skill improvement proposals are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE FUNCTION public.reject_skill_revision_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'skill revisions are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE FUNCTION public.rollup_agent_run_usage_hourly() RETURNS bigint
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_lock_ok BOOLEAN;
    v_from    TIMESTAMPTZ;
    v_to      TIMESTAMPTZ;
    v_rows    BIGINT := 0;
BEGIN
    SELECT pg_try_advisory_lock(4246) INTO v_lock_ok;
    IF NOT v_lock_ok THEN
        RETURN 0;
    END IF;

    BEGIN
        UPDATE agent_run_usage_hourly_rollup_state
           SET last_run_started_at = now(),
               last_error          = NULL
         WHERE id = 1
        RETURNING watermark_at INTO v_from;

        -- Cap each tick at a one-day window. In steady state v_from is
        -- recent, so LEAST picks `now() - 5 min` and nothing changes. But
        -- if the worker was paused (incident, migration freeze) the
        -- watermark can fall far behind; without the cap a single catch-up
        -- tick would recompute a multi-week window in one statement while
        -- holding lock 4246, blocking every other tick. Capped, catch-up
        -- advances in bounded one-day steps over successive ticks.
        v_to := LEAST(now() - INTERVAL '5 minutes', v_from + INTERVAL '1 day');

        IF v_from < v_to THEN
            v_rows := rollup_agent_run_usage_hourly_window(v_from, v_to);

            UPDATE agent_run_usage_hourly_rollup_state
               SET watermark_at         = v_to,
                   last_run_finished_at = now(),
                   last_run_rows        = v_rows
             WHERE id = 1;
        ELSE
            UPDATE agent_run_usage_hourly_rollup_state
               SET last_run_finished_at = now(),
                   last_run_rows        = 0
             WHERE id = 1;
        END IF;

        PERFORM pg_advisory_unlock(4246);
    EXCEPTION WHEN OTHERS THEN
        UPDATE agent_run_usage_hourly_rollup_state
           SET last_error           = SQLERRM,
               last_run_finished_at = now()
         WHERE id = 1;
        PERFORM pg_advisory_unlock(4246);
        RAISE;
    END;

    -- TTL prune. Runs AFTER the advisory lock is released: on a large
    -- stale backlog the prune can be slow, and holding lock 4246 through
    -- it would serialise every concurrent cron tick. It is a plain
    -- bounded DELETE — idempotent and safe to run unlocked.
    PERFORM prune_agent_run_usage_hourly_dirty();
    RETURN v_rows;
END;
$$;

CREATE FUNCTION public.rollup_agent_run_usage_hourly_window(p_from timestamp with time zone, p_to timestamp with time zone) RETURNS bigint
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    IF p_from >= p_to THEN
        RETURN 0;
    END IF;

    WITH
    dirty_from_updates AS (
        SELECT DISTINCT
            agent_run_usage_hour_bucket(tu.created_at) AS bucket_hour,
            a.workspace_id                        AS workspace_id,
            atq.runtime_id                        AS runtime_id,
            atq.agent_id                          AS agent_id,
            i.project_id                          AS project_id,
            tu.provider                           AS provider,
            tu.model                              AS model
          FROM agent_run_usage tu
          JOIN agent_run atq ON atq.id      = tu.run_id
          JOIN agent            a   ON a.id        = atq.agent_id
          LEFT JOIN task       i   ON i.id        = atq.task_id
         WHERE atq.runtime_id IS NOT NULL
           AND (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                -- Legacy updated_at-NULL rows; partial index from 078.
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    dirty_from_queue AS (
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, provider, model
          FROM agent_run_usage_hourly_dirty
         WHERE enqueued_at < p_to
    ),
    dirty_keys AS (
        SELECT * FROM dirty_from_updates
        UNION
        SELECT * FROM dirty_from_queue
    ),
    recomputed AS (
        SELECT
            dk.bucket_hour,
            dk.workspace_id,
            dk.runtime_id,
            dk.agent_id,
            dk.project_id,
            dk.provider,
            dk.model,
            SUM(tu.input_tokens)::bigint       AS input_tokens,
            SUM(tu.output_tokens)::bigint      AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint  AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
            -- Authoritative half: only rows the provider priced.
            COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
            -- Estimated half: tokens from rows the provider did not price.
            COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
            COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
            COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
            COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
            COUNT(DISTINCT tu.run_id)::bigint AS run_count,
            COUNT(*)::bigint                   AS event_count
          FROM dirty_keys dk
          JOIN agent_run atq ON atq.runtime_id  = dk.runtime_id
                                    AND atq.agent_id    = dk.agent_id
          JOIN agent            a   ON a.id            = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          LEFT JOIN task       i   ON i.id            = atq.task_id
          JOIN agent_run_usage       tu  ON tu.run_id      = atq.id
                                    AND tu.provider    = dk.provider
                                    AND tu.model       = dk.model
                                    AND agent_run_usage_hour_bucket(tu.created_at) = dk.bucket_hour
         WHERE (i.project_id IS NOT DISTINCT FROM dk.project_id)
         GROUP BY 1, 2, 3, 4, 5, 6, 7
    ),
    upserted AS (
        INSERT INTO agent_run_usage_hourly AS d (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            run_count, event_count
        )
        SELECT
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            run_count, event_count
          FROM recomputed
        ON CONFLICT ON CONSTRAINT uq_agent_run_usage_hourly_key DO UPDATE
            SET input_tokens                = EXCLUDED.input_tokens,
                output_tokens               = EXCLUDED.output_tokens,
                cache_read_tokens           = EXCLUDED.cache_read_tokens,
                cache_write_tokens          = EXCLUDED.cache_write_tokens,
                cost_usd_ticks              = EXCLUDED.cost_usd_ticks,
                uncosted_input_tokens       = EXCLUDED.uncosted_input_tokens,
                uncosted_output_tokens      = EXCLUDED.uncosted_output_tokens,
                uncosted_cache_read_tokens  = EXCLUDED.uncosted_cache_read_tokens,
                uncosted_cache_write_tokens = EXCLUDED.uncosted_cache_write_tokens,
                run_count                  = EXCLUDED.run_count,
                event_count                 = EXCLUDED.event_count,
                updated_at                  = now()
        RETURNING 1
    ),
    deleted_empty AS (
        DELETE FROM agent_run_usage_hourly d
         USING dirty_keys dk
         WHERE d.bucket_hour  = dk.bucket_hour
           AND d.workspace_id = dk.workspace_id
           AND d.runtime_id   = dk.runtime_id
           AND d.agent_id     = dk.agent_id
           AND d.project_id IS NOT DISTINCT FROM dk.project_id
           AND d.provider     = dk.provider
           AND d.model        = dk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_hour  = dk.bucket_hour
                  AND r.workspace_id = dk.workspace_id
                  AND r.runtime_id   = dk.runtime_id
                  AND r.agent_id     = dk.agent_id
                  AND r.project_id IS NOT DISTINCT FROM dk.project_id
                  AND r.provider     = dk.provider
                  AND r.model        = dk.model
           )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
      INTO v_rows;

    DELETE FROM agent_run_usage_hourly_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;

CREATE FUNCTION public.agent_run_usage_hour_bucket(ts timestamp with time zone) RETURNS timestamp with time zone
    LANGUAGE sql IMMUTABLE
    AS $$
    SELECT (date_trunc('hour', ts AT TIME ZONE 'UTC')) AT TIME ZONE 'UTC';
$$;

CREATE FUNCTION public.agent_run_usage_hourly_rollup_lag_seconds() RETURNS double precision
    LANGUAGE sql STABLE
    AS $$
    SELECT EXTRACT(EPOCH FROM (now() - last_run_finished_at))
      FROM agent_run_usage_hourly_rollup_state
     WHERE id = 1;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

CREATE TABLE public.activity_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    task_id uuid,
    actor_type text,
    actor_id uuid,
    action text NOT NULL,
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT activity_log_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text, 'system'::text])))
);

CREATE TABLE public.agent (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    avatar_url text,
    runtime_mode text NOT NULL,
    runtime_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    visibility text DEFAULT 'private'::text NOT NULL,
    status text DEFAULT 'offline'::text NOT NULL,
    max_concurrent_runs integer DEFAULT 6 NOT NULL,
    owner_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    runtime_id uuid,
    instructions text DEFAULT ''::text NOT NULL,
    archived_at timestamp with time zone,
    archived_by uuid,
    custom_env jsonb DEFAULT '{}'::jsonb NOT NULL,
    custom_args jsonb DEFAULT '[]'::jsonb NOT NULL,
    mcp_config jsonb,
    model text,
    thinking_level text,
    composio_toolkit_allowlist text[],
    permission_mode text DEFAULT 'private'::text NOT NULL,
    kind text DEFAULT 'user'::text NOT NULL,
    system_key text,
    disabled_runtime_skills jsonb DEFAULT '[]'::jsonb NOT NULL,
    service_tier text,
    CONSTRAINT agent_description_length CHECK ((char_length(description) <= 255)),
    CONSTRAINT agent_kind_check CHECK ((kind = ANY (ARRAY['user'::text, 'system'::text]))),
    CONSTRAINT agent_permission_mode_check CHECK ((permission_mode = ANY (ARRAY['private'::text, 'public_to'::text]))),
    CONSTRAINT agent_runtime_mode_check CHECK ((runtime_mode = ANY (ARRAY['local'::text, 'cloud'::text]))),
    CONSTRAINT agent_status_check CHECK ((status = ANY (ARRAY['idle'::text, 'working'::text, 'blocked'::text, 'error'::text, 'offline'::text]))),
    CONSTRAINT agent_visibility_check CHECK ((visibility = ANY (ARRAY['workspace'::text, 'private'::text])))
);

CREATE TABLE public.agent_invocation_target (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT agent_invocation_target_target_type_check CHECK ((target_type = ANY (ARRAY['workspace'::text, 'member'::text, 'team'::text])))
);

CREATE TABLE public.agent_mcp_server (
    agent_id uuid NOT NULL,
    server_id uuid NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.agent_runtime (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    daemon_id text,
    name text NOT NULL,
    runtime_mode text NOT NULL,
    provider text NOT NULL,
    status text DEFAULT 'offline'::text NOT NULL,
    device_info text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    owner_id uuid,
    legacy_daemon_id text,
    visibility text DEFAULT 'private'::text NOT NULL,
    profile_id uuid,
    custom_name text,
    CONSTRAINT agent_runtime_runtime_mode_check CHECK ((runtime_mode = ANY (ARRAY['local'::text, 'cloud'::text]))),
    CONSTRAINT agent_runtime_status_check CHECK ((status = ANY (ARRAY['online'::text, 'offline'::text]))),
    CONSTRAINT agent_runtime_visibility_check CHECK ((visibility = ANY (ARRAY['private'::text, 'public'::text])))
);

CREATE TABLE public.agent_skill (
    agent_id uuid NOT NULL,
    skill_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    enabled boolean DEFAULT true NOT NULL
);

CREATE TABLE public.agent_run (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    task_id uuid,
    status text DEFAULT 'queued'::text NOT NULL,
    priority integer DEFAULT 0 NOT NULL,
    dispatched_at timestamp with time zone,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    result jsonb,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    context jsonb,
    runtime_id uuid,
    session_id text,
    work_dir text,
    trigger_comment_id uuid,
    chat_session_id uuid,
    autopilot_run_id uuid,
    attempt integer DEFAULT 1 NOT NULL,
    max_attempts integer DEFAULT 2 NOT NULL,
    parent_run_id uuid,
    failure_reason text,
    trigger_summary text,
    force_fresh_session boolean DEFAULT false NOT NULL,
    is_leader_run boolean DEFAULT false NOT NULL,
    wait_reason text,
    initiator_user_id uuid,
    handoff_note text,
    prepare_lease_expires_at timestamp with time zone,
    squad_id uuid,
    runtime_mcp_overlay jsonb,
    escalation_for_run_id uuid,
    fire_at timestamp with time zone,
    originator_user_id uuid,
    runtime_connected_apps jsonb,
    coalesced_comment_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    delivered_comment_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    chat_input_run_id uuid,
    chat_finalize_deferred_at timestamp with time zone,
    originator_source text,
    delegated_from_run_id uuid,
    retry_of_run_id uuid,
    rerun_of_run_id uuid,
    rule_version_id uuid,
    trigger_evidence_kind text,
    trigger_evidence_ref_id uuid,
    accountable_user_id uuid,
    session_rollout_missing boolean DEFAULT false NOT NULL,
    retired_session_id text,
    trigger_chat_message_id uuid,
    execution_manifest jsonb,
    worktree_branch text,
    worktree_base_sha text,
    worktree_final_sha text,
    worktree_delivery_status text,
    workflow_stage_execution_id uuid,
    workflow_review_request_id uuid,
    CONSTRAINT agent_run_accountable_matches_originator CHECK (((originator_user_id IS NULL) OR ((accountable_user_id IS NOT NULL) AND (accountable_user_id = originator_user_id)))),
    CONSTRAINT agent_run_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'dispatched'::text, 'running'::text, 'completed'::text, 'failed'::text, 'cancelled'::text, 'waiting_local_directory'::text, 'deferred'::text]))),
    CONSTRAINT workflow_call_role_exclusive CHECK (((workflow_review_request_id IS NULL) OR (workflow_stage_execution_id IS NULL)))
);

CREATE TABLE public.agent_to_label (
    agent_id uuid NOT NULL,
    label_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.attachment (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    task_id uuid,
    comment_id uuid,
    uploader_type text NOT NULL,
    uploader_id uuid NOT NULL,
    filename text NOT NULL,
    url text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    chat_session_id uuid,
    chat_message_id uuid,
    run_id uuid,
    CONSTRAINT attachment_uploader_type_check CHECK ((uploader_type = ANY (ARRAY['member'::text, 'agent'::text])))
);

CREATE TABLE public.attachment_reference (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    attachment_id uuid NOT NULL,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT attachment_reference_target_type_check CHECK ((target_type = ANY (ARRAY['task'::text, 'project'::text])))
);

CREATE TABLE public.autopilot (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    assignee_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    execution_mode text DEFAULT 'create_task'::text NOT NULL,
    task_title_template text,
    created_by_type text NOT NULL,
    created_by_id uuid NOT NULL,
    last_run_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    assignee_type text DEFAULT 'agent'::text NOT NULL,
    project_id uuid,
    CONSTRAINT autopilot_assignee_type_check CHECK ((assignee_type = ANY (ARRAY['agent'::text, 'squad'::text]))),
    CONSTRAINT autopilot_created_by_type_check CHECK ((created_by_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT autopilot_execution_mode_check CHECK ((execution_mode = ANY (ARRAY['create_task'::text, 'run_only'::text]))),
    CONSTRAINT autopilot_status_check CHECK ((status = ANY (ARRAY['active'::text, 'paused'::text, 'archived'::text])))
);

CREATE TABLE public.autopilot_collaborator (
    autopilot_id uuid NOT NULL,
    user_type text NOT NULL,
    user_id uuid NOT NULL,
    granted_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT autopilot_collaborator_user_type_check CHECK ((user_type = 'member'::text))
);

CREATE TABLE public.autopilot_rule_version (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    autopilot_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    published_by_type text NOT NULL,
    published_by_id uuid,
    config_summary jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.autopilot_run (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    autopilot_id uuid NOT NULL,
    trigger_id uuid,
    source text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    task_id uuid,
    run_id uuid,
    triggered_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    failure_reason text,
    trigger_payload jsonb,
    result jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    squad_id uuid,
    planned_at timestamp with time zone,
    webhook_delivery_id uuid,
    CONSTRAINT autopilot_run_source_check CHECK ((source = ANY (ARRAY['schedule'::text, 'manual'::text, 'webhook'::text, 'api'::text]))),
    CONSTRAINT autopilot_run_status_check CHECK ((status = ANY (ARRAY['task_created'::text, 'running'::text, 'completed'::text, 'failed'::text, 'skipped'::text])))
);

CREATE TABLE public.autopilot_subscriber (
    autopilot_id uuid NOT NULL,
    user_type text NOT NULL,
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT autopilot_subscriber_user_type_check CHECK ((user_type = 'member'::text))
);

CREATE TABLE public.autopilot_trigger (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    autopilot_id uuid NOT NULL,
    kind text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    cron_expression text,
    timezone text DEFAULT 'UTC'::text,
    next_run_at timestamp with time zone,
    webhook_token text,
    label text,
    last_fired_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    provider text DEFAULT 'generic'::text NOT NULL,
    signing_secret text,
    event_filters jsonb,
    published_by_type text,
    published_by_id uuid,
    CONSTRAINT autopilot_trigger_kind_check CHECK ((kind = ANY (ARRAY['schedule'::text, 'webhook'::text, 'api'::text]))),
    CONSTRAINT autopilot_trigger_provider_check CHECK ((provider = ANY (ARRAY['generic'::text, 'github'::text])))
);

CREATE TABLE public.channel_binding_token (
    token_hash text NOT NULL,
    workspace_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    channel_type text NOT NULL,
    channel_user_id text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_binding_token_ttl_cap CHECK ((expires_at <= (created_at + '00:15:00'::interval)))
);

CREATE TABLE public.channel_chat_session_binding (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    channel_type text NOT NULL,
    channel_chat_id text NOT NULL,
    chat_type text NOT NULL,
    last_message_id text,
    last_thread_id text,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_chat_session_binding_chat_type_check CHECK ((chat_type = ANY (ARRAY['p2p'::text, 'group'::text])))
);

CREATE TABLE public.channel_inbound_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    installation_id uuid,
    channel_type text NOT NULL,
    channel_chat_id text,
    event_type text NOT NULL,
    channel_event_id text,
    channel_message_id text,
    drop_reason text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.channel_inbound_message_dedup (
    installation_id uuid NOT NULL,
    message_id text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone,
    claim_token uuid DEFAULT gen_random_uuid() NOT NULL
);

CREATE TABLE public.channel_installation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    channel_type text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    ws_lease_token text,
    ws_lease_expires_at timestamp with time zone,
    installer_user_id uuid NOT NULL,
    installed_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_installation_status_check CHECK ((status = ANY (ARRAY['active'::text, 'revoked'::text])))
);

CREATE TABLE public.channel_media_pending_object (
    storage_key text NOT NULL,
    workspace_id uuid NOT NULL,
    chat_message_id uuid NOT NULL,
    storage_url text NOT NULL,
    installation_id uuid,
    state text DEFAULT 'pending'::text NOT NULL,
    lease_token uuid,
    lease_expires_at timestamp with time zone,
    attempt integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    last_error text,
    tombstone_pass integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_media_pending_object_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'deleting'::text, 'tombstoned'::text])))
);

CREATE TABLE public.channel_outbound_card_message (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    run_id uuid,
    channel_type text NOT NULL,
    channel_chat_id text NOT NULL,
    channel_card_message_id text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    last_patched_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_outbound_card_message_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'streaming'::text, 'final'::text, 'error'::text])))
);

CREATE TABLE public.channel_user_binding (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    silieco_user_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    channel_type text NOT NULL,
    channel_user_id text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    bound_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.chat_draft_restore (
    id uuid NOT NULL,
    chat_session_id uuid NOT NULL,
    run_id uuid NOT NULL,
    content text NOT NULL,
    attachment_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.chat_message (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    run_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    failure_reason text,
    elapsed_ms bigint,
    message_kind text DEFAULT 'message'::text NOT NULL,
    channel_media_pending_until timestamp with time zone,
    channel_ingested boolean DEFAULT false NOT NULL,
    sender_user_id uuid,
    sender_agent_id uuid,
    mentioned_agent_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    CONSTRAINT chat_message_role_check CHECK ((role = ANY (ARRAY['user'::text, 'assistant'::text])))
);

CREATE TABLE public.chat_pinned_agent (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.chat_session (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    agent_id uuid,
    creator_id uuid NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    session_id text,
    work_dir text,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    unread_since timestamp with time zone,
    runtime_id uuid,
    last_read_at timestamp with time zone DEFAULT now() NOT NULL,
    is_agent_intro boolean DEFAULT false NOT NULL,
    pinned_at timestamp with time zone,
    project_id uuid,
    kind text DEFAULT 'direct'::text NOT NULL,
    CONSTRAINT chat_session_kind_check CHECK ((kind = ANY (ARRAY['direct'::text, 'group'::text]))),
    CONSTRAINT chat_session_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text])))
);

CREATE TABLE public.chat_session_participant (
    chat_session_id uuid NOT NULL,
    participant_type text NOT NULL,
    participant_id uuid NOT NULL,
    created_by uuid NOT NULL,
    last_read_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chat_session_participant_participant_type_check CHECK ((participant_type = ANY (ARRAY['member'::text, 'agent'::text])))
);

CREATE TABLE public.client_usage_daily (
    user_id uuid NOT NULL,
    client_type text NOT NULL,
    install_id uuid NOT NULL,
    activity_date date NOT NULL,
    workspace_id uuid,
    client_version text NOT NULL,
    os text NOT NULL,
    first_active_at timestamp with time zone NOT NULL,
    last_active_at timestamp with time zone NOT NULL,
    runtime_probed_at timestamp with time zone,
    probe_result text,
    runtime_count integer,
    provider_summary jsonb,
    online_count integer,
    offline_count integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT client_usage_daily_check CHECK ((first_active_at <= last_active_at)),
    CONSTRAINT client_usage_daily_check1 CHECK ((((runtime_probed_at IS NULL) AND (probe_result IS NULL) AND (runtime_count IS NULL) AND (provider_summary IS NULL) AND (online_count IS NULL) AND (offline_count IS NULL)) OR ((runtime_probed_at IS NOT NULL) AND (probe_result = 'error'::text) AND (runtime_count IS NULL) AND (provider_summary IS NULL) AND (online_count IS NULL) AND (offline_count IS NULL)) OR ((runtime_probed_at IS NOT NULL) AND (probe_result = 'success'::text) AND (runtime_count IS NOT NULL) AND (provider_summary IS NOT NULL) AND (online_count IS NOT NULL) AND (offline_count IS NOT NULL) AND ((online_count + offline_count) = runtime_count) AND (jsonb_typeof(provider_summary) = 'object'::text)))),
    CONSTRAINT client_usage_daily_client_type_check CHECK ((client_type = ANY (ARRAY['web'::text, 'desktop'::text]))),
    CONSTRAINT client_usage_daily_offline_count_check CHECK ((offline_count >= 0)),
    CONSTRAINT client_usage_daily_online_count_check CHECK ((online_count >= 0)),
    CONSTRAINT client_usage_daily_probe_result_check CHECK ((probe_result = ANY (ARRAY['success'::text, 'error'::text]))),
    CONSTRAINT client_usage_daily_runtime_count_check CHECK ((runtime_count >= 0))
);

CREATE TABLE public.comment (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    author_type text NOT NULL,
    author_id uuid NOT NULL,
    content text NOT NULL,
    type text DEFAULT 'comment'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    parent_id uuid,
    workspace_id uuid NOT NULL,
    resolved_at timestamp with time zone,
    resolved_by_type text,
    resolved_by_id uuid,
    source_run_id uuid,
    CONSTRAINT comment_author_type_check CHECK ((author_type = ANY (ARRAY['member'::text, 'agent'::text, 'system'::text]))),
    CONSTRAINT comment_resolved_consistency CHECK ((((resolved_at IS NULL) AND (resolved_by_type IS NULL) AND (resolved_by_id IS NULL)) OR ((resolved_at IS NOT NULL) AND (resolved_by_type IS NOT NULL) AND (resolved_by_id IS NOT NULL)))),
    CONSTRAINT comment_type_check CHECK ((type = ANY (ARRAY['comment'::text, 'status_change'::text, 'progress_update'::text, 'system'::text])))
);

CREATE TABLE public.comment_reaction (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    comment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    actor_type text NOT NULL,
    actor_id uuid NOT NULL,
    emoji text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT comment_reaction_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text])))
);

CREATE TABLE public.contact_sales_inquiry (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    business_email text NOT NULL,
    company_name text NOT NULL,
    company_size text NOT NULL,
    country_region text NOT NULL,
    use_case text NOT NULL,
    goals text DEFAULT ''::text NOT NULL,
    consent_outreach boolean DEFAULT false NOT NULL,
    consent_updates boolean DEFAULT false NOT NULL,
    submitter_ip inet,
    user_agent text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.daemon_connection (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    daemon_id text NOT NULL,
    status text DEFAULT 'disconnected'::text NOT NULL,
    last_heartbeat_at timestamp with time zone,
    runtime_info jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT daemon_connection_status_check CHECK ((status = ANY (ARRAY['connected'::text, 'disconnected'::text])))
);

CREATE TABLE public.daemon_token (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    token_hash text NOT NULL,
    workspace_id uuid NOT NULL,
    daemon_id text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.feedback (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    workspace_id uuid,
    message text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.github_installation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    installation_id bigint NOT NULL,
    account_login text NOT NULL,
    account_type text DEFAULT 'User'::text NOT NULL,
    account_avatar_url text,
    connected_by_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT github_installation_account_type_check CHECK ((account_type = ANY (ARRAY['User'::text, 'Organization'::text])))
);

CREATE TABLE public.github_pending_check_suite (
    workspace_id uuid NOT NULL,
    installation_id bigint NOT NULL,
    repo_owner text NOT NULL,
    repo_name text NOT NULL,
    pr_number integer NOT NULL,
    suite_id bigint NOT NULL,
    head_sha text NOT NULL,
    app_id bigint NOT NULL,
    conclusion text,
    status text NOT NULL,
    suite_updated_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.github_pending_installation (
    installation_id bigint NOT NULL,
    account_login text NOT NULL,
    account_type text DEFAULT 'User'::text NOT NULL,
    account_avatar_url text,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT github_pending_installation_account_login_check CHECK ((account_login <> ''::text)),
    CONSTRAINT github_pending_installation_account_type_check CHECK ((account_type = ANY (ARRAY['User'::text, 'Organization'::text])))
);

CREATE TABLE public.github_pull_request (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    installation_id bigint NOT NULL,
    repo_owner text NOT NULL,
    repo_name text NOT NULL,
    pr_number integer NOT NULL,
    title text NOT NULL,
    state text NOT NULL,
    html_url text NOT NULL,
    branch text,
    author_login text,
    author_avatar_url text,
    merged_at timestamp with time zone,
    closed_at timestamp with time zone,
    pr_created_at timestamp with time zone NOT NULL,
    pr_updated_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    head_sha text DEFAULT ''::text NOT NULL,
    mergeable_state text,
    additions integer DEFAULT 0 NOT NULL,
    deletions integer DEFAULT 0 NOT NULL,
    changed_files integer DEFAULT 0 NOT NULL,
    api_mergeable text,
    api_merge_state_status text,
    checks_rollup_state text,
    snapshot_head_sha text DEFAULT ''::text NOT NULL,
    snapshot_fetched_at timestamp with time zone,
    CONSTRAINT github_pull_request_state_check CHECK ((state = ANY (ARRAY['open'::text, 'closed'::text, 'merged'::text, 'draft'::text])))
);

CREATE TABLE public.github_pull_request_check_run (
    pr_id uuid NOT NULL,
    head_sha text NOT NULL,
    ordinal integer NOT NULL,
    name text NOT NULL,
    status text NOT NULL,
    conclusion text,
    details_url text,
    is_status_context boolean DEFAULT false NOT NULL
);

CREATE TABLE public.github_pull_request_check_suite (
    pr_id uuid NOT NULL,
    suite_id bigint NOT NULL,
    head_sha text NOT NULL,
    app_id bigint NOT NULL,
    conclusion text,
    status text NOT NULL,
    updated_at timestamp with time zone NOT NULL
);

CREATE TABLE public.inbox_item (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    recipient_type text NOT NULL,
    recipient_id uuid NOT NULL,
    type text NOT NULL,
    severity text DEFAULT 'info'::text NOT NULL,
    task_id uuid,
    title text NOT NULL,
    body text,
    read boolean DEFAULT false NOT NULL,
    archived boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_type text,
    actor_id uuid,
    details jsonb DEFAULT '{}'::jsonb,
    CONSTRAINT inbox_item_recipient_type_check CHECK ((recipient_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT inbox_item_severity_check CHECK ((severity = ANY (ARRAY['action_required'::text, 'attention'::text, 'info'::text])))
);

CREATE TABLE public.task (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    status text DEFAULT 'backlog'::text NOT NULL,
    priority text DEFAULT 'none'::text NOT NULL,
    assignee_type text,
    assignee_id uuid,
    creator_type text NOT NULL,
    creator_id uuid NOT NULL,
    parent_task_id uuid,
    acceptance_criteria jsonb DEFAULT '[]'::jsonb NOT NULL,
    context_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    due_date date,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    number integer DEFAULT 0 NOT NULL,
    project_id uuid,
    origin_type text,
    origin_id uuid,
    first_executed_at timestamp with time zone,
    start_date date,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    stage integer,
    properties jsonb DEFAULT '{}'::jsonb NOT NULL,
    workflow_instance_id uuid,
    workflow_stage_id uuid,
    workflow_stage_execution_id uuid,
    workflow_invalidated_by_rework_id uuid,
    CONSTRAINT task_assignee_type_check CHECK ((assignee_type = ANY (ARRAY['member'::text, 'agent'::text, 'squad'::text]))),
    CONSTRAINT task_creator_type_check CHECK ((creator_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT task_metadata_is_object CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT task_metadata_size_limit CHECK ((pg_column_size(metadata) <= 8192)),
    CONSTRAINT task_origin_type_check CHECK ((origin_type = ANY (ARRAY['autopilot'::text, 'quick_create'::text, 'lark_chat'::text, 'slack_chat'::text, 'agent_create'::text]))),
    CONSTRAINT task_priority_check CHECK ((priority = ANY (ARRAY['urgent'::text, 'high'::text, 'medium'::text, 'low'::text, 'none'::text]))),
    CONSTRAINT task_properties_is_object CHECK ((jsonb_typeof(properties) = 'object'::text)),
    CONSTRAINT task_properties_size_limit CHECK ((pg_column_size(properties) <= 16384)),
    CONSTRAINT task_stage_check CHECK (((stage IS NULL) OR (stage >= 1))),
    CONSTRAINT task_status_check CHECK ((status = ANY (ARRAY['backlog'::text, 'todo'::text, 'in_progress'::text, 'in_review'::text, 'done'::text, 'blocked'::text, 'cancelled'::text]))),
    CONSTRAINT task_workflow_stage_pair_check CHECK ((((workflow_instance_id IS NULL) AND (workflow_stage_id IS NULL)) OR ((workflow_instance_id IS NOT NULL) AND (workflow_stage_id IS NOT NULL))))
);

CREATE TABLE public.task_dependency (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    depends_on_task_id uuid NOT NULL,
    type text NOT NULL,
    CONSTRAINT task_dependency_type_check CHECK ((type = ANY (ARRAY['blocks'::text, 'blocked_by'::text, 'related'::text])))
);

CREATE TABLE public.task_label (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    color text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    resource_type text DEFAULT 'task'::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    CONSTRAINT task_label_resource_type_check CHECK ((resource_type = ANY (ARRAY['task'::text, 'agent'::text, 'skill'::text])))
);

CREATE TABLE public.task_property (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    type text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    archived_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    icon text DEFAULT ''::text NOT NULL,
    CONSTRAINT task_property_config_check CHECK ((jsonb_typeof(config) = 'object'::text)),
    CONSTRAINT task_property_type_check CHECK ((type = ANY (ARRAY['text'::text, 'number'::text, 'select'::text, 'multi_select'::text, 'date'::text, 'checkbox'::text, 'url'::text])))
);

CREATE TABLE public.task_pull_request (
    task_id uuid NOT NULL,
    pull_request_id uuid NOT NULL,
    linked_by_type text,
    linked_by_id uuid,
    linked_at timestamp with time zone DEFAULT now() NOT NULL,
    close_intent boolean DEFAULT false NOT NULL,
    reference_only boolean DEFAULT false NOT NULL
);

CREATE TABLE public.task_reaction (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    actor_type text NOT NULL,
    actor_id uuid NOT NULL,
    emoji text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT task_reaction_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text])))
);

CREATE TABLE public.task_subscriber (
    task_id uuid NOT NULL,
    user_type text NOT NULL,
    user_id uuid NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT task_subscriber_reason_check CHECK ((reason = ANY (ARRAY['creator'::text, 'assignee'::text, 'commenter'::text, 'mentioned'::text, 'manual'::text, 'autopilot'::text]))),
    CONSTRAINT task_subscriber_user_type_check CHECK ((user_type = ANY (ARRAY['member'::text, 'agent'::text])))
);

CREATE TABLE public.task_to_label (
    task_id uuid NOT NULL,
    label_id uuid NOT NULL
);

CREATE TABLE public.task_vcs_pull_request (
    task_id uuid NOT NULL,
    pull_request_id uuid NOT NULL,
    close_intent boolean DEFAULT false NOT NULL,
    reference_only boolean DEFAULT false NOT NULL,
    linked_by_type text,
    linked_by_id uuid,
    linked_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.lark_binding_token (
    token_hash text NOT NULL,
    workspace_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    lark_open_id text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT lark_binding_token_ttl_cap CHECK ((expires_at <= (created_at + '00:15:00'::interval)))
);

CREATE TABLE public.lark_chat_session_binding (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    lark_chat_id text NOT NULL,
    lark_chat_type text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_lark_message_id text,
    last_lark_thread_id text,
    CONSTRAINT lark_chat_session_binding_lark_chat_type_check CHECK ((lark_chat_type = ANY (ARRAY['p2p'::text, 'group'::text])))
);

CREATE TABLE public.lark_inbound_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    installation_id uuid,
    lark_chat_id text,
    event_type text NOT NULL,
    lark_event_id text,
    lark_message_id text,
    drop_reason text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.lark_inbound_message_dedup (
    installation_id uuid NOT NULL,
    message_id text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone,
    claim_token uuid DEFAULT gen_random_uuid() NOT NULL
);

CREATE TABLE public.lark_installation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    app_id text NOT NULL,
    app_secret_encrypted bytea NOT NULL,
    tenant_key text,
    bot_open_id text NOT NULL,
    installer_user_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    ws_lease_token text,
    ws_lease_expires_at timestamp with time zone,
    installed_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    bot_union_id text,
    region text DEFAULT 'feishu'::text NOT NULL,
    CONSTRAINT lark_installation_region_check CHECK ((region = ANY (ARRAY['feishu'::text, 'lark'::text]))),
    CONSTRAINT lark_installation_status_check CHECK ((status = ANY (ARRAY['active'::text, 'revoked'::text])))
);

CREATE TABLE public.lark_outbound_card_message (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    run_id uuid,
    lark_chat_id text NOT NULL,
    lark_card_message_id text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    last_patched_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT lark_outbound_card_message_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'streaming'::text, 'final'::text, 'error'::text])))
);

CREATE TABLE public.lark_user_binding (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    silieco_user_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    lark_open_id text NOT NULL,
    union_id text,
    bound_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.member (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT member_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'member'::text])))
);

CREATE TABLE public.notification_preference (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    preferences jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.personal_access_token (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    revoked boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.pinned_item (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    item_type text NOT NULL,
    item_id uuid NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT pinned_item_item_type_check CHECK ((item_type = ANY (ARRAY['task'::text, 'project'::text])))
);

CREATE TABLE public.project (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    icon text,
    status text DEFAULT 'planned'::text NOT NULL,
    lead_type text,
    lead_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    priority text DEFAULT 'none'::text NOT NULL,
    start_date date,
    due_date date,
    CONSTRAINT project_lead_type_check CHECK ((lead_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT project_priority_check CHECK ((priority = ANY (ARRAY['urgent'::text, 'high'::text, 'medium'::text, 'low'::text, 'none'::text]))),
    CONSTRAINT project_status_check CHECK ((status = ANY (ARRAY['planned'::text, 'in_progress'::text, 'paused'::text, 'completed'::text, 'cancelled'::text])))
);

CREATE TABLE public.project_resource (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    resource_type text NOT NULL,
    resource_ref jsonb NOT NULL,
    label text,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

CREATE TABLE public.runtime_profile (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    display_name text NOT NULL,
    protocol_family text NOT NULL,
    command_name text NOT NULL,
    description text,
    fixed_args jsonb DEFAULT '[]'::jsonb NOT NULL,
    visibility text DEFAULT 'workspace'::text NOT NULL,
    created_by uuid,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT runtime_profile_visibility_check CHECK ((visibility = ANY (ARRAY['workspace'::text, 'private'::text])))
);

CREATE TABLE public.skill (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.skill_evaluation_suite (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    skill_id uuid NOT NULL,
    name text NOT NULL,
    suite_hash text NOT NULL,
    schema_version text NOT NULL,
    validator_version text NOT NULL,
    definition bytea NOT NULL,
    improvement_count integer NOT NULL,
    regression_count integer NOT NULL,
    held_out_count integer NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT skill_evaluation_suite_check CHECK (((((improvement_count + regression_count) + held_out_count) >= 1) AND (((improvement_count + regression_count) + held_out_count) <= 1000))),
    CONSTRAINT skill_evaluation_suite_definition_check CHECK (((octet_length(definition) >= 1) AND (octet_length(definition) <= 8388608))),
    CONSTRAINT skill_evaluation_suite_held_out_count_check CHECK ((held_out_count >= 0)),
    CONSTRAINT skill_evaluation_suite_improvement_count_check CHECK ((improvement_count >= 0)),
    CONSTRAINT skill_evaluation_suite_name_check CHECK (((length(name) >= 1) AND (length(name) <= 256))),
    CONSTRAINT skill_evaluation_suite_regression_count_check CHECK ((regression_count >= 0)),
    CONSTRAINT skill_evaluation_suite_suite_hash_check CHECK ((suite_hash ~ '^sha256:[0-9a-f]{64}$'::text))
);

CREATE TABLE public.skill_file (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    skill_id uuid NOT NULL,
    path text NOT NULL,
    content text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.skill_improvement_proposal (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    skill_id uuid NOT NULL,
    baseline_revision_id uuid NOT NULL,
    candidate_revision_id uuid NOT NULL,
    request_id uuid NOT NULL,
    request_hash text NOT NULL,
    category text NOT NULL,
    reason text NOT NULL,
    hypothesis text NOT NULL,
    applicable_scope text NOT NULL,
    evidence jsonb NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT skill_improvement_proposal_applicable_scope_check CHECK (((length(applicable_scope) >= 1) AND (length(applicable_scope) <= 4000))),
    CONSTRAINT skill_improvement_proposal_category_check CHECK ((category = ANY (ARRAY['skill_instruction'::text, 'insufficient_input'::text, 'tool_failure'::text, 'contract_task'::text, 'model_capability'::text, 'human_correction'::text, 'positive_example'::text]))),
    CONSTRAINT skill_improvement_proposal_check CHECK ((baseline_revision_id <> candidate_revision_id)),
    CONSTRAINT skill_improvement_proposal_evidence_check CHECK ((jsonb_typeof(evidence) = 'object'::text)),
    CONSTRAINT skill_improvement_proposal_hypothesis_check CHECK (((length(hypothesis) >= 1) AND (length(hypothesis) <= 8000))),
    CONSTRAINT skill_improvement_proposal_reason_check CHECK (((length(reason) >= 1) AND (length(reason) <= 8000))),
    CONSTRAINT skill_improvement_proposal_request_hash_check CHECK ((request_hash ~ '^sha256:[0-9a-f]{64}$'::text))
);

CREATE TABLE public.skill_revision (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    source_kind text NOT NULL,
    source_id text NOT NULL,
    revision_hash text NOT NULL,
    bundle jsonb NOT NULL,
    manifest jsonb NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT skill_revision_bundle_check CHECK ((jsonb_typeof(bundle) = 'object'::text)),
    CONSTRAINT skill_revision_manifest_check CHECK ((jsonb_typeof(manifest) = 'object'::text)),
    CONSTRAINT skill_revision_revision_hash_check CHECK ((revision_hash ~ '^sha256:[0-9a-f]{64}$'::text)),
    CONSTRAINT skill_revision_source_id_check CHECK ((length(btrim(source_id)) > 0)),
    CONSTRAINT skill_revision_source_kind_check CHECK ((source_kind = ANY (ARRAY['workspace'::text, 'builtin'::text, 'runtime'::text])))
);

CREATE TABLE public.skill_to_label (
    skill_id uuid NOT NULL,
    label_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.squad (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    leader_id uuid NOT NULL,
    creator_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    archived_by uuid,
    avatar_url text,
    instructions text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.squad_member (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    squad_id uuid NOT NULL,
    member_type text NOT NULL,
    member_id uuid NOT NULL,
    role text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT squad_member_member_type_check CHECK ((member_type = ANY (ARRAY['agent'::text, 'member'::text])))
);

CREATE TABLE public.sys_cron_executions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    job_name text NOT NULL,
    scope_kind text DEFAULT 'global'::text NOT NULL,
    scope_id text DEFAULT 'global'::text NOT NULL,
    plan_time timestamp with time zone NOT NULL,
    status text NOT NULL,
    attempt integer DEFAULT 1 NOT NULL,
    max_attempts integer DEFAULT 3 NOT NULL,
    next_retry_at timestamp with time zone,
    runner_id text,
    lease_token uuid DEFAULT gen_random_uuid() NOT NULL,
    heartbeat_at timestamp with time zone,
    stale_after timestamp with time zone,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    duration_ms integer,
    rows_affected bigint,
    result jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_code text,
    error_msg text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_sys_cron_attempt CHECK (((attempt >= 1) AND (max_attempts >= attempt))),
    CONSTRAINT chk_sys_cron_duration CHECK (((duration_ms IS NULL) OR (duration_ms >= 0))),
    CONSTRAINT chk_sys_cron_status CHECK ((status = ANY (ARRAY['RUNNING'::text, 'SUCCESS'::text, 'FAILED'::text])))
);

CREATE TABLE public.agent_run_message (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    seq integer NOT NULL,
    type text NOT NULL,
    tool text,
    content text,
    input jsonb,
    output text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.agent_run_token (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    token_hash text NOT NULL,
    run_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.agent_run_usage (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    provider text DEFAULT ''::text NOT NULL,
    model text NOT NULL,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    cache_read_tokens bigint DEFAULT 0 NOT NULL,
    cache_write_tokens bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now(),
    cost_usd_ticks bigint
);

CREATE TABLE public.agent_run_usage_hourly (
    bucket_hour timestamp with time zone NOT NULL,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    project_id uuid,
    provider text NOT NULL,
    model text NOT NULL,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    cache_read_tokens bigint DEFAULT 0 NOT NULL,
    cache_write_tokens bigint DEFAULT 0 NOT NULL,
    run_count bigint DEFAULT 0 NOT NULL,
    event_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    cost_usd_ticks bigint DEFAULT 0 NOT NULL,
    uncosted_input_tokens bigint,
    uncosted_output_tokens bigint,
    uncosted_cache_read_tokens bigint,
    uncosted_cache_write_tokens bigint
);

CREATE TABLE public.agent_run_usage_hourly_dirty (
    bucket_hour timestamp with time zone NOT NULL,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    project_id uuid,
    provider text NOT NULL,
    model text NOT NULL,
    enqueued_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.agent_run_usage_hourly_rollup_state (
    id smallint DEFAULT 1 NOT NULL,
    watermark_at timestamp with time zone DEFAULT '1970-01-01 00:00:00+00'::timestamp with time zone NOT NULL,
    last_run_started_at timestamp with time zone,
    last_run_finished_at timestamp with time zone,
    last_run_rows bigint DEFAULT 0 NOT NULL,
    last_error text,
    CONSTRAINT agent_run_usage_hourly_rollup_state_id_check CHECK ((id = 1))
);

CREATE TABLE public."user" (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    avatar_url text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    onboarded_at timestamp with time zone,
    onboarding_questionnaire jsonb DEFAULT '{}'::jsonb NOT NULL,
    cloud_waitlist_email character varying(254),
    cloud_waitlist_reason text,
    starter_content_state text,
    language character varying(20) DEFAULT NULL::character varying,
    profile_description text DEFAULT ''::text NOT NULL,
    timezone text,
    password_hash text
);

CREATE TABLE public.user_composio_connection (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    toolkit_slug text NOT NULL,
    auth_config_id text NOT NULL,
    connected_account_id text NOT NULL,
    composio_user_id text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    connected_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.vcs_commit_status (
    connection_id uuid NOT NULL,
    sha text NOT NULL,
    context text NOT NULL,
    state text NOT NULL,
    target_url text,
    description text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.vcs_connection (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    provider text DEFAULT 'forgejo'::text NOT NULL,
    instance_url text NOT NULL,
    account_login text NOT NULL,
    access_token_encrypted text NOT NULL,
    webhook_secret_encrypted text NOT NULL,
    connected_by_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vcs_connection_provider_check CHECK ((provider = ANY (ARRAY['forgejo'::text, 'gitea'::text, 'gitlab'::text])))
);

CREATE TABLE public.vcs_pull_request (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    provider text DEFAULT 'forgejo'::text NOT NULL,
    repo_owner text NOT NULL,
    repo_name text NOT NULL,
    pr_number integer NOT NULL,
    title text NOT NULL,
    state text NOT NULL,
    html_url text NOT NULL,
    branch text,
    head_sha text DEFAULT ''::text NOT NULL,
    author_login text,
    author_avatar_url text,
    merged_at timestamp with time zone,
    closed_at timestamp with time zone,
    pr_created_at timestamp with time zone NOT NULL,
    pr_updated_at timestamp with time zone NOT NULL,
    additions integer DEFAULT 0 NOT NULL,
    deletions integer DEFAULT 0 NOT NULL,
    changed_files integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vcs_pull_request_provider_check CHECK ((provider = ANY (ARRAY['forgejo'::text, 'gitea'::text, 'gitlab'::text]))),
    CONSTRAINT vcs_pull_request_state_check CHECK ((state = ANY (ARRAY['open'::text, 'closed'::text, 'merged'::text, 'draft'::text])))
);

CREATE TABLE public.verification_code (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    code text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    attempts integer DEFAULT 0 NOT NULL
);

CREATE TABLE public.webhook_delivery (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    autopilot_id uuid NOT NULL,
    trigger_id uuid NOT NULL,
    provider text NOT NULL,
    event text DEFAULT 'webhook.received'::text NOT NULL,
    dedupe_key text,
    dedupe_source text,
    signature_status text DEFAULT 'not_required'::text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    attempt_count integer DEFAULT 1 NOT NULL,
    selected_headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    content_type text,
    raw_body bytea,
    response_status integer,
    response_body text,
    autopilot_run_id uuid,
    replayed_from_delivery_id uuid,
    error text,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    last_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_token uuid,
    lease_expires_at timestamp with time zone,
    dispatch_attempts integer DEFAULT 0 NOT NULL,
    CONSTRAINT webhook_delivery_provider_check CHECK ((provider = ANY (ARRAY['generic'::text, 'github'::text]))),
    CONSTRAINT webhook_delivery_signature_status_check CHECK ((signature_status = ANY (ARRAY['not_required'::text, 'valid'::text, 'invalid'::text, 'missing'::text]))),
    CONSTRAINT webhook_delivery_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'dispatched'::text, 'rejected'::text, 'ignored'::text, 'failed'::text])))
);

CREATE TABLE public.workflow (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    description text,
    status text DEFAULT 'draft'::text NOT NULL,
    current_version_id uuid,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    project_id uuid,
    CONSTRAINT workflow_name_check CHECK ((length(TRIM(BOTH FROM name)) > 0)),
    CONSTRAINT workflow_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'published'::text, 'archived'::text])))
);

CREATE TABLE public.workflow_artifact (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    workflow_stage_id uuid NOT NULL,
    name text NOT NULL,
    CONSTRAINT workflow_artifact_name_check CHECK ((length(TRIM(BOTH FROM name)) > 0))
);

CREATE TABLE public.workflow_artifact_revision (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    artifact_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    result_id uuid NOT NULL,
    upload_id uuid NOT NULL,
    revision integer NOT NULL,
    source_attachment_id uuid NOT NULL,
    kind text NOT NULL,
    media_type text NOT NULL,
    schema_version text NOT NULL,
    checksum text NOT NULL,
    size_bytes bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_artifact_revision_revision_check CHECK ((revision >= 1)),
    CONSTRAINT workflow_artifact_revision_size_bytes_check CHECK ((size_bytes >= 0))
);

CREATE TABLE public.workflow_artifact_upload (
    id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    storage_key text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '01:00:00'::interval) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_artifact_upload_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'committed'::text, 'deleting'::text])))
);

CREATE TABLE public.workflow_execution_action (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    kind text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_until timestamp with time zone,
    claim_token uuid,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_execution_action_attempts_check CHECK ((attempts >= 0)),
    CONSTRAINT workflow_execution_action_check CHECK ((((status = 'leased'::text) AND (lease_until IS NOT NULL) AND (claim_token IS NOT NULL)) OR ((status <> 'leased'::text) AND (lease_until IS NULL) AND (claim_token IS NULL)))),
    CONSTRAINT workflow_execution_action_kind_check CHECK ((kind = ANY (ARRAY['dispatch'::text, 'review'::text, 'advance'::text]))),
    CONSTRAINT workflow_execution_action_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'leased'::text, 'delivered'::text, 'cancelled'::text])))
);

CREATE TABLE public.workflow_execution_result (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    revision integer NOT NULL,
    data jsonb NOT NULL,
    submitted_by_type text NOT NULL,
    submitted_by_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_execution_result_data_check CHECK ((jsonb_typeof(data) = 'object'::text)),
    CONSTRAINT workflow_execution_result_revision_check CHECK ((revision >= 1)),
    CONSTRAINT workflow_execution_result_submitted_by_type_check CHECK ((submitted_by_type = ANY (ARRAY['member'::text, 'agent'::text])))
);

CREATE TABLE public.workflow_gate_decision (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    from_stage_id uuid NOT NULL,
    to_stage_id uuid,
    outcome text NOT NULL,
    actor_type text NOT NULL,
    actor_id uuid NOT NULL,
    note text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    execution_id uuid,
    result_revision integer,
    review_step text,
    basis_hash text,
    basis_snapshot jsonb,
    review_run_id uuid,
    invalidated_by_rework_id uuid,
    CONSTRAINT workflow_decision_basis_check CHECK ((((execution_id IS NULL) AND (result_revision IS NULL) AND (review_step IS NULL) AND (basis_hash IS NULL) AND (basis_snapshot IS NULL)) OR ((execution_id IS NOT NULL) AND (result_revision > 0) AND (result_revision IS NOT NULL) AND (review_step = ANY (ARRAY['human'::text, 'agent'::text, 'automatic'::text])) AND (review_step IS NOT NULL) AND (basis_hash ~ '^[0-9a-f]{64}$'::text) AND (basis_hash IS NOT NULL) AND (jsonb_typeof(basis_snapshot) = 'object'::text) AND (basis_snapshot IS NOT NULL)))),
    CONSTRAINT workflow_gate_decision_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT workflow_gate_decision_outcome_check CHECK ((outcome = ANY (ARRAY['approved'::text, 'rejected'::text])))
);

CREATE TABLE public.workflow_instance (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_id uuid NOT NULL,
    workflow_version_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    status text DEFAULT 'draft'::text NOT NULL,
    current_stage_id uuid,
    project_id uuid,
    created_by uuid NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    archived_by uuid,
    revision integer DEFAULT 1 NOT NULL,
    inputs jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT workflow_instance_inputs_check CHECK ((jsonb_typeof(inputs) = 'object'::text)),
    CONSTRAINT workflow_instance_revision_check CHECK ((revision >= 1)),
    CONSTRAINT workflow_instance_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'active'::text, 'waiting'::text, 'completed'::text, 'cancelled'::text]))),
    CONSTRAINT workflow_instance_title_check CHECK ((length(TRIM(BOTH FROM title)) > 0))
);

CREATE TABLE public.workflow_instance_change (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    revision integer NOT NULL,
    changed_by uuid NOT NULL,
    change_note text,
    before_plan jsonb NOT NULL,
    after_plan jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_instance_change_after_plan_check CHECK ((jsonb_typeof(after_plan) = 'object'::text)),
    CONSTRAINT workflow_instance_change_before_plan_check CHECK ((jsonb_typeof(before_plan) = 'object'::text)),
    CONSTRAINT workflow_instance_change_revision_check CHECK ((revision >= 2))
);

CREATE TABLE public.workflow_instance_stage (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    source_stage_id uuid,
    stable_key text NOT NULL,
    name text NOT NULL,
    description text,
    "position" integer NOT NULL,
    completion_rule jsonb DEFAULT '{"type": "all_runs_terminal"}'::jsonb NOT NULL,
    input_spec jsonb DEFAULT '{}'::jsonb NOT NULL,
    output_spec jsonb DEFAULT '{}'::jsonb NOT NULL,
    required_skills text[] DEFAULT '{}'::text[] NOT NULL,
    gate jsonb DEFAULT '{"type": "none"}'::jsonb NOT NULL,
    rollback_stage_key text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    required_skill_snapshots jsonb DEFAULT '[]'::jsonb NOT NULL,
    allowed_mcp_server_ids uuid[],
    execution_mode text DEFAULT 'manual'::text NOT NULL,
    executor_id uuid,
    input_bindings jsonb DEFAULT '{}'::jsonb NOT NULL,
    rework_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    retry_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT workflow_instance_stage_completion_rule_check CHECK ((jsonb_typeof(completion_rule) = 'object'::text)),
    CONSTRAINT workflow_instance_stage_execution_mode_check CHECK ((execution_mode = ANY (ARRAY['manual'::text, 'agent'::text]))),
    CONSTRAINT workflow_instance_stage_executor_check CHECK ((((execution_mode = 'agent'::text) AND (executor_id IS NOT NULL)) OR ((execution_mode = 'manual'::text) AND (executor_id IS NULL)))),
    CONSTRAINT workflow_instance_stage_gate_check CHECK ((jsonb_typeof(gate) = 'object'::text)),
    CONSTRAINT workflow_instance_stage_input_bindings_check CHECK ((jsonb_typeof(input_bindings) = 'object'::text)),
    CONSTRAINT workflow_instance_stage_input_spec_check CHECK ((jsonb_typeof(input_spec) = 'object'::text)),
    CONSTRAINT workflow_instance_stage_name_check CHECK ((length(TRIM(BOTH FROM name)) > 0)),
    CONSTRAINT workflow_instance_stage_output_spec_check CHECK ((jsonb_typeof(output_spec) = 'object'::text)),
    CONSTRAINT workflow_instance_stage_position_check CHECK (("position" >= 0)),
    CONSTRAINT workflow_instance_stage_retry_policy_check CHECK ((jsonb_typeof(retry_policy) = 'object'::text)),
    CONSTRAINT workflow_instance_stage_rework_policy_check CHECK ((jsonb_typeof(rework_policy) = 'object'::text)),
    CONSTRAINT workflow_instance_stage_stable_key_check CHECK ((length(TRIM(BOTH FROM stable_key)) > 0))
);

CREATE TABLE public.workflow_review_recovery (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    request_id uuid NOT NULL,
    failed_run_id uuid NOT NULL,
    new_run_id uuid NOT NULL,
    result_revision integer NOT NULL,
    basis_hash text NOT NULL,
    actor_id uuid NOT NULL,
    reason text NOT NULL,
    allowed_attempts integer NOT NULL,
    max_attempts integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_review_recovery_allowed_attempts_check CHECK (((allowed_attempts >= 1) AND (allowed_attempts <= 3))),
    CONSTRAINT workflow_review_recovery_basis_hash_check CHECK ((length(basis_hash) = 64)),
    CONSTRAINT workflow_review_recovery_max_attempts_check CHECK ((max_attempts > 0)),
    CONSTRAINT workflow_review_recovery_reason_check CHECK (((length(btrim(reason)) > 0) AND (octet_length(reason) <= 4000))),
    CONSTRAINT workflow_review_recovery_result_revision_check CHECK ((result_revision > 0))
);

CREATE TABLE public.workflow_review_request (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    execution_id uuid NOT NULL,
    reviewer_id uuid NOT NULL,
    result_id uuid NOT NULL,
    result_revision integer NOT NULL,
    basis_hash text NOT NULL,
    basis_snapshot jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    run_id uuid,
    CONSTRAINT workflow_review_request_basis_hash_check CHECK ((basis_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT workflow_review_request_basis_snapshot_check CHECK ((jsonb_typeof(basis_snapshot) = 'object'::text)),
    CONSTRAINT workflow_review_request_result_revision_check CHECK ((result_revision > 0)),
    CONSTRAINT workflow_review_request_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'queued'::text, 'decided'::text, 'invalidated'::text])))
);

CREATE TABLE public.workflow_rework (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    source_stage_id uuid NOT NULL,
    target_stage_id uuid NOT NULL,
    source_execution_id uuid,
    source_state_token text,
    plan_revision integer NOT NULL,
    actor_type text NOT NULL,
    actor_id uuid NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_rework_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT workflow_rework_check CHECK (((source_execution_id IS NOT NULL) OR ((source_state_token IS NOT NULL) AND (length(source_state_token) = 64)))),
    CONSTRAINT workflow_rework_plan_revision_check CHECK ((plan_revision >= 1))
);

CREATE TABLE public.workflow_stage (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_version_id uuid NOT NULL,
    stable_key text NOT NULL,
    name text NOT NULL,
    description text,
    "position" integer NOT NULL,
    completion_rule jsonb DEFAULT '{"type": "all_runs_terminal"}'::jsonb NOT NULL,
    input_spec jsonb DEFAULT '{}'::jsonb NOT NULL,
    output_spec jsonb DEFAULT '{}'::jsonb NOT NULL,
    required_skills text[] DEFAULT '{}'::text[] NOT NULL,
    gate jsonb DEFAULT '{"type": "none"}'::jsonb NOT NULL,
    rollback_stage_key text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    allowed_mcp_server_ids uuid[],
    execution_mode text DEFAULT 'manual'::text NOT NULL,
    executor_id uuid,
    input_bindings jsonb DEFAULT '{}'::jsonb NOT NULL,
    rework_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    retry_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT workflow_stage_completion_rule_check CHECK ((jsonb_typeof(completion_rule) = 'object'::text)),
    CONSTRAINT workflow_stage_execution_mode_check CHECK ((execution_mode = ANY (ARRAY['manual'::text, 'agent'::text]))),
    CONSTRAINT workflow_stage_executor_check CHECK ((((execution_mode = 'agent'::text) AND (executor_id IS NOT NULL)) OR ((execution_mode = 'manual'::text) AND (executor_id IS NULL)))),
    CONSTRAINT workflow_stage_gate_check CHECK ((jsonb_typeof(gate) = 'object'::text)),
    CONSTRAINT workflow_stage_input_bindings_check CHECK ((jsonb_typeof(input_bindings) = 'object'::text)),
    CONSTRAINT workflow_stage_input_spec_check CHECK ((jsonb_typeof(input_spec) = 'object'::text)),
    CONSTRAINT workflow_stage_name_check CHECK ((length(TRIM(BOTH FROM name)) > 0)),
    CONSTRAINT workflow_stage_output_spec_check CHECK ((jsonb_typeof(output_spec) = 'object'::text)),
    CONSTRAINT workflow_stage_position_check CHECK (("position" >= 0)),
    CONSTRAINT workflow_stage_retry_policy_check CHECK ((jsonb_typeof(retry_policy) = 'object'::text)),
    CONSTRAINT workflow_stage_rework_policy_check CHECK ((jsonb_typeof(rework_policy) = 'object'::text)),
    CONSTRAINT workflow_stage_stable_key_check CHECK ((length(TRIM(BOTH FROM stable_key)) > 0))
);

CREATE TABLE public.workflow_stage_execution (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_instance_id uuid NOT NULL,
    workflow_stage_id uuid NOT NULL,
    attempt integer NOT NULL,
    plan_revision integer NOT NULL,
    executor_id uuid NOT NULL,
    main_task_id uuid,
    config_snapshot jsonb NOT NULL,
    input_snapshot jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    failure_code text,
    failure_detail text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    main_run_id uuid,
    result_revision integer DEFAULT 0 NOT NULL,
    acceptance jsonb,
    rework_source_execution_id uuid,
    rework_metadata jsonb,
    CONSTRAINT workflow_stage_execution_acceptance_check CHECK (((acceptance IS NULL) OR (jsonb_typeof(acceptance) = 'object'::text))),
    CONSTRAINT workflow_stage_execution_attempt_check CHECK ((attempt >= 1)),
    CONSTRAINT workflow_stage_execution_config_snapshot_check CHECK ((jsonb_typeof(config_snapshot) = 'object'::text)),
    CONSTRAINT workflow_stage_execution_input_snapshot_check CHECK ((jsonb_typeof(input_snapshot) = 'object'::text)),
    CONSTRAINT workflow_stage_execution_plan_revision_check CHECK ((plan_revision >= 1)),
    CONSTRAINT workflow_stage_execution_result_revision_check CHECK ((result_revision >= 0)),
    CONSTRAINT workflow_stage_execution_rework_metadata_check CHECK (((rework_metadata IS NULL) OR (jsonb_typeof(rework_metadata) = 'object'::text))),
    CONSTRAINT workflow_stage_execution_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'queued'::text, 'running'::text, 'awaiting_review'::text, 'succeeded'::text, 'blocked'::text, 'failed'::text, 'cancelled'::text])))
);

CREATE TABLE public.workflow_version (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    workflow_id uuid NOT NULL,
    version integer NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    created_by uuid NOT NULL,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workflow_version_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'published'::text, 'superseded'::text]))),
    CONSTRAINT workflow_version_version_check CHECK ((version >= 1))
);

CREATE TABLE public.workspace (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    description text,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    context text,
    repos jsonb DEFAULT '[]'::jsonb NOT NULL,
    task_prefix text DEFAULT ''::text NOT NULL,
    task_counter integer DEFAULT 0 NOT NULL,
    avatar_url text,
    attribution_fail_closed boolean DEFAULT false NOT NULL
);

CREATE TABLE public.workspace_invitation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    inviter_id uuid NOT NULL,
    invitee_email text NOT NULL,
    invitee_user_id uuid,
    role text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '7 days'::interval) NOT NULL,
    CONSTRAINT workspace_invitation_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'member'::text]))),
    CONSTRAINT workspace_invitation_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'accepted'::text, 'declined'::text, 'expired'::text])))
);

CREATE TABLE public.workspace_mcp_server (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    config_encrypted text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE public.agent_run
    ADD CONSTRAINT agent_run_active_requires_runtime CHECK (((runtime_id IS NOT NULL) OR (completed_at IS NOT NULL))) NOT VALID;

ALTER TABLE public.runtime_profile
    ADD CONSTRAINT runtime_profile_protocol_family_check CHECK ((protocol_family = ANY (ARRAY['claude'::text, 'codebuddy'::text, 'codex'::text, 'copilot'::text, 'opencode'::text, 'openclaw'::text, 'hermes'::text, 'pi'::text, 'cursor'::text, 'kimi'::text, 'kiro'::text, 'antigravity'::text, 'qoder'::text, 'traecli'::text, 'deveco'::text, 'grok'::text, 'qwen'::text]))) NOT VALID;

CREATE TRIGGER skill_evaluation_suite_immutable BEFORE UPDATE ON public.skill_evaluation_suite FOR EACH ROW EXECUTE FUNCTION public.reject_skill_evaluation_suite_update();

CREATE TRIGGER skill_improvement_proposal_immutable BEFORE UPDATE ON public.skill_improvement_proposal FOR EACH ROW EXECUTE FUNCTION public.reject_skill_improvement_proposal_update();

CREATE TRIGGER skill_revision_immutable BEFORE UPDATE ON public.skill_revision FOR EACH ROW EXECUTE FUNCTION public.reject_skill_revision_update();

CREATE TRIGGER trg_atq_dirty_hourly BEFORE DELETE OR UPDATE OF runtime_id, task_id ON public.agent_run FOR EACH ROW EXECUTE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_atq();

CREATE TRIGGER trg_clear_runtime_mcp_overlay BEFORE UPDATE OF status ON public.agent_run FOR EACH ROW EXECUTE FUNCTION public.clear_runtime_mcp_overlay_on_terminal_state();

CREATE TRIGGER trg_task_delete_dirty_hourly BEFORE DELETE ON public.task FOR EACH ROW EXECUTE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_task_delete();

CREATE TRIGGER trg_task_project_dirty_hourly BEFORE UPDATE OF project_id ON public.task FOR EACH ROW EXECUTE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_task_project();

CREATE TRIGGER trg_tu_dirty_hourly BEFORE DELETE ON public.agent_run_usage FOR EACH ROW EXECUTE FUNCTION public.enqueue_agent_run_usage_hourly_dirty_for_tu();

SET check_function_bodies = true;

INSERT INTO public.agent_run_usage_hourly_rollup_state VALUES (1, '1970-01-01 00:00:00+00', NULL, NULL, 0, NULL);
