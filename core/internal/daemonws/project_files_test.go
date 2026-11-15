package daemonws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

func TestProjectFilesReplyBoundToConnection(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := &client{hub: hub, ctx: ctx, send: make(chan []byte, 1)}
	stranger := &client{hub: hub, ctx: ctx, send: make(chan []byte, 1)}
	hub.byRuntime["runtime"] = map[*client]bool{owner: true}
	done := make(chan error, 1)
	go func() {
		requestCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		out, err := hub.ReadProjectFiles(requestCtx, "runtime", protocol.ProjectFilesRequest{Root: "/project", Operation: "list"})
		if err == nil && out.Content != "correct" {
			t.Errorf("accepted foreign response: %q", out.Content)
		}
		done <- err
	}()
	var msg protocol.Message
	select {
	case frame := <-owner.send:
		if err := json.Unmarshal(frame, &msg); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request not sent")
	}
	var req protocol.ProjectFilesRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		t.Fatal(err)
	}
	foreign, _ := json.Marshal(protocol.ProjectFilesResponse{RequestID: req.RequestID, Content: "foreign"})
	stranger.handleProjectFilesResponse(foreign)
	correct, _ := json.Marshal(protocol.ProjectFilesResponse{RequestID: req.RequestID, Content: "correct"})
	owner.handleProjectFilesResponse(correct)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(hub.filePending) != 0 {
		t.Fatal("request leaked")
	}
}
func TestProjectFilesOfflineAndCancellation(t *testing.T) {
	hub := NewHub()
	if _, err := hub.ReadProjectFiles(context.Background(), "offline", protocol.ProjectFilesRequest{}); err == nil {
		t.Fatal("offline accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	owner := &client{hub: hub, ctx: ctx, send: make(chan []byte, 1)}
	hub.byRuntime["runtime"] = map[*client]bool{owner: true}
	cancel()
	if _, err := hub.ReadProjectFiles(ctx, "runtime", protocol.ProjectFilesRequest{}); err == nil {
		t.Fatal("canceled read accepted")
	}
	if len(hub.filePending) != 0 {
		t.Fatal("canceled request leaked")
	}
}
