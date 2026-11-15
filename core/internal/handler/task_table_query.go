package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

const (
	taskTableDefaultPageSize = 50
	taskTableMaxPageSize     = 100
	taskTableQueryTimeout    = 8 * time.Second
)

func withTaskTableQueryTimeout(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), taskTableQueryTimeout)
	return r.WithContext(ctx), cancel
}

func (h *Handler) beginTaskTableSnapshot(ctx context.Context) (*Handler, pgx.Tx, error) {
	if h.TxStarter == nil {
		return nil, nil, errors.New("transaction starter is unavailable")
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, nil, err
	}
	snapshot := *h
	snapshot.DB = tx
	snapshot.Queries = db.New(tx)
	return &snapshot, tx, nil
}

func writeTaskTableQueryFailure(w http.ResponseWriter, r *http.Request, message string) {
	if errors.Is(r.Context().Err(), context.DeadlineExceeded) {
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{
			"error":   "query_timeout",
			"message": "The table query took too long. Narrow the filters and retry.",
		})
		return
	}
	writeError(w, http.StatusInternalServerError, message)
}

type taskTableActorRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type taskTableScope struct {
	Kind          string             `json:"kind"`
	AssigneeTypes []string           `json:"assignee_types,omitempty"`
	ProjectID     string             `json:"project_id,omitempty"`
	Actor         *taskTableActorRef `json:"actor,omitempty"`
	Relation      string             `json:"relation,omitempty"`
}

