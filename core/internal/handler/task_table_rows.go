package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/logger"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

type taskTableRowResponse struct {
	Task             TaskResponse `json:"task"`
	DirectChildCount int64        `json:"direct_child_count"`
}

type taskTableRowsResponse struct {
	QueryFingerprint string                 `json:"query_fingerprint"`
	GroupKey         *string                `json:"group_key"`
	ParentID         *string                `json:"parent_id"`
	Total            int64                  `json:"total"`
	Rows             []taskTableRowResponse `json:"rows"`
	BranchTotal      int64                  `json:"branch_total"` // Current page size; retained for response compatibility.
	NextCursor       *string                `json:"next_cursor"`
}

type resolvedTaskTableSort struct {
	expression string
	direction  string
	castType   string
	nullsLast  bool
}

func (sort resolvedTaskTableSort) orderBy() string {
	orderBy := sort.expression + " " + strings.ToUpper(sort.direction)
	if sort.nullsLast {
		orderBy += " NULLS LAST"
	}
	return orderBy + ", i.created_at DESC, i.id DESC"
}

func (sort resolvedTaskTableSort) cursorPredicate(w http.ResponseWriter, cursor *taskTableCursor, addArg func(any) string) (string, bool) {
	if cursor == nil {
		return "TRUE", true
	}
	createdAt, err := time.Parse(time.RFC3339Nano, cursor.RowCreatedAt)
	if err != nil || cursor.RowID == "" {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return "", false
	}
	rowID, err := util.ParseUUID(cursor.RowID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return "", false
	}
	createdRef := addArg(createdAt)
	idRef := addArg(rowID)
	tie := fmt.Sprintf("(i.created_at < %s::timestamptz OR (i.created_at = %s::timestamptz AND i.id < %s::uuid))", createdRef, createdRef, idRef)
	if cursor.SortIsNull {
		if !sort.nullsLast || cursor.SortValue != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return "", false
		}
		return fmt.Sprintf("(%s IS NULL AND %s)", sort.expression, tie), true
	}
	if cursor.SortValue == nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return "", false
	}
	switch sort.castType {
	case "integer":
		if _, err := strconv.ParseInt(*cursor.SortValue, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return "", false
		}
	case "double precision", "numeric":
		if _, err := strconv.ParseFloat(*cursor.SortValue, 64); err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return "", false
		}
	case "timestamptz":
		if _, err := time.Parse(time.RFC3339Nano, *cursor.SortValue); err != nil {
			if _, postgresErr := time.Parse("2006-01-02 15:04:05.999999999Z07", *cursor.SortValue); postgresErr != nil {
				writeError(w, http.StatusBadRequest, "invalid cursor")
				return "", false
			}
		}
	case "date":
		if _, err := time.Parse("2006-01-02", *cursor.SortValue); err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return "", false
		}
	}
	valueRef := addArg(*cursor.SortValue)
	valueExpr := fmt.Sprintf("%s::%s", valueRef, sort.castType)
	comparison := ">"
	if sort.direction == "desc" {
		comparison = "<"
	}
	predicate := fmt.Sprintf("(%s %s %s OR (%s = %s AND %s))", sort.expression, comparison, valueExpr, sort.expression, valueExpr, tie)
	if sort.expression == "i.position" {
		// The exact mixed-direction keyset predicate is not itself indexable.
		// This redundant lower bound lets PostgreSQL start the default position
		// index at the cursor instead of filtering every preceding index entry.
		predicate = fmt.Sprintf("(%s >= %s AND %s)", sort.expression, valueExpr, predicate)
	}
	if sort.nullsLast {
		predicate = fmt.Sprintf("(%s IS NULL OR %s)", sort.expression, predicate)
	}
	return predicate, true
}

