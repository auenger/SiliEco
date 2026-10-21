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
	"github.com/silieco-ai/silieco/core/pkg/skillbundle"
)

var ErrSkillRevisionAccess = errors.New("skill revision access denied")
var ErrSkillRevisionInvalid = errors.New("invalid skill revision")

type SkillRevisionService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

type CreateWorkspaceSkillRevisionParams struct {
	WorkspaceID         pgtype.UUID
	SkillID             pgtype.UUID
	ActorID             pgtype.UUID
	Content             string
	Files               []skillbundle.File
	CompatibleContracts []string
	ToolDependencies    []skillbundle.ToolDependency
}

// CreateWorkspaceRevision records a candidate without changing live skill content
// or any published pointer. Identity and hashes are derived here, never trusted
// from a candidate payload. Builtin/runtime source flows are separate operations.
func (s *SkillRevisionService) CreateWorkspaceRevision(ctx context.Context, p CreateWorkspaceSkillRevisionParams) (db.SkillRevision, error) {
	empty := db.SkillRevision{}
	if !p.WorkspaceID.Valid || !p.SkillID.Valid || !p.ActorID.Valid {
		return empty, ErrSkillRevisionAccess
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if _, err := q.LockWorkspaceForSkillRevisionCreate(ctx, p.WorkspaceID); err != nil {
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
	spec := skillbundle.RevisionSpec{Skill: skillbundle.Skill{ID: util.UUIDToString(source.ID), Source: skillbundle.SourceWorkspace, Name: source.Name, Description: source.Description, Content: p.Content, Files: p.Files}, CompatibleContracts: p.CompatibleContracts, ToolDependencies: p.ToolDependencies}
	manifest, err := skillbundle.BuildRevisionManifest(spec)
	if err != nil {
		return empty, fmt.Errorf("%w: %v", ErrSkillRevisionInvalid, err)
	}
	files := make([]AgentSkillFileData, 0, len(p.Files))
	for _, file := range p.Files {
		files = append(files, AgentSkillFileData{Path: file.Path, Content: file.Content})
	}
	bundles, _ := BuildAgentSkillBundles([]AgentSkillData{{ID: spec.Skill.ID, Source: spec.Skill.Source, Name: source.Name, Description: spec.Skill.Description, Content: p.Content, Files: files}})
	bundleJSON, err := json.Marshal(bundles[0])
	if err != nil {
		return empty, err
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return empty, err
	}
	revision, err := q.CreateSkillRevision(ctx, db.CreateSkillRevisionParams{WorkspaceID: p.WorkspaceID, SourceKind: spec.Skill.Source, SourceID: spec.Skill.ID, RevisionHash: manifest.Hash, Bundle: bundleJSON, Manifest: manifestJSON, CreatedBy: p.ActorID})
	if errors.Is(err, pgx.ErrNoRows) {
		revision, err = q.GetSkillRevisionByHash(ctx, db.GetSkillRevisionByHashParams{WorkspaceID: p.WorkspaceID, SourceKind: spec.Skill.Source, SourceID: spec.Skill.ID, RevisionHash: manifest.Hash})
	}
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return revision, nil
}