type taskTableDateFilterRequest struct {
	Field string `json:"field"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type taskTableFiltersRequest struct {
	Statuses          []string                    `json:"statuses,omitempty"`
	Priorities        []string                    `json:"priorities,omitempty"`
	Assignees         []taskTableActorRef         `json:"assignees,omitempty"`
	IncludeNoAssignee bool                        `json:"include_no_assignee,omitempty"`
	Creators          []taskTableActorRef         `json:"creators,omitempty"`
	ProjectIDs        []string                    `json:"project_ids,omitempty"`
	IncludeNoProject  bool                        `json:"include_no_project,omitempty"`
	LabelIDs          []string                    `json:"label_ids,omitempty"`
	Properties        map[string][]string         `json:"properties,omitempty"`
	Date              *taskTableDateFilterRequest `json:"date,omitempty"`
	WorkingOnly       bool                        `json:"working_only,omitempty"`
	WorkingTaskIDs    []string                    `json:"working_task_ids,omitempty"`
	IncludeSubTasks   *bool                       `json:"include_sub_tasks,omitempty"`
}

type taskTableSortRequest struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type taskTableQuerySpec struct {
	Scope   taskTableScope          `json:"scope"`
	Filters taskTableFiltersRequest `json:"filters"`
	Search  string                  `json:"search,omitempty"`
	Sort    taskTableSortRequest    `json:"sort"`
}

type taskTableGroupSpec struct {
	Kind            string   `json:"kind"`
	PropertyID      string   `json:"property_id,omitempty"`
	IncludeEmpty    bool     `json:"include_empty,omitempty"`
	Primary         string   `json:"primary,omitempty"`
	Secondary       string   `json:"secondary,omitempty"`
	SecondaryValues []string `json:"secondary_values,omitempty"`
}

type taskTablePageRequest struct {
	Limit  int     `json:"limit,omitempty"`
	Cursor *string `json:"cursor,omitempty"`
}

type taskTableHierarchyRequest struct {
	Enabled bool `json:"enabled"`
}

type taskTableGroupsRequest struct {
	Query taskTableQuerySpec   `json:"query"`
	Group taskTableGroupSpec   `json:"group"`
	Page  taskTablePageRequest `json:"page"`
}

type taskTableRowsRequest struct {
	Query     taskTableQuerySpec        `json:"query"`
	Group     taskTableGroupSpec        `json:"group"`
	GroupKey  *string                   `json:"group_key"`
	Hierarchy taskTableHierarchyRequest `json:"hierarchy"`
	ParentID  *string                   `json:"parent_id"`
	Page      taskTablePageRequest      `json:"page"`
}

type taskTableFacetSpec struct {
	Kind       string `json:"kind"`
	PropertyID string `json:"property_id,omitempty"`
}

type taskTableFacetsRequest struct {
	Query        taskTableQuerySpec   `json:"query"`
	Facets       []taskTableFacetSpec `json:"facets"`
	IncludeTotal *bool                `json:"include_total,omitempty"`
}

type taskTableSQL struct {
	where       string
	args        []any
	fingerprint string
	workspaceID pgtype.UUID
}

type taskTableCursor struct {
	Version          int     `json:"v"`
	QueryFingerprint string  `json:"query"`
	GroupKey         *string `json:"group_key,omitempty"`
	ParentID         *string `json:"parent_id,omitempty"`
	GroupOrder       *int    `json:"group_order,omitempty"`
	GroupSortKey     *string `json:"group_sort_key,omitempty"`
	GroupCursorKey   *string `json:"group_cursor_key,omitempty"`
	BranchIdentity   string  `json:"branch_identity,omitempty"`
	SortValue        *string `json:"sort_value,omitempty"`
	SortIsNull       bool    `json:"sort_is_null,omitempty"`
	RowCreatedAt     string  `json:"row_created_at,omitempty"`
	RowID            string  `json:"row_id,omitempty"`
}

func decodeTaskTableJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid task table query: "+err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid task table query: request must contain one JSON object")
		return false
	}
	return true
}

func normalizeTaskTablePage(w http.ResponseWriter, page taskTablePageRequest) (int, *taskTableCursor, bool) {
	limit := page.Limit
	if limit == 0 {
		limit = taskTableDefaultPageSize
	}
	if limit < 1 || limit > taskTableMaxPageSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("page.limit must be between 1 and %d", taskTableMaxPageSize))
		return 0, nil, false
	}
	if page.Cursor == nil || strings.TrimSpace(*page.Cursor) == "" {
		return limit, nil, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(*page.Cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return 0, nil, false
	}
	var cursor taskTableCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return 0, nil, false
	}
	return limit, &cursor, true
}

func encodeTaskTableCursor(cursor taskTableCursor) *string {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return nil
	}
	value := base64.RawURLEncoding.EncodeToString(encoded)
	return &value
}

func taskTableCursorMatches(w http.ResponseWriter, cursor *taskTableCursor, fingerprint string, groupKey, parentID *string) bool {
	if cursor == nil {
		return true
	}
	if cursor.QueryFingerprint != fingerprint || !equalOptionalString(cursor.GroupKey, groupKey) || !equalOptionalString(cursor.ParentID, parentID) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "cursor_query_mismatch",
			"message": "cursor does not belong to this table query",
		})
		return false
	}
	return true
}

func equalOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func canonicalTaskTableFingerprint(workspaceID string, spec taskTableQuerySpec) (string, error) {
	explicitEmptyAssignees :=
		spec.Filters.Assignees != nil && len(spec.Filters.Assignees) == 0
	explicitEmptyWorkingTasks :=
		spec.Filters.WorkingTaskIDs != nil && len(spec.Filters.WorkingTaskIDs) == 0
	normalized := spec
	normalized.Search = strings.TrimSpace(normalized.Search)
	normalized.Scope.AssigneeTypes = sortedUniqueStrings(normalized.Scope.AssigneeTypes)
	normalized.Filters.Statuses = sortedUniqueStrings(normalized.Filters.Statuses)
	normalized.Filters.Priorities = sortedUniqueStrings(normalized.Filters.Priorities)
	normalized.Filters.ProjectIDs = sortedUniqueStrings(normalized.Filters.ProjectIDs)
	normalized.Filters.LabelIDs = sortedUniqueStrings(normalized.Filters.LabelIDs)
	normalized.Filters.Assignees = sortedUniqueActors(normalized.Filters.Assignees)
	normalized.Filters.WorkingTaskIDs = sortedUniqueStrings(normalized.Filters.WorkingTaskIDs)
	normalized.Filters.Creators = sortedUniqueActors(normalized.Filters.Creators)
	for key, values := range normalized.Filters.Properties {
		normalized.Filters.Properties[key] = sortedUniqueStrings(values)
	}
	encoded, err := json.Marshal(struct {
		WorkspaceID               string             `json:"workspace_id"`
		Query                     taskTableQuerySpec `json:"query"`
		ExplicitEmptyAssignees    bool               `json:"explicit_empty_assignees,omitempty"`
		ExplicitEmptyWorkingTasks bool               `json:"explicit_empty_working_tasks,omitempty"`
	}{
		WorkspaceID:               workspaceID,
		Query:                     normalized,
		ExplicitEmptyAssignees:    explicitEmptyAssignees,
		ExplicitEmptyWorkingTasks: explicitEmptyWorkingTasks,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil
	}
	return result
}

func sortedUniqueActors(values []taskTableActorRef) []taskTableActorRef {
	seen := make(map[string]taskTableActorRef, len(values))
	for _, value := range values {
		key := value.Type + ":" + value.ID
		seen[key] = value
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]taskTableActorRef, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func taskTableContainsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseTaskTableUUIDList(w http.ResponseWriter, values []string, field string) ([]pgtype.UUID, bool) {
	result := make([]pgtype.UUID, 0, len(values))
	for _, raw := range values {
		parsed, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+field)
			return nil, false
		}
		result = append(result, parsed)
	}
	return result, true
}

func parseTaskTableActor(w http.ResponseWriter, actor taskTableActorRef, field string) (taskActorFilter, bool) {
	if !isTaskActorType(actor.Type) {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return taskActorFilter{}, false
	}
	id, err := util.ParseUUID(actor.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return taskActorFilter{}, false
	}
	return taskActorFilter{actorType: actor.Type, actorID: id}, true
}

func appendTaskTableInvolvedPredicate(where []string, addArg func(any) string, userID pgtype.UUID) []string {
	ref := addArg(userID)
	return append(where, fmt.Sprintf(`(
    (i.assignee_type = 'agent' AND i.assignee_id IN (
       SELECT a.id FROM agent a
        WHERE a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'member'
          AND sm.member_id   = %[1]s::uuid
       UNION
       SELECT s.id
         FROM squad s
         JOIN agent a ON a.id = s.leader_id
        WHERE s.workspace_id = $1
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
       UNION
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
         JOIN agent a ON a.id = sm.member_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'agent'
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
)`, ref))
}

func (h *Handler) compileTaskTableQuery(w http.ResponseWriter, r *http.Request, spec taskTableQuerySpec) (taskTableSQL, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return taskTableSQL{}, false
	}
	fingerprint, err := canonicalTaskTableFingerprint(util.UUIDToString(workspaceUUID), spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to canonicalize table query")
		return taskTableSQL{}, false
	}

	where := []string{"i.workspace_id = $1"}
	args := []any{workspaceUUID}
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	for _, status := range spec.Filters.Statuses {
		if !taskTableContainsString(validTaskStatuses, status) {
			writeError(w, http.StatusBadRequest, "invalid filters.statuses")
			return taskTableSQL{}, false
		}
	}
	if len(spec.Filters.Statuses) > 0 {
		where = append(where, fmt.Sprintf("i.status = ANY(%s::text[])", addArg(sortedUniqueStrings(spec.Filters.Statuses))))
	}
	for _, priority := range spec.Filters.Priorities {
		if !taskTableContainsString(validTaskPriorities, priority) {
			writeError(w, http.StatusBadRequest, "invalid filters.priorities")
			return taskTableSQL{}, false
		}
	}
	if len(spec.Filters.Priorities) > 0 {
		where = append(where, fmt.Sprintf("i.priority = ANY(%s::text[])", addArg(sortedUniqueStrings(spec.Filters.Priorities))))
	}

	switch spec.Scope.Kind {
	case "workspace":
		for _, actorType := range spec.Scope.AssigneeTypes {
			if !isTaskActorType(actorType) {
				writeError(w, http.StatusBadRequest, "invalid scope.assignee_types")
				return taskTableSQL{}, false
			}
		}
		if len(spec.Scope.AssigneeTypes) > 0 {
			where = append(where, fmt.Sprintf("i.assignee_type = ANY(%s::text[])", addArg(sortedUniqueStrings(spec.Scope.AssigneeTypes))))
		}
	case "project":
		projectID, err := util.ParseUUID(spec.Scope.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid scope.project_id")
			return taskTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.project_id = %s::uuid", addArg(projectID)))
	case "assignee":
		if spec.Scope.Actor == nil {
			writeError(w, http.StatusBadRequest, "scope.actor is required")
			return taskTableSQL{}, false
		}
		actor, ok := parseTaskTableActor(w, *spec.Scope.Actor, "scope.actor")
		if !ok {
			return taskTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.assignee_type = %s::text AND i.assignee_id = %s::uuid", addArg(actor.actorType), addArg(actor.actorID)))
	case "creator":
		if spec.Scope.Actor == nil {
			writeError(w, http.StatusBadRequest, "scope.actor is required")
			return taskTableSQL{}, false
		}
		actor, ok := parseTaskTableActor(w, *spec.Scope.Actor, "scope.actor")
		if !ok {
			return taskTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.creator_type = %s::text AND i.creator_id = %s::uuid", addArg(actor.actorType), addArg(actor.actorID)))
	case "my":
		userID, ok := requireUserID(w, r)
		if !ok {
			return taskTableSQL{}, false
		}
		userUUID, err := util.ParseUUID(userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not authenticated")
			return taskTableSQL{}, false
		}
		relation := spec.Scope.Relation
		if relation == "" {
			relation = "any"
		}
		switch relation {
		case "assigned":
			where = append(where, fmt.Sprintf("i.assignee_type = 'member' AND i.assignee_id = %s::uuid", addArg(userUUID)))
		case "created":
			where = append(where, fmt.Sprintf("i.creator_type = 'member' AND i.creator_id = %s::uuid", addArg(userUUID)))
		case "involved":
			where = appendTaskTableInvolvedPredicate(where, addArg, userUUID)
		case "any":
			assignedRef := addArg(userUUID)
			createdRef := addArg(userUUID)
			involved := appendTaskTableInvolvedPredicate(nil, addArg, userUUID)[0]
			where = append(where, fmt.Sprintf("((i.assignee_type = 'member' AND i.assignee_id = %s::uuid) OR (i.creator_type = 'member' AND i.creator_id = %s::uuid) OR %s)", assignedRef, createdRef, involved))
		default:
			writeError(w, http.StatusBadRequest, "invalid scope.relation")
			return taskTableSQL{}, false
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid scope.kind")
		return taskTableSQL{}, false
	}

	if spec.Filters.Assignees != nil || spec.Filters.IncludeNoAssignee {
		ors := make([]string, 0, len(spec.Filters.Assignees)+1)
		for _, value := range spec.Filters.Assignees {
			actor, ok := parseTaskTableActor(w, value, "filters.assignees")
			if !ok {
				return taskTableSQL{}, false
			}
			ors = append(ors, fmt.Sprintf("(i.assignee_type = %s::text AND i.assignee_id = %s::uuid)", addArg(actor.actorType), addArg(actor.actorID)))
		}
		if spec.Filters.IncludeNoAssignee {
			ors = append(ors, "(i.assignee_type IS NULL AND i.assignee_id IS NULL)")
		}
		if len(ors) == 0 {
			// Omitted assignees means "no assignee filter"; an explicitly
			// empty list means "match none", so API callers can preserve an
			// intentionally active empty predicate.
			where = append(where, "FALSE")
		} else {
			where = append(where, "("+strings.Join(ors, " OR ")+")")
		}
	}

	if len(spec.Filters.Creators) > 0 {
		ors := make([]string, 0, len(spec.Filters.Creators))
		for _, value := range spec.Filters.Creators {
			actor, ok := parseTaskTableActor(w, value, "filters.creators")
			if !ok {
				return taskTableSQL{}, false
			}
			ors = append(ors, fmt.Sprintf("(i.creator_type = %s::text AND i.creator_id = %s::uuid)", addArg(actor.actorType), addArg(actor.actorID)))
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	projectIDs, ok := parseTaskTableUUIDList(w, spec.Filters.ProjectIDs, "filters.project_ids")
	if !ok {
		return taskTableSQL{}, false
	}
	if len(projectIDs) > 0 || spec.Filters.IncludeNoProject {
		ors := make([]string, 0, 2)
		if len(projectIDs) > 0 {
			ors = append(ors, fmt.Sprintf("i.project_id = ANY(%s::uuid[])", addArg(projectIDs)))
		}
		if spec.Filters.IncludeNoProject {
			ors = append(ors, "i.project_id IS NULL")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	labelIDs, ok := parseTaskTableUUIDList(w, spec.Filters.LabelIDs, "filters.label_ids")
	if !ok {
		return taskTableSQL{}, false
	}
	if len(labelIDs) > 0 {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM task_to_label itl WHERE itl.task_id = i.id AND itl.label_id = ANY(%s::uuid[]))", addArg(labelIDs)))
	}

	if len(spec.Filters.Properties) > 0 {
		raw, err := json.Marshal(spec.Filters.Properties)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid filters.properties")
			return taskTableSQL{}, false
		}
		compiled, ok := parsePropertiesFilterParam(w, string(raw))
		if !ok {
			return taskTableSQL{}, false
		}
		if len(compiled) > 0 {
			where = append(where, propertiesFilterPredicate(compiled, addArg))
		}
	}

	if spec.Filters.Date != nil {
		column := ""
		switch spec.Filters.Date.Field {
		case "created_at", "updated_at":
			column = spec.Filters.Date.Field
		default:
			writeError(w, http.StatusBadRequest, "invalid filters.date.field")
			return taskTableSQL{}, false
		}
		start, startErr := time.Parse(time.RFC3339Nano, spec.Filters.Date.Start)
		end, endErr := time.Parse(time.RFC3339Nano, spec.Filters.Date.End)
		if startErr != nil || endErr != nil || !start.Before(end) {
			writeError(w, http.StatusBadRequest, "invalid filters.date range")
			return taskTableSQL{}, false
		}
		where = append(where, fmt.Sprintf("i.%s >= %s AND i.%s < %s", column, addArg(start), column, addArg(end)))
	}

	if spec.Filters.WorkingOnly {
		where = append(where, "EXISTS (SELECT 1 FROM agent_run atq WHERE atq.task_id = i.id AND atq.status = 'running')")
	}
	workingTaskIDs, ok := parseTaskTableUUIDList(w, spec.Filters.WorkingTaskIDs, "filters.working_task_ids")
	if !ok {
		return taskTableSQL{}, false
	}
	if spec.Filters.WorkingTaskIDs != nil {
		if len(workingTaskIDs) == 0 {
			where = append(where, "FALSE")
		} else {
			where = append(where, fmt.Sprintf(
				"i.id = ANY(%s::uuid[])",
				addArg(workingTaskIDs),
			))
		}
	}
	if spec.Filters.IncludeSubTasks != nil && !*spec.Filters.IncludeSubTasks {
		where = append(where, "i.parent_task_id IS NULL")
	}
	where = appendTaskTableSearchFilter(where, addArg, spec.Search)

	return taskTableSQL{
		where:       strings.Join(where, " AND "),
		args:        args,
		fingerprint: fingerprint,
		workspaceID: workspaceUUID,
	}, true
}
