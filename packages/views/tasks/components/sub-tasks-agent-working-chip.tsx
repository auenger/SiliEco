"use client";

import { memo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@silieco/ui/components/ui/hover-card";
import { useWorkspaceId } from "@silieco/core/hooks";
import { workspaceWorkingAgentsOptions } from "@silieco/core/agents";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { WorkingAgentsHoverContent } from "./workspace-agent-working-chip";
import { useT } from "../../i18n";

interface SubTasksAgentWorkingChipProps {
  /** Parent task whose direct children this chip aggregates over. */
  parentTaskId: string;
}

/**
 * Aggregate "N agents working" chip for the sub-tasks header in task
 * detail (silieco#5825). The per-row TaskAgentActivityIndicator answers
 * "which sub-task is being worked on"; this chip answers "how many agents
 * are on this parent's children right now" without scanning the rows — and
 * keeps that signal visible while the list is collapsed.
 *
 * It reads the same /api/working-agents projection as the Tasks list header,
 * narrowed with `parent`. That is deliberate: a header count is a claim about
 * a scope, so the server owns both the scope and the arithmetic, exactly as
 * it does for the Tasks list. Deriving it here from the workspace task
 * snapshot would put a second definition of "working" in the client, and the
 * count and the hover body would each have to re-derive it.
 *
 * Row indicators keep reading the snapshot — one shared query sliced per row
 * is the right shape for a per-row cue, and a stale row decoration costs
 * nothing. A header number is the opposite: it has to be authoritative.
 */
export const SubTasksAgentWorkingChip = memo(
  function SubTasksAgentWorkingChip({
    parentTaskId,
  }: SubTasksAgentWorkingChipProps) {
    const { t } = useT("tasks");
    const wsId = useWorkspaceId();
    const { data: agents = [] } = useQuery(
      workspaceWorkingAgentsOptions(wsId, "task", undefined, parentTaskId),
    );

    if (agents.length === 0) return null;

    const agentIds = agents.map((agent) => agent.id);

    return (
      <HoverCard>
        <HoverCardTrigger
          render={
            <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-muted/60 px-2 py-0.5" />
          }
        >
          <AgentAvatarStack agentIds={agentIds} size="xs" max={3} />
          <span className="animate-chat-text-shimmer text-micro font-medium leading-none tabular-nums">
            {t(($) => $.agent_activity.chip_agents_working, {
              count: agentIds.length,
            })}
          </span>
        </HoverCardTrigger>
        <HoverCardContent align="start" className="w-72">
          <WorkingAgentsHoverContent agents={agents} />
        </HoverCardContent>
      </HoverCard>
    );
  },
);
