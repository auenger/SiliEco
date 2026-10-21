package handler

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/skillbundle"
)

type CreateSkillRevisionRequest struct {
	Content             string                       `json:"content"`
	Files               []CreateSkillFileRequest     `json:"files"`
	CompatibleContracts []string                     `json:"compatible_contracts"`
	ToolDependencies    []skillbundle.ToolDependency `json:"tool_dependencies"`
}

type SkillRevisionResponse struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	SourceKind   string          `json:"source_kind"`
	SourceID     string          `json:"source_id"`
	RevisionHash string          `json:"revision_hash"`
	Bundle       json.RawMessage `json:"bundle"`
	Manifest     json.RawMessage `json:"manifest"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    string          `json:"created_at"`
}

func skillRevisionToResponse(row db.SkillRevision) SkillRevisionResponse {
	return SkillRevisionResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), SourceKind: row.SourceKind, SourceID: row.SourceID, RevisionHash: row.RevisionHash, Bundle: row.Bundle, Manifest: row.Manifest, CreatedBy: uuidToString(row.CreatedBy), CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)}
}

func (h *Handler) CreateSkillRevision(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if r.Header.Get("X-Agent-ID") != "" || r.Header.Get("X-Task-ID") != "" {
		writeError(w, http.StatusForbidden, "candidate revision creation requires a member identity")
		return
	}
	source, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, source) {
		return
	}
	actorID, ok := parseUUIDOrBadRequest(w, userID, "actor id")
	if !ok {
		return
	}
	var request CreateSkillRevisionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid candidate revision body")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "candidate revision must contain one object")
		return
	}
	files := make([]skillbundle.File, 0, len(request.Files))
	for _, file := range request.Files {
		files = append(files, skillbundle.File{Path: file.Path, Content: file.Content})
	}
	svc := service.SkillRevisionService{Queries: h.Queries, TxStarter: h.TxStarter}
	revision, err := svc.CreateWorkspaceRevision(r.Context(), service.CreateWorkspaceSkillRevisionParams{WorkspaceID: source.WorkspaceID, SkillID: source.ID, ActorID: actorID, Content: request.Content, Files: files, CompatibleContracts: request.CompatibleContracts, ToolDependencies: request.ToolDependencies})
	switch {
	case errors.Is(err, service.ErrSkillRevisionAccess):
		writeError(w, http.StatusForbidden, "candidate revision access denied")
		return
	case errors.Is(err, service.ErrSkillRevisionInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to create candidate revision")
		return
	}
	writeJSON(w, http.StatusCreated, skillRevisionToResponse(revision))
}

func (h *Handler) GetSkillRevision(w http.ResponseWriter, r *http.Request) {
	source, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(source.WorkspaceID)); !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "revisionId"), "revision id")
	if !ok {
		return
	}
	revision, err := h.Queries.GetSkillRevisionInWorkspace(r.Context(), db.GetSkillRevisionInWorkspaceParams{WorkspaceID: source.WorkspaceID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (revision.SourceKind != skillbundle.SourceWorkspace || revision.SourceID != uuidToString(source.ID))) {
		writeError(w, http.StatusNotFound, "skill revision not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read skill revision")
		return
	}
	writeJSON(w, http.StatusOK, skillRevisionToResponse(revision))
}

type SkillRevisionSummaryResponse struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	SourceKind   string          `json:"source_kind"`
	SourceID     string          `json:"source_id"`
	RevisionHash string          `json:"revision_hash"`
	Manifest     json.RawMessage `json:"manifest"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    string          `json:"created_at"`
}

type SkillRevisionPageResponse struct {
	Revisions  []SkillRevisionSummaryResponse `json:"revisions"`
	NextCursor *string                        `json:"next_cursor"`
}

func (h *Handler) ListSkillRevisions(w http.ResponseWriter, r *http.Request) {
	source, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(source.WorkspaceID)); !ok {
		return
	}
	pageSize := 25
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "page_size must be between 1 and 100")
			return
		}
		pageSize = parsed
	}
	var afterID pgtype.UUID
	var afterCreatedAt pgtype.Timestamptz
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		parsed, ok := parseUUIDOrBadRequest(w, cursor, "cursor")
		if !ok {
			return
		}
		revision, err := h.Queries.GetSkillRevisionCursorInWorkspace(r.Context(), db.GetSkillRevisionCursorInWorkspaceParams{WorkspaceID: source.WorkspaceID, ID: parsed})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (revision.SourceKind != skillbundle.SourceWorkspace || revision.SourceID != uuidToString(source.ID))) {
			writeError(w, http.StatusBadRequest, "cursor does not belong to this skill")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve revision cursor")
			return
		}
		afterID = revision.ID
		afterCreatedAt = revision.CreatedAt
	}
	rows, err := h.Queries.ListSkillRevisionSummaries(r.Context(), db.ListSkillRevisionSummariesParams{WorkspaceID: source.WorkspaceID, SourceKind: skillbundle.SourceWorkspace, SourceID: uuidToString(source.ID), AfterID: afterID, AfterCreatedAt: afterCreatedAt, PageLimit: int32(pageSize + 1)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill revisions")
		return
	}
	response := SkillRevisionPageResponse{Revisions: make([]SkillRevisionSummaryResponse, 0, pageSize)}
	if len(rows) > pageSize {
		cursor := uuidToString(rows[pageSize-1].ID)
		response.NextCursor = &cursor
		rows = rows[:pageSize]
	}
	for _, row := range rows {
		response.Revisions = append(response.Revisions, SkillRevisionSummaryResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), SourceKind: row.SourceKind, SourceID: row.SourceID, RevisionHash: row.RevisionHash, Manifest: row.Manifest, CreatedBy: uuidToString(row.CreatedBy), CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)})
	}
	writeJSON(w, http.StatusOK, response)
}
