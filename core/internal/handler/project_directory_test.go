package handler

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectDirectoryScopeAndOffline(t *testing.T) {
	createProject := func(title string) string {
		w := httptest.NewRecorder()
		testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": title}))
		if w.Code != 201 {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		var project ProjectResponse
		if err := json.Unmarshal(w.Body.Bytes(), &project); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			testHandler.DeleteProject(httptest.NewRecorder(), withURLParam(newRequest("DELETE", "/api/projects/"+project.ID, nil), "id", project.ID))
		})
		return project.ID
	}
	projectID := createProject("Directory scope test")
	otherID := createProject("Other directory project")
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/projects/"+projectID+"/resources", map[string]any{"resource_type": "local_directory", "resource_ref": map[string]any{"daemon_id": "offline-test-machine", "local_path": "/test/project"}}), "id", projectID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != 201 {
		t.Fatalf("bind: %d %s", w.Code, w.Body.String())
	}
	var resource ProjectResourceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resource); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		project string
		want    int
	}{{projectID, http.StatusServiceUnavailable}, {otherID, http.StatusNotFound}} {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("GET", "/api/projects/"+tc.project+"/resources/"+resource.ID+"/directory", nil), "id", tc.project)
		chi.RouteContext(req.Context()).URLParams.Add("resourceId", resource.ID)
		testHandler.BrowseProjectDirectory(w, req)
		if w.Code != tc.want {
			t.Fatalf("directory read: got %d want %d: %s", w.Code, tc.want, w.Body.String())
		}
	}
}
