"use client";

import { useQuery } from "@tanstack/react-query";
import { taskIdentifierOptions } from "@silieco/core/tasks/queries";
import { useCurrentWorkspace } from "@silieco/core/paths";
import { isTaskIdentifier } from "@silieco/ui/markdown";
import type { Task } from "@silieco/core/types";

/**
 * Resolve a bare task identifier ("SILI-123") to a real task in the current
 * workspace, or `null`. Backs the Linear-style autolink render path.
 *
 * Server state → TanStack Query (key includes wsId + identifier, so identical
 * identifiers across many comments/messages share one request).
 *
 * The query is short-circuited (no network) when:
 *   - there is no current workspace, or
 *   - the token is not identifier-shaped, or
 *   - the identifier's prefix cannot match the workspace's `task_prefix`.
 *
 * When the prefix is unknown (workspace list still loading) we fall through to
 * the query and let the exact-match filter in `taskIdentifierOptions` decide.
 */
export function useResolveTaskIdentifier(identifier: string): Task | null {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const prefix = workspace?.task_prefix;
  const prefixMatches =
    !prefix ||
    identifier.toUpperCase().startsWith(`${prefix.toUpperCase()}-`);

  const { data } = useQuery({
    ...taskIdentifierOptions(wsId, identifier),
    enabled: Boolean(wsId) && isTaskIdentifier(identifier) && prefixMatches,
  });

  return data ?? null;
}
