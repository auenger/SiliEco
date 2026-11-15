import { ALL_STATUSES } from "@silieco/core/tasks/config";
import type {
  Task,
  TaskTableGroupDescriptor,
  TaskStatus,
  TaskTableFacetsRequest,
  TaskTableGroupsRequest,
  TaskTableQuerySpec,
  TaskTableRowsRequest,
  ListTasksParams,
  ListTasksResponse,
} from "@silieco/core/types";

type LegacyListTasks = (
  params?: ListTasksParams,
) => Promise<ListTasksResponse>;

function legacyParamsForStatus(
  query: TaskTableQuerySpec,
  status: TaskStatus,
): ListTasksParams {
  const scope = query.scope;
  return {
    status,
    limit: 50,
    offset: 0,
    ...(scope.kind === "project" ? { project_id: scope.project_id } : {}),
    ...(scope.kind === "assignee" && scope.actor
      ? {
          assignee_type: scope.actor.type,
          assignee_id: scope.actor.id,
        }
      : {}),
    ...(scope.kind === "creator" && scope.actor
      ? {
          creator_type: scope.actor.type,
          creator_id: scope.actor.id,
        }
      : {}),
    ...(query.search ? { search: query.search } : {}),
  };
}

async function rowsForStatus(
  listTasks: LegacyListTasks,
  query: TaskTableQuerySpec,
  status: TaskStatus,
) {
  if (
    query.filters.statuses &&
    !query.filters.statuses.includes(status)
  ) {
    return [];
  }
  const response = await listTasks(legacyParamsForStatus(query, status));
  return response.tasks.filter((task) => {
    if (
      query.filters.include_sub_tasks === false &&
      task.parent_task_id !== null
    ) {
      return false;
    }
    return task.status === status;
  });
}

async function allRows(
  listTasks: LegacyListTasks,
  query: TaskTableQuerySpec,
) {
  const rows = await Promise.all(
    ALL_STATUSES.map((status) =>
      rowsForStatus(
        listTasks,
        { ...query, filters: { ...query.filters, statuses: undefined } },
        status,
      ),
    ),
  );
  return rows.flat();
}

function primaryDescriptor(
  task: Task,
  primary: "assignee" | "project" | "parent",
  taskById: ReadonlyMap<string, Task>,
): Omit<TaskTableGroupDescriptor, "count" | "secondary_groups"> {
  if (primary === "assignee") {
    const actor =
      task.assignee_type && task.assignee_id
        ? { type: task.assignee_type, id: task.assignee_id }
        : null;
    return {
      key: actor
        ? `assignee:${actor.type}:${actor.id}`
        : "assignee:unassigned",
      value: { kind: "assignee", actor },
    };
  }
  if (primary === "project") {
    return {
      key: task.project_id ? `project:${task.project_id}` : "project:none",
      value: { kind: "project", project_id: task.project_id },
    };
  }
  const parent = task.parent_task_id
    ? taskById.get(task.parent_task_id) ?? null
    : null;
  return {
    key: task.parent_task_id
      ? `parent:${task.parent_task_id}`
      : "parent:none",
    value: {
      kind: "parent",
      parent_id: task.parent_task_id,
      parent: parent
        ? {
            id: parent.id,
            number: parent.number,
            identifier: parent.identifier,
            title: parent.title,
            status: parent.status,
          }
        : null,
      value_state: task.parent_task_id
        ? parent
          ? "value"
          : "unavailable"
        : "unset",
    },
  };
}

/**
 * Transitional adapter for pre-Table test fixtures. Production code never
 * imports this module; it lets existing surface tests keep their small
 * per-status in-memory data source while asserting the new request contract.
 */
