package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/skillbundle"
)

var ErrSkillProposalConflict = errors.New("proposal request already used for different content")

// CreateSkillImprovementProposalParams carries member-authored analysis, never
// claimed source metadata. Execution/result provenance is resolved in storage.
type CreateSkillImprovementProposalParams struct {
	WorkspaceID          pgtype.UUID
	SkillID              pgtype.UUID
	ActorID              pgtype.UUID
	RequestID            pgtype.UUID
	BaselineRevisionID   pgtype.UUID
	CandidateRevisionID  pgtype.UUID
	SourceExecutionID    pgtype.UUID
	SourceResultRevision int32
	Category             string
	Reason               string
	Hypothesis           string
	ApplicableScope      string
}

type SkillProposalEvidence struct {
	ExecutionID    string `json:"execution_id,omitempty"`
	InstanceID     string `json:"workflow_instance_id,omitempty"`
	StageID        string `json:"workflow_stage_id,omitempty"`
	Attempt        int32  `json:"attempt,omitempty"`
	PlanRevision   int32  `json:"plan_revision,omitempty"`
	ResultID       string `json:"result_id,omitempty"`
	ResultRevision int32  `json:"result_revision,omitempty"`
	ResultHash     string `json:"result_hash,omitempty"`
}

func skillProposalDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validSkillProposalText(value string, limit int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit && !strings.ContainsRune(value, '\x00')
}

func (s *SkillRevisionService) CreateImprovementProposal(ctx context.Context, p CreateSkillImprovementProposalParams) (db.SkillImprovementProposal, error) {
	empty := db.SkillImprovementProposal{}
	if !p.WorkspaceID.Valid || !p.SkillID.Valid || !p.ActorID.Valid {
		return empty, ErrSkillRevisionAccess
	}
	if !p.RequestID.Valid || !p.BaselineRevisionID.Valid || !p.CandidateRevisionID.Valid || p.BaselineRevisionID == p.CandidateRevisionID || p.SourceResultRevision < 0 || (!p.SourceExecutionID.Valid && p.SourceResultRevision != 0) {
		return empty, ErrSkillRevisionInvalid
	}
	switch p.Category {
	case "skill_instruction", "insufficient_input", "tool_failure", "contract_issue", "model_capability", "human_correction", "positive_example":
	default:
		return empty, ErrSkillRevisionInvalid
	}
	if !validSkillProposalText(p.Reason, 8000) || !validSkillProposalText(p.Hypothesis, 8000) || !validSkillProposalText(p.ApplicableScope, 4000) {
		return empty, ErrSkillRevisionInvalid
	}
	requestJSON, err := json.Marshal(p)
	if err != nil {
		return empty, err
	}
	requestHash := skillProposalDigest(requestJSON)
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if _, err = q.LockWorkspaceForSkillRevisionCreate(ctx, p.WorkspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrSkillRevisionAccess
		}
		return empty, err
	}
	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: p.WorkspaceID, UserID: p.ActorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrSkillRevisionAccess
	}
	if err != nil {
		return empty, err
	}
	source, err := q.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: p.SkillID})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrSkillRevisionAccess
	}
	if err != nil {
		return empty, err
	}
	if member.Role != "owner" && member.Role != "admin" && (member.Role != "member" || !source.CreatedBy.Valid || source.CreatedBy != p.ActorID) {
		return empty, ErrSkillRevisionAccess
	}
	// Replays retain their original evidence even if the source Run has since been
	// deleted. Current membership and skill-management permission still apply.
	existing, err := q.GetSkillImprovementProposalByRequest(ctx, db.GetSkillImprovementProposalByRequestParams{WorkspaceID: p.WorkspaceID, CreatedBy: p.ActorID, RequestID: p.RequestID})
	if err == nil {
		if existing.RequestHash != requestHash {
			return empty, ErrSkillProposalConflict
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	for _, id := range []pgtype.UUID{p.BaselineRevisionID, p.CandidateRevisionID} {
		revision, err := q.GetSkillRevisionInWorkspace(ctx, db.GetSkillRevisionInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrSkillRevisionAccess
		}
		if err != nil {
			return empty, err
		}
		if revision.SourceKind != skillbundle.SourceWorkspace || revision.SourceID != util.UUIDToString(p.SkillID) {
			return empty, ErrSkillRevisionAccess
		}
	}
	evidence := SkillProposalEvidence{}
	if p.SourceExecutionID.Valid {
		execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.SourceExecutionID})
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrSkillRevisionAccess
		}
		if err != nil {
			return empty, err
		}
		evidence = SkillProposalEvidence{ExecutionID: util.UUIDToString(execution.ID), InstanceID: util.UUIDToString(execution.WorkflowInstanceID), StageID: util.UUIDToString(execution.WorkflowStageID), Attempt: execution.Attempt, PlanRevision: execution.PlanRevision}
		if p.SourceResultRevision > 0 {
			result, err := q.GetWorkflowExecutionResult(ctx, db.GetWorkflowExecutionResultParams{WorkspaceID: p.WorkspaceID, ExecutionID: p.SourceExecutionID, Revision: p.SourceResultRevision})
			if errors.Is(err, pgx.ErrNoRows) {
				return empty, ErrSkillRevisionAccess
			}
			if err != nil {
				return empty, err
			}
			if result.WorkflowInstanceID != execution.WorkflowInstanceID {
				return empty, ErrSkillRevisionAccess
			}
			evidence.ResultID = util.UUIDToString(result.ID)
			evidence.ResultRevision = result.Revision
			evidence.ResultHash = skillProposalDigest(result.Data)
		}
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return empty, err
	}
	proposal, err := q.CreateSkillImprovementProposal(ctx, db.CreateSkillImprovementProposalParams{WorkspaceID: p.WorkspaceID, SkillID: p.SkillID, BaselineRevisionID: p.BaselineRevisionID, CandidateRevisionID: p.CandidateRevisionID, RequestID: p.RequestID, RequestHash: requestHash, Category: p.Category, Reason: p.Reason, Hypothesis: p.Hypothesis, ApplicableScope: p.ApplicableScope, Evidence: evidenceJSON, CreatedBy: p.ActorID})
	if errors.Is(err, pgx.ErrNoRows) {
		proposal, err = q.GetSkillImprovementProposalByRequest(ctx, db.GetSkillImprovementProposalByRequestParams{WorkspaceID: p.WorkspaceID, CreatedBy: p.ActorID, RequestID: p.RequestID})
		if err == nil && proposal.RequestHash != requestHash {
			return empty, ErrSkillProposalConflict
		}
	}
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return proposal, nil
}
