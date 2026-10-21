package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func workflowReviewRecoveryFixture(t *testing.T, retryPolicy ...string) (*pgxpool.Pool, *WorkflowExecutionService, RecoverWorkflowAgentReviewParams) {
	t.Helper()
	pool, svc, result, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: result.WorkspaceID, ID: result.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	gate := fmt.Sprintf(`{"type":"agent","decider":%q}`, util.UUIDToString(e.ExecutorID))
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',$2::jsonb) WHERE id=$1`, e.ID, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, e.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if len(retryPolicy) > 0 {
		if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{retry_policy}',$2::jsonb) WHERE id=$1`, e.ID, retryPolicy[0]); err != nil {
			t.Fatal(err)
		}
	}
	request, err := svc.PrepareAgentReview(ctx, result.WorkspaceID, result.InstanceID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, err = svc.DispatchAgentReview(ctx, result.WorkspaceID, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',attempt=3,max_attempts=3,session_id='failed-review-session',work_dir='/old-review-workdir' WHERE id=$1`, request.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordAgentDecision(ctx, RecordWorkflowAgentDecisionParams{WorkspaceID: result.WorkspaceID, RequestID: request.ID, ActorID: e.ExecutorID, TaskID: request.TaskID, ExpectedResultRevision: request.ResultRevision, ExpectedBasisHash: request.BasisHash, Outcome: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TaskService.FailTask(ctx, request.TaskID, "Connection closed mid-response", "", "", "agent_error.provider_network", false, ""); err != nil {
		t.Fatal(err)
	}
	return pool, svc, RecoverWorkflowAgentReviewParams{WorkspaceID: result.WorkspaceID, InstanceID: result.InstanceID, ExecutionID: e.ID, RequestID: request.ID, FailedTaskID: request.TaskID, ActorID: result.ActorID, ExpectedResultRevision: request.ResultRevision, ExpectedBasisHash: request.BasisHash, Reason: "Network repaired; review the same delivery again"}
}

