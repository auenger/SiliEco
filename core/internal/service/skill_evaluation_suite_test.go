package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/skilleval"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

func evaluationStorageSpec(executionID string) skilleval.SuiteSpec {
	cases := []skilleval.Case{}
	for _, partition := range []skilleval.Partition{skilleval.Improvement, skilleval.Regression, skilleval.HeldOut} {
		input := json.RawMessage(`{"number":1e+2,"prompt":"` + string(partition) + `-private-input"}`)
		answer, _ := json.Marshal(string(partition) + "-private-answer")
		schema := workflowcontract.Schema{Type: "object", Properties: map[string]workflowcontract.Schema{"answer": {Type: "string", Enum: []json.RawMessage{answer}}}, Required: []string{"answer"}}
		cases = append(cases, skilleval.Case{ID: string(partition), Partition: partition, Input: input, OutputContract: workflowcontract.Contract{SchemaVersion: workflowcontract.Version, Data: schema}, Checks: []skilleval.DataCheck{}, Source: &skilleval.CaseSource{ExecutionID: executionID, ResultRevision: 1}})
	}
	return skilleval.SuiteSpec{SchemaVersion: skilleval.SchemaVersion, ValidatorVersion: skilleval.ValidatorVersion, Name: "Storage evaluation", Cases: cases}
}

