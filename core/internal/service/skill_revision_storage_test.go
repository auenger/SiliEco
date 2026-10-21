package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/skillbundle"
)

func TestSkillRevisionStorageIsImmutableAndWorkspaceScoped(t *testing.T) {
	pool := newTaskClaimRacePool(t)
	ctx := context.Background()
	q := db.New(pool)
	agentID := createClaimCapacityFixture(t, ctx, pool)
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM skill_revision WHERE workspace_id=$1`, agent.WorkspaceID)
	})
	spec := skillbundle.RevisionSpec{Skill: skillbundle.Skill{ID: "builtin:builder", Source: skillbundle.SourceBuiltin, Name: "builder", Content: "# Immutable build instructions"}}
	manifest, err := skillbundle.BuildRevisionManifest(spec)
	if err != nil {
		t.Fatal(err)
	}
	bundleJSON, err := json.Marshal(spec.Skill)
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	params := db.CreateSkillRevisionParams{WorkspaceID: agent.WorkspaceID, SourceKind: spec.Skill.Source, SourceID: spec.Skill.ID, RevisionHash: manifest.Hash, Bundle: bundleJSON, Manifest: manifestJSON, CreatedBy: agent.OwnerID}
	revision, err := q.CreateSkillRevision(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateSkillRevision(ctx, params); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate revision not deduplicated: %v", err)
	}
	found, err := q.GetSkillRevisionByHash(ctx, db.GetSkillRevisionByHashParams{WorkspaceID: agent.WorkspaceID, SourceKind: spec.Skill.Source, SourceID: spec.Skill.ID, RevisionHash: manifest.Hash})
	if err != nil || found.ID != revision.ID {
		t.Fatalf("content lookup lost revision: %v", err)
	}
	if _, err := q.GetSkillRevisionInWorkspace(ctx, db.GetSkillRevisionInWorkspaceParams{WorkspaceID: util.MustParseUUID(uuid.NewString()), ID: revision.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace revision exposed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE skill_revision SET bundle='{}' WHERE id=$1`, revision.ID); err == nil {
		t.Fatal("immutable revision was updated")
	}
	current, err := q.GetSkillRevisionInWorkspace(ctx, db.GetSkillRevisionInWorkspaceParams{WorkspaceID: agent.WorkspaceID, ID: revision.ID})
	if err != nil || string(current.Bundle) != string(revision.Bundle) {
		t.Fatalf("failed update changed revision: %v", err)
	}
	if err := q.DeleteWorkspace(ctx, agent.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM skill_revision WHERE workspace_id=$1`, agent.WorkspaceID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("workspace deletion leaked revisions: %d %v", remaining, err)
	}
}
