"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useDndContext,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  horizontalListSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import {
  getCoreRowModel,
  useReactTable,
  type CellContext,
  type ColumnDef,
  type ColumnSizingState,
  type HeaderContext,
  type OnChangeFn,
  type Table as TanstackTable,
  type TableMeta,
} from "@tanstack/react-table";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  Download,
  EyeOff,
  GripVertical,
  Loader2,
  Pencil,
  Plus,
  Search,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { DataTable } from "@silieco/ui/components/ui/data-table";
import { Button } from "@silieco/ui/components/ui/button";
import { Input } from "@silieco/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@silieco/ui/components/ui/dropdown-menu";
import {
  TableCell,
  TableRow,
} from "@silieco/ui/components/ui/table";
import { Skeleton } from "@silieco/ui/components/ui/skeleton";
import { cn } from "@silieco/ui/lib/utils";
import { ApiError } from "@silieco/core/api";
import { useWorkspaceId } from "@silieco/core/hooks";
import { ALL_STATUSES } from "@silieco/core/tasks/config";
import {
  taskKeys,
  taskTableGroupsOptions,
  taskTableRowPageOptions,
} from "@silieco/core/tasks/queries";
import {
  TABLE_SYSTEM_COLUMNS,
  propertyIdFromViewKey,
  type SortField,
  type TableColumnKey,
  type TableSystemColumnKey,
} from "@silieco/core/tasks/stores/view-store";
import { useViewStore } from "@silieco/core/tasks/stores/view-store-context";
import { propertyListOptions } from "@silieco/core/properties";
import { useWorkspacePaths } from "@silieco/core/paths";
import { buildActorNameResolver, useActorName } from "@silieco/core/workspace/hooks";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@silieco/core/workspace/queries";
import type {
  Task,
  TaskProperty,
  TaskPropertyValue,
  TaskStatus,
  TaskTableGroupDescriptor,
  TaskTableGroupSpec,
  TaskTableQuerySpec,
  TaskTableRowsResponse,
  Project,
  UpdateTaskRequest,
} from "@silieco/core/types";
import {
  useInfiniteQuery,
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { ActorAvatar } from "../../common/actor-avatar";
import { LabelChip } from "../../labels/label-chip";
import { useNavigation } from "../../navigation";
import { ProjectPicker } from "../../projects/components/project-picker";
import { useT } from "../../i18n";
import { useTaskSurfaceActionsOptional } from "../surface/actions-context";
import { useTaskSurfaceSelection } from "../surface/selection-context";
import type { TaskCreateDefaults } from "../surface/types";
import { ProgressRing } from "./progress-ring";
import {
  AssigneePicker,
  DueDatePicker,
  LabelPicker,
  PriorityPicker,
  StartDatePicker,
  StatusPicker,
} from "./pickers";
import { CustomPropertyValueEditor } from "./pickers/custom-property-picker";
import {
  buildTaskTableCsv,
  getTaskTableSelectionRange,
  TaskTableExportIntegrityError,
  refreshFrozenTableRows,
  type TaskTableDisplayRow,
} from "./table-view-model";
import type { ChildProgress } from "./list-row";
import { ListLoadMoreFooter } from "./list-load-more-footer";
import { TaskAgentActivityIndicator } from "./task-agent-activity-indicator";

// Enough placeholder rows to cover a typical viewport; the virtualizer only
// mounts what fits, so overshooting costs nothing.
const SKELETON_ROW_COUNT = 12;

const SELECT_COLUMN_ID = "__select";
const ADD_COLUMN_ID = "__add";

type TableViewProps = {
  serverQuery: TaskTableQuerySpec;
  childProgressMap: Map<string, ChildProgress>;
  search: string;
  onSearchChange: (query: string) => void;
  onLoadedTasksChange: (tasks: Task[]) => void;
  onCreateTask: (defaults: TaskCreateDefaults) => void;
  exportTasks: () => Promise<Task[]>;
  resolveExportLookups: (needs: {
    projects: boolean;
    childProgress: boolean;
  }) => Promise<{
    projectMap: Map<string, Project>;
    childProgressMap: Map<string, ChildProgress>;
  }>;
};

type ServerBranch = {
  key: string;
  groupKey: string | null;
  parentId: string | null;
  ancestorIds: string[];
  cursors: Array<string | null>;
};

type ServerBranchState = {
  identity: string;
  structureIdentity: string;
  branches: Map<string, ServerBranch>;
};

type ServerBranchPageTarget = {
  branch: ServerBranch;
  cursor: string | null;
};

type ServerBranchData = {
  rows: TaskTableRowsResponse["rows"];
  total: number;
  nextCursor: string | null;
  headUpdatedAt: number;
  headFetching: boolean;
  loading: boolean;
  error: boolean;
  placeholder: boolean;
};

type LoadedTaskState = {
  membershipIdentity: string;
  tasks: Map<string, Task>;
};

function serverBranchKey(groupKey: string | null, parentId: string | null) {
  return `${groupKey ?? "ungrouped"}::${parentId ?? "root"}`;
}

function sameStringArray(left: string[], right: string[]) {
  return (
    left.length === right.length &&
    left.every((value, index) => value === right[index])
  );
}

/** Rebase the branch graph synchronously when a query changes.
 *
 * Filters/search/sort keep the same group/hierarchy structure, so preserving
 * branch identities while resetting every cursor to the head lets React Query
 * show the previous rows during the new request. A real structure change
 * (group kind or hierarchy) discards incompatible group/parent identities. */
function rebaseServerBranchState(
  previous: ServerBranchState,
  identity: string,
  structureIdentity: string,
  usesServerGrouping: boolean,
): ServerBranchState {
  if (previous.identity === identity) return previous;

  const branches =
    previous.structureIdentity === structureIdentity
      ? new Map(
          [...previous.branches].map(([key, branch]) => [
            key,
            { ...branch, cursors: [null] },
          ]),
        )
      : new Map<string, ServerBranch>();

  if (!usesServerGrouping) {
    const key = serverBranchKey(null, null);
    if (!branches.has(key)) {
      branches.set(key, {
        key,
        groupKey: null,
        parentId: null,
        ancestorIds: [],
        cursors: [null],
      });
    }
  }

  return { identity, structureIdentity, branches };
}

function tableGroupSpec(grouping: string): TaskTableGroupSpec {
  if (grouping === "status") return { kind: "status" };
  if (grouping === "assignee") return { kind: "assignee" };
  const propertyId = propertyIdFromViewKey(grouping);
  if (propertyId) return { kind: "property", property_id: propertyId };
  return { kind: "none" };
}

type ColumnLabelKey =
  | "title"
  | "identifier"
  | "status"
  | "priority"
  | "assignee"
  | "labels"
  | "project"
  | "start_date"
  | "due_date"
  | "created_at"
  | "updated_at"
  | "child_progress"
  | "creator";

const SORTABLE_COLUMNS: Partial<Record<TableSystemColumnKey, SortField>> = {
  title: "title",
  status: "status",
  priority: "priority",
  start_date: "start_date",
  due_date: "due_date",
  created_at: "created_at",
  updated_at: "updated_at",
};

function stopRowNavigation(event: React.SyntheticEvent) {
  event.stopPropagation();
}

function SelectAllCheckbox({
  taskIds,
  label,
}: {
  taskIds: string[];
  label: string;
}) {
  const selection = useTaskSurfaceSelection();
  const ref = useRef<HTMLInputElement>(null);
  const selectedCount = taskIds.filter((id) => selection.selectedIds.has(id)).length;
  const checked = taskIds.length > 0 && selectedCount === taskIds.length;

  useEffect(() => {
    if (ref.current) {
      ref.current.indeterminate = selectedCount > 0 && !checked;
    }
  }, [checked, selectedCount]);

  return (
    <input
      ref={ref}
      type="checkbox"
      aria-label={label}
      checked={checked}
      onChange={() =>
        checked ? selection.deselect(taskIds) : selection.select(taskIds)
      }
      className="size-3.5 cursor-pointer accent-primary"
    />
  );
}

function TaskCheckbox({
  checked,
  label,
  onToggle,
}: {
  checked: boolean;
  label: string;
  onToggle: (shiftKey: boolean) => void;
}) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      checked={checked}
      onClick={(event) => {
        event.stopPropagation();
        onToggle(event.shiftKey);
      }}
      onChange={() => undefined}
      className="size-3.5 cursor-pointer accent-primary"
    />
  );
}

