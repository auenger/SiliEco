package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

func (h *Handler) BrowseProjectDirectory(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(project.WorkspaceID)); !ok {
		return
	}
	resources, err := h.Queries.ListProjectResources(r.Context(), project.ID)
	if err != nil {
		writeError(w, 500, "cannot load project resources")
		return
	}
	var ref localDirectoryRef
	found := false
	for _, resource := range resources {
		if uuidToString(resource.ID) == chi.URLParam(r, "resourceId") && resource.ResourceType == "local_directory" {
			found = json.Unmarshal(resource.ResourceRef, &ref) == nil
			break
		}
	}
	if !found {
		writeError(w, 404, "project directory not found")
		return
	}
	h.readProjectDirectory(w, r, uuidToString(project.WorkspaceID), ref)
}

func (h *Handler) CheckProjectDirectory(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var ref localDirectoryRef
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&ref) != nil {
		writeError(w, 400, "invalid directory mapping")
		return
	}
	raw, _ := json.Marshal(ref)
	if _, err := validateLocalDirectoryRef(raw); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if !h.requireLocalDirectoryManager(w, r, project, raw) {
		return
	}
	h.readProjectDirectory(w, r, uuidToString(project.WorkspaceID), ref)
}

func (h *Handler) readProjectDirectory(w http.ResponseWriter, r *http.Request, workspaceID string, ref localDirectoryRef) {
	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, 500, "cannot load project machines")
		return
	}
	runtimeID := ""
	for _, runtime := range runtimes {
		if runtime.DaemonID.Valid && runtime.DaemonID.String == ref.DaemonID && h.DaemonHub != nil && h.DaemonHub.RuntimeConnectionCount(uuidToString(runtime.ID)) > 0 {
			runtimeID = uuidToString(runtime.ID)
			break
		}
	}
	if runtimeID == "" {
		writeError(w, 503, "project machine is offline")
		return
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 || offset > 1<<53 {
			writeError(w, 400, "invalid offset")
			return
		}
	}
	operation := r.URL.Query().Get("operation")
	if operation == "" {
		operation = "list"
	}
	if operation != "list" && operation != "preview" {
		writeError(w, 400, "invalid operation")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := h.DaemonHub.ReadProjectFiles(ctx, runtimeID, protocol.ProjectFilesRequest{Root: ref.LocalPath, Path: r.URL.Query().Get("path"), Operation: operation, Offset: offset})
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	if result.Error != "" {
		writeError(w, 422, result.Error)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
