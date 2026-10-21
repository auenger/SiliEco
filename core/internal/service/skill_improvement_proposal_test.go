package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestSkillImprovementProposalPinsEvidenceAndChecksBoundaries(t *testing.T) {
	pool, workflow, submission, _ := workflowResultFixture(t)
	ctx := context.Background()
	q := workflow.Queries
	result, err := workflow.SubmitResult(ctx, submission)
	if err != nil {
		t.Fatal(err)
	}
	source, err := q.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: submission.WorkspaceID, Name: "proposal-source", Content: "# Saved", Config: []byte(`{}`), CreatedBy: submission.ActorID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM skill_improvement_proposal WHERE workspace_id=$1`, submission.WorkspaceID)
		pool.Exec(context.Background(), `DELETE FROM skill_revision WHERE workspace_id=$1`, submission.WorkspaceID)
		q.DeleteSkill(context.Background(), db.DeleteSkillParams{WorkspaceID: submission.WorkspaceID, ID: source.ID})
	})
	svc := SkillRevisionService{Queries: q, TxStarter: pool}
	revisionParams := CreateWorkspaceSkillRevisionParams{WorkspaceID: submission.WorkspaceID, SkillID: source.ID, ActorID: submission.ActorID, Content: "# Baseline"}
	baseline, err := svc.CreateWorkspaceRevision(ctx, revisionParams)
	if err != nil {
		t.Fatal(err)
	}
	revisionParams.Content = "# Candidate"
	candidate, err := svc.CreateWorkspaceRevision(ctx, revisionParams)
	if err != nil {
		t.Fatal(err)
	}
	p := CreateSkillImprovementProposalParams{WorkspaceID: submission.WorkspaceID, SkillID: source.ID, ActorID: submission.ActorID, RequestID: util.MustParseUUID(uuid.NewString()), BaselineRevisionID: baseline.ID, CandidateRevisionID: candidate.ID, SourceExecutionID: submission.ExecutionID, SourceResultRevision: result.Revision, Category: "skill_instruction", Reason: "The report omitted a verification step", Hypothesis: "Explicit verification instructions reduce omissions", ApplicableScope: "Report-producing stages"}
	// Concurrent retries share one immutable record and author/request identity.
	var rows [2]db.SkillImprovementProposal
	var failures [2]error
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(i int) { defer wg.Done(); rows[i], failures[i] = svc.CreateImprovementProposal(ctx, p) }(i)
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if rows[0].ID != rows[1].ID {
		t.Fatal("concurrent replay created duplicate proposals")
	}
	proposal := rows[0]
	var evidence SkillProposalEvidence
	if err = json.Unmarshal(proposal.Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.ExecutionID != util.UUIDToString(submission.ExecutionID) || evidence.InstanceID != util.UUIDToString(submission.InstanceID) || evidence.ResultID != util.UUIDToString(result.ID) || evidence.ResultRevision != 1 || evidence.ResultHash != skillProposalDigest(result.Data) || evidence.Attempt != 1 {
		t.Fatalf("unverified provenance: %+v", evidence)
	}
	if strings.Contains(string(proposal.Evidence), "source_attachment") || strings.Contains(string(proposal.Evidence), "input_snapshot") {
		t.Fatal("proposal copied raw execution data")
	}
	submission.ExpectedRevision = 1
	if _, err = workflow.SubmitResult(ctx, submission); err != nil {
		t.Fatal(err)
	}
	replay, err := svc.CreateImprovementProposal(ctx, p)
	if err != nil || string(replay.Evidence) != string(proposal.Evidence) {
		t.Fatalf("new result changed original evidence: %v", err)
	}
	changed := p
	changed.Hypothesis = "A different hypothesis"
	if _, err = svc.CreateImprovementProposal(ctx, changed); !errors.Is(err, ErrSkillProposalConflict) {
		t.Fatalf("request reuse did not conflict: %v", err)
	}
	for _, mutate := range []func(*CreateSkillImprovementProposalParams){
		func(p *CreateSkillImprovementProposalParams) { p.WorkspaceID = util.MustParseUUID(uuid.NewString()) },
		func(p *CreateSkillImprovementProposalParams) { p.ActorID = util.MustParseUUID(uuid.NewString()) },
		func(p *CreateSkillImprovementProposalParams) {
			p.CandidateRevisionID = util.MustParseUUID(uuid.NewString())
		},
		func(p *CreateSkillImprovementProposalParams) {
			p.SourceExecutionID = util.MustParseUUID(uuid.NewString())
		},
		func(p *CreateSkillImprovementProposalParams) { p.SourceResultRevision = 100 },
	} {
		denied := p
		denied.RequestID = util.MustParseUUID(uuid.NewString())
		mutate(&denied)
		if _, err = svc.CreateImprovementProposal(ctx, denied); !errors.Is(err, ErrSkillRevisionAccess) {
			t.Fatalf("invalid provenance/actor accepted: %v", err)
		}
	}
	otherSource, err := q.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: p.WorkspaceID, Name: "other-proposal-source", Content: "# Other", Config: []byte(`{}`), CreatedBy: p.ActorID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		q.DeleteSkill(context.Background(), db.DeleteSkillParams{WorkspaceID: p.WorkspaceID, ID: otherSource.ID})
	})
	otherRevision, err := svc.CreateWorkspaceRevision(ctx, CreateWorkspaceSkillRevisionParams{WorkspaceID: p.WorkspaceID, SkillID: otherSource.ID, ActorID: p.ActorID, Content: "# Other candidate"})
	if err != nil {
		t.Fatal(err)
	}
	wrongSource := p
	wrongSource.RequestID = util.MustParseUUID(uuid.NewString())
	wrongSource.CandidateRevisionID = otherRevision.ID
	if _, err = svc.CreateImprovementProposal(ctx, wrongSource); !errors.Is(err, ErrSkillRevisionAccess) {
		t.Fatalf("candidate from another skill accepted: %v", err)
	}
	invalid := p
	invalid.RequestID = util.MustParseUUID(uuid.NewString())
	invalid.SourceExecutionID = pgtype.UUID{}
	if _, err = svc.CreateImprovementProposal(ctx, invalid); !errors.Is(err, ErrSkillRevisionInvalid) {
		t.Fatalf("result without execution accepted: %v", err)
	}
	invalid = p
	invalid.Category = "every_failure_is_a_skill_problem"
	if _, err = svc.CreateImprovementProposal(ctx, invalid); !errors.Is(err, ErrSkillRevisionInvalid) {
		t.Fatalf("unknown classification accepted: %v", err)
	}
	invalid = p
	invalid.Reason = "  "
	if _, err = svc.CreateImprovementProposal(ctx, invalid); !errors.Is(err, ErrSkillRevisionInvalid) {
		t.Fatalf("empty rationale accepted: %v", err)
	}
	// A member may not reuse an administrator's candidate provenance as authority.
	if _, err = pool.Exec(ctx, `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, p.WorkspaceID, p.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE skill SET created_by=NULL WHERE id=$1`, p.SkillID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateImprovementProposal(ctx, p); !errors.Is(err, ErrSkillRevisionAccess) {
		t.Fatalf("revoked editor replay accepted: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE skill SET created_by=$2 WHERE id=$1`, p.SkillID, p.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateImprovementProposal(ctx, p); err != nil {
		t.Fatalf("member creator replay denied: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE skill_improvement_proposal SET reason='rewritten' WHERE id=$1`, proposal.ID); err == nil {
		t.Fatal("proposal history could be overwritten")
	}
	if _, err = q.GetSkillImprovementProposal(ctx, db.GetSkillImprovementProposalParams{WorkspaceID: util.MustParseUUID(uuid.NewString()), SkillID: p.SkillID, ID: proposal.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace read: %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM skill_improvement_proposal WHERE workspace_id=$1`, p.WorkspaceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed writes leaked proposals: %d %v", count, err)
	}
	manual := p
	manual.RequestID = util.MustParseUUID(uuid.NewString())
	manual.SourceExecutionID = pgtype.UUID{}
	manual.SourceResultRevision = 0
	manual.Category = "human_correction"
	manualProposal, err := svc.CreateImprovementProposal(ctx, manual)
	if err != nil || string(manualProposal.Evidence) != "{}" {
		t.Fatalf("manual feedback fabricated execution evidence: %s %v", manualProposal.Evidence, err)
	}
	failedExecution := p
	failedExecution.RequestID = util.MustParseUUID(uuid.NewString())
	failedExecution.SourceResultRevision = 0
	failedExecution.Category = "tool_failure"
	withoutResult, err := svc.CreateImprovementProposal(ctx, failedExecution)
	if err != nil {
		t.Fatal(err)
	}
	var executionEvidence SkillProposalEvidence
	if err = json.Unmarshal(withoutResult.Evidence, &executionEvidence); err != nil {
		t.Fatal(err)
	}
	if executionEvidence.ExecutionID == "" || executionEvidence.ResultID != "" || executionEvidence.ResultRevision != 0 {
		t.Fatal("execution-only feedback invented result evidence")
	}
	current, err := q.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: p.SkillID})
	if err != nil || current.Content != "# Saved" {
		t.Fatalf("proposal altered live skill: %v", err)
	}
	if err = q.DeleteWorkspace(ctx, p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM skill_improvement_proposal WHERE workspace_id=$1`, p.WorkspaceID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("workspace cleanup leaked proposals: %d %v", count, err)
	}
}
