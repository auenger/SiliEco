package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

var ErrWorkflowReviewAccess = errors.New("workflow review is not authorized")

// WorkflowReviewBasis identifies the exact delivery a reviewer saw. Snapshot is
// server evidence and may contain internal metadata; do not expose it as an API
// response. Hash is a concurrency token, never an authorization credential.
type WorkflowReviewBasis struct {
	Hash           string
	ResultRevision int32
	Issues         []db.Issue
	Calls          []db.LockWorkflowReviewCallsRow
	Snapshot       json.RawMessage
	execution      db.WorkflowStageExecution
}

// PrepareHumanReview requires the configured human reviewer, live membership
// and current completion evidence. The decision path repeats all checks.
func (s *WorkflowExecutionService) PrepareHumanReview(ctx context.Context, workspaceID, instanceID, executionID, actorID pgtype.UUID) (WorkflowReviewBasis, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return WorkflowReviewBasis{}, err
	}
	defer tx.Rollback(ctx)
	basis, err := s.lockHumanReviewBasis(ctx, s.Queries.WithTx(tx), workspaceID, instanceID, executionID, actorID)
	if err != nil {
		return WorkflowReviewBasis{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkflowReviewBasis{}, err
	}
	return basis, nil
}

type RecordWorkflowHumanDecisionParams struct {
	WorkspaceID            pgtype.UUID
	InstanceID             pgtype.UUID
	ExecutionID            pgtype.UUID
	ActorID                pgtype.UUID
	ExpectedResultRevision int32
	ExpectedBasisHash      string
	Outcome                string
	Note                   string
}

// RecordHumanDecision records approval/rejection against immutable evidence.
// It does not mark delivery succeeded: advancement must revalidate this basis
// in its own Run transaction and establish the next versioned input handoff.
func (s *WorkflowExecutionService) RecordHumanDecision(ctx context.Context, p RecordWorkflowHumanDecisionParams) (db.WorkflowGateDecision, error) {
	return s.recordHumanDecision(ctx, p, false)
}

// DecideHumanExecution records the decision and, for approval, accepts and advances
// in the same transaction. Failed handoff never leaves a successful approval.
func (s *WorkflowExecutionService) DecideHumanExecution(ctx context.Context, p RecordWorkflowHumanDecisionParams) (db.WorkflowGateDecision, error) {
	return s.recordHumanDecision(ctx, p, true)
}

