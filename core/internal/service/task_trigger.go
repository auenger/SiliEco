package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// RunEnqueueSource identifies which kind of task write would start an agent
// run. It is surfaced in preview responses so the UI can explain each trigger.
type RunEnqueueSource string

const (
	// RunSourceAssign covers task creation and assignee changes — the task
	// is being handed to an agent/squad. Parks silently on backlog.
	RunSourceAssign RunEnqueueSource = "assign"
	// RunSourceStatus covers promoting an already-assigned task out of
	// backlog into an active status.
	RunSourceStatus RunEnqueueSource = "status"
)

// TaskTriggerProbe carries the request-scoped checks WillEnqueueRun cannot
// resolve from task state alone.
//
// CanAccessAgent is the private-agent gate. The write paths enforce it at the
// HTTP boundary (validateAssigneePair on assign, canEnqueueSquadLeader inside
// the squad enqueue helper) and therefore pass an allow-all probe so the gate
// is never duplicated or sunk into the service layer. Preview passes the real
// gate so it never leaks a private agent's readiness to a member who cannot
// see it. A nil func is treated as allow-all.
//
// IsSelfLoop reports whether promoting this task out of backlog would be the
// calling agent re-triggering its own running task. Only the status source
// consults it. A nil func means "not a self-loop".
//
// SuppressActiveSelfAssignment reports whether a direct agent assignment is a
// task-scoped agent claiming the same (task, agent) pair while that pair
// already has non-terminal work. The ownership write still succeeds; only the
// duplicate enqueue is suppressed.
type TaskTriggerProbe struct {
	CanAccessAgent               func(agent db.Agent) bool
	IsSelfLoop                   func() bool
	SuppressActiveSelfAssignment func(agentID pgtype.UUID) bool
}

// TaskTriggerInput describes one prospective task write in its post-write
// shape. AssigneeChanged / StatusChanged mark which fields the write touches;
// IsCreate marks a brand-new task (no prior task to cancel, no self-loop).
type TaskTriggerInput struct {
	Task            db.Task
	PrevStatus      string
	IsCreate        bool
	AssigneeChanged bool
	StatusChanged   bool
}

// TaskRunTrigger is the resolved decision shared by preview and the write
// paths. AgentID is the agent that will actually run — the assignee for an
// agent task, the squad leader for a squad task.
type TaskRunTrigger struct {
	TaskID       pgtype.UUID
	AgentID      pgtype.UUID
	AssigneeType string
	Source       RunEnqueueSource
}

func allowAllAgents(db.Agent) bool { return true }

// WillEnqueueRun is the single predicate answering "will this task write
// start an agent run, and for whom". It is the one source of truth shared by
// the task update / batch-update write paths and the preview endpoint,
// replacing the per-site copies that drifted (squad omitted, self-loop
// omitted, four entry points inconsistent — see SILI-3375).
//
// It is intentionally a distinct predicate from the comment trigger
// (assignee fallback comment routing): task writes park on backlog while comments fire
// in any status. The two only share leaf readiness checks (AgentReadiness,
// the pending-task dedup), not the top-level decision.
//
// The decision must equal the real enqueue conditions so preview never claims
// a net-new run that the write path then drops. The write enqueues through
// CreateAgentTask, guarded by the (task_id, agent_id) partial unique index
// over pending (queued/dispatched) tasks; the pending check below mirrors that
// guard, and only the status source needs it:
//   - status source (backlog → active) can re-fire against an assignee that
//     already holds a pending task (e.g. one a @mention raised while the task
//     sat in backlog); the check keeps preview from promising a run the unique
//     index would coalesce away.
//   - assign source (create / assignee change) skips the check: a create
//     targets a fresh task with no prior task, and a reassignment no longer
//     cancels existing tasks (#4963 / SILI-4113) — in the rare case the new
//     assignee already holds a pending task the insert simply no-ops on the
//     same unique index, so the assignee still ends up with one pending run.
func (s *TaskService) WillEnqueueRun(ctx context.Context, in TaskTriggerInput, probe TaskTriggerProbe) (TaskRunTrigger, bool) {
	task := in.Task
	if !task.AssigneeType.Valid || !task.AssigneeID.Valid {
		return TaskRunTrigger{}, false
	}
	canAccess := probe.CanAccessAgent
	if canAccess == nil {
		canAccess = allowAllAgents
	}

	var source RunEnqueueSource
	switch {
	case in.IsCreate || in.AssigneeChanged:
		// Backlog is the parking lot: assigning into backlog never starts a run.
		if task.Status == "backlog" {
			return TaskRunTrigger{}, false
		}
		source = RunSourceAssign
	case in.StatusChanged && in.PrevStatus == "backlog" &&
		task.Status != "done" && task.Status != "cancelled":
		if probe.IsSelfLoop != nil && probe.IsSelfLoop() {
			return TaskRunTrigger{}, false
		}
		source = RunSourceStatus
	default:
		return TaskRunTrigger{}, false
	}

	switch task.AssigneeType.String {
	case "agent":
		agent, err := s.Queries.GetAgent(ctx, task.AssigneeID)
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			return TaskRunTrigger{}, false
		}
		if !canAccess(agent) {
			return TaskRunTrigger{}, false
		}
		if source == RunSourceAssign && !in.IsCreate && probe.SuppressActiveSelfAssignment != nil &&
			probe.SuppressActiveSelfAssignment(task.AssigneeID) {
			return TaskRunTrigger{}, false
		}
		if source == RunSourceStatus && s.hasPendingRun(ctx, task.ID, task.AssigneeID) {
			return TaskRunTrigger{}, false
		}
		return TaskRunTrigger{
			TaskID:       task.ID,
			AgentID:      task.AssigneeID,
			AssigneeType: "agent",
			Source:       source,
		}, true

	case "squad":
		// Direct self-assignment suppression does not apply to squads: assigning
		// a squad changes execution context even when the actor is its leader.
		squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          task.AssigneeID,
			WorkspaceID: task.WorkspaceID,
		})
		if err != nil {
			return TaskRunTrigger{}, false
		}
		leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return TaskRunTrigger{}, false
		}
		ready, _, err := AgentReadiness(ctx, s.Queries, leader)
		if err != nil || !ready {
			return TaskRunTrigger{}, false
		}
		if !canAccess(leader) {
			return TaskRunTrigger{}, false
		}
		if source == RunSourceStatus && s.hasPendingRun(ctx, task.ID, squad.LeaderID) {
			return TaskRunTrigger{}, false
		}
		return TaskRunTrigger{
			TaskID:       task.ID,
			AgentID:      squad.LeaderID,
			AssigneeType: "squad",
			Source:       source,
		}, true
	}
	return TaskRunTrigger{}, false
}

// hasPendingRun reports whether the agent already holds a queued or dispatched
// task for the task (the (task_id, agent_id) unique-index slot). Errors fail
// closed to "pending" so preview never over-promises a run.
func (s *TaskService) hasPendingRun(ctx context.Context, taskID, agentID pgtype.UUID) bool {
	pending, err := s.Queries.HasPendingRunForTaskAndAgent(ctx, db.HasPendingRunForTaskAndAgentParams{
		TaskID:  taskID,
		AgentID: agentID,
		// Key dedup on the reviewed head so a pending run against an old HEAD
		// does not shadow a request after HEAD advanced (TEN-356).
		HeadSha: headShaText(s.AgentRunService.ResolveTaskReviewSHA(ctx, taskID)),
	})
	if err != nil {
		return true
	}
	return pending
}
