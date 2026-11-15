package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/db/relational"
)

// sameTaskRevision compares timestamp instants, not time.Time's internal
// representation. Rows loaded through different PostgreSQL query plans may
// carry equivalent locations with different pointers; struct equality would
// reject those as stale even though they represent the same database value.
func sameTaskRevision(a, b pgtype.Timestamptz) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.Time.Equal(b.Time)
}

// LockTaskWorkflowMutation locks parent projects, Runs and finally the task.
// Multiple Run/project identities are sorted to keep cross-Run moves deadlock
// free. Callers must use a transaction and commit their mutation before release.
func LockTaskWorkflowMutation(ctx context.Context, q *db.Queries, workspaceID, taskID, destinationID pgtype.UUID) (db.Task, map[pgtype.UUID]db.WorkflowInstance, error) {
	return lockTaskWorkflow(ctx, q, workspaceID, taskID, destinationID, true)
}

func lockTaskWorkflow(ctx context.Context, q *db.Queries, workspaceID, taskID, destinationID pgtype.UUID, requireMutable bool) (db.Task, map[pgtype.UUID]db.WorkflowInstance, error) {
	empty := db.Task{}
	if _, err := q.LockWorkspaceForWorkflowExecution(ctx, workspaceID); err != nil {
		return empty, nil, err
	}
	observed, err := q.GetTaskInWorkspace(ctx, db.GetTaskInWorkspaceParams{ID: taskID, WorkspaceID: workspaceID})
	if err != nil {
		return empty, nil, err
	}
	runs := map[pgtype.UUID]db.WorkflowInstance{}
	projects := map[pgtype.UUID]bool{}
	for _, id := range []pgtype.UUID{observed.WorkflowInstanceID, destinationID} {
		if !id.Valid {
			continue
		}
		if _, exists := runs[id]; exists {
			continue
		}
		run, err := q.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			return empty, nil, err
		}
		runs[id] = run
		if run.ProjectID.Valid {
			projects[run.ProjectID] = true
		}
	}
	projectIDs := make([]pgtype.UUID, 0, len(projects))
	for id := range projects {
		projectIDs = append(projectIDs, id)
	}
	sort.Slice(projectIDs, func(i, j int) bool { return util.UUIDToString(projectIDs[i]) < util.UUIDToString(projectIDs[j]) })
	for _, id := range projectIDs {
		if _, err := q.LockProjectForWorkflowExecution(ctx, db.LockProjectForWorkflowExecutionParams{ID: id, WorkspaceID: workspaceID}); err != nil {
			return empty, nil, err
		}
	}
	runIDs := make([]pgtype.UUID, 0, len(runs))
	for id := range runs {
		runIDs = append(runIDs, id)
	}
	sort.Slice(runIDs, func(i, j int) bool { return util.UUIDToString(runIDs[i]) < util.UUIDToString(runIDs[j]) })
	for _, id := range runIDs {
		locked, err := q.LockWorkflowInstanceInWorkspace(ctx, db.LockWorkflowInstanceInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			return empty, nil, err
		}
		if locked.ProjectID != runs[id].ProjectID {
			return empty, nil, fmt.Errorf("%w: workflow project changed", ErrWorkflowExecutionConflict)
		}
		runs[id] = locked
	}
	task, err := q.LockTaskForWorkflowMutation(ctx, db.LockTaskForWorkflowMutationParams{ID: taskID, WorkspaceID: workspaceID})
	if err != nil {
		return empty, nil, err
	}
	if task.WorkflowInstanceID != observed.WorkflowInstanceID || task.WorkflowStageID != observed.WorkflowStageID || task.ProjectID != observed.ProjectID {
		return empty, nil, fmt.Errorf("%w: task workflow membership changed", ErrWorkflowExecutionConflict)
	}
	if !requireMutable {
		return task, runs, nil
	}
	if task.WorkflowInvalidatedByReworkID.Valid {
		return empty, nil, fmt.Errorf("%w: task was invalidated by rework", ErrWorkflowExecutionConflict)
	}
	if task.WorkflowStageExecutionID.Valid {
		latest, err := q.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: workspaceID, WorkflowInstanceID: task.WorkflowInstanceID, WorkflowStageID: task.WorkflowStageID})
		if err != nil {
			return empty, nil, err
		}
		if latest.ID != task.WorkflowStageExecutionID {
			return empty, nil, fmt.Errorf("%w: task stage execution changed", ErrWorkflowExecutionConflict)
		}
	}
	if err := ensureWorkflowStageMutable(ctx, q, workspaceID, task.WorkflowInstanceID, task.WorkflowStageID); err != nil {
		return empty, nil, err
	}
	return task, runs, nil
}

