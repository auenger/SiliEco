"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  useQueries,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { ALL_STATUSES } from "@silieco/core/tasks/config";
import {
  taskKeys,
  taskTableRowPageOptions,
} from "@silieco/core/tasks/queries";
import type {
  Task,
  TaskStatus,
  TaskTableFacetsResponse,
  TaskTableQuerySpec,
  TaskTableRowsResponse,
} from "@silieco/core/types";

export interface TaskStatusPageState {
  total: number;
  loaded: number;
  hasMore: boolean;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  loadMore: () => void;
  retry: () => void;
}

export type TaskStatusPagination = Record<
  TaskStatus,
  TaskStatusPageState
>;

interface StatusCursorState {
  identity: string;
  cursors: Record<TaskStatus, Array<string | null>>;
}

interface StatusPageTarget {
  status: TaskStatus;
  cursor: string | null;
}

interface StatusBranchData {
  rows: Task[];
  nextCursor: string | null;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  headUpdatedAt: number;
  headFetching: boolean;
  /** The head (cursor === null) page alone is still loading with nothing to
   * show. Distinct from `isLoading`, which also accumulates tail pages so the
   * per-branch load-more sentinel can spin. */
  headPending: boolean;
}

function statusGroupKey(status: TaskStatus) {
  return `status:${status}`;
}

function initialCursorState(
  identity: string,
  statuses: readonly TaskStatus[],
): StatusCursorState {
  const cursors = Object.fromEntries(
    ALL_STATUSES.map((status) => [
      status,
      statuses.includes(status) ? [null] : [],
    ]),
  ) as StatusCursorState["cursors"];
  return { identity, cursors };
}

function rebaseCursorState(
  state: StatusCursorState,
  identity: string,
  statuses: readonly TaskStatus[],
) {
  const current =
    state.identity === identity ? state : initialCursorState(identity, statuses);
  let cursors: StatusCursorState["cursors"] | null = null;
  for (const status of statuses) {
    if (current.cursors[status].length > 0) continue;
    cursors ??= { ...current.cursors };
    cursors[status] = [null];
  }
  return cursors ? { ...current, cursors } : current;
}

function statusCountsFromFacets(
  facets: TaskTableFacetsResponse | undefined,
) {
  const counts = new Map<TaskStatus, number>();
  const statusFacet = facets?.facets.find((facet) => facet.kind === "status");
  for (const value of statusFacet?.values ?? []) {
    if (ALL_STATUSES.includes(value.key as TaskStatus)) {
      counts.set(value.key as TaskStatus, value.count);
    }
  }
  return counts;
}

export interface TaskStatusBranches {
  enabled: boolean;
  tasks: Task[];
  pagination: TaskStatusPagination;
  total: number;
  isTotalKnown: boolean;
  isLoading: boolean;
  isRefreshing: boolean;
}

/**
 * Server-authoritative status branches shared by List and status-grouped
 * Board. Every branch page is the same keyset-paged `/table/rows` contract
 * used by Table; status totals come from the disjunctive status facet, so
 * hidden/empty columns never depend on the loaded card window.
 */
