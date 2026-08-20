package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	skillpkg "github.com/silieco-ai/silieco/core/internal/skill"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

var errSkillNotRefreshable = errors.New("this skill was not imported from a refreshable source")

type skillOriginRef struct {
	Type      string
	SourceURL string
}

func parseSkillOrigin(config []byte) (skillOriginRef, bool) {
	var decoded struct {
		Origin struct {
			Type      string `json:"type"`
			SourceURL string `json:"source_url"`
		} `json:"origin"`
	}
	if json.Unmarshal(config, &decoded) != nil || decoded.Origin.Type == "" {
		return skillOriginRef{}, false
	}
	return skillOriginRef{Type: decoded.Origin.Type, SourceURL: strings.TrimSpace(decoded.Origin.SourceURL)}, true
}

func fetchImportedSkillFromOrigin(ctx context.Context, client *http.Client, origin skillOriginRef) (*importedSkill, error) {
	source, normalized, err := detectImportSource(origin.SourceURL)
	if err != nil || origin.SourceURL == "" {
		return nil, errSkillNotRefreshable
	}
	want := map[string]importSource{"clawhub": sourceClawHub, "skills_sh": sourceSkillsSh, "github": sourceGitHub}
	if expected, ok := want[origin.Type]; !ok || expected != source {
		return nil, errSkillNotRefreshable
	}
	switch source {
	case sourceClawHub:
		return fetchFromClawHub(ctx, client, normalized)
	case sourceSkillsSh:
		return fetchFromSkillsSh(ctx, client, normalized)
	case sourceGitHub:
		return fetchFromGitHub(ctx, client, normalized)
	default:
		return nil, errSkillNotRefreshable
	}
}

func importedSkillRevision(s *importedSkill) string {
	h := sha256.New()
	h.Write([]byte(s.content))
	files := append([]importedFile(nil), s.files...)
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	for _, file := range files {
		h.Write([]byte{0})
		h.Write([]byte(file.path))
		h.Write([]byte{0})
		h.Write([]byte(file.content))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func stampImportedSkillRevision(s *importedSkill) {
	if s.origin == nil {
		s.origin = map[string]any{}
	}
	s.origin["revision"] = importedSkillRevision(s)
}

func mergeSkillConfigOrigin(existing []byte, origin map[string]any) map[string]any {
	config := map[string]any{}
	_ = json.Unmarshal(existing, &config)
	config["origin"] = origin
	return config
}

func importedSkillFiles(s *importedSkill) []CreateSkillFileRequest {
	files := make([]CreateSkillFileRequest, 0, len(s.files))
	for _, file := range s.files {
		if validateFilePath(file.path) && !skillpkg.IsReservedContentPath(file.path) {
			files = append(files, CreateSkillFileRequest{Path: file.path, Content: file.content})
		}
	}
	return files
}

type SkillRefreshPreviewResponse struct {
	SkillID         string                 `json:"skill_id"`
	SourceURL       string                 `json:"source_url"`
	CurrentRevision string                 `json:"current_revision"`
	SourceRevision  string                 `json:"source_revision"`
	Changed         bool                   `json:"changed"`
	CurrentContent  string                 `json:"current_content"`
	SourceContent   string                 `json:"source_content"`
	Files           []SkillRefreshFileDiff `json:"files"`
}

type SkillRefreshFileDiff struct {
	Path           string  `json:"path"`
	Status         string  `json:"status"`
	CurrentContent *string `json:"current_content"`
	SourceContent  *string `json:"source_content"`
}

func stringPointer(value string) *string { return &value }

func skillRefreshFileDiff(current, source *importedSkill) []SkillRefreshFileDiff {
	currentByPath := map[string]string{"SKILL.md": current.content}
	sourceByPath := map[string]string{"SKILL.md": source.content}
	for _, file := range current.files {
		currentByPath[file.path] = file.content
	}
	for _, file := range source.files {
		sourceByPath[file.path] = file.content
	}
	paths := make([]string, 0, len(currentByPath)+len(sourceByPath))
	seen := make(map[string]bool, len(currentByPath)+len(sourceByPath))
	for path := range currentByPath {
		seen[path] = true
		paths = append(paths, path)
	}
	for path := range sourceByPath {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	diffs := make([]SkillRefreshFileDiff, 0, len(paths))
	for _, path := range paths {
		currentContent, hasCurrent := currentByPath[path]
		sourceContent, hasSource := sourceByPath[path]
		diff := SkillRefreshFileDiff{Path: path}
		switch {
		case !hasCurrent:
			diff.Status = "added"
			diff.SourceContent = stringPointer(sourceContent)
		case !hasSource:
			diff.Status = "deleted"
			diff.CurrentContent = stringPointer(currentContent)
		default:
			diff.Status = "unchanged"
			if currentContent != sourceContent {
				diff.Status = "modified"
			}
			diff.CurrentContent = stringPointer(currentContent)
			diff.SourceContent = stringPointer(sourceContent)
		}
		diffs = append(diffs, diff)
	}
	return diffs
}

func (h *Handler) loadRefreshCandidate(w http.ResponseWriter, r *http.Request, skill db.Skill) (*importedSkill, skillOriginRef, bool) {
	origin, ok := parseSkillOrigin(skill.Config)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, errSkillNotRefreshable.Error())
		return nil, skillOriginRef{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), importFetchTimeout)
	defer cancel()
	imported, err := fetchImportedSkillFromOrigin(ctx, &http.Client{Timeout: 30 * time.Second}, origin)
	if err != nil {
		if errors.Is(err, errSkillNotRefreshable) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		} else {
			status, msg := importFetchErrorResponse(ctx, err)
			writeError(w, status, msg)
		}
		return nil, skillOriginRef{}, false
	}
	stampImportedSkillRevision(imported)
	return imported, origin, true
}