func TestSkillEvaluationSuiteStorageAndPrivateReadBoundaries(t *testing.T) {
	pool, workflow, submission, _ := workflowResultFixture(t)
	ctx := context.Background()
	q := workflow.Queries
	if _, err := workflow.SubmitResult(ctx, submission); err != nil {
		t.Fatal(err)
	}
	source, err := q.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: submission.WorkspaceID, Name: "evaluation-source", Content: "# Live", Config: []byte(`{}`), CreatedBy: submission.ActorID})
	if err != nil {
		t.Fatal(err)
	}
	otherSource, err := q.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: submission.WorkspaceID, Name: "other-evaluation-source", Content: "# Other", Config: []byte(`{}`), CreatedBy: submission.ActorID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM skill_evaluation_suite WHERE workspace_id=$1`, submission.WorkspaceID)
		q.DeleteSkill(context.Background(), db.DeleteSkillParams{WorkspaceID: submission.WorkspaceID, ID: source.ID})
		q.DeleteSkill(context.Background(), db.DeleteSkillParams{WorkspaceID: submission.WorkspaceID, ID: otherSource.ID})
	})
	svc := SkillEvaluationService{Queries: q, TxStarter: pool}
	spec := evaluationStorageSpec(util.UUIDToString(submission.ExecutionID))
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	create := CreateSkillEvaluationSuiteParams{WorkspaceID: submission.WorkspaceID, SkillID: source.ID, ActorID: submission.ActorID, Definition: raw}
	summary, err := svc.CreateSuite(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.CreateSuite(ctx, create)
	if err != nil || replay.ID != summary.ID {
		t.Fatalf("duplicate suite version: %v", err)
	}
	encoded, _ := json.Marshal(summary)
	if strings.Contains(string(encoded), "definition") || strings.Contains(string(encoded), "private-answer") || strings.Contains(string(encoded), "private-input") || summary.ImprovementCount != 1 || summary.RegressionCount != 1 || summary.HeldOutCount != 1 {
		t.Fatalf("creation leaked private definition or lost counts: %s", encoded)
	}
	read := ReadSkillEvaluationSuiteParams{WorkspaceID: create.WorkspaceID, SkillID: create.SkillID, ActorID: create.ActorID, SuiteID: summary.ID}
	suite, err := svc.ReadSuiteForCustodian(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	if suite.Hash() != summary.SuiteHash || !strings.Contains(string(suite.StorageJSON()), "1e+2") || !strings.Contains(string(suite.StorageJSON()), "held_out-private-answer") {
		t.Fatal("definition bytes changed or custodian lost private data")
	}
	stored, err := q.GetSkillEvaluationSuite(ctx, db.GetSkillEvaluationSuiteParams{WorkspaceID: read.WorkspaceID, SkillID: read.SkillID, ID: read.SuiteID})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored.Definition, suite.StorageJSON()) {
		t.Fatal("database normalized suite bytes")
	}
	corrupt := stored
	corrupt.Definition = []byte(`{}`)
	if _, err = decodeStoredEvaluationSuite(corrupt); !errors.Is(err, ErrSkillEvaluationIntegrity) {
		t.Fatalf("corrupt definition accepted: %v", err)
	}
	corrupt = stored
	corrupt.SuiteHash = "sha256:" + strings.Repeat("0", 64)
	if _, err = decodeStoredEvaluationSuite(corrupt); !errors.Is(err, ErrSkillEvaluationIntegrity) {
		t.Fatalf("wrong digest accepted: %v", err)
	}
	corrupt = stored
	corrupt.HeldOutCount = 0
	if _, err = decodeStoredEvaluationSuite(corrupt); !errors.Is(err, ErrSkillEvaluationIntegrity) {
		t.Fatalf("wrong summary accepted: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE skill_evaluation_suite SET definition='{}'::bytea WHERE id=$1`, summary.ID); err == nil {
		t.Fatal("immutable suite was rewritten")
	}
	wrong := read
	wrong.SkillID = otherSource.ID
	if _, err = svc.ReadSuiteForCustodian(ctx, wrong); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("foreign skill definition read: %v", err)
	}
	wrong = read
	wrong.ActorID = util.MustParseUUID(uuid.NewString())
	if _, err = svc.ReadSuiteForCustodian(ctx, wrong); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("nonmember definition read: %v", err)
	}
	if _, err = q.GetSkillEvaluationSuite(ctx, db.GetSkillEvaluationSuiteParams{WorkspaceID: util.MustParseUUID(uuid.NewString()), SkillID: source.ID, ID: summary.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace suite exposed: %v", err)
	}
	// Source references must resolve before any new version can be persisted.
	invalidSpec := evaluationStorageSpec(uuid.NewString())
	invalidRaw, _ := json.Marshal(invalidSpec)
	invalid := create
	invalid.Definition = invalidRaw
	if _, err = svc.CreateSuite(ctx, invalid); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("unresolved source accepted: %v", err)
	}
	invalidSpec = evaluationStorageSpec(util.UUIDToString(submission.ExecutionID))
	invalidSpec.Cases[0].Source.ResultRevision = 100
	invalidRaw, _ = json.Marshal(invalidSpec)
	invalid.Definition = invalidRaw
	if _, err = svc.CreateSuite(ctx, invalid); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("unresolved result accepted: %v", err)
	}
	// Fixed cursor pages remain stable if newer versions are inserted.
	for _, name := range []string{"Second version", "Third version"} {
		spec.Name = name
		create.Definition, _ = json.Marshal(spec)
		if _, err = svc.CreateSuite(ctx, create); err != nil {
			t.Fatal(err)
		}
	}
	list := ListSkillEvaluationSuitesParams{WorkspaceID: read.WorkspaceID, SkillID: read.SkillID, ActorID: read.ActorID, PageSize: 1}
	page, err := svc.ListSuites(ctx, list)
	if err != nil || len(page.Suites) != 1 || !page.NextCursor.Valid {
		t.Fatalf("first suite page: %+v %v", page, err)
	}
	encoded, _ = json.Marshal(page)
	if strings.Contains(string(encoded), "private-answer") || strings.Contains(string(encoded), "definition") {
		t.Fatal("list exposed private material")
	}
	seen := map[string]bool{util.UUIDToString(page.Suites[0].ID): true}
	spec.Name = "New version during pagination"
	create.Definition, _ = json.Marshal(spec)
	if _, err = svc.CreateSuite(ctx, create); err != nil {
		t.Fatal(err)
	}
	for page.NextCursor.Valid {
		list.CursorID = page.NextCursor
		page, err = svc.ListSuites(ctx, list)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page.Suites {
			id := util.UUIDToString(row.ID)
			if seen[id] {
				t.Fatal("duplicate suite page")
			}
			seen[id] = true
		}
		if len(seen) > 3 {
			t.Fatal("new insert shifted old pages")
		}
	}
	if len(seen) != 3 || !seen[util.UUIDToString(summary.ID)] {
		t.Fatal("suite pagination skipped old versions")
	}
	wrongCursor := list
	wrongCursor.SkillID = otherSource.ID
	wrongCursor.CursorID = summary.ID
	if _, err = svc.ListSuites(ctx, wrongCursor); !errors.Is(err, ErrSkillEvaluationInvalid) {
		t.Fatalf("cursor from another skill accepted: %v", err)
	}
	// Skill authors get improvement cases; being an author never reveals held-out answers.
	if _, err = pool.Exec(ctx, `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, read.WorkspaceID, read.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ReadSuiteForCustodian(ctx, read); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("member author read held-out answers: %v", err)
	}
	if _, err = svc.CreateSuite(ctx, create); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("member author created private evaluation suite: %v", err)
	}
	cases, err := svc.ReadImprovementCases(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(cases)
	if len(cases) != 1 || cases[0].Partition != skilleval.Improvement || strings.Contains(string(encoded), "held_out-private") || strings.Contains(string(encoded), "regression-private") {
		t.Fatalf("improver saw private partitions: %s", encoded)
	}
	if _, err = pool.Exec(ctx, `UPDATE skill SET created_by=NULL WHERE id=$1`, read.SkillID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ReadImprovementCases(ctx, read); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("noneditor read improvement answers: %v", err)
	}
	if _, err = svc.ListSuites(ctx, list); err != nil {
		t.Fatalf("member lost metadata access: %v", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, read.WorkspaceID, read.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ListSuites(ctx, list); !errors.Is(err, ErrSkillEvaluationAccess) {
		t.Fatalf("removed member read suite list: %v", err)
	}
	if err = q.DeleteWorkspace(ctx, read.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM skill_evaluation_suite WHERE workspace_id=$1`, read.WorkspaceID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("workspace deletion left evaluation data: %d %v", count, err)
	}
}
