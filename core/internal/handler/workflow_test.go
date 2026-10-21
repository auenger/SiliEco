package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func workflowRequest(method, path string, body any, params map[string]string) *http.Request {
	req := newRequest(method, path, body)
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func workflowStateTokenForTest(t *testing.T, id string) string {
	t.Helper()
	row, err := testHandler.Queries.GetWorkflowInstanceInWorkspace(context.Background(), db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: parseUUID(testWorkspaceID), ID: parseUUID(id)})
	if err != nil {
		t.Fatal(err)
	}
	return workflowInstanceStateToken(row)
}

func cleanupWorkflowTestData(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_task_queue t USING workflow_stage_execution e WHERE t.workflow_stage_execution_id=e.id AND e.workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_review_recovery WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_rework WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_execution_action WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_artifact_revision WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_artifact WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_artifact_upload WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_execution_result WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_stage_execution WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(ctx, `UPDATE issue SET workflow_instance_id = NULL, workflow_stage_id = NULL, workflow_stage_execution_id = NULL, workflow_invalidated_by_rework_id = NULL WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_gate_decision WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_instance_change WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_instance_stage WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_instance WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_stage WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow_version WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workflow WHERE workspace_id = $1`, testWorkspaceID)
	})
}

func TestNormalizeWorkflowStagesValidatesRollbackAndGate(t *testing.T) {
	stages, err := normalizeWorkflowStages([]WorkflowStageInput{
		{Name: "Plan"},
		{
			Name:             "Review",
			Gate:             json.RawMessage(`{"type":"human"}`),
			RollbackStageKey: stringPtr("stage_1"),
		},
	})
	if err != nil {
		t.Fatalf("expected valid stages: %v", err)
	}
	if stages[0].StableKey != "stage_1" || stages[1].StableKey != "stage_2" {
		t.Fatalf("unexpected generated stable keys: %#v", stages)
	}

	_, err = normalizeWorkflowStages([]WorkflowStageInput{
		{Name: "Plan"},
		{Name: "Review", Gate: json.RawMessage(`{"type":"unknown"}`)},
	})
	if err == nil {
		t.Fatal("expected unknown gate type to be rejected")
	}

	_, err = normalizeWorkflowStages([]WorkflowStageInput{
		{Name: "Plan", RollbackStageKey: stringPtr("stage_2")},
		{Name: "Review"},
	})
	if err == nil {
		t.Fatal("expected forward rollback target to be rejected")
	}
}

func TestValidateWorkflowGateActorUsesConfiguredDecider(t *testing.T) {
	gate := workflowGateConfig{
		Type:         "human",
		Decider:      "member-1",
		HumanDecider: "member-1",
	}
	if err := validateWorkflowGateActor(gate, "member", "member-1"); err != nil {
		t.Fatalf("configured member should be allowed: %v", err)
	}
	if err := validateWorkflowGateActor(gate, "member", "member-2"); err == nil {
		t.Fatal("a different member must not be allowed through a specific human gate")
	}
	if err := validateWorkflowGateActor(workflowGateConfig{
		Type: "agent", Decider: "agent-1",
	}, "agent", "agent-2"); err == nil {
		t.Fatal("a different agent must not be allowed through a specific agent gate")
	}
}