func (h *Handler) taskTableOrderBy(w http.ResponseWriter, r *http.Request, workspaceID string, sortRequest taskTableSortRequest) (resolvedTaskTableSort, bool) {
	sortField := strings.TrimSpace(sortRequest.Field)
	if sortField == "" {
		sortField = "position"
	}
	resolved := resolvedTaskTableSort{
		expression: "i.position",
		direction:  "asc",
		castType:   "double precision",
	}
	switch sortField {
	case "position":
	case "title":
		resolved.expression = "i.title"
		resolved.castType = "text"
	case "created_at", "updated_at":
		resolved.expression = "i." + sortField
		resolved.castType = "timestamptz"
	case "start_date", "due_date":
		resolved.expression = "i." + sortField
		resolved.castType = "date"
		resolved.nullsLast = true
	case "status":
		resolved.expression = "CASE i.status WHEN 'backlog' THEN 0 WHEN 'todo' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'in_review' THEN 3 WHEN 'done' THEN 4 WHEN 'blocked' THEN 5 WHEN 'cancelled' THEN 6 ELSE 7 END"
		resolved.castType = "integer"
	case "priority":
		resolved.expression = "CASE i.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END"
		resolved.castType = "integer"
	default:
		expr, handled, err := h.propertySortExpr(r, workspaceID, sortField)
		if !handled {
			writeError(w, http.StatusBadRequest, "invalid query.sort.field")
			return resolvedTaskTableSort{}, false
		}
		if err != nil {
			if strings.HasPrefix(err.Error(), "resolve sort property:") {
				slog.Warn("resolve table sort property failed", append(logger.RequestAttrs(r), "error", err)...)
				writeTaskTableQueryFailure(w, r, "failed to resolve table sort")
			} else {
				writeError(w, http.StatusBadRequest, err.Error())
			}
			return resolvedTaskTableSort{}, false
		}
		if expr != "" {
			resolved.expression = expr
			resolved.castType = "text"
			if strings.Contains(expr, "::numeric") {
				resolved.castType = "numeric"
			}
			resolved.nullsLast = true
		}
	}

	direction := strings.ToLower(strings.TrimSpace(sortRequest.Direction))
	if direction == "" {
		direction = "asc"
	}
	if direction != "asc" && direction != "desc" {
		writeError(w, http.StatusBadRequest, "invalid query.sort.direction")
		return resolvedTaskTableSort{}, false
	}
	if sortField != "position" && resolved.expression != "i.position" {
		resolved.direction = direction
	}
	return resolved, true
}

func normalizeTaskTableGroupKey(w http.ResponseWriter, group taskTableGroupSpec, groupKey *string) (*string, bool) {
	if group.Kind == "none" {
		if groupKey != nil && strings.TrimSpace(*groupKey) != "" {
			writeError(w, http.StatusBadRequest, "group_key must be empty when group.kind=none")
			return nil, false
		}
		return nil, true
	}
	if groupKey == nil || strings.TrimSpace(*groupKey) == "" {
		writeError(w, http.StatusBadRequest, "group_key is required")
		return nil, false
	}
	normalized := strings.TrimSpace(*groupKey)
	return &normalized, true
}

