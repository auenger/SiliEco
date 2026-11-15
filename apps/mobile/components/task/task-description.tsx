/**
 * Description block. Renders markdown via the standalone mobile markdown
 * renderer at apps/mobile/lib/markdown/. Empty / null descriptions show
 * a muted "No description." placeholder rather than collapsing the block,
 * so the layout above the timeline stays stable when the user adds a
 * description later.
 *
 * Attachments are fetched per-task so markdown can resolve `mc://file/<id>`
 * image URIs into real `download_url` HTTPS endpoints — without this the
 * iOS image loader doesn't understand the mc: scheme and the image fails.
 * TanStack Query dedupes the request across this component and CommentCard
 * (both call `taskAttachmentsOptions(wsId, taskId)`), so only one
 * network roundtrip fires per task.
 */
import { View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Markdown } from "@/lib/markdown";
import { taskAttachmentsOptions } from "@/data/queries/tasks";
import { useWorkspaceStore } from "@/data/workspace-store";

export function TaskDescription({
  taskId,
  description,
}: {
  taskId: string;
  description: string | null;
}) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: attachments } = useQuery(
    taskAttachmentsOptions(wsId, taskId),
  );

  if (!description || description.trim().length === 0) {
    return (
      <View className="px-4 pb-4">
        <Text className="text-sm text-muted-foreground italic">
          No description.
        </Text>
      </View>
    );
  }
  return (
    <View className="px-4 pb-4">
      <Markdown content={description} attachments={attachments} />
    </View>
  );
}
