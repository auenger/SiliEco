/**
 * Tasks belonging to a project — status-grouped list.
 *
 * Mobile intentionally does NOT implement web's Board (kanban) view, only
 * the List form. Reasons:
 *   - Phone screens are too narrow to show ≥3 status columns at once, so
 *     kanban loses its core "see all-statuses pipeline at a glance" value;
 *     users end up swiping between near-empty columns.
 *   - Major mobile task apps (Linear iOS, Things, Apple Reminders) don't
 *     ship kanban either — list with status grouping is the established
 *     small-screen pattern for the same data.
 *   - This is a UI divergence, NOT semantic divergence (per
 *     mobile/CLAUDE.md "Behavioral parity"): same tasks, same status
 *     enum, same 6 BOARD_STATUSES grouping as web — only the layout
 *     differs. UI may diverge when semantics agree.
 *
 * Status grouping uses full `BOARD_STATUSES` (six visible groups, cancelled
 * excluded) to match web `packages/views/projects/components/project-detail.tsx`.
 * The earlier mobile-only "Open / Done" two-bucket layout was a parity
 * violation: same status enum value would appear in different visible
 * groups on mobile vs web. Cancelled is omitted on both clients.
 */
import { useMemo } from "react";
import { View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type { Task, TaskStatus } from "@silieco/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { StatusIcon } from "@/components/ui/status-icon";
import { TaskRow } from "@/components/task/task-row";
import { TasksLoading } from "@/components/task/tasks-loading";
import { projectTasksOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { BOARD_STATUSES, STATUS_LABEL } from "@/lib/task-status";

interface Props {
  projectId: string;
}

export function ProjectRelatedTasks({ projectId }: Props) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(
    projectTasksOptions(wsId, projectId),
  );

  const byStatus = useMemo(() => {
    const m = new Map<TaskStatus, Task[]>();
    for (const status of BOARD_STATUSES) m.set(status, []);
    for (const task of data ?? []) {
      const list = m.get(task.status);
      if (list) list.push(task);
    }
    return m;
  }, [data]);

  const navigateToTask = (id: string) => {
    if (wsSlug) router.push(`/${wsSlug}/task/${id}`);
  };

  if (isLoading) return <TasksLoading />;

  if (error) {
    return (
      <View className="px-4 py-6 gap-3">
        <Text className="text-sm text-destructive">
          Failed to load tasks:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  if ((data?.length ?? 0) === 0) {
    return (
      <View className="px-4 py-6">
        <Text className="text-sm text-muted-foreground">No tasks yet.</Text>
      </View>
    );
  }

  return (
    <View>
      {BOARD_STATUSES.map((status) => {
        const tasks = byStatus.get(status) ?? [];
        if (tasks.length === 0) return null;
        return (
          <View key={status}>
            <SectionHeader status={status} count={tasks.length} />
            {tasks.map((task) => (
              <TaskRow
                key={task.id}
                task={task}
                onPress={() => navigateToTask(task.id)}
              />
            ))}
          </View>
        );
      })}
    </View>
  );
}

function SectionHeader({
  status,
  count,
}: {
  status: TaskStatus;
  count: number;
}) {
  return (
    <View className="flex-row items-center gap-2 px-4 py-2 bg-background">
      <StatusIcon status={status} size={14} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {STATUS_LABEL[status]}
      </Text>
      <Text className="text-xs text-muted-foreground/60">{count}</Text>
    </View>
  );
}
