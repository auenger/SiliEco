package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowReviewFailureCommitsWithoutOrdinaryRetry(t *testing.T) {
	for _, outcome := range []string{"", "approved"} {
		t.Run("decision_"+outcome, func(t *testing.T) {
			pool, svc, p, _ := workflowResultFixture(t)
			ctx := context.Background()
			if _, err := svc.SubmitResult(ctx, p); err != nil {
				t.Fatal(err)
			}
			e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
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
			request, err := svc.PrepareAgentReview(ctx, p.WorkspaceID, p.InstanceID, e.ID)
			if err != nil {
				t.Fatal(err)
			}
			request, err = svc.DispatchAgentReview(ctx, p.WorkspaceID, request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',attempt=1,max_attempts=2 WHERE id=$1`, request.TaskID); err != nil {
				t.Fatal(err)
			}
			decisionParams := RecordWorkflowAgentDecisionParams{WorkspaceID: p.WorkspaceID, RequestID: request.ID, ActorID: e.ExecutorID, TaskID: request.TaskID, ExpectedResultRevision: request.ResultRevision, ExpectedBasisHash: request.BasisHash, Outcome: "approved"}
			if outcome != "" {
				if _, err := svc.RecordAgentDecision(ctx, decisionParams); err != nil {
					t.Fatal(err)
				}
			}
			failed, err := svc.TaskService.FailTask(ctx, request.TaskID, "API Error: Connection closed mid-response.", "", "", "agent_error.provider_network", false, "")
			if err != nil || failed == nil || failed.Status != "failed" {
				t.Fatalf("review failure rolled back: %+v %v", failed, err)
			}
			stored, err := svc.Queries.GetAgentTask(ctx, request.TaskID)
			if err != nil || stored.Status != "failed" || !stored.FailureReason.Valid {
				t.Fatalf("failure missing: %+v %v", stored, err)
			}
			if retry, err := svc.TaskService.MaybeRetryFailedTask(ctx, stored); err != nil || retry != nil {
				t.Fatalf("ordinary review retry: %+v %v", retry, err)
			}
			if _, err := svc.TaskService.FailTask(ctx, request.TaskID, "duplicate failure", "", "", "agent_error.provider_network", false, ""); err != nil {
				t.Fatal(err)
			}
			currentRequest, err := svc.PrepareAgentReview(ctx, p.WorkspaceID, p.InstanceID, e.ID)
			if err != nil || currentRequest.ID != request.ID || currentRequest.TaskID != request.TaskID {
				t.Fatalf("failure changed delivery evidence or redispatched: %+v %v", currentRequest, err)
			}
			if _, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, e.ID); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("failed reviewer accepted: %v", err)
			}
			if _, err := svc.RecordAgentDecision(ctx, decisionParams); !errors.Is(err, ErrWorkflowReviewAccess) && !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("failed reviewer decided: %v", err)
			}
			var children int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1 OR retry_of_task_id=$1`, request.TaskID).Scan(&children); err != nil || children != 0 {
				t.Fatalf("unbound retry children: %d %v", children, err)
			}

			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET max_attempts=1 WHERE id=$1`, request.TaskID); err != nil {
				t.Fatal(err)
			}
			if err := svc.RetryAgentReview(ctx, p.WorkspaceID, request.ID, request.TaskID); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("disabled retry ran: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET max_attempts=2 WHERE id=$1`, request.TaskID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='waiting' WHERE id=$1`, p.InstanceID); err != nil {
				t.Fatal(err)
			}
			if err := svc.RetryAgentReview(ctx, p.WorkspaceID, request.ID, request.TaskID); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("paused retry ran: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=$1`, p.InstanceID); err != nil {
				t.Fatal(err)
			}
			var scans sync.WaitGroup
			for range 2 {
				scans.Add(1)
				go func() {
					defer scans.Done()
					if err := svc.ScanAgentReviews(ctx); err != nil {
						t.Error(err)
					}
				}()
			}
			scans.Wait()

			retried, err := svc.PrepareAgentReview(ctx, p.WorkspaceID, p.InstanceID, e.ID)
			if err != nil || retried.ID != request.ID || retried.TaskID == request.TaskID || retried.Status != "queued" {
				t.Fatalf("review not retried: %+v %v", retried, err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, request.TaskID).Scan(&children); err != nil || children != 1 {
				t.Fatalf("concurrent retry count: %d %v", children, err)
			}
			child, err := svc.Queries.GetAgentTask(ctx, retried.TaskID)
			if err != nil || child.Attempt != 2 || child.ParentTaskID != request.TaskID || child.RetryOfTaskID != request.TaskID || !child.ForceFreshSession {
				t.Fatalf("invalid retry lineage: %+v %v", child, err)
			}
			if _, err := svc.RecordAgentDecision(ctx, decisionParams); !errors.Is(err, ErrWorkflowReviewAccess) {
				t.Fatalf("old reviewer decided after retry: %v", err)
			}
			if _, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, e.ID); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("old approval accepted retry: %v", err)
			}
			claimed, err := svc.TaskService.ClaimTask(ctx, e.ExecutorID)
			if err != nil || claimed == nil || claimed.ID != retried.TaskID {
				t.Fatalf("retry claim: %+v %v", claimed, err)
			}
			if _, err := svc.TaskService.StartTask(ctx, retried.TaskID); err != nil {
				t.Fatal(err)
			}
			decisionParams.TaskID = retried.TaskID
			newDecision, err := svc.RecordAgentDecision(ctx, decisionParams)
			if err != nil || newDecision.ReviewTaskID != retried.TaskID {
				t.Fatalf("retry decision: %+v %v", newDecision, err)
			}
			var decisions int
			wantDecisions := 1
			if outcome != "" {
				wantDecisions = 2
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_gate_decision WHERE execution_id=$1 AND review_step='agent'`, e.ID).Scan(&decisions); err != nil || decisions != wantDecisions {
				t.Fatalf("review history lost: %d %v", decisions, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, retried.TaskID); err != nil {
				t.Fatal(err)
			}
			if err := svc.ScanAgentReviews(ctx); err != nil {
				t.Fatal(err)
			}
			accepted, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: e.ID})
			if err != nil || accepted.Status != "succeeded" {
				t.Fatalf("retry not accepted: %+v %v", accepted, err)
			}

		})
	}
}

func TestWorkflowReviewRetryUsesExplicitPolicy(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	gate := fmt.Sprintf(`{"type":"agent","decider":%q}`, util.UUIDToString(e.ExecutorID))
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(jsonb_set(config_snapshot,'{gate}',$2::jsonb),'{retry_policy}','{"max_attempts":2,"backoff_seconds":60}') WHERE id=$1`, e.ID, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, e.MainTaskID); err != nil {
		t.Fatal(err)
	}
	request, err := svc.PrepareAgentReview(ctx, p.WorkspaceID, p.InstanceID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, err = svc.DispatchAgentReview(ctx, p.WorkspaceID, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Queries.GetAgentTask(ctx, request.TaskID)
	if err != nil || task.MaxAttempts != 2 {
		t.Fatalf("initial review budget: %d %v", task.MaxAttempts, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='failed',failure_reason='agent_error.provider_network',completed_at=now() WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RetryAgentReview(ctx, p.WorkspaceID, request.ID, task.ID); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("retry ignored delay: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET completed_at=now()-interval '61 seconds' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RetryAgentReview(ctx, p.WorkspaceID, request.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	request, err = svc.Queries.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: p.WorkspaceID, ID: request.ID})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.Queries.GetAgentTask(ctx, request.TaskID)
	if err != nil || child.Attempt != 2 || child.MaxAttempts != 2 {
		t.Fatalf("review child budget: %+v %v", child, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='failed',failure_reason='agent_error.provider_network',completed_at=now()-interval '61 seconds' WHERE id=$1`, child.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RetryAgentReview(ctx, p.WorkspaceID, request.ID, child.ID); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("provider widened review budget: %v", err)
	}
}
