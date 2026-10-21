package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/skillbundle"
)

func TestWorkspaceSkillRevisionDerivesIdentityAndPreservesSource(t *testing.T) {
	pool := newTaskClaimRacePool(t)
	ctx := context.Background()
	q := db.New(pool)
	agentID := createClaimCapacityFixture(t, ctx, pool)
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	source, err := q.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: agent.WorkspaceID, Name: "revision-source", Description: "Original description", Content: "# Live content", Config: []byte(`{}`), CreatedBy: agent.OwnerID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM skill_revision WHERE workspace_id=$1`, agent.WorkspaceID)
		q.DeleteSkill(context.Background(), db.DeleteSkillParams{ID: source.ID, WorkspaceID: agent.WorkspaceID})
	})
	svc := SkillRevisionService{Queries: q, TxStarter: pool}
	p := CreateWorkspaceSkillRevisionParams{WorkspaceID: agent.WorkspaceID, SkillID: source.ID, ActorID: agent.OwnerID, Content: "# Candidate content", Files: []skillbundle.File{{Path: "scripts/check.sh", Content: "echo check"}}, CompatibleContracts: []string{"result/v1"}}
	revision, err := svc.CreateWorkspaceRevision(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.CreateWorkspaceRevision(ctx, p)
	if err != nil || replay.ID != revision.ID {
		t.Fatalf("duplicate creation changed identity: %v", err)
	}
	if revision.SourceID != util.UUIDToString(source.ID) || revision.SourceKind != "workspace" || revision.CreatedBy != agent.OwnerID {
		t.Fatal("revision has untrusted provenance")
	}
	var bundle AgentSkillData
	var manifest skillbundle.RevisionManifest
	if err := json.Unmarshal(revision.Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(revision.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Hash != revision.RevisionHash || manifest.BundleHash != bundle.Hash || bundle.Content != p.Content {
		t.Fatal("stored hashes do not match candidate bundle")
	}
	current, err := q.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: p.SkillID})
	if err != nil || current.Content != "# Live content" {
		t.Fatalf("candidate overwrote live source: %v", err)
	}
	denied := p
	denied.ActorID = util.MustParseUUID(uuid.NewString())
	if _, err := svc.CreateWorkspaceRevision(ctx, denied); !errors.Is(err, ErrSkillRevisionAccess) {
		t.Fatalf("nonmember created revision: %v", err)
	}
	denied = p
	denied.SkillID = util.MustParseUUID(uuid.NewString())
	if _, err := svc.CreateWorkspaceRevision(ctx, denied); !errors.Is(err, ErrSkillRevisionAccess) {
		t.Fatalf("unresolved source accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, p.WorkspaceID, p.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE skill SET created_by=NULL WHERE id=$1`, p.SkillID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWorkspaceRevision(ctx, p); !errors.Is(err, ErrSkillRevisionAccess) {
		t.Fatalf("noncreator member created revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE skill SET created_by=$2 WHERE id=$1`, p.SkillID, p.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWorkspaceRevision(ctx, p); err != nil {
		t.Fatalf("member creator denied revision: %v", err)
	}
	invalid := p
	invalid.Files = []skillbundle.File{{Path: "../escape", Content: "x"}}
	if _, err := svc.CreateWorkspaceRevision(ctx, invalid); !errors.Is(err, ErrSkillRevisionInvalid) {
		t.Fatalf("invalid candidate accepted: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM skill_revision WHERE workspace_id=$1`, p.WorkspaceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed writes leaked revisions: %d %v", count, err)
	}
}
