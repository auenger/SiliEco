package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// ProjectResourceResponse is the JSON shape returned by the project resource API.
type ProjectResourceResponse struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"project_id"`
	WorkspaceID  string          `json:"workspace_id"`
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     int32           `json:"position"`
	CreatedAt    string          `json:"created_at"`
	CreatedBy    *string         `json:"created_by"`
}

func projectResourceToResponse(r db.ProjectResource) ProjectResourceResponse {
	ref := json.RawMessage(r.ResourceRef)
	if len(ref) == 0 {
		ref = json.RawMessage("{}")
	}
	return ProjectResourceResponse{
		ID:           uuidToString(r.ID),
		ProjectID:    uuidToString(r.ProjectID),
		WorkspaceID:  uuidToString(r.WorkspaceID),
		ResourceType: r.ResourceType,
		ResourceRef:  ref,
		Label:        textToPtr(r.Label),
		Position:     r.Position,
		CreatedAt:    timestampToString(r.CreatedAt),
		CreatedBy:    uuidToPtr(r.CreatedBy),
	}
}

// CreateProjectResourceRequest is the body for POST /api/projects/{id}/resources.
type CreateProjectResourceRequest struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     *int32          `json:"position"`
}

// UpdateProjectResourceRequest is the body for PUT /api/projects/{id}/resources/{resourceId}.
// resource_type cannot change after creation — pick a new type by deleting and
// re-adding. Every field is optional; omitted fields keep their current value.
type UpdateProjectResourceRequest struct {
	ResourceRef json.RawMessage `json:"resource_ref"`
	Label       *string         `json:"label"`
	Position    *int32          `json:"position"`
}

// validateAndNormalizeResourceRef checks the payload for a known resource_type.
// New types are added here without schema migration; unknown types are rejected
// at the API boundary so a typo can't slip through and produce a resource the
// daemon/UI doesn't understand.
func validateAndNormalizeResourceRef(resourceType string, ref json.RawMessage) (json.RawMessage, error) {
	if len(ref) == 0 {
		return nil, errors.New("resource_ref is required")
	}
	switch resourceType {
	case "github_repo":
		return validateGithubRepoRef(ref)
	case "local_directory":
		return validateLocalDirectoryRef(ref)
	default:
		return nil, fmt.Errorf("unknown resource_type %q", resourceType)
	}
}

type githubRepoRef struct {
	URL               string `json:"url"`
	DefaultBranchHint string `json:"default_branch_hint,omitempty"`
	Ref               string `json:"ref,omitempty"`
	Primary           bool   `json:"primary,omitempty"`
}

func validateGithubRepoRef(ref json.RawMessage) (json.RawMessage, error) {
	var payload githubRepoRef
	if err := json.Unmarshal(ref, &payload); err != nil {
		return nil, fmt.Errorf("invalid github_repo payload: %w", err)
	}
	payload.URL = strings.TrimSpace(payload.URL)
	if payload.URL == "" {
		return nil, errors.New("github_repo: url is required")
	}
	if !isValidGitRepoURL(payload.URL) {
		return nil, errors.New("github_repo: url must be a valid http(s) or ssh git URL")
	}
	payload.DefaultBranchHint = strings.TrimSpace(payload.DefaultBranchHint)
	payload.Ref = strings.TrimSpace(payload.Ref)
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// localDirectoryRef is the JSONB shape stored for resource_type=local_directory.
// It pins a project to an existing directory on a specific user machine, so
// agent tasks run in-place rather than in an isolated git worktree. The
// daemon_id scopes the path to one daemon registration — the same string path
// on a different machine is a different resource. The optional label is a
// human-readable hint used by the UI; the row-level project_resource.label
// column remains the generic column for any resource type.
type localDirectoryRef struct {
	LocalPath            string `json:"local_path"`
	DaemonID             string `json:"daemon_id"`
	RepositoryResourceID string `json:"repository_resource_id,omitempty"`
	Label                string `json:"label,omitempty"`
	ExecutionMode        string `json:"execution_mode,omitempty"`
}

func validateLocalDirectoryRef(ref json.RawMessage) (json.RawMessage, error) {
	var payload localDirectoryRef
	if err := json.Unmarshal(ref, &payload); err != nil {
		return nil, fmt.Errorf("invalid local_directory payload: %w", err)
	}
	payload.LocalPath = strings.TrimSpace(payload.LocalPath)
	if payload.LocalPath == "" {
		return nil, errors.New("local_directory: local_path is required")
	}
	if !isAbsoluteLocalPath(payload.LocalPath) {
		return nil, errors.New("local_directory: local_path must be an absolute path")
	}
	payload.DaemonID = strings.TrimSpace(payload.DaemonID)
	if payload.DaemonID == "" {
		return nil, errors.New("local_directory: daemon_id is required")
	}
	payload.RepositoryResourceID = strings.TrimSpace(payload.RepositoryResourceID)
	if payload.RepositoryResourceID != "" {
		if _, err := parseUUIDLoose(payload.RepositoryResourceID); err != nil {
			return nil, errors.New("local_directory: repository_resource_id must be a UUID")
		}
	}
	payload.Label = strings.TrimSpace(payload.Label)
	payload.ExecutionMode = strings.TrimSpace(payload.ExecutionMode)
	if payload.ExecutionMode == "" {
		payload.ExecutionMode = "in_place"
	}
	if payload.ExecutionMode != "in_place" && payload.ExecutionMode != "worktree" {
		return nil, errors.New("local_directory: execution_mode must be in_place or worktree")
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// isAbsoluteLocalPath checks the path looks absolute on either POSIX or
// Windows daemons. The server can't know which OS the daemon runs on, so we
// accept the union: a leading "/" (POSIX), a UNC prefix "\\", or a drive
// letter like "C:\" or "C:/". The daemon still verifies existence at run
// time — this is a typo guard, not a filesystem check.
func isAbsoluteLocalPath(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '/' {
		return true
	}
	if strings.HasPrefix(s, `\\`) {
		return true
	}
	if len(s) >= 3 && isDriveLetter(s[0]) && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	return false
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isValidGitRepoURL accepts the three forms a user can paste from GitHub's
// "Code" menu: https://, ssh:// (with explicit scheme), and the scp-like
// shorthand `git@host:owner/repo.git`. The check is intentionally lax — we are
// guarding against pasted garbage like "not-a-url", not enforcing a strict
// grammar — because the actual fetch happens client-side via `git clone` and
// the user gets a clearer error from git than from us.
func isValidGitRepoURL(s string) bool {
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		switch u.Scheme {
		case "http", "https", "ssh", "git":
			return true
		}
	}
	// scp-like ssh shorthand: [user@]host:path with a non-empty host and path,
	// and no spaces. Reject anything that looks like a URL with a scheme
	// (those should go through url.Parse above).
	if strings.Contains(s, " ") || strings.Contains(s, "://") {
		return false
	}
	colon := strings.Index(s, ":")
	if colon <= 0 || colon == len(s)-1 {
		return false
	}
	// In scp-like ssh shorthand `[user@]host:path`, `@` is only meaningful
	// as a user separator before the first ':'. If '@' appears at or after
	// the colon it is not the user separator — reject as malformed rather
	// than guess (and avoid a slice-bounds panic from blindly slicing).
	at := strings.Index(s, "@")
	if at >= colon {
		return false
	}
	hostStart := 0
	if at >= 0 {
		hostStart = at + 1
	}
	host := s[hostStart:colon]
	path := s[colon+1:]
	if host == "" || path == "" {
		return false
	}
	return true
}

// loadProjectForResource resolves the project, enforces workspace ownership,
// and returns its DB row. Used by all project_resource handlers.
func (h *Handler) loadProjectForResource(w http.ResponseWriter, r *http.Request, projectIDParam string) (db.Project, bool) {
	projectUUID, ok := parseUUIDOrBadRequest(w, projectIDParam, "project id")
	if !ok {
		return db.Project{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

// requireProjectResourceManager protects execution-affecting project resources.
// Workspace owners/admins and the member project lead may manage them; task
// tokens are always read-only here so an executing agent cannot redirect future
// runs to a different repository or local directory.
func (h *Handler) requireProjectResourceManager(w http.ResponseWriter, r *http.Request, project db.Project) bool {
	if r.Header.Get("X-Actor-Source") == "task_token" {
		writeError(w, http.StatusForbidden, "agent tasks cannot manage project resources")
		return false
	}
	member, ok := h.workspaceMember(w, r, uuidToString(project.WorkspaceID))
	if !ok {
		return false
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	if project.LeadType.Valid && project.LeadType.String == "member" &&
		project.LeadID.Valid && uuidToString(project.LeadID) == uuidToString(member.UserID) {
		return true
	}
	writeError(w, http.StatusForbidden, "only workspace admins or the project lead can manage project resources")
	return false
}

func (h *Handler) requireLocalDirectoryManager(w http.ResponseWriter, r *http.Request, project db.Project, ref json.RawMessage) bool {
	if r.Header.Get("X-Actor-Source") == "task_token" {
		writeError(w, http.StatusForbidden, "agent tasks cannot manage project resources")
		return false
	}
	member, ok := h.workspaceMember(w, r, uuidToString(project.WorkspaceID))
	if !ok {
		return false
	}
	if member.Role == "owner" || member.Role == "admin" ||
		(project.LeadType.Valid && project.LeadType.String == "member" &&
			project.LeadID.Valid && uuidToString(project.LeadID) == uuidToString(member.UserID)) {
		return true
	}
	var localRef localDirectoryRef
	if err := json.Unmarshal(ref, &localRef); err != nil {
		writeError(w, http.StatusBadRequest, "invalid local_directory payload")
		return false
	}
	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), project.WorkspaceID)
	if err == nil {
		for _, runtime := range runtimes {
			if runtime.DaemonID.Valid && runtime.DaemonID.String == localRef.DaemonID &&
				runtime.OwnerID.Valid && uuidToString(runtime.OwnerID) == uuidToString(member.UserID) {
				return true
			}
		}
	}
	writeError(w, http.StatusForbidden, "members may only manage local repository mappings on their own runtime machine")
	return false
}

// ListProjectResources returns the resources attached to a project.
func (h *Handler) ListProjectResources(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resources, err := h.Queries.ListProjectResources(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project resources")
		return
	}
	resp := make([]ProjectResourceResponse, len(resources))
	for i, res := range resources {
		resp[i] = projectResourceToResponse(res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": resp, "total": len(resp)})
}

// CreateProjectResource attaches a new resource to a project.
func (h *Handler) CreateProjectResource(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateProjectResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	if req.ResourceType == "" {
		writeError(w, http.StatusBadRequest, "resource_type is required")
		return
	}
	normalizedRef, err := validateAndNormalizeResourceRef(req.ResourceType, req.ResourceRef)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateLocalDirectoryRepository(r.Context(), project, req.ResourceType, normalizedRef); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ResourceType == "local_directory" {
		if !h.requireLocalDirectoryManager(w, r, project, normalizedRef) {
			return
		}
	} else if !h.requireProjectResourceManager(w, r, project) {
		return
	}

	if conflict, err := h.findLocalDirectoryConflict(r.Context(), project.ID, req.ResourceType, normalizedRef, pgtype.UUID{}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing resources")
		return
	} else if conflict {
		writeError(w, http.StatusConflict, "this daemon already has a local directory mapped to this repository")
		return
	}
	if conflict, err := h.findGithubRepositoryConflict(r.Context(), project.ID, req.ResourceType, normalizedRef, pgtype.UUID{}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing repositories")
		return
	} else if conflict {
		writeError(w, http.StatusConflict, "this repository is already attached to the project")
		return
	}

	var label pgtype.Text
	if req.Label != nil && strings.TrimSpace(*req.Label) != "" {
		label = pgtype.Text{String: strings.TrimSpace(*req.Label), Valid: true}
	}
	existingResources, err := h.Queries.ListProjectResources(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list existing resources")
		return
	}
	newGithubPrimary := false
	if req.ResourceType == "github_repo" {
		var repoRef githubRepoRef
		if err := json.Unmarshal(normalizedRef, &repoRef); err != nil {
			writeError(w, http.StatusBadRequest, "invalid github_repo payload")
			return
		}
		hasRepository := false
		for _, row := range existingResources {
			if row.ResourceType == "github_repo" {
				hasRepository = true
				break
			}
		}
		if !hasRepository {
			repoRef.Primary = true
			normalizedRef, err = json.Marshal(repoRef)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to normalize repository")
				return
			}
		}
		newGithubPrimary = repoRef.Primary
	}

	var position int32
	if req.Position != nil {
		position = *req.Position
	} else {
		position = int32(len(existingResources))
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin resource create")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if newGithubPrimary {
		if err := demoteGithubResources(r.Context(), qtx, existingResources, pgtype.UUID{}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update primary repository")
			return
		}
	}
	creator, _ := h.parseUserUUIDOrZero(userID)
	resource, err := qtx.CreateProjectResource(r.Context(), db.CreateProjectResourceParams{
		ProjectID:    project.ID,
		WorkspaceID:  project.WorkspaceID,
		ResourceType: req.ResourceType,
		ResourceRef:  normalizedRef,
		Label:        label,
		Position:     position,
		CreatedBy:    creator,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "this resource is already attached to the project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create project resource")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project resource")
		return
	}

	resp := projectResourceToResponse(resource)
	h.publish(
		protocol.EventProjectResourceCreated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"resource": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateProjectResource edits an existing resource's ref/label/position.
// resource_type is immutable — re-pointing a resource at a different type is
// almost always a different conceptual entity, so the caller should delete and
// re-add instead. Omitted fields keep their current value, including the
// `label` JSON null vs. missing distinction (missing = keep, explicit "" =
// clear).
func (h *Handler) UpdateProjectResource(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resourceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "resourceId"), "resource id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	existing, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{
		ID: resourceUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}
	if uuidToString(existing.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}
	if existing.ResourceType == "local_directory" {
		if !h.requireLocalDirectoryManager(w, r, project, existing.ResourceRef) {
			return
		}
	} else if !h.requireProjectResourceManager(w, r, project) {
		return
	}

	// Decode into a raw map first so we can tell "field omitted" from
	// "field present with zero value" — the label clear case in particular
	// relies on this distinction.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	nextRef := json.RawMessage(existing.ResourceRef)
	if rawRef, ok := raw["resource_ref"]; ok {
		normalized, err := validateAndNormalizeResourceRef(existing.ResourceType, rawRef)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		nextRef = normalized
	}
	if err := h.validateLocalDirectoryRepository(r.Context(), project, existing.ResourceType, nextRef); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if existing.ResourceType == "local_directory" && !h.requireLocalDirectoryManager(w, r, project, nextRef) {
		return
	}

	if conflict, err := h.findLocalDirectoryConflict(r.Context(), project.ID, existing.ResourceType, nextRef, existing.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing resources")
		return
	} else if conflict {
		writeError(w, http.StatusConflict, "another local directory on this daemon is already mapped to this repository")
		return
	}
	if conflict, err := h.findGithubRepositoryConflict(r.Context(), project.ID, existing.ResourceType, nextRef, existing.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing repositories")
		return
	} else if conflict {
		writeError(w, http.StatusConflict, "this repository is already attached to the project")
		return
	}

	nextLabel := existing.Label
	if rawLabel, ok := raw["label"]; ok {
		var labelStr *string
		if err := json.Unmarshal(rawLabel, &labelStr); err != nil {
			writeError(w, http.StatusBadRequest, "label must be a string or null")
			return
		}
		if labelStr == nil || strings.TrimSpace(*labelStr) == "" {
			nextLabel = pgtype.Text{}
		} else {
			nextLabel = pgtype.Text{String: strings.TrimSpace(*labelStr), Valid: true}
		}
	}

	nextPosition := existing.Position
	if rawPos, ok := raw["position"]; ok {
		var pos *int32
		if err := json.Unmarshal(rawPos, &pos); err != nil {
			writeError(w, http.StatusBadRequest, "position must be an integer")
			return
		}
		if pos != nil {
			nextPosition = *pos
		}
	}

	makePrimary := false
	if existing.ResourceType == "github_repo" {
		var ref githubRepoRef
		if err := json.Unmarshal(nextRef, &ref); err == nil {
			makePrimary = ref.Primary
		}
	}
	queries := h.Queries
	var tx pgx.Tx
	if makePrimary {
		tx, err = h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to begin resource update")
			return
		}
		defer tx.Rollback(r.Context())
		queries = h.Queries.WithTx(tx)
		rows, listErr := queries.ListProjectResources(r.Context(), project.ID)
		if listErr != nil || demoteGithubResources(r.Context(), queries, rows, existing.ID) != nil {
			writeError(w, http.StatusInternalServerError, "failed to update primary repository")
			return
		}
	}
	updated, err := queries.UpdateProjectResource(r.Context(), db.UpdateProjectResourceParams{
		ID:          existing.ID,
		ResourceRef: nextRef,
		Label:       nextLabel,
		Position:    nextPosition,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "this resource is already attached to the project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update project resource")
		return
	}
	if tx != nil {
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit project resource")
			return
		}
	}

	resp := projectResourceToResponse(updated)
	h.publish(
		protocol.EventProjectResourceUpdated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"resource": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusOK, resp)
}

// findLocalDirectoryConflict enforces one local checkout per
// (project, repository resource, daemon). A machine may map multiple project
// repositories, but it cannot map the same repository twice.
//
// The DB-level UNIQUE(project_id, resource_type, resource_ref) constraint
// alone is not enough here: it only fires on full ref-JSON equality, so a
// different local_path or even a typoed label on the same daemon would slip
// through. We do the daemon-scoped check here in application code instead.
//
// `excludeID` lets the update path ignore the row being edited.
func (h *Handler) findLocalDirectoryConflict(ctx context.Context, projectID pgtype.UUID, resourceType string, normalizedRef json.RawMessage, excludeID pgtype.UUID) (bool, error) {
	if resourceType != "local_directory" {
		return false, nil
	}
	var incoming localDirectoryRef
	if err := json.Unmarshal(normalizedRef, &incoming); err != nil {
		return false, err
	}
	rows, err := h.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.ResourceType != "local_directory" {
			continue
		}
		if excludeID.Valid && uuidToString(row.ID) == uuidToString(excludeID) {
			continue
		}
		var existing localDirectoryRef
		if err := json.Unmarshal(row.ResourceRef, &existing); err != nil {
			continue
		}
		sameRepository := existing.RepositoryResourceID == incoming.RepositoryResourceID
		if existing.DaemonID == incoming.DaemonID && sameRepository {
			return true, nil
		}
	}
	return false, nil
}

func (h *Handler) findGithubRepositoryConflict(ctx context.Context, projectID pgtype.UUID, resourceType string, normalizedRef json.RawMessage, excludeID pgtype.UUID) (bool, error) {
	if resourceType != "github_repo" {
		return false, nil
	}
	var incoming githubRepoRef
	if err := json.Unmarshal(normalizedRef, &incoming); err != nil {
		return false, err
	}
	rows, err := h.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.ResourceType != "github_repo" ||
			(excludeID.Valid && uuidToString(row.ID) == uuidToString(excludeID)) {
			continue
		}
		var existing githubRepoRef
		if err := json.Unmarshal(row.ResourceRef, &existing); err == nil && existing.URL == incoming.URL {
			return true, nil
		}
	}
	return false, nil
}

func (h *Handler) validateLocalDirectoryRepository(ctx context.Context, project db.Project, resourceType string, normalizedRef json.RawMessage) error {
	if resourceType != "local_directory" {
		return nil
	}
	var localRef localDirectoryRef
	if err := json.Unmarshal(normalizedRef, &localRef); err != nil {
		return err
	}
	if localRef.RepositoryResourceID == "" {
		return nil
	}
	repositoryID, err := parseUUIDLoose(localRef.RepositoryResourceID)
	if err != nil {
		return errors.New("local_directory: repository_resource_id must be a UUID")
	}
	repository, err := h.Queries.GetProjectResourceInWorkspace(ctx, db.GetProjectResourceInWorkspaceParams{
		ID: repositoryID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil || uuidToString(repository.ProjectID) != uuidToString(project.ID) || repository.ResourceType != "github_repo" {
		return errors.New("local_directory: repository_resource_id must reference a Git repository in this project")
	}
	return nil
}

func demoteGithubResources(ctx context.Context, queries *db.Queries, resources []db.ProjectResource, excludeID pgtype.UUID) error {
	for _, resource := range resources {
		if resource.ResourceType != "github_repo" ||
			(excludeID.Valid && uuidToString(resource.ID) == uuidToString(excludeID)) {
			continue
		}
		var ref githubRepoRef
		if err := json.Unmarshal(resource.ResourceRef, &ref); err != nil || !ref.Primary {
			continue
		}
		ref.Primary = false
		normalized, err := json.Marshal(ref)
		if err != nil {
			return err
		}
		if _, err := queries.UpdateProjectResource(ctx, db.UpdateProjectResourceParams{
			ID: resource.ID, ResourceRef: normalized, Label: resource.Label, Position: resource.Position,
		}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteProjectResource removes a resource from a project.
func (h *Handler) DeleteProjectResource(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resourceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "resourceId"), "resource id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	resource, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{
		ID: resourceUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}
	if uuidToString(resource.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project resource not found")
		return
	}
	if resource.ResourceType == "local_directory" {
		if !h.requireLocalDirectoryManager(w, r, project, resource.ResourceRef) {
			return
		}
	} else if !h.requireProjectResourceManager(w, r, project) {
		return
	}
	resources, err := h.Queries.ListProjectResources(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dependent resources")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin resource delete")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	wasPrimary := false
	if resource.ResourceType == "github_repo" {
		var repositoryRef githubRepoRef
		if err := json.Unmarshal(resource.ResourceRef, &repositoryRef); err == nil {
			wasPrimary = repositoryRef.Primary
		}
		for _, candidate := range resources {
			if candidate.ResourceType != "local_directory" {
				continue
			}
			var localRef localDirectoryRef
			if err := json.Unmarshal(candidate.ResourceRef, &localRef); err == nil &&
				localRef.RepositoryResourceID == uuidToString(resource.ID) {
				if err := qtx.DeleteProjectResource(r.Context(), candidate.ID); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to delete repository mappings")
					return
				}
			}
		}
	}
	if err := qtx.DeleteProjectResource(r.Context(), resource.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project resource")
		return
	}
	if wasPrimary {
		for _, candidate := range resources {
			if candidate.ResourceType != "github_repo" || uuidToString(candidate.ID) == uuidToString(resource.ID) {
				continue
			}
			var ref githubRepoRef
			if err := json.Unmarshal(candidate.ResourceRef, &ref); err != nil {
				continue
			}
			ref.Primary = true
			normalized, marshalErr := json.Marshal(ref)
			if marshalErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to promote repository")
				return
			}
			if _, err := qtx.UpdateProjectResource(r.Context(), db.UpdateProjectResourceParams{
				ID: candidate.ID, ResourceRef: normalized, Label: candidate.Label, Position: candidate.Position,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to promote repository")
				return
			}
			break
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit resource delete")
		return
	}
	h.publish(
		protocol.EventProjectResourceDeleted,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{
			"project_id":  uuidToString(project.ID),
			"resource_id": uuidToString(resource.ID),
		},
	)
	w.WriteHeader(http.StatusNoContent)
}

// parseUserUUIDOrZero converts a user ID string to a pgtype.UUID, returning a
// zero value on any error so the caller can store NULL for created_by when the
// authenticated principal is not a workspace member (e.g. internal-server use).
func (h *Handler) parseUserUUIDOrZero(userID string) (pgtype.UUID, bool) {
	if userID == "" {
		return pgtype.UUID{}, false
	}
	u, err := parseUUIDLoose(userID)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

// parseUUIDLoose mirrors util.ParseUUID but lives here to avoid pulling util
// into a tiny one-off helper. Keep the body minimal.
func parseUUIDLoose(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// listProjectResourcesForProject is a small helper used by the daemon claim
// handler to attach project resources to outgoing tasks.
func (h *Handler) listProjectResourcesForProject(ctx context.Context, projectID pgtype.UUID) []db.ProjectResource {
	if !projectID.Valid {
		return nil
	}
	rows, err := h.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return nil
	}
	return rows
}
