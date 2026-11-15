package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

const defaultFileResourcePageSize = 50
const maxFileResourcePageSize = 100

type FileResourceResponse struct {
	AttachmentResponse
	SourceTaskTitle    *string `json:"source_task_title"`
	SourceTaskNumber   *int32  `json:"source_task_number"`
	SourceProjectID    *string `json:"source_project_id"`
	SourceProjectTitle *string `json:"source_project_title"`
	ReferenceCount     int64   `json:"reference_count"`
}

type AttachmentReferenceResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	AttachmentID       string  `json:"attachment_id"`
	TargetType         string  `json:"target_type"`
	TargetID           string  `json:"target_id"`
	CreatedBy          string  `json:"created_by"`
	CreatedAt          string  `json:"created_at"`
	TargetTitle        *string `json:"target_title,omitempty"`
	TargetTaskNumber   *int32  `json:"target_task_number,omitempty"`
	TargetProjectID    *string `json:"target_project_id,omitempty"`
	TargetProjectTitle *string `json:"target_project_title,omitempty"`
}

type createAttachmentReferenceRequest struct {
	AttachmentID string `json:"attachment_id"`
}

func parseFileResourcePage(r *http.Request) (int32, int32) {
	limit := defaultFileResourcePageSize
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = min(parsed, maxFileResourcePageSize)
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return int32(limit), int32(offset)
}

func attachmentReferenceToResponse(ref db.AttachmentReference) AttachmentReferenceResponse {
	return AttachmentReferenceResponse{
		ID:           uuidToString(ref.ID),
		WorkspaceID:  uuidToString(ref.WorkspaceID),
		AttachmentID: uuidToString(ref.AttachmentID),
		TargetType:   ref.TargetType,
		TargetID:     uuidToString(ref.TargetID),
		CreatedBy:    uuidToString(ref.CreatedBy),
		CreatedAt:    timestampToString(ref.CreatedAt),
	}
}

func nonEmptyStringToPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func workspaceAttachmentReferenceToResponse(ref db.ListWorkspaceAttachmentReferencesRow) AttachmentReferenceResponse {
	return AttachmentReferenceResponse{
		ID:                 uuidToString(ref.ID),
		WorkspaceID:        uuidToString(ref.WorkspaceID),
		AttachmentID:       uuidToString(ref.AttachmentID),
		TargetType:         ref.TargetType,
		TargetID:           uuidToString(ref.TargetID),
		CreatedBy:          uuidToString(ref.CreatedBy),
		CreatedAt:          timestampToString(ref.CreatedAt),
		TargetTitle:        nonEmptyStringToPtr(ref.TargetTitle),
		TargetTaskNumber:   int32ToPtr(ref.TargetTaskNumber),
		TargetProjectID:    uuidToPtr(ref.TargetProjectID),
		TargetProjectTitle: nonEmptyStringToPtr(ref.TargetProjectTitle),
	}
}

func int32ToPtr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func (h *Handler) workspaceFileResourceToResponse(row db.ListWorkspaceFileResourcesRow, mode attachmentURLMode) FileResourceResponse {
	return FileResourceResponse{
		AttachmentResponse: h.attachmentToResponse(db.Attachment{
			ID: row.ID, WorkspaceID: row.WorkspaceID, TaskID: row.TaskID,
			CommentID: row.CommentID, UploaderType: row.UploaderType,
			UploaderID: row.UploaderID, Filename: row.Filename, Url: row.Url,
			ContentType: row.ContentType, SizeBytes: row.SizeBytes, CreatedAt: row.CreatedAt,
			ChatSessionID: row.ChatSessionID, ChatMessageID: row.ChatMessageID, RunID: row.RunID,
		}, mode),
		SourceTaskTitle: textToPtr(row.SourceTaskTitle), SourceTaskNumber: int32ToPtr(row.SourceTaskNumber),
		SourceProjectID: uuidToPtr(row.SourceProjectID), SourceProjectTitle: textToPtr(row.SourceProjectTitle),
		ReferenceCount: row.ReferenceCount,
	}
}

func (h *Handler) projectFileResourceToResponse(row db.ListProjectFileResourcesRow, mode attachmentURLMode) FileResourceResponse {
	return FileResourceResponse{
		AttachmentResponse: h.attachmentToResponse(db.Attachment{
			ID: row.ID, WorkspaceID: row.WorkspaceID, TaskID: row.TaskID,
			CommentID: row.CommentID, UploaderType: row.UploaderType,
			UploaderID: row.UploaderID, Filename: row.Filename, Url: row.Url,
			ContentType: row.ContentType, SizeBytes: row.SizeBytes, CreatedAt: row.CreatedAt,
			ChatSessionID: row.ChatSessionID, ChatMessageID: row.ChatMessageID, RunID: row.RunID,
		}, mode),
		SourceTaskTitle: textToPtr(row.SourceTaskTitle), SourceTaskNumber: int32ToPtr(row.SourceTaskNumber),
		SourceProjectID: uuidToPtr(row.SourceProjectID), SourceProjectTitle: textToPtr(row.SourceProjectTitle),
		ReferenceCount: row.ReferenceCount,
	}
}