func (h *Handler) requireSkillManager(w http.ResponseWriter, r *http.Request, skill db.Skill) (string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", false
	}
	member, ok := h.requireWorkspaceRole(w, r, uuidToString(skill.WorkspaceID), "skill not found", "owner", "admin", "member")
	if !ok {
		return "", false
	}
	if !roleAllowed(member.Role, "owner", "admin") && (!skill.CreatedBy.Valid || uuidToString(skill.CreatedBy) != userID) {
		writeError(w, http.StatusForbidden, "only the skill creator or a workspace admin can refresh this skill")
		return "", false
	}
	return userID, true
}

func (h *Handler) PreviewSkillRefresh(w http.ResponseWriter, r *http.Request) {
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.requireSkillManager(w, r, skill); !ok {
		return
	}
	imported, origin, ok := h.loadRefreshCandidate(w, r, skill)
	if !ok {
		return
	}
	currentFiles, err := h.Queries.ListSkillFiles(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load current skill files")
		return
	}
	currentImported := &importedSkill{content: skill.Content, files: make([]importedFile, 0, len(currentFiles))}
	for _, file := range currentFiles {
		currentImported.files = append(currentImported.files, importedFile{path: file.Path, content: file.Content})
	}
	currentRevision := importedSkillRevision(currentImported)
	sourceRevision := importedSkillRevision(imported)
	writeJSON(w, http.StatusOK, SkillRefreshPreviewResponse{
		SkillID: uuidToString(skill.ID), SourceURL: origin.SourceURL,
		CurrentRevision: currentRevision, SourceRevision: sourceRevision,
		Changed: currentRevision != sourceRevision, CurrentContent: skill.Content, SourceContent: imported.content,
		Files: skillRefreshFileDiff(currentImported, imported),
	})
}

func (h *Handler) refreshSkill(ctx context.Context, skill db.Skill, imported *importedSkill) (SkillWithFilesResponse, error) {
	config, err := json.Marshal(mergeSkillConfigOrigin(skill.Config, imported.origin))
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	updated, err := qtx.UpdateSkill(ctx, db.UpdateSkillParams{
		ID: skill.ID, Description: pgtype.Text{String: sanitizeNullBytes(imported.description), Valid: true},
		Content: pgtype.Text{String: sanitizeNullBytes(imported.content), Valid: true}, Config: config,
	})
	if err != nil {
		return SkillWithFilesResponse{}, err
	}
	if err := qtx.DeleteSkillFilesBySkill(ctx, skill.ID); err != nil {
		return SkillWithFilesResponse{}, err
	}
	files := make([]SkillFileResponse, 0, len(imported.files))
	for _, file := range importedSkillFiles(imported) {
		row, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{SkillID: skill.ID, Path: sanitizeNullBytes(file.Path), Content: sanitizeNullBytes(file.Content)})
		if err != nil {
			return SkillWithFilesResponse{}, err
		}
		files = append(files, skillFileToResponse(row))
	}
	if err := tx.Commit(ctx); err != nil {
		return SkillWithFilesResponse{}, err
	}
	return SkillWithFilesResponse{SkillResponse: skillToResponse(updated), Files: files}, nil
}

func (h *Handler) RefreshSkill(w http.ResponseWriter, r *http.Request) {
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := h.requireSkillManager(w, r, skill)
	if !ok {
		return
	}
	imported, _, ok := h.loadRefreshCandidate(w, r, skill)
	if !ok {
		return
	}
	result, err := h.refreshSkill(r.Context(), skill, imported)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "skill not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to refresh skill")
		}
		return
	}
	workspaceID := uuidToString(skill.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventSkillUpdated, workspaceID, actorType, actorID, map[string]any{"skill": result})
	writeJSON(w, http.StatusOK, result)
}

type bulkRefreshSkillsRequest struct {
	SkillIDs []string `json:"skill_ids"`
}

func (h *Handler) BulkRefreshSkills(w http.ResponseWriter, r *http.Request) {
	var req bulkRefreshSkillsRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.SkillIDs) == 0 || len(req.SkillIDs) > 100 {
		writeError(w, http.StatusBadRequest, "skill_ids is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	results := make([]map[string]any, 0, len(req.SkillIDs))
	for _, id := range req.SkillIDs {
		skillID, parseOK := parseUUIDOrBadRequest(w, id, "skill id")
		if !parseOK {
			return
		}
		skill, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{ID: skillID, WorkspaceID: parseUUID(workspaceID)})
		if err != nil {
			results = append(results, map[string]any{"skill_id": id, "status": "failed", "error": "skill not found"})
			continue
		}
		if _, ok := h.requireSkillManager(w, r, skill); !ok {
			return
		}
		origin, ok := parseSkillOrigin(skill.Config)
		if !ok {
			results = append(results, map[string]any{"skill_id": id, "status": "failed", "error": errSkillNotRefreshable.Error()})
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), importFetchTimeout)
		imported, fetchErr := fetchImportedSkillFromOrigin(ctx, &http.Client{Timeout: 30 * time.Second}, origin)
		cancel()
		if fetchErr != nil {
			results = append(results, map[string]any{"skill_id": id, "status": "failed", "error": fetchErr.Error()})
			continue
		}
		stampImportedSkillRevision(imported)
		updated, refreshErr := h.refreshSkill(r.Context(), skill, imported)
		if refreshErr != nil {
			results = append(results, map[string]any{"skill_id": id, "status": "failed", "error": refreshErr.Error()})
		} else {
			results = append(results, map[string]any{"skill_id": id, "status": "updated", "skill": updated})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