func TestWorkflowInstancePlanCanAdjustCurrentAndEmptyFutureStages(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupWorkflowTestData(t)

	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Adjustable SOP project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	createWorkflow := httptest.NewRecorder()
	testHandler.CreateWorkflow(createWorkflow, newRequest(http.MethodPost, "/api/workflows", map[string]any{
		"name": "Adjustable SOP", "project_id": projectID, "publish": true,
		"stages": []map[string]any{
			{"name": "Plan", "required_skills": []string{"builtin:silieco-working-on-issues"}},
			{"name": "Build", "output_spec": map[string]any{"default_artifact": "build.md"}},
			{"name": "Unused review"},
		},
	}))
	if createWorkflow.Code != http.StatusCreated {
		t.Fatalf("create workflow: status=%d body=%s", createWorkflow.Code, createWorkflow.Body.String())
	}
	var workflow WorkflowResponse
	if err := json.Unmarshal(createWorkflow.Body.Bytes(), &workflow); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}

	createRun := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(createRun, workflowRequest(
		http.MethodPost,
		"/api/workflows/"+workflow.ID+"/instances",
		map[string]any{"title": "Original run", "start": true},
		map[string]string{"id": workflow.ID},
	))
	if createRun.Code != http.StatusCreated {
		t.Fatalf("create run: status=%d body=%s", createRun.Code, createRun.Body.String())
	}
	var run WorkflowInstanceResponse
	if err := json.Unmarshal(createRun.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if len(run.Stages) != 3 || run.Revision != 1 {
		t.Fatalf("unexpected initial run plan: %#v", run)
	}
	const historicalSnapshot = `[{"id":"builtin:silieco-working-on-issues","source":"builtin","name":"silieco-working-on-issues","content":"# Frozen earlier revision"}]`
	if _, err := testPool.Exec(context.Background(), `UPDATE workflow_instance_stage SET required_skill_snapshots=$2::jsonb WHERE id=$1`, run.Stages[0].ID, historicalSnapshot); err != nil {
		t.Fatal(err)
	}
	stages := append([]WorkflowInstanceStageResponse(nil), run.Stages[:2]...)
	stages[0].Name = "Plan and align"
	stages[1].OutputSpec = json.RawMessage(`{"default_artifact":"release.md"}`)

	adjust := httptest.NewRecorder()
	testHandler.UpdateWorkflowInstancePlan(adjust, workflowRequest(
		http.MethodPatch,
		"/api/workflow-instances/"+run.ID+"/plan",
		map[string]any{
			"expected_revision": 1,
			"title":             "Adjusted run",
			"change_note":       "Remove the unused review stage",
			"stages":            stages,
		},
		map[string]string{"id": run.ID},
	))
	if adjust.Code != http.StatusOK {
		t.Fatalf("adjust run: status=%d body=%s", adjust.Code, adjust.Body.String())
	}
	var snapshotPreserved bool
	if err := testPool.QueryRow(context.Background(), `SELECT required_skill_snapshots=$2::jsonb FROM workflow_instance_stage WHERE id=$1`, run.Stages[0].ID, historicalSnapshot).Scan(&snapshotPreserved); err != nil || !snapshotPreserved {
		t.Fatalf("plan edit hot-updated pinned skill content: %v %v", snapshotPreserved, err)
	}
	var adjusted WorkflowInstanceResponse
	if err := json.Unmarshal(adjust.Body.Bytes(), &adjusted); err != nil {
		t.Fatalf("decode adjusted run: %v", err)
	}
	if adjusted.Revision != 2 || adjusted.Title != "Adjusted run" || len(adjusted.Stages) != 2 {
		t.Fatalf("unexpected adjusted run: %#v", adjusted)
	}
	if adjusted.Stages[0].Name != "Plan and align" || string(adjusted.Stages[1].OutputSpec) != `{"default_artifact":"release.md"}` {
		t.Fatalf("stage adjustments were not preserved: %#v", adjusted.Stages)
	}
	if len(adjusted.Changes) != 1 || adjusted.Changes[0].Revision != 2 {
		t.Fatalf("plan change must be audited: %#v", adjusted.Changes)
	}

	stale := httptest.NewRecorder()
	testHandler.UpdateWorkflowInstancePlan(stale, workflowRequest(
		http.MethodPatch,
		"/api/workflow-instances/"+run.ID+"/plan",
		map[string]any{"expected_revision": 1, "title": "Stale", "stages": adjusted.Stages},
		map[string]string{"id": run.ID},
	))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale revision must be rejected, got %d: %s", stale.Code, stale.Body.String())
	}

	advance := httptest.NewRecorder()
	testHandler.TransitionWorkflowInstance(advance, workflowRequest(
		http.MethodPost,
		"/api/workflow-instances/"+run.ID+"/transition",
		map[string]any{"outcome": "approved", "expected_state_token": workflowStateTokenForTest(t, run.ID)},
		map[string]string{"id": run.ID},
	))
	if advance.Code != http.StatusOK {
		t.Fatalf("advance run: status=%d body=%s", advance.Code, advance.Body.String())
	}
	adjusted.Stages[0].Name = "Rewrite history"
	locked := httptest.NewRecorder()
	testHandler.UpdateWorkflowInstancePlan(locked, workflowRequest(
		http.MethodPatch,
		"/api/workflow-instances/"+run.ID+"/plan",
		map[string]any{"expected_revision": 2, "title": adjusted.Title, "stages": adjusted.Stages},
		map[string]string{"id": run.ID},
	))
	if locked.Code != http.StatusConflict {
		t.Fatalf("completed stage must remain locked, got %d: %s", locked.Code, locked.Body.String())
	}

	deleteProject := httptest.NewRecorder()
	testHandler.DeleteProject(deleteProject, workflowRequest(
		http.MethodDelete,
		"/api/projects/"+projectID,
		nil,
		map[string]string{"id": projectID},
	))
	if deleteProject.Code != http.StatusNoContent {
		t.Fatalf("delete project: status=%d body=%s", deleteProject.Code, deleteProject.Body.String())
	}
	var remainingStages, remainingChanges int
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM workflow_instance_stage WHERE workflow_instance_id = $1),
			(SELECT count(*) FROM workflow_instance_change WHERE workflow_instance_id = $1)
	`, run.ID).Scan(&remainingStages, &remainingChanges); err != nil {
		t.Fatalf("count deleted run plan records: %v", err)
	}
	if remainingStages != 0 || remainingChanges != 0 {
		t.Fatalf("project deletion left run plan data: stages=%d changes=%d", remainingStages, remainingChanges)
	}
}

func TestWorkflowLifecycleKeepsTaskStatusAndStageIndependent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupWorkflowTestData(t)

	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Release project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	var otherProjectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Other project')
		RETURNING id
	`, testWorkspaceID).Scan(&otherProjectID); err != nil {
		t.Fatalf("create other project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, otherProjectID)
	})

	create := httptest.NewRecorder()
	testHandler.CreateWorkflow(create, newRequest(http.MethodPost, "/api/workflows", map[string]any{
		"name":       "Release SOP",
		"project_id": projectID,
		"publish":    true,
		"stages": []map[string]any{
			{"name": "Build"},
			{"name": "Approve", "gate": map[string]any{"type": "human"}, "rollback_stage_key": "stage_1"},
		},
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create workflow: status=%d body=%s", create.Code, create.Body.String())
	}
	var workflow WorkflowResponse
	if err := json.Unmarshal(create.Body.Bytes(), &workflow); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	if workflow.CurrentVersion == nil || len(workflow.CurrentVersion.Stages) != 2 {
		t.Fatalf("expected published two-stage workflow: %#v", workflow)
	}

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number
		)
		VALUES (
			$1, 'Ship release', 'todo', 'none', 'member', $2,
			0, (SELECT issue_counter + 1000 FROM workspace WHERE id = $1)
		)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, taskID)
	})

	mismatchedInstance := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(
		mismatchedInstance,
		workflowRequest(http.MethodPost, "/api/workflows/"+workflow.ID+"/instances", map[string]any{
			"title":      "Wrong project",
			"project_id": otherProjectID,
			"start":      true,
		}, map[string]string{"id": workflow.ID}),
	)
	if mismatchedInstance.Code != http.StatusBadRequest {
		t.Fatalf("expected mismatched run project to be rejected, got %d: %s", mismatchedInstance.Code, mismatchedInstance.Body.String())
	}

	createInstance := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(
		createInstance,
		workflowRequest(http.MethodPost, "/api/workflows/"+workflow.ID+"/instances", map[string]any{
			"title": "Release 1.0",
			"start": true,
		}, map[string]string{"id": workflow.ID}),
	)
	if createInstance.Code != http.StatusCreated {
		t.Fatalf("create instance: status=%d body=%s", createInstance.Code, createInstance.Body.String())
	}
	var instance WorkflowInstanceResponse
	if err := json.Unmarshal(createInstance.Body.Bytes(), &instance); err != nil {
		t.Fatalf("decode instance: %v", err)
	}
	if len(instance.Stages) != 2 {
		t.Fatalf("expected an independent two-stage run plan: %#v", instance)
	}
	firstStage := instance.Stages[0]
	secondStage := instance.Stages[1]
	if instance.CurrentStageID == nil || *instance.CurrentStageID != firstStage.ID {
		t.Fatalf("expected instance to start at first stage: %#v", instance)
	}
	if instance.ProjectID == nil || *instance.ProjectID != projectID {
		t.Fatalf("run must inherit the SOP project: %#v", instance)
	}

	emptyRunRecorder := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(
		emptyRunRecorder,
		workflowRequest(http.MethodPost, "/api/workflows/"+workflow.ID+"/instances", map[string]any{
			"title": "Unused release run",
			"start": true,
		}, map[string]string{"id": workflow.ID}),
	)
	if emptyRunRecorder.Code != http.StatusCreated {
		t.Fatalf("create empty run: status=%d body=%s", emptyRunRecorder.Code, emptyRunRecorder.Body.String())
	}
	var emptyRun WorkflowInstanceResponse
	if err := json.Unmarshal(emptyRunRecorder.Body.Bytes(), &emptyRun); err != nil {
		t.Fatalf("decode empty run: %v", err)
	}
	archiveEmpty := httptest.NewRecorder()
	testHandler.ArchiveWorkflowInstance(
		archiveEmpty,
		workflowRequest(http.MethodPost, "/api/workflow-instances/"+emptyRun.ID+"/archive", nil, map[string]string{"id": emptyRun.ID}),
	)
	if archiveEmpty.Code != http.StatusOK {
		t.Fatalf("archive empty run: status=%d body=%s", archiveEmpty.Code, archiveEmpty.Body.String())
	}
	var archivedRun WorkflowInstanceResponse
	if err := json.Unmarshal(archiveEmpty.Body.Bytes(), &archivedRun); err != nil {
		t.Fatalf("decode archived run: %v", err)
	}
	if archivedRun.ArchivedAt == nil || archivedRun.ArchivedBy == nil || archivedRun.TaskCount != 0 {
		t.Fatalf("archive audit fields missing: %#v", archivedRun)
	}

	// Task creation and update must use the immutable Stage IDs from this Run's
	// copied plan, not the source workflow_version Stage IDs. Run plans can be
	// adjusted independently after migration 255, so the two ID spaces are
	// intentionally different.
	createBoundTask := httptest.NewRecorder()
	testHandler.CreateIssue(createBoundTask, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Created directly in the SOP Run",
		"project_id":           projectID,
		"workflow_instance_id": instance.ID,
		"workflow_stage_id":    firstStage.ID,
	}))
	if createBoundTask.Code != http.StatusCreated {
		t.Fatalf("create Task with Run Stage: status=%d body=%s", createBoundTask.Code, createBoundTask.Body.String())
	}
	var directlyBound IssueResponse
	if err := json.Unmarshal(createBoundTask.Body.Bytes(), &directlyBound); err != nil {
		t.Fatalf("decode directly bound Task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, directlyBound.ID)
	})
	if directlyBound.WorkflowInstanceID == nil || *directlyBound.WorkflowInstanceID != instance.ID ||
		directlyBound.WorkflowStageID == nil || *directlyBound.WorkflowStageID != firstStage.ID {
		t.Fatalf("created Task did not keep Run assignment: %#v", directlyBound)
	}

	moveDirectlyBound := httptest.NewRecorder()
	testHandler.UpdateIssue(moveDirectlyBound, workflowRequest(
		http.MethodPatch,
		"/api/issues/"+directlyBound.ID,
		map[string]any{
			"workflow_instance_id": instance.ID,
			"workflow_stage_id":    secondStage.ID,
		},
		map[string]string{"id": directlyBound.ID},
	))
	if moveDirectlyBound.Code != http.StatusOK {
		t.Fatalf("move Task to another Run Stage: status=%d body=%s", moveDirectlyBound.Code, moveDirectlyBound.Body.String())
	}
	var directlyMoved IssueResponse
	if err := json.Unmarshal(moveDirectlyBound.Body.Bytes(), &directlyMoved); err != nil {
		t.Fatalf("decode directly moved Task: %v", err)
	}
	if directlyMoved.WorkflowStageID == nil || *directlyMoved.WorkflowStageID != secondStage.ID {
		t.Fatalf("updated Task did not keep target Run Stage: %#v", directlyMoved)
	}

	crossRunStage := httptest.NewRecorder()
	testHandler.UpdateIssue(crossRunStage, workflowRequest(
		http.MethodPatch,
		"/api/issues/"+directlyBound.ID,
		map[string]any{
			"workflow_instance_id": instance.ID,
			"workflow_stage_id":    emptyRun.Stages[0].ID,
		},
		map[string]string{"id": directlyBound.ID},
	))
	if crossRunStage.Code != http.StatusBadRequest {
		t.Fatalf("Stage from another Run must be rejected, got %d: %s", crossRunStage.Code, crossRunStage.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, directlyBound.ID); err != nil {
		t.Fatalf("delete directly bound Task: %v", err)
	}

	attach := httptest.NewRecorder()
	testHandler.AttachWorkflowTask(
		attach,
		workflowRequest(http.MethodPut, "/api/workflow-instances/"+instance.ID+"/tasks/"+taskID, map[string]any{
			"stage_id": firstStage.ID,
		}, map[string]string{"id": instance.ID, "taskId": taskID}),
	)
	if attach.Code != http.StatusOK {
		t.Fatalf("attach task: status=%d body=%s", attach.Code, attach.Body.String())
	}
	archiveWithTask := httptest.NewRecorder()
	testHandler.ArchiveWorkflowInstance(
		archiveWithTask,
		workflowRequest(http.MethodPost, "/api/workflow-instances/"+instance.ID+"/archive", nil, map[string]string{"id": instance.ID}),
	)
	if archiveWithTask.Code != http.StatusConflict {
		t.Fatalf("run with Task must not be archived, got %d: %s", archiveWithTask.Code, archiveWithTask.Body.String())
	}
	var attached IssueResponse
	if err := json.Unmarshal(attach.Body.Bytes(), &attached); err != nil {
		t.Fatalf("decode attached task: %v", err)
	}
	if attached.ProjectID == nil || *attached.ProjectID != projectID {
		t.Fatalf("Space-level Task must inherit the run project when attached: %#v", attached)
	}
	listRuns := httptest.NewRecorder()
	testHandler.ListWorkflowInstances(
		listRuns,
		newRequest(http.MethodGet, "/api/workflow-instances?project_id="+projectID, nil),
	)
	if listRuns.Code != http.StatusOK {
		t.Fatalf("list workflow runs: status=%d body=%s", listRuns.Code, listRuns.Body.String())
	}
	var listed struct {
		Instances []WorkflowInstanceResponse `json:"instances"`
	}
	if err := json.Unmarshal(listRuns.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode workflow run list: %v", err)
	}
	listedTaskCount := int64(-1)
	for _, listedRun := range listed.Instances {
		if listedRun.ID == instance.ID {
			listedTaskCount = listedRun.TaskCount
			break
		}
	}
	if listedTaskCount != 1 {
		t.Fatalf("workflow run list must report one linked Task, got %d: %s", listedTaskCount, listRuns.Body.String())
	}

	oldStateToken := workflowStateTokenForTest(t, instance.ID)
	if _, err := testPool.Exec(context.Background(), `UPDATE workflow_instance SET updated_at=updated_at+interval '1 microsecond' WHERE id=$1`, instance.ID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "malformed", oldStateToken} {
		staleReview := httptest.NewRecorder()
		testHandler.TransitionWorkflowInstance(staleReview, workflowRequest(http.MethodPost, "/transition", map[string]any{"outcome": "approved", "expected_state_token": token}, map[string]string{"id": instance.ID}))
		if staleReview.Code != http.StatusConflict || !strings.Contains(staleReview.Body.String(), "review state") {
			t.Fatalf("stale manual review accepted or wrong guard: %d %s", staleReview.Code, staleReview.Body.String())
		}
	}

	blocked := httptest.NewRecorder()
	testHandler.TransitionWorkflowInstance(
		blocked,
		workflowRequest(http.MethodPost, "/api/workflow-instances/"+instance.ID+"/transition", map[string]any{
			"expected_state_token": workflowStateTokenForTest(t, instance.ID),
			"outcome":              "approved",
		}, map[string]string{"id": instance.ID}),
	)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("expected open task to block transition, got %d: %s", blocked.Code, blocked.Body.String())
	}

	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET status='cancelled' WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	cancelled := httptest.NewRecorder()
	testHandler.TransitionWorkflowInstance(cancelled, workflowRequest(http.MethodPost, "/transition", map[string]any{"outcome": "approved", "expected_state_token": workflowStateTokenForTest(t, instance.ID)}, map[string]string{"id": instance.ID}))
	if cancelled.Code != http.StatusConflict {
		t.Fatalf("cancelled required task passed gate: %d %s", cancelled.Code, cancelled.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET status = 'done' WHERE id = $1`, taskID); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	advance := httptest.NewRecorder()
	testHandler.TransitionWorkflowInstance(
		advance,
		workflowRequest(http.MethodPost, "/api/workflow-instances/"+instance.ID+"/transition", map[string]any{
			"expected_state_token": workflowStateTokenForTest(t, instance.ID),
			"outcome":              "approved",
		}, map[string]string{"id": instance.ID}),
	)
	if advance.Code != http.StatusOK {
		t.Fatalf("advance workflow: status=%d body=%s", advance.Code, advance.Body.String())
	}

	move := httptest.NewRecorder()
	testHandler.AttachWorkflowTask(
		move,
		workflowRequest(http.MethodPut, "/api/workflow-instances/"+instance.ID+"/tasks/"+taskID, map[string]any{
			"stage_id": secondStage.ID,
		}, map[string]string{"id": instance.ID, "taskId": taskID}),
	)
	if move.Code != http.StatusOK {
		t.Fatalf("move task: status=%d body=%s", move.Code, move.Body.String())
	}
	var moved IssueResponse
	if err := json.Unmarshal(move.Body.Bytes(), &moved); err != nil {
		t.Fatalf("decode moved task: %v", err)
	}
	if moved.Status != "done" || moved.WorkflowStageID == nil || *moved.WorkflowStageID != secondStage.ID {
		t.Fatalf("stage move must preserve task status: %#v", moved)
	}

	complete := httptest.NewRecorder()
	testHandler.TransitionWorkflowInstance(
		complete,
		workflowRequest(http.MethodPost, "/api/workflow-instances/"+instance.ID+"/transition", map[string]any{
			"expected_state_token": workflowStateTokenForTest(t, instance.ID),
			"outcome":              "approved",
			"note":                 "Human release approval",
		}, map[string]string{"id": instance.ID}),
	)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete workflow: status=%d body=%s", complete.Code, complete.Body.String())
	}
	var completed WorkflowInstanceResponse
	if err := json.Unmarshal(complete.Body.Bytes(), &completed); err != nil {
		t.Fatalf("decode completed instance: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected completed instance, got %q", completed.Status)
	}

	var decisionCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM workflow_gate_decision
		WHERE workflow_instance_id = $1
	`, instance.ID).Scan(&decisionCount); err != nil {
		t.Fatalf("count gate decisions: %v", err)
	}
	if decisionCount != 1 {
		t.Fatalf("expected one human gate decision, got %d", decisionCount)
	}
}