function SortableColumnHeader({
  columnKey,
  label,
  sortField,
  sortBy,
  sortDirection,
  onSort,
  onHide,
  ascendingLabel,
  descendingLabel,
  hideLabel,
  reorderLabel,
}: {
  columnKey: TableColumnKey;
  label: string;
  sortField?: SortField;
  sortBy: SortField;
  sortDirection: "asc" | "desc";
  onSort: (field: SortField, direction: "asc" | "desc") => void;
  onHide?: () => void;
  ascendingLabel: string;
  descendingLabel: string;
  hideLabel: string;
  reorderLabel: string;
}) {
  const sortable = columnKey !== "title";
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: columnKey, disabled: !sortable });
  const active = sortField === sortBy;
  // Any column in flight, not only this one: the neighbours shift to open a
  // gap, and each is clipped by its own cell just the same.
  const isReordering = useDndContext().active != null;
  const nodeRef = useRef<HTMLDivElement | null>(null);

  // The cell clips its own content, which is what made a dragged column look
  // like it vanished rather than travelled. The clip only earns its keep at
  // rest, capping a label wider than its column, so it is lifted for the length
  // of a reorder and the column in hand is raised over its neighbours.
  //
  // Only overflow and stacking are touched. Transforming the <th> itself would
  // carry the header's full height along, but `transform` on a table cell is a
  // corner of the spec browsers take liberties with — Chromium lifts the cell
  // out of the table's box model and its geometry stops matching the row. The
  // wrapper below is padded out to the cell's size instead.
  useLayoutEffect(() => {
    const cell = nodeRef.current?.closest("th");
    if (!cell || !isReordering) return;
    cell.style.overflow = "visible";
    if (isDragging) cell.style.zIndex = "20";
    return () => {
      cell.style.removeProperty("overflow");
      cell.style.removeProperty("z-index");
    };
  }, [isDragging, isReordering]);

  return (
    <div
      ref={(node) => {
        nodeRef.current = node;
        setNodeRef(node);
      }}
      // Horizontal travel only. dnd-kit's layout animation also hands back
      // scaleX/scaleY — old rect over new rect — to tween an item into the
      // shape of the slot it landed in. Between two tabs of equal width that
      // ratio is 1 and never shows; between two columns it is not, so a 174px
      // column swapping with a 96px one gets stretched to 1.8x on the way.
      // Reordering columns changes no column's width, so there is nothing for
      // a shape tween to say here. The move and the settle stay animated
      // through `transition`.
      style={{
        transform: transform ? `translate3d(${transform.x}px, 0, 0)` : undefined,
        transition,
      }}
      // The wrapper spans the cell's own box — the negative margins undo the
      // <th>'s padding and put it back inside — so it renders exactly as at
      // rest while being what travels: a header-sized block rather than the
      // line of text in it. Height is derived rather than fixed at h-8: the
      // strip is taller than the cell's nominal height once row borders are in.
      className={cn(
        "group/header -mx-4 -my-2 flex h-[calc(100%+1rem)] min-w-0 items-center px-4",
        isDragging && "opacity-60",
      )}
    >
      {sortable && (
        <button
          type="button"
          aria-label={reorderLabel}
          className={cn(
            "-ml-2 mr-0.5 rounded p-0.5 text-muted-foreground/50 opacity-0 hover:bg-accent hover:text-muted-foreground group-hover/header:opacity-100 focus-visible:opacity-100",
            isDragging ? "cursor-grabbing opacity-100" : "cursor-grab",
          )}
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-3" />
        </button>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger className="flex min-w-0 items-center gap-1 rounded px-1.5 py-1 hover:bg-accent">
          <span className="truncate">{label}</span>
          {active &&
            (sortDirection === "asc" ? (
              <ArrowUp className="size-3 shrink-0" />
            ) : (
              <ArrowDown className="size-3 shrink-0" />
            ))}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-40">
          {sortField && (
            <>
              <DropdownMenuItem onClick={() => onSort(sortField, "asc")}>
                <ArrowUp />
                {ascendingLabel}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onSort(sortField, "desc")}>
                <ArrowDown />
                {descendingLabel}
              </DropdownMenuItem>
            </>
          )}
          {sortField && onHide && <DropdownMenuSeparator />}
          {onHide && (
            <DropdownMenuItem onClick={onHide}>
              <EyeOff />
              {hideLabel}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

export function TableColumnPicker({
  properties,
  trigger,
}: {
  properties: TaskProperty[];
  trigger: React.ReactElement;
}) {
  const { t } = useT("tasks");
  const [search, setSearch] = useState("");
  const tableColumns = useViewStore((state) => state.tableColumns);
  const toggleTableColumn = useViewStore((state) => state.toggleTableColumn);
  const selected = useMemo(
    () => new Set(tableColumns.map((column) => column.key)),
    [tableColumns],
  );
  const query = search.trim().toLocaleLowerCase();
  const systemColumns = TABLE_SYSTEM_COLUMNS.filter((key) =>
    t(($) => $.table.columns[key as ColumnLabelKey])
      .toLocaleLowerCase()
      .includes(query),
  );
  const visibleProperties = properties.filter((property) =>
    property.name.toLocaleLowerCase().includes(query),
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align="end" className="w-64 p-0">
        <div className="border-b p-2">
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== "Escape") event.stopPropagation();
            }}
            placeholder={t(($) => $.table.columns.search_placeholder)}
            className="h-7"
          />
        </div>
        <div className="max-h-80 overflow-y-auto p-1">
          {systemColumns.length > 0 && (
            <DropdownMenuGroup>
              <DropdownMenuLabel>
                {t(($) => $.table.columns.system_section)}
              </DropdownMenuLabel>
              {systemColumns.map((key) => (
                <DropdownMenuItem
                  key={key}
                  disabled={key === "title"}
                  onClick={(event) => {
                    event.preventDefault();
                    toggleTableColumn(key);
                  }}
                >
                  <input
                    type="checkbox"
                    checked={selected.has(key)}
                    readOnly
                    className="size-3.5 accent-primary"
                  />
                  {t(($) => $.table.columns[key as ColumnLabelKey])}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          )}
          {visibleProperties.length > 0 && (
            <>
              {systemColumns.length > 0 && <DropdownMenuSeparator />}
              <DropdownMenuGroup>
                <DropdownMenuLabel>
                  {t(($) => $.table.columns.property_section)}
                </DropdownMenuLabel>
                {visibleProperties.map((property) => {
                  const key = `property:${property.id}` as const;
                  return (
                    <DropdownMenuItem
                      key={property.id}
                      onClick={(event) => {
                        event.preventDefault();
                        toggleTableColumn(key);
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={selected.has(key)}
                        readOnly
                        className="size-3.5 accent-primary"
                      />
                      <span className="truncate">{property.name}</span>
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
            </>
          )}
          {systemColumns.length === 0 && visibleProperties.length === 0 && (
            <p className="px-2 py-6 text-center text-caption text-muted-foreground">
              {t(($) => $.table.columns.no_results)}
            </p>
          )}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function TableTaskSearch({
  value,
  onChange,
  placeholder,
  clearLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  clearLabel: string;
}) {
  return (
    <div className="relative w-56 shrink-0">
      <Search
        aria-hidden
        className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
      />
      <Input
        type="text"
        role="searchbox"
        inputMode="search"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-label={placeholder}
        placeholder={placeholder}
        className="h-7 pl-7 pr-7 text-caption"
      />
      {value && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={clearLabel}
          onClick={() => onChange("")}
          className="absolute right-0.5 top-0.5 text-muted-foreground"
        >
          <X className="size-3" />
        </Button>
      )}
    </div>
  );
}

export function InlineTitle({
  row,
  editing,
  onEditingChange,
  onUpdate,
  onOpen,
  onCreateSubTask,
  onToggleParent,
  toggleLabel,
  renameLabel,
  createSubTaskLabel,
}: {
  row: Extract<TaskTableDisplayRow, { kind: "task" }>;
  /** Rename state is owned by the table (one editor at a time) so it also
   *  survives cell remounts and drives the structure freeze. */
  editing: boolean;
  onEditingChange: (editing: boolean) => void;
  onUpdate: (updates: Partial<UpdateTaskRequest>) => void;
  /** Navigate to the task — clicking the title is the primary way IN. */
  onOpen: () => void;
  onCreateSubTask: () => void;
  onToggleParent: () => void;
  toggleLabel: string;
  renameLabel: string;
  createSubTaskLabel: string;
}) {
  const [draft, setDraft] = useState(row.task.title);
  const editingRef = useRef(editing);
  editingRef.current = editing;
  // True between the mousedown and the click of ONE gesture when that gesture
  // began while the rename input was up. onBlur commits and flips `editing`
  // off synchronously, before the click that caused the blur lands — so a
  // guard keyed only on the current `editing` value is already gone by click
  // time, and the commit-click bubbles into row navigation (and could hit the
  // title's own open handler): clicking away to save a rename would also open
  // the task (SILI-5108 review R1#2).
  const gestureStartedWhileEditingRef = useRef(false);

  useEffect(() => {
    // Realtime/cache snapshots should refresh the passive label, but must not
    // overwrite text the user is actively composing.
    if (!editingRef.current) setDraft(row.task.title);
  }, [row.task.title]);

  const commit = () => {
    const title = draft.trim();
    onEditingChange(false);
    if (title && title !== row.task.title) onUpdate({ title });
    else setDraft(row.task.title);
  };

  return (
    <div
      className="group/title relative flex min-w-0 items-center gap-1.5"
      style={{ paddingLeft: row.depth * 18 }}
      // Record whether the gesture began while editing (mousedown fires before
      // the blur that commits), then swallow that click in the capture phase —
      // before it can reach the row (navigation) or the title's open handler.
      // A gesture that began while NOT editing passes through untouched, so
      // clicking dead space still opens the task.
      onMouseDownCapture={() => {
        gestureStartedWhileEditingRef.current = editingRef.current;
      }}
      onClickCapture={(event) => {
        if (editing || gestureStartedWhileEditingRef.current) {
          event.stopPropagation();
        }
        gestureStartedWhileEditingRef.current = false;
      }}
    >
      {row.hasChildren ? (
        <button
          type="button"
          aria-label={toggleLabel}
          className="rounded p-0.5 text-muted-foreground hover:bg-accent"
          onClick={(event) => {
            event.stopPropagation();
            onToggleParent();
          }}
        >
          {row.collapsed ? (
            <ChevronRight className="size-3.5" />
          ) : (
            <ChevronDown className="size-3.5" />
          )}
        </button>
      ) : (
        <span className="w-4 shrink-0" />
      )}
      <span className="w-16 shrink-0 text-caption text-muted-foreground">
        {row.task.identifier}
      </span>
      <TaskAgentActivityIndicator taskId={row.task.id} />
      {editing ? (
        <Input
          autoFocus
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") commit();
            if (event.key === "Escape") {
              setDraft(row.task.title);
              onEditingChange(false);
            }
          }}
          className="h-7 min-w-0 flex-1 px-2"
        />
      ) : (
        <>
          <button
            type="button"
            className="min-w-0 flex-1 truncate text-left hover:underline"
            onClick={(event) => {
              event.stopPropagation();
              onOpen();
            }}
          >
            {row.task.title}
          </button>
          {/* Lifted out of the flex flow, the way SidebarMenuAction is. Laid
            * out inline these two reserved ~40px of the title column for
            * buttons that are invisible until hovered — and title is the
            * column with the least room to spare. The gradient fades the text
            * running underneath rather than letting the icons sit on top of
            * it; the sidebar has no need for one because its labels are short,
            * but a title runs to the cell's edge. focus-within keeps them
            * reachable by keyboard, where hover never fires. */}
          {/* The fade has to be whatever the cell is painted with at the
            * moment the actions show, and a hovered row is not the resting
            * background — pinned cells switch to the muted mix on hover, so
            * the gradient follows. */}
          {/* Keyed to the title cell, not the row: these act on the title,
            * and offering them from anywhere along a row puts them under the
            * pointer while it is somewhere else entirely. The fade still
            * follows the row's hover colour, since that is what the cell is
            * painted with when they appear. */}
          <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center gap-0.5 bg-gradient-to-l from-background from-70% to-transparent pr-1 pl-8 opacity-0 transition-opacity group-hover:from-[color-mix(in_oklab,var(--muted)_50%,var(--background))] group-hover/title:pointer-events-auto group-hover/title:opacity-100 focus-within:pointer-events-auto focus-within:opacity-100">
            <button
              type="button"
              aria-label={createSubTaskLabel}
              className="rounded p-1 text-muted-foreground/60 hover:bg-accent hover:text-foreground"
              onClick={(event) => {
                event.stopPropagation();
                onCreateSubTask();
              }}
            >
              <Plus className="size-3" />
            </button>
            <button
              type="button"
              aria-label={renameLabel}
              className="rounded p-1 text-muted-foreground/60 hover:bg-accent hover:text-foreground"
              onClick={(event) => {
                event.stopPropagation();
                setDraft(row.task.title);
                onEditingChange(true);
              }}
            >
              <Pencil className="size-3" />
            </button>
          </span>
        </>
      )}
    </div>
  );
}

function LazyLabelCell({
  task,
  open,
  onOpenChange,
}: {
  task: Task;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("tasks");
  const labels = task.labels ?? [];
  if (open) {
    return (
      <div onClick={stopRowNavigation}>
        <LabelPicker
          taskId={task.id}
          open
          onOpenChange={(next) => {
            if (!next) onOpenChange(false);
          }}
          triggerRender={<button type="button" className="flex max-w-full gap-1" />}
        />
      </div>
    );
  }
  return (
    <button
      type="button"
      className="flex max-w-full items-center gap-1 overflow-hidden rounded px-1 py-0.5 hover:bg-accent"
      onClick={(event) => {
        event.stopPropagation();
        onOpenChange(true);
      }}
    >
      {labels.length > 0 ? (
        <>
          {labels.slice(0, 2).map((label) => (
            <LabelChip key={label.id} label={label} />
          ))}
          {labels.length > 2 && (
            <span className="text-caption text-muted-foreground">+{labels.length - 2}</span>
          )}
        </>
      ) : (
        <span className="text-muted-foreground">{t(($) => $.table.empty_value)}</span>
      )}
    </button>
  );
}

// Extends the <tr> props so the virtualizer's measuring ref and data-index,
// which DataTable clones onto whatever renderRow returns, reach the element
// instead of being absorbed here. A group header is shorter than a data row,
// so it is exactly the kind of row the measurement exists for.
type TaskTableGroupRowProps = React.ComponentProps<"tr"> & {
  group: Extract<TaskTableDisplayRow, { kind: "group" }>;
  colSpan: number;
  onToggle: () => void;
};

export function TaskTableGroupRow({
  group,
  colSpan,
  onToggle,
  ...rowProps
}: TaskTableGroupRowProps) {
  return (
    <TableRow
      {...rowProps}
      className="bg-muted/40 hover:bg-muted/60"
      onClick={onToggle}
    >
      <TableCell colSpan={colSpan} className="h-9 px-4 py-1.5">
        <button
          type="button"
          className="sticky left-4 flex w-fit items-center gap-2 text-caption font-medium"
        >
          {group.collapsed ? (
            <ChevronRight className="size-3.5" />
          ) : (
            <ChevronDown className="size-3.5" />
          )}
          {group.label}
          <span className="font-normal tabular-nums text-muted-foreground">
            {group.count}
          </span>
        </button>
      </TableCell>
    </TableRow>
  );
}

function propertyDisplayValue(
  property: TaskProperty,
  value: TaskPropertyValue | undefined,
) {
  if (value === undefined) return "";
  const options = property.config.options ?? [];
  if (property.type === "select") {
    return options.find((option) => option.id === value)?.name ?? "";
  }
  if (property.type === "multi_select") {
    const ids = Array.isArray(value) ? value : [];
    return options
      .filter((option) => ids.includes(option.id))
      .map((option) => option.name)
      .join(", ");
  }
  return String(value);
}

/**
 * Render-time context for the module-level cell/header components below,
 * carried on `table.options.meta`. The renderers MUST be module-level
 * components with stable identities: TanStack's flexRender mounts a
 * function-typed `cell`/`header` as a React component, so a renderer closure
 * rebuilt when any lookup changed identity (childProgressMap on every
 * realtime refetch, propertyById, actor names…) was a NEW element type and
 * React remounted every cell — closing any open picker popup and dropping
 * in-progress drafts the moment workspace activity refreshed the window
 * (SILI-5108). Data flows through meta instead so the element types never
 * change.
 */
type TableViewMeta = {
  childProgressMap: Map<string, ChildProgress>;
  propertyById: Map<string, TaskProperty>;
  properties: TaskProperty[];
  visibleTaskIds: string[];
  /** `${row.key}:${column.id}` of the cell whose editor popup / rename input
   *  is open, or null. Owned by TableView so the open editor survives cell
   *  remounts and freezes the table structure while it is up. */
  editingCellKey: string | null;
  setEditingCellKey: (key: string | null) => void;
  updateTask: (taskId: string, updates: Partial<UpdateTaskRequest>) => void;
  openTask: (task: Task) => void;
  createSubTask: (task: Task) => void;
  toggleTableParentCollapsed: (taskId: string) => void;
  handleTaskSelection: (taskId: string, shiftKey: boolean) => void;
  getActorName: (actorType: string, actorId: string) => string;
  columnLabel: (key: TableColumnKey) => string;
  sortBy: SortField;
  sortDirection: "asc" | "desc";
  onSort: (field: SortField, direction: "asc" | "desc") => void;
  toggleTableColumn: (key: TableColumnKey) => void;
};

function getTableViewMeta(
  table: TanstackTable<TaskTableDisplayRow>,
): TableViewMeta {
  return table.options.meta as unknown as TableViewMeta;
}

/**
 * Release the hoisted editing key when the cell that owns it unmounts.
 *
 * Row virtualization (see data-table.tsx) unmounts a cell as its row scrolls
 * out of the rendered window. Base UI does NOT call onOpenChange(false) on
 * unmount, so without this the open picker's key — and the frozen row
 * structure keyed off it — would persist after the anchor row leaves the
 * viewport: the table would stay frozen, and scrolling the row back would
 * silently reopen the picker and discard any in-progress rename draft
 * (SILI-5108 review R1#3). Clearing the key iff this unmounting cell still owns
 * it thaws the structure and closes the editor.
 *
 * Live values are read through refs so the empty-dep cleanup always sees the
 * current key/setter. At initial mount a cell is never yet the active editor
 * (the editor is opened by a later interaction, which does not remount the
 * cell), so this never fires spuriously — including under StrictMode's
 * mount → unmount → mount probe, whose first cleanup sees `editingCellKey`
 * still unequal to this cell's key.
 */
export function useReleaseEditingCellOnUnmount(
  cellKey: string | null,
  editingCellKey: string | null,
  setEditingCellKey: (key: string | null) => void,
) {
  const editingCellKeyRef = useRef(editingCellKey);
  editingCellKeyRef.current = editingCellKey;
  const setEditingCellKeyRef = useRef(setEditingCellKey);
  setEditingCellKeyRef.current = setEditingCellKey;
  useEffect(() => {
    return () => {
      if (cellKey !== null && editingCellKeyRef.current === cellKey) {
        setEditingCellKeyRef.current(null);
      }
    };
  }, [cellKey]);
}

function TaskTableSelectHeader({
  table,
}: HeaderContext<TaskTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t } = useT("tasks");
  return (
    <SelectAllCheckbox
      taskIds={meta.visibleTaskIds}
      label={t(($) => $.table.select_all)}
    />
  );
}

function TaskTableSelectCell({
  row,
  table,
}: CellContext<TaskTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const selection = useTaskSurfaceSelection();
  const { t } = useT("tasks");
  if (row.original.kind !== "task") return null;
  const task = row.original.task;
  return (
    <TaskCheckbox
      checked={selection.selectedIds.has(task.id)}
      label={t(($) => $.table.select_task, { identifier: task.identifier })}
      onToggle={(shiftKey) => meta.handleTaskSelection(task.id, shiftKey)}
    />
  );
}

function TaskTableAddColumnHeader({
  table,
}: HeaderContext<TaskTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t } = useT("tasks");
  return (
    <TableColumnPicker
      properties={meta.properties}
      trigger={
        <button
          type="button"
          aria-label={t(($) => $.table.columns.add)}
          className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <Plus className="size-3.5" />
        </button>
      }
    />
  );
}

function TaskTableEmptyCell() {
  return null;
}

function TaskTableHeaderCell({
  column,
  table,
}: HeaderContext<TaskTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t } = useT("tasks");
  const key = column.id as TableColumnKey;
  const propertyId = propertyIdFromViewKey(key);
  const property = propertyId ? meta.propertyById.get(propertyId) : undefined;
  const staticSort = propertyId
    ? property && !["multi_select", "checkbox"].includes(property.type)
      ? (`property:${propertyId}` as SortField)
      : undefined
    : SORTABLE_COLUMNS[key as TableSystemColumnKey];
  const label = meta.columnLabel(key);
  return (
    <SortableColumnHeader
      columnKey={key}
      label={label}
      sortField={staticSort}
      sortBy={meta.sortBy}
      sortDirection={meta.sortDirection}
      onSort={meta.onSort}
      onHide={key === "title" ? undefined : () => meta.toggleTableColumn(key)}
      ascendingLabel={t(($) => $.table.sort_ascending)}
      descendingLabel={t(($) => $.table.sort_descending)}
      hideLabel={t(($) => $.table.columns.hide)}
      reorderLabel={t(($) => $.table.columns.reorder, { column: label })}
    />
  );
}

function TaskTableBodyCell({
  row,
  column,
  table,
}: CellContext<TaskTableDisplayRow, unknown>) {
  const meta = getTableViewMeta(table);
  const { t, i18n } = useT("tasks");
  // Computed (and the unmount responder registered) before the early return so
  // the hook order is stable across task/group rows.
  const cellKey =
    row.original.kind === "task" ? `${row.original.key}:${column.id}` : null;
  useReleaseEditingCellOnUnmount(
    cellKey,
    meta.editingCellKey,
    meta.setEditingCellKey,
  );
  // Placeholder rows go through the ordinary cell renderer so they inherit the
  // real column widths, pinning and borders — the grid is already correct
  // before any data arrives, so the rows swap in without shifting anything.
  if (row.original.kind === "skeleton") {
    return <Skeleton className="h-3.5 w-full" />;
  }
  if (row.original.kind !== "task") return null;
  const taskRow = row.original;
  const task = taskRow.task;
  const key = column.id as TableColumnKey;
  const editorOpen = meta.editingCellKey === cellKey;
  const setEditorOpen = (open: boolean) =>
    meta.setEditingCellKey(open ? cellKey : null);
  const onUpdate = (updates: Partial<UpdateTaskRequest>) =>
    meta.updateTask(task.id, updates);

  const propertyId = propertyIdFromViewKey(key);
  if (propertyId) {
    const property = meta.propertyById.get(propertyId);
    if (!property) return null;
    return (
      <div onClick={stopRowNavigation}>
        <CustomPropertyValueEditor
          task={task}
          property={property}
          open={editorOpen}
          onOpenChange={setEditorOpen}
        />
      </div>
    );
  }
  switch (key) {
    case "title":
      return (
        <InlineTitle
          row={taskRow}
          editing={editorOpen}
          onEditingChange={setEditorOpen}
          onUpdate={onUpdate}
          onOpen={() => meta.openTask(task)}
          onCreateSubTask={() => meta.createSubTask(task)}
          onToggleParent={() => meta.toggleTableParentCollapsed(task.id)}
          toggleLabel={t(($) => $.table.toggle_sub_tasks)}
          renameLabel={t(($) => $.table.rename_title)}
          createSubTaskLabel={t(($) => $.actions.create_sub_task)}
        />
      );
    case "identifier":
      return (
        <span className="text-caption text-muted-foreground">{task.identifier}</span>
      );
    case "status":
      return (
        <div onClick={stopRowNavigation}>
          <StatusPicker
            status={task.status}
            onUpdate={onUpdate}
            align="start"
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "priority":
      return (
        <div onClick={stopRowNavigation}>
          <PriorityPicker
            priority={task.priority}
            onUpdate={onUpdate}
            align="start"
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "assignee":
      return (
        <div onClick={stopRowNavigation}>
          <AssigneePicker
            assigneeType={task.assignee_type}
            assigneeId={task.assignee_id}
            onUpdate={onUpdate}
            align="start"
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "labels":
      return (
        <LazyLabelCell
          task={task}
          open={editorOpen}
          onOpenChange={setEditorOpen}
        />
      );
    case "project":
      return (
        <div onClick={stopRowNavigation}>
          <ProjectPicker
            projectId={task.project_id}
            onUpdate={onUpdate}
            open={editorOpen}
            onOpenChange={setEditorOpen}
            triggerRender={
              <button
                type="button"
                className="flex max-w-full items-center gap-1.5 rounded px-1 py-0.5 hover:bg-accent"
              />
            }
          />
        </div>
      );
    case "start_date":
      return (
        <div onClick={stopRowNavigation}>
          <StartDatePicker
            startDate={task.start_date}
            onUpdate={onUpdate}
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "due_date":
      return (
        <div onClick={stopRowNavigation}>
          <DueDatePicker
            dueDate={task.due_date}
            onUpdate={onUpdate}
            open={editorOpen}
            onOpenChange={setEditorOpen}
          />
        </div>
      );
    case "created_at":
    case "updated_at":
      return (
        <span className="text-caption text-muted-foreground">
          {new Intl.DateTimeFormat(i18n.language, {
            month: "short",
            day: "numeric",
            year: "numeric",
          }).format(new Date(task[key]))}
        </span>
      );
    case "child_progress": {
      const progress = meta.childProgressMap.get(task.id);
      return progress ? (
        <span className="inline-flex items-center gap-1.5 text-caption text-muted-foreground">
          <ProgressRing done={progress.done} total={progress.total} size={15} />
          {progress.done}/{progress.total}
        </span>
      ) : (
        <span className="text-muted-foreground">{t(($) => $.table.empty_value)}</span>
      );
    }
    case "creator":
      return (
        <span className="flex min-w-0 items-center gap-1.5">
          <ActorAvatar
            actorType={task.creator_type}
            actorId={task.creator_id}
            size="sm"
          />
          <span className="truncate">
            {meta.getActorName(task.creator_type, task.creator_id)}
          </span>
        </span>
      );
  }
  return null;
}

export function TableView({
  serverQuery,
  childProgressMap,
  search,
  onSearchChange,
  onLoadedTasksChange,
  onCreateTask,
  exportTasks,
  resolveExportLookups,
}: TableViewProps) {
  const { t } = useT("tasks");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const actions = useTaskSurfaceActionsOptional();
  const selection = useTaskSurfaceSelection();
  const { getActorName } = useActorName();
  const {
    data: properties = [],
    isSuccess: propertyCatalogSettled,
  } = useQuery(propertyListOptions(wsId));
  const propertyById = useMemo(
    () => new Map(properties.map((property) => [property.id, property])),
    [properties],
  );
  const activePropertyIds = useMemo(
    () => new Set(properties.map((property) => property.id)),
    [properties],
  );
  const groupablePropertyIds = useMemo(
    () =>
      new Set(
        properties
          .filter((property) => ["select", "checkbox"].includes(property.type))
          .map((property) => property.id),
      ),
    [properties],
  );
  const tableColumns = useViewStore((state) => state.tableColumns);
  const toggleTableColumn = useViewStore((state) => state.toggleTableColumn);
  const reorderTableColumn = useViewStore((state) => state.reorderTableColumn);
  const setTableColumnWidth = useViewStore((state) => state.setTableColumnWidth);
  const tableGrouping = useViewStore((state) => state.tableGrouping);
  const setTableGrouping = useViewStore((state) => state.setTableGrouping);
  const tableCollapsedGroups = useViewStore((state) => state.tableCollapsedGroups);
  const toggleTableGroupCollapsed = useViewStore(
    (state) => state.toggleTableGroupCollapsed,
  );
  const tableCollapsedParents = useViewStore((state) => state.tableCollapsedParents);
  const toggleTableParentCollapsed = useViewStore(
    (state) => state.toggleTableParentCollapsed,
  );
  const tableHierarchy = useViewStore((state) => state.tableHierarchy);
  const sortBy = useViewStore((state) => state.sortBy);
  const setSortBy = useViewStore((state) => state.setSortBy);
  const sortDirection = useViewStore((state) => state.sortDirection);
  const setSortDirection = useViewStore((state) => state.setSortDirection);
  const [exporting, setExporting] = useState<"all" | "selected" | null>(null);
  const selectionAnchorRef = useRef<string | null>(null);
  // The one cell whose editor (picker popup / rename input) is open — see
  // TableViewMeta.editingCellKey.
  const [editingCellKey, setEditingCellKey] = useState<string | null>(null);

  const groupingPropertyId = propertyIdFromViewKey(tableGrouping);
  const effectiveTableGrouping =
    groupingPropertyId &&
    propertyCatalogSettled &&
    !groupablePropertyIds.has(groupingPropertyId)
      ? "none"
      : tableGrouping;
  useEffect(() => {
    if (
      !groupingPropertyId ||
      !propertyCatalogSettled ||
      groupablePropertyIds.has(groupingPropertyId)
    ) {
      return;
    }
    setTableGrouping("none");
    toast.info(t(($) => $.table.group_property_unavailable));
  }, [
    groupablePropertyIds,
    groupingPropertyId,
    propertyCatalogSettled,
    setTableGrouping,
    t,
  ]);

  const serverGroupSpec = useMemo(
    () => tableGroupSpec(effectiveTableGrouping),
    [effectiveTableGrouping],
  );
  const usesServerGrouping = serverGroupSpec.kind !== "none";
  const serverGroupsRequestGroup =
    serverGroupSpec.kind === "none"
      ? ({ kind: "status" } as const)
      : serverGroupSpec;
  const serverGroupsQuery = useInfiniteQuery({
    ...taskTableGroupsOptions(
      wsId,
      serverQuery,
      serverGroupsRequestGroup,
    ),
    enabled: usesServerGrouping,
  });
  const {
    data: serverGroupsData,
    isPending: serverGroupsPending,
    isError: serverGroupsError,
    hasNextPage: hasNextServerGroupPage,
    isFetchingNextPage: fetchingNextServerGroupPage,
    refetch: refetchServerGroups,
    fetchNextPage: fetchNextServerGroupPage,
  } = serverGroupsQuery;
  useEffect(() => {
    const body =
      serverGroupsQuery.error instanceof ApiError &&
      serverGroupsQuery.error.body &&
      typeof serverGroupsQuery.error.body === "object"
        ? (serverGroupsQuery.error.body as { error?: unknown })
        : null;
    if (
      serverGroupsQuery.error instanceof ApiError &&
      serverGroupsQuery.error.status === 422 &&
      body?.error === "unsupported_group"
    ) {
      setTableGrouping("none");
      toast.info(t(($) => $.table.group_property_unavailable));
    }
  }, [serverGroupsQuery.error, setTableGrouping, t]);
  const serverGroups = useMemo(
    () => serverGroupsData?.pages.flatMap((page) => page.groups) ?? [],
    [serverGroupsData?.pages],
  );
  const serverIdentity = useMemo(
    () => JSON.stringify([serverQuery, serverGroupSpec, tableHierarchy]),
    [serverGroupSpec, serverQuery, tableHierarchy],
  );
  const serverStructureIdentity = useMemo(
    () => JSON.stringify([serverGroupSpec, tableHierarchy]),
    [serverGroupSpec, tableHierarchy],
  );
  const collapsedGroupSet = useMemo(
    () => new Set(tableCollapsedGroups),
    [tableCollapsedGroups],
  );
  const collapsedParentSet = useMemo(
    () => new Set(tableCollapsedParents),
    [tableCollapsedParents],
  );
  const [serverBranchState, setServerBranchState] =
    useState<ServerBranchState>({
      identity: "",
      structureIdentity: "",
      branches: new Map(),
    });

  const rebasedServerBranchState = useMemo(
    () =>
      rebaseServerBranchState(
        serverBranchState,
        serverIdentity,
        serverStructureIdentity,
        usesServerGrouping,
      ),
    [
      serverBranchState,
      serverIdentity,
      serverStructureIdentity,
      usesServerGrouping,
    ],
  );

  // Commit the synchronous rebase after render. Consumers use the derived
  // state above immediately, so a filter/search/sort transition never has an
  // empty frame while this effect catches state up.
  useEffect(() => {
    if (rebasedServerBranchState !== serverBranchState) {
      setServerBranchState(rebasedServerBranchState);
    }
  }, [rebasedServerBranchState, serverBranchState]);

  const activeServerBranches = rebasedServerBranchState.branches;
  const serverBranchPlaceholderRef = useRef(
    new Map<string, TaskTableRowsResponse>(),
  );
  const serverBranchPageTargets = useMemo<ServerBranchPageTarget[]>(
    () =>
      [...activeServerBranches.values()].flatMap((branch) =>
        branch.cursors.map((cursor) => ({ branch, cursor })),
      ),
    [activeServerBranches],
  );
  // useQueries compares and installs the supplied query list in an effect.
  // Keeping this array stable prevents a settled branch from being installed
  // again on every render (which can otherwise create a render loop once the
  // virtualized table starts measuring rows).
  const serverBranchQueries = useMemo(
    () =>
      serverBranchPageTargets.map(({ branch, cursor }) => {
        const placeholder =
          cursor === null
            ? serverBranchPlaceholderRef.current.get(
                `${serverStructureIdentity}:${branch.key}`,
              )
            : undefined;
        return {
          ...taskTableRowPageOptions(wsId, {
            query: serverQuery,
            group: serverGroupSpec,
            group_key: branch.groupKey,
            hierarchy: { enabled: tableHierarchy },
            parent_id: branch.parentId,
            page: { limit: 50, cursor },
          }),
          // QueriesObserver replaces observers by query hash, so
          // keepPreviousData alone cannot bridge a changed table query inside
          // useQueries. Retain the last settled head per structural branch to
          // keep the previous table painted while the new query is pending.
          ...(placeholder ? { placeholderData: () => placeholder } : {}),
          enabled:
            (branch.groupKey === null ||
              !collapsedGroupSet.has(branch.groupKey)) &&
            !branch.ancestorIds.some((id) => collapsedParentSet.has(id)),
        };
      }),
    [
      collapsedGroupSet,
      collapsedParentSet,
      serverBranchPageTargets,
      serverGroupSpec,
      serverQuery,
      serverStructureIdentity,
      tableHierarchy,
      wsId,
    ],
  );
  const combineServerBranchQueries = useCallback(
    (results: Array<UseQueryResult<TaskTableRowsResponse, Error>>) => {
      const byBranch: Record<string, ServerBranchData> = {};
      for (let index = 0; index < serverBranchPageTargets.length; index += 1) {
        const target = serverBranchPageTargets[index];
        const result = results[index];
        if (!target || !result) continue;
        const current = byBranch[target.branch.key] ?? {
          rows: [],
          total: 0,
          nextCursor: null,
          headUpdatedAt: 0,
          headFetching: false,
          loading: false,
          error: false,
          placeholder: false,
        };
        const page = result.data;
        if (page) {
          current.rows.push(...page.rows);
          if (target.cursor === null) current.total = page.total;
          current.nextCursor = page.next_cursor;
        }
        if (target.cursor === null) {
          current.headUpdatedAt = result.dataUpdatedAt;
          current.headFetching = result.isFetching;
        }
        current.loading ||= result.isPending || result.isFetching;
        current.error ||= result.isError;
        current.placeholder ||= result.isPlaceholderData;
        byBranch[target.branch.key] = current;
      }
      return byBranch;
    },
    [serverBranchPageTargets],
  );
  // `combine` is structurally shared by React Query. It must return only plain
  // objects/arrays: Map instances and freshly-created retry closures cannot be
  // shared, and make useQueries publish a different snapshot on every render.
  const serverBranchData = useQueries({
    queries: serverBranchQueries,
    combine: combineServerBranchQueries,
  });

  useEffect(() => {
    const next = new Map<string, TaskTableRowsResponse>();
    for (const branch of activeServerBranches.values()) {
      const key = `${serverStructureIdentity}:${branch.key}`;
      const data = serverBranchData[branch.key];
      if (!data || data.placeholder || data.loading || data.error) {
        const previous = serverBranchPlaceholderRef.current.get(key);
        if (previous) next.set(key, previous);
        continue;
      }
      next.set(key, {
        query_fingerprint: "__table_placeholder__",
        group_key: branch.groupKey,
        parent_id: branch.parentId,
        total: data.total,
        rows: data.rows,
        branch_total: data.rows.length,
        next_cursor: null,
      });
    }
    // Bound placeholders to the active structural graph. Sort/filter/search
    // transitions reuse these entries; old group/property configurations do
    // not accumulate for the lifetime of the Table component.
    serverBranchPlaceholderRef.current = next;
  }, [
    activeServerBranches,
    serverBranchData,
    serverStructureIdentity,
  ]);

  // Re-derive ancestry from the current row graph after realtime re-parenting.
  // Branch keys are parent ids, so an existing branch can move under a new
  // collapsed ancestor without being re-activated by the viewport sentinel.
  useEffect(() => {
    const desiredAncestors = new Map<string, string[]>();
    const visited = new Set<string>();
    const visit = (
      groupKey: string | null,
      parentId: string | null,
      ancestors: string[],
    ) => {
      const key = serverBranchKey(groupKey, parentId);
      if (visited.has(key)) return;
      visited.add(key);
      desiredAncestors.set(key, ancestors);
      for (const row of serverBranchData[key]?.rows ?? []) {
        if (row.direct_child_count > 0) {
          visit(groupKey, row.task.id, [...ancestors, row.task.id]);
        }
      }
    };

    if (usesServerGrouping) {
      for (const group of serverGroups) visit(group.key, null, []);
    } else {
      visit(null, null, []);
    }

    setServerBranchState((previous) => {
      if (previous.identity !== serverIdentity) return previous;
      let branches: Map<string, ServerBranch> | null = null;
      for (const [key, ancestors] of desiredAncestors) {
        const branch = previous.branches.get(key);
        if (!branch || sameStringArray(branch.ancestorIds, ancestors)) continue;
        branches ??= new Map(previous.branches);
        branches.set(key, { ...branch, ancestorIds: ancestors });
      }
      return branches ? { ...previous, branches } : previous;
    });
  }, [serverBranchData, serverGroups, serverIdentity, usesServerGrouping]);

  const activateServerBranch = useCallback(
    (
      groupKey: string | null,
      parentId: string | null,
      ancestorIds: string[],
    ) => {
      setServerBranchState((previous) => {
        if (previous.identity !== serverIdentity) return previous;
        const key = serverBranchKey(groupKey, parentId);
        const existing = previous.branches.get(key);
        if (existing && sameStringArray(existing.ancestorIds, ancestorIds)) {
          return previous;
        }
        const branches = new Map(previous.branches);
        branches.set(
          key,
          existing
            ? { ...existing, ancestorIds }
            : {
                key,
                groupKey,
                parentId,
                ancestorIds,
                cursors: [null],
              },
        );
        return { ...previous, branches };
      });
    },
    [serverIdentity],
  );

  // A broad Table invalidation refetches every active page. As soon as the
  // head starts refreshing, every later cursor is obsolete; discard them
  // before their concurrent responses can create a duplicate or missing row.
  // The revision check is a second line of defence for clients that restore a
  // refreshed head directly into the cache without exposing a fetching frame.
  const branchHeadRevisionRef = useRef<Record<string, number>>({});
  useEffect(() => {
    const previousRevisions = branchHeadRevisionRef.current;
    const nextRevisions: Record<string, number> = {};
    const branchesToTrim = new Set<string>();
    for (const [key, branch] of activeServerBranches) {
      const revision = serverBranchData[key]?.headUpdatedAt ?? 0;
      if (revision === 0) continue;
      nextRevisions[key] = revision;
      const seen = previousRevisions[key];
      if (
        branch.cursors.length > 1 &&
        (serverBranchData[key]?.headFetching ||
          (seen !== undefined && seen !== revision))
      ) {
        branchesToTrim.add(key);
      }
    }
    // Keep only the current query's active branches. This bounds the revision
    // bookkeeping across long sessions with many filter/search identities.
    branchHeadRevisionRef.current = nextRevisions;
    if (branchesToTrim.size === 0) return;

    setServerBranchState((previous) => {
      if (previous.identity !== serverIdentity) return previous;
      let branches: Map<string, ServerBranch> | null = null;
      for (const [key, branch] of previous.branches) {
        if (branchesToTrim.has(key)) {
          branches ??= new Map(previous.branches);
          branches.set(key, { ...branch, cursors: [null] });
        }
      }
      return branches ? { ...previous, branches } : previous;
    });
  }, [activeServerBranches, serverBranchData, serverIdentity]);

  const loadNextServerBranchPage = useCallback(
    (branchKey: string, cursor: string) => {
      setServerBranchState((previous) => {
        if (previous.identity !== serverIdentity) return previous;
        const branch = previous.branches.get(branchKey);
        if (!branch || branch.cursors.includes(cursor)) return previous;
        const branches = new Map(previous.branches);
        branches.set(branchKey, {
          ...branch,
          cursors: [...branch.cursors, cursor],
        });
        return { ...previous, branches };
      });
    },
    [serverIdentity],
  );

  const retryServerBranch = useCallback(
    (branchKey: string) => {
      const branch = activeServerBranches.get(branchKey);
      if (!branch) return;
      void queryClient.refetchQueries({
        queryKey: taskKeys.tableRows(
          wsId,
          serverQuery,
          serverGroupSpec,
          branch.groupKey,
          tableHierarchy,
          branch.parentId,
        ),
        exact: false,
        type: "active",
      });
    },
    [
      activeServerBranches,
      queryClient,
      serverGroupSpec,
      serverQuery,
      tableHierarchy,
      wsId,
    ],
  );

  const serverGroupLabel = useCallback(
    (descriptor: TaskTableGroupDescriptor) => {
      const value = descriptor.value;
      if (value.kind === "status") {
        if (ALL_STATUSES.includes(value.status as TaskStatus)) {
          return t(($) => $.status[value.status as TaskStatus]);
        }
        // Installed clients can receive a status introduced by a newer
        // backend. Keep the group usable instead of collapsing the response
        // to the schema fallback or rendering an empty label.
        return value.status;
      }
      if (value.kind === "assignee") {
        return value.actor
          ? getActorName(value.actor.type, value.actor.id)
          : t(($) => $.table.unassigned);
      }
      if (value.kind === "project") {
        return value.project_id
          ? value.project_id
          : t(($) => $.swimlane.no_project);
      }
      if (value.kind === "parent") {
        if (value.value_state === "unset") {
          return t(($) => $.swimlane.no_parent);
        }
        return value.parent?.title ?? t(($) => $.table.value_unavailable);
      }
      if (value.value_state === "unset") return t(($) => $.table.no_value);
      if (value.value_state === "unavailable") {
        return t(($) => $.table.value_unavailable);
      }
      const property = propertyById.get(value.property_id);
      if (typeof value.value === "boolean") {
        return value.value
          ? t(($) => $.pickers.custom_property.true_label)
          : t(($) => $.pickers.custom_property.false_label);
      }
      return (
        property?.config.options?.find((option) => option.id === value.value)
          ?.name ?? String(value.value ?? "")
      );
    },
    [getActorName, propertyById, t],
  );

  const serverDisplayRows = useMemo<TaskTableDisplayRow[]>(() => {
    const result: TaskTableDisplayRow[] = [];
    const seenTaskIds = new Set<string>();
    const appendBranch = (
      groupKey: string | null,
      parentId: string | null,
      depth: number,
      ancestorIds: string[],
    ) => {
      const key = serverBranchKey(groupKey, parentId);
      const data = serverBranchData[key];
      if (!data) {
        const registered = activeServerBranches.has(key);
        result.push({
          kind: "load_more",
          key: `${registered ? "loading" : "activate"}:${key}`,
          state: registered ? "loading" : "has_more",
          total: 0,
          onLoad: registered
            ? undefined
            : () => activateServerBranch(groupKey, parentId, ancestorIds),
        });
        return;
      }
      if (data.rows.length === 0 && data.loading) {
        result.push({
          kind: "load_more",
          key: `loading:${key}`,
          state: "loading",
          total: 0,
        });
      }
      for (const row of data.rows) {
        // A realtime move can briefly leave the same entity in old and new
        // branch caches. Render the first authoritative position only; duplicate
        // ids otherwise create duplicate React keys and duplicate selection.
        if (seenTaskIds.has(row.task.id)) continue;
        seenTaskIds.add(row.task.id);
        const collapsed = collapsedParentSet.has(row.task.id);
        result.push({
          kind: "task",
          key: row.task.id,
          task: row.task,
          depth,
          hasChildren: tableHierarchy && row.direct_child_count > 0,
          collapsed,
        });
        if (tableHierarchy && row.direct_child_count > 0 && !collapsed) {
          appendBranch(groupKey, row.task.id, depth + 1, [
            ...ancestorIds,
            row.task.id,
          ]);
        }
      }
      if (data.error) {
        result.push({
          kind: "load_more",
          key: `retry:${key}`,
          state: "error",
          total: data.total,
          onLoad: () => retryServerBranch(key),
        });
      } else if (data.nextCursor) {
        const nextCursor = data.nextCursor;
        result.push({
          kind: "load_more",
          key: `more:${key}:${nextCursor}`,
          state: data.loading ? "loading" : "has_more",
          total: data.total,
          onLoad: () => loadNextServerBranchPage(key, nextCursor),
        });
      } else if (data.rows.length > 0) {
        // Reaching the end is only worth marking on a branch that paginated;
        // the footer applies that rule, so the row is pushed unconditionally
        // and carries the total for it to judge by.
        result.push({
          kind: "load_more",
          key: `end:${key}`,
          state: "end",
          total: data.total,
        });
      }
    };

    if (usesServerGrouping) {
      for (const descriptor of serverGroups) {
        const collapsed = collapsedGroupSet.has(descriptor.key);
        result.push({
          kind: "group",
          key: descriptor.key,
          label: serverGroupLabel(descriptor),
          count: descriptor.count,
          collapsed,
        });
        if (!collapsed) appendBranch(descriptor.key, null, 0, []);
      }
    } else {
      appendBranch(null, null, 0, []);
    }
    if (
      usesServerGrouping &&
      serverGroups.length === 0 &&
      serverGroupsPending
    ) {
      result.push({
        kind: "load_more",
        key: "loading:groups",
        state: "loading",
        total: 0,
      });
    } else if (usesServerGrouping && serverGroupsError) {
      result.push({
        kind: "load_more",
        key: "retry:groups",
        state: "error",
        total: 0,
        onLoad: () => void refetchServerGroups(),
      });
    } else if (usesServerGrouping && hasNextServerGroupPage) {
      result.push({
        kind: "load_more",
        key: "more:groups",
        state: fetchingNextServerGroupPage ? "loading" : "has_more",
        total: 0,
        onLoad: () => void fetchNextServerGroupPage(),
      });
    }

    // Nothing has landed yet and something is still in flight: show the grid
    // filled with placeholders instead of one "Loading…" line, which reads as
    // an empty table more than a loading one.
    const isColdLoad =
      !result.some((row) => row.kind === "task") &&
      result.some((row) => row.kind === "load_more" && row.state === "loading");
    if (isColdLoad) {
      return Array.from({ length: SKELETON_ROW_COUNT }, (_, index) => ({
        kind: "skeleton" as const,
        key: `skeleton:${index}`,
      }));
    }

    return result;
  }, [
    collapsedGroupSet,
    collapsedParentSet,
    activeServerBranches,
    activateServerBranch,
    loadNextServerBranchPage,
    retryServerBranch,
    serverBranchData,
    serverGroupLabel,
    serverGroups,
    serverGroupsPending,
    serverGroupsError,
    hasNextServerGroupPage,
    fetchingNextServerGroupPage,
    refetchServerGroups,
    fetchNextServerGroupPage,
    tableHierarchy,
    usesServerGrouping,
  ]);

  const tableMembershipIdentity = useMemo(
    () =>
      JSON.stringify([
        serverQuery.scope,
        serverQuery.filters,
        serverQuery.search ?? "",
      ]),
    [serverQuery.filters, serverQuery.scope, serverQuery.search],
  );
  const authoritativeLoadedTasks = useMemo(() => {
    const byId = new Map<string, Task>();
    for (const branch of Object.values(serverBranchData)) {
      // Previous-data placeholders keep the old table painted during a query
      // transition, but they are not members of the new filter/search window.
      if (branch.placeholder) continue;
      for (const row of branch.rows) byId.set(row.task.id, row.task);
    }
    return [...byId.values()];
  }, [serverBranchData]);
  const [loadedTaskState, setLoadedTaskState] = useState<LoadedTaskState>({
    membershipIdentity: tableMembershipIdentity,
    tasks: new Map(),
  });
  useEffect(() => {
    setLoadedTaskState((previous) => {
      const reset = previous.membershipIdentity !== tableMembershipIdentity;
      const tasks = reset
        ? new Map<string, Task>()
        : new Map(previous.tasks);
      let changed = reset;
      for (const task of authoritativeLoadedTasks) {
        if (tasks.get(task.id) !== task) {
          tasks.set(task.id, task);
          changed = true;
        }
      }
      return changed
        ? { membershipIdentity: tableMembershipIdentity, tasks }
        : previous;
    });
  }, [authoritativeLoadedTasks, tableMembershipIdentity]);
  const loadedTasks = useMemo(
    () =>
      loadedTaskState.membershipIdentity === tableMembershipIdentity
        ? [...loadedTaskState.tasks.values()]
        : [],
    [
      loadedTaskState.tasks,
      loadedTaskState.membershipIdentity,
      tableMembershipIdentity,
    ],
  );

  const visibleColumnConfigs = useMemo(
    () =>
      tableColumns.filter((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        return !propertyId || activePropertyIds.has(propertyId);
      }),
    [activePropertyIds, tableColumns],
  );

  // While a cell editor popup / rename input is open, hold the row structure
  // still: server branch pagination and realtime refetches can rebuild or
  // reorder the row list, moving the anchor row out of the virtualized render
  // window and closing the popup the user just opened (SILI-5108). The snapshot
  // freezes ORDER only; task objects inside the rows keep tracking live
  // server-query data so the open editor reflects optimistic updates. Live
  // structure snaps back the moment the editor closes. Ref writes happen
  // during render on purpose: the snapshot must be captured from the same
  // render that flips `editing` on, and both branches are idempotent under
  // StrictMode double-render.
  const frozenRowsRef = useRef<TaskTableDisplayRow[] | null>(null);
  if (editingCellKey === null) frozenRowsRef.current = null;
  else if (frozenRowsRef.current === null)
    frozenRowsRef.current = serverDisplayRows;
  const frozenRows = frozenRowsRef.current;
  const taskById = useMemo(
    () => new Map(authoritativeLoadedTasks.map((task) => [task.id, task])),
    [authoritativeLoadedTasks],
  );
  const displayRows = useMemo(
    () =>
      frozenRows && frozenRows !== serverDisplayRows
        ? refreshFrozenTableRows(frozenRows, taskById)
        : serverDisplayRows,
    [frozenRows, taskById, serverDisplayRows],
  );
  const visibleTaskIds = useMemo(
    () =>
      displayRows
        .filter((row): row is Extract<TaskTableDisplayRow, { kind: "task" }> => row.kind === "task")
        .map((row) => row.task.id),
    [displayRows],
  );
  useEffect(() => {
    onLoadedTasksChange(loadedTasks);
  }, [loadedTasks, onLoadedTasksChange]);
  const selectedTasks = useMemo(
    () => loadedTasks.filter((task) => selection.selectedIds.has(task.id)),
    [loadedTasks, selection.selectedIds],
  );
  const handleTaskSelection = useCallback(
    (taskId: string, shiftKey: boolean) => {
      const range = shiftKey
        ? getTaskTableSelectionRange(
            visibleTaskIds,
            selectionAnchorRef.current,
            taskId,
          )
        : null;

      if (range) {
        if (selection.selectedIds.has(taskId)) selection.deselect(range);
        else selection.select(range);
        return;
      }

      selection.toggle(taskId);
      selectionAnchorRef.current = taskId;
    },
    [selection, visibleTaskIds],
  );

  useEffect(() => {
    if (selection.selectedIds.size === 0) selectionAnchorRef.current = null;
  }, [selection.selectedIds]);

  const columnLabel = useCallback(
    (key: TableColumnKey) => {
      const propertyId = propertyIdFromViewKey(key);
      if (propertyId) return propertyById.get(propertyId)?.name ?? t(($) => $.table.no_value);
      return t(($) => $.table.columns[key as ColumnLabelKey]);
    },
    [propertyById, t],
  );

  const updateTask = useCallback(
    (taskId: string, updates: Partial<UpdateTaskRequest>) =>
      actions?.updateTask(taskId, updates),
    [actions],
  );

  const openTask = useCallback(
    (task: Task) => {
      const path = paths.taskDetail(task.id);
      if (navigation.openInNewTab) {
        navigation.openInNewTab(path, task.identifier, { activate: true });
        return;
      }

      window.open(
        navigation.getShareableUrl(path),
        "_blank",
        "noopener,noreferrer",
      );
    },
    [navigation, paths],
  );

  const createSubTask = useCallback(
    (task: Task) =>
      onCreateTask({
        parent_task_id: task.id,
        parent_task_identifier: task.identifier,
        ...(task.project_id ? { project_id: task.project_id } : {}),
      }),
    [onCreateTask],
  );

  const onSort = useCallback(
    (field: SortField, direction: "asc" | "desc") => {
      setSortBy(field);
      setSortDirection(direction);
    },
    [setSortBy, setSortDirection],
  );

  // Fresh object every render is fine — cells read it through
  // table.options.meta at render time. What must NOT change per render are
  // the column defs' component identities below.
  const viewMeta: TableViewMeta = {
    childProgressMap,
    propertyById,
    properties,
    visibleTaskIds,
    editingCellKey,
    setEditingCellKey,
    updateTask,
    openTask,
    createSubTask,
    toggleTableParentCollapsed,
    handleTaskSelection,
    getActorName,
    columnLabel,
    sortBy,
    sortDirection,
    onSort,
    toggleTableColumn,
  };

  const columns = useMemo<ColumnDef<TaskTableDisplayRow>[]>(
    () => [
      {
        id: SELECT_COLUMN_ID,
        size: 44,
        minSize: 44,
        maxSize: 44,
        enableResizing: false,
        header: TaskTableSelectHeader,
        cell: TaskTableSelectCell,
      },
      ...visibleColumnConfigs.map((config): ColumnDef<TaskTableDisplayRow> => {
        const definition: ColumnDef<TaskTableDisplayRow> = {
          id: config.key,
          minSize: config.key === "title" ? 260 : 96,
          maxSize: 640,
          enableResizing: true,
          header: TaskTableHeaderCell,
          cell: TaskTableBodyCell,
        };
        if (config.width !== undefined) definition.size = config.width;
        return definition;
      }),
      {
        id: ADD_COLUMN_ID,
        size: 48,
        minSize: 48,
        maxSize: 48,
        enableResizing: false,
        header: TaskTableAddColumnHeader,
        cell: TaskTableEmptyCell,
      },
    ],
    [visibleColumnConfigs],
  );

  const columnSizing = useMemo<ColumnSizingState>(
    () =>
      Object.fromEntries(
        visibleColumnConfigs
          .filter((column) => column.width !== undefined)
          .map((column) => [column.key, column.width!]),
      ),
    [visibleColumnConfigs],
  );
  const handleColumnSizingChange = useCallback<OnChangeFn<ColumnSizingState>>(
    (updater) => {
      const next = typeof updater === "function" ? updater(columnSizing) : updater;
      for (const column of visibleColumnConfigs) {
        const width = next[column.key];
        if (width !== column.width) setTableColumnWidth(column.key, width);
      }
    },
    [columnSizing, setTableColumnWidth, visibleColumnConfigs],
  );

  const table = useReactTable({
    data: displayRows,
    columns,
    getRowId: (row) => row.key,
    getCoreRowModel: getCoreRowModel(),
    state: {
      columnSizing,
      columnPinning: { left: [SELECT_COLUMN_ID, "title"], right: [] },
    },
    meta: viewMeta as TableMeta<TaskTableDisplayRow>,
    onColumnSizingChange: handleColumnSizingChange,
    columnResizeMode: "onChange",
  });

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const handleDragEnd = useCallback(
    ({ active, over }: DragEndEvent) => {
      if (!over || active.id === over.id) return;
      reorderTableColumn(active.id as TableColumnKey, over.id as TableColumnKey);
    },
    [reorderTableColumn],
  );

  const handleExport = async (mode: "all" | "selected") => {
    setExporting(mode);
    try {
      // Every lookup the file depends on is AWAITED here rather than read
      // from render-time hook state — a cold or errored query must fail the
      // export, not silently degrade it: with an unsettled catalog the
      // user's property columns vanish from the file, and with unsettled
      // directories every actor exports as "Unknown *" (round-2 review
      // P2#4). fetchQuery throws on failure, which lands in the catch below.
      const needsPropertyCatalog = tableColumns.some(
        (column) => propertyIdFromViewKey(column.key) !== null,
      );
      // fetchQuery bypasses the options' `select`, so unwrap the response.
      const exportProperties = needsPropertyCatalog
        ? (await queryClient.fetchQuery(propertyListOptions(wsId))).properties
        : [];
      const exportPropertyById = new Map(
        exportProperties.map((property) => [property.id, property]),
      );
      // Configured columns against the RESOLVED catalog — only a property
      // definition that is genuinely gone (archived/deleted) drops out.
      const csvColumns = tableColumns.filter((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        return !propertyId || exportPropertyById.has(propertyId);
      });
      const needsActors = csvColumns.some(
        (column) => column.key === "assignee" || column.key === "creator",
      );
      const [rows, exportLookups, exportActorName] = await Promise.all([
        mode === "all" ? exportTasks() : Promise.resolve(selectedTasks),
        resolveExportLookups({
          projects: csvColumns.some((column) => column.key === "project"),
          childProgress: csvColumns.some(
            (column) => column.key === "child_progress",
          ),
        }),
        needsActors
          ? Promise.all([
              queryClient.fetchQuery(memberListOptions(wsId)),
              queryClient.fetchQuery(agentListOptions(wsId)),
              queryClient.fetchQuery(squadListOptions(wsId)),
            ]).then(([members, agents, squads]) =>
              buildActorNameResolver({ members, agents, squads }),
            )
          : Promise.resolve(getActorName),
      ]);
      const headers = csvColumns.map((column) => {
        const propertyId = propertyIdFromViewKey(column.key);
        if (propertyId) return exportPropertyById.get(propertyId)?.name ?? "";
        return columnLabel(column.key);
      });
      const csvRows = rows.map((task) =>
        csvColumns.map((column) => {
          const propertyId = propertyIdFromViewKey(column.key);
          if (propertyId) {
            const property = exportPropertyById.get(propertyId);
            return property
              ? propertyDisplayValue(property, task.properties[propertyId])
              : "";
          }
          switch (column.key) {
            case "title":
              return task.title;
            case "identifier":
              return task.identifier;
            case "status":
              return t(($) => $.status[task.status]);
            case "priority":
              return t(($) => $.priority[task.priority]);
            case "assignee":
              return task.assignee_type && task.assignee_id
                ? exportActorName(task.assignee_type, task.assignee_id)
                : "";
            case "labels":
              return task.labels?.map((label) => label.name).join(", ") ?? "";
            case "project":
              return task.project_id
                ? exportLookups.projectMap.get(task.project_id)?.title ?? ""
                : "";
            case "start_date":
            case "due_date":
              return task[column.key] ?? "";
            case "created_at":
            case "updated_at":
              return task[column.key];
            case "child_progress": {
              const progress = exportLookups.childProgressMap.get(task.id);
              return progress ? `${progress.done}/${progress.total}` : "";
            }
            case "creator":
              return exportActorName(task.creator_type, task.creator_id);
          }
          return "";
        }),
      );
      const csv = buildTaskTableCsv(headers, csvRows);
      const blob = new Blob(["\uFEFF", csv], { type: "text/csv;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      const filenamePrefix = mode === "all" ? "tasks" : "tasks-selected";
      anchor.download = `${filenamePrefix}-${new Date().toISOString().slice(0, 10)}.csv`;
      anchor.click();
      URL.revokeObjectURL(url);
      toast.success(t(($) => $.table.export_success, { count: rows.length }));
    } catch (error) {
      toast.error(
        error instanceof Error &&
          !(error instanceof TaskTableExportIntegrityError) &&
          error.message
          ? error.message
          : t(($) => $.table.export_failed),
      );
    } finally {
      setExporting(null);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5">
        <TableTaskSearch
          value={search}
          onChange={onSearchChange}
          placeholder={t(($) => $.table.search_placeholder)}
          clearLabel={t(($) => $.table.search_clear)}
        />
        <span className="mr-auto" />
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="sm"
                className="h-7"
                disabled={exporting !== null}
              >
                {exporting ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Download className="size-3.5" />
                )}
                {t(($) => $.table.export)}
                <ChevronDown className="size-3" />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-52">
            <DropdownMenuItem onClick={() => void handleExport("all")}>
              <Download className="size-3.5" />
              {t(($) => $.table.export_all)}
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={selectedTasks.length === 0}
              onClick={() => void handleExport("selected")}
            >
              <Download className="size-3.5" />
              {t(($) => $.table.export_selected, {
                count: selectedTasks.length,
              })}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        // Columns only ever swap sideways, so the header should not follow the
        // pointer up out of its own strip — same constraint the desktop tab bar
        // puts on tab reordering.
        modifiers={[restrictToHorizontalAxis]}
        // Modifiers constrain the drag's movement but not its auto-scrolling,
        // which reads raw pointer coordinates: drifting a few pixels vertically
        // while dragging a header sent the rows scrolling underneath it. Zero
        // on y removes an axis the gesture cannot act on. On x it stays, since
        // a table wider than its viewport needs it to reach a distant slot, but
        // the default 0.2 arms it a fifth of the way in from either edge, which
        // is most of a wide header.
        autoScroll={{ threshold: { x: 0.05, y: 0 } }}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={visibleColumnConfigs.map((column) => column.key)}
          strategy={horizontalListSortingStrategy}
        >
          <DataTable
            table={table}
            virtualizeRows
            emptyMessage={t(($) => $.table.empty)}
            onRowClick={(row) => {
              if (row.original.kind === "task") {
                openTask(row.original.task);
              }
            }}
            renderRow={(row) => {
              if (row.original.kind === "group") {
                return (
                  <TaskTableGroupRow
                    group={row.original}
                    colSpan={table.getVisibleLeafColumns().length}
                    onToggle={() => toggleTableGroupCollapsed(row.original.key)}
                  />
                );
              }
              if (row.original.kind === "load_more") {
                const loadMoreRow = row.original;
                return (
                  <TableRow className="hover:bg-transparent">
                    <TableCell
                      colSpan={table.getVisibleLeafColumns().length}
                      className="p-0"
                    >
                      {/* The same footer Board / List / Swimlane end their
                        * columns with. Hand-rolling it here had left the table
                        * as the one surface where a failed page read as muted
                        * body text rather than an error, and where reaching the
                        * end of a paginated branch said nothing at all. The row
                        * only supplies the cell it lives in. */}
                      <div className="sticky left-0 w-full">
                        <ListLoadMoreFooter
                          hasMore={
                            loadMoreRow.state === "loading" ||
                            loadMoreRow.state === "has_more"
                          }
                          isLoading={loadMoreRow.state === "loading"}
                          total={loadMoreRow.total}
                          onLoadMore={() => loadMoreRow.onLoad?.()}
                          isError={loadMoreRow.state === "error"}
                          onRetry={loadMoreRow.onLoad}
                        />
                      </div>
                    </TableCell>
                  </TableRow>
                );
              }
              return null;
            }}
            className="min-h-0 flex-1"
          />
        </SortableContext>
      </DndContext>
    </div>
  );
}