export function useTaskStatusBranches({
  wsId,
  query,
  statuses,
  facets,
  facetsPending,
  facetsFetching,
  enabled,
}: {
  wsId: string;
  query: TaskTableQuerySpec;
  statuses: readonly TaskStatus[];
  facets: TaskTableFacetsResponse | undefined;
  facetsPending: boolean;
  facetsFetching: boolean;
  enabled: boolean;
}): TaskStatusBranches {
  const queryClient = useQueryClient();
  const identity = useMemo(() => JSON.stringify(query), [query]);
  const [cursorState, setCursorState] = useState<StatusCursorState>(() =>
    initialCursorState(identity, statuses),
  );
  const activeCursorState = rebaseCursorState(
    cursorState,
    identity,
    statuses,
  );

  useEffect(() => {
    if (cursorState !== activeCursorState) {
      setCursorState(activeCursorState);
    }
  }, [activeCursorState, cursorState]);

  const pageTargets = useMemo<StatusPageTarget[]>(
    () =>
      enabled
        ? statuses.flatMap((status) =>
            activeCursorState.cursors[status].map((cursor) => ({
              status,
              cursor,
            })),
          )
        : [],
    [activeCursorState.cursors, enabled, statuses],
  );
  const headPlaceholderRef = useRef(
    new Map<TaskStatus, TaskTableRowsResponse>(),
  );
  const pageQueries = useMemo(
    () =>
      pageTargets.map(({ status, cursor }) => {
        const placeholder =
          cursor === null ? headPlaceholderRef.current.get(status) : undefined;
        return {
          ...taskTableRowPageOptions(wsId, {
            query,
            group: { kind: "status" },
            group_key: statusGroupKey(status),
            hierarchy: { enabled: false },
            parent_id: null,
            page: { limit: 50, cursor },
          }),
          // useQueries replaces observers when the query hash changes, so its
          // built-in keepPreviousData cannot bridge a filter/sort transition.
          // Retain only the last settled HEAD per fixed status branch. Tails
          // are deliberately detached; exact facets remain server-owned.
          ...(placeholder ? { placeholderData: () => placeholder } : {}),
          enabled,
        };
      }),
    [enabled, pageTargets, query, wsId],
  );
  const pageResults = useQueries({ queries: pageQueries }) as Array<
    UseQueryResult<TaskTableRowsResponse, Error>
  >;
  useEffect(() => {
    const next = new Map(headPlaceholderRef.current);
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const result = pageResults[index];
      if (
        target?.cursor !== null ||
        !result?.data ||
        result.isPlaceholderData ||
        result.isError
      ) {
        continue;
      }
      next.set(target.status, result.data);
    }
    headPlaceholderRef.current = next;
  }, [pageResults, pageTargets]);

  const branchData = useMemo(() => {
    const result = new Map<TaskStatus, StatusBranchData>();
    for (const status of statuses) {
      result.set(status, {
        rows: [],
        nextCursor: null,
        isLoading: false,
        isFetching: false,
        isError: false,
        headUpdatedAt: 0,
        headFetching: false,
        headPending: false,
      });
    }

    const headFetching = new Set<TaskStatus>();
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const queryResult = pageResults[index];
      if (
        target?.cursor === null &&
        queryResult?.isFetching &&
        activeCursorState.cursors[target.status].length > 1
      ) {
        headFetching.add(target.status);
      }
    }

    const seenByStatus = new Map<TaskStatus, Set<string>>();
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const queryResult = pageResults[index];
      if (!target || !queryResult) continue;
      // A broad invalidation makes every cursor stale as soon as the head
      // starts refreshing. Hide detached tails immediately; the effect below
      // trims their cursor observers before their responses can re-enter.
      if (target.cursor !== null && headFetching.has(target.status)) continue;

      const current = result.get(target.status);
      if (!current) continue;
      const page = queryResult.data;
      if (page) {
        const seen = seenByStatus.get(target.status) ?? new Set<string>();
        for (const row of page.rows) {
          // Realtime can patch an task's status before the broad query
          // invalidation has moved it between branch caches. Never render a
          // patched card under a status it no longer belongs to.
          if (row.task.status !== target.status) continue;
          if (seen.has(row.task.id)) continue;
          seen.add(row.task.id);
          current.rows.push(row.task);
        }
        seenByStatus.set(target.status, seen);
        current.nextCursor = page.next_cursor;
      }
      current.isLoading ||= queryResult.isPending;
      current.isFetching ||= queryResult.isFetching;
      current.isError ||= queryResult.isError;
      if (target.cursor === null) {
        current.headUpdatedAt = queryResult.dataUpdatedAt;
        current.headFetching = queryResult.isFetching;
        current.headPending = queryResult.isPending;
      }
    }
    return result;
  }, [
    activeCursorState.cursors,
    pageResults,
    pageTargets,
    statuses,
  ]);

  // Once a head page refreshes, its old cursor chain no longer belongs to the
  // current snapshot. Match Table's branch behavior by dropping every tail.
  const headRevisionRef = useRef<{
    identity: string;
    revisions: Partial<Record<TaskStatus, number>>;
  }>({ identity, revisions: {} });
  useEffect(() => {
    const previous =
      headRevisionRef.current.identity === identity
        ? headRevisionRef.current.revisions
        : {};
    const next: Partial<Record<TaskStatus, number>> = {};
    const trim = new Set<TaskStatus>();
    for (const status of statuses) {
      const branch = branchData.get(status);
      if (!branch || branch.headUpdatedAt === 0) continue;
      next[status] = branch.headUpdatedAt;
      const seen = previous[status];
      if (
        activeCursorState.cursors[status].length > 1 &&
        (branch.headFetching ||
          (seen !== undefined && seen !== branch.headUpdatedAt))
      ) {
        trim.add(status);
      }
    }
    headRevisionRef.current = { identity, revisions: next };
    if (trim.size === 0) return;
    setCursorState((previousState) => {
      if (previousState.identity !== identity) return previousState;
      const cursors = { ...previousState.cursors };
      for (const status of trim) cursors[status] = [null];
      return { ...previousState, cursors };
    });
  }, [
    activeCursorState.cursors,
    branchData,
    identity,
    statuses,
  ]);

  const counts = useMemo(() => statusCountsFromFacets(facets), [facets]);
  const loadMore = useCallback(
    (status: TaskStatus) => {
      const cursor = branchData.get(status)?.nextCursor;
      if (!cursor) return;
      setCursorState((previous) => {
        if (previous.identity !== identity) return previous;
        const current = previous.cursors[status];
        if (current.includes(cursor)) return previous;
        return {
          ...previous,
          cursors: {
            ...previous.cursors,
            [status]: [...current, cursor],
          },
        };
      });
    },
    [branchData, identity],
  );
  const retry = useCallback(
    (status: TaskStatus) => {
      void queryClient.refetchQueries({
        queryKey: taskKeys.tableRows(
          wsId,
          query,
          { kind: "status" },
          statusGroupKey(status),
          false,
          null,
        ),
        exact: false,
        type: "active",
      });
    },
    [query, queryClient, wsId],
  );

  const pagination = useMemo<TaskStatusPagination>(() => {
    return Object.fromEntries(
      ALL_STATUSES.map((status) => {
        const branch = branchData.get(status);
        const loaded = branch?.rows.length ?? 0;
        const total = counts.get(status) ?? loaded;
        return [
          status,
          {
            total,
            loaded,
            hasMore: enabled && !!branch?.nextCursor,
            isLoading: enabled && (branch?.isLoading ?? false),
            isFetching: enabled && (branch?.isFetching ?? false),
            isError: enabled && (branch?.isError ?? false),
            loadMore: () => loadMore(status),
            retry: () => retry(status),
          },
        ];
      }),
    ) as TaskStatusPagination;
  }, [branchData, counts, enabled, loadMore, retry]);

  const tasks = useMemo(
    () => statuses.flatMap((status) => branchData.get(status)?.rows ?? []),
    [branchData, statuses],
  );
  const isTotalKnown = facets !== undefined;
  const total = facets?.total ?? tasks.length;
  // Surface-level loading reflects only each branch's HEAD page. A tail page
  // (load-more) is pending/fetching while the already-rendered rows stay on
  // screen, so it must never re-trigger the full-surface skeleton or the
  // global refreshing indicator — the per-branch sentinel owns "loading more".
  const headsPending = statuses.some(
    (status) => branchData.get(status)?.headPending,
  );
  const headsFetching = statuses.some(
    (status) => branchData.get(status)?.headFetching,
  );

  return {
    enabled,
    tasks,
    pagination,
    total,
    isTotalKnown,
    isLoading: enabled && (facetsPending || headsPending),
    isRefreshing:
      enabled &&
      !facetsPending &&
      !headsPending &&
      (facetsFetching || headsFetching),
  };
}
