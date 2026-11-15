"use client";

import { useQuery } from "@tanstack/react-query";
import { ActorAvatar as ActorAvatarBase } from "@silieco/ui/components/common/actor-avatar";
import { Skeleton } from "@silieco/ui/components/ui/skeleton";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useWorkspacePaths } from "@silieco/core/paths";
import { agentListOptions } from "@silieco/core/workspace/queries";
import { resolvePublicFileUrl } from "@silieco/core/workspace/avatar-url";
import {
  agentRunSnapshotOptions,
  useAgentPresenceDetail,
} from "@silieco/core/agents";
import { taskDetailOptions } from "@silieco/core/tasks";
import type { AgentRun } from "@silieco/core/types";
import { AlertTriangle } from "lucide-react";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { availabilityConfig, workloadConfig } from "../presence";

interface AgentLivePeekCardProps {
  agentId: string;
}

// Live "peek" card for an agent avatar — shows the three live signals the
// squad members tab cares about (workload, current task, last activity).
// Companion to AgentProfileCard, which surfaces static identity (description,
// runtime, skills, owner). Keeping them separate avoids polluting the 23+
// existing AgentProfileCard call sites with live-only concerns.
export function AgentLivePeekCard({ agentId }: AgentLivePeekCardProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  const { data: snapshot = [] } = useQuery(agentRunSnapshotOptions(wsId));
  const presence = useAgentPresenceDetail(wsId, agentId);

  const agent = agents.find((a) => a.id === agentId);

  if (agentsLoading && !agent) {
    return (
      <div className="flex items-center gap-3">
        <Skeleton className="h-10 w-10 rounded-full" />
        <div className="flex-1 space-y-1.5">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-3 w-20" />
        </div>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="text-caption text-muted-foreground">
        {t(($) => $.profile_card.unavailable)}
      </div>
    );
  }

  const agentRuns = snapshot.filter((t) => t.agent_id === agentId);
  const runningTask = agentRuns.find(
    (t) => t.status === "running" && !!t.task_id,
  );
  const currentTaskId = runningTask?.task_id ?? null;
  const lastTerminal = pickLatestTerminal(agentRuns);

  const initials = agent.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  // Archived wins over workload — a retired agent reads "Archived", never
  // "Idle"/"Working". availability is the unified signal (see
  // deriveAgentPresenceDetail); for archived it's set before any task scan.
  const isArchived =
    presence !== "loading" && presence.availability === "archived";
  const workload = presence === "loading" ? null : presence.workload;
  const workloadVisual = workload ? workloadConfig[workload] : null;
  const archivedVisual = availabilityConfig.archived;

  return (
    <div className="flex flex-col gap-3 text-left">
      {/* Header — avatar + name. */}
      <div className="flex items-start gap-3">
        <ActorAvatarBase
          name={agent.name}
          initials={initials}
          avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
          isAgent
          size="xl"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-body font-semibold">{agent.name}</p>
          <div className="mt-0.5 inline-flex items-center gap-1.5">
            {isArchived ? (
              <>
                <archivedVisual.icon
                  className={`h-3 w-3 shrink-0 ${archivedVisual.textClass}`}
                />
                <span className={`text-caption ${archivedVisual.textClass}`}>
                  {t(($) => $.availability.archived)}
                </span>
              </>
            ) : workloadVisual ? (
              <>
                <workloadVisual.icon
                  className={`h-3 w-3 shrink-0 ${workloadVisual.textClass}`}
                />
                <span className={`text-caption ${workloadVisual.textClass}`}>
                  {t(($) => $.workload[workload!])}
                </span>
              </>
            ) : (
              <Skeleton className="h-3 w-12" />
            )}
          </div>
        </div>
      </div>

      {/* Meta rows. */}
      <div className="flex flex-col gap-1.5 text-caption">
        <CurrentTaskRow
          wsId={wsId}
          taskId={currentTaskId}
          label={t(($) => $.live_peek.current_task_label)}
          emptyLabel={t(($) => $.live_peek.no_current_task)}
          taskHref={(id) => p.taskDetail(id)}
        />
        <LastActivityRow
          run={lastTerminal}
          label={t(($) => $.live_peek.last_activity_label)}
          emptyLabel={t(($) => $.live_peek.no_recent_activity)}
          failedLabel={t(($) => $.live_peek.failed_indicator)}
        />
      </div>
    </div>
  );
}

// Pick the most recent terminal task for last-activity display. The snapshot
// already caps this to one terminal row per agent (see queries.ts header), but
// a defensive max-by-completed_at keeps the card honest if that shape ever
// changes. Only completed / failed count, matching the snapshot's outcome
// filter exactly: cancelled never reaches this card, and treating it as a
// terminal outcome here would let an aborted attempt mask the agent's last
// real result if the endpoint ever started returning it.
function pickLatestTerminal(runs: readonly AgentRun[]): AgentRun | null {
  let best: AgentRun | null = null;
  for (const t of runs) {
    if (t.status !== "completed" && t.status !== "failed") {
      continue;
    }
    if (!t.completed_at) continue;
    if (!best || (best.completed_at && t.completed_at > best.completed_at)) {
      best = t;
    }
  }
  return best;
}

function CurrentTaskRow({
  wsId,
  taskId,
  label,
  emptyLabel,
  taskHref,
}: {
  wsId: string;
  taskId: string | null;
  label: string;
  emptyLabel: string;
  taskHref: (id: string) => string;
}) {
  // Lazy task detail — only enabled while the card is mounted AND we have
  // a running task id. snapshot already gives us the id; this hook just
  // resolves the human identifier (SILI-123) + title.
  const { data: task } = useQuery({
    ...taskDetailOptions(wsId, taskId ?? ""),
    enabled: !!taskId,
  });

  return (
    <div className="flex items-center gap-1.5">
      <span className="w-16 shrink-0 text-muted-foreground">{label}</span>
      {taskId ? (
        task ? (
          <AppLink
            href={taskHref(taskId)}
            className="min-w-0 truncate text-brand hover:underline"
            title={`${task.identifier} ${task.title}`}
          >
            <span className="mr-1 font-mono text-micro">{task.identifier}</span>
            <span>{task.title}</span>
          </AppLink>
        ) : (
          <Skeleton className="h-3 w-24" />
        )
      ) : (
        <span className="text-muted-foreground">{emptyLabel}</span>
      )}
    </div>
  );
}

function LastActivityRow({
  run,
  label,
  emptyLabel,
  failedLabel,
}: {
  run: AgentRun | null;
  label: string;
  emptyLabel: string;
  failedLabel: string;
}) {
  const timeAgo = useTimeAgo();
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-16 shrink-0 text-muted-foreground">{label}</span>
      {run && run.completed_at ? (
        <span className="inline-flex min-w-0 items-center gap-1 truncate">
          <span className="truncate">{timeAgo(run.completed_at)}</span>
          {run.status === "failed" && (
            // Failed terminal state shows here only — workload above stays a
            // clean "what's on the plate now" reading (working/queued/idle),
            // matching the project's deliberate split between current and
            // historical state.
            <span
              className="inline-flex items-center gap-0.5 rounded bg-warning/10 px-1 py-0.5 text-micro font-medium text-warning"
              title={failedLabel}
            >
              <AlertTriangle className="h-2.5 w-2.5" />
              {failedLabel}
            </span>
          )}
        </span>
      ) : (
        <span className="text-muted-foreground">{emptyLabel}</span>
      )}
    </div>
  );
}
