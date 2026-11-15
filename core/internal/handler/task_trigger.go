package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/service"
	"github.com/silieco-ai/silieco/core/internal/util"
	agentver "github.com/silieco-ai/silieco/core/pkg/agent"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// maxPreviewTriggerTasks caps a single preview request so a pathological
// selection cannot fan out into thousands of readiness probes.
const maxPreviewTriggerTasks = 500

// issueTriggerWriteProbe builds the probe the write paths feed to
// WillEnqueueRun. The private-agent gate is already enforced at the HTTP
// boundary (validateAssigneePair on assign) and inside enqueueSquadLeaderTask
// (canEnqueueSquadLeader), so a write must NOT re-run or sink it — it passes
// allow-all. The self-loop check needs the request's X-Run-ID header.
func (h *Handler) taskTriggerWriteProbe(r *http.Request, actorType, actorID string, task db.Task) service.TaskTriggerProbe {
	return service.TaskTriggerProbe{
		CanAccessAgent: nil, // allow-all; gate lives at the write boundary
		IsSelfLoop: func() bool {
			return h.isAgentRunningOnTask(r, actorType, task)
		},
		SuppressActiveSelfAssignment: func(agentID pgtype.UUID) bool {
			suppress := h.shouldSuppressActiveSelfAssignment(r.Context(), actorType, actorID, task.ID, agentID)
			if suppress {
				slog.Info("suppressing duplicate self-assignment enqueue",
					"task_id", uuidToString(task.ID), "agent_id", uuidToString(agentID))
			}
			return suppress
		},
	}
}

// issueTriggerPreviewProbe mirrors the real write-time gates for the read-only
// preview: the private-agent gate (so preview never leaks a private agent's
// readiness to a member who cannot see it — matching validateAssigneePair /
// canEnqueueSquadLeader) and the same self-loop guard.
func (h *Handler) taskTriggerPreviewProbe(r *http.Request, actorType, actorID, workspaceID string, task db.Task) service.TaskTriggerProbe {
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	return service.TaskTriggerProbe{
		CanAccessAgent: func(agent db.Agent) bool {
			return h.canInvokeAgent(r.Context(), agent, actorType, actorID, originatorUserID, workspaceID)
		},
		IsSelfLoop: func() bool {
			return h.isAgentRunningOnTask(r, actorType, task)
		},
		SuppressActiveSelfAssignment: func(agentID pgtype.UUID) bool {
			return h.shouldSuppressActiveSelfAssignment(r.Context(), actorType, actorID, task.ID, agentID)
		},
	}
}

func (h *Handler) shouldSuppressActiveSelfAssignment(ctx context.Context, actorType, actorID string, taskID, targetAgentID pgtype.UUID) bool {
	if actorType != "agent" || actorID == "" || actorID != uuidToString(targetAgentID) {
		return false
	}
	active, err := h.hasActiveRunForTaskAndAgent(ctx, taskID, targetAgentID)
	return active || err != nil
}

// dispatchTaskRun executes the enqueue side effect for a decision produced by
// WillEnqueueRun, carrying an optional handoff note into the run's opening
// context. The squad path still flows through enqueueSquadLeaderTask so the
// leader access gate and pending dedup stay in one place.
func (h *Handler) dispatchTaskRun(ctx context.Context, task db.Task, trigger service.TaskRunTrigger, actorType, actorID, handoffNote string) {
	switch trigger.AssigneeType {
	case "agent":
		// The member who performed this assign/promote is the accountable human
		// for the run (SILI-4302 §4). An agent actor is not a human, so only a
		// member actor is threaded; otherwise attribution falls back to the chain.
		_, _ = h.AgentRunService.EnqueueRunForTaskWithHandoff(ctx, task, handoffNote, memberActorUserID(actorType, actorID))
	case "squad":
		h.enqueueSquadLeaderRun(ctx, task, pgtype.UUID{}, actorType, actorID, handoffNote)
	}
}

// memberActorUserID returns the acting member's user id as a pgtype.UUID when the
// actor is a member, and an invalid UUID otherwise (an agent actor id is not a
// human and must never become an accountable human). Used to thread the
// assign/promote actor into the attribution resolver (SILI-4302 §4).
func memberActorUserID(actorType, actorID string) pgtype.UUID {
	if actorType != "member" {
		return pgtype.UUID{}
	}
	uid, err := util.ParseUUID(actorID)
	if err != nil {
		return pgtype.UUID{}
	}
	return uid
}

