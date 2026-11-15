import { propertyIdFromViewKey } from "@silieco/core/tasks/stores/view-store";
import type {
  TableCalculation,
  TableColumnKey,
} from "@silieco/core/tasks/stores/view-store";
import type {
  Task,
  TaskPropertyValue,
} from "@silieco/core/types";

/** Export must fail closed when paged Table responses cannot prove that the
 * complete query window was collected. The UI translates this marker instead
 * of exposing a protocol detail to users. */
export class TaskTableExportIntegrityError extends Error {
  constructor() {
    super("Table export response was incomplete");
    this.name = "TaskTableExportIntegrityError";
  }
}

export type TaskTableDisplayRow =
  | {
      kind: "group";
      key: string;
      label: string;
      count: number;
      collapsed: boolean;
    }
  | {
      kind: "task";
      key: string;
      task: Task;
      depth: number;
      hasChildren: boolean;
      collapsed: boolean;
    }
  // Stands in for a row that has not arrived yet. Cold loads render these in
  // the table's own grid rather than swapping the surface for a generic
  // placeholder: columns, widths and the header are known before any data is,
  // so only the rows need standing in for, and the layout does not shift when
  // they are replaced.
  | { kind: "skeleton"; key: string }
  // Carries the state rather than a finished label: the row renders through
  // ListLoadMoreFooter, the same footer Board / List / Swimlane use, which owns
  // the wording and the styling for each state.
  | {
      kind: "load_more";
      key: string;
      state: "loading" | "has_more" | "error" | "end";
      // Rows past this many mean the branch actually paginated, which is what
      // decides whether reaching the end is worth marking.
      total: number;
      onLoad?: () => void;
    };

export function getTaskTableSelectionRange(
  taskIds: string[],
  anchorId: string | null,
  targetId: string,
): string[] | null {
  if (!anchorId) return null;
  const anchorIndex = taskIds.indexOf(anchorId);
  const targetIndex = taskIds.indexOf(targetId);
  if (anchorIndex === -1 || targetIndex === -1) return null;

  const start = Math.min(anchorIndex, targetIndex);
  const end = Math.max(anchorIndex, targetIndex);
  return taskIds.slice(start, end + 1);
}

/**
 * Refresh the task objects inside a frozen row snapshot. While a cell editor
 * popup is open the table renders a structural snapshot (row order, grouping,
 * nesting stay put so the popup's anchor row cannot be reordered out of the
 * virtualized render window mid-interaction), but the VALUES inside those
 * rows must stay live — a multi-select toggle, for example, commits while the
 * popup is open and its checkmark has to reflect the optimistic cache.
 * Tasks deleted from the live window keep their stale snapshot object; the
 * structure catches up the moment the editor closes.
 */
export function refreshFrozenTableRows(
  snapshot: TaskTableDisplayRow[],
  taskById: ReadonlyMap<string, Task>,
): TaskTableDisplayRow[] {
  return snapshot.map((row) => {
    if (row.kind !== "task") return row;
    const live = taskById.get(row.task.id);
    return live && live !== row.task ? { ...row, task: live } : row;
  });
}
function columnValue(
  task: Task,
  columnKey: TableColumnKey,
): TaskPropertyValue | string | number | null | undefined {
  const propertyId = propertyIdFromViewKey(columnKey);
  if (propertyId) return task.properties[propertyId];
  switch (columnKey) {
    case "identifier":
      return task.identifier;
    case "title":
      return task.title;
    case "status":
      return task.status;
    case "priority":
      return task.priority;
    case "assignee":
      return task.assignee_id;
    case "labels":
      return task.labels?.map((label) => label.name).join(", ");
    case "project":
      return task.project_id;
    case "start_date":
      return task.start_date;
    case "due_date":
      return task.due_date;
    case "created_at":
      return task.created_at;
    case "updated_at":
      return task.updated_at;
    case "child_progress":
      return undefined;
    case "creator":
      return task.creator_id;
  }
  return undefined;
}

export function calculateTaskTableColumn(
  tasks: Task[],
  columnKey: TableColumnKey,
  calculation: TableCalculation,
) {
  if (calculation === "none") return null;
  const values = tasks
    .map((task) => columnValue(task, columnKey))
    .filter((value) => value !== undefined && value !== null && value !== "");
  if (calculation === "count") return values.length;
  const numbers = values.filter((value): value is number => typeof value === "number");
  if (numbers.length === 0) return null;
  const sum = numbers.reduce((total, value) => total + value, 0);
  return calculation === "sum" ? sum : sum / numbers.length;
}

function escapeCsvCell(value: unknown) {
  const raw = value == null ? "" : String(value);
  // Spreadsheet apps execute string cells beginning with these characters as
  // formulas. Prefix text (including headers) with an apostrophe so exported
  // user-controlled titles/properties remain data. Preserve real numbers as
  // numbers — a negative numeric value is not an injected formula string.
  const text =
    typeof value === "string" && /^[=+\-@\t\r]/.test(raw)
      ? `'${raw}`
      : raw;
  return /[",\n\r]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

export function buildTaskTableCsv(
  headers: string[],
  rows: unknown[][],
) {
  return [headers, ...rows]
    .map((row) => row.map(escapeCsvCell).join(","))
    .join("\r\n");
}
