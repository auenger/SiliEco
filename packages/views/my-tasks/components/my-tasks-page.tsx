"use client";

import { useStore } from "zustand";
import { ListTodo } from "lucide-react";
import { useAuthStore } from "@silieco/core/auth";
import {
  myTasksRelationFromScope,
  myTasksViewStore,
} from "@silieco/core/tasks/stores/my-tasks-view-store";
import { PageHeader } from "../../layout/page-header";
import { TaskSurface } from "../../tasks/surface/task-surface";
import { useT } from "../../i18n";
import { MyTasksHeader } from "./my-tasks-header";

export function MyTasksPage() {
  const { t } = useT("my-tasks");
  const user = useAuthStore((s) => s.user);
  const scope = useStore(myTasksViewStore, (s) => s.scope);
  const setScope = useStore(myTasksViewStore, (s) => s.setScope);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-body font-medium">{t(($) => $.page.breadcrumb)}</h1>
      </PageHeader>

      {user ? (
        <TaskSurface
          scope={{
            type: "my",
            userId: user.id,
            relation: myTasksRelationFromScope(scope),
          }}
          modes={["board", "list", "table", "swimlane"]}
          batchToolbar="list"
          renderHeader={({ controller }) => (
            <MyTasksHeader
              allTasks={controller.surfaceTasks}
              scope={scope}
              onScopeChange={setScope}
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
              <p className="text-caption">{t(($) => $.page.empty_description)}</p>
            </div>
          )}
        />
      ) : null}
    </div>
  );
}
