package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// SubscriberResponse is the JSON shape returned for each task subscriber.
type SubscriberResponse struct {
	TaskID    string `json:"task_id"`
	UserType  string `json:"user_type"`
	UserID    string `json:"user_id"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

func subscriberToResponse(s db.TaskSubscriber) SubscriberResponse {
	return SubscriberResponse{
		TaskID:    uuidToString(s.TaskID),
		UserType:  s.UserType,
		UserID:    uuidToString(s.UserID),
		Reason:    s.Reason,
		CreatedAt: timestampToString(s.CreatedAt),
	}
}

// ListTaskSubscribers returns all subscribers for an task.
func (h *Handler) ListTaskSubscribers(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	task, ok := h.loadTaskForUser(w, r, taskID)
	if !ok {
		return
	}

	subscribers, err := h.Queries.ListTaskSubscribers(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscribers")
		return
	}

	resp := make([]SubscriberResponse, len(subscribers))
	for i, s := range subscribers {
		resp[i] = subscriberToResponse(s)
	}

	writeJSON(w, http.StatusOK, resp)
}

// SubscribeToTask subscribes a user to an task with reason "manual".
// If request body contains user_id, subscribes that user; otherwise subscribes the caller.
func (h *Handler) SubscribeToTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	task, ok := h.loadTaskForUser(w, r, taskID)
	if !ok {
		return
	}

	workspaceID := uuidToString(task.WorkspaceID)
	// Default target: the caller, derived via resolveActor so an agent caller
	// (X-Agent-ID set) subscribes itself rather than the underlying member.
	callerActorType, callerActorID := h.resolveActor(r, requestUserID(r), workspaceID)
	targetUserType := callerActorType
	targetUserID := callerActorID
	var req struct {
		UserID   *string `json:"user_id"`
		UserType *string `json:"user_type"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != nil && *req.UserID != "" {
		targetUserID = *req.UserID
	}
	if req.UserType != nil && *req.UserType != "" {
		targetUserType = *req.UserType
	}

	if !h.isWorkspaceEntity(r.Context(), targetUserType, targetUserID, workspaceID) {
		writeError(w, http.StatusForbidden, "target user is not a member of this workspace")
		return
	}

	err := h.Queries.AddTaskSubscriber(r.Context(), db.AddTaskSubscriberParams{
		TaskID:   task.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
		Reason:   "manual",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}

	h.publish(protocol.EventSubscriberAdded, workspaceID, callerActorType, callerActorID, map[string]any{
		"task_id":   taskID,
		"user_type": targetUserType,
		"user_id":   targetUserID,
		"reason":    "manual",
	})

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": true})
}

// UnsubscribeFromTask removes a user's subscription from an task.
// If request body contains user_id, unsubscribes that user; otherwise unsubscribes the caller.
func (h *Handler) UnsubscribeFromTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	task, ok := h.loadTaskForUser(w, r, taskID)
	if !ok {
		return
	}

	workspaceID := uuidToString(task.WorkspaceID)
	// Default target: the caller, derived via resolveActor so an agent caller
	// (X-Agent-ID set) unsubscribes itself rather than the underlying member.
	callerActorType, callerActorID := h.resolveActor(r, requestUserID(r), workspaceID)
	targetUserType := callerActorType
	targetUserID := callerActorID
	var req struct {
		UserID   *string `json:"user_id"`
		UserType *string `json:"user_type"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID != nil && *req.UserID != "" {
		targetUserID = *req.UserID
	}
	if req.UserType != nil && *req.UserType != "" {
		targetUserType = *req.UserType
	}

	if !h.isWorkspaceEntity(r.Context(), targetUserType, targetUserID, workspaceID) {
		writeError(w, http.StatusForbidden, "target user is not a member of this workspace")
		return
	}

	err := h.Queries.RemoveTaskSubscriber(r.Context(), db.RemoveTaskSubscriberParams{
		TaskID:   task.ID,
		UserType: targetUserType,
		UserID:   parseUUID(targetUserID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsubscribe")
		return
	}

	h.publish(protocol.EventSubscriberRemoved, workspaceID, callerActorType, callerActorID, map[string]any{
		"task_id":   taskID,
		"user_type": targetUserType,
		"user_id":   targetUserID,
	})

	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": false})
}
