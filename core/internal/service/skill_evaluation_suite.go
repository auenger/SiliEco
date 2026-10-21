package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/skilleval"
)

var ErrSkillEvaluationAccess = errors.New("skill evaluation access denied")
var ErrSkillEvaluationInvalid = errors.New("invalid skill evaluation suite")
var ErrSkillEvaluationIntegrity = errors.New("stored skill evaluation suite failed integrity checks")

type SkillEvaluationService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}
type SkillEvaluationSuiteSummary = db.ListSkillEvaluationSuiteSummariesRow

type CreateSkillEvaluationSuiteParams struct {
	WorkspaceID pgtype.UUID
	SkillID     pgtype.UUID
	ActorID     pgtype.UUID
	Definition  []byte
}

type ReadSkillEvaluationSuiteParams struct {
	WorkspaceID pgtype.UUID
	SkillID     pgtype.UUID
	ActorID     pgtype.UUID
	SuiteID     pgtype.UUID
}

type evaluationPermission int

const (
	evaluationMember evaluationPermission = iota
	evaluationEditor
	evaluationCustodian
)

// Complete definitions are restricted to workspace owners/admins. Skill authors
// may consume improvement cases, but authorship does not grant held-out answers.
func authorizeSkillEvaluation(ctx context.Context, q *db.Queries, workspaceID, skillID, actorID pgtype.UUID, permission evaluationPermission) error {
	if !workspaceID.Valid || !skillID.Valid || !actorID.Valid {
		return ErrSkillEvaluationAccess
	}
	if _, err := q.LockWorkspaceForSkillRevisionCreate(ctx, workspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSkillEvaluationAccess
		}
		return err
	}
	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: actorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSkillEvaluationAccess
	}
	if err != nil {
		return err
	}
	source, err := q.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{WorkspaceID: workspaceID, ID: skillID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSkillEvaluationAccess
	}
	if err != nil {
		return err
	}
	if permission == evaluationMember || member.Role == "owner" || member.Role == "admin" {
		return nil
	}
	if permission == evaluationEditor && member.Role == "member" && source.CreatedBy.Valid && source.CreatedBy == actorID {
		return nil
	}
	return ErrSkillEvaluationAccess
}

func evaluationSummary(row db.SkillEvaluationSuite) SkillEvaluationSuiteSummary {
	return SkillEvaluationSuiteSummary{ID: row.ID, WorkspaceID: row.WorkspaceID, SkillID: row.SkillID, Name: row.Name, SuiteHash: row.SuiteHash, SchemaVersion: row.SchemaVersion, ValidatorVersion: row.ValidatorVersion, ImprovementCount: row.ImprovementCount, RegressionCount: row.RegressionCount, HeldOutCount: row.HeldOutCount, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt}
}

func evaluationCounts(spec skilleval.SuiteSpec) (improvement, regression, heldOut int32) {
	for _, c := range spec.Cases {
		switch c.Partition {
		case skilleval.Improvement:
			improvement++
		case skilleval.Regression:
			regression++
		case skilleval.HeldOut:
			heldOut++
		}
	}
	return
}

func decodeStoredEvaluationSuite(row db.SkillEvaluationSuite) (skilleval.Suite, error) {
	suite, err := skilleval.Parse(row.Definition)
	if err != nil || suite.Hash() != row.SuiteHash {
		return skilleval.Suite{}, ErrSkillEvaluationIntegrity
	}
	var spec skilleval.SuiteSpec
	if err = json.Unmarshal(suite.StorageJSON(), &spec); err != nil {
		return skilleval.Suite{}, ErrSkillEvaluationIntegrity
	}
	improvement, regression, heldOut := evaluationCounts(spec)
	if spec.Name != row.Name || spec.SchemaVersion != row.SchemaVersion || spec.ValidatorVersion != row.ValidatorVersion || improvement != row.ImprovementCount || regression != row.RegressionCount || heldOut != row.HeldOutCount {
		return skilleval.Suite{}, ErrSkillEvaluationIntegrity
	}
	return suite, nil
}

