package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// sameIssueRevision compares timestamp instants, not time.Time's internal
// representation. Rows loaded through different PostgreSQL query plans may
// carry equivalent locations with different pointers; struct equality would
// reject those as stale even though they represent the same database value.
func sameIssueRevision(a, b pgtype.Timestamptz) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.Time.Equal(b.Time)
}

// LockIssueWorkflowMutation locks parent projects, Runs and finally the issue.
// Multiple Run/project identities are sorted to keep cross-Run moves deadlock
// free. Callers must use a transaction and commit their mutation before release.
func LockIssueWorkflowMutation(ctx context.Context, q *db.Queries, workspaceID, issueID, destinationID pgtype.UUID) (db.Issue, map[pgtype.UUID]db.WorkflowInstance, error) {
	return lockIssueWorkflow(ctx, q, workspaceID, issueID, destinationID, true)
}

func lockIssueWorkflow(ctx context.Context, q *db.Queries, workspaceID, issueID, destinationID pgtype.UUID, requireMutable bool) (db.Issue, map[pgtype.UUID]db.WorkflowInstance, error) {
	empty := db.Issue{}
	if _, err := q.LockWorkspaceForWorkflowExecution(ctx, workspaceID); err != nil {
		return empty, nil, err
	}
	observed, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
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
	issue, err := q.LockIssueForWorkflowMutation(ctx, db.LockIssueForWorkflowMutationParams{ID: issueID, WorkspaceID: workspaceID})
	if err != nil {
		return empty, nil, err
	}
	if issue.WorkflowInstanceID != observed.WorkflowInstanceID || issue.WorkflowStageID != observed.WorkflowStageID || issue.ProjectID != observed.ProjectID {
		return empty, nil, fmt.Errorf("%w: task workflow membership changed", ErrWorkflowExecutionConflict)
	}
	if !requireMutable {
		return issue, runs, nil
	}
	if issue.WorkflowInvalidatedByReworkID.Valid {
		return empty, nil, fmt.Errorf("%w: task was invalidated by rework", ErrWorkflowExecutionConflict)
	}
	if issue.WorkflowStageExecutionID.Valid {
		latest, err := q.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: workspaceID, WorkflowInstanceID: issue.WorkflowInstanceID, WorkflowStageID: issue.WorkflowStageID})
		if err != nil {
			return empty, nil, err
		}
		if latest.ID != issue.WorkflowStageExecutionID {
			return empty, nil, fmt.Errorf("%w: task stage execution changed", ErrWorkflowExecutionConflict)
		}
	}
	if err := ensureWorkflowStageMutable(ctx, q, workspaceID, issue.WorkflowInstanceID, issue.WorkflowStageID); err != nil {
		return empty, nil, err
	}
	return issue, runs, nil
}

// MoveIssueStage changes membership under the same Run locks used by transition.
// An invalid stage ID means detach from instanceID, not detach from any Run.
func (s *WorkflowExecutionService) MoveIssueStage(ctx context.Context, workspaceID, issueID, instanceID, stageID pgtype.UUID) (db.Issue, error) {
	empty := db.Issue{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	issue, runs, err := LockIssueWorkflowMutation(ctx, q, workspaceID, issueID, instanceID)
	if err != nil {
		return empty, err
	}
	for _, run := range runs {
		if run.ArchivedAt.Valid || run.Status == "completed" || run.Status == "cancelled" {
			return empty, ErrWorkflowExecutionConflict
		}
	}
	var updated db.Issue
	if !stageID.Valid {
		if issue.WorkflowInstanceID != instanceID {
			return empty, ErrWorkflowExecutionConflict
		}
		updated, err = q.DetachIssueFromWorkflow(ctx, db.DetachIssueFromWorkflowParams{ID: issueID, WorkspaceID: workspaceID, WorkflowInstanceID: instanceID})
	} else {
		if err := ensureWorkflowStageMutable(ctx, q, workspaceID, instanceID, stageID); err != nil {
			return empty, err
		}
		run := runs[instanceID]
		if !run.ProjectID.Valid || (issue.ProjectID.Valid && issue.ProjectID != run.ProjectID) {
			return empty, ErrWorkflowExecutionConflict
		}
		if _, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{ID: stageID, WorkflowInstanceID: instanceID, WorkspaceID: workspaceID}); err != nil {
			return empty, err
		}
		updated, err = q.AttachIssueToWorkflowStage(ctx, db.AttachIssueToWorkflowStageParams{ID: issueID, WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, WorkflowStageID: stageID, ProjectID: run.ProjectID})
	}
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return updated, nil
}

