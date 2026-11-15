"use client";

import { ChevronDown } from "lucide-react";
import { Button } from "@silieco/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@silieco/ui/components/ui/dropdown-menu";
import { Tooltip, TooltipTrigger, TooltipContent } from "@silieco/ui/components/ui/tooltip";
import type {
  Task,
  TaskTableFacetSpec,
  TaskTableFacetsResponse,
} from "@silieco/core/types";
import {
  myTasksRelationFromScope,
  type MyTasksScope,
} from "@silieco/core/tasks/stores/my-tasks-view-store";
import { useViewStore } from "@silieco/core/tasks/stores/view-store-context";
import { useT } from "../../i18n";
import { WorkspaceAgentWorkingChip } from "../../tasks/components/workspace-agent-working-chip";
import {
  TaskDisplayControls,
  ViewRefreshIndicator,
} from "../../tasks/components/tasks-header";

export function MyTasksHeader({
  allTasks,
  scope,
  onScopeChange,
  isRefreshing = false,
  facetCountsExact = true,
  tableFacetCounts,
  onTableFacetChange,
}: {
  allTasks: Task[];
  scope: MyTasksScope;
  onScopeChange: (scope: MyTasksScope) => void;
  isRefreshing?: boolean;
  /** See TaskDisplayControls.facetCountsExact. */
  facetCountsExact?: boolean;
  tableFacetCounts?: TaskTableFacetsResponse;
  onTableFacetChange: (facet: TaskTableFacetSpec | null) => void;
}) {
  const { t } = useT("my-tasks");
  const { t: tTasks } = useT("tasks");
  const mineRelation = myTasksRelationFromScope(scope);
  const SCOPES: { value: MyTasksScope; label: string; description: string }[] = [
    { value: "all", label: t(($) => $.header.scope.all_label), description: t(($) => $.header.scope.all_description) },
    { value: "assigned", label: t(($) => $.header.scope.assigned_label), description: t(($) => $.header.scope.assigned_description) },
    { value: "created", label: t(($) => $.header.scope.created_label), description: t(($) => $.header.scope.created_description) },
    { value: "agents", label: t(($) => $.header.scope.agents_label), description: t(($) => $.header.scope.agents_description) },
  ];
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const toggleAgentRunningFilter = useViewStore(
    (s) => s.toggleAgentRunningFilter,
  );
  const scopeLabel = SCOPES.find((s) => s.value === scope)?.label ?? SCOPES[0]?.label;

  return (
    <div className="h-12 shrink-0 overflow-x-auto px-4 [-webkit-overflow-scrolling:touch]">
      <div className="flex h-full w-max min-w-full items-center justify-between gap-2">
        <div className="hidden shrink-0 items-center gap-1 md:flex">
          {SCOPES.map((s) => (
            <Tooltip key={s.value}>
              <TooltipTrigger
                render={
                  <Button
                    variant="outline"
                    size="sm"
                    className={
                      scope === s.value
                        ? "bg-accent text-accent-foreground hover:bg-accent/80"
                        : "text-muted-foreground"
                    }
                    onClick={() => onScopeChange(s.value)}
                  >
                    {s.label}
                  </Button>
                }
              />
              <TooltipContent side="bottom">{s.description}</TooltipContent>
            </Tooltip>
          ))}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className="shrink-0 gap-1 text-muted-foreground md:hidden"
              >
                <span className="truncate">{scopeLabel}</span>
                <ChevronDown className="size-3 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="w-auto">
            <DropdownMenuRadioGroup
              value={scope}
              onValueChange={(value) => onScopeChange(value as MyTasksScope)}
            >
              {SCOPES.map((s) => (
                <DropdownMenuRadioItem key={s.value} value={s.value}>
                  {s.label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <div className="flex shrink-0 items-center gap-1">
          {agentRunningFilter && (
            <span className="mr-1 hidden text-caption text-muted-foreground md:inline">
              {tTasks(($) => $.agent_activity.filter_active_label)}
            </span>
          )}
          <WorkspaceAgentWorkingChip
            value={agentRunningFilter}
            onToggle={toggleAgentRunningFilter}
            mineRelation={mineRelation === "all" ? "any" : mineRelation}
          />
          <TaskDisplayControls
            scopedTasks={allTasks}
            facetCountsExact={facetCountsExact}
            tableFacetCounts={tableFacetCounts}
            onTableFacetChange={onTableFacetChange}
          />
          <ViewRefreshIndicator active={isRefreshing} />
        </div>
      </div>
    </div>
  );
}
