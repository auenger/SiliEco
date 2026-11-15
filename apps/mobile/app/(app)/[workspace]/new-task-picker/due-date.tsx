/**
 * Due-date picker route for the in-progress new-task draft. See ./status.tsx.
 *
 * Same Done / Clear pattern as the task-detail variant
 * (`task/[id]/picker/due-date.tsx`) — UIDatePicker doesn't auto-commit, so
 * the route renders a tiny header with action buttons.
 */
import { useRef } from "react";
import { Pressable, View } from "react-native";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import {
  DueDatePickerBody,
  type DueDatePickerBodyHandle,
} from "@/components/task/pickers/due-date-picker-body";
import { useNewTaskDraftStore } from "@/data/stores/new-task-draft-store";

export default function NewTaskDueDatePickerRoute() {
  const dueDate = useNewTaskDraftStore((s) => s.dueDate);
  const setDueDate = useNewTaskDraftStore((s) => s.setDueDate);
  const ref = useRef<DueDatePickerBodyHandle>(null);

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pt-4 pb-2">
        <Text className="text-base font-semibold text-foreground">
          Due date
        </Text>
        <View className="flex-row items-center gap-1">
          {dueDate ? (
            <Pressable
              onPress={() => {
                setDueDate(null);
                router.back();
              }}
              hitSlop={6}
              className="px-2 py-1 rounded-md active:bg-secondary"
            >
              <Text className="text-sm text-destructive">Clear</Text>
            </Pressable>
          ) : null}
          <Pressable
            onPress={() => {
              const iso = ref.current?.getIso();
              if (iso) setDueDate(iso);
              router.back();
            }}
            hitSlop={6}
            className="px-2 py-1 rounded-md active:bg-secondary"
          >
            <Text className="text-sm font-medium text-primary">Done</Text>
          </Pressable>
        </View>
      </View>
      <DueDatePickerBody ref={ref} value={dueDate} />
    </View>
  );
}
