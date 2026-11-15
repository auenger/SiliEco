"use client";

import { ListTodo, Plus } from "lucide-react";
import { useModalStore } from "@silieco/core/modals";
import type {
  Task,
  TaskTableFacetSpec,
  TaskTableFacetsResponse,
} from "@silieco/core/types";
import { useTasksScopeStore } from "@silieco/core/tasks/stores/tasks-scope-store";
import { useViewStore } from "@silieco/core/tasks/stores/view-store-context";
import { PageHeader } from "../../layout/page-header";
import { Button } from "@silieco/ui/components/ui/button";
import { useT } from "../../i18n";
import { TaskSurface } from "../surface/task-surface";
import { TasksHeader } from "./tasks-header";

function TasksSurfaceHeader({
  tasks,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  tasks: Task[];
  isRefreshing: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: TaskTableFacetsResponse;
  onTableFacetChange: (facet: TaskTableFacetSpec | null) => void;
}) {
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);

  return (
    <TasksHeader
      scopedTasks={tasks}
      dateFilter={dateFilter}
      onDateFilterChange={setDateFilter}
      isRefreshing={isRefreshing}
      facetCountsExact={facetCountsExact}
      tableFacetCounts={tableFacetCounts}
      onTableFacetChange={onTableFacetChange}
    />
  );
}

export function TasksPage() {
  const { t } = useT("tasks");
  const scope = useTasksScopeStore((s) => s.scope);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-body font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
        <Button
          size="sm"
          className="ml-auto"
          onClick={() => useModalStore.getState().open("create-task", {})}
        >
          <Plus className="size-3.5" />
          {t(($) => $.page.create_task)}
        </Button>
      </PageHeader>

      <TaskSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <TasksSurfaceHeader
            tasks={controller.surfaceTasks}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={controller.facetCountsExact}
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
          />
        )}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-body">{t(($) => $.page.empty_title)}</p>
            <p className="text-caption">{t(($) => $.page.empty_hint)}</p>
          </div>
        )}
      />
    </div>
  );
}
