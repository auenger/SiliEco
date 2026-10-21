package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSkillRevisionHTTPKeepsCandidateSeparateFromLiveSkill(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var sourceID, otherID string
	for index, target := range []*string{&sourceID, &otherID} {
		if err := testPool.QueryRow(ctx, `INSERT INTO skill(workspace_id,name,description,content,created_by) VALUES($1,$2,'','Live content',$3) RETURNING id`, testWorkspaceID, "revision-api-"+string(rune('a'+index)), testUserID).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM skill_revision WHERE workspace_id=$1 AND source_id IN ($2,$3)`, testWorkspaceID, sourceID, otherID)
		testPool.Exec(ctx, `DELETE FROM skill WHERE id IN ($1,$2)`, sourceID, otherID)
	})
	create := func(body map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.CreateSkillRevision(w, workflowRequest(http.MethodPost, "/revisions", body, map[string]string{"id": sourceID}))
		return w
	}
	body := map[string]any{"content": "Candidate content", "compatible_contracts": []string{"result/v1"}}
	response := create(body)
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var revision SkillRevisionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	if revision.SourceID != sourceID || revision.CreatedBy != testUserID {
		t.Fatal("untrusted revision identity")
	}
	replay := create(body)
	var repeated SkillRevisionResponse
	if err := json.Unmarshal(replay.Body.Bytes(), &repeated); err != nil || replay.Code != http.StatusCreated || repeated.ID != revision.ID {
		t.Fatalf("duplicate creation: %d %v", replay.Code, err)
	}
	for _, source := range []string{sourceID, otherID} {
		read := httptest.NewRecorder()
		testHandler.GetSkillRevision(read, workflowRequest(http.MethodGet, "/revision", nil, map[string]string{"id": source, "revisionId": revision.ID}))
		want := http.StatusOK
		if source == otherID {
			want = http.StatusNotFound
		}
		if read.Code != want {
			t.Fatalf("source-bound read: %d %s", read.Code, read.Body.String())
		}
	}
	if invalid := create(map[string]any{"content": "Candidate", "revision_hash": "forged"}); invalid.Code != http.StatusBadRequest {
		t.Fatalf("client hash accepted: %d", invalid.Code)
	}
	request := workflowRequest(http.MethodPost, "/revisions", body, map[string]string{"id": sourceID})
	request.Header.Set("X-Agent-ID", sourceID)
	denied := httptest.NewRecorder()
	testHandler.CreateSkillRevision(denied, request)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("agent impersonated member: %d", denied.Code)
	}
	for _, content := range []string{"Second candidate", "Third candidate"} {
		if got := create(map[string]any{"content": content}); got.Code != http.StatusCreated {
			t.Fatalf("seed revision: %d %s", got.Code, got.Body.String())
		}
	}
	list := func(cursor string) SkillRevisionPageResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListSkillRevisions(w, workflowRequest(http.MethodGet, "/revisions?page_size=1&cursor="+cursor, nil, map[string]string{"id": sourceID}))
		if w.Code != http.StatusOK {
			t.Fatalf("list revisions: %d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `"bundle":`) {
			t.Fatal("summary endpoint leaked full bundle")
		}
		var page SkillRevisionPageResponse
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := list("")
	if len(first.Revisions) != 1 || first.NextCursor == nil {
		t.Fatalf("missing first page cursor: %+v", first)
	}
	newTop := create(map[string]any{"content": "New candidate during pagination"})
	if newTop.Code != http.StatusCreated {
		t.Fatal(newTop.Body.String())
	}
	seen := map[string]bool{first.Revisions[0].ID: true}
	cursor := first.NextCursor
	for cursor != nil {
		page := list(*cursor)
		for _, item := range page.Revisions {
			if seen[item.ID] {
				t.Fatal("pagination repeated revision")
			}
			seen[item.ID] = true
		}
		cursor = page.NextCursor
		if len(seen) > 3 {
			t.Fatal("pagination shifted after a new insert")
		}
	}
	if len(seen) != 3 || !seen[revision.ID] {
		t.Fatalf("pagination skipped old revision: %+v", seen)
	}
	wrongCursor := httptest.NewRecorder()
	testHandler.ListSkillRevisions(wrongCursor, workflowRequest(http.MethodGet, "/revisions?cursor="+revision.ID, nil, map[string]string{"id": otherID}))
	if wrongCursor.Code != http.StatusBadRequest {
		t.Fatalf("foreign source cursor accepted: %d", wrongCursor.Code)
	}
	var content string
	if err := testPool.QueryRow(ctx, `SELECT content FROM skill WHERE id=$1`, sourceID).Scan(&content); err != nil || content != "Live content" {
		t.Fatalf("candidate changed live skill: %s %v", content, err)
	}
}
