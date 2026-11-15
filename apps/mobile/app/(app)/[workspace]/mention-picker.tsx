/**
 * Workspace-level mention picker route — formSheet, opened from any
 * composer that has an `@` button (currently the task-comment composer
 * and the chat composer).
 *
 * `?mode=` controls which sections render:
 *   - "comment" (default) — @all + People + Agents + Squads + Tasks.
 *     The comment composer offers the full surface; mentions notify the
 *     mentioned actor.
 *   - "chat" — Tasks only. Chat is user ↔ single agent, so member /
 *     agent / squad / @all mentions are noise (and would generate
 *     unintended notifications). Tasks remain useful as "reference this
 *     ticket for the agent's context".
 *
 * Lives at workspace level (not nested under task/[id]) because the chat
 * tab has no per-session route to nest under; making it workspace-level
 * keeps a single route file serving both contexts.
 */
import { useLocalSearchParams } from "expo-router";
import { MentionPickerBody } from "@/components/task/pickers/mention-picker-body";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

type Mode = "comment" | "chat";

export default function MentionPickerRoute() {
  const { mode: rawMode } = useLocalSearchParams<{ mode?: string }>();
  const mode: Mode = rawMode === "chat" ? "chat" : "comment";
  const placeholder =
    mode === "chat" ? "Reference a Task" : "Search people or Tasks";
  const query = useNativeSearchBar(placeholder, { autoFocus: true });
  return <MentionPickerBody mode={mode} query={query} />;
}
