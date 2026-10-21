package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowAgentReviewFreezesDeliveryAndInvalidatesOldRequest(t *testing.T) {
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
			prepare := func() (db.WorkflowReviewRequest, error) {
				return svc.PrepareAgentReview(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID)
			}
			if _, err := prepare(); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("incomplete delivery prepared: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, e.MainTaskID); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			ids := make(chan pgtype.UUID, 2)
			errs := make(chan error, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); r, err := prepare(); ids <- r.ID; errs <- err }()
			}
			wg.Wait()
			close(ids)
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var id pgtype.UUID
			for next := range ids {
				if id.Valid && id != next {
					t.Fatal("duplicate request")
				}
				id = next
			}
			first, err := prepare()
			if err != nil || first.ReviewerID != e.ExecutorID || first.ResultRevision != 1 || !first.ResultID.Valid || len(first.BasisHash) != 64 || len(first.BasisSnapshot) == 0 {
				t.Fatalf("unbound review: %+v %v", first, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE issue SET title='Revised delivery',updated_at=now() WHERE id=$1`, e.MainIssueID); err != nil {
				t.Fatal(err)
			}
			second, err := prepare()
			if err != nil || second.ID == first.ID || second.BasisHash == first.BasisHash {
				t.Fatalf("changed evidence reused request: %+v %v", second, err)
			}
			var status string
			if err := pool.QueryRow(ctx, `SELECT status FROM workflow_review_request WHERE id=$1`, first.ID).Scan(&status); err != nil || status != "invalidated" {
				t.Fatalf("old request still live: %s %v", status, err)
			}
			if _, err := svc.DispatchAgentReview(ctx, p.WorkspaceID, first.ID); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("invalidated request dispatched: %v", err)
			}
			constraint := "review_dispatch_" + strings.ReplaceAll(util.UUIDToString(second.ID), "-", "")
			if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE workflow_review_request ADD CONSTRAINT %s CHECK(id <> '%s' OR task_id IS NULL)`, constraint, util.UUIDToString(second.ID))); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pool.Exec(ctx, "ALTER TABLE workflow_review_request DROP CONSTRAINT IF EXISTS "+constraint) })
			if _, err := svc.DispatchAgentReview(ctx, p.WorkspaceID, second.ID); err == nil || !strings.Contains(err.Error(), constraint) {
				t.Fatalf("missing binding failure: %v", err)
			}
			var queued int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND id<>$2`, e.MainIssueID, e.MainTaskID).Scan(&queued); err != nil || queued != 0 {
				t.Fatalf("orphan review call: %d %v", queued, err)
			}
			if _, err := pool.Exec(ctx, "ALTER TABLE workflow_review_request DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			dispatchErrors := make(chan error, 2)
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := svc.DispatchAgentReview(ctx, p.WorkspaceID, second.ID)
					dispatchErrors <- err
				}()
			}
			wg.Wait()
			close(dispatchErrors)
			for err := range dispatchErrors {
				if err != nil {
					t.Fatal(err)
				}
			}
			bound, err := svc.DispatchAgentReview(ctx, p.WorkspaceID, second.ID)
			reviewCall := bound.TaskID
			if err != nil || !reviewCall.Valid || bound.Status != "queued" {
				t.Fatalf("dispatch: %+v %v", bound, err)
			}
			var fresh bool
			var note string
			var count int
			if err := pool.QueryRow(ctx, `SELECT force_fresh_session,handoff_note FROM agent_task_queue WHERE id=$1 AND workflow_stage_execution_id IS NULL`, reviewCall).Scan(&fresh, &note); err != nil || !fresh || !strings.Contains(note, "silieco workflow agent-review "+util.UUIDToString(p.InstanceID)) || !strings.Contains(note, "silieco workflow agent-decision "+util.UUIDToString(p.InstanceID)) || !strings.Contains(note, second.BasisHash) || !strings.Contains(note, util.UUIDToString(second.ResultID)) {
				t.Fatalf("review context: %v %s %v", fresh, note, err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE workflow_review_request_id=$1`, second.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("duplicate dispatch: %d %v", count, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET workflow_stage_execution_id=$2 WHERE id=$1`, reviewCall, e.ID); err == nil {
				t.Fatal("review call became execution call")
			}
			again, err := prepare()
			if err != nil || again.ID != second.ID || again.BasisHash != second.BasisHash {
				t.Fatalf("review call changed delivery basis: %+v %v", again, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET issue_id=NULL WHERE id=$1`, reviewCall); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: e.ExecutorID, PrepareLeaseSecs: 60}); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("review call used ordinary claim: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET issue_id=$2 WHERE id=$1`, reviewCall, e.MainIssueID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent SET archived_at=now() WHERE id=$1`, e.ExecutorID); err != nil {
				t.Fatal(err)
			}
			if _, err := prepare(); !errors.Is(err, ErrWorkflowReviewAccess) {
				t.Fatalf("archived reviewer accepted: %v", err)
			}
		})
	}
}
