"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ActorAvatar as ActorAvatarBase } from "@silieco/ui/components/common/actor-avatar";
import { useActorName } from "@silieco/core/workspace/hooks";
import { useWorkspaceId } from "@silieco/core/hooks";
import { runtimeListOptions } from "@silieco/core/runtimes/queries";
import { agentListOptions } from "@silieco/core/workspace/queries";
import { deriveAgentAvailability } from "@silieco/core/agents";
import type { AgentRun, Task } from "@silieco/core/types";
import { workloadConfig } from "../presence";
import { useT } from "../../i18n";

interface AgentActivityHoverContentProps {
  // Active tasks (running / queued / dispatched) to render — caller filters
  // by task id or by workspace scope. Order is preserved; we render every
  // task as its own row.
  runs: readonly AgentRun[];
}

/**
 * Tick `now` once per second so duration labels update live while a hover
 * card is open. setInterval only runs while the card is mounted (Base UI
 * portals the content but tears it down on close), so this costs nothing
 * when the card is closed.
 */
function useActivityNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

/**
 * O(1) agent + runtime lookups so each task row resolves without an N×M
 * scan. Cheap — agents/runtimes count in tens at most.
 */
function useActivityLookups() {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const agentById = new Map(agents.map((a) => [a.id, a] as const));
  const runtimeById = new Map(runtimes.map((r) => [r.id, r] as const));
  return { agentById, runtimeById };
}

type ActivityLookups = ReturnType<typeof useActivityLookups>;

/**
 * One task row: agent avatar, name, status dot, status label, duration.
 *
 * Status colour follows the workspace's existing composition rule:
 *   - running                       → brand (text-brand)
 *   - queued, runtime online        → muted gray (transient race)
 *   - queued, runtime offline/etc.  → warning amber (genuine stuck)
 * — same rule as agent-presence-indicator.tsx so users see a single,
 * consistent language for "agent is in trouble" vs "just enqueued".
 */
function AgentActivityRunRow({
  run,
  now,
  agentById,
  runtimeById,
}: {
  run: AgentRun;
  now: number;
} & ActivityLookups) {
  const { t } = useT("tasks");
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();

  const agent = agentById.get(run.agent_id);
  const runtime = runtimeFrom(agent?.runtime_id, runtimeById);
  const availability = deriveAgentAvailability(runtime, now);
  const isRunning = run.status === "running";
  // queued/dispatched both read as "queued" in the user-facing copy —
  // `dispatched` is the daemon-acked sub-state of queued and not
  // user-meaningful here.
  const wl = isRunning ? workloadConfig.working : workloadConfig.queued;
  // queued + online → muted gray (transient race, no warning);
  // queued + offline/unstable → keep warning amber from workloadConfig.
  // Mirrors agent-presence-indicator.tsx.
  const dotClass = isRunning
    ? "bg-brand"
    : availability === "online"
      ? "bg-muted-foreground/40"
      : "bg-warning";
  const labelClass = isRunning
    ? wl.textClass
    : availability === "online"
      ? "text-muted-foreground"
      : wl.textClass;
  const startedFrom = isRunning
    ? (run.started_at ?? run.dispatched_at ?? run.created_at)
    : run.created_at;

  return (
    <div className="flex items-center gap-2 text-caption">
      <ActorAvatarBase
        name={getActorName("agent", run.agent_id)}
        initials={getActorInitials("agent", run.agent_id)}
        avatarUrl={getActorAvatarUrl("agent", run.agent_id)}
        isAgent
        size="sm"
      />
      <span className="flex-1 truncate font-medium">
        {getActorName("agent", run.agent_id)}
      </span>
      <span className="flex shrink-0 items-center gap-1.5">
        <span className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />
        <span className={labelClass}>
          {isRunning
            ? t(($) => $.agent_activity.status_running)
            : t(($) => $.agent_activity.status_queued)}
        </span>
        <span className="tabular-nums text-muted-foreground">
          {formatDuration(startedFrom, now)}
        </span>
      </span>
    </div>
  );
}

/**
 * Shared hover-card body for "what are these agents doing right now?" — used
 * by TaskAgentActivityIndicator (per-task). One row per task.
 *
 * The workspace-wide chip uses WorkspaceAgentActivityHoverContent below,
 * which groups the same rows by task.
 */
