"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { selectRecentContexts, useRecentContextStore, type RecentContextEntry } from "@silieco/core/chat";
import { taskDetailOptions } from "@silieco/core/tasks/queries";
import { projectDetailOptions } from "@silieco/core/projects/queries";
import type { Task, Project } from "@silieco/core/types";
import type { MentionItem } from "../../editor/extensions/mention-suggestion";
import { useNavigation } from "../../navigation";

const MAX_RECENT_MENTION_ITEMS = 8;

function mentionKey(item: Pick<MentionItem, "type" | "id">): string {
  return `${item.type}:${item.id}`;
}

function taskToMentionItem(task: Pick<Task, "id" | "identifier" | "title" | "status">, group: "current" | "recent"): MentionItem {
  return {
    id: task.id,
    label: task.identifier,
    type: "task",
    description: task.title,
    status: task.status,
    group,
  };
}

function projectToMentionItem(project: Pick<Project, "id" | "title" | "description" | "icon" | "status">, group: "current" | "recent"): MentionItem {
  return {
    id: project.id,
    label: project.title,
    type: "project",
    description: project.description ?? undefined,
    icon: project.icon,
    projectStatus: project.status,
    group,
  };
}

function recentEntryToMentionItem(entry: RecentContextEntry): MentionItem {
  return {
    id: entry.id,
    label: entry.label ?? entry.id,
    type: entry.type,
    description: entry.subtitle,
    status: entry.status,
    projectStatus: entry.projectStatus,
    icon: entry.icon,
    group: "recent",
  };
}

function hydrateRecentEntry(entry: RecentContextEntry, data: Task | Project | undefined): MentionItem {
  if (!data) return recentEntryToMentionItem(entry);
  return entry.type === "task"
    ? taskToMentionItem(data as Task, "recent")
    : projectToMentionItem(data as Project, "recent");
}

export function parseCurrentContextRoute(pathname: string, searchParams: URLSearchParams): { type: "task" | "project"; id: string } | null {
  const taskMatch = pathname.match(/^\/[^/]+\/(?:runs|tasks)\/([^/]+)$/);
  if (taskMatch?.[1]) return { type: "task", id: decodeURIComponent(taskMatch[1]) };

  const projectMatch = pathname.match(/^\/[^/]+\/projects\/([^/]+)$/);
  if (projectMatch?.[1]) return { type: "project", id: decodeURIComponent(projectMatch[1]) };

  const inboxMatch = pathname.match(/^\/[^/]+\/inbox$/);
  const inboxTaskId = searchParams.get("task");
  if (inboxMatch && inboxTaskId) return { type: "task", id: inboxTaskId };

  return null;
}

export function useChatContextItems(wsId: string): MentionItem[] {
  const { pathname, searchParams } = useNavigation();
  const currentRoute = parseCurrentContextRoute(pathname, searchParams);
  const recentEntries = useRecentContextStore(selectRecentContexts(wsId));
  const visibleRecentEntries = useMemo(
    () => recentEntries.slice(0, MAX_RECENT_MENTION_ITEMS),
    [recentEntries],
  );

  const { data: currentTask } = useQuery({
    ...taskDetailOptions(wsId, currentRoute?.type === "task" ? currentRoute.id : ""),
    enabled: currentRoute?.type === "task",
  });

  const { data: currentProject } = useQuery({
    ...projectDetailOptions(wsId, currentRoute?.type === "project" ? currentRoute.id : ""),
    enabled: currentRoute?.type === "project",
  });

  const recentQueries = useQueries({
    queries: visibleRecentEntries.map((entry) => ({
      ...(entry.type === "task"
        ? taskDetailOptions(wsId, entry.id)
        : projectDetailOptions(wsId, entry.id)),
      staleTime: 30_000,
    })),
  });

  return useMemo(() => {
    const currentItems: MentionItem[] = [];
    if (currentTask) currentItems.push(taskToMentionItem(currentTask, "current"));
    if (currentProject) currentItems.push(projectToMentionItem(currentProject, "current"));

    const hidden = new Set(currentItems.map(mentionKey));
    const recentItems = visibleRecentEntries
      .map((entry, index) => hydrateRecentEntry(entry, recentQueries[index]?.data as Task | Project | undefined))
      .filter((item) => !hidden.has(mentionKey(item)))
      .slice(0, MAX_RECENT_MENTION_ITEMS);

    return [...currentItems, ...recentItems];
  }, [currentTask, currentProject, recentQueries, visibleRecentEntries]);
}
