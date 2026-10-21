package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

type CreateSkillImprovementProposalRequest struct {
	RequestID            string `json:"request_id"`
	BaselineRevisionID   string `json:"baseline_revision_id"`
	CandidateRevisionID  string `json:"candidate_revision_id"`
	SourceExecutionID    string `json:"source_execution_id,omitempty"`
	SourceResultRevision int32  `json:"source_result_revision,omitempty"`
	Category             string `json:"category"`
	Reason               string `json:"reason"`
	Hypothesis           string `json:"hypothesis"`
	ApplicableScope      string `json:"applicable_scope"`
}

type SkillImprovementProposalResponse struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspace_id"`
	SkillID             string          `json:"skill_id"`
	RequestID           string          `json:"request_id"`
	BaselineRevisionID  string          `json:"baseline_revision_id"`
	CandidateRevisionID string          `json:"candidate_revision_id"`
	Category            string          `json:"category"`
	Reason              string          `json:"reason"`
	Hypothesis          string          `json:"hypothesis"`
	ApplicableScope     string          `json:"applicable_scope"`
	Evidence            json.RawMessage `json:"evidence"`
	CreatedBy           string          `json:"created_by"`
	CreatedAt           string          `json:"created_at"`
}

func skillImprovementProposalToResponse(row db.SkillImprovementProposal) SkillImprovementProposalResponse {
	return SkillImprovementProposalResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), SkillID: uuidToString(row.SkillID), RequestID: uuidToString(row.RequestID), BaselineRevisionID: uuidToString(row.BaselineRevisionID), CandidateRevisionID: uuidToString(row.CandidateRevisionID), Category: row.Category, Reason: row.Reason, Hypothesis: row.Hypothesis, ApplicableScope: row.ApplicableScope, Evidence: row.Evidence, CreatedBy: uuidToString(row.CreatedBy), CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)}
}

func (h *Handler) CreateSkillImprovementProposal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if r.Header.Get("X-Agent-ID") != "" || r.Header.Get("X-Task-ID") != "" {
		writeError(w, http.StatusForbidden, "improvement proposals require a member identity")
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
	var body CreateSkillImprovementProposalRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid improvement proposal body")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "proposal must contain one object")
		return
	}
	p := service.CreateSkillImprovementProposalParams{WorkspaceID: source.WorkspaceID, SkillID: source.ID, ActorID: actorID, SourceResultRevision: body.SourceResultRevision, Category: body.Category, Reason: body.Reason, Hypothesis: body.Hypothesis, ApplicableScope: body.ApplicableScope}
	for _, field := range []struct {
		value, name string
		target      *pgtype.UUID
	}{{body.RequestID, "request_id", &p.RequestID}, {body.BaselineRevisionID, "baseline_revision_id", &p.BaselineRevisionID}, {body.CandidateRevisionID, "candidate_revision_id", &p.CandidateRevisionID}} {
		value, ok := parseUUIDOrBadRequest(w, field.value, field.name)
		if !ok {
			return
		}
		*field.target = value
	}
	if body.SourceExecutionID != "" {
		value, ok := parseUUIDOrBadRequest(w, body.SourceExecutionID, "source_execution_id")
		if !ok {
			return
		}
		p.SourceExecutionID = value
	}
	svc := service.SkillRevisionService{Queries: h.Queries, TxStarter: h.TxStarter}
	proposal, err := svc.CreateImprovementProposal(r.Context(), p)
	switch {
	case errors.Is(err, service.ErrSkillRevisionAccess):
		writeError(w, http.StatusForbidden, "proposal source or membership access denied")
		return
	case errors.Is(err, service.ErrSkillRevisionInvalid):
		writeError(w, http.StatusBadRequest, "invalid proposal revisions, classification or analysis")
		return
	case errors.Is(err, service.ErrSkillProposalConflict):
		writeError(w, http.StatusConflict, "proposal request already used for different content")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to create improvement proposal")
		return
	}
	writeJSON(w, http.StatusCreated, skillImprovementProposalToResponse(proposal))
}

func (h *Handler) GetSkillImprovementProposal(w http.ResponseWriter, r *http.Request) {
	source, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(source.WorkspaceID)); !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "proposalId"), "proposal id")
	if !ok {
		return
	}
	proposal, err := h.Queries.GetSkillImprovementProposal(r.Context(), db.GetSkillImprovementProposalParams{WorkspaceID: source.WorkspaceID, SkillID: source.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "improvement proposal not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read improvement proposal")
		return
	}
	writeJSON(w, http.StatusOK, skillImprovementProposalToResponse(proposal))
}

type SkillImprovementProposalPageResponse struct {
	Proposals  []SkillImprovementProposalResponse `json:"proposals"`
	NextCursor *string                            `json:"next_cursor"`
}

func (h *Handler) ListSkillImprovementProposals(w http.ResponseWriter, r *http.Request) {
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
		id, ok := parseUUIDOrBadRequest(w, cursor, "cursor")
		if !ok {
			return
		}
		proposal, err := h.Queries.GetSkillImprovementProposal(r.Context(), db.GetSkillImprovementProposalParams{WorkspaceID: source.WorkspaceID, SkillID: source.ID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "cursor does not belong to this skill")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve proposal cursor")
			return
		}
		afterID = proposal.ID
		afterCreatedAt = proposal.CreatedAt
	}
	rows, err := h.Queries.ListSkillImprovementProposals(r.Context(), db.ListSkillImprovementProposalsParams{WorkspaceID: source.WorkspaceID, SkillID: source.ID, AfterID: afterID, AfterCreatedAt: afterCreatedAt, PageLimit: int32(pageSize + 1)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list improvement proposals")
		return
	}
	response := SkillImprovementProposalPageResponse{Proposals: make([]SkillImprovementProposalResponse, 0, pageSize)}
	if len(rows) > pageSize {
		cursor := uuidToString(rows[pageSize-1].ID)
		response.NextCursor = &cursor
		rows = rows[:pageSize]
	}
	for _, row := range rows {
		response.Proposals = append(response.Proposals, skillImprovementProposalToResponse(row))
	}
	writeJSON(w, http.StatusOK, response)
}
