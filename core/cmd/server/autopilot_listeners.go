package main

import (
	"context"
	"log/slog"

	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/handler"
	"github.com/silieco-ai/silieco/core/internal/service"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// registerAutopilotListeners hooks into task and run events to keep
// autopilot runs in sync with their linked tasks and agent runs.
func registerAutopilotListeners(bus *events.Bus, svc *service.AutopilotService) {
	ctx := context.Background()

	// When a task with origin_type='autopilot' reaches a terminal status,
	// update the corresponding autopilot run.
	bus.Subscribe(protocol.EventTaskUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		statusChanged, _ := payload["status_changed"].(bool)
		if !statusChanged {
			return
		}
		task, ok := payload["task"].(handler.TaskResponse)
		if !ok {
			return
		}
		// Only handle statuses that finalize an autopilot run.
		if task.Status != "done" && task.Status != "in_review" && task.Status != "cancelled" && task.Status != "blocked" {
			return
		}
		// Load the full task from DB to check origin_type.
		dbTask, err := svc.Queries.GetTask(ctx, parseUUID(task.ID))
		if err != nil {
			slog.Debug("autopilot listener: failed to load task", "task_id", task.ID, "error", err)
			return
		}
		svc.SyncRunFromTask(ctx, dbTask)
	})

	// When a task completes or fails, check if it's an autopilot run_only task.
	bus.Subscribe(protocol.EventRunCompleted, func(e events.Event) {
		syncRunFromRunEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventRunFailed, func(e events.Event) {
		syncRunFromRunEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventRunCancelled, func(e events.Event) {
		syncRunFromRunEvent(ctx, svc, e)
	})
}

func syncRunFromRunEvent(ctx context.Context, svc *service.AutopilotService, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	runID, ok := payload["run_id"].(string)
	if !ok || runID == "" {
		return
	}
	agentRun, err := svc.Queries.GetAgentRun(ctx, parseUUID(runID))
	if err != nil {
		return
	}
	if agentRun.AutopilotRunID.Valid {
		svc.SyncRunFromRun(ctx, agentRun)
		return
	}
	if e.Type == protocol.EventRunFailed {
		svc.SyncRunFromLinkedTaskRun(ctx, agentRun)
	}
}
