package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// replayAcceptedAgentDecision returns only the exact existing approval bound to
// the accepted delivery. The caller holds the Run lock. It never creates a new
// decision or advances the Run, even if the Run has since moved to another stage.
func (s *WorkflowExecutionService) replayAcceptedAgentDecision(ctx context.Context, q *db.Queries, run db.WorkflowInstance, execution db.WorkflowStageExecution, p RecordWorkflowAgentDecisionParams) (db.WorkflowGateDecision, error) {
	empty := db.WorkflowGateDecision{}
	request, err := q.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: p.WorkspaceID, ID: p.RequestID})
	if err != nil {
		return empty, err
	}
	if request.WorkflowInstanceID != run.ID || execution.WorkflowInstanceID != run.ID || request.ExecutionID != execution.ID || request.Status != "decided" || request.ReviewerID != p.ActorID || request.TaskID != p.TaskID {
		return empty, ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: p.WorkspaceID, UserID: run.CreatedBy}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	reviewer, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: p.ActorID})
	if err != nil || reviewer.ArchivedAt.Valid {
		return empty, ErrWorkflowReviewAccess
	}
	allowed, err := workflowMemberCanInvoke(ctx, q, reviewer, run.CreatedBy)
	if err != nil {
		return empty, err
	}
	if !allowed {
		return empty, ErrWorkflowReviewAccess
	}
	task, err := q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{WorkspaceID: p.WorkspaceID, ID: p.TaskID})
	if err != nil {
		return empty, err
	}
	if task.Status != "completed" || task.AgentID != p.ActorID || task.WorkflowReviewRequestID != request.ID || task.WorkflowStageExecutionID.Valid || task.IssueID != execution.MainIssueID {
		return empty, ErrWorkflowReviewAccess
	}
	decision, err := q.GetWorkflowAgentDecisionByBasis(ctx, db.GetWorkflowAgentDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: execution.ID, BasisHash: pgtype.Text{String: request.BasisHash, Valid: true}, ReviewTaskID: request.TaskID})
	if err != nil {
		return empty, err
	}
	var accepted workflowAcceptance
	var config struct {
		Gate struct {
			Type string `json:"type"`
		} `json:"gate"`
	}
	if json.Unmarshal(execution.Acceptance, &accepted) != nil || json.Unmarshal(execution.ConfigSnapshot, &config) != nil {
		return empty, ErrWorkflowExecutionConflict
	}
	if accepted.SchemaVersion != "silieco.acceptance/v1" || accepted.ResultID != request.ResultID || accepted.ResultRevision != request.ResultRevision || accepted.BasisHash != request.BasisHash || p.ExpectedBasisHash != request.BasisHash || p.ExpectedResultRevision != request.ResultRevision || decision.ActorID != p.ActorID || decision.Outcome != "approved" || decision.Outcome != p.Outcome || decision.Note.String != p.Note || !decision.ResultRevision.Valid || decision.ResultRevision.Int32 != request.ResultRevision {
		return empty, ErrWorkflowExecutionConflict
	}
	if (config.Gate.Type != "agent" || accepted.ReviewStep != "agent" || accepted.DecisionID != decision.ID) && (config.Gate.Type != "hybrid" || accepted.ReviewStep != "human" || !accepted.DecisionID.Valid) {
		return empty, ErrWorkflowExecutionConflict
	}
	if config.Gate.Type == "hybrid" {
		human, err := q.GetWorkflowExecutionDecisionByBasis(ctx, db.GetWorkflowExecutionDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: execution.ID, BasisHash: pgtype.Text{String: request.BasisHash, Valid: true}})
		if err != nil {
			return empty, err
		}
		if human.ID != accepted.DecisionID || human.Outcome != "approved" || !human.ResultRevision.Valid || human.ResultRevision.Int32 != request.ResultRevision {
			return empty, ErrWorkflowExecutionConflict
		}
	}

	return decision, nil
}
