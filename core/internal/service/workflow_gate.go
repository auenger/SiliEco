package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// freezeWorkflowExecutionGate resolves symbolic reviewers once, at execution
// creation. Neither a later assignee change nor a live plan edit can redirect
// the review. This does not dispatch review tasks or authorize acceptance.
func freezeWorkflowExecutionGate(ctx context.Context, q *db.Queries, run db.WorkflowInstance, executorID pgtype.UUID, raw json.RawMessage) (json.RawMessage, error) {
	var gate map[string]json.RawMessage
	if err := json.Unmarshal(raw, &gate); err != nil || gate == nil {
		return nil, fmt.Errorf("gate must be an object")
	}
	var kind string
	if value, ok := gate["type"]; ok {
		if err := json.Unmarshal(value, &kind); err != nil {
			return nil, fmt.Errorf("invalid gate type")
		}
	}
	if kind == "" {
		kind = "none"
		gate["type"] = json.RawMessage(`"none"`)
	}
	switch kind {
	case "none", "human":
		return json.Marshal(gate)
	case "agent", "hybrid":
	default:
		return nil, fmt.Errorf("unsupported gate type %q", kind)
	}
	var decider string
	if err := json.Unmarshal(gate["decider"], &decider); err != nil {
		return nil, fmt.Errorf("agent reviewer is required")
	}
	reviewerID, err := util.ParseUUID(decider)
	if decider == "@self" {
		reviewerID = executorID
		err = nil
	}
	if err != nil || !reviewerID.Valid {
		return nil, fmt.Errorf("agent reviewer must be an ID or @self")
	}
	reviewer, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: run.WorkspaceID, ID: reviewerID})
	if err != nil || reviewer.ArchivedAt.Valid {
		return nil, ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: run.WorkspaceID, UserID: run.CreatedBy}); err != nil {
		return nil, ErrWorkflowReviewAccess
	}
	allowed, err := workflowMemberCanInvoke(ctx, q, reviewer, run.CreatedBy)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrWorkflowReviewAccess
	}
	if kind == "hybrid" {
		var human string
		if err := json.Unmarshal(gate["human_decider"], &human); err != nil {
			return nil, fmt.Errorf("hybrid human reviewer is required")
		}
		humanID, err := util.ParseUUID(human)
		if err != nil {
			return nil, fmt.Errorf("hybrid human reviewer must be a member ID")
		}
		if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: run.WorkspaceID, UserID: humanID}); err != nil {
			return nil, ErrWorkflowReviewAccess
		}
	}
	gate["decider"], err = json.Marshal(util.UUIDToString(reviewerID))
	if err != nil {
		return nil, err
	}
	gate["decider_type"] = json.RawMessage(`"agent"`)
	return json.Marshal(gate)
}