func (s *WorkflowExecutionService) recordHumanDecision(ctx context.Context, p RecordWorkflowHumanDecisionParams, advance bool) (db.WorkflowGateDecision, error) {
	empty := db.WorkflowGateDecision{}
	if p.ExpectedResultRevision < 1 || len(p.ExpectedBasisHash) != 64 || (p.Outcome != "approved" && p.Outcome != "rejected") {
		return empty, ErrWorkflowExecutionConflict
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
	if err != nil {
		return empty, err
	}
	if advance && p.Outcome == "approved" {
		execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
		if err != nil {
			return empty, err
		}
		if execution.WorkflowInstanceID != p.InstanceID {
			return empty, pgx.ErrNoRows
		}
		if execution.Status == "succeeded" {
			if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: p.WorkspaceID, UserID: p.ActorID}); err != nil {
				return empty, ErrWorkflowReviewAccess
			}
			decision, err := q.GetWorkflowExecutionDecisionByBasis(ctx, db.GetWorkflowExecutionDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: p.ExecutionID, BasisHash: pgtype.Text{String: p.ExpectedBasisHash, Valid: true}})
			if err != nil {
				return empty, err
			}
			var accepted workflowAcceptance
			if json.Unmarshal(execution.Acceptance, &accepted) != nil || accepted.SchemaVersion != "silieco.acceptance/v1" || accepted.DecisionID != decision.ID || accepted.BasisHash != p.ExpectedBasisHash || accepted.ResultRevision != p.ExpectedResultRevision || decision.ActorID != p.ActorID || decision.Outcome != p.Outcome || decision.Note.String != p.Note {
				return empty, ErrWorkflowExecutionConflict
			}
			if err := tx.Commit(ctx); err != nil {
				return empty, err
			}
			return decision, nil
		}
	}
	basis, err := s.lockHumanReviewBasis(ctx, q, p.WorkspaceID, p.InstanceID, p.ExecutionID, p.ActorID)
	if err != nil {
		return empty, err
	}
	if basis.Hash != p.ExpectedBasisHash || basis.ResultRevision != p.ExpectedResultRevision {
		return empty, ErrWorkflowExecutionConflict
	}
	existing, err := q.GetWorkflowExecutionDecisionByBasis(ctx, db.GetWorkflowExecutionDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: p.ExecutionID, BasisHash: pgtype.Text{String: basis.Hash, Valid: true}})
	if err == nil {
		if existing.ActorID != p.ActorID || existing.Outcome != p.Outcome || existing.Note.String != p.Note {
			return empty, ErrWorkflowExecutionConflict
		}
		if advance && p.Outcome == "approved" {
			if _, err := s.advanceAcceptedExecutionInTx(ctx, q, run, basis, existing.ID, "human"); err != nil {
				return empty, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	decision, err := q.CreateWorkflowExecutionDecision(ctx, db.CreateWorkflowExecutionDecisionParams{
		WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, FromStageID: basis.execution.WorkflowStageID,
		Outcome: p.Outcome, ActorID: p.ActorID, Note: pgtype.Text{String: p.Note, Valid: p.Note != ""}, ExecutionID: p.ExecutionID,
		ResultRevision: pgtype.Int4{Int32: basis.ResultRevision, Valid: true}, BasisHash: pgtype.Text{String: basis.Hash, Valid: true}, BasisSnapshot: basis.Snapshot,
	})
	if err != nil {
		return empty, err
	}
	if advance && p.Outcome == "approved" {
		if _, err := s.advanceAcceptedExecutionInTx(ctx, q, run, basis, decision.ID, "human"); err != nil {
			return empty, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return decision, nil
}

func (s *WorkflowExecutionService) lockHumanReviewBasis(ctx context.Context, q *db.Queries, workspaceID, instanceID, executionID, actorID pgtype.UUID) (WorkflowReviewBasis, error) {
	empty := WorkflowReviewBasis{}
	if !workspaceID.Valid || !instanceID.Valid || !executionID.Valid || !actorID.Valid {
		return empty, ErrWorkflowReviewAccess
	}
	run, err := lockWorkflowExecutionRun(ctx, q, workspaceID, instanceID)
	if err != nil {
		return empty, err
	}
	execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: executionID})
	if err != nil {
		return empty, err
	}
	if execution.WorkflowInstanceID != instanceID {
		return empty, pgx.ErrNoRows
	}
	if run.Status != "active" || run.ArchivedAt.Valid || run.CurrentStageID != execution.WorkflowStageID || run.Revision != execution.PlanRevision || execution.Status != "awaiting_review" {
		return empty, ErrWorkflowExecutionConflict
	}
	var config struct {
		OutputSpec json.RawMessage `json:"output_spec"`
		Gate       struct {
			Type         string `json:"type"`
			Decider      string `json:"decider"`
			HumanDecider string `json:"human_decider"`
		} `json:"gate"`
	}
	if err := json.Unmarshal(execution.ConfigSnapshot, &config); err != nil {
		return empty, err
	}
	// Hybrid requires a completed approval against this exact evidence below.
	if config.Gate.Type != "human" && config.Gate.Type != "hybrid" {
		return empty, ErrWorkflowReviewAccess
	}
	decider := config.Gate.HumanDecider
	if decider == "" {
		decider = config.Gate.Decider
	}
	expected, err := util.ParseUUID(decider)
	if err != nil || expected != actorID {
		return empty, ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: actorID}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: run.CreatedBy}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	executor, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: execution.ExecutorID, WorkspaceID: workspaceID})
	if err != nil || executor.ArchivedAt.Valid {
		return empty, ErrWorkflowReviewAccess
	}
	allowed, err := workflowMemberCanInvoke(ctx, q, executor, run.CreatedBy)
	if err != nil {
		return empty, err
	}
	if !allowed {
		return empty, ErrWorkflowReviewAccess
	}
	basis, err := s.captureWorkflowReviewBasis(ctx, q, run, execution, false)
	if err != nil {
		return empty, err
	}
	if config.Gate.Type == "hybrid" {
		if _, err := s.completedAgentApproval(ctx, q, run, execution, basis); err != nil {
			return empty, err
		}
	}
	return basis, nil
}

