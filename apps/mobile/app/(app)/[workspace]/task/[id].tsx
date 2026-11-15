/**
 * Task detail screen.
 *
 * Read-mostly timeline with an inline comment composer pinned to the
 * bottom (`<InlineCommentComposer>`). The composer is a single
 * `<TextInput>` + mention suggestion bar — no modal route, no toolbar,
 * no draft persistence. Sticks to the keyboard via `KeyboardStickyView`.
 *
 * Header note: the parent _layout.tsx already declares the `task/[id]`
 * Stack.Screen with title "Task". We override that here once the data
 * lands so the navigation bar shows `SILI-123` (Linear-style).
 */
import { useCallback, useEffect } from "react";
import {
  ActionSheetIOS,
  ActivityIndicator,
  Alert,
  Linking,
  View,
} from "react-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import * as Clipboard from "expo-clipboard";
import type { Task } from "@silieco/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { TimelineList } from "@/components/task/timeline-list";
import { AgentHeaderBadge } from "@/components/task/agent-header-badge";
import { InlineCommentComposer } from "@/components/task/inline-comment-composer";
import {
  taskDetailOptions,
  taskKeys,
  taskTimelineOptions,
} from "@/data/queries/tasks";
import { useDeleteTask } from "@/data/mutations/tasks";
import { pinListOptions } from "@/data/queries/pins";
import { useCreatePin, useDeletePin } from "@/data/mutations/pins";
import { useAuthStore } from "@/data/auth-store";
import { useTaskRealtime } from "@/data/realtime/use-task-realtime";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useViewedTasksStore } from "@/data/viewed-tasks-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";

export default function TaskDetail() {
  // `highlight` + `h` come from inbox deep-link (apps/mobile/app/(app)/
  // [workspace]/(tabs)/inbox.tsx). `highlight` is the target comment id;
  // `h` is a per-tap nonce so re-tapping the same row re-fires the
  // scroll-and-flash effect.
  const { id, workspace: wsSlug, highlight, h } = useLocalSearchParams<{
    id: string;
    workspace: string;
    highlight?: string;
    h?: string;
  }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();

  const detail = useQuery(taskDetailOptions(wsId, id));
  const timeline = useQuery(taskTimelineOptions(wsId, id));

  // Subscribe to per-task WS events: status/priority/assignee/label
  // changes, comments, activity, reactions, agent task progress.
  // Mounted with `id` — cleans up automatically on navigate-away.
  // If another client deletes the task we're viewing, pop back so the
  // user isn't stranded on a 404 detail page.
  useTaskRealtime(id, () => router.back());

  // Track viewed tasks so the chat composer's `@` suggestion bar can
  // surface "Recent" — the user just looked at SILI-123, likely wants to
  // ask the agent about it next. Workspace-scoped + in-memory; see
  // data/viewed-tasks-store.ts.
  useEffect(() => {
    if (wsId && id) {
      useViewedTasksStore.getState().push(wsId, id);
    }
  }, [wsId, id]);

  // Screen-scoped composer state — clear on unmount so re-entering the
  // task starts from a clean slate (no stale text-selection comment id,
  // no stale "Replying to X" target). Both stores are singletons used by
  // the long-press action sheet.
  useEffect(() => {
    return () => {
      useCommentSelectStore.getState().clear();
      useReplyTargetStore.getState().clear();
    };
  }, []);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: taskKeys.timeline(wsId, id) }),
    ]);
  }, [detail, qc, wsId, id]);

  const task = detail.data;
  const deleteTask = useDeleteTask();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: pins } = useQuery(pinListOptions(wsId, userId));
  const isPinned =
    !!task &&
    !!pins?.some((p) => p.item_type === "task" && p.item_id === task.id);
  const createPin = useCreatePin();
  const deletePin = useDeletePin();

  // Three-dot menu: Pin/Unpin / Copy link / Open on web (if web URL set) /
  // Delete. Mirrors apps/mobile/app/(app)/[workspace]/project/[id].tsx — same
  // ActionSheetIOS + Alert.alert confirm pattern. Property edits (status,
  // priority, assignee, due_date) live on the TaskHeaderCard chips inside
  // the timeline list, not in this menu — one entry per action.
  const onPressMore = useCallback(() => {
    if (!task || !wsSlug) return;
    const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const taskLink = webUrl
      ? `${webUrl}/${wsSlug}/task/${task.identifier}`
      : null;
    const options: string[] = ["Cancel"];
    options.push(isPinned ? "Unpin" : "Pin");
    options.push("Edit details");
    if (taskLink) options.push("Copy link");
    if (taskLink) options.push("Open on web");
    options.push("Delete task");
    const destructiveIndex = options.length - 1;
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options,
        cancelButtonIndex: 0,
        destructiveButtonIndex: destructiveIndex,
        title: task.identifier,
      },
      (i) => {
        const label = options[i];
        if (label === "Pin") {
          createPin.mutate({ item_type: "task", item_id: task.id });
        } else if (label === "Unpin") {
          deletePin.mutate({ itemType: "task", itemId: task.id });
        } else if (label === "Edit details") {
          if (wsSlug) router.push(`/${wsSlug}/task/${task.id}/edit`);
        } else if (label === "Copy link" && taskLink) {
          Clipboard.setStringAsync(taskLink);
        } else if (label === "Open on web" && taskLink) {
          Linking.openURL(taskLink);
        } else if (label === "Delete task") {
          confirmDelete(task, () =>
            deleteTask.mutate(task.id, {
              onSuccess: () => router.back(),
            }),
          );
        }
      },
    );
  }, [task, wsSlug, deleteTask, isPinned, createPin, deletePin]);

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          title: task?.identifier ?? "Task",
          headerBackTitle: "Back",
          headerRight: task
            ? () => (
                <View className="flex-row items-center gap-2">
                  {/* Ambient agent-working badge — renders null when no
                   *  active tasks, so it doesn't crowd the header in the
                   *  common case. See agent-header-badge.tsx. */}
                  <AgentHeaderBadge taskId={id} />
                  <IconButton
                    name="ellipsis-horizontal"
                    onPress={onPressMore}
                    accessibilityLabel="Task actions"
                  />
                </View>
              )
            : undefined,
        }}
      />
      {detail.isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : detail.error || !task ? (
        <View className="flex-1 items-center justify-center px-6 gap-3">
          <Text className="text-sm text-destructive text-center">
            Failed to load task:{" "}
            {detail.error instanceof Error
              ? detail.error.message
              : "not found"}
          </Text>
          <Button variant="outline" onPress={() => detail.refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : (
        <View className="flex-1">
          <TimelineList
            task={task}
            entries={timeline.data}
            timelineLoading={timeline.isLoading}
            refreshing={detail.isRefetching || timeline.isRefetching}
            onRefresh={onRefresh}
            highlightCommentId={highlight}
            highlightNonce={h}
          />
          <InlineCommentComposer taskId={id} />
        </View>
      )}
    </View>
  );
}

function confirmDelete(task: Task, onConfirm: () => void) {
  Alert.alert(
    "Delete task?",
    `${task.identifier} and its comments, reactions, and attachments will be permanently deleted. This cannot be undone.`,
    [
      { text: "Cancel", style: "cancel" },
      { text: "Delete", style: "destructive", onPress: onConfirm },
    ],
  );
}
