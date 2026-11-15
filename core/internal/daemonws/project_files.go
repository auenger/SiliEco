package daemonws

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

type projectFilePending struct {
	client *client
	result chan protocol.ProjectFilesResponse
}

// ReadProjectFiles correlates replies with the exact authenticated connection.
func (h *Hub) ReadProjectFiles(ctx context.Context, runtimeID string, req protocol.ProjectFilesRequest) (protocol.ProjectFilesResponse, error) {
	var zero protocol.ProjectFilesResponse
	h.mu.Lock()
	if len(h.filePending) >= 16 {
		h.mu.Unlock()
		return zero, errors.New("file browser is busy; retry shortly")
	}
	var selected *client
	for c := range h.byRuntime[runtimeID] {
		selected = c
		break
	}
	if selected == nil {
		h.mu.Unlock()
		return zero, errors.New("project machine is offline")
	}
	req.RequestID = uuid.NewString()
	pending := projectFilePending{selected, make(chan protocol.ProjectFilesResponse, 1)}
	if h.filePending == nil {
		h.filePending = make(map[string]projectFilePending)
	}
	h.filePending[req.RequestID] = pending
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.filePending, req.RequestID); h.mu.Unlock() }()
	payload, _ := json.Marshal(req)
	frame, _ := json.Marshal(protocol.Message{Type: protocol.EventProjectFilesRequest, Payload: payload})
	if !selected.trySend(frame) {
		return zero, errors.New("project machine is busy or disconnected")
	}
	select {
	case result := <-pending.result:
		return result, nil
	case <-selected.ctx.Done():
		return zero, errors.New("project machine disconnected")
	case <-ctx.Done():
		return zero, errors.New("project machine did not respond in time")
	}
}
func (c *client) handleProjectFilesResponse(raw json.RawMessage) {
	var response protocol.ProjectFilesResponse
	if json.Unmarshal(raw, &response) != nil {
		return
	}
	c.hub.mu.RLock()
	pending, ok := c.hub.filePending[response.RequestID]
	c.hub.mu.RUnlock()
	if ok && pending.client == c {
		select {
		case pending.result <- response:
		default:
		}
	}
}