export function statusTableMethodsFromLegacy(listTasks: LegacyListTasks) {
  return {
    listTaskTableGroups: async (request: TaskTableGroupsRequest) => {
      if (request.group.kind === "compound") {
        const tasks = await allRows(listTasks, request.query);
        const taskById = new Map(tasks.map((task) => [task.id, task]));
        const grouped = new Map<
          string,
          {
            descriptor: ReturnType<typeof primaryDescriptor>;
            tasks: Task[];
          }
        >();
        for (const task of tasks) {
          const descriptor = primaryDescriptor(
            task,
            request.group.primary,
            taskById,
          );
          const current = grouped.get(descriptor.key) ?? {
            descriptor,
            tasks: [],
          };
          current.tasks.push(task);
          grouped.set(descriptor.key, current);
        }
        return {
          query_fingerprint: "test",
          total: tasks.length,
          groups: Array.from(grouped.values(), ({ descriptor, tasks }) => ({
            ...descriptor,
            count: tasks.length,
            secondary_groups: ALL_STATUSES.flatMap((status) => {
              const count = tasks.filter((task) => task.status === status).length;
              return count
                ? [{
                    key: `compound:${descriptor.key}:status:${status}`,
                    value: { kind: "status" as const, status },
                    count,
                  }]
                : [];
            }),
          })),
          next_cursor: null,
        };
      }
      const groups = await Promise.all(
        ALL_STATUSES.map(async (status) => ({
          status,
          tasks: await rowsForStatus(listTasks, request.query, status),
        })),
      );
      const nonEmpty = groups.filter(({ tasks }) => tasks.length > 0);
      return {
        query_fingerprint: "test",
        total: nonEmpty.reduce((sum, group) => sum + group.tasks.length, 0),
        groups: nonEmpty.map(({ status, tasks }) => ({
          key: `status:${status}`,
          value: { kind: "status" as const, status },
          count: tasks.length,
        })),
        next_cursor: null,
      };
    },
    listTaskTableRows: async (request: TaskTableRowsRequest) => {
      if (request.group.kind === "compound") {
        const primary = request.group.primary;
        const marker = request.group_key?.lastIndexOf(":status:") ?? -1;
        const status =
          marker >= 0
            ? request.group_key?.slice(marker + ":status:".length)
            : undefined;
        const primaryKey =
          marker >= 0
            ? request.group_key?.slice("compound:".length, marker)
            : undefined;
        const all = await allRows(listTasks, request.query);
        const taskById = new Map(all.map((task) => [task.id, task]));
        const tasks = all.filter((task) => {
          const descriptor = primaryDescriptor(
            task,
            primary,
            taskById,
          );
          return descriptor.key === primaryKey && task.status === status;
        });
        return {
          query_fingerprint: "test",
          group_key: request.group_key,
          parent_id: request.parent_id,
          total: 0,
          rows: tasks.map((task) => ({
            task,
            direct_child_count: 0,
          })),
          branch_total: tasks.length,
          next_cursor: null,
        };
      }
      const rawStatus = request.group_key?.replace(/^status:/, "");
      const status = ALL_STATUSES.find((value) => value === rawStatus);
      const tasks = status
        ? await rowsForStatus(listTasks, request.query, status)
        : [];
      return {
        query_fingerprint: "test",
        group_key: request.group_key,
        parent_id: request.parent_id,
        total: 0,
        rows: tasks.map((task) => ({
          task,
          direct_child_count: 0,
        })),
        branch_total: tasks.length,
        next_cursor: null,
      };
    },
    listTaskTableFacets: async (request: TaskTableFacetsRequest) => {
      const groups = await Promise.all(
        ALL_STATUSES.map(async (status) => ({
          status,
          tasks: await rowsForStatus(
            listTasks,
            {
              ...request.query,
              filters: {
                ...request.query.filters,
                statuses: undefined,
              },
            },
            status,
          ),
        })),
      );
      return {
        query_fingerprint: "test",
        total: groups.reduce((sum, group) => sum + group.tasks.length, 0),
        facets: request.facets.map((facet) => ({
          ...facet,
          values:
            facet.kind === "status"
              ? groups
                  .filter(({ tasks }) => tasks.length > 0)
                  .map(({ status, tasks }) => ({
                    key: status,
                    count: tasks.length,
                  }))
              : [],
        })),
      };
    },
  };
}
