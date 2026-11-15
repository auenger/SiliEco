package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestSameTaskRevisionComparesInstants(t *testing.T) {
	instant := time.Date(2026, time.September, 20, 17, 0, 57, 424956000, time.UTC)
	utc := pgtype.Timestamptz{Time: instant, Valid: true}
	sameInstantDifferentLocation := pgtype.Timestamptz{
		Time:  instant.In(time.FixedZone("UTC equivalent", 0)),
		Valid: true,
	}

	if !sameTaskRevision(utc, sameInstantDifferentLocation) {
		t.Fatal("the same instant with a different location representation was treated as stale")
	}
	if sameTaskRevision(utc, pgtype.Timestamptz{Time: instant.Add(time.Microsecond), Valid: true}) {
		t.Fatal("different revision instants were treated as equal")
	}
	if sameTaskRevision(utc, pgtype.Timestamptz{}) {
		t.Fatal("valid and invalid revisions were treated as equal")
	}
}

func TestWorkflowMembershipWaitsForRunAndRevalidatesClosure(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	projectID := util.MustParseUUID(uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO project(id,workspace_id,title) VALUES ($1,$2,'Mutation locking')`, projectID, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM project WHERE id=$1`, projectID) })
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET project_id=$2 WHERE id=$1`, p.InstanceID, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task SET project_id=$2 WHERE id=$1`, execution.MainTaskID, projectID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := svc.Queries.WithTx(tx).LockWorkflowInstanceInWorkspace(ctx, db.LockWorkflowInstanceInWorkspaceParams{ID: p.InstanceID, WorkspaceID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := svc.MoveTaskStage(short, p.WorkspaceID, execution.MainTaskID, p.InstanceID, pgtype.UUID{}); err == nil {
		t.Fatal("membership escaped Run lock")
	}
	if _, err := tx.Exec(ctx, `UPDATE workflow_instance SET status='completed' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveTaskStage(ctx, p.WorkspaceID, execution.MainTaskID, p.InstanceID, pgtype.UUID{}); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("closed Run detached: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	detached, err := svc.MoveTaskStage(ctx, p.WorkspaceID, execution.MainTaskID, p.InstanceID, pgtype.UUID{})
	if err != nil || detached.WorkflowInstanceID.Valid {
		t.Fatalf("detach: %+v %v", detached, err)
	}
	attached, err := svc.MoveTaskStage(ctx, p.WorkspaceID, execution.MainTaskID, p.InstanceID, execution.WorkflowStageID)
	if err != nil || attached.WorkflowInstanceID != p.InstanceID {
		t.Fatalf("attach: %+v %v", attached, err)
	}
	// Opposite moves acquire both Run locks in the same order.
	otherRun := util.MustParseUUID(uuid.NewString())
	otherStage := util.MustParseUUID(uuid.NewString())
	otherTask := util.MustParseUUID(uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO workflow_instance(id,workspace_id,workflow_id,workflow_version_id,title,status,created_by,project_id) SELECT $2,workspace_id,workflow_id,workflow_version_id,'Other Run','active',created_by,project_id FROM workflow_instance WHERE id=$1`, p.InstanceID, otherRun); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workflow_instance_stage(id,workspace_id,workflow_instance_id,stable_key,name,position) VALUES ($1,$2,$3,'other','Other stage',0)`, otherStage, p.WorkspaceID, otherRun); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task(id,workspace_id,title,status,creator_type,creator_id,project_id,workflow_instance_id,workflow_stage_id) VALUES ($1,$2,'Other task','todo','member',$3,$4,$5,$6)`, otherTask, p.WorkspaceID, p.ActorID, projectID, otherRun, otherStage); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := svc.MoveTaskStage(deadline, p.WorkspaceID, execution.MainTaskID, otherRun, otherStage)
		results <- err
	}()
	go func() {
		<-start
		_, err := svc.MoveTaskStage(deadline, p.WorkspaceID, otherTask, p.InstanceID, execution.WorkflowStageID)
		results <- err
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("opposite Run moves failed: %v", err)
		}
	}
	if _, err := svc.MoveTaskStage(ctx, p.WorkspaceID, execution.MainTaskID, p.InstanceID, util.MustParseUUID(uuid.NewString())); err == nil {
		t.Fatal("foreign stage attached")
	}
}

