"use client";

import { useMemo, useState } from "react";
import { useStore } from "zustand";
import { ListTodo, Search } from "lucide-react";
import type {
  Task,
  TaskTableFacetSpec,
  TaskTableFacetsResponse,
} from "@silieco/core/types";
import {
  actorTasksViewStore,
  type ActorTasksScope,
} from "@silieco/core/tasks/stores/actor-tasks-view-store";
import { Button } from "@silieco/ui/components/ui/button";
import { Input } from "@silieco/ui/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@silieco/ui/components/ui/tooltip";
import {
  TaskDisplayControls,
  ViewRefreshIndicator,
} from "../tasks/components/tasks-header";
import { TaskSurface } from "../tasks/surface/task-surface";
import { useT } from "../i18n";

export type TaskActorType = "member" | "agent";

const SCOPE_VALUES: ActorTasksScope[] = ["assigned", "created"];

function ActorTasksHeader({
  tasks,
  search,
  onSearchChange,
  scope,
  onScopeChange,
  isRefreshing = false,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  tasks: Task[];
  search: string;
  onSearchChange: (value: string) => void;
  scope: ActorTasksScope;
  onScopeChange: (scope: ActorTasksScope) => void;
  isRefreshing?: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: TaskTableFacetsResponse;
  onTableFacetChange: (facet: TaskTableFacetSpec | null) => void;
}) {
  const { t } = useT("tasks");

  return (
    <div className="flex h-12 shrink-0 items-center justify-between gap-3 border-b px-4">
      <div className="flex items-center gap-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t(($) => $.actor_tasks.search_placeholder)}
            className="h-8 w-64 pl-8 text-body"
          />
        </div>
        <div className="flex items-center gap-1">
          {SCOPE_VALUES.map((value) => (
            <Tooltip key={value}>
              <TooltipTrigger
                render={
                  <Button
                    variant="outline"
                    size="sm"
                    className={
                      scope === value
                        ? "bg-accent text-accent-foreground hover:bg-accent/80"
                        : "text-muted-foreground"
                    }
                    onClick={() => onScopeChange(value)}
                  >
                    {t(($) => $.actor_tasks.scope[value].label)}
                  </Button>
                }
              />
              <TooltipContent side="bottom">
                {t(($) => $.actor_tasks.scope[value].description)}
              </TooltipContent>
            </Tooltip>
          ))}
        </div>
      </div>
      <div className="flex items-center">
        <TaskDisplayControls
          scopedTasks={tasks}
          hideViewToggle
          facetCountsExact={facetCountsExact}
          tableFacetCounts={tableFacetCounts}
          onTableFacetChange={onTableFacetChange}
        />
        <ViewRefreshIndicator active={isRefreshing} />
      </div>
    </div>
  );
}

export function ActorTasksPanel({
  actorType,
  actorId,
}: {
  actorType: TaskActorType;
  actorId: string;
}) {
  const { t } = useT("tasks");
  const scope = useStore(actorTasksViewStore, (s) => s.scope);
  const setScope = useStore(actorTasksViewStore, (s) => s.setScope);
  const [search, setSearch] = useState("");
  const surfaceScope = useMemo(
    () =>
      ({
        type: "actor" as const,
        actorType,
        actorId,
        relation: scope,
      }),
    [actorId, actorType, scope],
  );

  return (
    <TaskSurface
      scope={surfaceScope}
      modes={["list"]}
      batchToolbar="always"
      contentClassName="p-1"
      search={search}
      renderHeader={({ controller }) => (
        <ActorTasksHeader
          tasks={controller.surfaceTasks}
          search={search}
          onSearchChange={setSearch}
          scope={scope}
          onScopeChange={setScope}
          isRefreshing={controller.isRefreshing}
          facetCountsExact={controller.facetCountsExact}
          tableFacetCounts={controller.tableFacetCounts}
          onTableFacetChange={controller.setActiveTableFacet}
        />
      )}
      renderEmpty={() =>
        search.trim() === "" ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-body">
              {t(($) => $.actor_tasks.empty[scope].title)}
            </p>
            <p className="text-caption">
              {t(($) => $.actor_tasks.empty[scope].description)}
            </p>
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <Search className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-body">{t(($) => $.actor_tasks.search_empty)}</p>
          </div>
        )
      }
    />
  );
}