// TaskTriggerPreviewRequest asks "if I apply this assignee and/or status to
// these tasks (or create one), which runs will start". All fields are
// optional; a nil prospective field means "leave unchanged".
type TaskTriggerPreviewRequest struct {
	// TaskIDs are existing tasks to evaluate (single assign, single status,
	// or a batch). Empty with IsCreate=true evaluates a candidate new task.
	TaskIDs []string `json:"task_ids"`
	// IsCreate previews a not-yet-persisted task from AssigneeType/ID/Status.
	IsCreate     bool    `json:"is_create"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	Status       *string `json:"status"`
}

// TaskTriggerPreviewItem is one task that WILL start a run under the
// prospective write. AgentID is the runnable agent (squad leader for squads).
// HandoffSupported is the soft-gate signal: false when the target runtime's
// daemon is too old to render a handoff note, so the UI can gray out the note
// box rather than silently drop the text. The assignment itself still works.
type TaskTriggerPreviewItem struct {
	TaskID           string `json:"task_id"`
	AgentID          string `json:"agent_id"`
	Source           string `json:"source"`
	HandoffSupported bool   `json:"handoff_supported"`
}

// TaskTriggerPreviewResponse lists every task that will enqueue plus a total
// the UI can show directly ("将启动 N 个"). Tasks that will NOT start a run are
// simply absent, so total_count == len(triggers).
type TaskTriggerPreviewResponse struct {
	Triggers   []TaskTriggerPreviewItem `json:"triggers"`
	TotalCount int                      `json:"total_count"`
}

// PreviewTaskTrigger dry-runs WillEnqueueRun for a prospective task write and
// returns the runs that would start, without any side effect. It is the single
// authority the four entry points (create / single assign / single status /
// batch) consult so the frontend never re-implements the enqueue rule
// (SILI-3375). Mirrors PreviewCommentTriggers.
func (h *Handler) PreviewTaskTrigger(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace is required")
		return
	}

	var req TaskTriggerPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.TaskIDs) > maxPreviewTriggerTasks {
		writeError(w, http.StatusBadRequest, "too many task_ids")
		return
	}

	// Resolve the prospective assignee once — a malformed id is a deterministic
	// 400, never a silent miscount.
	var (
		newAssigneeType pgtype.Text
		newAssigneeID   pgtype.UUID
		hasNewAssignee  bool
	)
	if req.AssigneeType != nil && *req.AssigneeType != "" && req.AssigneeID != nil && *req.AssigneeID != "" {
		id, parseOK := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
		if !parseOK {
			return
		}
		newAssigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
		newAssigneeID = id
		hasNewAssignee = true
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	resp := TaskTriggerPreviewResponse{Triggers: make([]TaskTriggerPreviewItem, 0)}

	appendTrigger := func(task db.Task, in service.TaskTriggerInput) {
		probe := h.taskTriggerPreviewProbe(r, actorType, actorID, workspaceID, task)
		if trigger, ok := h.TaskService.WillEnqueueRun(r.Context(), in, probe); ok {
			resp.Triggers = append(resp.Triggers, TaskTriggerPreviewItem{
				TaskID:           uuidToString(trigger.TaskID),
				AgentID:          uuidToString(trigger.AgentID),
				Source:           string(trigger.Source),
				HandoffSupported: h.runtimeSupportsHandoff(r.Context(), trigger.AgentID),
			})
		}
	}

	if req.IsCreate {
		wsUUID, err := util.ParseUUID(workspaceID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workspace")
			return
		}
		status := "todo"
		if req.Status != nil && *req.Status != "" {
			status = *req.Status
		}
		candidate := db.Task{
			WorkspaceID:  wsUUID,
			Status:       status,
			AssigneeType: newAssigneeType,
			AssigneeID:   newAssigneeID,
		}
		appendTrigger(candidate, service.TaskTriggerInput{Task: candidate, IsCreate: true})
		resp.TotalCount = len(resp.Triggers)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	for _, rawID := range req.TaskIDs {
		taskUUID, err := util.ParseUUID(rawID)
		if err != nil {
			continue // malformed id contributes no trigger; deterministic
		}
		loaded, err := h.Queries.GetTaskInWorkspace(r.Context(), db.GetTaskInWorkspaceParams{
			ID:          taskUUID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue // cross-workspace / unknown id contributes no trigger
		}

		post := loaded
		in := service.TaskTriggerInput{PrevStatus: loaded.Status}
		if hasNewAssignee {
			post.AssigneeType = newAssigneeType
			post.AssigneeID = newAssigneeID
			in.AssigneeChanged = loaded.AssigneeType.String != newAssigneeType.String ||
				uuidToString(loaded.AssigneeID) != uuidToString(newAssigneeID)
		}
		if req.Status != nil && *req.Status != "" {
			post.Status = *req.Status
			in.StatusChanged = loaded.Status != *req.Status
		}
		in.Task = post
		appendTrigger(post, in)
	}

	resp.TotalCount = len(resp.Triggers)
	writeJSON(w, http.StatusOK, resp)
}

// runtimeSupportsHandoff reports whether the agent's bound runtime reports a
// CLI version new enough to render handoff notes. Drives the preview's
// handoff_supported soft-gate signal. Any resolution failure → false (degrade).
func (h *Handler) runtimeSupportsHandoff(ctx context.Context, agentID pgtype.UUID) bool {
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.RuntimeID.Valid {
		return false
	}
	rt, err := h.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return false
	}
	return agentver.HandoffSupported(readRuntimeCLIVersion(rt.Metadata))
}
