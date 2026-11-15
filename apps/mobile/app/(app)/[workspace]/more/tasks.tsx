/**
 * Workspace-wide Tasks page. Mirrors web `packages/views/tasks/components/
 * tasks-page.tsx:32-94`: fetch every task in the workspace, expose
 * `all / members / agents` scope tabs, group by status, allow status +
 * priority filtering.
 *
 * Scope is a **client-side** filter on `assignee_type` — matches web
 * `tasks-page.tsx:90-94`. This keeps `taskListOptions(wsId)` workspace-
 * scoped (no scope param on the wire), so `taskKeys.list(wsId)` and
 * `useTasksRealtime` need no changes.
 *
 * Differences vs My Tasks (`(tabs)/my-tasks.tsx`):
 *   - Workspace-wide list (all tasks), not user-scoped.
 *   - Three scopes are `all / members / agents` (assignee_type pre-filter),
 *     not `assigned / created / agents` (per-user predicates).
 *   - Independent filter store (`useTasksViewStore`) so workspace-level
 *     filters don't bleed into the per-user view.
 *
 * Filters beyond status/priority (assignee / project / label / creator)
 * are deferred — power-user features with non-trivial picker cost; ship
 * after the parity-critical scope tabs land.
 */
import { useMemo } from "react";
import { Pressable, SectionList, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import type { Task, TaskPriority, TaskStatus } from "@silieco/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
// Header chrome (back + "Tasks" title) comes from the parent Stack
// (`apps/mobile/app/(app)/[workspace]/_layout.tsx:269`). The Filter
// affordance now lives in <ScopeToolbar> below, matching web's
// TasksHeader pattern (scope + filter share a row).
import { StatusIcon } from "@/components/ui/status-icon";
import { TaskRow } from "@/components/task/task-row";
import { TasksLoading } from "@/components/task/tasks-loading";
import { taskListOptions } from "@/data/queries/tasks";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useTasksViewStore,
  type TasksScope,
} from "@/data/stores/tasks-view-store";
import { useClearFiltersOnWorkspaceChange } from "@/lib/use-clear-filters-on-workspace-change";
import {
  BOARD_STATUSES,
  PRIORITY_LABEL,
  STATUS_LABEL,
} from "@/lib/task-status";
import { filterTasks } from "@/lib/filter-tasks";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

type TaskSection = { status: TaskStatus; data: Task[] };

// Scope tab definitions. Mirrors web `tasksScopeStore`. Counts are NOT
// rendered on the pill labels — web's `TasksHeader` doesn't show them
// either, and on SE3 (375pt) "(123)" appended to each label pushes the
// row past the safe width when filter icon shares the row. Per-status
// counts still appear on the SectionList headers below.
const SCOPES: { value: TasksScope; label: string }[] = [
  { value: "all", label: "All" },
  { value: "members", label: "Members" },
  { value: "agents", label: "Agents" },
];

