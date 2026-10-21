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

func TestWorkflowAgentDecisionRequiresCompletedReviewAndHybridHuman(t *testing.T) {
	for _, kind := range []string{"agent", "hybrid"} {
		t.Run(kind, func(t *testing.T) {
			pool, svc, p, _ := workflowResultFixture(t)
			ctx := context.Background()
			if _, err := svc.SubmitResult(ctx, p); err != nil {
				t.Fatal(err)
			}
			e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
			if err != nil {
				t.Fatal(err)
			}
			gate := fmt.Sprintf(`{"type":%q,"decider":%q,"human_decider":%q}`, kind, util.UUIDToString(e.ExecutorID), util.UUIDToString(p.ActorID))
			if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',$2::jsonb) WHERE id=$1`, e.ID, gate); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, e.MainTaskID); err != nil {
				t.Fatal(err)
			}
			// Concurrent scans recover durable state without producing another call.
			var scans sync.WaitGroup
			scanErrors := make(chan error, 2)
			for range 2 {
				scans.Add(1)
				go func() { defer scans.Done(); scanErrors <- svc.ScanAgentReviews(ctx) }()
			}
			scans.Wait()
			close(scanErrors)
			for err := range scanErrors {
				if err != nil {
					t.Fatal(err)
				}
			}
			request, err := svc.PrepareAgentReview(ctx, p.WorkspaceID, p.InstanceID, e.ID)
			if err != nil {
				t.Fatal(err)
			}
			request, err = svc.DispatchAgentReview(ctx, p.WorkspaceID, request.ID)
			if err != nil {
				t.Fatal(err)
			}
			var reviewCalls int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE workflow_review_request_id=$1`, request.ID).Scan(&reviewCalls); err != nil || reviewCalls != 1 {
				t.Fatalf("scan duplicated review calls: %d %v", reviewCalls, err)
			}
			decisionParams := RecordWorkflowAgentDecisionParams{WorkspaceID: p.WorkspaceID, RequestID: request.ID, ActorID: e.ExecutorID, TaskID: request.TaskID, ExpectedResultRevision: request.ResultRevision, ExpectedBasisHash: request.BasisHash, Outcome: "approved"}
			if _, err := svc.RecordAgentDecision(ctx, decisionParams); !errors.Is(err, ErrWorkflowReviewAccess) {
				t.Fatalf("queued call decided: %v", err)
			}
			var wg sync.WaitGroup
			claims := make(chan *db.AgentTaskQueue, 2)
			claimErrors := make(chan error, 2)
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					task, err := svc.TaskService.ClaimTask(ctx, e.ExecutorID)
					claims <- task
					claimErrors <- err
				}()
			}
			wg.Wait()
			close(claims)
			close(claimErrors)
			for err := range claimErrors {
				if err != nil {
					t.Fatal(err)
				}
			}
			var claimed *db.AgentTaskQueue
			count := 0
			for task := range claims {
				if task != nil {
					claimed = task
					count++
				}
			}
			if count != 1 || claimed.ID != request.TaskID {
				t.Fatalf("review claim count %d: %+v", count, claimed)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='waiting' WHERE id=$1`, p.InstanceID); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.TaskService.FinalizeTaskClaim(ctx, *claimed, db.CreateTaskTokenParams{}, nil, false); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("paused review received token: %v", err)
			}
			if _, err := svc.TaskService.StartTask(ctx, request.TaskID); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("paused review started: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=$1`, p.InstanceID); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.TaskService.StartTask(ctx, request.TaskID); err != nil {
				t.Fatal(err)
			}
			wrong := decisionParams
			wrong.TaskID = e.MainTaskID
			if _, err := svc.RecordAgentDecision(ctx, wrong); !errors.Is(err, ErrWorkflowReviewAccess) {
				t.Fatalf("executor call reviewed: %v", err)
			}
			wrong = decisionParams
			wrong.ExpectedResultRevision++
			if _, err := svc.RecordAgentDecision(ctx, wrong); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("wrong revision reviewed: %v", err)
			}
			decision, err := svc.RecordAgentDecision(ctx, decisionParams)
			if err != nil {
				t.Fatal(err)
			}
			duplicate, err := svc.RecordAgentDecision(ctx, decisionParams)
			if err != nil || duplicate.ID != decision.ID {
				t.Fatalf("duplicate decision: %+v %v", duplicate, err)
			}
			wrong = decisionParams
			wrong.Outcome = "rejected"
			if _, err := svc.RecordAgentDecision(ctx, wrong); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("decision rewritten: %v", err)
			}
			if err := svc.ScanAgentReviews(ctx); err != nil {
				t.Fatal(err)
			}
			stillWaiting, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: e.ID})
			if err != nil || stillWaiting.Status != "awaiting_review" {
				t.Fatalf("running reviewer advanced: %+v %v", stillWaiting, err)
			}
			if kind == "agent" {
				if _, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, e.ID); !errors.Is(err, ErrWorkflowExecutionConflict) {
					t.Fatalf("running reviewer accepted: %v", err)
				}
			} else {
				if _, err := svc.PrepareHumanReview(ctx, p.WorkspaceID, p.InstanceID, e.ID, p.ActorID); !errors.Is(err, ErrWorkflowExecutionConflict) {
					t.Fatalf("unfinished reviewer bypassed: %v", err)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, request.TaskID); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := svc.ScanAgentReviews(ctx); err != nil {
					t.Fatal(err)
				}
			}
			afterScan, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: e.ID})
			wantStatus := "succeeded"
			if kind == "hybrid" {
				wantStatus = "awaiting_review"
			}
			if err != nil || afterScan.Status != wantStatus {
				t.Fatalf("scan acceptance: %+v %v", afterScan, err)
			}
			if kind == "agent" {
				run, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, e.ID)
				if err != nil || run.Status != "completed" {
					t.Fatalf("agent acceptance: %+v %v", run, err)
				}
				if _, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, e.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := svc.AcceptAgentReviewedExecution(ctx, p.WorkspaceID, p.InstanceID, e.ID); !errors.Is(err, ErrWorkflowReviewAccess) {
					t.Fatalf("hybrid skipped human: %v", err)
				}
				basis, err := svc.PrepareHumanReview(ctx, p.WorkspaceID, p.InstanceID, e.ID, p.ActorID)
				if err != nil {
					t.Fatal(err)
				}
				if basis.Hash != request.BasisHash {
					t.Fatal("human reviewed different evidence")
				}
				if _, err := svc.DecideHumanExecution(ctx, RecordWorkflowHumanDecisionParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, ExecutionID: e.ID, ActorID: p.ActorID, ExpectedResultRevision: basis.ResultRevision, ExpectedBasisHash: basis.Hash, Outcome: "approved"}); err != nil {
					t.Fatal(err)
				}
			}
			replayed, err := svc.RecordAgentDecision(ctx, decisionParams)
			if err != nil || replayed.ID != decision.ID {
				t.Fatalf("accepted decision replay: %+v %v", replayed, err)
			}
			for _, change := range []func(*RecordWorkflowAgentDecisionParams){
				func(p *RecordWorkflowAgentDecisionParams) { p.ExpectedResultRevision++ },
				func(p *RecordWorkflowAgentDecisionParams) {
					p.ExpectedBasisHash = "b" + p.ExpectedBasisHash[1:]
					if p.ExpectedBasisHash == request.BasisHash {
						p.ExpectedBasisHash = "c" + p.ExpectedBasisHash[1:]
					}
				},
				func(p *RecordWorkflowAgentDecisionParams) { p.Outcome = "rejected" },
				func(p *RecordWorkflowAgentDecisionParams) { p.Note = "changed" },
			} {
				wrong := decisionParams
				change(&wrong)
				if _, err := svc.RecordAgentDecision(ctx, wrong); !errors.Is(err, ErrWorkflowExecutionConflict) {
					t.Fatalf("accepted decision changed: %v", err)
				}
			}
			wrong = decisionParams
			wrong.TaskID = e.MainTaskID
			if _, err := svc.RecordAgentDecision(ctx, wrong); !errors.Is(err, ErrWorkflowReviewAccess) {
				t.Fatalf("executor replayed reviewer decision: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent SET archived_at=now() WHERE id=$1`, e.ExecutorID); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.RecordAgentDecision(ctx, decisionParams); !errors.Is(err, ErrWorkflowReviewAccess) {
				t.Fatalf("archived reviewer replayed: %v", err)
			}

		})
	}
}