// ListWorkspaceFileResources returns durable task/comment files. Chat uploads
// are intentionally excluded because they remain temporary conversation data.
func (h *Handler) ListWorkspaceFileResources(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	limit, offset := parseFileResourcePage(r)
	rows, err := h.Queries.ListWorkspaceFileResources(r.Context(), db.ListWorkspaceFileResourcesParams{
		WorkspaceID: workspaceID, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}
	total, err := h.Queries.CountWorkspaceFileResources(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count files")
		return
	}
	mode := attachmentURLModeFromRequest(r)
	files := make([]FileResourceResponse, len(rows))
	for i, row := range rows {
		files[i] = h.workspaceFileResourceToResponse(row, mode)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "total": total})
}

func (h *Handler) ListProjectFileResources(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	limit, offset := parseFileResourcePage(r)
	rows, err := h.Queries.ListProjectFileResources(r.Context(), db.ListProjectFileResourcesParams{
		WorkspaceID: project.WorkspaceID, ProjectID: project.ID, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project files")
		return
	}
	total, err := h.Queries.CountProjectFileResources(r.Context(), db.CountProjectFileResourcesParams{
		WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count project files")
		return
	}
	mode := attachmentURLModeFromRequest(r)
	files := make([]FileResourceResponse, len(rows))
	for i, row := range rows {
		files[i] = h.projectFileResourceToResponse(row, mode)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "total": total})
}

func (h *Handler) ListAttachmentReferences(w http.ResponseWriter, r *http.Request) {
	attachment, ok := h.loadAttachmentForRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListAttachmentReferences(r.Context(), db.ListAttachmentReferencesParams{
		WorkspaceID: attachment.WorkspaceID, AttachmentID: attachment.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list attachment references")
		return
	}
	refs := make([]AttachmentReferenceResponse, len(rows))
	for i, row := range rows {
		refs[i] = attachmentReferenceToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"references": refs, "total": len(refs)})
}

// ListWorkspaceAttachmentReferences returns all durable reference edges with
// display metadata in one query. The workspace resource page uses this to show
// exactly which projects and tasks consume every file without an N+1 request.
func (h *Handler) ListWorkspaceAttachmentReferences(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceAttachmentReferences(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace attachment references")
		return
	}
	refs := make([]AttachmentReferenceResponse, len(rows))
	for i, row := range rows {
		refs[i] = workspaceAttachmentReferenceToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"references": refs, "total": len(refs)})
}

func (h *Handler) listTargetAttachmentReferences(w http.ResponseWriter, r *http.Request, targetType string, targetID, workspaceID pgtype.UUID) {
	rows, err := h.Queries.ListAttachmentReferencesByTarget(r.Context(), db.ListAttachmentReferencesByTargetParams{
		WorkspaceID: workspaceID, TargetType: targetType, TargetID: targetID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list attachment references")
		return
	}
	refs := make([]AttachmentReferenceResponse, len(rows))
	for i, row := range rows {
		refs[i] = attachmentReferenceToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"references": refs, "total": len(refs)})
}

func (h *Handler) ListTaskAttachmentReferences(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTaskForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.listTargetAttachmentReferences(w, r, "task", task.ID, task.WorkspaceID)
}

func (h *Handler) ListProjectAttachmentReferences(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.listTargetAttachmentReferences(w, r, "project", project.ID, project.WorkspaceID)
}

func (h *Handler) createAttachmentReference(w http.ResponseWriter, r *http.Request, targetType string, targetID, workspaceID pgtype.UUID) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req createAttachmentReferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	attachmentID, ok := parseUUIDOrBadRequest(w, req.AttachmentID, "attachment_id")
	if !ok {
		return
	}
	attachment, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: attachmentID, WorkspaceID: workspaceID})
	if err != nil || attachment.ChatSessionID.Valid || attachment.ChatMessageID.Valid {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	creatorID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	ref, err := h.Queries.CreateAttachmentReference(r.Context(), db.CreateAttachmentReferenceParams{
		WorkspaceID: workspaceID, AttachmentID: attachment.ID, TargetType: targetType,
		TargetID: targetID, CreatedBy: creatorID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create attachment reference")
		return
	}
	writeJSON(w, http.StatusCreated, attachmentReferenceToResponse(ref))
}

func (h *Handler) CreateTaskAttachmentReference(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTaskForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.createAttachmentReference(w, r, "task", task.ID, task.WorkspaceID)
}

func (h *Handler) CreateProjectAttachmentReference(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectResourceManager(w, r, project) {
		return
	}
	h.createAttachmentReference(w, r, "project", project.ID, project.WorkspaceID)
}

func (h *Handler) deleteAttachmentReference(w http.ResponseWriter, r *http.Request, targetType string, targetID, workspaceID pgtype.UUID) {
	attachmentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "attachmentId"), "attachment id")
	if !ok {
		return
	}
	deleted, err := h.Queries.DeleteAttachmentReference(r.Context(), db.DeleteAttachmentReferenceParams{
		WorkspaceID: workspaceID, AttachmentID: attachmentID, TargetType: targetType, TargetID: targetID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove attachment reference")
		return
	}
	if deleted == 0 {
		writeError(w, http.StatusNotFound, "attachment reference not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteTaskAttachmentReference(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTaskForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.deleteAttachmentReference(w, r, "task", task.ID, task.WorkspaceID)
}

func (h *Handler) DeleteProjectAttachmentReference(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok || !h.requireProjectResourceManager(w, r, project) {
		return
	}
	h.deleteAttachmentReference(w, r, "project", project.ID, project.WorkspaceID)
}
