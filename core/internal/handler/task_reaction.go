package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/silieco-ai/silieco/core/internal/logger"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

type TaskReactionResponse struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`
	Emoji     string `json:"emoji"`
	CreatedAt string `json:"created_at"`
}

func taskReactionToResponse(r db.TaskReaction) TaskReactionResponse {
	return TaskReactionResponse{
		ID:        uuidToString(r.ID),
		TaskID:    uuidToString(r.TaskID),
		ActorType: r.ActorType,
		ActorID:   uuidToString(r.ActorID),
		Emoji:     r.Emoji,
		CreatedAt: timestampToString(r.CreatedAt),
	}
}

func (h *Handler) AddTaskReaction(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	task, ok := h.loadTaskForUser(w, r, taskID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji is required")
		return
	}

	workspaceID := uuidToString(task.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	reaction, err := h.Queries.AddTaskReaction(r.Context(), db.AddTaskReactionParams{
		TaskID:      task.ID,
		WorkspaceID: task.WorkspaceID,
		ActorType:   actorType,
		ActorID:     parseUUID(actorID),
		Emoji:       req.Emoji,
	})
	if err != nil {
		slog.Warn("add task reaction failed", append(logger.RequestAttrs(r), "error", err, "task_id", taskID)...)
		writeError(w, http.StatusInternalServerError, "failed to add reaction")
		return
	}

	resp := taskReactionToResponse(reaction)
	h.publish(protocol.EventTaskReactionAdded, workspaceID, actorType, actorID, map[string]any{
		"reaction":     resp,
		"task_id":      uuidToString(task.ID),
		"task_title":   task.Title,
		"task_status":  task.Status,
		"creator_type": task.CreatorType,
		"creator_id":   uuidToString(task.CreatorID),
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) RemoveTaskReaction(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	task, ok := h.loadTaskForUser(w, r, taskID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji is required")
		return
	}

	workspaceID := uuidToString(task.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	if err := h.Queries.RemoveTaskReaction(r.Context(), db.RemoveTaskReactionParams{
		TaskID:    task.ID,
		ActorType: actorType,
		ActorID:   parseUUID(actorID),
		Emoji:     req.Emoji,
	}); err != nil {
		slog.Warn("remove task reaction failed", append(logger.RequestAttrs(r), "error", err, "task_id", taskID)...)
		writeError(w, http.StatusInternalServerError, "failed to remove reaction")
		return
	}

	h.publish(protocol.EventTaskReactionRemoved, workspaceID, actorType, actorID, map[string]any{
		"task_id":    uuidToString(task.ID),
		"emoji":      req.Emoji,
		"actor_type": actorType,
		"actor_id":   actorID,
	})
	w.WriteHeader(http.StatusNoContent)
}
