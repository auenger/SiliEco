"use client";

import { useQuery } from "@tanstack/react-query";
import { ActorAvatar } from "@silieco/ui/components/common/actor-avatar";
import { Button } from "@silieco/ui/components/ui/button";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@silieco/ui/components/ui/hover-card";
import { workspaceWorkingAgentsOptions } from "@silieco/core/agents";
import { useWorkspaceId } from "@silieco/core/hooks";
import type {
  WorkspaceWorkingAgent,
  WorkspaceWorkingAgentMineRelation,
} from "@silieco/core/types";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { useT } from "../../i18n";

interface WorkspaceAgentWorkingChipProps {
  value: boolean;
  onToggle: () => void;
  mineRelation?: WorkspaceWorkingAgentMineRelation;
}

/**
 * Which colour tier the chip wears, and the only classes allowed alongside
 * it. Activity uses a tint, the active filter uses the filled brand tier,
 * and an idle workspace stays neutral.
 */
export function chipAppearance(
  value: boolean,
  hasAgents: boolean,
): { variant: "brand" | "brandSubtle" | "outline"; className: string } {
  const layout = "h-8 px-2 md:h-7 md:px-2.5";
  if (value) return { variant: "brand", className: layout };
  if (hasAgents) return { variant: "brandSubtle", className: layout };
  return { variant: "outline", className: `${layout} text-muted-foreground` };
}

/**
 * Hover body for every surface that reads the working-agents projection —
 * the workspace filter chip here and the sub-tasks header chip on task
 * detail. Shared so a narrowed read and an unnarrowed one describe activity
 * the same way; the only difference between the two is the query's scope.
 */
export function WorkingAgentsHoverContent({
  agents,
}: {
  agents: readonly WorkspaceWorkingAgent[];
}) {
  const { t } = useT("tasks");

  if (agents.length === 0) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.agent_activity.empty_hover)}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="text-caption font-medium text-muted-foreground">
        {t(($) => $.agent_activity.hover_header, { count: agents.length })}
      </div>
      <div className="flex flex-col gap-1.5">
        {agents.map((agent) => (
          <div key={agent.id} className="flex items-center gap-2 text-caption">
            <ActorAvatar
              name={agent.name}
              initials={agent.name.trim().slice(0, 2).toUpperCase()}
              avatarUrl={agent.avatar_url ?? undefined}
              isAgent
              size="sm"
            />
            <span className="min-w-0 flex-1 truncate font-medium">
              {agent.name}
            </span>
            <span className="shrink-0 tabular-nums text-muted-foreground">
              {t(($) => $.agent_activity.runs_count, {
                count: agent.running_run_count,
              })}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * Workspace-wide agents-working filter chip.
 *
 * Its data comes from the independent GET /api/working-agents projection, so
 * Table pagination and locally loaded rows can never turn a known count into
 * an indeterminate "—". Tasks uses the workspace task projection; My Tasks
 * passes its authenticated relation so the server scopes the count. Clicking
 * the control only toggles view state; the Table controller translates these
 * same returned ids into assignee filters.
 */
export function WorkspaceAgentWorkingChip({
  value,
  onToggle,
  mineRelation,
}: WorkspaceAgentWorkingChipProps) {
  const { t } = useT("tasks");
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(
    workspaceWorkingAgentsOptions(wsId, "task", mineRelation),
  );
  const agentIds = agents.map((agent) => agent.id);
  const agentCount = agents.length;
  const hasAgents = agentCount > 0;
  const label = t(($) => $.agent_activity.chip_agents_working, {
    count: agentCount,
  });
  const appearance = chipAppearance(value, hasAgents);

  const trigger = (
    <Button
      variant={appearance.variant}
      size="sm"
      className={appearance.className}
      onClick={onToggle}
      aria-pressed={value}
      aria-label={label}
    >
      {hasAgents && <AgentAvatarStack agentIds={agentIds} size="sm" max={3} />}
      <span className="tabular-nums md:hidden">{agentCount}</span>
      <span className="hidden tabular-nums md:inline">{label}</span>
    </Button>
  );

  return (
    <HoverCard>
      <HoverCardTrigger render={trigger} />
      <HoverCardContent align="end" className="w-72">
        <WorkingAgentsHoverContent agents={agents} />
      </HoverCardContent>
    </HoverCard>
  );
}
