package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type taskTableEnrichmentFailTxStarter struct {
	inner           txStarter
	labelCalls      *int
	tableQueryCalls *int
	facetQueryCalls *int
	rowQuerySQL     *string
	groupQuerySQL   *string
}

func (s taskTableEnrichmentFailTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &taskTableEnrichmentFailTx{
		Tx:              tx,
		labelCalls:      s.labelCalls,
		tableQueryCalls: s.tableQueryCalls,
		facetQueryCalls: s.facetQueryCalls,
		rowQuerySQL:     s.rowQuerySQL,
		groupQuerySQL:   s.groupQuerySQL,
	}, nil
}

type taskTableEnrichmentFailTx struct {
	pgx.Tx
	labelCalls      *int
	tableQueryCalls *int
	facetQueryCalls *int
	rowQuerySQL     *string
	groupQuerySQL   *string
}

func (tx *taskTableEnrichmentFailTx) recordTableQuery(sql string) {
	if tx.tableQueryCalls != nil {
		if strings.Contains(sql, "page AS MATERIALIZED (") ||
			strings.Contains(sql, "SELECT COUNT(*)::bigint FROM task i WHERE") {
			*tx.tableQueryCalls = *tx.tableQueryCalls + 1
		}
	}
	if tx.facetQueryCalls != nil && strings.Contains(sql, "GROUP BY GROUPING SETS") {
		*tx.facetQueryCalls = *tx.facetQueryCalls + 1
	}
	if tx.rowQuerySQL != nil && strings.Contains(sql, "page AS MATERIALIZED (") {
		*tx.rowQuerySQL = sql
	}
	if tx.groupQuerySQL != nil && strings.Contains(sql, "WITH grouped AS (") {
		*tx.groupQuerySQL = sql
	}
}

func (tx *taskTableEnrichmentFailTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.recordTableQuery(sql)
	if strings.Contains(sql, "ListLabelsForTasks") {
		*tx.labelCalls = *tx.labelCalls + 1
		// A real PostgreSQL statement error poisons the transaction until
		// rollback. Before enrichment moved after Commit, this turned the
		// otherwise successful row window into a 500.
		_, err := tx.Tx.Exec(ctx, "SELECT * FROM task_table_missing_enrichment_relation")
		return nil, err
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx *taskTableEnrichmentFailTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.recordTableQuery(sql)
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestCanonicalTaskTableFingerprintNormalizesSetLikeArrays(t *testing.T) {
	left := taskTableQuerySpec{
		Scope: taskTableScope{Kind: "workspace", AssigneeTypes: []string{"agent", "member", "agent"}},
		Filters: taskTableFiltersRequest{
			Statuses:   []string{"todo", "backlog", "todo"},
			ProjectIDs: []string{"b", "a"},
		},
		Sort: taskTableSortRequest{Field: "title", Direction: "asc"},
	}
	right := taskTableQuerySpec{
		Scope: taskTableScope{Kind: "workspace", AssigneeTypes: []string{"member", "agent"}},
		Filters: taskTableFiltersRequest{
			Statuses:   []string{"backlog", "todo"},
			ProjectIDs: []string{"a", "b"},
		},
		Sort: taskTableSortRequest{Field: "title", Direction: "asc"},
	}
	leftFingerprint, err := canonicalTaskTableFingerprint("workspace-1", left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := canonicalTaskTableFingerprint("workspace-1", right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("equivalent table queries produced different fingerprints: %s != %s", leftFingerprint, rightFingerprint)
	}
}

func TestCanonicalTaskTableFingerprintBindsWorkspace(t *testing.T) {
	spec := taskTableQuerySpec{
		Scope: taskTableScope{Kind: "workspace"},
		Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
	}
	left, err := canonicalTaskTableFingerprint("workspace-1", spec)
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalTaskTableFingerprint("workspace-2", spec)
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("equivalent queries in different workspaces produced the same fingerprint")
	}
}

func TestTaskTableExplicitEmptyAssigneesMatchesNone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	base := taskTableQuerySpec{
		Scope: taskTableScope{Kind: "workspace"},
		Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
	}
	withEmpty := base
	withEmpty.Filters.Assignees = []taskTableActorRef{}

	unfilteredFingerprint, err := canonicalTaskTableFingerprint(testWorkspaceID, base)
	if err != nil {
		t.Fatal(err)
	}
	emptyFingerprint, err := canonicalTaskTableFingerprint(testWorkspaceID, withEmpty)
	if err != nil {
		t.Fatal(err)
	}
	if unfilteredFingerprint == emptyFingerprint {
		t.Fatal("explicit empty assignees must not share the unfiltered cursor fingerprint")
	}

	w := httptest.NewRecorder()
	compiled, ok := testHandler.compileTaskTableQuery(
		w,
		newRequest(http.MethodPost, "/api/tasks/table/rows", nil),
		withEmpty,
	)
	if !ok {
		t.Fatalf("compile failed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(compiled.where, "FALSE") {
		t.Fatalf("explicit empty assignees predicate = %q, want FALSE", compiled.where)
	}
}

func TestTaskTableWorkingTaskIDsAreExplicitAndAssigneeIndependent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	base := taskTableQuerySpec{
		Scope: taskTableScope{Kind: "workspace"},
		Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
	}
	withEmpty := base
	withEmpty.Filters.WorkingTaskIDs = []string{}

	unfilteredFingerprint, err := canonicalTaskTableFingerprint(testWorkspaceID, base)
	if err != nil {
		t.Fatal(err)
	}
	emptyFingerprint, err := canonicalTaskTableFingerprint(testWorkspaceID, withEmpty)
	if err != nil {
		t.Fatal(err)
	}
	if unfilteredFingerprint == emptyFingerprint {
		t.Fatal("explicit empty working_task_ids must not share the unfiltered cursor fingerprint")
	}

	w := httptest.NewRecorder()
	compiled, ok := testHandler.compileTaskTableQuery(
		w,
		newRequest(http.MethodPost, "/api/tasks/table/rows", nil),
		withEmpty,
	)
	if !ok {
		t.Fatalf("compile empty filter: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(compiled.where, "FALSE") {
		t.Fatalf("explicit empty working_task_ids predicate = %q, want FALSE", compiled.where)
	}

	withTask := base
	withTask.Filters.WorkingTaskIDs = []string{
		"00000000-0000-4000-8000-000000000001",
	}
	w = httptest.NewRecorder()
	compiled, ok = testHandler.compileTaskTableQuery(
		w,
		newRequest(http.MethodPost, "/api/tasks/table/rows", nil),
		withTask,
	)
	if !ok {
		t.Fatalf("compile task filter: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(compiled.where, "i.id = ANY(") {
		t.Errorf("working-task predicate = %q, want task-id membership", compiled.where)
	}
	if strings.Contains(compiled.where, "i.assignee_id") {
		t.Fatalf("working-task predicate must not filter task assignees: %q", compiled.where)
	}
}

func TestTaskTableWorkingTaskProjectionMatchesRunTasksNotAssignees(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	workingAgentID := createHandlerTestAgent(t, "table-working-task-agent", []byte(`{}`))
	otherAgentID := createHandlerTestAgent(t, "table-other-working-agent", []byte(`{}`))

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 3
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve task numbers: %v", err)
	}

	insertTask := func(title string, number int, assigneeType string, assigneeID any) string {
		t.Helper()
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO task (
				workspace_id, title, status, priority, creator_type, creator_id,
				assignee_type, assignee_id, position, number
			)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, $4, $5, $6, $7)
			RETURNING id
		`,
			testWorkspaceID,
			title,
			testUserID,
			assigneeType,
			assigneeID,
			number,
			number,
		).Scan(&taskID); err != nil {
			t.Fatalf("insert task %q: %v", title, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
		})
		return taskID
	}

	assignedOnlyTaskID := insertTask(
		"assigned to working agent but not being edited",
		finalNumber-2,
		"agent",
		workingAgentID,
	)
	editedTaskID := insertTask(
		"being edited by working agent but assigned to member",
		finalNumber-1,
		"member",
		testUserID,
	)
	otherAgentTaskID := insertTask(
		"being edited by another agent",
		finalNumber,
		"agent",
		workingAgentID,
	)
	createHandlerTestRunForAgentOnTask(t, workingAgentID, editedTaskID)
	createHandlerTestRunForAgentOnTask(t, otherAgentID, otherAgentTaskID)

	workingRecorder := httptest.NewRecorder()
	testHandler.ListWorkspaceWorkingAgents(
		workingRecorder,
		newRequest(http.MethodGet, "/api/working-agents?type=task", nil),
	)
	if workingRecorder.Code != http.StatusOK {
		t.Fatalf(
			"list working agents status = %d: %s",
			workingRecorder.Code,
			workingRecorder.Body.String(),
		)
	}
	var workingAgents []WorkspaceWorkingAgent
	if err := json.NewDecoder(workingRecorder.Body).Decode(&workingAgents); err != nil {
		t.Fatalf("decode working agents: %v", err)
	}
	workingTaskIDs := make([]string, 0, 2)
	for _, agent := range workingAgents {
		if agent.ID == workingAgentID || agent.ID == otherAgentID {
			workingTaskIDs = append(workingTaskIDs, agent.TaskIDs...)
		}
	}

	listRows := func(taskIDs []string) taskTableRowsResponse {
		t.Helper()
		recorder := httptest.NewRecorder()
		testHandler.ListTaskTableRows(
			recorder,
			newRequest(http.MethodPost, "/api/tasks/table/rows", map[string]any{
				"query": map[string]any{
					"scope": map[string]any{"kind": "workspace"},
					"filters": map[string]any{
						// A map intentionally preserves [] in JSON. Marshaling
						// issueTableFiltersRequest directly would apply its
						// omitempty tag and turn the match-none case into no filter.
						"working_task_ids": taskIDs,
					},
					"sort": map[string]any{
						"field":     "position",
						"direction": "asc",
					},
				},
				"group":     map[string]any{"kind": "none"},
				"hierarchy": map[string]any{"enabled": false},
				"page":      map[string]any{"limit": 10},
			}),
		)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list rows status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var response taskTableRowsResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode rows: %v", err)
		}
		return response
	}

	response := listRows(workingTaskIDs)
	if response.Total != 2 || len(response.Rows) != 2 {
		t.Fatalf("working rows total=%d rows=%d, want two", response.Total, len(response.Rows))
	}
	gotTaskIDs := make(map[string]struct{}, len(response.Rows))
	for _, row := range response.Rows {
		gotTaskIDs[row.Task.ID] = struct{}{}
	}
	if _, ok := gotTaskIDs[editedTaskID]; !ok {
		t.Errorf("missing task edited by selected working agent: %s", editedTaskID)
	}
	if _, ok := gotTaskIDs[otherAgentTaskID]; !ok {
		t.Errorf("missing task edited by other working agent: %s", otherAgentTaskID)
	}
	if _, ok := gotTaskIDs[assignedOnlyTaskID]; ok {
		t.Errorf("included assigned-only task without a running run: %s", assignedOnlyTaskID)
	}

	empty := listRows([]string{})
	if empty.Total != 0 || len(empty.Rows) != 0 {
		t.Fatalf("explicit empty working-task filter returned total=%d rows=%d", empty.Total, len(empty.Rows))
	}
}

func TestTaskTableCursorRejectsAnotherQuery(t *testing.T) {
	groupKey := "status:todo"
	cursor := taskTableCursor{
		Version:          1,
		QueryFingerprint: "sha256:old",
		GroupKey:         &groupKey,
	}
	w := httptest.NewRecorder()
	if taskTableCursorMatches(w, &cursor, "sha256:new", &groupKey, nil) {
		t.Fatal("cursor from another query unexpectedly matched")
	}
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestTaskTablePositionCursorIncludesIndexableLowerBound(t *testing.T) {
	cursorValue := "90000"
	cursor := taskTableCursor{
		SortValue:    &cursorValue,
		RowCreatedAt: "2026-01-01T00:00:00Z",
		RowID:        "00000000-0000-4000-8000-000000000001",
	}
	args := make([]any, 0, 3)
	predicate, ok := (resolvedTaskTableSort{
		expression: "i.position",
		direction:  "asc",
		castType:   "double precision",
	}).cursorPredicate(httptest.NewRecorder(), &cursor, func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	})
	if !ok {
		t.Fatal("valid position cursor was rejected")
	}
	if !strings.Contains(predicate, "i.position >= $3::double precision") {
		t.Fatalf("position cursor is missing its indexable lower bound: %s", predicate)
	}
}

func TestTaskTableGroupIdentityBindsIncludeEmpty(t *testing.T) {
	withoutEmpty := taskTableGroupIdentity(taskTableGroupSpec{
		Kind:       "property",
		PropertyID: "00000000-0000-4000-8000-000000000001",
	})
	withEmpty := taskTableGroupIdentity(taskTableGroupSpec{
		Kind:         "property",
		PropertyID:   "00000000-0000-4000-8000-000000000001",
		IncludeEmpty: true,
	})
	if withoutEmpty == withEmpty {
		t.Fatalf("include-empty property cursors share an identity: %q", withEmpty)
	}
}

func TestTaskTableCompoundCellKeyResolvesPrimaryAndStatus(t *testing.T) {
	primary := resolvedTaskTableGroup{kind: "parent"}
	compound := resolvedTaskTableGroup{kind: "compound", primary: &primary}
	key := compoundCellGroupKey(
		"parent:00000000-0000-4000-8000-000000000001",
		"todo",
	)
	args := make([]any, 0, 2)
	predicate, ok := compound.predicate(
		httptest.NewRecorder(),
		key,
		func(value any) string {
			args = append(args, value)
			return fmt.Sprintf("$%d", len(args))
		},
	)
	if !ok {
		t.Fatal("valid compound cell key was rejected")
	}
	if !strings.Contains(predicate, "i.parent_task_id = $1::uuid") ||
		!strings.Contains(predicate, "i.status = $2::text") ||
		len(args) != 2 || args[1] != "todo" {
		t.Fatalf("compound cell predicate lost a dimension: %s args=%#v", predicate, args)
	}
}

func TestTaskTableRowsCommitsBeforeBestEffortEnrichment(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Table enrichment %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var taskNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 1
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&taskNumber); err != nil {
		t.Fatalf("reserve task number: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		VALUES ($1, 'table-enrichment', 'todo', 'none', 'member', $2, 1, $3, $4)
	`, testWorkspaceID, testUserID, taskNumber, projectID); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	labelCalls := 0
	tableQueryCalls := 0
	rowQuerySQL := ""
	handler := *testHandler
	handler.TxStarter = taskTableEnrichmentFailTxStarter{
		inner:           testHandler.TxStarter,
		labelCalls:      &labelCalls,
		tableQueryCalls: &tableQueryCalls,
		rowQuerySQL:     &rowQuerySQL,
	}
	recorder := httptest.NewRecorder()
	handler.ListTaskTableRows(recorder, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
		Query: taskTableQuerySpec{
			Scope: taskTableScope{Kind: "project", ProjectID: projectID},
			Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group:     taskTableGroupSpec{Kind: "none"},
		Hierarchy: taskTableHierarchyRequest{Enabled: false},
		Page:      taskTablePageRequest{Limit: 10},
	}))

	if recorder.Code != http.StatusOK {
		t.Fatalf("rows status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if labelCalls != 0 {
		t.Fatalf("best-effort labels ran inside snapshot transaction %d times", labelCalls)
	}
	var response taskTableRowsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if len(response.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(response.Rows))
	}
	if response.Total != 1 || response.BranchTotal != 1 {
		t.Fatalf("unexpected root counts: total=%d branch_total=%d", response.Total, response.BranchTotal)
	}
	if tableQueryCalls != 2 {
		t.Fatalf("ungrouped root head executed %d table queries, want 2", tableQueryCalls)
	}
	if !strings.Contains(rowQuerySQL, "WITH page AS MATERIALIZED") ||
		strings.Contains(rowQuerySQL, "membership AS") ||
		strings.Contains(rowQuerySQL, "FROM membership child") {
		t.Fatalf("flat rows query must page directly without hierarchy work:\n%s", rowQuerySQL)
	}
}

func TestTaskTableStatusGroupingOverOneThousandRows(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Server table grouping %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 1001
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve task numbers: %v", err)
	}
	firstNumber := finalNumber - 1000
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		SELECT $1, 'server-table-' || n::text,
		       CASE WHEN n <= 501 THEN 'todo' ELSE 'done' END,
		       'none', 'member', $2, n::double precision,
		       $3 + n - 1, $4
		FROM generate_series(1, 1001) AS n
	`, testWorkspaceID, testUserID, firstNumber, projectID); err != nil {
		t.Fatalf("seed tasks: %v", err)
	}

	query := taskTableQuerySpec{
		Scope:   taskTableScope{Kind: "project", ProjectID: projectID},
		Filters: taskTableFiltersRequest{},
		Sort:    taskTableSortRequest{Field: "title", Direction: "asc"},
	}
	w := httptest.NewRecorder()
	testHandler.ListTaskTableGroups(w, newRequest("POST", "/api/tasks/table/groups", taskTableGroupsRequest{
		Query: query,
		Group: taskTableGroupSpec{Kind: "status"},
		Page:  taskTablePageRequest{Limit: 100},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("groups status = %d: %s", w.Code, w.Body.String())
	}
	var groups taskTableGroupsResponse
	if err := json.NewDecoder(w.Body).Decode(&groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if groups.Total != 1001 {
		t.Fatalf("total = %d, want 1001", groups.Total)
	}
	counts := map[string]int64{}
	for _, group := range groups.Groups {
		counts[group.Key] = group.Count
	}
	if counts["status:todo"] != 501 || counts["status:done"] != 500 {
		t.Fatalf("unexpected group counts: %#v", counts)
	}
	firstGroupPageRecorder := httptest.NewRecorder()
	testHandler.ListTaskTableGroups(firstGroupPageRecorder, newRequest("POST", "/api/tasks/table/groups", taskTableGroupsRequest{
		Query: query,
		Group: taskTableGroupSpec{Kind: "status"},
		Page:  taskTablePageRequest{Limit: 1},
	}))
	var firstGroupPage taskTableGroupsResponse
	if firstGroupPageRecorder.Code != http.StatusOK {
		t.Fatalf("first group page status = %d: %s", firstGroupPageRecorder.Code, firstGroupPageRecorder.Body.String())
	}
	if err := json.NewDecoder(firstGroupPageRecorder.Body).Decode(&firstGroupPage); err != nil {
		t.Fatalf("decode first group page: %v", err)
	}
	if len(firstGroupPage.Groups) != 1 || firstGroupPage.NextCursor == nil {
		t.Fatalf("unexpected first group page: %#v", firstGroupPage)
	}
	secondGroupPageRecorder := httptest.NewRecorder()
	testHandler.ListTaskTableGroups(secondGroupPageRecorder, newRequest("POST", "/api/tasks/table/groups", taskTableGroupsRequest{
		Query: query,
		Group: taskTableGroupSpec{Kind: "status"},
		Page:  taskTablePageRequest{Limit: 1, Cursor: firstGroupPage.NextCursor},
	}))
	var secondGroupPage taskTableGroupsResponse
	if secondGroupPageRecorder.Code != http.StatusOK {
		t.Fatalf("second group page status = %d: %s", secondGroupPageRecorder.Code, secondGroupPageRecorder.Body.String())
	}
	if err := json.NewDecoder(secondGroupPageRecorder.Body).Decode(&secondGroupPage); err != nil {
		t.Fatalf("decode second group page: %v", err)
	}
	if len(secondGroupPage.Groups) != 1 || secondGroupPage.Groups[0].Key == firstGroupPage.Groups[0].Key || secondGroupPage.Total != 1001 {
		t.Fatalf("group keyset pagination mismatch: first=%#v second=%#v", firstGroupPage, secondGroupPage)
	}

	groupKey := "status:todo"
	labelCalls := 0
	tableQueryCalls := 0
	rowsHandler := *testHandler
	rowsHandler.TxStarter = taskTableEnrichmentFailTxStarter{
		inner:           testHandler.TxStarter,
		labelCalls:      &labelCalls,
		tableQueryCalls: &tableQueryCalls,
	}
	rowsRecorder := httptest.NewRecorder()
	rowsHandler.ListTaskTableRows(rowsRecorder, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
		Query:     query,
		Group:     taskTableGroupSpec{Kind: "status"},
		GroupKey:  &groupKey,
		Hierarchy: taskTableHierarchyRequest{Enabled: false},
		Page:      taskTablePageRequest{Limit: 50},
	}))
	if rowsRecorder.Code != http.StatusOK {
		t.Fatalf("rows status = %d: %s", rowsRecorder.Code, rowsRecorder.Body.String())
	}
	var rows taskTableRowsResponse
	if err := json.NewDecoder(rowsRecorder.Body).Decode(&rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	if rows.Total != 0 || rows.BranchTotal != 50 || len(rows.Rows) != 50 || rows.NextCursor == nil {
		t.Fatalf("unexpected rows page: total=%d branch_total=%d rows=%d cursor=%v", rows.Total, rows.BranchTotal, len(rows.Rows), rows.NextCursor)
	}
	if tableQueryCalls != 1 {
		t.Fatalf("grouped root head executed %d table queries, want 1", tableQueryCalls)
	}
	firstPageIDs := make(map[string]struct{}, len(rows.Rows))
	for _, row := range rows.Rows {
		firstPageIDs[row.Task.ID] = struct{}{}
	}
	secondRowsRecorder := httptest.NewRecorder()
	rowsHandler.ListTaskTableRows(secondRowsRecorder, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
		Query:     query,
		Group:     taskTableGroupSpec{Kind: "status"},
		GroupKey:  &groupKey,
		Hierarchy: taskTableHierarchyRequest{Enabled: false},
		Page:      taskTablePageRequest{Limit: 50, Cursor: rows.NextCursor},
	}))
	if secondRowsRecorder.Code != http.StatusOK {
		t.Fatalf("second rows status = %d: %s", secondRowsRecorder.Code, secondRowsRecorder.Body.String())
	}
	var secondRows taskTableRowsResponse
	if err := json.NewDecoder(secondRowsRecorder.Body).Decode(&secondRows); err != nil {
		t.Fatalf("decode second rows: %v", err)
	}
	if secondRows.Total != 0 || secondRows.BranchTotal != 50 || len(secondRows.Rows) != 50 {
		t.Fatalf("unexpected grouped continuation: total=%d branch_total=%d rows=%d", secondRows.Total, secondRows.BranchTotal, len(secondRows.Rows))
	}
	if tableQueryCalls != 2 {
		t.Fatalf("grouped continuation executed %d cumulative table queries, want 2", tableQueryCalls)
	}
	for _, row := range secondRows.Rows {
		if _, duplicate := firstPageIDs[row.Task.ID]; duplicate {
			t.Fatalf("keyset cursor repeated task %s across pages", row.Task.ID)
		}
	}

	ungroupedRecorder := httptest.NewRecorder()
	rowsHandler.ListTaskTableRows(ungroupedRecorder, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
		Query:     query,
		Group:     taskTableGroupSpec{Kind: "none"},
		Hierarchy: taskTableHierarchyRequest{Enabled: false},
		Page:      taskTablePageRequest{Limit: 50},
	}))
	if ungroupedRecorder.Code != http.StatusOK {
		t.Fatalf("ungrouped rows status = %d: %s", ungroupedRecorder.Code, ungroupedRecorder.Body.String())
	}
	var ungroupedRows taskTableRowsResponse
	if err := json.NewDecoder(ungroupedRecorder.Body).Decode(&ungroupedRows); err != nil {
		t.Fatalf("decode ungrouped rows: %v", err)
	}
	if ungroupedRows.Total != 1001 || ungroupedRows.BranchTotal != 50 || len(ungroupedRows.Rows) != 50 || ungroupedRows.NextCursor == nil {
		t.Fatalf("unexpected ungrouped root head: total=%d branch_total=%d rows=%d cursor=%v", ungroupedRows.Total, ungroupedRows.BranchTotal, len(ungroupedRows.Rows), ungroupedRows.NextCursor)
	}
	if tableQueryCalls != 4 {
		t.Fatalf("ungrouped root head executed %d cumulative table queries, want 4", tableQueryCalls)
	}

	ungroupedNextRecorder := httptest.NewRecorder()
	rowsHandler.ListTaskTableRows(ungroupedNextRecorder, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
		Query:     query,
		Group:     taskTableGroupSpec{Kind: "none"},
		Hierarchy: taskTableHierarchyRequest{Enabled: false},
		Page:      taskTablePageRequest{Limit: 50, Cursor: ungroupedRows.NextCursor},
	}))
	if ungroupedNextRecorder.Code != http.StatusOK {
		t.Fatalf("ungrouped continuation status = %d: %s", ungroupedNextRecorder.Code, ungroupedNextRecorder.Body.String())
	}
	var ungroupedNext taskTableRowsResponse
	if err := json.NewDecoder(ungroupedNextRecorder.Body).Decode(&ungroupedNext); err != nil {
		t.Fatalf("decode ungrouped continuation: %v", err)
	}
	if ungroupedNext.Total != 0 || ungroupedNext.BranchTotal != 50 || len(ungroupedNext.Rows) != 50 {
		t.Fatalf("unexpected ungrouped continuation: total=%d branch_total=%d rows=%d", ungroupedNext.Total, ungroupedNext.BranchTotal, len(ungroupedNext.Rows))
	}
	if tableQueryCalls != 5 {
		t.Fatalf("ungrouped continuation executed %d cumulative table queries, want 5", tableQueryCalls)
	}

	for _, sortCase := range []taskTableSortRequest{
		{Field: "status", Direction: "desc"},
		{Field: "created_at", Direction: "desc"},
		{Field: "due_date", Direction: "asc"},
	} {
		sortQuery := query
		sortQuery.Sort = sortCase
		fetchPage := func(cursor *string) taskTableRowsResponse {
			t.Helper()
			recorder := httptest.NewRecorder()
			testHandler.ListTaskTableRows(recorder, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
				Query:     sortQuery,
				Group:     taskTableGroupSpec{Kind: "status"},
				GroupKey:  &groupKey,
				Hierarchy: taskTableHierarchyRequest{Enabled: false},
				Page:      taskTablePageRequest{Limit: 10, Cursor: cursor},
			}))
			if recorder.Code != http.StatusOK {
				t.Fatalf("%s cursor page status = %d: %s", sortCase.Field, recorder.Code, recorder.Body.String())
			}
			var response taskTableRowsResponse
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode %s cursor page: %v", sortCase.Field, err)
			}
			return response
		}
		first := fetchPage(nil)
		if len(first.Rows) != 10 || first.NextCursor == nil {
			t.Fatalf("%s first cursor page is incomplete", sortCase.Field)
		}
		seen := make(map[string]struct{}, len(first.Rows))
		for _, row := range first.Rows {
			seen[row.Task.ID] = struct{}{}
		}
		second := fetchPage(first.NextCursor)
		if len(second.Rows) != 10 {
			t.Fatalf("%s second cursor page length = %d", sortCase.Field, len(second.Rows))
		}
		for _, row := range second.Rows {
			if _, duplicate := seen[row.Task.ID]; duplicate {
				t.Fatalf("%s keyset cursor repeated task %s", sortCase.Field, row.Task.ID)
			}
		}
	}

	filteredQuery := query
	filteredQuery.Filters.Statuses = []string{"todo"}
	facetsRecorder := httptest.NewRecorder()
	testHandler.ListTaskTableFacets(facetsRecorder, newRequest("POST", "/api/tasks/table/facets", taskTableFacetsRequest{
		Query:  filteredQuery,
		Facets: []taskTableFacetSpec{{Kind: "status"}},
	}))
	if facetsRecorder.Code != http.StatusOK {
		t.Fatalf("facets status = %d: %s", facetsRecorder.Code, facetsRecorder.Body.String())
	}
	var facets taskTableFacetsResponse
	if err := json.NewDecoder(facetsRecorder.Body).Decode(&facets); err != nil {
		t.Fatalf("decode facets: %v", err)
	}
	if facets.Total != 501 || len(facets.Facets) != 1 {
		t.Fatalf("unexpected filtered facet response: total=%d facets=%d", facets.Total, len(facets.Facets))
	}
	facetCounts := map[string]int64{}
	for _, value := range facets.Facets[0].Values {
		facetCounts[value.Key] = value.Count
	}
	if facetCounts["todo"] != 501 || facetCounts["done"] != 500 {
		t.Fatalf("status facet must ignore its own active filter: %#v", facetCounts)
	}

	includeTotal := false
	facetCountQueries := 0
	facetsHandler := *testHandler
	facetsHandler.TxStarter = taskTableEnrichmentFailTxStarter{
		inner:           testHandler.TxStarter,
		tableQueryCalls: &facetCountQueries,
	}
	noTotalRecorder := httptest.NewRecorder()
	facetsHandler.ListTaskTableFacets(noTotalRecorder, newRequest("POST", "/api/tasks/table/facets", taskTableFacetsRequest{
		Query:        filteredQuery,
		Facets:       []taskTableFacetSpec{{Kind: "status"}},
		IncludeTotal: &includeTotal,
	}))
	if noTotalRecorder.Code != http.StatusOK {
		t.Fatalf("facets without total status = %d: %s", noTotalRecorder.Code, noTotalRecorder.Body.String())
	}
	var noTotal taskTableFacetsResponse
	if err := json.NewDecoder(noTotalRecorder.Body).Decode(&noTotal); err != nil {
		t.Fatalf("decode facets without total: %v", err)
	}
	if noTotal.Total != 0 || len(noTotal.Facets) != 1 {
		t.Fatalf("unexpected facets without total: %#v", noTotal)
	}
	if facetCountQueries != 0 {
		t.Fatalf("include_total=false executed %d total count queries, want 0", facetCountQueries)
	}

	batchFacetQueries := 0
	batchTotalQueries := 0
	batchHandler := *testHandler
	batchHandler.TxStarter = taskTableEnrichmentFailTxStarter{
		inner:           testHandler.TxStarter,
		facetQueryCalls: &batchFacetQueries,
		tableQueryCalls: &batchTotalQueries,
	}
	batchRecorder := httptest.NewRecorder()
	batchHandler.ListTaskTableFacets(batchRecorder, newRequest("POST", "/api/tasks/table/facets", taskTableFacetsRequest{
		Query: query,
		Facets: []taskTableFacetSpec{
			{Kind: "status"},
			{Kind: "priority"},
			{Kind: "assignee"},
			{Kind: "creator"},
			{Kind: "project"},
		},
	}))
	if batchRecorder.Code != http.StatusOK {
		t.Fatalf("batch facets status = %d: %s", batchRecorder.Code, batchRecorder.Body.String())
	}
	var batchResponse taskTableFacetsResponse
	if err := json.NewDecoder(batchRecorder.Body).Decode(&batchResponse); err != nil {
		t.Fatalf("decode batch facets: %v", err)
	}
	if batchResponse.Total != 1001 || len(batchResponse.Facets) != 5 {
		t.Fatalf("unexpected batch facets response: total=%d facets=%d", batchResponse.Total, len(batchResponse.Facets))
	}
	if batchFacetQueries != 1 || batchTotalQueries != 0 {
		t.Fatalf("batch facets executed grouping queries=%d total queries=%d, want 1 and 0", batchFacetQueries, batchTotalQueries)
	}
	batchCounts := make(map[string]map[string]int64, len(batchResponse.Facets))
	for _, facet := range batchResponse.Facets {
		counts := make(map[string]int64, len(facet.Values))
		for _, value := range facet.Values {
			counts[value.Key] = value.Count
		}
		batchCounts[facet.Kind] = counts
	}
	if batchCounts["status"]["todo"] != 501 || batchCounts["status"]["done"] != 500 ||
		batchCounts["priority"]["none"] != 1001 ||
		batchCounts["assignee"]["__none__"] != 1001 ||
		batchCounts["creator"]["member:"+testUserID] != 1001 ||
		batchCounts["project"][projectID] != 1001 {
		t.Fatalf("unexpected batched facet counts: %#v", batchCounts)
	}

	mixedRecorder := httptest.NewRecorder()
	testHandler.ListTaskTableFacets(mixedRecorder, newRequest("POST", "/api/tasks/table/facets", taskTableFacetsRequest{
		Query:  filteredQuery,
		Facets: []taskTableFacetSpec{{Kind: "status"}, {Kind: "priority"}},
	}))
	if mixedRecorder.Code != http.StatusOK {
		t.Fatalf("mixed facets status = %d: %s", mixedRecorder.Code, mixedRecorder.Body.String())
	}
	var mixedResponse taskTableFacetsResponse
	if err := json.NewDecoder(mixedRecorder.Body).Decode(&mixedResponse); err != nil {
		t.Fatalf("decode mixed facets: %v", err)
	}
	if mixedResponse.Total != 501 || len(mixedResponse.Facets) != 2 ||
		mixedResponse.Facets[0].Kind != "status" || mixedResponse.Facets[1].Kind != "priority" {
		t.Fatalf("unexpected mixed facets response: %#v", mixedResponse)
	}
	mixedCounts := make(map[string]map[string]int64, len(mixedResponse.Facets))
	for _, facet := range mixedResponse.Facets {
		counts := make(map[string]int64, len(facet.Values))
		for _, value := range facet.Values {
			counts[value.Key] = value.Count
		}
		mixedCounts[facet.Kind] = counts
	}
	if mixedCounts["status"]["todo"] != 501 || mixedCounts["status"]["done"] != 500 ||
		mixedCounts["priority"]["none"] != 501 {
		t.Fatalf("mixed facets lost disjunctive semantics: %#v", mixedCounts)
	}
}

func TestTaskTableAssigneeNamesResolveAfterGrouping(t *testing.T) {
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Server table assignee grouping')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 2
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve task numbers: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, assignee_type, assignee_id,
			creator_type, creator_id, position, number, project_id
		)
		VALUES
			($1, 'Assigned row', 'todo', 'none', 'member', $2, 'member', $2, 1, $3, $4),
			($1, 'Unassigned row', 'todo', 'none', NULL, NULL, 'member', $2, 2, $3 + 1, $4)
	`, testWorkspaceID, testUserID, finalNumber-1, projectID); err != nil {
		t.Fatalf("seed tasks: %v", err)
	}

	groupQuerySQL := ""
	handler := *testHandler
	handler.TxStarter = taskTableEnrichmentFailTxStarter{
		inner:         testHandler.TxStarter,
		groupQuerySQL: &groupQuerySQL,
	}
	recorder := httptest.NewRecorder()
	handler.ListTaskTableGroups(recorder, newRequest("POST", "/api/tasks/table/groups", taskTableGroupsRequest{
		Query: taskTableQuerySpec{
			Scope: taskTableScope{Kind: "project", ProjectID: projectID},
			Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: taskTableGroupSpec{Kind: "assignee"},
		Page:  taskTablePageRequest{Limit: 10},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("groups status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response taskTableGroupsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	counts := make(map[string]int64, len(response.Groups))
	for _, group := range response.Groups {
		counts[group.Key] = group.Count
	}
	if counts["assignee:member:"+testUserID] != 1 || counts["assignee:unassigned"] != 1 {
		t.Fatalf("unexpected assignee groups: %#v", counts)
	}
	if response.Groups[0].Key != "assignee:member:"+testUserID ||
		response.Groups[len(response.Groups)-1].Key != "assignee:unassigned" {
		t.Fatalf("assignee groups are not actor-priority ordered: %#v", response.Groups)
	}
	sortedAt := strings.Index(groupQuerySQL, "), sorted AS (")
	nameLookupAt := strings.Index(groupQuerySQL, `SELECT u.name FROM "user" u`)
	if sortedAt < 0 || nameLookupAt < sortedAt {
		t.Fatalf("assignee names must resolve after actor aggregation:\n%s", groupQuerySQL)
	}
}

func TestTaskTableCompoundParentGroupsReturnExactStatusCells(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Compound parent grouping %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 4
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve task numbers: %v", err)
	}
	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		VALUES ($1, 'Parent context', 'done', 'none', 'member', $2, 1, $3, $4)
		RETURNING id
	`, testWorkspaceID, testUserID, finalNumber-3, projectID).Scan(&parentID); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			parent_task_id, position, number, project_id
		)
		VALUES
			($1, 'Child todo', 'todo', 'none', 'member', $2, $3, 2, $4, $5),
			($1, 'Child review', 'in_review', 'none', 'member', $2, $3, 3, $4 + 1, $5),
			($1, 'No parent', 'todo', 'none', 'member', $2, NULL, 4, $4 + 2, $5)
	`, testWorkspaceID, testUserID, parentID, finalNumber-2, projectID); err != nil {
		t.Fatalf("seed grouped tasks: %v", err)
	}

	recorder := httptest.NewRecorder()
	testHandler.ListTaskTableGroups(recorder, newRequest("POST", "/api/tasks/table/groups", taskTableGroupsRequest{
		Query: taskTableQuerySpec{
			Scope: taskTableScope{Kind: "project", ProjectID: projectID},
			Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: taskTableGroupSpec{Kind: "compound", Primary: "parent", Secondary: "status"},
		Page:  taskTablePageRequest{Limit: 10},
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("compound groups status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response taskTableGroupsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode compound groups: %v", err)
	}
	if response.Total != 4 || len(response.Groups) != 2 {
		t.Fatalf("unexpected compound group totals: %#v", response)
	}
	byKey := make(map[string]taskTableGroupDescriptorResponse, len(response.Groups))
	for _, group := range response.Groups {
		byKey[group.Key] = group
	}
	parent := byKey["parent:"+parentID]
	if parent.Count != 2 || parent.Value.Parent == nil ||
		parent.Value.Parent.Title != "Parent context" ||
		parent.Value.ValueState != "value" {
		t.Fatalf("parent descriptor lost context: %#v", parent)
	}
	parentCells := make(map[string]int64, len(parent.SecondaryGroups))
	for _, cell := range parent.SecondaryGroups {
		parentCells[cell.Value.Status] = cell.Count
		if !strings.HasPrefix(cell.Key, "compound:") {
			t.Fatalf("cell key is not opaque compound key: %q", cell.Key)
		}
	}
	if parentCells["todo"] != 1 || parentCells["in_review"] != 1 {
		t.Fatalf("unexpected parent status cells: %#v", parentCells)
	}
	noParent := byKey["parent:none"]
	if noParent.Count != 2 || noParent.Value.ValueState != "unset" {
		t.Fatalf("unexpected no-parent lane: %#v", noParent)
	}

	// A second parent has only a hidden-status child. Under a visible-status
	// compound query it must stay a No-parent card instead of creating an
	// empty lane. The first parent does have a visible child, so it is promoted
	// to a lane header and must disappear from the No-parent rows and counts.
	var hiddenParentNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = task_counter + 2
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&hiddenParentNumber); err != nil {
		t.Fatalf("reserve hidden-parent task numbers: %v", err)
	}
	var hiddenParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		VALUES ($1, 'Visible parent card', 'todo', 'none', 'member', $2, 5, $3, $4)
		RETURNING id
	`, testWorkspaceID, testUserID, hiddenParentNumber-1, projectID).Scan(&hiddenParentID); err != nil {
		t.Fatalf("create hidden-only parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			parent_task_id, position, number, project_id
		)
		VALUES ($1, 'Hidden child', 'done', 'none', 'member', $2, $3, 6, $4, $5)
	`, testWorkspaceID, testUserID, hiddenParentID, hiddenParentNumber, projectID); err != nil {
		t.Fatalf("create hidden child: %v", err)
	}

	filteredGroup := taskTableGroupSpec{
		Kind:            "compound",
		Primary:         "parent",
		Secondary:       "status",
		SecondaryValues: []string{"todo"},
	}
	filteredRecorder := httptest.NewRecorder()
	testHandler.ListTaskTableGroups(filteredRecorder, newRequest("POST", "/api/tasks/table/groups", taskTableGroupsRequest{
		Query: taskTableQuerySpec{
			Scope: taskTableScope{Kind: "project", ProjectID: projectID},
			Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: filteredGroup,
		Page:  taskTablePageRequest{Limit: 10},
	}))
	if filteredRecorder.Code != http.StatusOK {
		t.Fatalf("filtered compound groups status = %d: %s", filteredRecorder.Code, filteredRecorder.Body.String())
	}
	var filtered taskTableGroupsResponse
	if err := json.NewDecoder(filteredRecorder.Body).Decode(&filtered); err != nil {
		t.Fatalf("decode filtered compound groups: %v", err)
	}
	if filtered.Total != 3 || len(filtered.Groups) != 2 {
		t.Fatalf("unexpected visible compound groups: %#v", filtered)
	}
	filteredByKey := make(map[string]taskTableGroupDescriptorResponse, len(filtered.Groups))
	for _, descriptor := range filtered.Groups {
		filteredByKey[descriptor.Key] = descriptor
	}
	if _, exists := filteredByKey["parent:"+hiddenParentID]; exists {
		t.Fatalf("hidden-only parent lane was returned: %#v", filtered.Groups)
	}
	filteredParent := filteredByKey["parent:"+parentID]
	filteredNoParent := filteredByKey["parent:none"]
	if filteredParent.Count != 2 || filteredNoParent.Count != 2 {
		t.Fatalf("parent header was not removed from aligned counts: parent=%#v no-parent=%#v", filteredParent, filteredNoParent)
	}

	cellKey := func(descriptor taskTableGroupDescriptorResponse, status string) string {
		t.Helper()
		for _, cell := range descriptor.SecondaryGroups {
			if cell.Value.Status == status {
				return cell.Key
			}
		}
		t.Fatalf("missing %s cell in %#v", status, descriptor)
		return ""
	}
	listCell := func(key string) taskTableRowsResponse {
		t.Helper()
		rowsRecorder := httptest.NewRecorder()
		testHandler.ListTaskTableRows(rowsRecorder, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
			Query: taskTableQuerySpec{
				Scope: taskTableScope{Kind: "project", ProjectID: projectID},
				Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
			},
			Group:     filteredGroup,
			GroupKey:  &key,
			Hierarchy: taskTableHierarchyRequest{Enabled: false},
			Page:      taskTablePageRequest{Limit: 10},
		}))
		if rowsRecorder.Code != http.StatusOK {
			t.Fatalf("filtered compound rows status = %d: %s", rowsRecorder.Code, rowsRecorder.Body.String())
		}
		var result taskTableRowsResponse
		if err := json.NewDecoder(rowsRecorder.Body).Decode(&result); err != nil {
			t.Fatalf("decode filtered compound rows: %v", err)
		}
		return result
	}
	noParentTodo := listCell(cellKey(filteredNoParent, "todo"))
	if len(noParentTodo.Rows) != 2 {
		t.Fatalf("unexpected visible no-parent rows: %#v", noParentTodo.Rows)
	}
	noParentIDs := map[string]bool{}
	for _, row := range noParentTodo.Rows {
		noParentIDs[row.Task.ID] = true
	}
	if noParentIDs[parentID] || !noParentIDs[hiddenParentID] {
		t.Fatalf("parent header/card semantics diverged: %#v", noParentIDs)
	}
	noParentDone := listCell(cellKey(filteredNoParent, "done"))
	if len(noParentDone.Rows) != 0 {
		t.Fatalf("promoted parent remained in No-parent rows: %#v", noParentDone.Rows)
	}
}

func TestTaskTableHierarchyDoesNotCrossGroups(t *testing.T) {
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Server table cross-group hierarchy')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 2
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve task numbers: %v", err)
	}
	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		VALUES ($1, 'Todo parent', 'todo', 'none', 'member', $2, 1, $3, $4)
		RETURNING id
	`, testWorkspaceID, testUserID, finalNumber-1, projectID).Scan(&parentID); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	var childID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			parent_task_id, position, number, project_id
		)
		VALUES ($1, 'Done child', 'done', 'none', 'member', $2, $3, 2, $4, $5)
		RETURNING id
	`, testWorkspaceID, testUserID, parentID, finalNumber, projectID).Scan(&childID); err != nil {
		t.Fatalf("create child: %v", err)
	}

	query := taskTableQuerySpec{
		Scope:   taskTableScope{Kind: "project", ProjectID: projectID},
		Filters: taskTableFiltersRequest{},
		Sort:    taskTableSortRequest{Field: "position", Direction: "asc"},
	}
	rowQuerySQL := ""
	rowsHandler := *testHandler
	rowsHandler.TxStarter = taskTableEnrichmentFailTxStarter{
		inner:       testHandler.TxStarter,
		rowQuerySQL: &rowQuerySQL,
	}
	listGroup := func(groupKey string) taskTableRowsResponse {
		t.Helper()
		w := httptest.NewRecorder()
		rowsHandler.ListTaskTableRows(w, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
			Query:     query,
			Group:     taskTableGroupSpec{Kind: "status"},
			GroupKey:  &groupKey,
			Hierarchy: taskTableHierarchyRequest{Enabled: true},
			Page:      taskTablePageRequest{Limit: 50},
		}))
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: status=%d body=%s", groupKey, w.Code, w.Body.String())
		}
		var response taskTableRowsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode %s: %v", groupKey, err)
		}
		return response
	}

	doneRows := listGroup("status:done")
	if len(doneRows.Rows) != 1 || doneRows.Rows[0].Task.ID != childID {
		t.Fatalf("done child must become a root in its own group: %#v", doneRows.Rows)
	}
	todoRows := listGroup("status:todo")
	if len(todoRows.Rows) != 1 || todoRows.Rows[0].Task.ID != parentID {
		t.Fatalf("todo group root mismatch: %#v", todoRows.Rows)
	}
	if todoRows.Rows[0].DirectChildCount != 0 {
		t.Fatalf("cross-group child leaked into todo parent count: %d", todoRows.Rows[0].DirectChildCount)
	}
	if !strings.Contains(rowQuerySQL, "membership AS NOT MATERIALIZED") ||
		!strings.Contains(rowQuerySQL, "page AS MATERIALIZED") ||
		!strings.Contains(rowQuerySQL, "(SELECT parent.id FROM membership parent") ||
		strings.Contains(rowQuerySQL, "NOT EXISTS (SELECT 1 FROM membership parent") ||
		strings.Contains(rowQuerySQL, "child_counts AS") {
		t.Fatalf("hierarchy rows query must preserve ordered paging and page-local counts:\n%s", rowQuerySQL)
	}
}

