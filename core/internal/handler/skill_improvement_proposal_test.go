package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestSkillImprovementProposalHTTPBoundariesAndPagination(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var sourceID, otherID string
	for i, target := range []*string{&sourceID, &otherID} {
		if err := testPool.QueryRow(ctx, `INSERT INTO skill(workspace_id,name,description,content,created_by) VALUES($1,$2,'','Live source',$3) RETURNING id`, testWorkspaceID, "proposal-api-"+string(rune('a'+i)), testUserID).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM skill_improvement_proposal WHERE workspace_id=$1 AND skill_id IN ($2,$3)`, testWorkspaceID, sourceID, otherID)
		testPool.Exec(ctx, `DELETE FROM skill_revision WHERE workspace_id=$1 AND source_id IN ($2,$3)`, testWorkspaceID, sourceID, otherID)
		testPool.Exec(ctx, `DELETE FROM skill WHERE id IN ($1,$2)`, sourceID, otherID)
	})
	revisions := []string{}
	for _, content := range []string{"# Baseline", "# Candidate"} {
		w := httptest.NewRecorder()
		testHandler.CreateSkillRevision(w, workflowRequest(http.MethodPost, "/revisions", map[string]any{"content": content}, map[string]string{"id": sourceID}))
		if w.Code != http.StatusCreated {
			t.Fatal(w.Body.String())
		}
		var row SkillRevisionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, row.ID)
	}
	body := map[string]any{"request_id": uuid.NewString(), "baseline_revision_id": revisions[0], "candidate_revision_id": revisions[1], "category": "human_correction", "reason": "Manual correction", "hypothesis": "Explicit steps reduce omissions", "applicable_scope": "Report stages"}
	create := func(payload map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.CreateSkillImprovementProposal(w, workflowRequest(http.MethodPost, "/proposals", payload, map[string]string{"id": sourceID}))
		return w
	}
	first := create(body)
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	var proposal SkillImprovementProposalResponse
	if err := json.Unmarshal(first.Body.Bytes(), &proposal); err != nil {
		t.Fatal(err)
	}
	if proposal.CreatedBy != testUserID || proposal.SkillID != sourceID || string(proposal.Evidence) != "{}" {
		t.Fatal("untrusted proposal provenance")
	}
	var replay SkillImprovementProposalResponse
	response := create(body)
	if err := json.Unmarshal(response.Body.Bytes(), &replay); err != nil || response.Code != http.StatusCreated || replay.ID != proposal.ID {
		t.Fatalf("proposal replay: %d %v", response.Code, err)
	}
	body["hypothesis"] = "Changed hypothesis"
	if w := create(body); w.Code != http.StatusConflict {
		t.Fatalf("request conflict: %d %s", w.Code, w.Body.String())
	}
	body["hypothesis"] = "Explicit steps reduce omissions"
	for field, value := range map[string]any{"created_by": testUserID, "evidence": map[string]any{"attempt": 1}, "request_id": "invalid", "source_execution_id": "invalid", "source_result_revision": 1} {
		invalid := map[string]any{}
		for k, v := range body {
			invalid[k] = v
		}
		invalid[field] = value
		if w := create(invalid); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s accepted: %d %s", field, w.Code, w.Body.String())
		}
	}
	req := workflowRequest(http.MethodPost, "/proposals", body, map[string]string{"id": sourceID})
	req.Header.Set("X-Agent-ID", sourceID)
	denied := httptest.NewRecorder()
	testHandler.CreateSkillImprovementProposal(denied, req)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("agent proposal accepted: %d", denied.Code)
	}
	for _, id := range []string{sourceID, otherID} {
		w := httptest.NewRecorder()
		testHandler.GetSkillImprovementProposal(w, workflowRequest(http.MethodGet, "/proposal", nil, map[string]string{"id": id, "proposalId": proposal.ID}))
		want := http.StatusOK
		if id == otherID {
			want = http.StatusNotFound
		}
		if w.Code != want {
			t.Fatalf("source-bound read: %d", w.Code)
		}
	}
	for i := 0; i < 2; i++ {
		body["request_id"] = uuid.NewString()
		if w := create(body); w.Code != http.StatusCreated {
			t.Fatal(w.Body.String())
		}
	}
	list := func(cursor string) SkillImprovementProposalPageResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListSkillImprovementProposals(w, workflowRequest(http.MethodGet, "/proposals?page_size=1&cursor="+cursor, nil, map[string]string{"id": sourceID}))
		if w.Code != http.StatusOK {
			t.Fatal(w.Body.String())
		}
		var page SkillImprovementProposalPageResponse
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	page := list("")
	if len(page.Proposals) != 1 || page.NextCursor == nil {
		t.Fatal("missing page cursor")
	}
	seen := map[string]bool{page.Proposals[0].ID: true}
	body["request_id"] = uuid.NewString()
	if w := create(body); w.Code != http.StatusCreated {
		t.Fatal(w.Body.String())
	}
	for page.NextCursor != nil {
		page = list(*page.NextCursor)
		for _, row := range page.Proposals {
			if seen[row.ID] {
				t.Fatal("duplicate proposal page")
			}
			seen[row.ID] = true
		}
		if len(seen) > 3 {
			t.Fatal("new insertion shifted page")
		}
	}
	if len(seen) != 3 || !seen[proposal.ID] {
		t.Fatal("pagination skipped older proposals")
	}
	wrong := httptest.NewRecorder()
	testHandler.ListSkillImprovementProposals(wrong, workflowRequest(http.MethodGet, "/proposals?cursor="+proposal.ID, nil, map[string]string{"id": otherID}))
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("foreign cursor accepted: %d", wrong.Code)
	}
	var live string
	if err := testPool.QueryRow(ctx, `SELECT content FROM skill WHERE id=$1`, sourceID).Scan(&live); err != nil || live != "Live source" {
		t.Fatalf("proposal changed live content: %s %v", live, err)
	}
}
