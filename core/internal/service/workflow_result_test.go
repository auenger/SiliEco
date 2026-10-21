package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/storage"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

type workflowResultStorage struct {
	storage.Storage
	mu          sync.Mutex
	objects     map[string][]byte
	failUpload  bool
	failDelete  bool
	afterUpload func()
}

func (s *workflowResultStorage) ObjectURL(key string) string { return "https://storage.test/" + key }
func (s *workflowResultStorage) KeyFromURL(url string) string {
	return strings.TrimPrefix(url, "https://storage.test/")
}
func (s *workflowResultStorage) Upload(_ context.Context, key string, data []byte, _, _ string) (string, error) {
	s.mu.Lock()
	s.objects[key] = bytes.Clone(data)
	fail := s.failUpload
	callback := s.afterUpload
	s.mu.Unlock()
	if callback != nil {
		callback()
	}
	if fail {
		return "", errors.New("ambiguous upload failure")
	}
	return s.ObjectURL(key), nil
}
func (s *workflowResultStorage) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("missing object")
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(data))), nil
}
func (s *workflowResultStorage) DeleteObject(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDelete {
		return errors.New("delete unavailable")
	}
	delete(s.objects, key)
	return nil
}

func workflowResultFixture(t *testing.T) (*pgxpool.Pool, *WorkflowExecutionService, SubmitWorkflowResultParams, *workflowResultStorage) {
	t.Helper()
	ctx := context.Background()
	pool, svc, ensure := workflowExecutionFixture(t)
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET output_spec='{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"},"artifacts":[{"name":"report","kind":"report","media_type":"text/markdown","schema_version":"report/v1","required":true}]}}' WHERE id=$1`, ensure.StageID); err != nil {
		t.Fatal(err)
	}
	svc.TaskService = NewTaskService(svc.Queries, pool, nil, events.New())
	execution, err := svc.EnsureStageExecution(ctx, ensure)
	if err != nil {
		t.Fatal(err)
	}
	action, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, ensure.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchStageAction(ctx, ensure.WorkspaceID, action.ID, action.ClaimToken); err != nil {
		t.Fatal(err)
	}
	execution, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: ensure.WorkspaceID, ID: execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Queries.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: ensure.WorkspaceID, ID: ensure.InstanceID})
	if err != nil {
		t.Fatal(err)
	}
	blob := &workflowResultStorage{objects: map[string][]byte{"source": []byte("# Report\nValidated content")}}
	svc.Storage = blob
	attachmentID := util.MustParseUUID(uuid.NewString())
	_, err = svc.Queries.CreateAttachment(ctx, db.CreateAttachmentParams{ID: attachmentID, WorkspaceID: ensure.WorkspaceID, IssueID: execution.MainIssueID, TaskID: execution.MainTaskID, UploaderType: "member", UploaderID: run.CreatedBy, Filename: "report.md", Url: blob.ObjectURL("source"), ContentType: "text/markdown", SizeBytes: int64(len(blob.objects["source"]))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `DELETE FROM attachment WHERE id=$1`, attachmentID)
		if err != nil {
			t.Error(err)
		}
	})
	payload, err := json.Marshal(workflowcontract.Submission{Data: json.RawMessage(`{}`), Artifacts: []workflowcontract.Artifact{{Name: "report", Kind: "report", MediaType: "text/markdown", SchemaVersion: "report/v1", AttachmentID: uuid.UUID(attachmentID.Bytes).String(), Checksum: workflowcontract.Checksum(blob.objects["source"])}}})
	if err != nil {
		t.Fatal(err)
	}
	return pool, svc, SubmitWorkflowResultParams{WorkspaceID: ensure.WorkspaceID, InstanceID: ensure.InstanceID, ExecutionID: execution.ID, ActorType: "member", ActorID: run.CreatedBy, Submission: payload}, blob
}

func TestWorkflowResultImmutableRevisions(t *testing.T) {
	pool, svc, p, blob := workflowResultFixture(t)
	ctx := context.Background()
	first, err := svc.SubmitResult(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.Queries.ListWorkflowResultArtifacts(ctx, db.ListWorkflowResultArtifactsParams{WorkspaceID: p.WorkspaceID, ResultID: first.ID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("artifacts: %v %v", rows, err)
	}
	if first.Revision != 1 || rows[0].Revision != 1 || !bytes.Equal(blob.objects[rows[0].StorageKey], blob.objects["source"]) {
		t.Fatal("result did not freeze source bytes")
	}
	if _, err := svc.SubmitResult(ctx, p); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("stale submission: %v", err)
	}
	p.ExpectedRevision = 1
	second, err := svc.SubmitResult(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	secondRows, err := svc.Queries.ListWorkflowResultArtifacts(ctx, db.ListWorkflowResultArtifactsParams{WorkspaceID: p.WorkspaceID, ResultID: second.ID})
	if err != nil || len(secondRows) != 1 {
		t.Fatal(err)
	}
	if second.Revision != 2 || secondRows[0].Revision != 2 || secondRows[0].ArtifactID != rows[0].ArtifactID || secondRows[0].StorageKey == rows[0].StorageKey {
		t.Fatal("artifact revision identity was not preserved")
	}
	delete(blob.objects, "source")
	if len(blob.objects[rows[0].StorageKey]) == 0 {
		t.Fatal("source deletion removed frozen output")
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_artifact_upload SET expires_at=now()-interval '1 day' WHERE workspace_id=$1`, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := svc.CleanupArtifacts(ctx); err != nil {
		t.Fatal(err)
	}
	if len(blob.objects[rows[0].StorageKey]) == 0 {
		t.Fatal("cleanup removed committed artifact")
	}
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil || execution.Status != "awaiting_review" || execution.ResultRevision != 2 {
		t.Fatalf("execution %+v %v", execution, err)
	}
}

