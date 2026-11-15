/**
 * Due-date picker route for an existing task.
 *
 * Diverges from the other single-select pickers because the native
 * UIDatePicker needs a confirmation step — the user spins to a date but
 * doesn't auto-commit on every onChange. Done / Clear buttons live in a
 * mini header row inside the route body (the parent Stack hides its own
 * header per the formSheet config), and on submit we fire the mutation +
 * router.back().
 */
import { useRef } from "react";
import { Pressable, View } from "react-native";
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import {
  DueDatePickerBody,
  type DueDatePickerBodyHandle,
} from "@/components/task/pickers/due-date-picker-body";
import { taskDetailOptions } from "@/data/queries/tasks";
import { useUpdateTask } from "@/data/mutations/tasks";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function TaskDueDatePickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: task } = useQuery(taskDetailOptions(wsId, id));
  const updateTask = useUpdateTask(id);
  const ref = useRef<DueDatePickerBodyHandle>(null);

  const value = task?.due_date ?? null;

  return (
    <View className="flex-1">
      <DueDateHeader
        hasValue={!!value}
        onDone={() => {
          const iso = ref.current?.getIso();
          if (iso) updateTask.mutate({ due_date: iso });
          router.back();
        }}
        onClear={() => {
          updateTask.mutate({ due_date: null });
          router.back();
        }}
      />
      <DueDatePickerBody ref={ref} value={value} />
    </View>
  );
}

function DueDateHeader({
  hasValue,
  onDone,
  onClear,
}: {
  hasValue: boolean;
  onDone: () => void;
  onClear: () => void;
}) {
  return (
    <View className="flex-row items-center justify-between px-4 pt-4 pb-2">
      <Text className="text-base font-semibold text-foreground">Due date</Text>
      <View className="flex-row items-center gap-1">
        {hasValue ? (
          <Pressable
            onPress={onClear}
            hitSlop={6}
            className="px-2 py-1 rounded-md active:bg-secondary"
          >
            <Text className="text-sm text-destructive">Clear</Text>
          </Pressable>
        ) : null}
        <Pressable
          onPress={onDone}
          hitSlop={6}
          className="px-2 py-1 rounded-md active:bg-secondary"
        >
          <Text className="text-sm font-medium text-primary">Done</Text>
        </Pressable>
      </View>
    </View>
  );
}