export function AgentActivityHoverContent({
  runs,
}: AgentActivityHoverContentProps) {
  const { t } = useT("tasks");
  const now = useActivityNow();
  const { agentById, runtimeById } = useActivityLookups();

  if (runs.length === 0) return null;

  return (
    <div className="flex flex-col gap-2">
      <div className="text-caption font-medium text-muted-foreground">
        {/* One row per task, so count tasks — not agents. A single agent can
            run several tasks at once, so an agent-worded header here would
            disagree with the row count below. */}
        {t(($) => $.agent_activity.hover_header_runs, { count: runs.length })}
      </div>
      <div className="flex flex-col gap-1.5">
        {runs.map((run) => (
          <AgentActivityRunRow
            key={run.id}
            run={run}
            now={now}
            agentById={agentById}
            runtimeById={runtimeById}
          />
        ))}
      </div>
    </div>
  );
}

interface WorkspaceAgentActivityHoverContentProps {
  /** Tasks the working filter leaves on screen, in list order. Each has at
   *  least one running task. */
  tasks: readonly Task[];
  /** Running tasks for those tasks, keyed by task id. */
  runsByTaskId: ReadonlyMap<string, readonly AgentRun[]>;
  /** Total running tasks across `tasks` — the second header figure. */
  runCount: number;
}

/**
 * Hover-card body for the workspace working chip (SILI-4884).
 *
 * The chip says WHO is working ("N agents working"); this card says WHERE.
 * The header carries the two figures the chip does not — how many tasks
 * that work lands on, and how many tasks it takes — and the rows group by
 * task, mirroring what clicking the chip does to the list.
 *
 * It says nothing about work it excludes. Chat/autopilot runs have no
 * linked task and leave no trace anywhere on this page: no row, no head,
 * no indicator. A footnote about them would explain an absence the user
 * never perceived — inventing a discrepancy rather than resolving one.
 * Same for tasks on tasks the current filters or the loaded page exclude.
 *
 * Deliberately not a dashboard: two figures and grouped rows.
 */
export function WorkspaceAgentActivityHoverContent({
  tasks,
  runsByTaskId,
  runCount,
}: WorkspaceAgentActivityHoverContentProps) {
  const { t } = useT("tasks");
  const now = useActivityNow();
  const { agentById, runtimeById } = useActivityLookups();

  if (tasks.length === 0) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.agent_activity.empty_hover)}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2.5">
      <div className="text-caption font-medium text-muted-foreground">
        {`${t(($) => $.agent_activity.tasks_count, {
          count: tasks.length,
        })} · ${t(($) => $.agent_activity.runs_count, { count: runCount })}`}
      </div>
      <div className="flex flex-col gap-2.5">
        {tasks.map((task) => (
          <div key={task.id} className="flex flex-col gap-1.5">
            <div className="flex items-baseline gap-1.5 text-caption">
              <span className="shrink-0 font-mono text-micro text-muted-foreground">
                {task.identifier}
              </span>
              <span className="truncate">{task.title}</span>
            </div>
            <div className="flex flex-col gap-1.5">
              {(runsByTaskId.get(task.id) ?? []).map((run) => (
                <AgentActivityRunRow
                  key={run.id}
                  run={run}
                  now={now}
                  agentById={agentById}
                  runtimeById={runtimeById}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function runtimeFrom<T extends { id: string }>(
  id: string | undefined,
  byId: Map<string, T>,
): T | null {
  if (!id) return null;
  return byId.get(id) ?? null;
}

// Compact `2m 14s` / `45s` / `1h 03m` duration since the given ISO string.
// Capped at hours — anything over a day for a running task is a sign of a
// stuck runtime, but the hover card is not the place to relitigate that;
// the row will read as `26h 12m` and the user can act.
//
// Exported so the task-detail header live chip formats its collapsed
// single-agent elapsed with the same `2m 14s` / `1h 03m` rule used here.
export function formatDuration(fromIso: string, nowMs: number): string {
  const start = new Date(fromIso).getTime();
  if (!Number.isFinite(start)) return "";
  const sec = Math.max(0, Math.round((nowMs - start) / 1000));
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  const remSec = sec % 60;
  if (min < 60) return `${min}m ${pad2(remSec)}s`;
  const hr = Math.floor(min / 60);
  const remMin = min % 60;
  return `${hr}h ${pad2(remMin)}m`;
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}