func TestWorkflowResultRejectsInvalidContentAndScope(t *testing.T) {
	for _, mode := range []string{"checksum", "external", "foreign issue", "actor", "revision", "format"} {
		t.Run(mode, func(t *testing.T) {
			pool, svc, p, blob := workflowResultFixture(t)
			ctx := context.Background()
			switch mode {
			case "checksum":
				blob.objects["source"][0] = '!'
			case "external":
				if _, err := pool.Exec(ctx, `UPDATE attachment SET url='https://evil.test/source' WHERE workspace_id=$1`, p.WorkspaceID); err != nil {
					t.Fatal(err)
				}
			case "foreign issue":
				if _, err := pool.Exec(ctx, `UPDATE attachment SET issue_id=NULL,task_id=NULL WHERE workspace_id=$1`, p.WorkspaceID); err != nil {
					t.Fatal(err)
				}
			case "actor":
				p.ActorID = util.MustParseUUID(uuid.NewString())
			case "revision":
				p.ExpectedRevision = 1
			case "format":
				blob.objects["source"][0] = 0
				var submission workflowcontract.Submission
				if err := json.Unmarshal(p.Submission, &submission); err != nil {
					t.Fatal(err)
				}
				submission.Artifacts[0].Checksum = workflowcontract.Checksum(blob.objects["source"])
				var err error
				p.Submission, err = json.Marshal(submission)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.SubmitResult(ctx, p); err == nil {
				t.Fatal("invalid submission accepted")
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_result WHERE workspace_id=$1`, p.WorkspaceID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial result %d %v", count, err)
			}
		})
	}
}

func TestWorkflowResultConcurrentSubmission(t *testing.T) {
	_, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := svc.SubmitResult(ctx, p); results <- err }()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, ErrWorkflowExecutionConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", successes, conflicts)
	}
}

func TestWorkflowResultUploadFailureAndLateObjectCleanup(t *testing.T) {
	pool, svc, p, blob := workflowResultFixture(t)
	ctx := context.Background()
	blob.failUpload = true
	if _, err := svc.SubmitResult(ctx, p); err == nil {
		t.Fatal("upload failure accepted")
	}
	var key string
	if err := pool.QueryRow(ctx, `SELECT storage_key FROM workflow_artifact_upload WHERE workspace_id=$1 AND status='deleting'`, p.WorkspaceID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	blob.failDelete = true
	if err := svc.CleanupArtifacts(ctx); err == nil {
		t.Fatal("delete failure hidden")
	}
	blob.failDelete = false
	if _, err := pool.Exec(ctx, `UPDATE workflow_artifact_upload SET expires_at=now() WHERE workspace_id=$1`, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := svc.CleanupArtifacts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := blob.objects[key]; ok {
		t.Fatal("abandoned blob retained")
	}
	blob.objects[key] = []byte("late PUT")
	if _, err := pool.Exec(ctx, `UPDATE workflow_artifact_upload SET expires_at=now(),created_at=now()-interval '3 days' WHERE workspace_id=$1`, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := svc.CleanupArtifacts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := blob.objects[key]; ok {
		t.Fatal("late upload not reclaimed")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_artifact_upload WHERE workspace_id=$1`, p.WorkspaceID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("tombstone count %d %v", count, err)
	}
}

func TestWorkflowResultRevalidatesAfterUpload(t *testing.T) {
	pool, svc, p, blob := workflowResultFixture(t)
	ctx := context.Background()
	blob.afterUpload = func() {
		if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='cancelled' WHERE id=$1`, p.InstanceID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.SubmitResult(ctx, p); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("cancelled Run accepted: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_result WHERE workspace_id=$1`, p.WorkspaceID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial result %d %v", count, err)
	}
}

func TestWorkflowResultAgentTaskIdentity(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	p.ActorType = "agent"
	p.ActorID = execution.ExecutorID
	p.SourceTaskID = execution.MainTaskID
	if _, err := svc.SubmitResult(ctx, p); !errors.Is(err, ErrWorkflowResultAccess) {
		t.Fatalf("queued agent submitted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	wrong := p
	wrong.SourceTaskID = util.MustParseUUID(uuid.NewString())
	if _, err := svc.SubmitResult(ctx, wrong); !errors.Is(err, ErrWorkflowResultAccess) {
		t.Fatalf("foreign task accepted: %v", err)
	}
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowResultProjectCleanup(t *testing.T) {
	pool, svc, p, blob := workflowResultFixture(t)
	ctx := context.Background()
	result, err := svc.SubmitResult(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.Queries.ListWorkflowResultArtifacts(ctx, db.ListWorkflowResultArtifactsParams{WorkspaceID: p.WorkspaceID, ResultID: result.ID})
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	projectID := util.MustParseUUID(uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO project (id,workspace_id,title) VALUES ($1,$2,'Result cleanup')`, projectID, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM project WHERE id=$1`, projectID) })
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET project_id=$2 WHERE id=$1`, p.InstanceID, projectID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := svc.Queries.WithTx(tx)
	if _, err := q.LockProjectForDelete(ctx, db.LockProjectForDeleteParams{ID: projectID, WorkspaceID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	if err := q.DeleteProjectWorkflowResults(ctx, db.DeleteProjectWorkflowResultsParams{WorkspaceID: p.WorkspaceID, ProjectID: projectID}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"workflow_execution_result", "workflow_artifact_revision", "workflow_artifact"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", p.WorkspaceID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan %s %d %v", table, count, err)
		}
	}
	if err := svc.CleanupArtifacts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, exists := blob.objects[rows[0].StorageKey]; exists {
		t.Fatal("deleted project retained artifact bytes")
	}
}

func TestWorkflowResultDatabaseFailureRollsBackRevision(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	constraint := "workflow_result_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := pool.Exec(ctx, "ALTER TABLE workflow_artifact_revision ADD CONSTRAINT "+constraint+" CHECK (workspace_id <> '"+uuid.UUID(p.WorkspaceID.Bytes).String()+"'::uuid) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "ALTER TABLE workflow_artifact_revision DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	if _, err := svc.SubmitResult(ctx, p); err == nil {
		t.Fatal("database failure accepted")
	}
	for _, table := range []string{"workflow_execution_result", "workflow_artifact_revision", "workflow_artifact"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", p.WorkspaceID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial %s %d %v", table, count, err)
		}
	}
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil || execution.ResultRevision != 0 || execution.Status != "queued" {
		t.Fatalf("partial execution: %+v %v", execution, err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE workflow_artifact_revision DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}
