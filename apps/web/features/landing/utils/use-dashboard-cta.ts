"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@silieco/core/auth";
import { workspaceListOptions } from "@silieco/core/workspace";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@silieco/core/paths";
import type { Workspace } from "@silieco/core/types";

/**
 * While the workspace list is in flight the CTA points at `/tasks`, which the
 * proxy rewrites to the last workspace. That keeps the button working during
 * hydration without hiding or disabling it (which would flicker). A visitor
 * with no workspace history lands back on the landing page in that window and
 * can click again once the list resolves.
 */
const LOADING_FALLBACK_HREF = "/tasks";

export function resolveDashboardCtaHref({
  isAuthenticated,
  isWorkspaceListFetched,
  workspaces,
  hasOnboarded,
}: {
  isAuthenticated: boolean;
  isWorkspaceListFetched: boolean;
  workspaces: Workspace[] | undefined;
  hasOnboarded: boolean;
}): string {
  if (!isAuthenticated) return paths.login();
  if (!isWorkspaceListFetched || !workspaces) return LOADING_FALLBACK_HREF;
  return resolvePostAuthDestination(workspaces, hasOnboarded);
}

/**
 * Destination for the landing "Dashboard" CTA.
 *
 * These CTAs used to point at `/` and lean on the proxy bouncing logged-in
 * visitors from the root path to their workspace. Once `/` stayed public on the
 * official marketing host, that bounce stopped and the CTA resolved to the page
 * the visitor was already on — a click with no visible effect. Resolve the real
 * destination here instead, through the same resolver
 * `RedirectIfAuthenticated` uses, so "where the dashboard lives" has one source
 * of truth.
 *
 * Shares `workspaceListOptions()`' query key with `RedirectIfAuthenticated`, so
 * on the landing page the list is typically already in flight or cached and this
 * adds no request.
 */
export function useDashboardCtaHref(): string {
  const user = useAuthStore((s) => s.user);
  const hasOnboarded = useHasOnboarded();

  const { data, isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  return resolveDashboardCtaHref({
    isAuthenticated: !!user,
    isWorkspaceListFetched: isFetched,
    workspaces: data,
    hasOnboarded,
  });
}