// MoveTaskStage changes membership under the same Run locks used by transition.
// An invalid stage ID means detach from instanceID, not detach from any Run.
func (s *WorkflowExecutionService) MoveTaskStage(ctx context.Context, workspaceID, taskID, instanceID, stageID pgtype.UUID) (db.Task, error) {
	empty := db.Task{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	task, runs, err := LockTaskWorkflowMutation(ctx, q, workspaceID, taskID, instanceID)
	if err != nil {
		return empty, err
	}
	if stageID.Valid && (task.WorkflowInstanceID != instanceID || task.WorkflowStageID != stageID) {
		run := runs[instanceID]
		if err := prepareWorkflowForNewTask(ctx, q, run, stageID); err != nil {
			return empty, err
		}
		if run.Status == "completed" {
			run.Status = "active"
			runs[instanceID] = run
		}
	}
	for _, run := range runs {
		if run.ArchivedAt.Valid || run.Status == "completed" || run.Status == "cancelled" {
			return empty, ErrWorkflowExecutionConflict
		}
	}
	var updated db.Task
	if !stageID.Valid {
		if task.WorkflowInstanceID != instanceID {
			return empty, ErrWorkflowExecutionConflict
		}
		updated, err = q.DetachTaskFromWorkflow(ctx, db.DetachTaskFromWorkflowParams{ID: taskID, WorkspaceID: workspaceID, WorkflowInstanceID: instanceID})
	} else {
		if err := ensureWorkflowStageMutable(ctx, q, workspaceID, instanceID, stageID); err != nil {
			return empty, err
		}
		run := runs[instanceID]
		if !run.ProjectID.Valid || (task.ProjectID.Valid && task.ProjectID != run.ProjectID) {
			return empty, ErrWorkflowExecutionConflict
		}
		if _, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{ID: stageID, WorkflowInstanceID: instanceID, WorkspaceID: workspaceID}); err != nil {
			return empty, err
		}
		updated, err = q.AttachTaskToWorkflowStage(ctx, db.AttachTaskToWorkflowStageParams{ID: taskID, WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, WorkflowStageID: stageID, ProjectID: run.ProjectID})
	}
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return updated, nil
}

// UpdateTaskWithWorkflowLock guards ordinary and batch task edits with the
// same membership protocol as dedicated SOP moves. The caller's observed row
// supplies a mandatory stale-write check even for clients without a revision.
func (s *WorkflowExecutionService) UpdateTaskWithWorkflowLock(ctx context.Context, observed db.Task, params db.UpdateTaskParams) (db.Task, error) {
	empty := db.Task{}
	if params.ID != observed.ID {
		return empty, ErrWorkflowExecutionConflict
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	current, runs, err := LockTaskWorkflowMutation(ctx, q, observed.WorkspaceID, observed.ID, params.WorkflowInstanceID)
	if err != nil {
		return empty, err
	}
	if !sameTaskRevision(current.UpdatedAt, observed.UpdatedAt) {
		return empty, fmt.Errorf("%w: task revision changed", ErrWorkflowExecutionConflict)
	}
	if params.WorkflowInstanceID.Valid && (current.WorkflowInstanceID != params.WorkflowInstanceID || current.WorkflowStageID != params.WorkflowStageID) {
		run := runs[params.WorkflowInstanceID]
		if err := prepareWorkflowForNewTask(ctx, q, run, params.WorkflowStageID); err != nil {
			return empty, err
		}
		if run.Status == "completed" {
			run.Status = "active"
			runs[params.WorkflowInstanceID] = run
		}
	}
	for _, run := range runs {
		if run.ArchivedAt.Valid || run.Status == "completed" || run.Status == "cancelled" {
			return empty, fmt.Errorf("%w: workflow run is closed", ErrWorkflowExecutionConflict)
		}
	}
	if params.WorkflowInstanceID.Valid != params.WorkflowStageID.Valid {
		return empty, fmt.Errorf("%w: incomplete workflow destination", ErrWorkflowExecutionConflict)
	}
	if params.WorkflowInstanceID.Valid {
		if err := ensureWorkflowStageMutable(ctx, q, observed.WorkspaceID, params.WorkflowInstanceID, params.WorkflowStageID); err != nil {
			return empty, err
		}
		run := runs[params.WorkflowInstanceID]
		if !run.ProjectID.Valid || run.ProjectID != params.ProjectID {
			return empty, fmt.Errorf("%w: workflow project does not match task project", ErrWorkflowExecutionConflict)
		}
		if _, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{ID: params.WorkflowStageID, WorkflowInstanceID: run.ID, WorkspaceID: observed.WorkspaceID}); err != nil {
			return empty, err
		}
	}
	updated, err := q.UpdateTask(ctx, params)
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return updated, nil
}