// captureWorkflowReviewBasis runs under the parent Run lock. Accepted sources
// are rechecked against their original evidence without requiring them to be the
// current stage. Acceptance metadata itself is excluded from the fingerprint.
func (s *WorkflowExecutionService) captureWorkflowReviewBasis(ctx context.Context, q *db.Queries, run db.WorkflowInstance, execution db.WorkflowStageExecution, accepted bool) (WorkflowReviewBasis, error) {
	empty := WorkflowReviewBasis{}
	workspaceID, instanceID, executionID := run.WorkspaceID, run.ID, execution.ID
	if execution.WorkspaceID != workspaceID || execution.WorkflowInstanceID != instanceID || execution.PlanRevision != run.Revision || run.Status != "active" || run.ArchivedAt.Valid {
		return empty, ErrWorkflowExecutionConflict
	}
	if accepted && execution.Status != "succeeded" {
		return empty, ErrWorkflowExecutionConflict
	}
	var config struct {
		OutputSpec json.RawMessage `json:"output_spec"`
	}
	if err := json.Unmarshal(execution.ConfigSnapshot, &config); err != nil {
		return empty, err
	}
	issues, err := q.LockWorkflowReviewIssues(ctx, db.LockWorkflowReviewIssuesParams{WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, WorkflowStageID: execution.WorkflowStageID, WorkflowStageExecutionID: execution.ID})
	if err != nil {
		return empty, err
	}
	calls, err := q.LockWorkflowReviewCalls(ctx, db.LockWorkflowReviewCallsParams{WorkspaceID: workspaceID, InstanceID: instanceID, StageID: execution.WorkflowStageID, ExecutionID: executionID})
	if err != nil {
		return empty, err
	}
	readiness, err := q.GetWorkflowExecutionReadiness(ctx, db.GetWorkflowExecutionReadinessParams{WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, ID: executionID})
	if err != nil {
		return empty, err
	}
	if accepted {
		readiness.Status = "awaiting_review"
		readiness.CurrentExecution = pgtype.Bool{Bool: true, Valid: true}
	}
	if !workflowCompletionConditions(readiness).ReadyForReview {
		return empty, ErrWorkflowExecutionConflict
	}
	// Independently include bound calls whose issue was deleted or moved.
	for _, call := range calls {
		switch call.Status {
		case "queued", "dispatched", "running", "waiting_local_directory", "deferred":
			return empty, ErrWorkflowExecutionConflict
		}
	}
	result, err := q.GetWorkflowExecutionResult(ctx, db.GetWorkflowExecutionResultParams{WorkspaceID: workspaceID, ExecutionID: executionID, Revision: execution.ResultRevision})
	if err != nil {
		return empty, err
	}
	if result.WorkflowInstanceID != instanceID {
		return empty, ErrWorkflowExecutionConflict
	}
	artifacts, err := q.ListWorkflowResultArtifacts(ctx, db.ListWorkflowResultArtifactsParams{WorkspaceID: workspaceID, ResultID: result.ID})
	if err != nil {
		return empty, err
	}
	contract, err := workflowcontract.FromSpec(config.OutputSpec, true)
	if err != nil {
		return empty, err
	}
	manifest := make([]workflowcontract.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		manifest = append(manifest, workflowcontract.Artifact{Name: artifact.Name, Kind: artifact.Kind, MediaType: artifact.MediaType, SchemaVersion: artifact.SchemaVersion, AttachmentID: util.UUIDToString(artifact.SourceAttachmentID), Checksum: artifact.Checksum})
	}
	submission, err := json.Marshal(workflowcontract.Submission{Data: result.Data, Artifacts: manifest})
	if err != nil {
		return empty, err
	}
	violations, err := contract.ValidateSubmission(submission)
	if err != nil {
		return empty, err
	}
	if len(violations) > 0 {
		return empty, &WorkflowResultValidationError{Violations: violations}
	}
	// Frozen InputSnapshot already records the exact consumed values. Unrelated
	// Run input corrections must not invalidate this delivery.
	fingerprintExecution := struct {
		db.WorkflowStageExecution
		ReworkSourceExecutionID *struct{} `json:"rework_source_execution_id,omitempty"`
		ReworkMetadata          *struct{} `json:"rework_metadata,omitempty"`
	}{WorkflowStageExecution: execution}
	fingerprintExecution.Status = "awaiting_review"
	fingerprintExecution.UpdatedAt = pgtype.Timestamptz{}
	fingerprintExecution.FinishedAt = pgtype.Timestamptz{}
	fingerprintExecution.Acceptance = nil
	// Membership is scoped by execution before hashing. Keep the v3 issue
	// projection stable so adding the binding column does not invalidate
	// already accepted evidence during migration.
	type fingerprintIssue struct {
		db.Issue
		WorkflowStageExecutionID      *struct{} `json:"workflow_stage_execution_id,omitempty"`
		WorkflowInvalidatedByReworkID *struct{} `json:"workflow_invalidated_by_rework_id,omitempty"`
	}
	fingerprintIssues := make([]fingerprintIssue, len(issues))
	for i, issue := range issues {
		fingerprintIssues[i] = fingerprintIssue{Issue: issue}
	}
	snapshot, err := json.Marshal(struct {
		Version   string                              `json:"version"`
		Execution any                                 `json:"execution"`
		Issues    []fingerprintIssue                  `json:"issues"`
		Calls     []db.LockWorkflowReviewCallsRow     `json:"calls"`
		Result    db.WorkflowExecutionResult          `json:"result"`
		Artifacts []db.ListWorkflowResultArtifactsRow `json:"artifacts"`
	}{"silieco.review/v3", fingerprintExecution, fingerprintIssues, calls, result, artifacts})
	if err != nil {
		return empty, err
	}
	digest := sha256.Sum256(snapshot)
	return WorkflowReviewBasis{Hash: hex.EncodeToString(digest[:]), ResultRevision: execution.ResultRevision, Snapshot: snapshot, Issues: issues, Calls: calls, execution: execution}, nil
}
