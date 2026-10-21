package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestWorkflowReviewBrowserFixture seeds only the explicitly selected browser
// fixture. Its synthetic provider has no daemon and never invokes an agent CLI.
func TestWorkflowReviewBrowserFixture(t *testing.T) {
	runText := os.Getenv("SILIECO_REVIEW_BROWSER_RUN")
	if runText == "" {
		t.Skip("explicit browser fixture only")
	}
	runID, err := util.ParseUUID(runText)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := db.New(pool)
	var wsID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM workflow_instance WHERE id=$1 AND title LIKE 'Review recovery browser %'`, runID).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	run, err := q.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: wsID, ID: runID})
	if err != nil {
		t.Fatal(err)
	}
	runtimeName := "review-browser-" + runText
	if os.Getenv("SILIECO_REVIEW_BROWSER_MODE") == "cleanup" {
		for _, sql := range []string{`DELETE FROM agent_task_queue WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id=$1 AND name=$2)`, `DELETE FROM agent WHERE workspace_id=$1 AND name=$2`, `DELETE FROM agent_runtime WHERE workspace_id=$1 AND name=$2`} {
			if _, err := pool.Exec(ctx, sql, wsID, runtimeName); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	if !strings.HasPrefix(run.Title, "Review recovery browser ") {
		t.Fatal("not a browser fixture")
	}
	var runtimeID, agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status,visibility,owner_id) VALUES($1,$2,'cloud','sop_browser_fixture','online','private',$3) RETURNING id`, wsID, runtimeName, run.CreatedBy).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,description,runtime_mode,runtime_config,runtime_id,visibility,max_concurrent_tasks,owner_id) VALUES($1,$2,'Controlled browser fixture','cloud','{}',$3,'private',1,$4) RETURNING id`, wsID, runtimeName, runtimeID, run.CreatedBy).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	gate, _ := json.Marshal(map[string]string{"type": "agent", "decider": util.UUIDToString(agentID)})
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET execution_mode='agent',executor_id=$2,gate=$3::jsonb,output_spec='{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}' WHERE id=$1`, run.CurrentStageID, agentID, gate); err != nil {
		t.Fatal(err)
	}

	if os.Getenv("SILIECO_REVIEW_BROWSER_MODE") == "rework" {
		if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET position=1,rollback_stage_key='manual_input' WHERE id=$1`, run.CurrentStageID); err != nil {
			t.Fatal(err)
		}
		var manualID pgtype.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode) VALUES($1,$2,'manual_input','Manual input',0,'manual') RETURNING id`, wsID, runID).Scan(&manualID); err != nil {
			t.Fatal(err)
		}
		if _, err := q.CreateIssue(ctx, db.CreateIssueParams{WorkspaceID: wsID, Title: "Previous manual input", Status: "done", Priority: "none", CreatorType: "member", CreatorID: run.CreatedBy, Number: 9908, WorkflowInstanceID: runID, WorkflowStageID: manualID}); err != nil {
			t.Fatal(err)
		}
	}
	svc := WorkflowExecutionService{Queries: q, TxStarter: pool, TaskService: NewTaskService(q, pool, nil, events.New())}
	e, err := svc.EnsureStageExecution(ctx, EnsureWorkflowExecutionParams{WorkspaceID: wsID, InstanceID: runID, StageID: run.CurrentStageID, ExecutorID: agentID, ExpectedRevision: run.Revision, InputSnapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	action, err := q.ClaimWorkflowExecutionAction(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchStageAction(ctx, wsID, action.ID, action.ClaimToken); err != nil {
		t.Fatal(err)
	}
	e, err = q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: wsID, ID: e.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitResult(ctx, SubmitWorkflowResultParams{WorkspaceID: wsID, InstanceID: runID, ExecutionID: e.ID, ActorType: "member", ActorID: run.CreatedBy, Submission: []byte(`{"data":{},"artifacts":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, e.MainTaskID); err != nil {
		t.Fatal(err)
	}
	request, err := svc.PrepareAgentReview(ctx, wsID, runID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, err = svc.DispatchAgentReview(ctx, wsID, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='failed',failure_reason='agent_error.provider_network',attempt=3,max_attempts=3,completed_at=now() WHERE id=$1`, request.TaskID); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SILIECO_REVIEW_BROWSER_MODE") == "missing" {
		if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed',failure_reason=NULL WHERE id=$1`, request.TaskID); err != nil {
			t.Fatal(err)
		}
	}

}