func TestTaskTableHierarchyRootKeysetPagination(t *testing.T) {
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Server table hierarchy pagination')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 3
		WHERE id = $1
		RETURNING task_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve task numbers: %v", err)
	}
	rows, err := testPool.Query(ctx, `
		INSERT INTO task (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, project_id
		)
		SELECT $1, 'Hierarchy root ' || n::text, 'todo', 'none', 'member', $2,
		       n::double precision, $3 + n - 1, $4
		FROM generate_series(1, 3) AS n
		ORDER BY n
		RETURNING id
	`, testWorkspaceID, testUserID, finalNumber-2, projectID)
	if err != nil {
		t.Fatalf("seed hierarchy roots: %v", err)
	}
	expectedIDs := make(map[string]struct{}, 3)
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			rows.Close()
			t.Fatalf("scan hierarchy root: %v", err)
		}
		expectedIDs[taskID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("seed hierarchy roots: %v", err)
	}
	rows.Close()

	query := taskTableQuerySpec{
		Scope: taskTableScope{Kind: "project", ProjectID: projectID},
		Sort:  taskTableSortRequest{Field: "position", Direction: "asc"},
	}
	fetchPage := func(cursor *string) taskTableRowsResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListTaskTableRows(w, newRequest("POST", "/api/tasks/table/rows", taskTableRowsRequest{
			Query:     query,
			Group:     taskTableGroupSpec{Kind: "none"},
			Hierarchy: taskTableHierarchyRequest{Enabled: true},
			Page:      taskTablePageRequest{Limit: 2, Cursor: cursor},
		}))
		if w.Code != http.StatusOK {
			t.Fatalf("list hierarchy roots: status=%d body=%s", w.Code, w.Body.String())
		}
		var response taskTableRowsResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode hierarchy roots: %v", err)
		}
		return response
	}

	first := fetchPage(nil)
	if first.Total != 3 || len(first.Rows) != 2 || first.NextCursor == nil {
		t.Fatalf("unexpected first hierarchy page: total=%d rows=%d cursor=%v", first.Total, len(first.Rows), first.NextCursor)
	}
	second := fetchPage(first.NextCursor)
	if second.Total != 0 || len(second.Rows) != 1 || second.NextCursor != nil {
		t.Fatalf("unexpected second hierarchy page: total=%d rows=%d cursor=%v", second.Total, len(second.Rows), second.NextCursor)
	}

	seenIDs := make(map[string]struct{}, 3)
	for _, page := range []taskTableRowsResponse{first, second} {
		for _, row := range page.Rows {
			if _, duplicate := seenIDs[row.Task.ID]; duplicate {
				t.Fatalf("hierarchy root %s repeated across pages", row.Task.ID)
			}
			seenIDs[row.Task.ID] = struct{}{}
		}
	}
	if len(seenIDs) != len(expectedIDs) {
		t.Fatalf("reachable hierarchy roots = %d, want %d", len(seenIDs), len(expectedIDs))
	}
	for taskID := range expectedIDs {
		if _, ok := seenIDs[taskID]; !ok {
			t.Fatalf("hierarchy root %s was not reachable through pagination", taskID)
		}
	}
}