func TestSpaceWorkflowCanRunInsideAProject(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupWorkflowTestData(t)

	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Space SOP target')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	create := httptest.NewRecorder()
	testHandler.CreateWorkflow(create, newRequest(http.MethodPost, "/api/workflows", map[string]any{
		"name":    "Space release SOP",
		"publish": true,
		"stages": []map[string]any{
			{
				"name": "Delivery",
				"completion_rule": map[string]any{
					"type":            "all_tasks_terminal",
					"evaluation_mode": "on_task_change",
				},
			},
		},
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create Space workflow: status=%d body=%s", create.Code, create.Body.String())
	}
	var workflow WorkflowResponse
	if err := json.Unmarshal(create.Body.Bytes(), &workflow); err != nil {
		t.Fatalf("decode Space workflow: %v", err)
	}
	if workflow.ProjectID != nil {
		t.Fatalf("Space workflow must not be pinned to a project: %#v", workflow)
	}

	missingProject := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(
		missingProject,
		workflowRequest(http.MethodPost, "/api/workflows/"+workflow.ID+"/instances", map[string]any{
			"title": "Missing target",
			"start": true,
		}, map[string]string{"id": workflow.ID}),
	)
	if missingProject.Code != http.StatusBadRequest {
		t.Fatalf("expected Space workflow run without project to fail, got %d: %s", missingProject.Code, missingProject.Body.String())
	}

	createRun := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(
		createRun,
		workflowRequest(http.MethodPost, "/api/workflows/"+workflow.ID+"/instances", map[string]any{
			"title":      "Project delivery",
			"project_id": projectID,
			"start":      true,
		}, map[string]string{"id": workflow.ID}),
	)
	if createRun.Code != http.StatusCreated {
		t.Fatalf("start Space workflow in project: status=%d body=%s", createRun.Code, createRun.Body.String())
	}
	var run WorkflowInstanceResponse
	if err := json.Unmarshal(createRun.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode workflow run: %v", err)
	}
	if run.ProjectID == nil || *run.ProjectID != projectID {
		t.Fatalf("Space workflow run must bind to the selected project: %#v", run)
	}

	list := httptest.NewRecorder()
	testHandler.ListWorkflows(
		list,
		newRequest(http.MethodGet, "/api/workflows?project_id="+projectID, nil),
	)
	if list.Code != http.StatusOK {
		t.Fatalf("list project workflows: status=%d body=%s", list.Code, list.Body.String())
	}
	var payload struct {
		Workflows []WorkflowResponse `json:"workflows"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode workflow list: %v", err)
	}
	found := false
	for _, item := range payload.Workflows {
		if item.ID == workflow.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Project workflow list must include reusable Space SOPs")
	}
}

func stringPtr(value string) *string { return &value }

func TestNormalizeWorkflowExecutionConfiguration(t *testing.T) {
	executor := "12345678-1234-1234-1234-123456789abc"
	valid := WorkflowStageInput{Name: "Build", ExecutionMode: "agent", ExecutorID: &executor, OutputSpec: json.RawMessage(`{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}`)}
	normalized, err := normalizeWorkflowStages([]WorkflowStageInput{valid})
	if err != nil {
		t.Fatal(err)
	}
	if string(normalized[0].CompletionRule) != `{"type":"validated_result"}` {
		t.Fatalf("unexpected completion rule: %s", normalized[0].CompletionRule)
	}
	for _, mutate := range []func(*WorkflowStageInput){
		func(s *WorkflowStageInput) { s.ExecutorID = nil },
		func(s *WorkflowStageInput) { s.ExecutionMode = "manual" },
		func(s *WorkflowStageInput) { s.ExecutionMode = "unknown" },
		func(s *WorkflowStageInput) { s.OutputSpec = json.RawMessage(`{}`) },
		func(s *WorkflowStageInput) { s.CompletionRule = json.RawMessage(`{"type":"all_tasks_terminal"}`) },
	} {
		invalid := valid
		mutate(&invalid)
		if _, err := normalizeWorkflowStages([]WorkflowStageInput{invalid}); err == nil {
			t.Fatalf("accepted invalid execution configuration: %+v", invalid)
		}
	}
}

func TestWorkflowStartCreatesDispatchIntentAtomically(t *testing.T) {
	if testPool == nil || testHandler == nil {
		t.Skip("database not available")
	}
	cleanupWorkflowTestData(t)
	ctx := context.Background()
	executor := createHandlerTestAgent(t, "SOP executor", []byte(`{}`))
	var projectID string
	if err := testPool.QueryRow(ctx, `INSERT INTO project (workspace_id,title) VALUES ($1,'Automated SOP test') RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id=$1`, projectID) })
	create := httptest.NewRecorder()
	testHandler.CreateWorkflow(create, newRequest(http.MethodPost, "/api/workflows", map[string]any{
		"name": "Automated SOP", "project_id": projectID, "publish": true,
		"stages": []map[string]any{{"name": "Build", "execution_mode": "agent", "executor_id": executor,
			"input_bindings": json.RawMessage(`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"goal","source":{"kind":"run_input","pointer":"/goal"}}]}`),
			"input_spec":     json.RawMessage(`{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["goal"],"properties":{"goal":{"type":"string"}}}}}`),
			"output_spec":    json.RawMessage(`{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}`),
		}},
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create definition: %d %s", create.Code, create.Body.String())
	}
	var definition WorkflowResponse
	if err := json.Unmarshal(create.Body.Bytes(), &definition); err != nil {
		t.Fatal(err)
	}
	start := func(input any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.CreateWorkflowInstance(w, workflowRequest(http.MethodPost, "/api/workflows/"+definition.ID+"/instances", map[string]any{"title": "Run", "start": true, "inputs": input}, map[string]string{"id": definition.ID}))
		return w
	}
	blocked := start(map[string]any{})
	if blocked.Code != http.StatusCreated {
		t.Fatalf("missing input did not preserve Run: %d %s", blocked.Code, blocked.Body.String())
	}
	var blockedRun WorkflowInstanceResponse
	if err := json.Unmarshal(blocked.Body.Bytes(), &blockedRun); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1 AND status='blocked' AND failure_code='input_unavailable'`, blockedRun.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing startup blocker: %d %v", count, err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE workflow_instance_id=$1`, blockedRun.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("blocked startup dispatched: %d %v", count, err)
	}
	diagnostics := httptest.NewRecorder()
	testHandler.ListWorkflowExecutions(diagnostics, workflowRequest(http.MethodGet, "/executions", nil, map[string]string{"id": blockedRun.ID}))
	if diagnostics.Code != http.StatusOK || !strings.Contains(diagnostics.Body.String(), `"failure_code":"input_unavailable"`) || !strings.Contains(diagnostics.Body.String(), `"failure_detail":`) {
		t.Fatalf("missing public diagnostics: %d %s", diagnostics.Code, diagnostics.Body.String())
	}
	created := start(map[string]any{"goal": "Produce a report"})
	if created.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", created.Code, created.Body.String())
	}
	var run WorkflowInstanceResponse
	if err := json.Unmarshal(created.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if len(run.Stages[0].InputBindings) < 3 || run.Stages[0].ExecutionMode != "agent" || run.Stages[0].ExecutorID == nil || *run.Stages[0].ExecutorID != executor {
		t.Fatalf("execution config not copied: %+v", run.Stages)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution e JOIN workflow_execution_action a ON a.execution_id=e.id WHERE e.workflow_instance_id=$1 AND e.input_snapshot#>>'{data,goal}'='Produce a report' AND e.input_snapshot->>'schema_version'='silieco.input-snapshot/v1' AND e.config_snapshot->'input_bindings'->>'schema_version'='silieco.input-bindings/v1'`, run.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing atomic dispatch intent: %d %v", count, err)
	}
	executionSvc := &service.WorkflowExecutionService{Queries: testHandler.Queries, TxStarter: testHandler.TxStarter, TaskService: testHandler.TaskService}
	if err := executionSvc.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workflow_instance_id=$1`, run.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("start-to-dispatch failed: %d %v", count, err)
	}
	var executionID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM workflow_stage_execution WHERE workflow_instance_id=$1`, run.ID).Scan(&executionID); err != nil {
		t.Fatal(err)
	}
	resultParams := map[string]string{"id": run.ID, "executionId": executionID}
	submit := func(body any) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		testHandler.SubmitWorkflowResult(response, workflowRequest(http.MethodPost, "/results", body, resultParams))
		return response
	}
	for _, invalid := range []any{
		map[string]any{"submission": map[string]any{"data": map[string]any{}}},
		map[string]any{"expected_revision": 0, "submission": map[string]any{"data": map[string]any{}, "accepted": true}},
	} {
		rejected := submit(invalid)
		if rejected.Code != http.StatusBadRequest && rejected.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid result: %d %s", rejected.Code, rejected.Body.String())
		}
	}
	saved := submit(map[string]any{"expected_revision": 0, "submission": map[string]any{"data": map[string]any{}, "artifacts": []any{}}})
	if saved.Code != http.StatusCreated {
		t.Fatalf("save result: %d %s", saved.Code, saved.Body.String())
	}
	var result WorkflowResultResponse
	if err := json.Unmarshal(saved.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 || string(result.Data) != "{}" {
		t.Fatalf("invalid result JSON: %s", saved.Body.String())
	}
	stale := submit(map[string]any{"expected_revision": 0, "submission": map[string]any{"data": map[string]any{}}})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale result: %d %s", stale.Code, stale.Body.String())
	}
	read := httptest.NewRecorder()
	testHandler.GetWorkflowResult(read, workflowRequest(http.MethodGet, "/results/1", nil, map[string]string{"id": run.ID, "executionId": executionID, "revision": "1"}))
	if read.Code != http.StatusOK {
		t.Fatalf("read result: %d %s", read.Code, read.Body.String())
	}
	readinessResponse := httptest.NewRecorder()
	testHandler.GetWorkflowExecutionReadiness(readinessResponse, workflowRequest(http.MethodGet, "/readiness", nil, resultParams))
	var readiness service.WorkflowExecutionReadiness
	if err := json.Unmarshal(readinessResponse.Body.Bytes(), &readiness); err != nil {
		t.Fatal(err)
	}
	if readinessResponse.Code != http.StatusOK || readiness.ReadyForReview || readiness.Scope != "readiness_snapshot" || readiness.ResultRevision != 1 || len(readiness.Blockers) == 0 {
		t.Fatalf("result bypassed completion conditions: %d %s", readinessResponse.Code, readinessResponse.Body.String())
	}
	testWorkflowAgentReviewAPI(t, run.ID, executionID)
	testWorkflowHumanReviewAPI(t, run.ID, executionID)
	testWorkflowResultDownload(t, run.ID, executionID, result)
	testWorkflowDeleteFailurePreservesCalls(t, executionID)
	transition := httptest.NewRecorder()
	testHandler.TransitionWorkflowInstance(transition, workflowRequest(http.MethodPost, "/api/workflow-instances/"+run.ID+"/transition", map[string]any{"outcome": "approved", "expected_state_token": workflowStateTokenForTest(t, run.ID)}, map[string]string{"id": run.ID}))
	if transition.Code != http.StatusConflict {
		t.Fatalf("manual transition bypassed result acceptance: %d %s", transition.Code, transition.Body.String())
	}
	version := httptest.NewRecorder()
	testHandler.CreateWorkflowVersion(version, workflowRequest(http.MethodPost, "/api/workflows/"+definition.ID+"/versions", map[string]any{
		"publish": true, "stages": []map[string]any{
			{"name": "Manual preparation"},
			{"name": "Automated build", "execution_mode": "agent", "executor_id": executor, "output_spec": json.RawMessage(`{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}`)},
		},
	}, map[string]string{"id": definition.ID}))
	if version.Code != http.StatusCreated {
		t.Fatalf("publish manual/agent version: %d %s", version.Code, version.Body.String())
	}
	manualStart := startRunForWorkflowTest(t, definition.ID)
	advance := httptest.NewRecorder()
	testHandler.TransitionWorkflowInstance(advance, workflowRequest(http.MethodPost, "/api/workflow-instances/"+manualStart.ID+"/transition", map[string]any{"outcome": "approved", "expected_state_token": workflowStateTokenForTest(t, manualStart.ID)}, map[string]string{"id": manualStart.ID}))
	if advance.Code != http.StatusOK {
		t.Fatalf("enter automatic stage: %d %s", advance.Code, advance.Body.String())
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution e JOIN workflow_execution_action a ON a.execution_id=e.id WHERE e.workflow_instance_id=$1`, manualStart.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("next stage intent missing: %d %v", count, err)
	}

	testWorkflowInputCorrectionAPI(t, blockedRun.ID)
	testWorkflowReworkAPI(t, blockedRun.ID)
}

func startRunForWorkflowTest(t *testing.T, workflowID string) WorkflowInstanceResponse {
	t.Helper()
	response := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(response, workflowRequest(http.MethodPost, "/api/workflows/"+workflowID+"/instances", map[string]any{"title": "Manual then agent", "start": true}, map[string]string{"id": workflowID}))
	if response.Code != http.StatusCreated {
		t.Fatalf("start run: %d %s", response.Code, response.Body.String())
	}
	var run WorkflowInstanceResponse
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	return run
}