func TestWorkflowGenericTaskUpdateRevalidatesState(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	projectID := util.MustParseUUID(uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO project(id,workspace_id,title) VALUES ($1,$2,'Generic updates')`, projectID, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM project WHERE id=$1`, projectID) })
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET project_id=$2 WHERE id=$1`, p.InstanceID, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task SET project_id=$2 WHERE id=$1`, execution.MainTaskID, projectID); err != nil {
		t.Fatal(err)
	}
	observed, err := svc.Queries.GetTaskInWorkspace(ctx, db.GetTaskInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: execution.MainTaskID})
	if err != nil {
		t.Fatal(err)
	}
	params := db.UpdateTaskParams{ID: observed.ID, Status: pgtype.Text{String: "done", Valid: true}, AssigneeType: observed.AssigneeType, AssigneeID: observed.AssigneeID, StartDate: observed.StartDate, DueDate: observed.DueDate, ParentTaskID: observed.ParentTaskID, ProjectID: observed.ProjectID, Stage: observed.Stage, WorkflowInstanceID: observed.WorkflowInstanceID, WorkflowStageID: observed.WorkflowStageID}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := svc.Queries.WithTx(tx).LockWorkflowInstanceInWorkspace(ctx, db.LockWorkflowInstanceInWorkspaceParams{ID: p.InstanceID, WorkspaceID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := svc.UpdateTaskWithWorkflowLock(short, observed, params); err == nil {
		t.Fatal("ordinary edit escaped Run lock")
	}
	if _, err := tx.Exec(ctx, `UPDATE workflow_instance SET status='completed' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTaskWithWorkflowLock(ctx, observed, params); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("closed Run changed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	wrongProject := params
	wrongProject.ProjectID = util.MustParseUUID(uuid.NewString())
	if _, err := svc.UpdateTaskWithWorkflowLock(ctx, observed, wrongProject); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("cross-project update accepted: %v", err)
	}
	updated, err := svc.UpdateTaskWithWorkflowLock(ctx, observed, params)
	if err != nil || updated.Status != "done" {
		t.Fatalf("ordinary update %+v %v", updated, err)
	}
	params.Status = pgtype.Text{String: "in_progress", Valid: true}
	if _, err := svc.UpdateTaskWithWorkflowLock(ctx, observed, params); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("stale ordinary update accepted: %v", err)
	}
	if _, err := svc.UpdateTaskStatusWithWorkflowLock(ctx, observed, "in_progress"); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("stale automatic status accepted: %v", err)
	}
	automatic, err := svc.UpdateTaskStatusWithWorkflowLock(ctx, updated, "in_progress")
	if err != nil || automatic.AssigneeID != updated.AssigneeID || automatic.ProjectID != updated.ProjectID || automatic.WorkflowStageID != updated.WorkflowStageID {
		t.Fatalf("automatic status lost assignment: %+v %v", automatic, err)
	}
}

func TestWorkflowTaskCreationWaitsForTransition(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	projectID := util.MustParseUUID(uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO project(id,workspace_id,title) VALUES ($1,$2,'Create locking')`, projectID, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM project WHERE id=$1`, projectID) })
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET project_id=$2 WHERE id=$1`, p.InstanceID, projectID); err != nil {
		t.Fatal(err)
	}
	creator := NewTaskService(svc.Queries, pool, events.New(), nil, nil)
	params := TaskCreateParams{WorkspaceID: p.WorkspaceID, Title: "New required delivery", Status: "todo", Priority: "none", CreatorType: "member", CreatorID: p.ActorID, ProjectID: projectID, WorkflowInstanceID: p.InstanceID, WorkflowStageID: execution.WorkflowStageID}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := svc.Queries.WithTx(tx).LockWorkflowInstanceInWorkspace(ctx, db.LockWorkflowInstanceInWorkspaceParams{ID: p.InstanceID, WorkspaceID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := creator.Create(short, params, TaskCreateOpts{}); err == nil {
		t.Fatal("creation escaped transition lock")
	}
	if _, err := tx.Exec(ctx, `UPDATE workflow_instance SET status='completed' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := creator.Create(ctx, params, TaskCreateOpts{}); !errors.Is(err, ErrWorkflowAssignmentInvalid) {
		t.Fatalf("creation in closed Run: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task WHERE workspace_id=$1 AND title=$2`, p.WorkspaceID, params.Title).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial creation: %d %v", count, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := creator.Create(ctx, params, TaskCreateOpts{}); err != nil {
		t.Fatal(err)
	}
}