func TestWorkflowReviewRecoveryIsAuditedIdempotentAndRequiresNewApproval(t *testing.T) {
	pool, svc, p := workflowReviewRecoveryFixture(t)
	ctx := context.Background()
	parent, err := svc.Queries.GetAgentTask(ctx, p.FailedTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_token(token_hash,task_id,agent_id,workspace_id,user_id,expires_at) VALUES($5,$1,$2,$3,$4,now()+interval '1 hour')`, parent.ID, parent.AgentID, p.WorkspaceID, p.ActorID, "recover-review-"+util.UUIDToString(parent.ID)); err != nil {
		t.Fatal(err)
	}
	status, err := svc.InspectAgentReviewStatus(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID, p.ActorID)
	if err != nil || status.Phase != "failed" || !status.CanRecover || status.RecoveryAttempts != 3 {
		t.Fatalf("exhausted review not recoverable: %+v %v", status, err)
	}
	var wg sync.WaitGroup
	records := make(chan db.WorkflowReviewRecovery, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, err := svc.RecoverAgentReview(ctx, p)
			if err != nil {
				t.Error(err)
				return
			}
			records <- record
		}()
	}
	wg.Wait()
	close(records)
	var recovered db.WorkflowReviewRecovery
	for record := range records {
		if recovered.ID.Valid && recovered.ID != record.ID {
			t.Fatal("duplicate manual recovery")
		}
		recovered = record
	}
	if !recovered.ID.Valid || recovered.AllowedAttempts != 3 || recovered.MaxAttempts != 6 || recovered.ActorID != p.ActorID || recovered.Reason != p.Reason {
		t.Fatalf("recovery audit missing: %+v", recovered)
	}
	child, err := svc.Queries.GetAgentTask(ctx, recovered.NewTaskID)
	if err != nil || child.Attempt != 4 || child.MaxAttempts != 6 || child.SessionID.Valid || !child.ForceFreshSession || child.WorkflowReviewRequestID != p.RequestID || child.ParentTaskID != p.FailedTaskID {
		t.Fatalf("invalid recovery call: %+v %v", child, err)
	}
	var tokens, children int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_token WHERE task_id=$1),(SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1)`, parent.ID).Scan(&tokens, &children); err != nil || tokens != 0 || children != 1 {
		t.Fatalf("old credentials or duplicate calls: %d %d %v", tokens, children, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("old failed approval accepted new call: %v", err)
	}
	if _, err := svc.RecordAgentDecision(ctx, RecordWorkflowAgentDecisionParams{WorkspaceID: p.WorkspaceID, RequestID: p.RequestID, ActorID: child.AgentID, TaskID: child.ID, ExpectedResultRevision: p.ExpectedResultRevision, ExpectedBasisHash: p.ExpectedBasisHash, Outcome: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID); err != nil {
		t.Fatal(err)
	}
	replay, err := svc.RecoverAgentReview(ctx, p)
	if err != nil || replay.NewTaskID != child.ID {
		t.Fatalf("completed recovery replay dispatched again: %+v %v", replay, err)
	}
}

func TestWorkflowReviewRecoveryRejectsUnsafeRequests(t *testing.T) {
	for _, scenario := range []string{"actor", "basis", "reason", "paused", "automatic_pending", "archived"} {
		t.Run(scenario, func(t *testing.T) {
			pool, svc, p := workflowReviewRecoveryFixture(t)
			ctx := context.Background()
			switch scenario {
			case "actor":
				p.ActorID = util.MustParseUUID("11111111-1111-4111-8111-111111111111")
			case "basis":
				p.ExpectedBasisHash = strings.Repeat("a", 64)
			case "reason":
				p.Reason = " "
			case "paused":
				if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='waiting' WHERE id=$1`, p.InstanceID); err != nil {
					t.Fatal(err)
				}
			case "automatic_pending":
				if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET attempt=1,max_attempts=3 WHERE id=$1`, p.FailedTaskID); err != nil {
					t.Fatal(err)
				}
			case "archived":
				if _, err := pool.Exec(ctx, `UPDATE agent SET archived_at=now() WHERE id=(SELECT reviewer_id FROM workflow_review_request WHERE id=$1)`, p.RequestID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.RecoverAgentReview(ctx, p); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			var children, records int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1),(SELECT count(*) FROM workflow_review_recovery WHERE failed_task_id=$1)`, p.FailedTaskID).Scan(&children, &records); err != nil || children != 0 || records != 0 {
				t.Fatalf("rejected recovery left writes: %d %d %v", children, records, err)
			}
		})
	}
}

func TestWorkflowReviewRecoveryCanRebindRuntimeWithoutOldWorkdir(t *testing.T) {
	pool, svc, p := workflowReviewRecoveryFixture(t)
	ctx := context.Background()
	otherAgentID := createClaimCapacityFixture(t, ctx, pool)
	otherAgent, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(otherAgentID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET runtime_id=$2,attempt=1,max_attempts=3 WHERE id=$1`, p.FailedTaskID, otherAgent.RuntimeID); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.RecoverAgentReview(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.Queries.GetAgentTask(ctx, recovered.NewTaskID)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := svc.Queries.GetAgent(ctx, child.AgentID)
	if err != nil || child.RuntimeID != agent.RuntimeID || child.WorkDir.Valid || child.SessionID.Valid {
		t.Fatalf("old host state reused: %+v %v", child, err)
	}
}

func TestWorkflowReviewRecoveryCompletedCallRequiresMissingDecision(t *testing.T) {
	for _, outcome := range []string{"missing", "approved", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			pool, svc, p := workflowReviewRecoveryFixture(t)
			ctx := context.Background()
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, p.FailedTaskID); err != nil {
				t.Fatal(err)
			}
			if outcome == "missing" {
				if _, err := pool.Exec(ctx, `DELETE FROM workflow_gate_decision WHERE review_task_id=$1`, p.FailedTaskID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := pool.Exec(ctx, `UPDATE workflow_gate_decision SET outcome=$2 WHERE review_task_id=$1`, p.FailedTaskID, outcome); err != nil {
					t.Fatal(err)
				}
			}
			status, err := svc.InspectAgentReviewStatus(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID, p.ActorID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := svc.RecoverAgentReview(ctx, p)
			if outcome != "missing" {
				if status.CanRecover || status.Phase != outcome || !errors.Is(err, ErrWorkflowExecutionConflict) {
					t.Fatalf("reviewed call recovered: %+v %v", status, err)
				}
				return
			}
			if err != nil || status.Phase != "missing_decision" || !status.CanRecover || !result.NewTaskID.Valid {
				t.Fatalf("incomplete review cannot recover: %+v %+v %v", status, result, err)
			}
			parent, err := svc.Queries.GetAgentTask(ctx, p.FailedTaskID)
			if err != nil || parent.Status != "completed" {
				t.Fatalf("historical completion rewritten: %+v %v", parent, err)
			}
			child, err := svc.Queries.GetAgentTask(ctx, result.NewTaskID)
			if err != nil || child.Status != "queued" || child.SessionID.Valid || !child.ForceFreshSession {
				t.Fatalf("invalid new review: %+v %v", child, err)
			}
			replay, err := svc.RecoverAgentReview(ctx, p)
			if err != nil || replay.ID != result.ID {
				t.Fatalf("duplicate recovery: %+v %v", replay, err)
			}
		})
	}
}

func TestWorkflowReviewRecoveryRacesLateDecision(t *testing.T) {
	pool, svc, p := workflowReviewRecoveryFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM workflow_gate_decision WHERE review_task_id=$1`, p.FailedTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, p.FailedTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_review_request SET status='queued' WHERE id=$1`, p.RequestID); err != nil {
		t.Fatal(err)
	}
	parent, err := svc.Queries.GetAgentTask(ctx, p.FailedTaskID)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; _, err := svc.RecoverAgentReview(ctx, p); results <- err }()
	go func() {
		<-start
		_, err := svc.RecordAgentDecision(ctx, RecordWorkflowAgentDecisionParams{WorkspaceID: p.WorkspaceID, RequestID: p.RequestID, TaskID: p.FailedTaskID, ActorID: parent.AgentID, ExpectedResultRevision: p.ExpectedResultRevision, ExpectedBasisHash: p.ExpectedBasisHash, Outcome: "approved"})
		results <- err
	}()
	close(start)
	success := 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if !errors.Is(err, ErrWorkflowExecutionConflict) && !errors.Is(err, ErrWorkflowReviewAccess) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("recovery and late decision both took effect: %d", success)
	}
}

func TestWorkflowReviewRecoveryGrantSurvivesExplicitRetryPolicy(t *testing.T) {
	_, svc, p := workflowReviewRecoveryFixture(t, `{"max_attempts":2,"backoff_seconds":0}`)
	ctx := context.Background()
	if _, err := svc.RecoverAgentReview(ctx, p); err != nil {
		t.Fatal(err)
	}
	request, err := svc.Queries.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: p.WorkspaceID, ID: p.RequestID})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.Queries.GetAgentTask(ctx, request.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	ceiling, _, err := taskRetrySettings(ctx, svc.Queries, child, "agent_error.provider_network")
	if err != nil || ceiling != child.MaxAttempts || ceiling <= child.Attempt {
		t.Fatalf("manual grant lost: ceiling=%d attempt=%d max=%d err=%v", ceiling, child.Attempt, child.MaxAttempts, err)
	}
}