func (h *Handler) ListTaskTableRows(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}
	baseHandler := h
	var request taskTableRowsRequest
	if !decodeTaskTableJSON(w, r, &request) {
		return
	}
	r, cancel := withTaskTableQueryTimeout(r)
	defer cancel()
	snapshot, tx, err := h.beginTaskTableSnapshot(r.Context())
	if err != nil {
		slog.Warn("ListTaskTableRows snapshot failed", append(logger.RequestAttrs(r), "error", err)...)
		writeTaskTableQueryFailure(w, r, "failed to start table query")
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	h = snapshot
	limit, cursor, ok := normalizeTaskTablePage(w, request.Page)
	if !ok {
		return
	}
	groupKey, ok := normalizeTaskTableGroupKey(w, request.Group, request.GroupKey)
	if !ok {
		return
	}
	if !request.Hierarchy.Enabled && request.ParentID != nil {
		writeError(w, http.StatusBadRequest, "parent_id requires hierarchy.enabled=true")
		return
	}
	compiled, ok := h.compileTaskTableQuery(w, r, request.Query)
	if !ok {
		return
	}
	if !taskTableCursorMatches(w, cursor, compiled.fingerprint, groupKey, request.ParentID) {
		return
	}
	branchIdentity := taskTableGroupIdentity(request.Group) + ":hierarchy=" + strconv.FormatBool(request.Hierarchy.Enabled)
	if cursor != nil && cursor.BranchIdentity != branchIdentity {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "cursor_query_mismatch",
			"message": "cursor does not belong to this table branch",
		})
		return
	}
	group, ok := h.resolveTaskTableGroup(w, r, compiled.workspaceID, request.Group, true)
	if !ok {
		return
	}
	resolvedSort, ok := h.taskTableOrderBy(w, r, util.UUIDToString(compiled.workspaceID), request.Query.Sort)
	if !ok {
		return
	}

	args := append([]any(nil), compiled.args...)
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	predicateKey := ""
	if groupKey != nil {
		predicateKey = *groupKey
	}
	groupPredicate, ok := group.predicate(w, predicateKey, addArg)
	if !ok {
		return
	}
	branchPredicate := "TRUE"
	if request.Hierarchy.Enabled {
		if request.ParentID == nil {
			// Keep parent membership as a scalar lookup. NOT EXISTS is equivalent
			// semantically, but PostgreSQL turns it into a full hash anti-join and
			// loses the ordered workspace index before LIMIT.
			branchPredicate = "i.parent_task_id IS NULL OR (SELECT parent.id FROM membership parent WHERE parent.id = i.parent_task_id) IS NULL"
		} else {
			parentUUID, err := util.ParseUUID(*request.ParentID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid parent_id")
				return
			}
			parentRef := addArg(parentUUID)
			branchPredicate = fmt.Sprintf("i.parent_task_id = %s::uuid AND EXISTS (SELECT 1 FROM membership parent WHERE parent.id = %s::uuid)", parentRef, parentRef)
		}
	}
	cursorPredicate, ok := resolvedSort.cursorPredicate(w, cursor, addArg)
	if !ok {
		return
	}
	limitRef := addArg(limit + 1)

	// Pick the requested page before computing hierarchy metadata. The old query
	// materialized every matching task and aggregated every parent before LIMIT,
	// which made a 51-row page spill the entire workspace membership to disk.
	// NOT MATERIALIZED lets PostgreSQL push parent_id/id predicates into task and
	// use the parent indexes for child branches and per-page child counts.
	ctePrefix := "WITH "
	pageSource := "task"
	pagePredicate := fmt.Sprintf("(%s) AND (%s)", compiled.where, groupPredicate)
	if request.Hierarchy.Enabled {
		ctePrefix += fmt.Sprintf(`membership AS NOT MATERIALIZED (
  SELECT i.*
  FROM task i
  WHERE %s AND (%s)
), `, compiled.where, groupPredicate)
		pageSource = "membership"
		pagePredicate = branchPredicate
	} else if group.kind == "compound" &&
		group.primary != nil &&
		group.primary.kind == "parent" &&
		group.secondaryFiltered {
		visibleRef := addArg(group.secondaryValues)
		ctePrefix += fmt.Sprintf(`membership AS NOT MATERIALIZED (
  SELECT i.*
  FROM task i
  WHERE %s
), promoted_parents AS (
  SELECT DISTINCT child.parent_task_id AS id
  FROM membership child
  WHERE child.parent_task_id IS NOT NULL
    AND child.status = ANY(%s::text[])
), `, compiled.where, visibleRef)
		pageSource = "membership"
		pagePredicate = fmt.Sprintf(`(%s) AND NOT (
  i.parent_task_id IS NULL AND
  EXISTS (SELECT 1 FROM promoted_parents p WHERE p.id = i.id)
)`, groupPredicate)
	}
	cte := fmt.Sprintf(`%spage AS MATERIALIZED (
  SELECT i.*, (%s)::text AS table_sort_key
  FROM %s i
  WHERE (%s) AND %s
  ORDER BY %s
  LIMIT %s
)`, ctePrefix, resolvedSort.expression, pageSource, pagePredicate, cursorPredicate, resolvedSort.orderBy(), limitRef)
	childCountExpr := "0::bigint"
	if request.Hierarchy.Enabled {
		childCountExpr = "(SELECT COUNT(*)::bigint FROM membership child WHERE child.parent_task_id = i.id)"
	}

	query := fmt.Sprintf(`%s
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_task_id, i.position, i.start_date, i.due_date, i.created_at,
	       i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
	       %s AS direct_child_count, i.table_sort_key
	FROM page i
	ORDER BY %s`, cte, childCountExpr, resolvedSort.orderBy())

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		slog.Warn("ListTaskTableRows query failed", append(logger.RequestAttrs(r), "error", err)...)
		writeTaskTableQueryFailure(w, r, "failed to list table rows")
		return
	}
	defer rows.Close()

	type scannedRow struct {
		task       db.ListTasksRow
		childCount int64
		sortKey    pgtype.Text
	}
	scanned := make([]scannedRow, 0, limit+1)
	for rows.Next() {
		var row scannedRow
		if err := rows.Scan(
			&row.task.ID,
			&row.task.WorkspaceID,
			&row.task.Title,
			&row.task.Description,
			&row.task.Status,
			&row.task.Priority,
			&row.task.AssigneeType,
			&row.task.AssigneeID,
			&row.task.CreatorType,
			&row.task.CreatorID,
			&row.task.ParentTaskID,
			&row.task.Position,
			&row.task.StartDate,
			&row.task.DueDate,
			&row.task.CreatedAt,
			&row.task.UpdatedAt,
			&row.task.Number,
			&row.task.ProjectID,
			&row.task.Metadata,
			&row.task.Stage,
			&row.task.Properties,
			&row.childCount,
			&row.sortKey,
		); err != nil {
			writeTaskTableQueryFailure(w, r, "failed to list table rows")
			return
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		writeTaskTableQueryFailure(w, r, "failed to list table rows")
		return
	}
	rows.Close()

	// Only the ungrouped root head consumes a query-wide total. Group headers
	// get their exact totals from /groups; child branches and continuation pages
	// must not pay for a full-membership COUNT.
	var total int64
	if cursor == nil && request.Group.Kind == "none" && request.ParentID == nil {
		if err := h.DB.QueryRow(r.Context(), fmt.Sprintf("SELECT COUNT(*)::bigint FROM task i WHERE %s", compiled.where), compiled.args...).Scan(&total); err != nil {
			slog.Warn("ListTaskTableRows total count failed", append(logger.RequestAttrs(r), "error", err)...)
			writeTaskTableQueryFailure(w, r, "failed to count table rows")
			return
		}
	}

	var nextCursor *string
	if len(scanned) > limit {
		scanned = scanned[:limit]
		last := scanned[len(scanned)-1]
		var sortValue *string
		if last.sortKey.Valid {
			value := last.sortKey.String
			sortValue = &value
		}
		nextCursor = encodeTaskTableCursor(taskTableCursor{
			Version:          1,
			QueryFingerprint: compiled.fingerprint,
			GroupKey:         groupKey,
			ParentID:         request.ParentID,
			BranchIdentity:   branchIdentity,
			SortValue:        sortValue,
			SortIsNull:       !last.sortKey.Valid,
			RowCreatedAt:     last.task.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
			RowID:            util.UUIDToString(last.task.ID),
		})
	}

	// The row window, optional root total and cursor are the authoritative snapshot. Commit
	// it before the best-effort display enrichment below: PostgreSQL aborts a
	// transaction after any statement error, so running getTaskPrefix or
	// labelsByTask inside the snapshot would turn their intentionally tolerated
	// failures into a fatal Commit error for the entire page.
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("ListTaskTableRows snapshot commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeTaskTableQueryFailure(w, r, "failed to finish table query")
		return
	}
	committed = true

	prefix := baseHandler.getTaskPrefix(r.Context(), compiled.workspaceID)
	taskIDs := make([]pgtype.UUID, len(scanned))
	for index, row := range scanned {
		taskIDs[index] = row.task.ID
	}
	labelsByTask := baseHandler.labelsByTask(r.Context(), compiled.workspaceID, taskIDs)
	responseRows := make([]taskTableRowResponse, len(scanned))
	for index, row := range scanned {
		task := taskListRowToResponse(row.task, prefix)
		labels := labelsByTask[task.ID]
		if labels == nil {
			labels = []LabelResponse{}
		}
		task.Labels = &labels
		responseRows[index] = taskTableRowResponse{
			Task:             task,
			DirectChildCount: row.childCount,
		}
	}

	response := taskTableRowsResponse{
		QueryFingerprint: compiled.fingerprint,
		GroupKey:         groupKey,
		ParentID:         request.ParentID,
		Total:            total,
		Rows:             responseRows,
		BranchTotal:      int64(len(responseRows)),
		NextCursor:       nextCursor,
	}
	writeJSON(w, http.StatusOK, response)
}