export default function TasksPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const scope = useTasksViewStore((s) => s.scope);
  const setScope = useTasksViewStore((s) => s.setScope);
  const statusFilters = useTasksViewStore((s) => s.statusFilters);
  const priorityFilters = useTasksViewStore((s) => s.priorityFilters);

  const openFilter = () => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/tasks-filter",
      params: { workspace: wsSlug, scope: "all" },
    });
  };

  useClearFiltersOnWorkspaceChange(
    useTasksViewStore.getState().clearFilters,
    wsId,
  );

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    taskListOptions(wsId),
  );

  const allTasks = data ?? [];

  // Scope pre-filter — mirrors web `tasks-page.tsx:90-94`. Applied before
  // status/priority filtering so chip filters operate on the visible slice.
  const scopedTasks = useMemo(() => {
    if (scope === "members") {
      return allTasks.filter((i) => i.assignee_type === "member");
    }
    if (scope === "agents") {
      return allTasks.filter(
        (i) => i.assignee_type === "agent" || i.assignee_type === "squad",
      );
    }
    return allTasks;
  }, [allTasks, scope]);

  const filtered = useMemo(
    () => filterTasks(scopedTasks, statusFilters, priorityFilters),
    [scopedTasks, statusFilters, priorityFilters],
  );

  // Section grouping uses BOARD_STATUSES (cancelled excluded) — matches web
  // `tasks-page.tsx:117-125`.
  const sections = useMemo<TaskSection[]>(() => {
    if (filtered.length === 0) return [];
    const byStatus = new Map<TaskStatus, Task[]>();
    for (const task of filtered) {
      const list = byStatus.get(task.status);
      if (list) list.push(task);
      else byStatus.set(task.status, [task]);
    }
    const visibleStatuses =
      statusFilters.length > 0
        ? BOARD_STATUSES.filter((s) => statusFilters.includes(s))
        : BOARD_STATUSES;
    return visibleStatuses
      .map((status) => ({ status, data: byStatus.get(status) ?? [] }))
      .filter((s) => s.data.length > 0);
  }, [filtered, statusFilters]);

  const hasActiveFilters =
    statusFilters.length > 0 || priorityFilters.length > 0;

  const showEmptyState = !isLoading && !error && filtered.length === 0;

  return (
    <View className="flex-1 bg-background">
      <ScopeToolbar
        scopes={SCOPES}
        scope={scope}
        onChange={(v) => setScope(v)}
        onOpenFilter={openFilter}
        hasActiveFilters={hasActiveFilters}
      />
      {hasActiveFilters ? (
        <ActiveFilterChips
          statusFilters={statusFilters}
          priorityFilters={priorityFilters}
          onClearStatus={(s) =>
            useTasksViewStore.getState().toggleStatusFilter(s)
          }
          onClearPriority={(p) =>
            useTasksViewStore.getState().togglePriorityFilter(p)
          }
        />
      ) : null}
      {isLoading ? (
        <TasksLoading />
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load tasks:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : showEmptyState ? (
        <EmptyState
          message={
            hasActiveFilters
              ? "No tasks match the current filters."
              : emptyMessageForScope(scope)
          }
        />
      ) : (
        <SectionList
          sections={sections}
          keyExtractor={(item) => item.id}
          stickySectionHeadersEnabled={false}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderSectionHeader={({ section }) => (
            <SectionHeader status={section.status} count={section.data.length} />
          )}
          contentContainerClassName="pb-6"
          renderItem={({ item }) => (
            <TaskRow
              task={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/task/${item.id}`);
              }}
            />
          )}
          refreshing={isRefetching}
          onRefresh={refetch}
        />
      )}
    </View>
  );
}

/**
 * Outline icon button matching the pill height. Identical to the helper in
 * `(tabs)/my-tasks.tsx` for the same reason ScopeToolbar is duplicated:
 * two callers don't justify a shared primitive yet.
 */
function FilterButton({
  onPress,
  hasActiveFilters,
}: {
  onPress: () => void;
  hasActiveFilters: boolean;
}) {
  const { colorScheme } = useColorScheme();
  return (
    <View style={{ position: "relative" }} className="ml-2">
      <Button
        variant="outline"
        size="sm"
        onPress={onPress}
        accessibilityLabel="Filter"
        className="w-9 px-0"
      >
        <Ionicons
          name="options-outline"
          size={16}
          color={THEME[colorScheme].mutedForeground}
        />
      </Button>
      {hasActiveFilters ? (
        <View
          pointerEvents="none"
          className="absolute top-1 right-1 size-1.5 rounded-full bg-brand"
        />
      ) : null}
    </View>
  );
}

/**
 * Toolbar row mirroring web `TasksHeader`
 * (`packages/views/tasks/components/tasks-header.tsx:516-543`): left-aligned
 * scope pill group + right-side Filter icon (red dot on active filters).
 * Identical to the equivalent in `(tabs)/my-tasks.tsx` — kept duplicated
 * because the threshold for a shared `components/ui/` primitive is 3 callers,
 * and two callers don't justify the abstraction yet.
 */
function ScopeToolbar<S extends string>({
  scopes,
  scope,
  onChange,
  onOpenFilter,
  hasActiveFilters,
}: {
  scopes: { value: S; label: string }[];
  scope: S;
  onChange: (value: S) => void;
  onOpenFilter: () => void;
  hasActiveFilters: boolean;
}) {
  return (
    <View className="flex-row items-center justify-between px-4 pt-2 pb-2">
      <View className="flex-row items-center gap-1 flex-shrink min-w-0">
        {scopes.map((s) => {
          const active = scope === s.value;
          return (
            <Button
              key={s.value}
              variant="outline"
              size="sm"
              onPress={() => onChange(s.value)}
              className={active ? "bg-accent" : ""}
              accessibilityState={{ selected: active }}
            >
              <Text
                numberOfLines={1}
                className={active ? "text-accent-foreground" : "text-muted-foreground"}
              >
                {s.label}
              </Text>
            </Button>
          );
        })}
      </View>
      <FilterButton
        onPress={onOpenFilter}
        hasActiveFilters={hasActiveFilters}
      />
    </View>
  );
}

function ActiveFilterChips({
  statusFilters,
  priorityFilters,
  onClearStatus,
  onClearPriority,
}: {
  statusFilters: TaskStatus[];
  priorityFilters: TaskPriority[];
  onClearStatus: (s: TaskStatus) => void;
  onClearPriority: (p: TaskPriority) => void;
}) {
  return (
    <View className="flex-row flex-wrap gap-1.5 px-4 pb-2">
      {statusFilters.map((s) => (
        <Chip
          key={`s-${s}`}
          label={STATUS_LABEL[s]}
          onClear={() => onClearStatus(s)}
        />
      ))}
      {priorityFilters.map((p) => (
        <Chip
          key={`p-${p}`}
          label={PRIORITY_LABEL[p]}
          onClear={() => onClearPriority(p)}
        />
      ))}
    </View>
  );
}

function Chip({ label, onClear }: { label: string; onClear: () => void }) {
  const { colorScheme } = useColorScheme();
  return (
    <Pressable
      onPress={onClear}
      className="flex-row items-center gap-1 pl-2.5 pr-2 py-1 rounded-full border border-border bg-secondary/40 active:bg-secondary"
    >
      <Text className="text-xs text-foreground">{label}</Text>
      <Ionicons
        name="close"
        size={12}
        color={THEME[colorScheme].mutedForeground}
      />
    </Pressable>
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

function EmptyState({ message }: { message: string }) {
  return (
    <View className="flex-1 items-center justify-center px-6">
      <Text className="text-sm text-muted-foreground text-center">
        {message}
      </Text>
    </View>
  );
}

function emptyMessageForScope(scope: TasksScope): string {
  switch (scope) {
    case "all":
      return "No tasks in this workspace.";
    case "members":
      return "No tasks assigned to a member.";
    case "agents":
      return "No tasks assigned to agents or squads.";
  }
}