// UpdateIssueWithWorkflowLock guards ordinary and batch issue edits with the
// same membership protocol as dedicated SOP moves. The caller's observed row
// supplies a mandatory stale-write check even for clients without a revision.
func (s *WorkflowExecutionService) UpdateIssueWithWorkflowLock(ctx context.Context, observed db.Issue, params db.UpdateIssueParams) (db.Issue, error) {
	empty := db.Issue{}
	if params.ID != observed.ID {
		return empty, ErrWorkflowExecutionConflict
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	current, runs, err := LockIssueWorkflowMutation(ctx, q, observed.WorkspaceID, observed.ID, params.WorkflowInstanceID)
	if err != nil {
		return empty, err
	}
	if !sameIssueRevision(current.UpdatedAt, observed.UpdatedAt) {
		return empty, fmt.Errorf("%w: task revision changed", ErrWorkflowExecutionConflict)
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
	updated, err := q.UpdateIssue(ctx, params)
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return updated, nil
}

// UpdateIssueStatusWithWorkflowLock preserves the observed assignment and
// nullable fields while applying an automatic status transition. A newer issue
// version is rejected instead of being overwritten by a delayed webhook/reset.
func (s *WorkflowExecutionService) UpdateIssueStatusWithWorkflowLock(ctx context.Context, issue db.Issue, status string) (db.Issue, error) {
	return s.UpdateIssueWithWorkflowLock(ctx, issue, db.UpdateIssueParams{ID: issue.ID, Status: pgtype.Text{String: status, Valid: true}, AssigneeType: issue.AssigneeType, AssigneeID: issue.AssigneeID, StartDate: issue.StartDate, DueDate: issue.DueDate, ParentIssueID: issue.ParentIssueID, ProjectID: issue.ProjectID, Stage: issue.Stage, WorkflowInstanceID: issue.WorkflowInstanceID, WorkflowStageID: issue.WorkflowStageID})
}

// WorkflowIssueDeletion contains only committed deletion effects. Callers send
// notifications and delete storage objects after this operation succeeds.
type WorkflowIssueDeletion struct {
	Issue          db.Issue
	CancelledTasks []db.AgentTaskQueue
	AttachmentURLs []string
}

func (s *WorkflowExecutionService) DeleteIssueWithWorkflowLock(ctx context.Context, workspaceID, issueID pgtype.UUID) (WorkflowIssueDeletion, error) {
	empty := WorkflowIssueDeletion{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	issue, runs, err := LockIssueWorkflowMutation(ctx, q, workspaceID, issueID, pgtype.UUID{})
	if err != nil {
		return empty, err
	}
	for _, run := range runs {
		if run.ArchivedAt.Valid || run.Status == "completed" || run.Status == "cancelled" {
			return empty, ErrWorkflowExecutionConflict
		}
	}
	cancelled, err := q.CancelAgentTasksByIssue(ctx, issue.ID)
	if err != nil {
		return empty, err
	}
	if err := q.FailAutopilotRunsByIssue(ctx, issue.ID); err != nil {
		return empty, err
	}
	if err := q.DeleteAttachmentReferencesByTarget(ctx, db.DeleteAttachmentReferencesByTargetParams{WorkspaceID: workspaceID, TargetType: "issue", TargetID: issue.ID}); err != nil {
		return empty, err
	}
	if _, err := q.DetachReferencedAttachmentsFromIssue(ctx, db.DetachReferencedAttachmentsFromIssueParams{WorkspaceID: workspaceID, IssueID: issue.ID}); err != nil {
		return empty, err
	}
	urls, err := q.ListAttachmentURLsByIssueOrComments(ctx, issue.ID)
	if err != nil {
		return empty, err
	}
	if err := q.DeleteIssue(ctx, db.DeleteIssueParams{ID: issue.ID, WorkspaceID: workspaceID}); err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return WorkflowIssueDeletion{Issue: issue, CancelledTasks: cancelled, AttachmentURLs: urls}, nil
}