// UpdateTaskStatusWithWorkflowLock preserves the observed assignment and
// nullable fields while applying an automatic status transition. A newer task
// version is rejected instead of being overwritten by a delayed webhook/reset.
func (s *WorkflowExecutionService) UpdateTaskStatusWithWorkflowLock(ctx context.Context, task db.Task, status string) (db.Task, error) {
	return s.UpdateTaskWithWorkflowLock(ctx, task, db.UpdateTaskParams{ID: task.ID, Status: pgtype.Text{String: status, Valid: true}, AssigneeType: task.AssigneeType, AssigneeID: task.AssigneeID, StartDate: task.StartDate, DueDate: task.DueDate, ParentTaskID: task.ParentTaskID, ProjectID: task.ProjectID, Stage: task.Stage, WorkflowInstanceID: task.WorkflowInstanceID, WorkflowStageID: task.WorkflowStageID})
}

// WorkflowTaskDeletion contains only committed deletion effects. Callers send
// notifications and delete storage objects after this operation succeeds.
type WorkflowTaskDeletion struct {
	Task           db.Task
	CancelledRuns  []db.AgentRun
	AttachmentURLs []string
}

func (s *WorkflowExecutionService) DeleteTaskWithWorkflowLock(ctx context.Context, workspaceID, taskID pgtype.UUID) (WorkflowTaskDeletion, error) {
	empty := WorkflowTaskDeletion{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	task, runs, err := LockTaskWorkflowMutation(ctx, q, workspaceID, taskID, pgtype.UUID{})
	if err != nil {
		return empty, err
	}
	for _, run := range runs {
		if run.ArchivedAt.Valid || run.Status == "completed" || run.Status == "cancelled" {
			return empty, ErrWorkflowExecutionConflict
		}
	}
	if err := relational.LockForDelete(ctx, tx, relational.DeleteTask, task.ID, workspaceID); err != nil {
		return empty, err
	}
	cancelled, err := q.CancelAgentRunsByTask(ctx, task.ID)
	if err != nil {
		return empty, err
	}
	if err := q.FailAutopilotRunsByTask(ctx, task.ID); err != nil {
		return empty, err
	}
	if err := q.DeleteAttachmentReferencesByTarget(ctx, db.DeleteAttachmentReferencesByTargetParams{WorkspaceID: workspaceID, TargetType: "task", TargetID: task.ID}); err != nil {
		return empty, err
	}
	if _, err := q.DetachReferencedAttachmentsFromTask(ctx, db.DetachReferencedAttachmentsFromTaskParams{WorkspaceID: workspaceID, TaskID: task.ID}); err != nil {
		return empty, err
	}
	urls, err := q.ListAttachmentURLsByTaskOrComments(ctx, task.ID)
	if err != nil {
		return empty, err
	}
	if err := q.DeleteTask(ctx, db.DeleteTaskParams{ID: task.ID, WorkspaceID: workspaceID}); err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return WorkflowTaskDeletion{Task: task, CancelledRuns: cancelled, AttachmentURLs: urls}, nil
}