// CreateSuite returns metadata only. Definition bytes retain exact JSON number
// lexemes, so PostgreSQL JSONB normalization cannot silently alter the suite hash.
func (s *SkillEvaluationService) CreateSuite(ctx context.Context, p CreateSkillEvaluationSuiteParams) (SkillEvaluationSuiteSummary, error) {
	empty := SkillEvaluationSuiteSummary{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if err = authorizeSkillEvaluation(ctx, q, p.WorkspaceID, p.SkillID, p.ActorID, evaluationCustodian); err != nil {
		return empty, err
	}
	suite, err := skilleval.Parse(p.Definition)
	if err != nil {
		return empty, fmt.Errorf("%w: %v", ErrSkillEvaluationInvalid, err)
	}
	// Existing immutable versions remain replayable after source history is removed.
	existing, lookupErr := q.GetSkillEvaluationSuiteByHash(ctx, db.GetSkillEvaluationSuiteByHashParams{WorkspaceID: p.WorkspaceID, SkillID: p.SkillID, SuiteHash: suite.Hash()})
	if lookupErr == nil {
		if _, err = decodeStoredEvaluationSuite(existing); err != nil {
			return empty, err
		}
		return evaluationSummary(existing), nil
	}
	if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return empty, lookupErr
	}
	var spec skilleval.SuiteSpec
	if err = json.Unmarshal(suite.StorageJSON(), &spec); err != nil {
		return empty, err
	}
	for _, c := range spec.Cases {
		if c.Source == nil {
			continue
		}
		executionID, err := util.ParseUUID(c.Source.ExecutionID)
		if err != nil {
			return empty, ErrSkillEvaluationInvalid
		}
		execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: executionID})
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrSkillEvaluationAccess
		}
		if err != nil {
			return empty, err
		}
		if c.Source.ResultRevision > 0 {
			result, err := q.GetWorkflowExecutionResult(ctx, db.GetWorkflowExecutionResultParams{WorkspaceID: p.WorkspaceID, ExecutionID: executionID, Revision: c.Source.ResultRevision})
			if errors.Is(err, pgx.ErrNoRows) {
				return empty, ErrSkillEvaluationAccess
			}
			if err != nil {
				return empty, err
			}
			if result.WorkflowInstanceID != execution.WorkflowInstanceID {
				return empty, ErrSkillEvaluationAccess
			}
		}
	}
	improvement, regression, heldOut := evaluationCounts(spec)
	row, err := q.CreateSkillEvaluationSuite(ctx, db.CreateSkillEvaluationSuiteParams{WorkspaceID: p.WorkspaceID, SkillID: p.SkillID, Name: spec.Name, SuiteHash: suite.Hash(), SchemaVersion: spec.SchemaVersion, ValidatorVersion: spec.ValidatorVersion, Definition: suite.StorageJSON(), ImprovementCount: improvement, RegressionCount: regression, HeldOutCount: heldOut, CreatedBy: p.ActorID})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = q.GetSkillEvaluationSuiteByHash(ctx, db.GetSkillEvaluationSuiteByHashParams{WorkspaceID: p.WorkspaceID, SkillID: p.SkillID, SuiteHash: suite.Hash()})
	}
	if err != nil {
		return empty, err
	}
	if _, err = decodeStoredEvaluationSuite(row); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return evaluationSummary(row), nil
}

func (s *SkillEvaluationService) readSuite(ctx context.Context, p ReadSkillEvaluationSuiteParams, permission evaluationPermission) (skilleval.Suite, error) {
	if !p.SuiteID.Valid {
		return skilleval.Suite{}, ErrSkillEvaluationAccess
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return skilleval.Suite{}, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if err = authorizeSkillEvaluation(ctx, q, p.WorkspaceID, p.SkillID, p.ActorID, permission); err != nil {
		return skilleval.Suite{}, err
	}
	row, err := q.GetSkillEvaluationSuite(ctx, db.GetSkillEvaluationSuiteParams{WorkspaceID: p.WorkspaceID, SkillID: p.SkillID, ID: p.SuiteID})
	if errors.Is(err, pgx.ErrNoRows) {
		return skilleval.Suite{}, ErrSkillEvaluationAccess
	}
	if err != nil {
		return skilleval.Suite{}, err
	}
	return decodeStoredEvaluationSuite(row)
}

func (s *SkillEvaluationService) ReadSuiteForCustodian(ctx context.Context, p ReadSkillEvaluationSuiteParams) (skilleval.Suite, error) {
	return s.readSuite(ctx, p, evaluationCustodian)
}

func (s *SkillEvaluationService) ReadImprovementCases(ctx context.Context, p ReadSkillEvaluationSuiteParams) ([]skilleval.Case, error) {
	suite, err := s.readSuite(ctx, p, evaluationEditor)
	if err != nil {
		return nil, err
	}
	return suite.ImprovementCases()
}

type ListSkillEvaluationSuitesParams struct {
	WorkspaceID pgtype.UUID
	SkillID     pgtype.UUID
	ActorID     pgtype.UUID
	CursorID    pgtype.UUID
	PageSize    int32
}
type SkillEvaluationSuitePage struct {
	Suites     []SkillEvaluationSuiteSummary
	NextCursor pgtype.UUID
}

func (s *SkillEvaluationService) ListSuites(ctx context.Context, p ListSkillEvaluationSuitesParams) (SkillEvaluationSuitePage, error) {
	empty := SkillEvaluationSuitePage{}
	if p.PageSize == 0 {
		p.PageSize = 25
	}
	if p.PageSize < 1 || p.PageSize > 100 {
		return empty, ErrSkillEvaluationInvalid
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if err = authorizeSkillEvaluation(ctx, q, p.WorkspaceID, p.SkillID, p.ActorID, evaluationMember); err != nil {
		return empty, err
	}
	var after pgtype.Timestamptz
	if p.CursorID.Valid {
		cursor, err := q.GetSkillEvaluationSuiteCursor(ctx, db.GetSkillEvaluationSuiteCursorParams{WorkspaceID: p.WorkspaceID, SkillID: p.SkillID, ID: p.CursorID})
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrSkillEvaluationInvalid
		}
		if err != nil {
			return empty, err
		}
		after = cursor.CreatedAt
	}
	rows, err := q.ListSkillEvaluationSuiteSummaries(ctx, db.ListSkillEvaluationSuiteSummariesParams{WorkspaceID: p.WorkspaceID, SkillID: p.SkillID, AfterID: p.CursorID, AfterCreatedAt: after, PageLimit: p.PageSize + 1})
	if err != nil {
		return empty, err
	}
	page := SkillEvaluationSuitePage{Suites: rows}
	if len(rows) > int(p.PageSize) {
		page.NextCursor = rows[p.PageSize-1].ID
		page.Suites = rows[:p.PageSize]
	}
	return page, nil
}
