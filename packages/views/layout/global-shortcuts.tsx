"use client";

import { useEffect } from "react";
import { useSidebar } from "@silieco/ui/components/ui/sidebar";
import {
  getShortcut,
  isEditableShortcutTarget,
  shortcutMatchesEvent,
  SHORTCUT_ACTION_BY_ID,
  useShortcutStore,
  type ShortcutActionId,
} from "@silieco/core/shortcuts";
import { openCreateTaskWithPreference } from "@silieco/core/tasks/stores";
import { useModalStore } from "@silieco/core/modals";
import { useWorkspacePaths } from "@silieco/core/paths";
import { isImeComposing } from "@silieco/core/utils";
import { useNavigation } from "../navigation";
import { useSearchStore } from "../search/search-store";

const GLOBAL_ACTIONS: readonly ShortcutActionId[] = [
  "openSearch",
  "createTask",
  "toggleSidebar",
  "goInbox",
  "goChat",
  "goMyTasks",
  "goTasks",
  "goProjects",
  "goAutopilots",
  "goAgents",
  "goSquads",
  "goUsage",
  "goRuntimes",
  "goSkills",
  "goSettings",
];

export function shouldIgnoreGlobalShortcutEvent(event: KeyboardEvent): boolean {
  return event.defaultPrevented || event.repeat || isImeComposing(event);
}

/** Executes configurable product-level shortcuts inside the dashboard shell. */
export function GlobalShortcuts() {
  const { toggleSidebar } = useSidebar();
  const navigation = useNavigation();
  const workspacePaths = useWorkspacePaths();

  // Subscribe so changing a binding in Settings immediately refreshes the
  // listener closure; getShortcut remains useful to non-React call sites.
  const overrides = useShortcutStore((state) => state.overrides);

  useEffect(() => {
    const destinations: Partial<Record<ShortcutActionId, string>> = {
      goInbox: workspacePaths.inbox(),
      goChat: workspacePaths.chat(),
      goMyTasks: workspacePaths.myTasks(),
      goTasks: workspacePaths.tasks(),
      goProjects: workspacePaths.projects(),
      goAutopilots: workspacePaths.autopilots(),
      goAgents: workspacePaths.agents(),
      goSquads: workspacePaths.squads(),
      goUsage: workspacePaths.usage(),
      goRuntimes: workspacePaths.runtimes(),
      goSkills: workspacePaths.skills(),
      goSettings: workspacePaths.settings(),
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      // Component/editor handlers run before this document-level listener.
      // Respect their preventDefault instead of double-triggering a product
      // action after the focused control already consumed the same chord.
      if (shouldIgnoreGlobalShortcutEvent(event)) return;

      const actionId = GLOBAL_ACTIONS.find((candidate) => {
        const action = SHORTCUT_ACTION_BY_ID[candidate];
        if (!action.allowInEditable && isEditableShortcutTarget(event.target)) {
          return false;
        }
        return shortcutMatchesEvent(getShortcut(candidate), event);
      });
      if (!actionId) return;

      event.preventDefault();
      if (actionId === "openSearch") {
        useSearchStore.getState().toggle();
        return;
      }
      if (actionId === "toggleSidebar") {
        toggleSidebar();
        return;
      }
      if (actionId === "createTask") {
        if (useModalStore.getState().modal) return;
        const projectMatch = navigation.pathname.match(
          /^\/[^/]+\/projects\/([^/]+)$/,
        );
        const data = projectMatch
          ? { project_id: projectMatch[1] }
          : undefined;
        openCreateTaskWithPreference(data);
        return;
      }

      const destination = destinations[actionId];
      if (destination && destination !== navigation.pathname) {
        navigation.push(destination);
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [navigation, overrides, toggleSidebar, workspacePaths]);

  return null;
}
