package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

func testWorkflowResultDownload(t *testing.T, runID, executionID string, result WorkflowResultResponse) {
	t.Helper()
	ctx := context.Background()
	store := &mockStorage{files: map[string][]byte{"frozen-report": []byte("# Frozen report\nTemperature: 80")}}
	oldStorage := testHandler.Storage
	testHandler.Storage = store
	defer func() { testHandler.Storage = oldStorage }()
	uploadID := parseUUID(uuid.NewString())
	if _, err := testHandler.Queries.CreateWorkflowArtifactUpload(ctx, db.CreateWorkflowArtifactUploadParams{ID: uploadID, WorkspaceID: parseUUID(testWorkspaceID), WorkflowInstanceID: parseUUID(runID), StorageKey: "frozen-report"}); err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.Queries.CommitWorkflowArtifactUpload(ctx, db.CommitWorkflowArtifactUploadParams{ID: uploadID, WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal(err)
	}
	var stageID string
	if err := testPool.QueryRow(ctx, `SELECT workflow_stage_id FROM workflow_stage_execution WHERE id=$1`, executionID).Scan(&stageID); err != nil {
		t.Fatal(err)
	}
	artifact, err := testHandler.Queries.GetOrCreateWorkflowArtifact(ctx, db.GetOrCreateWorkflowArtifactParams{WorkspaceID: parseUUID(testWorkspaceID), WorkflowInstanceID: parseUUID(runID), WorkflowStageID: parseUUID(stageID), Name: "report.md"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := testHandler.Queries.CreateWorkflowArtifactRevision(ctx, db.CreateWorkflowArtifactRevisionParams{WorkspaceID: parseUUID(testWorkspaceID), WorkflowInstanceID: parseUUID(runID), ArtifactID: artifact.ID, ExecutionID: parseUUID(executionID), ResultID: parseUUID(result.ID), UploadID: uploadID, Revision: 1, SourceAttachmentID: parseUUID(uuid.NewString()), Kind: "report", MediaType: "text/markdown", SchemaVersion: "report/v1", Checksum: workflowcontract.Checksum(store.files["frozen-report"]), SizeBytes: int64(len(store.files["frozen-report"]))})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": runID, "executionId": executionID, "revision": "1", "artifactRevisionId": uuidToString(revision.ID)}
	read := httptest.NewRecorder()
	testHandler.GetWorkflowResult(read, workflowRequest(http.MethodGet, "/results/1", nil, params))
	var response WorkflowResultResponse
	if err := json.Unmarshal(read.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if read.Code != http.StatusOK || len(response.Artifacts) != 1 || response.Artifacts[0].ArtifactID != uuidToString(artifact.ID) || response.Artifacts[0].Revision != 1 || strings.Contains(read.Body.String(), "frozen-report") {
		t.Fatalf("artifact manifest: %d %s", read.Code, read.Body.String())
	}
	download := httptest.NewRecorder()
	testHandler.GetWorkflowResultArtifact(download, workflowRequest(http.MethodGet, response.Artifacts[0].ContentURL, nil, params))
	if download.Code != http.StatusOK || download.Body.String() != string(store.files["frozen-report"]) || download.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("download: %d %s", download.Code, download.Body.String())
	}
	for _, key := range []string{"id", "executionId", "revision", "artifactRevisionId"} {
		original := params[key]
		params[key] = uuid.NewString()
		if key == "revision" {
			params[key] = "2"
		}
		rejected := httptest.NewRecorder()
		testHandler.GetWorkflowResultArtifact(rejected, workflowRequest(http.MethodGet, "/artifact", nil, params))
		if rejected.Code != http.StatusNotFound {
			t.Fatalf("wrong %s accepted: %d %s", key, rejected.Code, rejected.Body.String())
		}
		params[key] = original
	}
	store.files["frozen-report"][0] = '!'
	corrupt := httptest.NewRecorder()
	testHandler.GetWorkflowResultArtifact(corrupt, workflowRequest(http.MethodGet, "/artifact", nil, params))
	if corrupt.Code != http.StatusConflict || strings.Contains(corrupt.Body.String(), "Temperature") {
		t.Fatalf("corrupt bytes leaked: %d %s", corrupt.Code, corrupt.Body.String())
	}
	delete(store.files, "frozen-report")
	missing := httptest.NewRecorder()
	testHandler.GetWorkflowResultArtifact(missing, workflowRequest(http.MethodGet, "/artifact", nil, params))
	if missing.Code != http.StatusBadGateway {
		t.Fatalf("missing object: %d %s", missing.Code, missing.Body.String())
	}
}

func testWorkflowDeleteFailurePreservesCalls(t *testing.T, executionID string) {
	t.Helper()
	ctx := context.Background()
	var issueID, taskID, before string
	if err := testPool.QueryRow(ctx, `SELECT e.main_issue_id,e.main_task_id,t.status FROM workflow_stage_execution e JOIN agent_task_queue t ON t.id=e.main_task_id WHERE e.id=$1`, executionID).Scan(&issueID, &taskID, &before); err != nil {
		t.Fatal(err)
	}
	name := "workflow_delete_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := testPool.Exec(ctx, "CREATE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='"+issueID+"'::uuid THEN RAISE EXCEPTION 'injected deletion failure'; END IF; RETURN OLD; END $$"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DROP TRIGGER IF EXISTS "+name+" ON issue")
		testPool.Exec(context.Background(), "DROP FUNCTION IF EXISTS "+name+"()")
	})
	if _, err := testPool.Exec(ctx, "CREATE TRIGGER "+name+" BEFORE DELETE ON issue FOR EACH ROW EXECUTE FUNCTION "+name+"()"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	testHandler.DeleteIssue(response, workflowRequest(http.MethodDelete, "/api/issues/"+issueID, nil, map[string]string{"id": issueID}))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("injected deletion: %d %s", response.Code, response.Body.String())
	}
	batch := httptest.NewRecorder()
	testHandler.BatchDeleteIssues(batch, newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{issueID}}))
	var batchResult struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(batch.Body.Bytes(), &batchResult); err != nil {
		t.Fatal(err)
	}
	if batch.Code != http.StatusOK || batchResult.Deleted != 0 {
		t.Fatalf("failed batch deletion: %d %s", batch.Code, batch.Body.String())
	}
	var after string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&after); err != nil || after != before {
		t.Fatalf("failed deletion changed task: before=%s after=%s err=%v", before, after, err)
	}
	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM issue WHERE id=$1)`, issueID).Scan(&exists); err != nil || !exists {
		t.Fatalf("failed deletion lost issue: %v %v", exists, err)
	}
	if _, err := testPool.Exec(ctx, "DROP TRIGGER "+name+" ON issue"); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workflow_instance SET status='completed' WHERE id=(SELECT workflow_instance_id FROM workflow_stage_execution WHERE id=$1)`, executionID); err != nil {
		t.Fatal(err)
	}
	closed := httptest.NewRecorder()
	testHandler.BatchDeleteIssues(closed, newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{issueID}}))
	if err := json.Unmarshal(closed.Body.Bytes(), &batchResult); err != nil {
		t.Fatal(err)
	}
	if closed.Code != http.StatusOK || batchResult.Deleted != 0 {
		t.Fatalf("closed Run batch deleted: %d %s", closed.Code, closed.Body.String())
	}
	if _, err := testPool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=(SELECT workflow_instance_id FROM workflow_stage_execution WHERE id=$1)`, executionID); err != nil {
		t.Fatal(err)
	}
	success := httptest.NewRecorder()
	testHandler.BatchDeleteIssues(success, newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{issueID}}))
	if err := json.Unmarshal(success.Body.Bytes(), &batchResult); err != nil {
		t.Fatal(err)
	}
	if success.Code != http.StatusOK || batchResult.Deleted != 1 {
		t.Fatalf("batch retry failed: %d %s", success.Code, success.Body.String())
	}

}
