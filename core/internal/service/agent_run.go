package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/analytics"
	"github.com/silieco-ai/silieco/core/internal/attribution"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/featureflags"
	obsmetrics "github.com/silieco-ai/silieco/core/internal/metrics"
	"github.com/silieco-ai/silieco/core/internal/realtime"
	"github.com/silieco-ai/silieco/core/internal/runtimeapps"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/db/relational"
	"github.com/silieco-ai/silieco/core/pkg/featureflag"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
	"github.com/silieco-ai/silieco/core/pkg/redact"
	"github.com/silieco-ai/silieco/core/pkg/runfailure"
	"github.com/silieco-ai/silieco/core/pkg/skillbundle"
)

type AgentRunService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Hub       *realtime.Hub
	Bus       *events.Bus
	Analytics analytics.Client
	Metrics   *obsmetrics.BusinessMetrics
	Wakeup    RunWakeupNotifier
	// FeatureFlags is the server-side toggle router. Nil is valid and returns
	// each call site's default.
	FeatureFlags *featureflag.Service
	// EmptyClaim caches "this runtime has no queued task" so the daemon
	// poll path can skip a Postgres scan on the steady-state empty case.
	// Optional — a nil cache disables the fast path and every claim
	// goes through the DB. Wired in router.go from the shared Redis
	// client.
	EmptyClaim *EmptyClaimCache
	// Composio computes the per-task MCP overlay (Stage 3 of the Composio
	// epic, SILI-3721) — the integration's "current user's connected apps
	// → MCP session URL" hook called from each Enqueue* path. Optional: a
	// nil ComposioOverlayBuilder turns the overlay step into a no-op so
	// every Silieco deployment that hasn't enabled Composio behaves
	// exactly as before. Wired in router.go after composiointeg.NewService
	// succeeds; the concrete type is *composio.Service.
	Composio ComposioOverlayBuilder

	analyticsContextMu    sync.Mutex
	analyticsContextCache map[string]analytics.RunContext
	analyticsContextOrder []string
}

// ComposioOverlayBuilder is the seam TaskService uses to build the per-task
// MCP overlay at enqueue time. Implemented by
// internal/integrations/composio.Service.BuildTaskOverlay; tests provide an
// inline fake so they don't have to spin a fake Composio SDK.
//
// Contract: a zero MCPOverlayResult means "no overlay for this run" — covers
// all gates the implementation enforces (no owner / empty allowlist / empty
// intersection with active connections / empty session URL). Any non-empty
// MCPOverlay is the exact value to store in agent_run.runtime_mcp_overlay;
// ConnectedApps is non-secret metadata to store alongside it for daemon brief
// injection. A non-nil error is surfaced to the caller but treated as
// best-effort — failed overlay computation must not fail the enqueue.
//
// agent is passed by value so the builder can inspect OwnerID and
// ComposioToolkitAllowlist without re-querying the DB; every enqueue path
// already loaded the agent for runtime/archive checks, so passing it is
// free and avoids a second GetAgent round-trip in the hot path.
type ComposioOverlayBuilder interface {
	BuildRunOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error)
}

type RunWakeupNotifier interface {
	NotifyRunAvailable(runtimeID, runID string)
}

// triggerSummaryMaxLen caps the snapshot length so the row stays cheap to
// transmit (it ends up in every task list response). 200 is enough for a
// recognisable preview of a one-paragraph comment.
const triggerSummaryMaxLen = 200

// truncateForSummary returns s shortened to maxRunes, with a trailing
// `…` when truncated. Operates on runes (not bytes) so multibyte characters
// — Chinese / emoji — count as one each. Strips surrounding whitespace
// first so a leading newline doesn't waste budget.
func truncateForSummary(s string, maxRunes int) string {
	// strings.Builder + Grow avoids the O(N²) realloc cycle of `+=` in
	// a loop. Grow uses byte length, which is an upper bound for the
	// rune-equivalent output (replacing \n/\r/\t with space is byte-equal
	// for ASCII whitespace), so we never reallocate.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	rs := []rune(strings.TrimSpace(b.String()))
	if len(rs) <= maxRunes {
		return string(rs)
	}
	return string(rs[:maxRunes]) + "…"
}

// maxSynthesizedFallbackCommentRunes bounds the completion-fallback comment that
// CompleteTask synthesizes from a task's final output when the agent left no
// comment of its own during the run. A real final assistant message is at most
// a few thousand words; anything larger is a runaway raw-stream dump — every
// streamed text delta concatenated together plus a literal `tool call` line per
// tool_use event — which some runtimes/providers emit as the task's Output on
// long, tool-heavy runs. Such a dump (observed at 190–264 KB) must never be
// posted, even partially, to the task thread (GH #5455).
const maxSynthesizedFallbackCommentRunes = 8000

const oversizedFallbackCommentNotice = "This task completed, but its output was too large to post safely. The raw output was not posted. Review the task in this task's Execution log."

// truncateFallbackCommentBody bounds a synthesized completion-fallback comment
// body. Unlike truncateForSummary (which flattens newlines for a one-line row
// snapshot), it preserves genuine final messages below the cap verbatim. Output
// above the cap is untrusted: the reported failure mode puts process narration
// and tool traces at the head, so retaining any excerpt can expose execution
// details and still discard the final answer. Replace the entire body with a
// fixed notice instead. Callers pass the already-redacted body.
func truncateFallbackCommentBody(body string, maxRunes int) string {
	if utf8.RuneCountInString(body) <= maxRunes {
		return body
	}
	return oversizedFallbackCommentNotice
}

const (
	runAnalyticsContextCacheMax = 4096
	// claimResponseRecoveryWindow must exceed daemon client.Timeout for
	// /runs/claim (30s) plus /tasks/{id}/start (30s) plus scheduling slack.
	// Longer pre-start work is protected by prepareLeaseDuration instead of
	// stretching this global crash-recovery window.
	claimResponseRecoveryWindow = 90 * time.Second
	prepareLeaseDuration        = 45 * time.Second
)

// buildCommentTriggerSummary fetches the comment content and truncates
// it for storage on the task row. Returns an invalid pgtype.Text when
// the comment is missing (deleted / wrong workspace / etc) so the column
// stays NULL — front-end falls back to a structural label in that case.
//
// workspaceID scopes the fetch to the task's own workspace: the summary is
// later returned in claim / task-history responses, so a foreign comment UUID
// reaching an enqueue/merge path must NOT leak another workspace's text even in
// truncated form (SILI-4252).
func (s *AgentRunService) buildCommentTriggerSummary(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.Text {
	if !commentID.Valid {
		return pgtype.Text{}
	}
	comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID:          commentID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return pgtype.Text{}
	}
	summary := truncateForSummary(comment.Content, triggerSummaryMaxLen)
	if summary == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: summary, Valid: true}
}

// ResolveOriginatorFromTriggerComment is the exported wrapper used by the
// comment-merge path (SILI-4195) to compute the top-of-chain human originator
// for a newly-arrived comment, so a merge can be gated on the originator being
// unchanged. workspaceID scopes the comment lookup to the task's workspace
// (SILI-4252). See resolveOriginatorFromTriggerComment for the chain rules.
func (s *AgentRunService) ResolveOriginatorFromTriggerComment(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.UUID {
	return s.resolveOriginatorFromTriggerComment(ctx, workspaceID, commentID)
}

// AttributionForMergedComment resolves the FULL attribution snapshot for a comment
// being coalesced into an already-queued task (SILI-4302). A merge re-attributes the
// run to the newly-arrived comment's human, so the whole snapshot — source, evidence,
// delegation lineage, and both person columns — must move together as one
// attribution.Result; re-stamping only the person columns would leave a run showing
// B accountable while still pointing at A's stale source / evidence / level. isMention
// picks the agent-authored label (delegation for a mention / thread-parent, otherwise
// comment_source), matching the fresh-enqueue routing.
//
// The merge re-opens the same fail-closed decision the original enqueue faced: a merge
// swaps the effective trigger, responsible human, and evidence to the NEW comment, so
// "the enqueue already checked" does not carry over. It runs the comment through
// applyAttributionFallback — the identical fail-closed gate the fresh-enqueue path uses
// — and returns ErrAttributionFailClosed when the new comment cannot be attributed
// precisely and the workspace forbids the owner_fallback degrade. The caller must then
// REFUSE the merge and keep the original (precisely-attributed) task snapshot rather
// than re-stamp a queued run to a degraded owner_fallback (Elon must-fix).
func (s *AgentRunService) AttributionForMergedComment(ctx context.Context, workspaceID, commentID pgtype.UUID, isMention bool, agent db.Agent) (attribution.Result, error) {
	agentAuthoredSource := attribution.SourceCommentSource
	if isMention {
		agentAuthoredSource = attribution.SourceDelegation
	}
	attr := s.attributionFromTriggerComment(ctx, workspaceID, commentID, agentAuthoredSource)
	return s.applyAttributionFallback(ctx, attr, agent)
}

// BuildCommentTriggerSummary is the exported wrapper used by the comment-merge
// path (SILI-4195) to refresh a coalesced task's trigger_summary to the newest
// trigger comment's snapshot. workspaceID scopes the lookup (SILI-4252).
func (s *AgentRunService) BuildCommentTriggerSummary(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.Text {
	return s.buildCommentTriggerSummary(ctx, workspaceID, commentID)
}

// BuildRuntimeMCPOverlayForMerge recomputes the Composio MCP overlay +
// connected-app metadata for (originatorUserID, agent), used when a merge
// re-stamps a coalesced task's originator (SILI-4195 review must-fix #1). The
// overlay is a pure function of (originator, agent); re-stamping it alongside
// originator_user_id keeps the coalescing run's connected-app capabilities and
// audit attribution consistent with the latest trigger comment's originator
// instead of the task's original one. Fails soft to empty (same as the enqueue
// path) so a transient Composio hiccup never blocks the merge.
func (s *AgentRunService) BuildRuntimeMCPOverlayForMerge(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (overlay, connectedApps []byte) {
	data := s.buildRuntimeMCPOverlay(ctx, originatorUserID, agent)
	return data.Overlay, data.ConnectedApps
}

func NewAgentRunService(q *db.Queries, tx TxStarter, hub *realtime.Hub, bus *events.Bus, wakeups ...RunWakeupNotifier) *AgentRunService {
	var wakeup RunWakeupNotifier
	if len(wakeups) > 0 {
		wakeup = wakeups[0]
	}
	return &AgentRunService{Queries: q, TxStarter: tx, Hub: hub, Bus: bus, Wakeup: wakeup}
}

var trivialDoneMarkers = []string{
	"done",
	"готово",
	"готова",
	"сделано",
	"完成",
	"完了",
}

func isTrivialDoneOutput(output string) bool {
	normalized := strings.TrimSpace(strings.ToLower(output))
	normalized = strings.Trim(normalized, ".!！。… ")
	for _, marker := range trivialDoneMarkers {
		if normalized == marker {
			return true
		}
	}
	return false
}

func (s *AgentRunService) captureRunQueued(ctx context.Context, agentRun db.AgentRun) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunEnqueued(source, runtimeMode)
	}
}

type runtimeMCPOverlayData struct {
	Overlay       json.RawMessage
	ConnectedApps json.RawMessage
}

// buildRuntimeMCPOverlay computes the optional per-task Composio MCP overlay.
// Enqueue paths call this BEFORE inserting the queued row so the daemon cannot
// claim a task during the network round-trip to Composio and miss the overlay.
func (s *AgentRunService) buildRuntimeMCPOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) runtimeMCPOverlayData {
	if s == nil || s.Composio == nil {
		return runtimeMCPOverlayData{}
	}
	if !featureflags.ComposioMCPAppsEnabled(ctx, s.FeatureFlags) {
		return runtimeMCPOverlayData{}
	}
	result, err := s.Composio.BuildRunOverlay(ctx, originatorUserID, agent)
	if err != nil {
		slog.Warn("runtime mcp overlay: BuildTaskOverlay failed; task will run without composio overlay",
			"originator_user_id", util.UUIDToString(originatorUserID),
			"agent_id", util.UUIDToString(agent.ID),
			"error", err,
		)
		return runtimeMCPOverlayData{}
	}
	if len(result.MCPOverlay) == 0 {
		slog.Debug("runtime mcp overlay: no composio overlay for task",
			"originator_user_id", util.UUIDToString(originatorUserID),
			"agent_id", util.UUIDToString(agent.ID),
		)
		return runtimeMCPOverlayData{}
	}
	data := runtimeMCPOverlayData{Overlay: result.MCPOverlay}
	if len(result.ConnectedApps) > 0 {
		raw, err := json.Marshal(result.ConnectedApps)
		if err != nil {
			slog.Warn("runtime mcp overlay: marshal connected app metadata failed",
				"originator_user_id", util.UUIDToString(originatorUserID),
				"agent_id", util.UUIDToString(agent.ID),
				"error", err,
			)
			return data
		}
		data.ConnectedApps = raw
	}
	return data
}

// resolveOriginatorFromTriggerComment returns the top-of-chain HUMAN user
// id for a comment that triggered an Enqueue* path. The chain rules
// (SILI-3869):
//
//   - trigger comment authored by a member → originator = author_id (that
//     member IS the top-of-chain human).
//   - trigger comment authored by an agent → read the parent task via
//     comment.source_run_id and inherit its originator_user_id. This is
//     the load-bearing case for agent fan-out: agent A @-mentions agent B,
//     comment author is A, but we MUST surface the human who originally
//     told A to run, not lose the originator at the first agent hop.
//   - missing comment / unknown source task / NULL parent originator →
//     invalid pgtype.UUID. BuildTaskOverlay treats that as "no overlay"
//     (gate 1).
//
// A nil receiver / nil Queries falls through to invalid so unit-test
// setups that don't wire a DB stay safe. workspaceID scopes the comment lookup
// to the task's workspace so a foreign comment UUID cannot resolve an
// originator from another tenant (SILI-4252).
func (s *AgentRunService) resolveOriginatorFromTriggerComment(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.UUID {
	// The originator VALUE is independent of the agent-authored source label, so
	// any label works here; comment_source is passed only as a placeholder.
	return s.attributionFromTriggerComment(ctx, workspaceID, commentID, attribution.SourceCommentSource).UserID
}

// attributionFromTriggerComment resolves the full attribution (accountable
// human + provenance label + delegation lineage + evidence) for a
// comment-triggered run. It performs the DB reads and hands the gathered facts
// to the pure attribution.ClassifyComment rules so the classification stays
// side-effect-free and unit-tested. The returned UserID is byte-identical to
// the pre-SILI-4302 originator resolution, so authorization behavior (Composio
// overlay, canInvokeAgent A2A gate) is unchanged. workspaceID scopes the comment
// lookup to the task's workspace (SILI-4252).
//
// agentAuthoredSource selects the label for an agent-authored trigger comment:
// attribution.SourceCommentSource for the task-assignee-reacting path,
// attribution.SourceDelegation for an explicit mention / thread-parent /
// squad-leader path.
func (s *AgentRunService) attributionFromTriggerComment(ctx context.Context, workspaceID, commentID pgtype.UUID, agentAuthoredSource attribution.Source) attribution.Result {
	if s == nil || s.Queries == nil || !commentID.Valid {
		return attribution.Result{Source: attribution.SourceUnattributed}
	}
	comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID:          commentID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return attribution.Result{Source: attribution.SourceUnattributed}
	}
	return s.attributionFromComment(ctx, comment, agentAuthoredSource)
}

// attributionFromComment classifies a run from an already-loaded trigger comment,
// so a caller that already has the row (e.g. to inspect author_type) does not
// re-read it. Kept byte-identical to the inline logic attributionFromTriggerComment
// used before, so authorization behavior is unchanged.
func (s *AgentRunService) attributionFromComment(ctx context.Context, comment db.Comment, agentAuthoredSource attribution.Source) attribution.Result {
	facts := attribution.CommentFacts{
		CommentID:  comment.ID,
		AuthorType: comment.AuthorType,
		AuthorID:   comment.AuthorID,
	}
	// For an agent-authored comment, walk comment.source_run_id → parent task →
	// parent.originator_user_id (set by every agent comment-write path since
	// migration 120). A NULL/missing source task leaves ParentOriginator
	// invalid, which ClassifyComment maps to unattributed.
	if comment.AuthorType == "agent" && comment.SourceRunID.Valid {
		facts.SourceRunID = comment.SourceRunID
		if parent, err := s.Queries.GetAgentRun(ctx, comment.SourceRunID); err == nil {
			facts.ParentOriginator = parent.OriginatorUserID
			facts.ParentAccountable = parent.AccountableUserID
		}
	}
	return attribution.ClassifyComment(facts, agentAuthoredSource)
}

// resolveOriginatorForTaskRun returns the top-of-chain human for task-backed
// dispatches. Comment-triggered runs keep the existing comment-chain semantics;
// direct task assignment/creation falls back to the task's member creator.
// Agent-created tasks that carry an explicit task-origin link — quick_create
// (daemon quick-create flow) or agent_create (an agent's ordinary `task
// create`, SILI-4305) — inherit that origin task's originator, since origin_id
// points at the agent_run row that created the task. Other
// agent/system origins, including autopilot, deliberately remain unattributed.
func (s *AgentRunService) resolveOriginatorForTaskRun(ctx context.Context, task db.Task, triggerCommentID pgtype.UUID) pgtype.UUID {
	return s.attributionForTaskRun(ctx, task, triggerCommentID, attribution.SourceCommentSource, pgtype.UUID{}).UserID
}

// attributionForTaskRun resolves the full attribution for an task-backed
// enqueue. Comment-triggered runs keep the comment-chain semantics; direct
// assignment/creation falls back to the task's member creator; agent-created
// quick-create tasks inherit the origin task's human as a delegation. The
// accountable-human value is byte-identical to resolveOriginatorForTaskRun,
// which now delegates here — so there is a single source of truth and
// authorization is unaffected. agentAuthoredSource labels the agent-authored
// trigger comment case (see attributionFromTriggerComment).
func (s *AgentRunService) attributionForTaskRun(ctx context.Context, task db.Task, triggerCommentID pgtype.UUID, agentAuthoredSource attribution.Source, actorUserID pgtype.UUID) attribution.Result {
	// A direct member action is the accountable human AND originator, ahead of any
	// trigger comment, origin, or rule (SILI-4302 §4/§5). This covers assign/promote,
	// a manual autopilot trigger, and a manual rerun — the last of which may INHERIT
	// a triggerCommentID for the daemon's prompt context, but must still attribute to
	// the member who clicked rerun, not the original comment's human. So the actor is
	// checked before the trigger-comment / origin branches.
	if actorUserID.Valid {
		return attribution.ClassifyDirect(attribution.DirectFacts{TaskID: task.ID, ActorUserID: actorUserID})
	}
	if triggerCommentID.Valid {
		if s == nil || s.Queries == nil {
			return attribution.Result{Source: attribution.SourceUnattributed}
		}
		// workspace-scoped so a foreign comment UUID cannot resolve a human from
		// another tenant (SILI-4252).
		comment, err := s.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
			ID:          triggerCommentID,
			WorkspaceID: task.WorkspaceID,
		})
		if err != nil {
			return attribution.Result{Source: attribution.SourceUnattributed}
		}
		// A member/agent trigger comment resolves the human (direct_human / delegation
		// / comment_source). A SYSTEM-authored comment — today the Stage-completion
		// child-done comment (task_child_done.go), which wakes the parent assignee
		// and threads no actor — carries no human and is not part of any delegation
		// chain. Classifying it would degrade straight to owner_fallback (the agent's
		// own owner), which is wrong for a Stage cascade: the woken run should be
		// accountable to whoever caused the PARENT task to exist. So for a system
		// comment we skip the comment branch and fall through to the parent task's
		// own provenance below — the same creator / agent_create-origin /
		// autopilot-origin chain a direct enqueue resolves — reaching owner_fallback
		// only if that provenance itself has no human (SILI-4302; raised by Bohan on
		// the stage-cascade fallback).
		if comment.AuthorType != "system" {
			return s.attributionFromComment(ctx, comment, agentAuthoredSource)
		}
	}
	// Autopilot-origin tasks (origin_id is the autopilot id) from a schedule /
	// webhook trigger: no human authorized the run, so originator stays NULL, but it
	// is accountable to the human currently RESPONSIBLE for the firing trigger's
	// effective config (creator, then last substantive editor) — trigger_owner
	// (SILI-4302; Elon must-fix), degrading to the rule publisher when no such member
	// is recoverable. Resolved the same way run_only dispatch resolves
	// it, so both autopilot execution modes attribute identically. (A manual trigger
	// carries an actor and is already handled above.) The task only stores the
	// autopilot id, so bridge task → active run → trigger_id to find the trigger.
	if s != nil && s.Queries != nil && task.OriginType.Valid &&
		task.OriginType.String == "autopilot" && task.OriginID.Valid {
		var triggerID pgtype.UUID
		if run, err := s.Queries.GetAutopilotRunByTask(ctx, task.ID); err == nil {
			triggerID = run.TriggerID
		}
		return triggerOwnerAttribution(ctx, s.Queries, triggerID, task.WorkspaceID, task.OriginID, attribution.EvidenceTaskAssignment, task.ID)
	}
	facts := attribution.DirectFacts{
		TaskID:      task.ID,
		CreatorType: task.CreatorType,
		CreatorID:   task.CreatorID,
	}
	// Member-created tasks resolve without a DB read. Only origin-linked
	// agent-created tasks (quick_create, agent_create) need to load the origin
	// task to inherit its human, and only when the DB is wired (nil Queries keeps
	// unit-test setups safe and yields unattributed). Both origin types stamp
	// origin_id with the agent_run row that created the task, so the
	// top-of-chain human is that task's originator_user_id (SILI-4305).
	if !(task.CreatorType == "member" && task.CreatorID.Valid) &&
		s != nil && s.Queries != nil && task.OriginType.Valid && task.OriginID.Valid &&
		(task.OriginType.String == "quick_create" || task.OriginType.String == "agent_create") {
		facts.OriginType = task.OriginType.String
		facts.OriginRunID = task.OriginID
		if agentRun, err := s.Queries.GetAgentRun(ctx, task.OriginID); err == nil {
			facts.OriginOriginator = agentRun.OriginatorUserID
			facts.OriginAccountable = agentRun.AccountableUserID
		}
	}
	return attribution.ClassifyDirect(facts)
}

// ruleOwnerAttribution resolves the rule_owner attribution for an autopilot run
// from its active (latest) rule version snapshot (SILI-4302 §3.4). Shared by both
// autopilot execution modes — run_only dispatch and the create_task enqueue path —
// so they attribute identically. originator stays NULL (an autopilot carries no
// human's authority); only the audit-accountable side is set, to the version's
// member publisher. A missing version (autopilot published before this feature, or
// none yet) or a non-member/absent publisher degrades to unattributed rather than
// fabricating a human. Never returns an error: attribution must not fail an
// enqueue, and a degraded label is the honest fallback.
func ruleOwnerAttribution(ctx context.Context, q *db.Queries, workspaceID, autopilotID pgtype.UUID, evidenceKind attribution.EvidenceKind, evidenceRefID pgtype.UUID) attribution.Result {
	if q == nil || !autopilotID.Valid {
		return attribution.RuleOwner(pgtype.UUID{}, pgtype.UUID{}, evidenceKind, evidenceRefID)
	}
	ver, err := q.GetActiveAutopilotRuleVersion(ctx, db.GetActiveAutopilotRuleVersionParams{
		WorkspaceID: workspaceID,
		AutopilotID: autopilotID,
	})
	if err != nil {
		return attribution.RuleOwner(pgtype.UUID{}, pgtype.UUID{}, evidenceKind, evidenceRefID)
	}
	var publisher pgtype.UUID
	if ver.PublishedByType == "member" {
		publisher = ver.PublishedByID
	}
	return attribution.RuleOwner(publisher, ver.ID, evidenceKind, evidenceRefID)
}

// triggerOwnerAttribution resolves an autopilot schedule/webhook run to the human
// currently RESPONSIBLE for the firing trigger's effective config (SILI-4302; Bohan +
// Elon must-fix). triggerID is the autopilot_run's trigger_id. The trigger row's
// published_by starts at the creator and transfers to whoever later substantively
// edits it, so the run attributes to whoever last shaped what fires it — not the
// original creator. A trigger with no recorded publisher (predating this migration)
// or an agent publisher degrades to ruleOwnerAttribution (rule publisher, then
// owner_fallback) — the same coarser behavior autopilots had before, so nothing
// regresses. Never errors: attribution must not fail an enqueue.
func triggerOwnerAttribution(ctx context.Context, q *db.Queries, triggerID, workspaceID, autopilotID pgtype.UUID, evidenceKind attribution.EvidenceKind, evidenceRefID pgtype.UUID) attribution.Result {
	if q != nil && triggerID.Valid {
		// published_by is the member CURRENTLY responsible for this trigger's
		// effective config: the creator until someone substantively edits it (that
		// trigger's cron/filter/webhook, or an autopilot-level change that bumps all
		// its triggers), then the editor. So a run attributes to whoever last shaped
		// what fires it, not the original creator — and editing another trigger never
		// moves this one (SILI-4302; Elon must-fix).
		if trig, err := q.GetAutopilotTrigger(ctx, triggerID); err == nil &&
			trig.PublishedByType.Valid && trig.PublishedByType.String == "member" && trig.PublishedByID.Valid {
			return attribution.TriggerOwner(trig.PublishedByID, evidenceKind, evidenceRefID)
		}
	}
	return ruleOwnerAttribution(ctx, q, workspaceID, autopilotID, evidenceKind, evidenceRefID)
}

// ErrAttributionFailClosed signals that a run resolved to no precise accountable
// human and the enqueue is REFUSED rather than started. It covers three cases, all
// of which mean "we cannot guarantee an accountable human for this run" (SILI-4302
// §1/§3.5): the workspace opted into fail-closed; the workspace policy could not be
// read (so we cannot confirm fallback is allowed — fail closed, don't run); or
// owner_fallback has no agent owner to fall back to. Enqueue paths surface it so the
// run never starts.
var ErrAttributionFailClosed = errors.New("attribution: no precise accountable human and enqueue refused (fail-closed policy, policy read failed, or no agent owner)")

// ErrDuplicatePendingRun means a fresh enqueue lost the race to a concurrent
// one: a queued/dispatched run for the same (task, agent) already exists, so
// the idx_one_pending_run_per_task_agent unique index rejected the insert
// (#5914). This is a benign outcome — a sibling run already covers this target
// — not a server fault. Enqueue paths return it so callers can report a
// success-shaped coalesced outcome / structured 409 instead of surfacing the
// raw Postgres constraint as a 500. It is returned BARE (the raw driver text,
// including the index name, is logged once at debug and never wrapped in) so no
// upper-layer log or response can leak the constraint name (#5914, Elon review).
var ErrDuplicatePendingRun = errors.New("a pending run for this task and agent already exists")

// isDuplicatePendingRunErr reports whether err is the unique-index violation on
// idx_one_pending_run_per_task_agent (a concurrent enqueue won the race).
func isDuplicatePendingRunErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_one_pending_run_per_task_agent"
}

// applyAttributionFallback applies the workspace's degraded-attribution policy to a
// resolved attribution whose source came back unattributed (no precise human). A
// PRECISE attribution passes through untouched (no policy read at all). For an
// unattributed run the accountable-never-null guarantee is enforced fail-closed —
// we never silently enqueue a task that could run with a NULL accountable_user_id:
//
//   - policy read fails (or no workspace) → REFUSE. We cannot confirm the workspace
//     permits fallback, so we do not run an unattributable task on a transient DB
//     hiccup. (Only the rare unattributed path pays this; precise runs never read.)
//   - fail-closed workspace → REFUSE.
//   - otherwise → owner_fallback (accountable = agent owner, audit-only, originator
//     untouched). If there is no valid agent owner, owner_fallback stays
//     unattributed → REFUSE rather than enqueue a NULL-accountable task.
//
// Keeping this at the enqueue boundary (not inside the pure classifiers) means
// owner_fallback needs the agent owner, which every enqueue path has in hand.
func (s *AgentRunService) applyAttributionFallback(ctx context.Context, attr attribution.Result, agent db.Agent) (attribution.Result, error) {
	if attr.Source != attribution.SourceUnattributed {
		return attr, nil
	}
	if s == nil || s.Queries == nil || !agent.WorkspaceID.Valid {
		return attr, fmt.Errorf("%w: workspace policy unavailable", ErrAttributionFailClosed)
	}
	failClosed, err := s.Queries.GetWorkspaceAttributionFailClosed(ctx, agent.WorkspaceID)
	if err != nil {
		// Cannot confirm the workspace allows fallback → fail closed rather than
		// silently run an unattributable task.
		return attr, fmt.Errorf("%w: policy read failed: %v", ErrAttributionFailClosed, err)
	}
	if failClosed {
		return attr, ErrAttributionFailClosed
	}
	fallback := attribution.OwnerFallback(attr, agent.OwnerID)
	if fallback.Source == attribution.SourceUnattributed {
		// owner_fallback could not resolve an accountable human (no valid agent
		// owner): refuse rather than enqueue a NULL-accountable task.
		return attr, fmt.Errorf("%w: no agent owner to attribute", ErrAttributionFailClosed)
	}
	return fallback, nil
}

// attributionCreateParams maps a resolved attribution onto the CreateAgentTask
// provenance columns. originator_source is always stamped (never NULL for a new
// row); delegation lineage and evidence are stamped only when present.
func attributionCreateParams(attr attribution.Result) (source pgtype.Text, delegatedFrom pgtype.UUID, evidenceKind pgtype.Text, evidenceRef pgtype.UUID) {
	source = pgtype.Text{String: attr.Source.String(), Valid: true}
	delegatedFrom = attr.DelegatedFromRunID
	evidenceKind = pgtype.Text{String: string(attr.EvidenceKind), Valid: attr.EvidenceKind != ""}
	evidenceRef = attr.EvidenceRefID
	return
}

// OriginatorForTaskRun exposes resolveOriginatorForTaskRun to callers
// outside the service package (the squad-leader access gate in the handler
// layer) so the gate judges the top-of-chain human with the exact same
// resolution the enqueue path persists on the task row. Without a shared entry
// point the gate saw an empty originator for agent-triggered assigns and denied
// private leaders that the write path would have attributed correctly
// (SILI-4305).
func (s *AgentRunService) OriginatorForTaskRun(ctx context.Context, task db.Task, triggerCommentID pgtype.UUID) pgtype.UUID {
	return s.resolveOriginatorForTaskRun(ctx, task, triggerCommentID)
}

func (s *AgentRunService) captureRunDispatched(ctx context.Context, agentRun db.AgentRun) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunDispatched(util.UUIDToString(agentRun.ID), source, runtimeMode, runQueueWaitSeconds(agentRun))
	}
}

func (s *AgentRunService) AnalyticsContextForRun(ctx context.Context, agentRun db.AgentRun) analytics.RunContext {
	return s.runAnalyticsContext(ctx, agentRun)
}

func (s *AgentRunService) captureRunStarted(ctx context.Context, agentRun db.AgentRun) {
	if s.Metrics != nil {
		source, runtimeMode, provider := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunStarted(source, runtimeMode, provider)
	}
}

func (s *AgentRunService) captureRunCompleted(ctx context.Context, agentRun db.AgentRun) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunTerminal(util.UUIDToString(agentRun.ID), source, runtimeMode, agentRun.Status, runRunSeconds(agentRun), runTotalSeconds(agentRun), agentRun.Attempt)
	}
}

func (s *AgentRunService) captureRunFailed(ctx context.Context, agentRun db.AgentRun) {
	failureReason := runFailureReason(agentRun)
	if s.Metrics != nil {
		source, runtimeMode, _ := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunTerminal(util.UUIDToString(agentRun.ID), source, runtimeMode, agentRun.Status, runRunSeconds(agentRun), runTotalSeconds(agentRun), agentRun.Attempt)
		s.Metrics.RecordRunFailed(source, runtimeMode, failureReason)
	}
}

func (s *AgentRunService) captureRunCancelled(ctx context.Context, agentRun db.AgentRun) {
	if s.Metrics != nil {
		source, runtimeMode, _ := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunTerminal(util.UUIDToString(agentRun.ID), source, runtimeMode, agentRun.Status, runRunSeconds(agentRun), runTotalSeconds(agentRun), agentRun.Attempt)
	}
	// Revoke any mat_ task tokens minted for this task. Cancellation is
	// a terminal transition, so the running agent process no longer
	// needs to call back; eagerly deleting the token closes the
	// window where a compromised process could keep authenticating
	// against the API until the 24h expiry. Failure is non-fatal — the
	// expiry / FK cascade are the durable guards. SILI-2600.
	if err := s.Queries.DeleteAgentRunTokensByRun(ctx, agentRun.ID); err != nil {
		slog.Warn("cancel run: failed to revoke task tokens",
			"run_id", util.UUIDToString(agentRun.ID), "error", err)
	}
}

// costUSDTicks is the provider's own price for this usage in 1e-10 USD, or 0
// when it reported none — the metrics layer prefers it over its rate table.
func (s *AgentRunService) CaptureAgentRunUsage(ctx context.Context, agentRun db.AgentRun, provider, model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, costUSDTicks int64) {
	if s.Metrics == nil {
		return
	}
	source, runtimeMode, _ := s.runMetricsContext(ctx, agentRun)
	s.Metrics.RecordLLMUsage(source, runtimeMode, provider, model, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, costUSDTicks)
}

func (s *AgentRunService) CaptureQueuedExpiredRuns(ctx context.Context, agentRuns []db.AgentRun) {
	if s.Metrics == nil {
		return
	}
	for _, agentRun := range agentRuns {
		source, runtimeMode, _ := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunQueuedExpired(source, runtimeMode)
	}
}

func (s *AgentRunService) CaptureLeaseExpiredRuns(ctx context.Context, agentRuns []db.AgentRun) {
	if s.Metrics == nil {
		return
	}
	for _, agentRun := range agentRuns {
		source, _, _ := s.runMetricsContext(ctx, agentRun)
		s.Metrics.RecordRunLeaseExpired(source)
	}
}

func (s *AgentRunService) cachedRunAnalyticsContext(agentRun db.AgentRun) (analytics.RunContext, bool) {
	key := runAnalyticsContextKey(agentRun)
	if key == "" {
		return analytics.RunContext{}, false
	}
	s.analyticsContextMu.Lock()
	defer s.analyticsContextMu.Unlock()
	if s.analyticsContextCache == nil {
		return analytics.RunContext{}, false
	}
	tc, ok := s.analyticsContextCache[key]
	return tc, ok
}

func (s *AgentRunService) storeRunAnalyticsContext(agentRun db.AgentRun, tc analytics.RunContext) {
	if tc.WorkspaceID == "" {
		return
	}
	key := runAnalyticsContextKey(agentRun)
	if key == "" {
		return
	}
	s.analyticsContextMu.Lock()
	defer s.analyticsContextMu.Unlock()
	if s.analyticsContextCache == nil {
		s.analyticsContextCache = make(map[string]analytics.RunContext)
	}
	if _, ok := s.analyticsContextCache[key]; !ok {
		s.analyticsContextOrder = append(s.analyticsContextOrder, key)
		if len(s.analyticsContextOrder) > runAnalyticsContextCacheMax {
			oldest := s.analyticsContextOrder[0]
			s.analyticsContextOrder = s.analyticsContextOrder[1:]
			delete(s.analyticsContextCache, oldest)
		}
	}
	s.analyticsContextCache[key] = tc
}

func runAnalyticsContextKey(agentRun db.AgentRun) string {
	runID := util.UUIDToString(agentRun.ID)
	if runID == "" {
		return ""
	}
	return strings.Join([]string{
		runID,
		util.UUIDToString(agentRun.RuntimeID),
		util.UUIDToString(agentRun.TaskID),
		util.UUIDToString(agentRun.ChatSessionID),
		util.UUIDToString(agentRun.AutopilotRunID),
	}, "|")
}

func (s *AgentRunService) runMetricsContext(ctx context.Context, agentRun db.AgentRun) (source, runtimeMode, provider string) {
	tc := s.runAnalyticsContext(ctx, agentRun)
	source = "other"
	switch {
	case agentRun.ChatSessionID.Valid:
		source = "chat"
	case agentRun.TaskID.Valid:
		if tc.Source == analytics.SourceAutopilot {
			source = "autopilot_task"
		} else {
			source = "task"
		}
	case agentRun.AutopilotRunID.Valid:
		source = "autopilot"
	default:
		if _, ok := s.parseQuickCreateContext(agentRun); ok {
			source = "quick_create"
		} else if tc.Source != "" {
			source = tc.Source
		}
	}
	return source, tc.RuntimeMode, tc.Provider
}

func (s *AgentRunService) runAnalyticsContext(ctx context.Context, agentRun db.AgentRun) analytics.RunContext {
	if tc, ok := s.cachedRunAnalyticsContext(agentRun); ok {
		return tc
	}
	tc := analytics.RunContext{
		AgentID: util.UUIDToString(agentRun.AgentID),
		RunID:   util.UUIDToString(agentRun.ID),
		Source:  analytics.SourceManual,
	}
	if agentRun.TaskID.Valid {
		tc.TaskID = util.UUIDToString(agentRun.TaskID)
	}
	if agentRun.ChatSessionID.Valid {
		tc.ChatSessionID = util.UUIDToString(agentRun.ChatSessionID)
		tc.Source = analytics.SourceChat
	}
	if agentRun.AutopilotRunID.Valid {
		tc.AutopilotRunID = util.UUIDToString(agentRun.AutopilotRunID)
		tc.Source = analytics.SourceAutopilot
	}

	if agentRun.RuntimeID.Valid {
		if rt, err := s.Queries.GetAgentRuntime(ctx, agentRun.RuntimeID); err == nil {
			tc.WorkspaceID = util.UUIDToString(rt.WorkspaceID)
			tc.RuntimeMode = rt.RuntimeMode
			tc.Provider = rt.Provider
		}
	}
	if tc.WorkspaceID == "" || tc.RuntimeMode == "" {
		if agent, err := s.Queries.GetAgent(ctx, agentRun.AgentID); err == nil {
			if tc.WorkspaceID == "" {
				tc.WorkspaceID = util.UUIDToString(agent.WorkspaceID)
			}
			if tc.RuntimeMode == "" {
				tc.RuntimeMode = agent.RuntimeMode
			}
		}
	}

	if agentRun.TaskID.Valid {
		if task, err := s.Queries.GetTask(ctx, agentRun.TaskID); err == nil {
			tc.WorkspaceID = util.UUIDToString(task.WorkspaceID)
			if task.CreatorType == "member" {
				tc.UserID = util.UUIDToString(task.CreatorID)
			}
			if task.OriginType.Valid {
				switch task.OriginType.String {
				case "autopilot":
					tc.Source = analytics.SourceAutopilot
					if ap, err := s.Queries.GetAutopilot(ctx, task.OriginID); err == nil {
						if ap.CreatedByType == "member" {
							tc.UserID = util.UUIDToString(ap.CreatedByID)
						}
					}
				case "quick_create":
					tc.Source = analytics.SourceManual
				}
			}
		}
	}
	if agentRun.ChatSessionID.Valid {
		if cs, err := s.Queries.GetChatSession(ctx, agentRun.ChatSessionID); err == nil {
			tc.WorkspaceID = util.UUIDToString(cs.WorkspaceID)
			tc.UserID = util.UUIDToString(cs.CreatorID)
		}
	}
	if agentRun.AutopilotRunID.Valid {
		if run, err := s.Queries.GetAutopilotRun(ctx, agentRun.AutopilotRunID); err == nil {
			if ap, err := s.Queries.GetAutopilot(ctx, run.AutopilotID); err == nil {
				tc.WorkspaceID = util.UUIDToString(ap.WorkspaceID)
				if ap.CreatedByType == "member" {
					tc.UserID = util.UUIDToString(ap.CreatedByID)
				}
			}
		}
	}
	if qc, ok := s.parseQuickCreateContext(agentRun); ok {
		tc.WorkspaceID = qc.WorkspaceID
		tc.UserID = qc.RequesterID
		tc.Source = analytics.SourceManual
	}
	s.storeRunAnalyticsContext(agentRun, tc)
	return tc
}

func runQueueWaitSeconds(agentRun db.AgentRun) float64 {
	return durationSeconds(agentRun.CreatedAt, agentRun.DispatchedAt)
}

func runRunSeconds(agentRun db.AgentRun) float64 {
	return durationSeconds(agentRun.StartedAt, agentRun.CompletedAt)
}

func runTotalSeconds(agentRun db.AgentRun) float64 {
	return durationSeconds(agentRun.CreatedAt, agentRun.CompletedAt)
}

func durationSeconds(start, end pgtype.Timestamptz) float64 {
	if !start.Valid || !end.Valid {
		return -1
	}
	seconds := end.Time.Sub(start.Time).Seconds()
	if seconds < 0 {
		return 0
	}
	return seconds
}

func runFailureReason(agentRun db.AgentRun) string {
	if agentRun.FailureReason.Valid && agentRun.FailureReason.String != "" {
		return agentRun.FailureReason.String
	}
	return "agent_error"
}

func runErrorType(reason string) string {
	switch reason {
	case "runtime_offline", "runtime_reconnect_timeout", "runtime_recovery":
		return "runtime"
	case "timeout", "codex_semantic_inactivity":
		return "timeout"
	case "iteration_limit", "agent_fallback_message":
		return "agent_output"
	case "cancelled", "user_cancelled":
		return "cancelled"
	default:
		return "agent_error"
	}
}

// EnqueueRunForTask creates a queued task for an agent-assigned task.
// No context snapshot is stored — the agent fetches all data it needs at
// runtime via the silieco CLI.
func (s *AgentRunService) EnqueueRunForTask(ctx context.Context, task db.Task, triggerCommentID ...pgtype.UUID) (db.AgentRun, error) {
	var commentID pgtype.UUID
	if len(triggerCommentID) > 0 {
		commentID = triggerCommentID[0]
	}
	return s.enqueueTaskRun(ctx, task, commentID, false, "", pgtype.UUID{}, pgtype.UUID{})
}

// EnqueueRunForTaskWithHandoff is the assign/promote variant that carries a
// handoff note into the run's opening context (SILI-3375). The note rides a
// dedicated task column; the daemon renders it via the assignment-handoff
// branch. Empty note behaves exactly like EnqueueRunForTask. actorUserID is the
// member who performed the assign/promote and becomes the accountable human for
// the run (SILI-4302 §4); invalid when the caller has no member actor.
func (s *AgentRunService) EnqueueRunForTaskWithHandoff(ctx context.Context, task db.Task, handoffNote string, actorUserID pgtype.UUID) (db.AgentRun, error) {
	return s.enqueueTaskRun(ctx, task, pgtype.UUID{}, false, handoffNote, actorUserID, pgtype.UUID{})
}

// enqueueTaskRun is the shared implementation behind EnqueueRunForTask
// and the manual rerun path. forceFreshSession=true marks the task so the
// daemon claim handler skips the (agent_id, task_id) resume lookup — the
// user already judged the prior output bad, a fresh agent session is the
// expected behavior.
// ResolveTaskReviewSHA returns the head SHA of the commit currently under
// review for an task (the head_sha of its most-relevant linked PR), or the
// empty string when the task has no linked PR. Callers thread this into both
// the reviewer-loop dedup check and the enqueue path so a pending review task
// pinned to an old head does not satisfy a request after HEAD advanced
// (TEN-356). Empty string is the safe default: it makes dedup fall back to the
// pre-TEN-356 (task_id, agent_id) key and leaves the task's context NULL.
//
// The lookup fails soft — any DB error (including "no linked PR") returns "" so
// a transient github-table hiccup can never over-dedup a review out of
// existence; the worst case is the pre-TEN-356 coalescing behavior.
func (s *AgentRunService) ResolveTaskReviewSHA(ctx context.Context, taskID pgtype.UUID) string {
	if !taskID.Valid {
		return ""
	}
	sha, err := s.Queries.GetTaskReviewHeadSha(ctx, taskID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("resolve task review sha failed",
				"task_id", util.UUIDToString(taskID), "error", err)
		}
		return ""
	}
	return sha
}

// headShaText wraps a resolved review SHA into the pgtype.Text the dedup/enqueue
// queries expect. Empty SHA marshals to an invalid (NULL) Text so the queries
// take their fall-back branch.
func headShaText(sha string) pgtype.Text {
	return pgtype.Text{String: sha, Valid: sha != ""}
}

// ResolveTaskReviewSHAParam is ResolveTaskReviewSHA wrapped as the pgtype.Text
// the dedup queries take, so both service- and handler-package call sites can
// key dedup on the reviewed head with a single call (TEN-356).
func (s *AgentRunService) ResolveTaskReviewSHAParam(ctx context.Context, taskID pgtype.UUID) pgtype.Text {
	return headShaText(s.ResolveTaskReviewSHA(ctx, taskID))
}

func (s *AgentRunService) enqueueTaskRun(ctx context.Context, task db.Task, triggerCommentID pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfRunID pgtype.UUID) (db.AgentRun, error) {
	return s.enqueueTaskRunWithCommentPlan(ctx, task, triggerCommentID, nil, forceFreshSession, handoffNote, actorUserID, rerunOfRunID)
}

func (s *AgentRunService) enqueueTaskRunWithCommentPlan(ctx context.Context, task db.Task, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfRunID pgtype.UUID) (db.AgentRun, error) {
	// Resolve attribution and external overlays before holding a DB connection.
	prepared, err := s.prepareTaskRunWithCommentPlan(ctx, task, triggerCommentID, coalescedCommentIDs, forceFreshSession, handoffNote, actorUserID, rerunOfRunID)
	if err != nil {
		return db.AgentRun{}, err
	}

	agentRun, err := s.insertPreparedTaskRun(ctx, task, prepared)
	if err != nil {
		return db.AgentRun{}, err
	}
	slog.Info("task enqueued", "run_id", util.UUIDToString(agentRun.ID), "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(task.AssigneeID), "force_fresh_session", forceFreshSession)
	// Publish before waking the daemon so dispatch cannot precede queued.
	s.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
	s.NotifyRunEnqueued(ctx, agentRun)
	return agentRun, nil
}

// insertPreparedTaskRun serializes unbound task calls with Run mutations.
// Preparation must finish before entering this transaction; explicit mention
// targets need not be the task assignee. Callers publish only after commit.
func (s *AgentRunService) insertPreparedTaskRun(ctx context.Context, task db.Task, prepared db.CreateAgentRunParams) (db.AgentRun, error) {
	return s.insertTaskCall(ctx, task, prepared.AgentID, prepared.RuntimeID, func(q *db.Queries, current db.Task) (db.AgentRun, error) {
		prepared.Priority = priorityToInt(current.Priority)
		return q.CreateAgentRun(ctx, prepared)
	})
}

// insertTaskCall keeps queued and deferred calls under the same Run protocol.
// The callback must use the provided transaction and perform no external reads.
func (s *AgentRunService) insertTaskCall(ctx context.Context, task db.Task, agentID, runtimeID pgtype.UUID, insert func(*db.Queries, db.Task) (db.AgentRun, error)) (db.AgentRun, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.AgentRun{}, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	current, runs, err := LockTaskWorkflowMutation(ctx, q, task.WorkspaceID, task.ID, pgtype.UUID{})
	if err != nil {
		return db.AgentRun{}, err
	}
	if current.WorkflowInstanceID != task.WorkflowInstanceID || current.WorkflowStageID != task.WorkflowStageID || current.AssigneeID != task.AssigneeID || current.AssigneeType != task.AssigneeType {
		return db.AgentRun{}, ErrWorkflowExecutionConflict
	}
	for _, run := range runs {
		if run.ArchivedAt.Valid || run.Status == "completed" || run.Status == "cancelled" {
			return db.AgentRun{}, ErrWorkflowExecutionConflict
		}
	}
	if current.WorkflowInstanceID.Valid {
		stage, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{ID: current.WorkflowStageID, WorkflowInstanceID: current.WorkflowInstanceID, WorkspaceID: current.WorkspaceID})
		if err != nil {
			return db.AgentRun{}, err
		}
		if stage.ExecutionMode != "manual" || current.Status == "done" || current.Status == "cancelled" || current.Status == "backlog" {
			return db.AgentRun{}, ErrWorkflowExecutionConflict
		}
	}
	task = current
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: task.WorkspaceID})
	if err != nil {
		return db.AgentRun{}, err
	}
	if agent.ArchivedAt.Valid || agent.RuntimeID != runtimeID {
		return db.AgentRun{}, ErrWorkflowExecutionConflict
	}
	agentRun, err := insert(q, current)
	if err != nil {
		return db.AgentRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.AgentRun{}, err
	}
	return agentRun, nil
}

// createTaskRunWithCommentPlan inserts through the caller's transaction when
// provided. It performs no enqueue notification; callers notify after commit.
func (s *AgentRunService) createTaskRunWithCommentPlan(ctx context.Context, queries *db.Queries, task db.Task, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfRunID pgtype.UUID) (db.AgentRun, error) {
	params, err := s.prepareTaskRunWithCommentPlan(ctx, task, triggerCommentID, coalescedCommentIDs, forceFreshSession, handoffNote, actorUserID, rerunOfRunID)
	if err != nil {
		return db.AgentRun{}, err
	}
	agentRun, err := queries.CreateAgentRun(ctx, params)
	if err != nil {
		return db.AgentRun{}, fmt.Errorf("create run: %w", err)
	}
	return agentRun, nil
}

func (s *AgentRunService) prepareTaskRunWithCommentPlan(ctx context.Context, task db.Task, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfRunID pgtype.UUID) (db.CreateAgentRunParams, error) {
	if !task.AssigneeID.Valid {
		slog.Error("task enqueue failed", "task_id", util.UUIDToString(task.ID), "error", "task has no assignee")
		return db.CreateAgentRunParams{}, fmt.Errorf("task has no assignee")
	}

	agent, err := s.Queries.GetAgent(ctx, task.AssigneeID)
	if err != nil {
		slog.Error("task enqueue failed", "task_id", util.UUIDToString(task.ID), "error", err)
		return db.CreateAgentRunParams{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("task enqueue skipped: agent is archived", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agent.ID))
		return db.CreateAgentRunParams{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("task enqueue failed", "task_id", util.UUIDToString(task.ID), "error", "agent has no runtime")
		return db.CreateAgentRunParams{}, fmt.Errorf("agent has no runtime")
	}

	// The task assignee reacting to an agent-authored comment is a
	// comment_source attribution (a special case of delegation); a member
	// comment or direct member assignment is direct_human. attr.UserID is the
	// same value the pre-SILI-4302 resolver produced, so overlay/authorization
	// are unchanged; the extra fields are audit provenance.
	attr := s.attributionForTaskRun(ctx, task, triggerCommentID, attribution.SourceCommentSource, actorUserID)
	// No precise human resolved → owner_fallback (accountable = agent owner), or
	// refuse the enqueue if the workspace is fail-closed (SILI-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("task enqueue refused: attribution fail-closed", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(task.AssigneeID))
		return db.CreateAgentRunParams{}, err
	}
	originatorUserID := attr.UserID
	runtimeMCPOverlay := s.buildRuntimeMCPOverlay(ctx, originatorUserID, agent)
	attrSource, attrDelegatedFrom, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	return db.CreateAgentRunParams{
		AgentID:              task.AssigneeID,
		RuntimeID:            agent.RuntimeID,
		TaskID:               task.ID,
		Priority:             priorityToInt(task.Priority),
		TriggerCommentID:     triggerCommentID,
		CoalescedCommentIds:  coalescedCommentIDs,
		TriggerSummary:       s.buildCommentTriggerSummary(ctx, task.WorkspaceID, triggerCommentID),
		ForceFreshSession:    pgtype.Bool{Bool: forceFreshSession, Valid: forceFreshSession},
		HandoffNote:          pgtype.Text{String: handoffNote, Valid: handoffNote != ""},
		OriginatorUserID:     originatorUserID,
		AccountableUserID:    attr.AccountableUserID,
		RuleVersionID:        attr.RuleVersionID,
		RerunOfRunID:         rerunOfRunID,
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		OriginatorSource:     attrSource,
		DelegatedFromRunID:   attrDelegatedFrom,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
		// Stamp the reviewed head so dedup can distinguish this run's target
		// from a later request against a new HEAD (TEN-356).
		HeadSha: headShaText(s.ResolveTaskReviewSHA(ctx, task.ID)),
	}, nil
}

// EnqueueTaskForMention creates a queued task for a mentioned agent on an task.
// Unlike EnqueueRunForTask, this takes an explicit agent ID rather than
// deriving it from the task assignee.
func (s *AgentRunService) EnqueueRunForMention(ctx context.Context, task db.Task, agentID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentRun, error) {
	return s.enqueueMentionRun(ctx, task, agentID, triggerCommentID, false, pgtype.UUID{}, false, "", pgtype.UUID{}, pgtype.UUID{})
}

// EnqueueTaskForThreadParent creates a queued task for the agent who authored
// the direct parent comment a member replied to.
func (s *AgentRunService) EnqueueRunForThreadParent(ctx context.Context, task db.Task, agentID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentRun, error) {
	return s.enqueueMentionRun(ctx, task, agentID, triggerCommentID, false, pgtype.UUID{}, false, "", pgtype.UUID{}, pgtype.UUID{})
}

// EnqueueTaskForSquadLeader is the leader-role variant of EnqueueTaskForMention.
// The resulting task carries is_leader_run=true so that downstream
// self-trigger guards can distinguish a comment posted while the agent was
// acting as the squad's leader (skip) from one posted while it was acting
// as a worker (do not skip). This matters for agents that are simultaneously
// the leader and a worker of the same squad — see migration 090.
//
// squadID is stamped onto the task's squad_id column so the daemon claim
// handler can locate the squad and inject its briefing regardless of how the
// leader task was triggered (comment @squad, task assign, autopilot,
// sub-task done callback). See migration 127.
func (s *AgentRunService) EnqueueRunForSquadLeader(ctx context.Context, task db.Task, leaderID pgtype.UUID, squadID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentRun, error) {
	return s.enqueueMentionRun(ctx, task, leaderID, triggerCommentID, true, squadID, false, "", pgtype.UUID{}, pgtype.UUID{})
}

// EnqueueTaskForSquadLeaderWithHandoff is the assign/promote variant carrying a
// handoff note into the leader run's opening context (SILI-3375). Empty note
// behaves exactly like EnqueueTaskForSquadLeader. actorUserID is the member who
// performed the assign/promote and becomes the accountable human (SILI-4302 §4);
// invalid when the caller has no member actor.
func (s *AgentRunService) EnqueueRunForSquadLeaderWithHandoff(ctx context.Context, task db.Task, leaderID pgtype.UUID, squadID pgtype.UUID, handoffNote string, actorUserID pgtype.UUID) (db.AgentRun, error) {
	return s.enqueueMentionRun(ctx, task, leaderID, pgtype.UUID{}, true, squadID, false, handoffNote, actorUserID, pgtype.UUID{})
}

func (s *AgentRunService) enqueueMentionRun(ctx context.Context, task db.Task, agentID pgtype.UUID, triggerCommentID pgtype.UUID, isLeader bool, squadID pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfRunID pgtype.UUID) (db.AgentRun, error) {
	return s.enqueueMentionRunWithCommentPlan(ctx, task, agentID, triggerCommentID, nil, isLeader, squadID, forceFreshSession, handoffNote, actorUserID, rerunOfRunID)
}

func (s *AgentRunService) enqueueMentionRunWithCommentPlan(ctx context.Context, task db.Task, agentID pgtype.UUID, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, isLeader bool, squadID pgtype.UUID, forceFreshSession bool, handoffNote string, actorUserID pgtype.UUID, rerunOfRunID pgtype.UUID) (db.AgentRun, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("mention task enqueue failed: agent not found", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentRun{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("mention task enqueue skipped: agent is archived", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentRun{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("mention task enqueue failed: agent has no runtime", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentRun{}, fmt.Errorf("agent has no runtime")
	}

	// An explicit mention / thread-parent / squad-leader hop from an
	// agent-authored comment is a delegation (the parent task's human is
	// copied); a member mention is direct_human. attr.UserID matches the
	// pre-SILI-4302 value, so authorization is unchanged.
	attr := s.attributionForTaskRun(ctx, task, triggerCommentID, attribution.SourceDelegation, actorUserID)
	// No precise human resolved → owner_fallback (accountable = agent owner), or
	// refuse the enqueue if the workspace is fail-closed (SILI-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("mention task enqueue refused: attribution fail-closed", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentRun{}, err
	}
	originatorUserID := attr.UserID
	runtimeMCPOverlay := s.buildRuntimeMCPOverlay(ctx, originatorUserID, agent)
	attrSource, attrDelegatedFrom, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	agentRun, err := s.insertPreparedTaskRun(ctx, task, db.CreateAgentRunParams{
		AgentID:              agentID,
		RuntimeID:            agent.RuntimeID,
		TaskID:               task.ID,
		Priority:             priorityToInt(task.Priority),
		TriggerCommentID:     triggerCommentID,
		CoalescedCommentIds:  coalescedCommentIDs,
		TriggerSummary:       s.buildCommentTriggerSummary(ctx, task.WorkspaceID, triggerCommentID),
		IsLeaderRun:          pgtype.Bool{Bool: isLeader, Valid: isLeader},
		ForceFreshSession:    pgtype.Bool{Bool: forceFreshSession, Valid: forceFreshSession},
		HandoffNote:          pgtype.Text{String: handoffNote, Valid: handoffNote != ""},
		SquadID:              squadID,
		OriginatorUserID:     originatorUserID,
		AccountableUserID:    attr.AccountableUserID,
		RuleVersionID:        attr.RuleVersionID,
		RerunOfRunID:         rerunOfRunID,
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		OriginatorSource:     attrSource,
		DelegatedFromRunID:   attrDelegatedFrom,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
		// Stamp the reviewed head so dedup can distinguish this run's target
		// from a later request against a new HEAD (TEN-356).
		HeadSha: headShaText(s.ResolveTaskReviewSHA(ctx, task.ID)),
	})
	if err != nil {
		// A concurrent enqueue for the same (task, agent) won the race and the
		// unique index rejected this insert. That is benign — a sibling run
		// already covers this target — so log it at debug and return a typed
		// sentinel the caller maps to a coalesced outcome / 409 rather than a
		// 500 that leaks the raw constraint name (#5914).
		if isDuplicatePendingRunErr(err) {
			slog.Debug("mention task enqueue coalesced: pending task already exists", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))
			return db.AgentRun{}, ErrDuplicatePendingRun
		}
		slog.Error("mention task enqueue failed", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentRun{}, fmt.Errorf("create run: %w", err)
	}

	slog.Info("mention task enqueued", "run_id", util.UUIDToString(agentRun.ID), "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID), "is_leader_run", isLeader)
	// See EnqueueRunForTask for ordering rationale.
	s.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
	s.NotifyRunEnqueued(ctx, agentRun)
	return agentRun, nil
}

// EnqueueDeferredAssigneeFallback creates an inert task that becomes claimable
// only after PromoteDueDeferredTasksForRuntime flips it from deferred to queued.
func (s *AgentRunService) EnqueueDeferredAssigneeFallback(ctx context.Context, task db.Task, agentID, squadID pgtype.UUID, escalationForRunID pgtype.UUID, triggerCommentID pgtype.UUID, fireAt time.Time) (db.AgentRun, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("deferred fallback enqueue failed: agent not found", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentRun{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("deferred fallback enqueue skipped: agent is archived", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentRun{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("deferred fallback enqueue failed: agent has no runtime", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentRun{}, fmt.Errorf("agent has no runtime")
	}

	// The fallback assignee is reacting to the same trigger comment as the primary
	// routed task, so resolve attribution from that comment (member author →
	// direct_human; agent author → comment_source chain) and stamp it at creation.
	// Promotion later only flips status, so stamping here keeps the eventual run
	// off the NULL-source bypass (SILI-4302 §2). Overlay is intentionally left for
	// the existing promotion path — this change is attribution-only. No direct
	// actor here: the fallback is comment-routed, so attribution rides the comment.
	attr := s.attributionForTaskRun(ctx, task, triggerCommentID, attribution.SourceCommentSource, pgtype.UUID{})
	// No precise human resolved → owner_fallback (accountable = agent owner), or
	// refuse if the workspace is fail-closed (SILI-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("deferred fallback enqueue refused: attribution fail-closed", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentRun{}, err
	}
	attrSource, attrDelegatedFrom, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	isLeader := squadID.Valid
	prepared := db.CreateDeferredAgentRunParams{
		AgentID:              agentID,
		RuntimeID:            agent.RuntimeID,
		TaskID:               task.ID,
		Priority:             priorityToInt(task.Priority),
		TriggerCommentID:     triggerCommentID,
		TriggerSummary:       s.buildCommentTriggerSummary(ctx, task.WorkspaceID, triggerCommentID),
		IsLeaderRun:          pgtype.Bool{Bool: isLeader, Valid: isLeader},
		SquadID:              squadID,
		EscalationForRunID:   escalationForRunID,
		FireAt:               pgtype.Timestamptz{Time: fireAt, Valid: true},
		OriginatorUserID:     attr.UserID,
		AccountableUserID:    attr.AccountableUserID,
		OriginatorSource:     attrSource,
		DelegatedFromRunID:   attrDelegatedFrom,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
	}
	agentRun, err := s.insertTaskCall(ctx, task, agentID, agent.RuntimeID, func(q *db.Queries, current db.Task) (db.AgentRun, error) {
		prepared.Priority = priorityToInt(current.Priority)
		return q.CreateDeferredAgentRun(ctx, prepared)
	})
	if err != nil {
		slog.Error("deferred fallback enqueue failed", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentRun{}, fmt.Errorf("create deferred run: %w", err)
	}

	slog.Info("deferred fallback task enqueued",
		"run_id", util.UUIDToString(agentRun.ID),
		"task_id", util.UUIDToString(task.ID),
		"agent_id", util.UUIDToString(agentID),
		"fire_at", fireAt.UTC().Format(time.RFC3339),
	)
	return agentRun, nil
}

// QuickCreateContext is the JSON payload stored on a quick-create task's
// context column. The daemon detects this variant via Type == "quick_create"
// and switches to the quick-create prompt template; the completion path
// uses RequesterID + WorkspaceID to write the inbox notification.
//
// ProjectID is the optional project the user picked in the modal. When
// non-empty the daemon claim handler resolves the project's title +
// resources, and the prompt template instructs the agent to pass
// `--project <uuid>` so the new task lands in that project.
//
// SquadID is non-empty when the user picked a squad (rather than an agent)
// in the modal. The task is still enqueued against the squad's leader
// agent (Queries.CreateQuickCreateTask is agent-scoped); SquadID is the
// hint the daemon claim handler uses to layer the squad-leader briefing
// onto the agent's Instructions, matching the behavior of task-bound
// tasks assigned to the squad.
type QuickCreateContext struct {
	Type               string   `json:"type"`
	Prompt             string   `json:"prompt"`
	RequesterID        string   `json:"requester_id"`
	WorkspaceID        string   `json:"workspace_id"`
	Priority           string   `json:"priority,omitempty"`
	DueDate            string   `json:"due_date,omitempty"`
	ProjectID          string   `json:"project_id,omitempty"`
	WorkflowInstanceID string   `json:"workflow_instance_id,omitempty"`
	WorkflowStageID    string   `json:"workflow_stage_id,omitempty"`
	SquadID            string   `json:"squad_id,omitempty"`
	AttachmentIDs      []string `json:"attachment_ids,omitempty"`
	// ParentTaskID is the optional UUID of the parent task the new task
	// should be filed under. Set when the user opens the modal from "Add
	// sub task" on an existing task; the daemon claim handler resolves the
	// parent's identifier and the prompt template instructs the agent to
	// pass `--parent <uuid>` so the sub-task relationship is preserved
	// across the manual→agent mode flip.
	ParentTaskID string `json:"parent_task_id,omitempty"`
}

// QuickCreateContextType marks a task as a quick-create job.
const QuickCreateContextType = "quick_create"

// EnqueueQuickCreateTask creates a queued task that has no task / chat /
// autopilot link — the user's natural-language prompt is stored in the
// task's context JSONB and the agent is expected to translate it into a
// `silieco task create` call. Pre-validates that the agent is reachable
// (not archived, has a runtime) so the API can reject up-front rather than
// queue a task no one will ever claim.
//
// projectID is optional (zero-valued pgtype.UUID when the user didn't pick
// one). The handler is responsible for validating it belongs to the same
// workspace before passing it in.
//
// squadID is non-empty (Valid) when the user picked a squad as the actor.
// The handler has already resolved it to the squad's leader agent for
// agentID; the squadID hint is stamped into the task context so the daemon
// claim handler can inject the squad-leader briefing on dispatch.
//
// parentTaskID is optional (zero-valued pgtype.UUID when the user didn't
// open the modal from "Add sub task"). The handler is responsible for
// validating it belongs to the same workspace before passing it in.
func (s *AgentRunService) EnqueueQuickCreateRun(
	ctx context.Context,
	workspaceID, requesterID pgtype.UUID,
	agentID, squadID pgtype.UUID,
	prompt, priority, dueDate string,
	projectID, parentTaskID, workflowInstanceID, workflowStageID pgtype.UUID,
	attachmentIDs []pgtype.UUID,
) (db.AgentRun, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return db.AgentRun{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return db.AgentRun{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return db.AgentRun{}, fmt.Errorf("agent has no runtime")
	}

	payload := QuickCreateContext{
		Type:        QuickCreateContextType,
		Prompt:      prompt,
		RequesterID: util.UUIDToString(requesterID),
		WorkspaceID: util.UUIDToString(workspaceID),
		Priority:    priority,
		DueDate:     dueDate,
	}
	if projectID.Valid {
		payload.ProjectID = util.UUIDToString(projectID)
	}
	if workflowInstanceID.Valid {
		payload.WorkflowInstanceID = util.UUIDToString(workflowInstanceID)
		payload.WorkflowStageID = util.UUIDToString(workflowStageID)
	}
	if squadID.Valid {
		payload.SquadID = util.UUIDToString(squadID)
	}
	if parentTaskID.Valid {
		payload.ParentTaskID = util.UUIDToString(parentTaskID)
	}
	if len(attachmentIDs) > 0 {
		payload.AttachmentIDs = make([]string, 0, len(attachmentIDs))
		for _, id := range attachmentIDs {
			if id.Valid {
				payload.AttachmentIDs = append(payload.AttachmentIDs, util.UUIDToString(id))
			}
		}
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return db.AgentRun{}, fmt.Errorf("marshal quick-create context: %w", err)
	}

	// The requester who submitted the quick-create modal is the direct_human
	// originator and accountable. Quick-create is the ONE enqueue path with no
	// antecedent row to point the uniform evidence pair at: the run's whole job is
	// to CREATE the task, so at enqueue time there is no comment / task / session
	// / run to reference (the task is linked back later via LinkRunToTask).
	// Evidence is therefore intentionally NULL; the accountable human is captured on
	// originator/accountable_user_id, so this is not a NULL-source bypass — source
	// is still stamped direct_human (SILI-4302 §2).
	attr := attribution.DirectHumanRun(requesterID, "", pgtype.UUID{})
	// An unresolved requester degrades to owner_fallback (accountable = agent
	// owner), or is refused if the workspace is fail-closed (SILI-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		return db.AgentRun{}, err
	}
	attrSource, _, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	runtimeMCPOverlay := s.buildRuntimeMCPOverlay(ctx, requesterID, agent)
	agentRun, err := s.Queries.CreateQuickCreateRun(ctx, db.CreateQuickCreateRunParams{
		AgentID:              agentID,
		RuntimeID:            agent.RuntimeID,
		Priority:             priorityToInt("high"),
		Context:              contextJSON,
		OriginatorUserID:     requesterID,
		AccountableUserID:    attr.AccountableUserID,
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		OriginatorSource:     attrSource,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
	})
	if err != nil {
		return db.AgentRun{}, fmt.Errorf("create quick-create run: %w", err)
	}

	slog.Info("quick-create task enqueued",
		"run_id", util.UUIDToString(agentRun.ID),
		"agent_id", util.UUIDToString(agentID),
		"squad_id", payload.SquadID,
		"requester_id", util.UUIDToString(requesterID),
		"workspace_id", util.UUIDToString(workspaceID),
		"project_id", payload.ProjectID,
		"workflow_instance_id", payload.WorkflowInstanceID,
		"workflow_stage_id", payload.WorkflowStageID,
		"parent_task_id", payload.ParentTaskID,
	)
	// Match every other Enqueue* path: kick the daemon WS so the task
	// gets claimed promptly instead of waiting for the next 30 s poll
	// cycle. Without this the user perceives "quick create never
	// triggered" because the modal closes immediately and the task
	// sits in 'queued' until the next sleepWithContextOrWakeup tick.
	s.NotifyRunEnqueued(ctx, agentRun)
	return agentRun, nil
}

// ErrChatTaskAgentArchived signals that EnqueueChatTask refused to
// queue work because the destination agent has been archived. This
// is a productizable state — surface it to the user as "this agent
// has been archived" rather than retrying.
var ErrChatRunAgentArchived = errors.New("chat run: agent archived")

// ErrChatTaskAgentNoRuntime signals that EnqueueChatTask refused to
// queue work because the agent has never been associated with a
// runtime (agent.runtime_id IS NULL). This is the "agent has no
// daemon configured" case — productizable as "agent offline".
//
// IMPORTANT: this is NOT the same as "the daemon is currently
// disconnected". When agent.runtime_id IS set, EnqueueChatTask
// enqueues the task and the daemon claims it on next online; that
// path returns a task row, not this error.
var ErrChatRunAgentNoRuntime = errors.New("chat run: agent has no runtime")

// EnqueueChatTask creates a task-owned input batch for a chat session. Channel
// media makes the task deferred until binding completes or its durable fallback
// deadline expires; other chat tasks are queued immediately. Unlike task
// tasks, chat tasks have no task_id.
//
// Errors split into two layers:
//
//   - Productizable rejections (agent archived, no runtime) return
//     the sentinel errors above. Callers (e.g. the Lark dispatcher)
//     can errors.Is them to decide a user-visible outcome.
//
//   - Infrastructure failures (DB load / insert errors) are wrapped
//     as ordinary errors. The caller should treat them as retryable
//     or page-worthy, NOT as user-facing state.
//
// initiatorUserID is the user who actually sent the triggering message — the
// real requester behind this run. Callers pass it explicitly because
// chat_session.creator_id is not a reliable source: Lark group sessions set the
// creator to the installer, not the sender (see the lark dispatcher). Web chat
// passes the request user; the lark dispatcher passes the inbound sender of the
// latest message in the silence window. Stored on the task so the daemon brief
// can attribute the run to the right person. See SILI-2645.
//
// forceFreshSession applies only to the task created by this call. The daemon
// uses it to skip prior chat-session resume for this dispatch without clearing
// the chat session's stored resume pointer for future normal messages.
func (s *AgentRunService) EnqueueChatRun(ctx context.Context, chatSession db.ChatSession, initiatorUserID pgtype.UUID, forceFreshSession bool) (db.AgentRun, error) {
	agent, err := s.Queries.GetAgent(ctx, chatSession.AgentID)
	if err != nil {
		slog.Error("chat task enqueue failed", "chat_session_id", util.UUIDToString(chatSession.ID), "error", err)
		return db.AgentRun{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return db.AgentRun{}, ErrChatRunAgentArchived
	}
	if !agent.RuntimeID.Valid {
		return db.AgentRun{}, ErrChatRunAgentNoRuntime
	}

	// The chat sender (initiator) is the direct_human originator and accountable.
	// Evidence uses the uniform pair (kind=chat, ref=chat_session_id) so the
	// attribution UI links to the conversation the same way it does for
	// autopilot_run / task_assignment — the dedicated chat_session_id column still
	// exists for its own consumers. An unresolved sender (some Lark group messages)
	// degrades to unattributed rather than a NULL-source bypass (SILI-4302 §2).
	attr := attribution.DirectHumanRun(initiatorUserID, attribution.EvidenceChat, chatSession.ID)
	// An unresolved sender degrades to owner_fallback (accountable = agent owner),
	// or is refused if the workspace is fail-closed (SILI-4302 §3.5).
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		slog.Warn("chat task enqueue refused: attribution fail-closed", "chat_session_id", util.UUIDToString(chatSession.ID))
		return db.AgentRun{}, err
	}
	attrSource, _, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	runtimeMCPOverlay := s.buildRuntimeMCPOverlay(ctx, initiatorUserID, agent)

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.AgentRun{}, fmt.Errorf("begin chat task enqueue: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	mediaPendingUntil, err := qtx.GetChannelMediaPendingUntil(ctx, chatSession.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("chat task enqueue failed", "chat_session_id", util.UUIDToString(chatSession.ID), "error", err)
		return db.AgentRun{}, fmt.Errorf("load channel media pending deadline: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		mediaPendingUntil = pgtype.Timestamptz{}
	}
	agentRun, err := qtx.CreateChatRun(ctx, db.CreateChatRunParams{
		AgentID:           chatSession.AgentID,
		RuntimeID:         agent.RuntimeID,
		Priority:          2, // medium priority for chat
		ChatSessionID:     chatSession.ID,
		InitiatorUserID:   initiatorUserID,
		FireAt:            mediaPendingUntil,
		OriginatorUserID:  initiatorUserID,
		AccountableUserID: attr.AccountableUserID,
		ForceFreshSession: pgtype.Bool{
			Bool:  forceFreshSession,
			Valid: true,
		},
		RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
		RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		OriginatorSource:     attrSource,
		TriggerEvidenceKind:  attrEvidenceKind,
		TriggerEvidenceRefID: attrEvidenceRef,
	})
	if err != nil {
		slog.Error("chat task enqueue failed", "chat_session_id", util.UUIDToString(chatSession.ID), "error", err)
		return db.AgentRun{}, fmt.Errorf("create chat run: %w", err)
	}
	agentRun, err = qtx.SetChatRunInputOwnerSelf(ctx, agentRun.ID)
	if err != nil {
		return db.AgentRun{}, fmt.Errorf("set channel chat task input owner: %w", err)
	}
	if err := qtx.LinkUnownedChannelChatMessagesToRun(ctx, db.LinkUnownedChannelChatMessagesToRunParams{
		RunID:         agentRun.ID,
		ChatSessionID: chatSession.ID,
	}); err != nil {
		return db.AgentRun{}, fmt.Errorf("seal channel chat task input: %w", err)
	}
	// The deadline read above ran before the seal; under READ COMMITTED a media
	// message committed between the two statements is sealed into this task
	// without deferring it, and the daemon could claim it before its media
	// binds. Re-derive the deferral from the sealed batch itself.
	switch corrected, err := qtx.DeferChatRunForSealedPendingMedia(ctx, agentRun.ID); {
	case err == nil:
		agentRun = corrected
	case !errors.Is(err, pgx.ErrNoRows):
		return db.AgentRun{}, fmt.Errorf("defer chat task for sealed pending media: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.AgentRun{}, fmt.Errorf("commit chat task enqueue: %w", err)
	}

	if agentRun.Status == "deferred" {
		slog.Info("chat task deferred for channel media",
			"run_id", util.UUIDToString(agentRun.ID),
			"chat_session_id", util.UUIDToString(chatSession.ID),
			"agent_id", util.UUIDToString(chatSession.AgentID),
			"fire_at", agentRun.FireAt.Time,
		)
		// Fence the clear-vs-create race: media completion may have found no
		// committed task after our deadline read. Re-check after commit so the
		// task is promoted immediately when the marker has already cleared.
		// The task is already durably committed, so a fence failure must not
		// surface as an enqueue failure — the router would treat it as "no
		// task" and clear the typing indicator while the run still happens.
		// The claim-path deferred promoter re-queues it at fire_at regardless.
		if err := s.PromoteChannelChatRunsIfMediaReady(ctx, chatSession.ID); err != nil {
			slog.Warn("chat task media-ready fence failed; deferred task falls back to its deadline",
				"run_id", util.UUIDToString(agentRun.ID),
				"chat_session_id", util.UUIDToString(chatSession.ID),
				"error", err)
		}
		return agentRun, nil
	}

	slog.Info("chat task enqueued", "run_id", util.UUIDToString(agentRun.ID), "chat_session_id", util.UUIDToString(chatSession.ID), "agent_id", util.UUIDToString(chatSession.AgentID))
	// See EnqueueRunForTask for ordering rationale.
	s.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
	s.NotifyRunEnqueued(ctx, agentRun)
	return agentRun, nil
}

// PromoteChannelChatTasksIfMediaReady queues channel tasks as soon as every
// unexpired media marker in the session has been cleared. If the process dies
// first, the normal deferred-task promoter queues them at their persisted
// fire_at deadline, preserving the placeholder fallback across restarts.
func (s *AgentRunService) PromoteChannelChatRunsIfMediaReady(ctx context.Context, sessionID pgtype.UUID) error {
	agentRuns, err := s.Queries.PromoteChannelChatRunsIfMediaReady(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("promote channel chat tasks after media: %w", err)
	}
	for _, agentRun := range agentRuns {
		slog.Info("channel media-ready chat task promoted",
			"run_id", util.UUIDToString(agentRun.ID),
			"chat_session_id", util.UUIDToString(sessionID),
			"agent_id", util.UUIDToString(agentRun.AgentID),
		)
		s.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
		s.NotifyRunEnqueued(ctx, agentRun)
	}
	return nil
}

// DirectChatSendResult carries the rows a transactional direct-chat send
// persisted, so the handler can broadcast the user message and shape its
// response without re-reading them.
type DirectChatSendResult struct {
	AgentRun           db.AgentRun
	Message            db.ChatMessage
	BoundAttachmentIDs []pgtype.UUID
}

// GroupChatSendResult carries one persisted group message and the zero or more
// agent runs created by explicit @mentions in that message.
type GroupChatSendResult struct {
	Runs               []db.AgentRun
	Message            db.ChatMessage
	BoundAttachmentIDs []pgtype.UUID
}

type preparedGroupChatTarget struct {
	agent        db.Agent
	overlay      runtimeMCPOverlayData
	attr         attribution.Result
	source       pgtype.Text
	evidenceKind pgtype.Text
	evidenceRef  pgtype.UUID
}

// SendGroupChatMessage atomically appends a human group message and enqueues a
// fresh task for every explicitly mentioned agent. A message with no mentioned
// agents is persisted without creating work. Group runs deliberately start a
// fresh provider session: their durable context is the speaker-labelled group
// transcript assembled by Core at claim time, so sessions from different
// agents can never overwrite or inherit each other's provider memory.
func (s *AgentRunService) SendGroupChatMessage(
	ctx context.Context,
	session db.ChatSession,
	initiatorUserID pgtype.UUID,
	content string,
	attachmentIDs []pgtype.UUID,
	uploaderType string,
	uploaderID pgtype.UUID,
	agents []db.Agent,
) (*GroupChatSendResult, error) {
	prepared := make([]preparedGroupChatTarget, 0, len(agents))
	for _, agent := range agents {
		overlay := s.buildRuntimeMCPOverlay(ctx, initiatorUserID, agent)
		attr := attribution.DirectHumanRun(initiatorUserID, attribution.EvidenceChat, session.ID)
		resolved, err := s.applyAttributionFallback(ctx, attr, agent)
		if err != nil {
			return nil, err
		}
		source, _, evidenceKind, evidenceRef := attributionCreateParams(resolved)
		prepared = append(prepared, preparedGroupChatTarget{
			agent: agent, overlay: overlay, attr: resolved, source: source,
			evidenceKind: evidenceKind, evidenceRef: evidenceRef,
		})
	}

	out := GroupChatSendResult{Runs: make([]db.AgentRun, 0, len(prepared))}
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if _, err := qtx.LockChatSessionForRuntimeBind(ctx, session.ID); err != nil {
			return fmt.Errorf("lock group chat session: %w", err)
		}

		mentionedAgentIDs := make([]pgtype.UUID, 0, len(prepared))
		for _, target := range prepared {
			mentionedAgentIDs = append(mentionedAgentIDs, target.agent.ID)
		}
		msg, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ChatSessionID:     session.ID,
			Role:              "user",
			Content:           content,
			SenderUserID:      initiatorUserID,
			MentionedAgentIds: mentionedAgentIDs,
		})
		if err != nil {
			return fmt.Errorf("create group chat message: %w", err)
		}
		out.Message = msg

		if len(attachmentIDs) > 0 {
			bound, err := qtx.LinkAttachmentsToChatMessage(ctx, db.LinkAttachmentsToChatMessageParams{
				ChatMessageID: msg.ID,
				ChatSessionID: session.ID,
				WorkspaceID:   session.WorkspaceID,
				UploaderType:  uploaderType,
				UploaderID:    uploaderID,
				AttachmentIds: attachmentIDs,
			})
			if err != nil {
				return fmt.Errorf("link group chat attachments: %w", err)
			}
			out.BoundAttachmentIDs = bound
		}

		for _, target := range prepared {
			agentRun, err := qtx.CreateGroupChatRun(ctx, db.CreateGroupChatRunParams{
				AgentID:              target.agent.ID,
				RuntimeID:            target.agent.RuntimeID,
				ChatSessionID:        session.ID,
				InitiatorUserID:      initiatorUserID,
				OriginatorUserID:     target.attr.UserID,
				AccountableUserID:    target.attr.AccountableUserID,
				RuntimeMcpOverlay:    target.overlay.Overlay,
				RuntimeConnectedApps: target.overlay.ConnectedApps,
				OriginatorSource:     target.source,
				TriggerEvidenceKind:  target.evidenceKind,
				TriggerEvidenceRefID: target.evidenceRef,
				TriggerChatMessageID: msg.ID,
			})
			if err != nil {
				return fmt.Errorf("create group chat run: %w", err)
			}
			out.Runs = append(out.Runs, agentRun)
		}

		if err := qtx.TouchChatSession(ctx, session.ID); err != nil {
			return fmt.Errorf("touch group chat session: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	for _, agentRun := range out.Runs {
		s.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
		s.NotifyRunEnqueued(ctx, agentRun)
	}
	return &out, nil
}

// SendDirectChatMessage atomically persists one web/mobile direct-chat turn:
// the owning task (which claims its own input batch via chat_input_run_id), the
// user message bound to that task, any attachment bindings, and the session
// touch all commit together (SILI-4351). The daemon is only notified after the
// commit, so it can never observe a message without a task or a task without its
// input owner — and a later claim reads exactly this task's user messages
// instead of scanning trailing history.
//
// The caller must have already gated the session and preflighted the agent
// (archived / no-runtime), passing the loaded agent in; this method trusts those
// permission checks. It does NOT trust the agent's runtime_id: that field is
// re-read inside the transaction (see below).
func (s *AgentRunService) SendDirectChatMessage(ctx context.Context, session db.ChatSession, agent db.Agent, initiatorUserID pgtype.UUID, content string, attachmentIDs []pgtype.UUID, uploaderType string, uploaderID pgtype.UUID) (*DirectChatSendResult, error) {
	// Build the per-task Composio overlay before the transaction — it can do
	// network I/O and must not run with a DB transaction open.
	overlay := s.buildRuntimeMCPOverlay(ctx, initiatorUserID, agent)

	// Full attribution for the chat sender, resolved before the tx (the policy read
	// + fallback must not run with a transaction open) — the same direct_human stamp
	// EnqueueChatTask writes. Without this the direct-chat path was a bypass: it set
	// originator_user_id but left accountable_user_id / source / evidence NULL,
	// violating the one-way invariant and dropping the audit source (SILI-4302 §2).
	attr := attribution.DirectHumanRun(initiatorUserID, attribution.EvidenceChat, session.ID)
	attr, err := s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		return nil, err
	}
	attrSource, _, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)

	var out DirectChatSendResult
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		// Serialise this send against a concurrent runtime rebind of the same
		// session (SILI-5163). The lock must be taken first and the agent re-read
		// under it: the runtime_id the caller loaded can already be stale by the
		// time we get here, and a send blocked behind a rebind would otherwise
		// resume and stamp its task with the runtime the switch just moved away
		// from — leaving the user with a "switched" confirmation and a reply
		// running on the old runtime. Locking chat_session first also matches the
		// delete path's lock order, so the two cannot deadlock.
		if _, err := qtx.LockChatSessionForRuntimeBind(ctx, session.ID); err != nil {
			return fmt.Errorf("lock chat session: %w", err)
		}
		carrier, err := qtx.GetAgent(ctx, session.AgentID)
		if err != nil {
			return fmt.Errorf("reload chat agent: %w", err)
		}
		if !carrier.RuntimeID.Valid {
			return ErrChatRunAgentNoRuntime
		}

		agentRun, err := qtx.CreateChatRun(ctx, db.CreateChatRunParams{
			AgentID:              session.AgentID,
			RuntimeID:            carrier.RuntimeID,
			Priority:             2, // medium priority for chat; matches EnqueueChatTask
			ChatSessionID:        session.ID,
			InitiatorUserID:      initiatorUserID,
			OriginatorUserID:     attr.UserID,
			AccountableUserID:    attr.AccountableUserID,
			ForceFreshSession:    pgtype.Bool{Bool: false, Valid: true},
			RuntimeMcpOverlay:    overlay.Overlay,
			RuntimeConnectedApps: overlay.ConnectedApps,
			OriginatorSource:     attrSource,
			TriggerEvidenceKind:  attrEvidenceKind,
			TriggerEvidenceRefID: attrEvidenceRef,
		})
		if err != nil {
			return fmt.Errorf("create direct chat run: %w", err)
		}
		// Claim this task's own input batch (chat_input_run_id = id) in the same
		// transaction, before the user message is written with run_id = task.id.
		agentRun, err = qtx.SetChatRunInputOwnerSelf(ctx, agentRun.ID)
		if err != nil {
			return fmt.Errorf("stamp direct chat input owner: %w", err)
		}
		out.AgentRun = agentRun

		// Create the user message already owned by this task (run_id = task.id),
		// so it belongs to this immutable input batch the instant it exists.
		msg, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ChatSessionID: session.ID,
			Role:          "user",
			Content:       content,
			RunID:         agentRun.ID,
			SenderUserID:  initiatorUserID,
		})
		if err != nil {
			return fmt.Errorf("create user chat message: %w", err)
		}
		out.Message = msg

		if len(attachmentIDs) > 0 {
			bound, err := qtx.LinkAttachmentsToChatMessage(ctx, db.LinkAttachmentsToChatMessageParams{
				ChatMessageID: msg.ID,
				ChatSessionID: session.ID,
				WorkspaceID:   session.WorkspaceID,
				UploaderType:  uploaderType,
				UploaderID:    uploaderID,
				AttachmentIds: attachmentIDs,
			})
			if err != nil {
				return fmt.Errorf("link chat attachments: %w", err)
			}
			out.BoundAttachmentIDs = bound
		}

		if err := qtx.TouchChatSession(ctx, session.ID); err != nil {
			return fmt.Errorf("touch chat session: %w", err)
		}
		return nil
	}); err != nil {
		slog.Error("direct chat send failed",
			"chat_session_id", util.UUIDToString(session.ID),
			"agent_id", util.UUIDToString(session.AgentID),
			"error", err)
		return nil, err
	}

	slog.Info("direct chat task enqueued",
		"run_id", util.UUIDToString(out.AgentRun.ID),
		"chat_session_id", util.UUIDToString(session.ID),
		"agent_id", util.UUIDToString(session.AgentID))
	// Notify only after commit. See EnqueueRunForTask for ordering rationale.
	s.broadcastRunEvent(ctx, protocol.EventRunQueued, out.AgentRun)
	s.NotifyRunEnqueued(ctx, out.AgentRun)
	return &out, nil
}

// CancelRunsForTask cancels every active task on the task, reconciles each
// affected agent's status, and broadcasts run:cancelled events so frontends
// clear their live cards.
//
// Callers are explicit task-lifecycle cleanup paths only — DeleteTask and
// BatchDeleteTasks, where the owning task row is going away so its tasks
// must not be left orphaned. A plain status flip, `cancelled` included, no
// longer routes here (SILI-4465): cancelling an task is not an implicit "stop
// all runs" switch. Do not re-add a status-driven caller.
//
// Before #1587 this path was "cancel rows and return", which left each affected
// agent stuck at status="working" indefinitely, requiring a manual
// `silieco agent update <id> --status idle` to unwedge. It now reconciles agent
// status and broadcasts run:cancelled, matching CancelTask and RerunTask.
func (s *AgentRunService) CancelRunsForTask(ctx context.Context, taskID pgtype.UUID) error {
	cancelled, err := s.Queries.CancelAgentRunsByTask(ctx, taskID)
	if err != nil {
		return err
	}
	for _, t := range cancelled {
		s.captureRunCancelled(ctx, t)
		s.ReconcileAgentStatus(ctx, t.AgentID)
		s.broadcastRunEvent(ctx, protocol.EventRunCancelled, t)
	}
	s.notifyRunsFinished(cancelled)
	return nil
}

// CancelTasksForAgent cancels every active task belonging to an agent
// (queued + dispatched + running), reconciles the agent's status, and
// broadcasts run:cancelled events. Used by the agent-level "Cancel all
// tasks" action — same shape as CancelRunsForTask but scoped on agent_id.
//
// Returns the cancelled rows so callers can report counts / log them.
func (s *AgentRunService) CancelRunsForAgent(ctx context.Context, agentID pgtype.UUID) ([]db.AgentRun, error) {
	cancelled, err := s.Queries.CancelAgentRunsByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, t := range cancelled {
		s.captureRunCancelled(ctx, t)
		s.broadcastRunEvent(ctx, protocol.EventRunCancelled, t)
	}
	// Reconcile once after the loop — agent transitions from
	// working→available based on remaining task counts, no need to call
	// per row (the rows we just cancelled all belong to the same agent).
	s.ReconcileAgentStatus(ctx, agentID)
	s.notifyRunsFinished(cancelled)
	return cancelled, nil
}

// CancelTasksByTriggerComment cancels active tasks whose planned comment batch
// contains the given edited/deleted comment. The historical method name is
// retained for call-site stability. It must run before deletion clears the
// trigger FK; the returned rows let the handler re-route every surviving input.
func (s *AgentRunService) CancelRunsByTriggerComment(ctx context.Context, commentID pgtype.UUID) ([]db.AgentRun, error) {
	cancelled, err := s.Queries.CancelAgentRunsByTriggerComment(ctx, commentID)
	if err != nil {
		return nil, err
	}
	for _, t := range cancelled {
		s.captureRunCancelled(ctx, t)
		s.ReconcileAgentStatus(ctx, t.AgentID)
		s.broadcastRunEvent(ctx, protocol.EventRunCancelled, t)
	}
	s.notifyRunsFinished(cancelled)
	return cancelled, nil
}

// BroadcastCancelledTasks reconciles each affected agent's status and emits
// run:cancelled for every row. Callers must invoke this AFTER committing the
// cancellation so subscribers don't observe a "cancelled" event for a row
// that the tx might still roll back.
func (s *AgentRunService) BroadcastCancelledRuns(ctx context.Context, cancelled []db.AgentRun) {
	for _, t := range cancelled {
		s.captureRunCancelled(ctx, t)
		s.ReconcileAgentStatus(ctx, t.AgentID)
		s.broadcastRunEvent(ctx, protocol.EventRunCancelled, t)
	}
	s.notifyRunsFinished(cancelled)
}

func (s *AgentRunService) CaptureCancelledRuns(ctx context.Context, cancelled []db.AgentRun) {
	for _, t := range cancelled {
		s.captureRunCancelled(ctx, t)
	}
}

type CancelledChatMessageResult struct {
	ChatSessionID  string
	MessageID      string
	Content        string
	RestoreToInput bool
	// Attachments are the rows detached from the deleted user message so they
	// survive the ON DELETE CASCADE and can re-bind when the restored draft is
	// re-sent.
	Attachments []db.Attachment
}

type CancelRunResult struct {
	AgentRun             db.AgentRun
	CancelledChatMessage *CancelledChatMessageResult
}

// CancelTaskOptions carries what the caller knows about the client that asked
// for the cancellation.
type CancelRunOptions struct {
	// ClientSupportsDraftRestore is true when the caller can recover a prompt
	// through the durable draft-restore path (#5219). Only such a client may be
	// handed a deferred outcome; for anyone else the empty-transcript judgment
	// stays synchronous, because the cancel response is their only chance to get
	// the prompt back. See protocol.AppCapabilityChatDraftRestoreV1.
	ClientSupportsDraftRestore bool
}

// CancelTask cancels a single task by ID. It broadcasts a run:cancelled event
// so frontends can update immediately.
func (s *AgentRunService) CancelRun(ctx context.Context, runID pgtype.UUID) (*db.AgentRun, error) {
	// Every caller of this wrapper cancels a non-chat task — task/autopilot
	// tasks through the task-scoped endpoint, plus the daemon and sweeper paths
	// — so finalizeCancelledChatMessage returns before the gate is even read.
	// Should a chat task ever reach here, there is no client waiting on a
	// synchronous restore anyway, and the durable path is the only one that can
	// hand the prompt back at all.
	result, err := s.CancelRunWithResult(ctx, runID, CancelRunOptions{ClientSupportsDraftRestore: true})
	if err != nil {
		return nil, err
	}
	return &result.AgentRun, nil
}

// CancelTaskWithResult cancels a single task and returns any chat-specific
// cleanup result needed by user-facing callers.
func (s *AgentRunService) CancelRunWithResult(ctx context.Context, runID pgtype.UUID, opts CancelRunOptions) (*CancelRunResult, error) {
	agentRun, err := s.Queries.CancelAgentRun(ctx, runID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := s.Queries.GetAgentRun(ctx, runID)
		if err != nil {
			return nil, fmt.Errorf("cancel run: %w", err)
		}
		return &CancelRunResult{AgentRun: existing}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cancel run: %w", err)
	}

	slog.Info("task cancelled", "run_id", util.UUIDToString(agentRun.ID), "task_id", util.UUIDToString(agentRun.TaskID))
	s.captureRunCancelled(ctx, agentRun)
	cancelledChatMessage := s.finalizeCancelledChatMessage(ctx, agentRun, opts)

	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, agentRun.AgentID)

	// Broadcast cancellation as a run:failed event so frontends clear the live card
	s.broadcastRunEvent(ctx, protocol.EventRunCancelled, agentRun)
	s.NotifyRunFinished(agentRun)

	return &CancelRunResult{
		AgentRun:             agentRun,
		CancelledChatMessage: cancelledChatMessage,
	}, nil
}

// chatInputOwnerID resolves the id the task's user-message input batch is
// keyed on: chat_input_run_id when set (auto-retry clones inherit their
// parent's, so provenance checks reach the parent's sealed messages), falling
// back to the task's own id for legacy rows.
func chatInputOwnerID(agentRun db.AgentRun) pgtype.UUID {
	if agentRun.ChatInputRunID.Valid {
		return agentRun.ChatInputRunID
	}
	return agentRun.ID
}

func (s *AgentRunService) finalizeCancelledChatMessage(ctx context.Context, agentRun db.AgentRun, opts CancelRunOptions) *CancelledChatMessageResult {
	if !agentRun.ChatSessionID.Valid {
		return nil
	}
	var cancelled *CancelledChatMessageResult
	if err := s.runInTxWithHandle(ctx, func(qtx *db.Queries, tx pgx.Tx) error {
		messages, err := qtx.ListAgentRunMessages(ctx, agentRun.ID)
		if err != nil {
			return fmt.Errorf("list cancelled chat task messages: %w", err)
		}
		restorable := len(messages) == 0
		if restorable {
			// Channel-ingested user messages are the durable record of what
			// the platform sender wrote — the sender has no Silieco composer
			// to restore a draft into. The gate is the immutable per-message
			// channel_ingested stamp, NOT the channel_chat_session_binding
			// row: archiving a session or rebinding an installation deletes
			// the binding while the messages (and a still-cancellable task)
			// remain. Keyed by the input-batch owner id so an auto-retry
			// clone (which inherits chat_input_run_id) reaches the same
			// verdict as its parent. A channel task settles as "Stopped."
			// below instead of deleting its sealed input batch.
			channelIngested, err := qtx.RunHasChannelIngestedMessages(ctx, chatInputOwnerID(agentRun))
			if err != nil {
				return fmt.Errorf("check cancelled chat channel provenance: %w", err)
			}
			restorable = !channelIngested
		}
		if restorable && agentRun.StartedAt.Valid && opts.ClientSupportsDraftRestore {
			// A started task's daemon learns of the cancellation by polling
			// and may still be flushing its transcript tail, so "empty" is
			// not trustworthy yet. Defer the judgment until the daemon acks
			// its flush (cancel-ack) or the sweeper grace period expires
			// (#5219). "Non-empty" needs no deferral: late rows only append.
			//
			// Deferring is gated on the client: clients and server do not
			// upgrade together, and a client that cannot read the durable
			// restore would take an empty cancel response as "nothing to put
			// back" and lose the prompt. Such a client falls through to the
			// legacy synchronous branch below — it keeps the pre-#5219 race
			// (an in-flight transcript tail can still be misjudged as empty),
			// which is exactly the behaviour it has against an old server, and
			// strictly better than dropping the input.
			if _, err := qtx.MarkChatFinalizeDeferred(ctx, agentRun.ID); err != nil {
				return fmt.Errorf("mark chat finalize deferred: %w", err)
			}
			return nil
		}
		if restorable {
			// Detach attachments BEFORE deleting the user message — the
			// attachment FK is ON DELETE CASCADE, so deleting first would
			// destroy rows the restored draft needs to re-bind.
			if err := relational.LockForDelete(ctx, tx, relational.DeleteUserChatMessageByRun, agentRun.ID); err != nil {
				return fmt.Errorf("lock cancelled chat input dependencies: %w", err)
			}
			detached, err := qtx.DetachAttachmentsFromUserChatMessageByRun(ctx, agentRun.ID)
			if err != nil {
				return fmt.Errorf("detach cancelled chat message attachments: %w", err)
			}
			deleted, err := qtx.DeleteUserChatMessageByRun(ctx, agentRun.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("delete empty cancelled chat user message: %w", err)
			}
			cancelled = &CancelledChatMessageResult{
				ChatSessionID:  util.UUIDToString(deleted.ChatSessionID),
				MessageID:      util.UUIDToString(deleted.ID),
				Content:        deleted.Content,
				RestoreToInput: true,
				Attachments:    detached,
			}
			return nil
		}
		if _, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ChatSessionID: agentRun.ChatSessionID,
			Role:          "assistant",
			Content:       "Stopped.",
			RunID:         agentRun.ID,
			ElapsedMs:     computeChatElapsedMs(agentRun),
			SenderAgentID: agentRun.AgentID,
		}); err != nil {
			return fmt.Errorf("create cancelled chat message: %w", err)
		}
		return nil
	}); err != nil {
		slog.Error("failed to finalize cancelled chat message",
			"run_id", util.UUIDToString(agentRun.ID),
			"chat_session_id", util.UUIDToString(agentRun.ChatSessionID),
			"error", err,
		)
		return nil
	}
	return cancelled
}

// FinalizeDeferredCancelledChat settles the empty/non-empty judgment that
// finalizeCancelledChatMessage deferred for a started-but-empty cancelled
// chat task (#5219). Called from the daemon's cancel-ack (transcript flush
// complete) and from the sweeper grace-period fallback; the marker claim is
// atomic, so concurrent callers cannot finalize the same task twice and a
// call with no pending marker is a no-op. The settled outcome is broadcast
// as chat:cancel_finalized since the cancel HTTP response has long returned.
func (s *AgentRunService) FinalizeDeferredCancelledChat(ctx context.Context, runID pgtype.UUID) {
	var (
		agentRun db.AgentRun
		payload  protocol.ChatCancelFinalizedPayload
		settled  bool
	)
	if err := s.runInTxWithHandle(ctx, func(qtx *db.Queries, tx pgx.Tx) error {
		// Lock the task's chat_session first. chat_draft_restore has no FK
		// (SILI-3515), so the insert below takes no lock of its own on the
		// session — without this, a workspace/agent/session delete that swept
		// the table just before we commit would leave our restore row (holding
		// the user's prompt) orphaned forever. The deleters take the same lock
		// before their sweep, so one of us blocks: either they wait and their
		// sweep sees our row, or we wait and find no session left to restore
		// into. Locking the session BEFORE the task claim also fixes the global
		// lock order (chat_session -> agent_run) that keeps this from
		// deadlocking against the deleters' cascade.
		_, err := qtx.LockChatSessionForRun(ctx, runID)
		sessionGone := errors.Is(err, pgx.ErrNoRows)
		if err != nil && !sessionGone {
			return fmt.Errorf("lock chat session for deferred finalize: %w", err)
		}

		// Claim the marker inside the settlement tx: a failed settlement then
		// rolls the claim back so the sweeper can retry, instead of leaving the
		// task with a cleared marker and no finalized outcome. The row lock
		// still serializes the daemon ack and the sweeper — the loser's UPDATE
		// blocks until the winner commits, then matches no row (ErrNoRows) — so
		// the same task is never finalized twice.
		claimed, err := qtx.ClaimChatFinalizeDeferred(ctx, runID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim deferred chat finalize: %w", err)
		}
		agentRun = claimed
		if sessionGone {
			// The session cascaded away (its FK NULLs the column below anyway):
			// there is no transcript to settle and nowhere to put a restore. The
			// claim above still cleared the marker, so the sweeper stops retrying.
			return nil
		}
		if !claimed.ChatSessionID.Valid {
			return nil
		}
		settled = true
		payload.ChatSessionID = util.UUIDToString(claimed.ChatSessionID)
		payload.RunID = util.UUIDToString(claimed.ID)
		payload.InitiatorUserID = util.UUIDToString(claimed.InitiatorUserID)

		messages, err := qtx.ListAgentRunMessages(ctx, claimed.ID)
		if err != nil {
			return fmt.Errorf("list cancelled chat task messages: %w", err)
		}
		restorable := len(messages) == 0
		if restorable {
			// Same immutable-provenance guard as finalizeCancelledChatMessage:
			// channel tasks never restore-delete their sealed input. The sync
			// path no longer defers such tasks; this covers markers created by
			// an older replica during a rolling deploy.
			channelIngested, err := qtx.RunHasChannelIngestedMessages(ctx, chatInputOwnerID(claimed))
			if err != nil {
				return fmt.Errorf("check cancelled chat channel provenance: %w", err)
			}
			restorable = !channelIngested
		}
		if restorable {
			// The transcript stayed empty through the daemon flush: same
			// outcome as the synchronous empty branch, but the cancel HTTP
			// response is long gone and the broadcast is best-effort. The
			// restore is persisted in this same tx and served by the
			// creator-authorized draft-restores endpoint, so a client that
			// misses the event recovers it on the next session open; the
			// event itself carries no content and is only an invalidation
			// hint.
			if err := relational.LockForDelete(ctx, tx, relational.DeleteUserChatMessageByRun, claimed.ID); err != nil {
				return fmt.Errorf("lock cancelled chat input dependencies: %w", err)
			}
			detached, err := qtx.DetachAttachmentsFromUserChatMessageByRun(ctx, claimed.ID)
			if err != nil {
				return fmt.Errorf("detach cancelled chat message attachments: %w", err)
			}
			deleted, err := qtx.DeleteUserChatMessageByRun(ctx, claimed.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				payload.Outcome = ""
				return nil
			}
			if err != nil {
				return fmt.Errorf("delete empty cancelled chat user message: %w", err)
			}
			attachmentIDs := make([]pgtype.UUID, 0, len(detached))
			for _, a := range detached {
				attachmentIDs = append(attachmentIDs, a.ID)
			}
			if _, err := qtx.CreateChatDraftRestore(ctx, db.CreateChatDraftRestoreParams{
				ID:            deleted.ID,
				ChatSessionID: claimed.ChatSessionID,
				RunID:         claimed.ID,
				Content:       deleted.Content,
				AttachmentIds: attachmentIDs,
			}); err != nil {
				return fmt.Errorf("create chat draft restore: %w", err)
			}
			payload.Outcome = protocol.ChatCancelOutcomeRestored
			payload.MessageID = util.UUIDToString(deleted.ID)
			return nil
		}
		row, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ChatSessionID: claimed.ChatSessionID,
			Role:          "assistant",
			Content:       "Stopped.",
			RunID:         claimed.ID,
			ElapsedMs:     computeChatElapsedMs(claimed),
			SenderAgentID: claimed.AgentID,
		})
		if err != nil {
			return fmt.Errorf("create cancelled chat message: %w", err)
		}
		payload.Outcome = protocol.ChatCancelOutcomeStopped
		payload.MessageID = util.UUIDToString(row.ID)
		payload.Content = row.Content
		payload.MessageKind = row.MessageKind
		if row.CreatedAt.Valid {
			payload.CreatedAt = row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if row.ElapsedMs.Valid {
			payload.ElapsedMs = row.ElapsedMs.Int64
		}
		return nil
	}); err != nil {
		slog.Error("failed to finalize deferred cancelled chat",
			"run_id", util.UUIDToString(runID),
			"error", err,
		)
		return
	}
	if !settled || payload.Outcome == "" {
		return
	}
	s.broadcastChatCancelFinalized(ctx, agentRun, payload)
}

func (s *AgentRunService) broadcastChatCancelFinalized(ctx context.Context, agentRun db.AgentRun, payload protocol.ChatCancelFinalizedPayload) {
	workspaceID := s.ResolveRunWorkspaceID(ctx, agentRun)
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:          protocol.EventChatCancelFinalized,
		WorkspaceID:   workspaceID,
		ActorType:     "system",
		ActorID:       "",
		ChatSessionID: util.UUIDToString(agentRun.ChatSessionID),
		Payload:       payload,
	})
}

// ClaimTask atomically claims the next queued task for an agent,
// respecting max_concurrent_runs.
func (s *AgentRunService) ClaimRun(ctx context.Context, agentID pgtype.UUID) (*db.AgentRun, error) {
	start := time.Now()
	var (
		outcome                                                              = "unknown"
		getAgentMs, countRunningMs, claimAgentMs, updateStatusMs, dispatchMs int64
		claimed                                                              *db.AgentRun
	)
	defer func() {
		s.maybeLogClaimSlow(agentID, outcome, start, getAgentMs, countRunningMs, claimAgentMs, updateStatusMs, dispatchMs)
	}()

	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		t0 := time.Now()
		agent, err := qtx.GetAgentForClaimUpdate(ctx, agentID)
		getAgentMs = time.Since(t0).Milliseconds()
		if err != nil {
			outcome = "error_get_agent"
			return fmt.Errorf("agent not found: %w", err)
		}

		t0 = time.Now()
		running, err := qtx.CountRunningRuns(ctx, agentID)
		countRunningMs = time.Since(t0).Milliseconds()
		if err != nil {
			outcome = "error_count_running"
			return fmt.Errorf("count running tasks: %w", err)
		}
		if running >= int64(agent.MaxConcurrentRuns) {
			slog.Debug("task claim: no capacity", "agent_id", util.UUIDToString(agentID), "running", running, "max", agent.MaxConcurrentRuns)
			outcome = "no_capacity"
			return nil
		}

		t0 = time.Now()
		agentRun, err := qtx.ClaimAgentRun(ctx, db.ClaimAgentRunParams{
			AgentID:          agentID,
			PrepareLeaseSecs: prepareLeaseDuration.Seconds(),
		})
		claimAgentMs = time.Since(t0).Milliseconds()
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Debug("task claim: no tasks available", "agent_id", util.UUIDToString(agentID))
				outcome = "no_tasks"
				return nil
			}
			outcome = "error_claim"
			return fmt.Errorf("claim run: %w", err)
		}

		claimedRun := agentRun
		claimed = &claimedRun
		return nil
	})
	if err != nil {
		if outcome == "unknown" {
			outcome = "error_transaction"
		}
		return nil, err
	}
	if claimed == nil {
		claimed, err = s.claimNextWorkflowReview(ctx, agentID)
		if err != nil {
			return nil, err
		}
		if claimed == nil {
			return nil, nil
		}
	}

	slog.Info("task claimed", "run_id", util.UUIDToString(claimed.ID), "agent_id", util.UUIDToString(agentID))
	s.captureRunDispatched(ctx, *claimed)

	// Refresh agent status from active tasks. This avoids a stale unconditional
	// working write racing after a just-cancelled claim.
	t0 := time.Now()
	s.ReconcileAgentStatus(ctx, agentID)
	updateStatusMs = time.Since(t0).Milliseconds()

	// Broadcast run:dispatch. ResolveTaskWorkspaceID inside this path can
	// re-query task/chat_session/autopilot_run, so it can also be a real
	// contributor to claim latency.
	t0 = time.Now()
	s.broadcastRunDispatch(ctx, *claimed)
	dispatchMs = time.Since(t0).Milliseconds()

	outcome = "claimed"
	return claimed, nil
}

// ClaimTaskForRuntime claims the next runnable task for a runtime while
// still respecting each agent's max_concurrent_runs limit.
//
// Empty-claim fast path: when EmptyClaim is configured and a recent
// check verified the runtime had no queued tasks, returns immediately
// without touching Postgres. The cache is invalidated synchronously on
// every enqueue (notifyTaskAvailable), so a queued task becomes
// claimable on the next call rather than waiting for the TTL.
func (s *AgentRunService) ClaimRunForRuntime(ctx context.Context, runtimeID pgtype.UUID) (*db.AgentRun, error) {
	start := time.Now()
	var (
		outcome          = "no_task"
		listMs, loopMs   int64
		listCount, tried int
		claimedFlag      bool
	)
	defer func() {
		totalMs := time.Since(start).Milliseconds()
		if totalMs < 300 {
			return
		}
		slog.Info("claim_for_runtime slow",
			"runtime_id", util.UUIDToString(runtimeID),
			"outcome", outcome,
			"total_ms", totalMs,
			"list_pending_ms", listMs,
			"list_pending_count", listCount,
			"agents_tried", tried,
			"claim_loop_ms", loopMs,
			"claimed", claimedFlag,
		)
	}()

	runtimeKey := util.UUIDToString(runtimeID)
	if err := s.PromoteDueDeferredRunsForRuntime(ctx, runtimeID); err != nil {
		outcome = "error_promote_deferred"
		return nil, err
	}

	// Check this before EmptyClaim: a lost claim response moves the task out of
	// `queued`, so the empty-queued cache cannot represent recoverability.
	stale, err := s.Queries.ReclaimStaleDispatchedRunForRuntime(ctx, db.ReclaimStaleDispatchedRunForRuntimeParams{
		RuntimeID:         runtimeID,
		ClaimRecoverySecs: claimResponseRecoveryWindow.Seconds(),
		PrepareLeaseSecs:  prepareLeaseDuration.Seconds(),
	})
	if err == nil {
		outcome = "reclaimed_dispatched"
		claimedFlag = true
		slog.Info("stale dispatched task reclaimed",
			"run_id", util.UUIDToString(stale.ID),
			"runtime_id", runtimeKey,
			"agent_id", util.UUIDToString(stale.AgentID),
		)
		return &stale, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		outcome = "error_reclaim_dispatched"
		return nil, fmt.Errorf("reclaim stale dispatched run: %w", err)
	}

	if s.EmptyClaim.IsEmpty(ctx, runtimeKey) {
		outcome = "empty_cache_hit"
		return nil, nil
	}

	// Sample the invalidation version BEFORE the SELECT. If a
	// concurrent enqueue Bumps between this read and the post-SELECT
	// MarkEmpty, the next IsEmpty will see the empty key tagged with
	// a stale version and reject it — closing the race that would
	// otherwise stall the just-queued task until the empty key's TTL
	// expired.
	preSelectVersion := s.EmptyClaim.CurrentVersion(ctx, runtimeKey)

	t0 := time.Now()
	agentRuns, err := s.Queries.ListQueuedClaimCandidatesByRuntime(ctx, runtimeID)
	listMs = time.Since(t0).Milliseconds()
	listCount = len(agentRuns)
	if err != nil {
		outcome = "error_list"
		return nil, fmt.Errorf("list queued claim candidates: %w", err)
	}

	if len(agentRuns) == 0 {
		s.EmptyClaim.MarkEmpty(ctx, runtimeKey, preSelectVersion)
		outcome = "empty_db"
		return nil, nil
	}

	loopStart := time.Now()
	triedAgents := map[string]struct{}{}
	var claimed *db.AgentRun
	for _, candidate := range agentRuns {
		agentKey := util.UUIDToString(candidate.AgentID)
		if _, seen := triedAgents[agentKey]; seen {
			continue
		}
		triedAgents[agentKey] = struct{}{}
		tried++

		agentRun, err := s.ClaimRun(ctx, candidate.AgentID)
		if err != nil {
			loopMs = time.Since(loopStart).Milliseconds()
			outcome = "error_claim"
			return nil, err
		}
		if agentRun != nil && agentRun.RuntimeID == runtimeID {
			claimed = agentRun
			break
		}
	}
	loopMs = time.Since(loopStart).Milliseconds()
	if claimed != nil {
		claimedFlag = true
		outcome = "claimed"
	}

	return claimed, nil
}

// FinalizeTaskClaim atomically persists the task-scoped token and, for a
// comment-backed task, the exact comment ids embedded in the response. The
// handler must call this only after the full payload has been built and before
// writing any response bytes. A failure rolls both writes back so the claim can
// be safely returned to the queue.
func (s *AgentRunService) FinalizeRunClaim(
	ctx context.Context,
	agentRun db.AgentRun,
	token db.CreateAgentRunTokenParams,
	deliveredCommentIDs []pgtype.UUID,
	recordCommentReceipt bool,
) ([]pgtype.UUID, error) {
	receipt := agentRun.DeliveredCommentIds
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if agentRun.WorkflowReviewRequestID.Valid {
			workflow := WorkflowExecutionService{Queries: s.Queries, TxStarter: s.TxStarter}
			if err := workflow.validateReviewCallInTx(ctx, qtx, agentRun); err != nil {
				return err
			}
		}
		if _, err := qtx.CreateAgentRunToken(ctx, token); err != nil {
			return fmt.Errorf("create task token: %w", err)
		}
		if !recordCommentReceipt {
			return nil
		}
		persisted, err := qtx.SetRunDeliveredCommentIDs(ctx, db.SetRunDeliveredCommentIDsParams{
			DeliveredCommentIds:      deliveredCommentIDs,
			RunID:                    agentRun.ID,
			RuntimeID:                agentRun.RuntimeID,
			DispatchedAt:             agentRun.DispatchedAt,
			ExpectedTriggerCommentID: agentRun.TriggerCommentID,
		})
		if err != nil {
			return fmt.Errorf("set delivered comment ids: %w", err)
		}
		receipt = persisted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

// RequeueTaskAfterClaimFailure immediately releases an exact dispatched claim
// whose payload finalization failed before the HTTP response was written. The
// SQL CAS includes dispatched_at so a late handler cannot roll back a newer
// reclaim. This is not a fresh enqueue: do not duplicate queued analytics.
func (s *AgentRunService) RequeueRunAfterClaimFailure(ctx context.Context, agentRun db.AgentRun) (*db.AgentRun, error) {
	requeued, err := s.Queries.RequeueAgentRunAfterClaimFailure(ctx, db.RequeueAgentRunAfterClaimFailureParams{
		RunID:        agentRun.ID,
		RuntimeID:    agentRun.RuntimeID,
		DispatchedAt: agentRun.DispatchedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("requeue task after claim failure: %w", err)
	}
	s.ReconcileAgentStatus(ctx, requeued.AgentID)
	s.broadcastRunEvent(ctx, protocol.EventRunQueued, requeued)
	s.notifyRunAvailable(requeued)
	slog.Info("task requeued after claim finalization failure",
		"run_id", util.UUIDToString(requeued.ID),
		"runtime_id", util.UUIDToString(requeued.RuntimeID),
	)
	return &requeued, nil
}

// ClaimTasksForRuntimes is the machine-level (SILI-4257) batch counterpart of
// ClaimTaskForRuntime: it claims up to maxTasks tasks across every runtime in
// runtimeIDs in a single call, so a daemon can poll for all of its runtimes
// with one HTTP request and a constant number of DB queries instead of one
// request (and one promote/reclaim/list cycle) per runtime.
//
// It preserves the exact per-runtime semantics, just set-ified:
//  1. promote due deferred tasks across the set (one UPDATE);
//  2. reclaim up to maxTasks stale-dispatched tasks across the set (one UPDATE)
//     — done before the empty-cache check because a lost claim response moves
//     the task out of `queued`, which the empty-queued cache cannot represent;
//  3. short-circuit runtimes whose empty-claim verdict is cached, sampling the
//     invalidation version for the rest BEFORE the candidate SELECT;
//  4. list queued candidates across the non-empty set (one SELECT);
//  5. mark still-empty runtimes so their next idle poll skips Postgres;
//  6. claim per distinct agent via ClaimTask (unchanged — preserves the
//     per-(task, agent) serialization, the agent concurrency cap, and every
//     dispatch side effect) until maxTasks is reached.
//
// The returned slice contains both reclaimed and freshly-claimed tasks, each
// already carrying its runtime_id so the daemon routes it to the matching
// runtime locally.
func (s *AgentRunService) ClaimRunsForRuntimes(ctx context.Context, runtimeIDs []pgtype.UUID, maxRuns int) ([]db.AgentRun, error) {
	if len(runtimeIDs) == 0 || maxRuns <= 0 {
		return nil, nil
	}

	// De-dup runtime IDs defensively so MarkEmpty/version bookkeeping stays
	// unambiguous even if a daemon ever sends a duplicate.
	seen := make(map[string]struct{}, len(runtimeIDs))
	uniqueIDs := make([]pgtype.UUID, 0, len(runtimeIDs))
	runtimeInSet := make(map[string]struct{}, len(runtimeIDs))
	for _, rid := range runtimeIDs {
		key := util.UUIDToString(rid)
		runtimeInSet[key] = struct{}{}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		uniqueIDs = append(uniqueIDs, rid)
	}

	claimed := make([]db.AgentRun, 0, maxRuns)

	// 1. Promote due deferred tasks across the whole set (promote-first, like
	// the singular path). Replay the per-row side effects the singular service
	// method PromoteDueDeferredTasksForRuntime performs — crucially
	// EmptyClaim.Bump (via NotifyTaskEnqueued → notifyTaskAvailable) so a
	// just-promoted deferred task invalidates its runtime's cached empty
	// verdict BEFORE the empty-cache filter in step 3; otherwise a stale
	// MarkEmpty from a prior idle poll would short-circuit the runtime and the
	// promoted task would sit unclaimed until the empty key's TTL. Also emits
	// the deferred→queued UI event and the enqueue analytics sample.
	promoted, err := s.Queries.PromoteDueDeferredRunsForRuntimes(ctx, uniqueIDs)
	if err != nil {
		return nil, fmt.Errorf("promote deferred tasks: %w", err)
	}
	for _, agentRun := range promoted {
		slog.Info("deferred fallback task promoted (batch)",
			"run_id", util.UUIDToString(agentRun.ID),
			"runtime_id", util.UUIDToString(agentRun.RuntimeID),
			"agent_id", util.UUIDToString(agentRun.AgentID),
		)
		s.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
		s.NotifyRunEnqueued(ctx, agentRun)
	}

	// 2. Reclaim lost-response dispatched tasks across the set, up to maxTasks.
	reclaimed, err := s.Queries.ReclaimStaleDispatchedRunsForRuntimes(ctx, db.ReclaimStaleDispatchedRunsForRuntimesParams{
		RuntimeIds:        uniqueIDs,
		ClaimRecoverySecs: claimResponseRecoveryWindow.Seconds(),
		PrepareLeaseSecs:  prepareLeaseDuration.Seconds(),
		MaxRuns:           int32(maxRuns),
	})
	if err != nil {
		return nil, fmt.Errorf("reclaim stale dispatched tasks: %w", err)
	}
	for i := range reclaimed {
		claimed = append(claimed, reclaimed[i])
		slog.Info("stale dispatched task reclaimed (batch)",
			"run_id", util.UUIDToString(reclaimed[i].ID),
			"runtime_id", util.UUIDToString(reclaimed[i].RuntimeID),
			"agent_id", util.UUIDToString(reclaimed[i].AgentID),
		)
	}
	if len(claimed) >= maxRuns {
		return claimed[:maxRuns], nil
	}

	// 3. Empty-cache short-circuit + version sampling for the remaining runtimes.
	nonEmpty := make([]pgtype.UUID, 0, len(uniqueIDs))
	versions := make(map[string]int64, len(uniqueIDs))
	for _, rid := range uniqueIDs {
		key := util.UUIDToString(rid)
		if s.EmptyClaim.IsEmpty(ctx, key) {
			continue
		}
		versions[key] = s.EmptyClaim.CurrentVersion(ctx, key)
		nonEmpty = append(nonEmpty, rid)
	}
	if len(nonEmpty) == 0 {
		return claimed, nil
	}

	// 4. One candidate SELECT across the non-empty set.
	candidates, err := s.Queries.ListQueuedClaimCandidatesByRuntimes(ctx, nonEmpty)
	if err != nil {
		// Steps 2/6 commit reclaimed/claimed tasks in their own transactions,
		// so `claimed` may already hold tasks dispatched server-side. Dropping
		// them with a 500 makes the daemon HTTP-fall-back and claim a SECOND
		// batch into the same free slots (the first batch then waits for stale
		// reclaim) — the same double-claim this PR set out to remove
		// (SILI-4257). Prefer partial success: hand back what committed so the
		// handler finalizes and returns it; the errored candidates stay queued
		// for the next poll.
		if len(claimed) > 0 {
			slog.Error("batch claim: candidate query failed after partial success; returning claimed tasks to avoid loss",
				"error", err, "claimed", len(claimed))
			return claimed, nil
		}
		return nil, fmt.Errorf("list queued claim candidates: %w", err)
	}

	// 5. Mark runtimes with zero candidates empty so their next idle poll skips
	// Postgres. Runtimes that had at least one candidate are intentionally not
	// marked (positive results always re-check the DB, matching the singular
	// path).
	withCandidates := make(map[string]struct{}, len(candidates))
	for i := range candidates {
		withCandidates[util.UUIDToString(candidates[i].RuntimeID)] = struct{}{}
	}
	for _, rid := range nonEmpty {
		key := util.UUIDToString(rid)
		if _, ok := withCandidates[key]; !ok {
			s.EmptyClaim.MarkEmpty(ctx, key, versions[key])
		}
	}

	// 6. Claim per distinct agent (unchanged path → same per-(task, agent)
	// serialization, capacity cap, and dispatch side effects) until maxTasks is
	// reached.
	triedAgents := make(map[string]struct{}, len(candidates))
	for i := range candidates {
		if len(claimed) >= maxRuns {
			break
		}
		agentKey := util.UUIDToString(candidates[i].AgentID)
		if _, tried := triedAgents[agentKey]; tried {
			continue
		}
		triedAgents[agentKey] = struct{}{}

		agentRun, err := s.ClaimRun(ctx, candidates[i].AgentID)
		if err != nil {
			// Each ClaimTask commits in its own transaction, so earlier
			// iterations (and step-2 reclaims) are already dispatched
			// server-side. Returning nil here would drop them and force the
			// daemon to double-claim via HTTP fallback (SILI-4257). Return the
			// partial batch instead; the failed agent's task stays queued.
			if len(claimed) > 0 {
				slog.Error("batch claim: claim task failed after partial success; returning claimed tasks to avoid loss",
					"error", err, "claimed", len(claimed))
				return claimed, nil
			}
			return nil, fmt.Errorf("claim run: %w", err)
		}
		if agentRun == nil {
			continue
		}
		// ClaimAgentTask selects by agent only; guard that the claimed task
		// belongs to a runtime this daemon hosts. An agent with a
		// higher-priority queued task on ANOTHER daemon's runtime could
		// otherwise be dispatched here and dropped — matching the singular
		// path's runtime_id guard. Such a stray dispatch is recovered by the
		// reclaim path on the owning daemon's next poll.
		if _, ok := runtimeInSet[util.UUIDToString(agentRun.RuntimeID)]; !ok {
			continue
		}
		claimed = append(claimed, *agentRun)
	}

	return claimed, nil
}

func (s *AgentRunService) PromoteDueDeferredRunsForRuntime(ctx context.Context, runtimeID pgtype.UUID) error {
	agentRuns, err := s.Queries.PromoteDueDeferredRunsForRuntime(ctx, runtimeID)
	if err != nil {
		return fmt.Errorf("promote due deferred tasks: %w", err)
	}
	for _, agentRun := range agentRuns {
		slog.Info("deferred fallback task promoted",
			"run_id", util.UUIDToString(agentRun.ID),
			"runtime_id", util.UUIDToString(runtimeID),
			"agent_id", util.UUIDToString(agentRun.AgentID),
		)
		s.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
		s.NotifyRunEnqueued(ctx, agentRun)
	}
	return nil
}

// maybeLogClaimSlow emits one structured log per ClaimTask call when its total
// latency exceeds 300ms, so the prod tail can be diagnosed without flooding
// logs at normal poll rates. Called via defer so it captures the full path
// including post-claim updateAgentStatus / broadcastTaskDispatch (both of
// which can hit the DB) and any error exit.
func (s *AgentRunService) maybeLogClaimSlow(agentID pgtype.UUID, outcome string, start time.Time, getAgentMs, countRunningMs, claimAgentMs, updateStatusMs, dispatchMs int64) {
	totalMs := time.Since(start).Milliseconds()
	if totalMs < 300 {
		return
	}
	slog.Info("claim_task slow",
		"agent_id", util.UUIDToString(agentID),
		"outcome", outcome,
		"total_ms", totalMs,
		"get_agent_ms", getAgentMs,
		"count_running_ms", countRunningMs,
		"claim_agent_ms", claimAgentMs,
		"update_status_ms", updateStatusMs,
		"dispatch_ms", dispatchMs,
	)
}

// StartTask transitions a dispatched task to running.
// Task status is NOT changed here — the agent manages it via the CLI.
func (s *AgentRunService) StartRun(ctx context.Context, runID pgtype.UUID) (*db.AgentRun, error) {
	agentRun, err := s.startRunWithReviewValidation(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("start run: %w", err)
	}
	s.cancelDeferredEscalationsForRun(ctx, agentRun.ID)

	slog.Info("task started", "run_id", util.UUIDToString(agentRun.ID), "task_id", util.UUIDToString(agentRun.TaskID))
	s.captureRunStarted(ctx, agentRun)
	// Tell every connected workspace WS client that this task transitioned
	// (dispatched | waiting_local_directory) → running. Without this, the
	// workspace-wide `agentTaskSnapshot` query only refreshes on the 30s
	// staleTime, so any UI that distinguishes "queued" from "running" (e.g.
	// the task-card agent activity indicator) lags by up to half a minute
	// on the transition users care about most.
	s.broadcastRunEvent(ctx, protocol.EventRunRunning, agentRun)
	return &agentRun, nil
}

func (s *AgentRunService) cancelDeferredEscalationsForRun(ctx context.Context, runID pgtype.UUID) {
	cancelled, err := s.Queries.CancelDeferredEscalationsForRun(ctx, runID)
	if err != nil {
		slog.Warn("cancel deferred escalations for task failed", "run_id", util.UUIDToString(runID), "error", err)
		return
	}
	for _, agentRun := range cancelled {
		slog.Info("deferred fallback task cancelled",
			"run_id", util.UUIDToString(agentRun.ID),
			"primary_run_id", util.UUIDToString(runID),
			"reason", "primary_acknowledged",
		)
	}
}

func (s *AgentRunService) CancelDeferredEscalationsForTaskAgent(ctx context.Context, taskID, agentID pgtype.UUID) {
	cancelled, err := s.Queries.CancelDeferredEscalationsForTaskAgent(ctx, db.CancelDeferredEscalationsForTaskAgentParams{
		TaskID:  taskID,
		AgentID: agentID,
	})
	if err != nil {
		slog.Warn("cancel deferred escalations for task agent failed",
			"task_id", util.UUIDToString(taskID),
			"agent_id", util.UUIDToString(agentID),
			"error", err)
		return
	}
	for _, agentRun := range cancelled {
		slog.Info("deferred fallback task cancelled",
			"run_id", util.UUIDToString(agentRun.ID),
			"task_id", util.UUIDToString(taskID),
			"agent_id", util.UUIDToString(agentID),
			"reason", "agent_comment_acknowledged",
		)
	}
}

// ExtendTaskPrepareLease keeps a claimed-but-not-started task protected while
// the daemon resolves cached inputs and prepares the execution environment.
func (s *AgentRunService) ExtendRunPrepareLease(ctx context.Context, runID, runtimeID pgtype.UUID) (*db.AgentRun, error) {
	agentRun, err := s.Queries.ExtendAgentRunPrepareLease(ctx, db.ExtendAgentRunPrepareLeaseParams{
		ID:        runID,
		RuntimeID: runtimeID,
		LeaseSecs: prepareLeaseDuration.Seconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("extend task prepare lease: %w", err)
	}
	return &agentRun, nil
}

// MarkTaskWaitingLocalDirectory parks a dispatched task in the
// waiting_local_directory state while the daemon waits for another in-flight
// task to release the project_resource path lock. reason carries a short
// human-readable hint (typically the contested path) that the UI surfaces
// next to the status. Returns the updated row so the daemon can confirm the
// transition and so the broadcast carries the up-to-date snapshot.
func (s *AgentRunService) MarkRunWaitingLocalDirectory(ctx context.Context, runID pgtype.UUID, reason string) (*db.AgentRun, error) {
	reason = strings.TrimSpace(reason)
	agentRun, err := s.Queries.MarkAgentRunWaitingLocalDirectory(ctx, db.MarkAgentRunWaitingLocalDirectoryParams{
		ID:               runID,
		WaitReason:       pgtype.Text{String: reason, Valid: reason != ""},
		PrepareLeaseSecs: prepareLeaseDuration.Seconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("mark task waiting_local_directory: %w", err)
	}

	slog.Info("task waiting_local_directory",
		"run_id", util.UUIDToString(agentRun.ID),
		"task_id", util.UUIDToString(agentRun.TaskID),
		"reason", reason,
	)
	s.broadcastRunEvent(ctx, protocol.EventRunWaitingLocalDirectory, agentRun)
	return &agentRun, nil
}

// CompleteTask marks a task as completed.
// Task status is NOT changed here — the agent manages it via the CLI.
//
// For chat tasks, CompleteAgentTask and the chat_session resume-pointer
// update run in a single transaction. This closes a race where the next
// queued chat message could be claimed in the window between the task
// flipping to 'completed' and chat_session.session_id being refreshed,
// causing the new task to resume against a stale (or NULL) session.
type WorktreeDelivery struct {
	Branch   string
	BaseSHA  string
	FinalSHA string
	Status   string
}

func worktreeText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func (s *AgentRunService) CompleteRun(ctx context.Context, runID pgtype.UUID, result []byte, sessionID, workDir string, sessionRolloutMissing bool, retiredSessionID string) (*db.AgentRun, error) {
	return s.CompleteRunWithDelivery(ctx, runID, result, sessionID, workDir, sessionRolloutMissing, retiredSessionID, WorktreeDelivery{})
}

func (s *AgentRunService) CompleteRunWithDelivery(ctx context.Context, runID pgtype.UUID, result []byte, sessionID, workDir string, sessionRolloutMissing bool, retiredSessionID string, delivery WorktreeDelivery) (*db.AgentRun, error) {
	var agentRun db.AgentRun
	// chatAssistantMsg is the single assistant outcome row written for a chat
	// task inside the completion transaction below. It is broadcast (chat:done)
	// only after the transaction commits.
	var chatAssistantMsg *db.ChatMessage
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		t, err := qtx.CompleteAgentRun(ctx, db.CompleteAgentRunParams{
			ID:                     runID,
			Result:                 result,
			SessionID:              pgtype.Text{String: sessionID, Valid: sessionID != ""},
			WorkDir:                pgtype.Text{String: workDir, Valid: workDir != ""},
			SessionRolloutMissing:  sessionRolloutMissing,
			WorktreeBranch:         worktreeText(delivery.Branch),
			WorktreeBaseSha:        worktreeText(delivery.BaseSHA),
			WorktreeFinalSha:       worktreeText(delivery.FinalSHA),
			WorktreeDeliveryStatus: worktreeText(delivery.Status),
			RetiredSessionID:       pgtype.Text{String: retiredSessionID, Valid: retiredSessionID != ""},
		})
		if err != nil {
			return err
		}
		agentRun = t

		if t.ChatSessionID.Valid {
			// Pin the chat_session's runtime_id alongside the session_id so the
			// next claim can apply the runtime-guard. Both fields move together:
			// when there's no session_id to record, leave runtime_id untouched
			// (NULL → COALESCE keeps the existing value).
			var sessionRuntimeID pgtype.UUID
			if sessionID != "" {
				sessionRuntimeID = t.RuntimeID
			}
			// COALESCE in SQL guarantees empty inputs don't wipe the
			// existing resume pointer; we still surface DB errors.
			if err := qtx.UpdateChatSessionSession(ctx, db.UpdateChatSessionSessionParams{
				ID:        t.ChatSessionID,
				SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""},
				WorkDir:   pgtype.Text{String: workDir, Valid: workDir != ""},
				RuntimeID: sessionRuntimeID,
			}); err != nil {
				return fmt.Errorf("update chat session resume pointer: %w", err)
			}
			// A turn that recovered by abandoning its session still has to
			// retire it here. The COALESCE above cannot: a fresh retry that
			// succeeds without emitting a new id passes sessionID == "", which
			// preserves the pointer — and the pointer is the poisoned session
			// (GH #6066). Runs after the update so a real new id wins first and
			// the match guard then finds nothing to clear.
			if retiredSessionID != "" {
				if err := qtx.ClearChatSessionSessionIfMatches(ctx, db.ClearChatSessionSessionIfMatchesParams{
					ID:        t.ChatSessionID,
					SessionID: pgtype.Text{String: retiredSessionID, Valid: true},
					RuntimeID: t.RuntimeID,
				}); err != nil {
					return fmt.Errorf("clear retired chat session resume pointer: %w", err)
				}
			}

			// Write the assistant outcome in the SAME transaction as the status
			// flip and resume-pointer update (SILI-4351). For a task-owned direct
			// task this is exactly one row (message or no_response); for a
			// legacy/channel task an empty output writes no row (see
			// writeChatCompletionOutcome). Failing here rolls the whole completion
			// back so the daemon retries the terminal callback, and the status CAS
			// above guarantees a replay can't write a second row.
			msg, err := s.writeChatCompletionOutcome(ctx, qtx, t, result)
			if err != nil {
				return fmt.Errorf("write chat assistant outcome: %w", err)
			}
			chatAssistantMsg = msg
		}
		return nil
	}); err != nil {
		// When parallel agents race, a task may already be completed,
		// cancelled, or failed by the time this call runs. The UPDATE
		// … WHERE status = 'running' returns no rows in that case.
		// Treat it as an idempotent success — same pattern as CancelTask.
		if existing, lookupErr := s.Queries.GetAgentRun(ctx, runID); lookupErr == nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Info("complete run: already finalized",
					"run_id", util.UUIDToString(runID),
					"current_status", existing.Status,
					"agent_id", util.UUIDToString(existing.AgentID),
				)
				return &existing, nil
			}
			slog.Warn("complete task failed",
				"run_id", util.UUIDToString(runID),
				"current_status", existing.Status,
				"task_id", util.UUIDToString(existing.TaskID),
				"chat_session_id", util.UUIDToString(existing.ChatSessionID),
				"agent_id", util.UUIDToString(existing.AgentID),
				"error", err,
			)
		} else {
			slog.Warn("complete task failed: task not found",
				"run_id", util.UUIDToString(runID),
				"lookup_error", lookupErr,
			)
		}
		return nil, fmt.Errorf("complete run: %w", err)
	}

	slog.Info("task completed", "run_id", util.UUIDToString(agentRun.ID), "task_id", util.UUIDToString(agentRun.TaskID))
	s.captureRunCompleted(ctx, agentRun)

	// Invariant: every completed run for a task must have at least one agent
	// comment on the task, so the user always sees something when a run
	// ends. If the agent posted a comment during execution (result, progress
	// ping, or CLI reply), HasAgentCommentedSince returns true and we skip.
	// Otherwise, synthesize one from the final output. For comment-triggered
	// tasks, TriggerCommentID threads the fallback under the original comment;
	// for assignment-triggered tasks it is NULL and the fallback is top-level.
	// Chat tasks have no TaskID and are handled separately below.
	if agentRun.TaskID.Valid {
		suppressNoActionComment, err := HasSquadLeaderNoActionEvaluationForRun(ctx, s.Queries, agentRun)
		if err != nil {
			slog.Warn("checking squad leader no_action evaluation failed",
				"run_id", util.UUIDToString(agentRun.ID),
				"task_id", util.UUIDToString(agentRun.TaskID),
				"agent_id", util.UUIDToString(agentRun.AgentID),
				"error", err,
			)
		}
		agentCommented, _ := s.Queries.HasAgentCommentedSince(ctx, db.HasAgentCommentedSinceParams{
			TaskID:   agentRun.TaskID,
			AuthorID: agentRun.AgentID,
			Since:    agentRun.StartedAt,
		})
		if !suppressNoActionComment && !agentCommented {
			var payload protocol.RunCompletedPayload
			if err := json.Unmarshal(result, &payload); err == nil {
				if payload.Output != "" {
					// Match the CLI's --content / --description behavior: agents that
					// emit literal `\n` 4-char sequences (Python/JSON-style) get them
					// decoded into real newlines before the comment hits the DB. See
					// util.UnescapeBackslashEscapes for the exact contract.
					body := util.UnescapeBackslashEscapes(payload.Output)
					if agentRun.TriggerCommentID.Valid && isTrivialDoneOutput(body) {
						slog.Warn("suppressing trivial comment-trigger fallback output",
							"run_id", util.UUIDToString(agentRun.ID),
							"task_id", util.UUIDToString(agentRun.TaskID),
							"agent_id", util.UUIDToString(agentRun.AgentID),
						)
					} else {
						// Redact first, then bound: a runaway raw-stream Output (GH #5455)
						// must never reach the task thread, even as a clipped excerpt.
						content := truncateFallbackCommentBody(redact.Text(body), maxSynthesizedFallbackCommentRunes)
						s.createAgentComment(ctx, agentRun.TaskID, agentRun.AgentID, content, "comment", agentRun.TriggerCommentID, agentRun.ID)
					}
				}
			}
		}
	}

	// Quick-create tasks: locate the task the agent just created and push
	// an inbox confirmation to the requester. The agent has no task / chat
	// link, so the regular completion paths above don't apply. We find the
	// new task by querying for the most recent task this agent created in
	// the requester's workspace since the task started — more robust than
	// parsing the agent's stdout for an identifier.
	if qc, ok := s.parseQuickCreateContext(agentRun); ok {
		s.notifyQuickCreateCompleted(ctx, agentRun, qc, result)
	}

	// For chat tasks, broadcast chat:done AFTER commit. The single assistant
	// outcome row (message or no_response) and the resume pointer were already
	// persisted inside the transaction above. Unread is derived from the read
	// cursor (chat_session.last_read_at) vs the assistant messages after it — a
	// no_response row has role='assistant' and a fresh created_at, so it counts
	// as unread just like a text reply, no per-reply stamping needed.
	if agentRun.ChatSessionID.Valid {
		// The assistant outcome row (message / no_response) and any attachment
		// binding were written inside the completion transaction above by
		// writeChatCompletionOutcome. Broadcast chat:done AFTER commit.
		s.broadcastChatDone(ctx, agentRun, chatAssistantMsg)
	}

	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, agentRun.AgentID)

	// Broadcast
	s.broadcastRunEvent(ctx, protocol.EventRunCompleted, agentRun)

	return &agentRun, nil
}

// chatNoResponseFallback is the non-empty English body stored on a no_response
// assistant row. New clients render a localized "no text reply this turn"
// message keyed on message_kind='no_response'; older clients that ignore
// message_kind still show this text instead of an empty bubble (SILI-4351).
const chatNoResponseFallback = "The agent finished this turn without a text reply."

// writeChatCompletionOutcome writes the assistant chat_message outcome for a
// completed chat task inside the caller's completion transaction, returning the
// row (nil when none is written).
//
// Direct (web/mobile) tasks get the explicit single-outcome contract: a
// non-empty final output becomes an ordinary assistant message, and an
// empty/whitespace output becomes a visible no_response outcome carrying a
// non-empty English fallback body. It never auto-retries: an empty output is a
// legitimate terminal result (a tool-only turn) and re-running it would repeat
// side effects already performed.
//
// Channel (Slack/Lark) and legacy tasks keep the prior behavior: a non-empty
// output writes an ordinary assistant message, but an EMPTY output writes NO
// row, so chat:done carries empty content and the channel outbound silently
// drops it (SILI-4351 review): the no_response fallback body must never be
// pushed to an external channel. Channel tasks now own a sealed input batch
// (chat_input_run_id set) just like direct tasks, so the discriminator is the
// immutable channel_ingested stamp on the owned batch — keyed by the batch
// owner id so an auto-retry clone (which inherits chat_input_run_id) reaches
// the same verdict as its parent — while a NULL owner marks a legacy task.
func (s *AgentRunService) writeChatCompletionOutcome(ctx context.Context, qtx *db.Queries, agentRun db.AgentRun, result []byte) (*db.ChatMessage, error) {
	// result is the daemon request re-marshalled by the handler, so it is always
	// valid JSON; an empty Output is the only case this branch cares about.
	var payload protocol.RunCompletedPayload
	_ = json.Unmarshal(result, &payload)
	// Same unescape as the task-comment path: literal `\n` from agent stdout
	// becomes a real newline so the chat panel renders paragraph breaks.
	body := util.UnescapeBackslashEscapes(payload.Output)
	isEmpty := strings.TrimSpace(body) == ""

	// SILI-4899 completion-boundary observation. Measures whether the delivery
	// contract in the runtime brief is actually landing on the chat surface.
	// Strictly non-blocking: the reply is written either way.
	s.observeChatOutputLocalPath(agentRun, body)

	// Attachments the agent uploaded during this task (tagged with run_id, not
	// yet bound to any owner) are part of this reply. They make an empty-text
	// turn a real image/file response — NOT a no_response — and need a row to
	// hang on. Count + bind run on qtx so message creation and binding are one
	// atomic outcome.
	wsUUID, _ := util.ParseUUID(s.ResolveRunWorkspaceID(ctx, agentRun))
	var pendingAttachments int64
	if wsUUID.Valid {
		n, err := qtx.CountUnboundChatAttachmentsForRun(ctx, db.CountUnboundChatAttachmentsForRunParams{
			WorkspaceID: wsUUID,
			RunID:       agentRun.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("count chat attachments: %w", err)
		}
		pendingAttachments = n
	}

	// Channel/legacy empty completion with nothing to show: emit no assistant
	// row, only an empty chat:done for typing/lifecycle. Keeps the Slack/Lark
	// silent-drop path — the outbound patcher forwards any non-empty content
	// verbatim, so the fallback body must never be written for a channel task.
	// Attachments still force a row — the agent produced a deliverable the
	// user must see.
	if isEmpty && pendingAttachments == 0 {
		if !agentRun.ChatInputRunID.Valid {
			return nil, nil // legacy task
		}
		channelIngested, err := qtx.RunHasChannelIngestedMessages(ctx, agentRun.ChatInputRunID)
		if err != nil {
			return nil, fmt.Errorf("check chat completion channel provenance: %w", err)
		}
		if channelIngested {
			return nil, nil // channel task
		}
	}

	params := db.CreateChatMessageParams{
		ChatSessionID: agentRun.ChatSessionID,
		Role:          "assistant",
		RunID:         agentRun.ID,
		ElapsedMs:     computeChatElapsedMs(agentRun),
		SenderAgentID: agentRun.AgentID,
	}
	switch {
	case !isEmpty:
		params.Content = redact.Text(body)
		// message_kind left NULL → COALESCE defaults to 'message'.
	case pendingAttachments > 0:
		// Image/file-only reply: a real 'message' outcome with empty text — the
		// attachment cards ARE the response, so it must not read as no_response.
		params.Content = ""
	default:
		// Task-owned direct task, empty output, no attachments: explicit,
		// visible no_response outcome.
		params.Content = chatNoResponseFallback
		params.MessageKind = pgtype.Text{String: protocol.ChatMessageKindNoResponse, Valid: true}
	}
	row, err := qtx.CreateChatMessage(ctx, params)
	if err != nil {
		return nil, err
	}

	// Bind the task's still-unclaimed attachments to the reply we just wrote.
	if pendingAttachments > 0 && wsUUID.Valid {
		bound, err := qtx.BindChatAttachmentsToMessage(ctx, db.BindChatAttachmentsToMessageParams{
			ChatMessageID: row.ID,
			WorkspaceID:   wsUUID,
			RunID:         agentRun.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("bind chat attachments: %w", err)
		}
		if len(bound) > 0 {
			slog.Info("bound chat attachments to assistant reply",
				"run_id", util.UUIDToString(agentRun.ID),
				"message_id", util.UUIDToString(row.ID),
				"count", len(bound),
			)
		}
	}
	return &row, nil
}

// observeChatOutputLocalPath records a metric when a chat reply references a
// runtime-local path (SILI-4899). Observation only — it never mutates the reply,
// never fails the completion, and makes no claim to have fixed anything.
//
// Two hard constraints shape it:
//
//  1. Lexical only. The path lives on the daemon's machine, so the server cannot
//     os.Stat it the way the CLI lint can. That leaves two signals it can be
//     confident about without guessing: a `file://` URL, and the task's own
//     recorded work_dir as a prefix. Anything subtler would be a guess, and a
//     guess is not worth a false signal on a dashboard.
//  2. No path, no body text, and no fragment of either may reach the metric or
//     the log — only the classification and the task id.
func (s *AgentRunService) observeChatOutputLocalPath(agentRun db.AgentRun, body string) {
	if s.Metrics == nil || strings.TrimSpace(body) == "" {
		return
	}
	kind := ""
	switch {
	case strings.Contains(strings.ToLower(body), "file://"):
		kind = "file_url"
	case agentRun.WorkDir.Valid && agentRun.WorkDir.String != "" && strings.Contains(body, agentRun.WorkDir.String):
		kind = "workdir_path"
	default:
		return
	}
	s.Metrics.RecordChatOutputLocalPath(kind)
	slog.Warn("chat reply references a runtime-local path",
		"run_id", util.UUIDToString(agentRun.ID),
		"kind", kind,
	)
}

// FailTask marks a task as failed.
// Task status is NOT changed here — the agent manages it via the CLI.
//
// sessionID/workDir are optional: when the agent established a real session
// before failing (e.g. crashed mid-conversation, was cancelled, or hit a
// tool error), the daemon should pass them so we can preserve the resume
// pointer on both the task row and the chat_session — otherwise the next
// chat turn would silently start a brand-new session and lose memory.
//
// failureReason is a coarse classifier consumed by the auto-retry path.
// Pass "" when unknown — the server runs the raw error text through
// taskfailure.Classify so the persisted failure_reason still lands in
// the canonical refined taxonomy rather than the legacy "agent_error"
// coarse bucket. Daemon callers that already produced a refined reason
// (via classifyPoisonedError, the timeout / runtime classifier, etc.)
// will have their value preserved untouched.
func (s *AgentRunService) FailRun(ctx context.Context, runID pgtype.UUID, errMsg, sessionID, workDir, failureReason string, sessionRolloutMissing bool, retiredSessionID string) (*db.AgentRun, error) {
	return s.FailRunWithDelivery(ctx, runID, errMsg, sessionID, workDir, failureReason, sessionRolloutMissing, retiredSessionID, WorktreeDelivery{})
}

func (s *AgentRunService) FailRunWithDelivery(ctx context.Context, runID pgtype.UUID, errMsg, sessionID, workDir, failureReason string, sessionRolloutMissing bool, retiredSessionID string, delivery WorktreeDelivery) (*db.AgentRun, error) {
	errMsg = util.SanitizeTextForPostgres(errMsg)
	sessionID = util.SanitizeTextForPostgres(sessionID)
	workDir = util.SanitizeTextForPostgres(workDir)
	failureReason = util.SanitizeTextForPostgres(failureReason)
	retiredSessionID = util.SanitizeTextForPostgres(retiredSessionID)
	// SILI-2946: synthesise a refined reason from the error text whenever the
	// caller didn't supply one. This is the last write-path guard against
	// "agent_error" coarse rows ending up in agent_run.failure_reason
	// — every other path either provides a classified reason directly
	// (sweepers writing 'queued_expired' / 'runtime_offline' / 'timeout'
	// / 'runtime_recovery' via SQL) or runs the daemon's classifyPoisonedError
	// + taskfailure.Classify chain.
	if failureReason == "" {
		failureReason = runfailure.Classify(errMsg).String()
	}
	// SILI-5370: daemons upgrade on their own cadence, so a fix that depends on
	// a new daemon-side label only reaches hosts that happened to update. An
	// older daemon reports a *non-empty* catchall for a failed skill-bundle
	// download, which the branch above deliberately leaves alone — without this
	// the retry and the actionable copy would both skip every un-upgraded host.
	// Runs after the empty-reason branch so a legacy reason synthesised there
	// is normalised too, and before the retry pre-compute below so the upgraded
	// reason is what decides retry eligibility.
	failureReason = runfailure.NormalizeDaemonReason(failureReason, errMsg).String()

	// Pre-compute the auto-retry so the retry child can be created inside the
	// SAME transaction as the fail (SILI-4351). Doing it atomically closes the
	// window between the fail committing and the retry appearing during which a
	// newer chat task could claim the now-idle session and jump ahead of the
	// retry. The overlay build can do network I/O (Composio), so we resolve it
	// here — before the transaction — and only for retryable failures, so the
	// common agent_error path skips this work entirely.
	var (
		wantRetry        bool
		retryParent      db.AgentRun
		retryOverlay     runtimeMCPOverlayData
		retryFireAt      pgtype.Timestamptz
		retryMaxAttempts pgtype.Int4
	)
	if retryableReasons[failureReason] {
		if parent, perr := s.Queries.GetAgentRun(ctx, runID); perr != nil {
			slog.Warn("fail task auto-retry: load parent failed",
				"run_id", util.UUIDToString(runID), "error", perr)
		} else if ceiling, delay, policyErr := runRetrySettings(ctx, s.Queries, parent, failureReason); policyErr != nil {
			slog.Warn("fail task auto-retry: invalid frozen policy", "run_id", util.UUIDToString(runID), "error", policyErr)
		} else if retryEligibleWithCeiling(failureReason, parent, ceiling) {
			wantRetry = true
			retryParent = parent
			// Persist the reason-aware effective budget into the child so the
			// retry chain self-describes (e.g. provider_network → max_attempts=3),
			// rather than leaking a contradictory attempt=N/max_attempts=2 row.
			retryMaxAttempts = pgtype.Int4{Int32: ceiling, Valid: true}
			// Defer this attempt when the reason's schedule calls for a backoff
			// (provider_network's final attempt waits ~5s); a zero delay leaves
			// fire_at NULL so the child is created immediately-claimable.
			if delay > 0 {
				retryFireAt = pgtype.Timestamptz{Time: time.Now().Add(delay), Valid: true}
			}
			if agent, aerr := s.Queries.GetAgent(ctx, parent.AgentID); aerr != nil {
				// Best-effort: a missing overlay is not retry-fatal — the child
				// simply runs without the Composio overlay.
				slog.Warn("fail task auto-retry: load agent for overlay failed",
					"run_id", util.UUIDToString(runID),
					"agent_id", util.UUIDToString(parent.AgentID), "error", aerr)
			} else {
				retryOverlay = s.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, agent)
			}
		}
	}

	var agentRun db.AgentRun
	var retried *db.AgentRun
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if wantRetry {
			if err := lockWorkflowRunRetry(ctx, qtx, retryParent); err != nil {
				return err
			}
		}
		t, err := qtx.FailAgentRun(ctx, db.FailAgentRunParams{
			ID:                     runID,
			Error:                  pgtype.Text{String: errMsg, Valid: true},
			FailureReason:          pgtype.Text{String: failureReason, Valid: failureReason != ""},
			SessionID:              pgtype.Text{String: sessionID, Valid: sessionID != ""},
			WorkDir:                pgtype.Text{String: workDir, Valid: workDir != ""},
			WorktreeBranch:         worktreeText(delivery.Branch),
			WorktreeBaseSha:        worktreeText(delivery.BaseSHA),
			WorktreeFinalSha:       worktreeText(delivery.FinalSHA),
			WorktreeDeliveryStatus: worktreeText(delivery.Status),
			SessionRolloutMissing:  sessionRolloutMissing,
			RetiredSessionID:       pgtype.Text{String: retiredSessionID, Valid: retiredSessionID != ""},
		})
		if err != nil {
			return err
		}
		agentRun = t

		// Keep resume-unsafe sessions on the task row for observability, but
		// do not promote them to the chat-level resume pointer.
		//
		// Declining to overwrite is not enough on its own: the claim handler
		// reads chat_session.session_id BEFORE consulting
		// GetLastChatTaskSession, so a pointer already holding the dead
		// session bypasses every filter that query applies and the next turn
		// resumes it (GH #6066). Clear it in this same transaction, matched on
		// the exact session and runtime so a concurrent turn's newer pointer
		// is left alone. ResumeUnsafeFailure (not the reason alone) so an
		// un-upgraded daemon's agent_error.unknown row is caught by the error
		// text too.
		if t.ChatSessionID.Valid && ResumeUnsafeFailure(failureReason, errMsg) {
			deadSession := sessionID
			if deadSession == "" {
				deadSession = t.SessionID.String
			}
			if deadSession != "" {
				if err := qtx.ClearChatSessionSessionIfMatches(ctx, db.ClearChatSessionSessionIfMatchesParams{
					ID:        t.ChatSessionID,
					SessionID: pgtype.Text{String: deadSession, Valid: true},
					RuntimeID: t.RuntimeID,
				}); err != nil {
					return fmt.Errorf("clear poisoned chat session resume pointer: %w", err)
				}
			}
		}
		// A session the run explicitly retired must go too, whatever the
		// terminal status: the fresh-session retry that replaced it may well
		// have succeeded, and the pointer would otherwise still name the
		// transcript it retried away from.
		if t.ChatSessionID.Valid && retiredSessionID != "" {
			if err := qtx.ClearChatSessionSessionIfMatches(ctx, db.ClearChatSessionSessionIfMatchesParams{
				ID:        t.ChatSessionID,
				SessionID: pgtype.Text{String: retiredSessionID, Valid: true},
				RuntimeID: t.RuntimeID,
			}); err != nil {
				return fmt.Errorf("clear retired chat session resume pointer: %w", err)
			}
		}

		// ResumeUnsafeFailure, not resumeUnsafeFailureReason: the reason-only
		// check passes an un-upgraded daemon's agent_error.unknown row, which
		// would re-pin the very session the clear above just removed.
		if t.ChatSessionID.Valid && !ResumeUnsafeFailure(failureReason, errMsg) {
			// Pin the chat_session's runtime_id alongside the session_id so the
			// next claim can apply the runtime-guard. Both fields move together:
			// when there's no session_id to record, leave runtime_id untouched
			// (NULL → COALESCE keeps the existing value).
			var sessionRuntimeID pgtype.UUID
			if sessionID != "" {
				sessionRuntimeID = t.RuntimeID
			}
			if err := qtx.UpdateChatSessionSession(ctx, db.UpdateChatSessionSessionParams{
				ID:        t.ChatSessionID,
				SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""},
				WorkDir:   pgtype.Text{String: workDir, Valid: workDir != ""},
				RuntimeID: sessionRuntimeID,
			}); err != nil {
				return fmt.Errorf("update chat session resume pointer: %w", err)
			}
		}

		// Create the retry child atomically with the fail. CreateRetryTask reads
		// the just-failed parent row (same tx), so it inherits chat_input_run_id
		// and the bumped chat-retry priority; broadcast/notify happen after commit.
		if wantRetry {
			child, cerr := qtx.CreateRetryRun(ctx, db.CreateRetryRunParams{
				ID:                   runID,
				FireAt:               retryFireAt,
				MaxAttempts:          retryMaxAttempts,
				RuntimeMcpOverlay:    retryOverlay.Overlay,
				RuntimeConnectedApps: retryOverlay.ConnectedApps,
			})
			if cerr == nil {
				retried = &child
			} else if !errors.Is(cerr, pgx.ErrNoRows) || (!t.WorkflowStageExecutionID.Valid && !t.WorkflowReviewRequestID.Valid && !t.TaskID.Valid) {
				return fmt.Errorf("create retry run: %w", cerr)
			}
			// An ineligible SOP retry must not roll back the failed call.
		}
		return nil
	}); err != nil {
		if existing, lookupErr := s.Queries.GetAgentRun(ctx, runID); lookupErr == nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Info("fail run: already finalized",
					"run_id", util.UUIDToString(runID),
					"current_status", existing.Status,
					"agent_id", util.UUIDToString(existing.AgentID),
				)
				return &existing, nil
			}
			slog.Warn("fail task failed",
				"run_id", util.UUIDToString(runID),
				"current_status", existing.Status,
				"task_id", util.UUIDToString(existing.TaskID),
				"chat_session_id", util.UUIDToString(existing.ChatSessionID),
				"agent_id", util.UUIDToString(existing.AgentID),
				"error", err,
			)
		} else {
			slog.Warn("fail task failed: task not found",
				"run_id", util.UUIDToString(runID),
				"lookup_error", lookupErr,
			)
		}
		return nil, fmt.Errorf("fail run: %w", err)
	}

	slog.Warn("task failed", "run_id", util.UUIDToString(agentRun.ID), "task_id", util.UUIDToString(agentRun.TaskID), "error", errMsg, "failure_reason", failureReason)
	s.captureRunFailed(ctx, agentRun)

	// The auto-retry child (if any) was created inside the transaction above so
	// no newer chat task could jump ahead of it. Surface it now: broadcast
	// queued first, then notify the daemon — see EnqueueRunForTask for the
	// ordering rationale. A deferred child (backoff armed via fire_at) is NOT
	// queued yet: PromoteDueDeferredTasksForRuntime emits its queued event and
	// daemon wakeup when fire_at arrives, so announcing it here would be wrong.
	if retried != nil {
		slog.Info("task auto-retry enqueued",
			"parent_run_id", util.UUIDToString(agentRun.ID),
			"child_run_id", util.UUIDToString(retried.ID),
			"reason", failureReason,
			"attempt", retried.Attempt,
			"max_attempts", retried.MaxAttempts,
			"status", retried.Status,
		)
		if retried.Status == "queued" {
			s.broadcastRunEvent(ctx, protocol.EventRunQueued, *retried)
			s.NotifyRunEnqueued(ctx, *retried)
		}
	}

	// Skip the per-failure system comment when we'll immediately retry —
	// the new task will surface its own status to the user, and we don't
	// want to spam the task with "task timed out" messages on every
	// daemon hiccup.
	// Reviewer failures stay on the call: touching the delivery task would
	// change its evidence hash and accidentally trigger another review request.
	if errMsg != "" && agentRun.TaskID.Valid && retried == nil && !agentRun.WorkflowReviewRequestID.Valid {
		s.createAgentComment(ctx, agentRun.TaskID, agentRun.AgentID, redact.Text(errMsg), "system", agentRun.TriggerCommentID, agentRun.ID)
	}

	// Mirror the task fallback for chat tasks: write an assistant
	// chat_message tagged with the daemon-reported failure_reason so the
	// conversation history shows what happened. Skip when auto-retry is
	// pending (the new attempt will write its own outcome) — same guard as
	// the task path above.
	if agentRun.ChatSessionID.Valid && retried == nil {
		if _, err := s.Queries.CreateChatMessage(ctx, db.CreateChatMessageParams{
			ChatSessionID: agentRun.ChatSessionID,
			Role:          "assistant",
			Content:       redact.Text(errMsg),
			RunID:         pgtype.UUID{Bytes: agentRun.ID.Bytes, Valid: true},
			FailureReason: pgtype.Text{String: failureReason, Valid: failureReason != ""},
			ElapsedMs:     computeChatElapsedMs(agentRun),
			SenderAgentID: agentRun.AgentID,
		}); err != nil {
			slog.Error("failed to save failure chat message",
				"run_id", util.UUIDToString(agentRun.ID),
				"chat_session_id", util.UUIDToString(agentRun.ChatSessionID),
				"error", err)
		}
	}

	// Quick-create tasks: push a failure inbox notification to the
	// requester so they can either retry or fall back to the advanced form
	// without losing their original prompt. Skipped when an auto-retry is
	// pending — the new attempt will write its own outcome.
	if retried == nil {
		if qc, ok := s.parseQuickCreateContext(agentRun); ok {
			s.notifyQuickCreateFailed(ctx, agentRun, qc, errMsg)
		}
	}
	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, agentRun.AgentID)

	// Broadcast
	s.broadcastRunEvent(ctx, protocol.EventRunFailed, agentRun)

	return &agentRun, nil
}

// retryableReasons enumerates failure reasons that the auto-retry path is
// allowed to act on. Agent-side errors (compile failures, model rejections,
// etc.) are intentionally excluded — those are real problems that the user
// should see, not infrastructure flakiness.
//
// The one agent_error.* exception is provider_network: a mid-stream provider
// disconnect (e.g. Claude Code's "API Error: Connection closed mid-response")
// is transient infrastructure flakiness, not an agent decision. Unattended
// task runs otherwise terminate on it, while interactive chat only survives
// because the CLI's own in-process retry happens to recover first — so we make
// the platform retry it directly (SILI-4910). It is resume-safe (not in
// resumeUnsafeFailureReason), so the retry child inherits the session and
// continues the truncated conversation rather than restarting from scratch.
// skill_bundle_unavailable is retryable for the same reason: the agent process
// never started, so there is nothing to be idempotent about, and every bundle
// that did download is already cached on disk — a retry resumes from there
// instead of re-fetching the whole set (SILI-5370).
var retryableReasons = map[string]bool{
	"runtime_offline":           true,
	"runtime_recovery":          true,
	"timeout":                   true,
	"codex_semantic_inactivity": true,
	string(runfailure.ReasonAgentProviderNetwork):   true,
	string(runfailure.ReasonSkillBundleUnavailable): true,
}

// Transient provider stream cuts (provider_network) get a bespoke three-tier
// schedule (SILI-4910): first run + immediate retry + one retry deferred ~5s.
// A blip that survives the immediate retry gets a short cooldown before the
// final attempt instead of firing back-to-back. Every other retryable reason
// keeps the task's generic max_attempts ceiling and retries immediately.
const (
	providerNetworkMaxAttempts    = 3
	providerNetworkFinalRetryWait = 5 * time.Second
)

// retryAttemptCeiling reports how many attempts the auto-retry path allows for
// a failure reason. It only ever WIDENS the task's generic max_attempts, and
// only for reasons with a bespoke schedule; everything else keeps the column's
// value (default 2 = first run + one retry).
//
// max_attempts <= 1 explicitly disables auto-retry (055_task_lease_and_retry.up
// .sql: "1 disables retry"), so it is never overridden — a disabled task must
// not be revived by a raised ceiling. Callers persist this value into the retry
// child (CreateRetryTask's max_attempts) so the row stays self-consistent:
// provider_network's chain records attempt=3, max_attempts=3, not a
// contradictory attempt=3, max_attempts=2 (SILI-4910).
func retryAttemptCeiling(reason string, runMaxAttempts int32) int32 {
	if runMaxAttempts <= 1 {
		return runMaxAttempts
	}
	if reason == string(runfailure.ReasonAgentProviderNetwork) && runMaxAttempts < providerNetworkMaxAttempts {
		return providerNetworkMaxAttempts
	}
	return runMaxAttempts
}

// retryDelayForAttempt reports how long to defer the NEXT attempt after a
// failure at failedAttempt. Only provider_network's final attempt is deferred
// (~5s); every other retry — including provider_network's first — is immediate
// (zero delay → the child is created 'queued', claimable at once). Callers pass
// the returned delay to CreateRetryTask via fire_at.
func retryDelayForAttempt(reason string, failedAttempt int32) time.Duration {
	if reason == string(runfailure.ReasonAgentProviderNetwork) &&
		failedAttempt >= providerNetworkMaxAttempts-1 {
		return providerNetworkFinalRetryWait
	}
	return 0
}

func resumeUnsafeFailureReason(reason string) bool {
	switch reason {
	// Failures that poison the agent CONVERSATION (not the workdir): resuming
	// the same session would immediately replay the stuck/oversized state.
	// Keep in sync with the GetLastTaskSession / GetLastChatTaskSession resume
	// blacklists. (CreateRetryTask's fresh-session CASE WHEN only needs the
	// subset of these that is also auto-retryable, currently
	// codex_semantic_inactivity.)
	case "iteration_limit", "agent_fallback_message", "api_invalid_request", "codex_semantic_inactivity", "agent_error.context_overflow":
		return true
	default:
		return false
	}
}

// ResumeUnsafeFailure reports whether a failed task's agent session must NOT be
// resumed on a retry. It combines the failure_reason poison set
// (resumeUnsafeFailureReason) with the SAME defense-in-depth on raw error text
// that the GetLastTaskSession / GetLastChatTaskSession resume queries apply: an
// Anthropic 400 invalid_request_error means the conversation history itself is
// unprocessable even when failure_reason was mis- or un-classified (legacy
// 'agent_error' rows written before SILI-1921, or deploy-window rows). Callers
// that only have a failure_reason (e.g. at fail time) may pass an empty
// errorText.
//
// This is the shared source of truth for the manual-retry claim path, which
// reads the exact source task instead of GetLastTaskSession and would otherwise
// bypass the error-text guard.
func ResumeUnsafeFailure(failureReason, errorText string) bool {
	if resumeUnsafeFailureReason(failureReason) {
		return true
	}
	lower := strings.ToLower(errorText)
	if strings.Contains(lower, "400") && strings.Contains(lower, "invalid_request_error") {
		return true
	}
	// Same defense-in-depth for the provider-agnostic empty-message shape:
	// a daemon too old to carry classifyPoisonedError's new branch reports
	// agent_error.unknown, and without this the manual-retry path would
	// happily resume the transcript the provider just refused (GH #6066).
	return runfailure.UnresumableHistory(errorText)
}

// retryEligible reports whether a failed task qualifies for an automatic retry
// attempt: an infrastructure-shaped failure_reason, remaining attempt budget,
// not an autopilot run, and linked to an task or chat session. Shared by
// FailTask's in-transaction retry and the orphan sweeper's MaybeRetryFailedTask
// so both agree on which failures re-run.
func retryEligible(failureReason string, t db.AgentRun) bool {
	return retryEligibleWithCeiling(failureReason, t, retryAttemptCeiling(failureReason, t.MaxAttempts))
}

func retryEligibleWithCeiling(failureReason string, t db.AgentRun, ceiling int32) bool {
	return retryableReasons[failureReason] &&
		t.Attempt < ceiling &&
		!t.AutopilotRunID.Valid &&
		!t.WorkflowReviewRequestID.Valid &&
		(t.TaskID.Valid || t.ChatSessionID.Valid)
}

// MaybeRetryFailedTask spawns a fresh queued attempt for a recently-failed
// task when the failure was infrastructure-shaped (daemon crash, runtime
// went offline, dispatch/run timeout) and the task hasn't exhausted its
// max_attempts budget. The child task inherits agent/runtime/task/chat
// links and, for resume-safe failures, the parent's session_id/work_dir so
// the agent can resume the conversation when the backend supports it. Returns
// the new task, or nil when no retry was created.
//
// Review calls are excluded: their recovery must rebind review evidence and
// cannot use the ordinary task clone.
// Autopilot tasks are NOT auto-retried here; the autopilot scheduler owns
// its own re-run cadence and we don't want to double-fire it.
func (s *AgentRunService) MaybeRetryFailedRun(ctx context.Context, parent db.AgentRun) (*db.AgentRun, error) {
	if parent.Status != "failed" {
		return nil, nil
	}
	reason := ""
	if parent.FailureReason.Valid {
		reason = parent.FailureReason.String
	}
	if !retryableReasons[reason] {
		return nil, nil
	}
	ceiling, delay, policyErr := runRetrySettings(ctx, s.Queries, parent, reason)
	if policyErr != nil {
		return nil, policyErr
	}
	// Both failure callbacks and sweepers honor the immutable SOP policy.
	// Calls without a custom policy retain the reason-specific task defaults.
	if parent.Attempt >= ceiling {
		slog.Info("task auto-retry skipped: budget exhausted",
			"run_id", util.UUIDToString(parent.ID),
			"attempt", parent.Attempt,
			"max_attempts", parent.MaxAttempts,
			"ceiling", ceiling,
		)
		return nil, nil
	}
	// Autopilot has its own retry semantics (don't double-trigger) and a task
	// with no task/chat link has nowhere to report its retry — retryEligible
	// covers both, keeping this sweeper path in sync with FailTask's in-tx retry.
	if !retryEligibleWithCeiling(reason, parent, ceiling) {
		return nil, nil
	}

	var runtimeMCPOverlay runtimeMCPOverlayData
	agent, agentErr := s.Queries.GetAgent(ctx, parent.AgentID)
	if agentErr != nil {
		// Best-effort: failing to resolve the agent for the overlay is not
		// retry-fatal. Log and continue — the daemon will reject the claim
		// later if the agent is genuinely gone.
		slog.Warn("task auto-retry: load agent for overlay failed",
			"parent_run_id", util.UUIDToString(parent.ID),
			"agent_id", util.UUIDToString(parent.AgentID),
			"error", agentErr,
		)
	} else {
		runtimeMCPOverlay = s.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, agent)
	}
	// Mirror FailTask's in-tx backoff + effective-budget persistence: defer the
	// final provider_network attempt ~5s via fire_at (zero delay leaves fire_at
	// NULL for an immediate child), and write the reason-aware ceiling into the
	// child's max_attempts so the retry chain stays self-consistent.
	var retryFireAt pgtype.Timestamptz
	if delay > 0 {
		retryFireAt = pgtype.Timestamptz{Time: time.Now().Add(delay), Valid: true}
	}
	var child db.AgentRun
	err := s.runInTx(ctx, func(qtx *db.Queries) error {
		if err := lockWorkflowRunRetry(ctx, qtx, parent); err != nil {
			return err
		}
		var err error
		child, err = qtx.CreateRetryRun(ctx, db.CreateRetryRunParams{
			ID:                   parent.ID,
			FireAt:               retryFireAt,
			MaxAttempts:          pgtype.Int4{Int32: ceiling, Valid: true},
			RuntimeMcpOverlay:    runtimeMCPOverlay.Overlay,
			RuntimeConnectedApps: runtimeMCPOverlay.ConnectedApps,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) && (parent.WorkflowStageExecutionID.Valid || parent.TaskID.Valid) {
		return nil, nil
	}
	if err != nil {
		slog.Warn("task auto-retry failed",
			"parent_run_id", util.UUIDToString(parent.ID),
			"reason", reason,
			"error", err,
		)
		return nil, err
	}
	slog.Info("task auto-retry enqueued",
		"parent_run_id", util.UUIDToString(parent.ID),
		"child_run_id", util.UUIDToString(child.ID),
		"reason", reason,
		"attempt", child.Attempt,
		"max_attempts", child.MaxAttempts,
		"status", child.Status,
	)
	// A queued child transitions ∅ → queued (same as EnqueueTaskFor*): broadcast
	// queued first, then notify the daemon — see EnqueueRunForTask for ordering
	// rationale. A deferred child (backoff armed) stays inert until
	// PromoteDueDeferredTasksForRuntime fires its queued event + wakeup.
	if child.Status == "queued" {
		s.broadcastRunEvent(ctx, protocol.EventRunQueued, child)
		s.NotifyRunEnqueued(ctx, child)
	}
	return &child, nil
}

// RerunTask creates a fresh queued task for an agent on the task. Used by
// the manual rerun endpoint.
//
// Target agent resolution:
//   - sourceTaskID Valid: rerun the agent that ran that task (and reuse its
//     leader/worker role). This is what the execution log retry button uses
//     so a per-row retry survives a subsequent assignee change and correctly
//     re-fires the squad worker or mention agent whose row was clicked. The
//     source task's trigger_comment_id is also inherited (when the caller
//     didn't pass one) so a per-row rerun of a comment- or mention-triggered
//     task stays comment-triggered — the daemon's buildCommentPrompt path
//     keys on TriggerCommentID, and losing it would degrade the rerun into
//     a generic task run that no longer carries the original comment.
//   - sourceTaskID empty: fall back to the task's current assignee (agent
//     or squad leader). This preserves the CLI / API contract for callers
//     that have an task ID but no specific task to target.
//
// A retry ALWAYS reuses the source task's workdir when it still exists on
// disk (SILI-4869): a transient failure — network, provider 5xx/rate-limit,
// runtime_offline, timeout, or an auth/quota/config error the user has since
// fixed — should not throw away the work already done. Only the agent SESSION
// is conditionally resumed, and that decision is made later by the daemon claim
// handler from the SOURCE task (via rerun_of_run_id), NOT baked into this row.
// enqueueRerunTask pins force_fresh_session=true so an old claim handler during
// a rolling deploy degrades to a clean start rather than resuming a different
// execution; the new claim handler ignores the flag for reruns and resumes the
// session only when the source failure did not poison the conversation (see
// service.ResumeUnsafeFailure) and the source ran on the same runtime. When the
// dir is objectively unreusable (GC'd, absent on the claiming runtime, or never
// recorded) the daemon falls back to a fresh workdir. Auto-retry of an orphaned
// mid-flight failure (HandleFailedTasks → MaybeRetryFailedTask →
// CreateRetryTask) takes its own path, so SILI-1128's mid-flight resume contract
// is preserved.
//
// ErrRerunInvokeNotAllowed signals that RerunTask refused to rerun because the
// current operator may not invoke the resolved target agent. The handler maps it
// to a structured 403 (no task was cancelled or created).
var ErrRerunInvokeNotAllowed = errors.New("rerun: operator not allowed to invoke target agent")

// Only tasks belonging to the target agent on this task are cancelled.
// Tasks owned by other agents on the same task (e.g. a parallel
// @-mention agent) are left alone — rerun must not collateral-cancel
// them.
//
// canInvoke re-validates that the current operator may invoke the RESOLVED
// target agent, keyed on the historical agent for a run_id rerun and on the
// current assignee/leader otherwise (SILI-4525). It runs AFTER the target is
// resolved but BEFORE any prior task is cancelled or a new one is created, so a
// caller who can see the task but cannot invoke its private agent cannot use
// rerun as a back door — and a blocked rerun mutates nothing. Pass nil only
// from trusted internal callers (tests, backfill) that have already gated.
func (s *AgentRunService) RerunTask(ctx context.Context, taskID pgtype.UUID, sourceRunID pgtype.UUID, triggerCommentID pgtype.UUID, actorUserID pgtype.UUID, canInvoke func(agent db.Agent) bool) (*db.AgentRun, error) {
	task, err := s.Queries.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}

	// Determine the target agent for the rerun.
	var (
		agentID             pgtype.UUID
		isLeader            bool
		squadID             pgtype.UUID
		coalescedCommentIDs []pgtype.UUID
	)
	if sourceRunID.Valid {
		sourceRun, err := s.Queries.GetAgentRun(ctx, sourceRunID)
		if err != nil {
			return nil, fmt.Errorf("load source run: %w", err)
		}
		if !sourceRun.TaskID.Valid || util.UUIDToString(sourceRun.TaskID) != util.UUIDToString(taskID) {
			return nil, fmt.Errorf("source task does not belong to this task")
		}
		agentID = sourceRun.AgentID
		isLeader = sourceRun.IsLeaderRun
		// Carry the source task's squad provenance so a rerun of a leader
		// task still injects the squad briefing at claim time (see migration
		// 127 / daemon claim handler).
		squadID = sourceRun.SquadID
		// Inherit trigger provenance so a per-row rerun of a comment- or
		// mention-triggered task stays a comment-triggered task. Without
		// this the daemon's buildCommentPrompt path is skipped (it keys on
		// TriggerCommentID) and the rerun degrades into a generic task
		// run that has lost the original comment context. Only override
		// when the caller didn't pass one explicitly.
		if !triggerCommentID.Valid {
			coalescedCommentIDs = append([]pgtype.UUID{}, sourceRun.CoalescedCommentIds...)
			if sourceRun.TriggerCommentID.Valid {
				triggerCommentID = sourceRun.TriggerCommentID
			} else if len(coalescedCommentIDs) > 0 {
				triggerCommentID, coalescedCommentIDs, err = s.promoteNewestSurvivingComment(ctx, coalescedCommentIDs)
				if err != nil {
					return nil, fmt.Errorf("repair source comment plan: %w", err)
				}
			}
		}
	} else {
		switch {
		case task.AssigneeType.String == "agent" && task.AssigneeID.Valid:
			agentID = task.AssigneeID
		case task.AssigneeType.String == "squad" && task.AssigneeID.Valid:
			squad, err := s.Queries.GetSquad(ctx, task.AssigneeID)
			if err != nil {
				return nil, fmt.Errorf("task is assigned to a squad but squad not found")
			}
			agentID = squad.LeaderID
			isLeader = true
			squadID = task.AssigneeID
		default:
			return nil, fmt.Errorf("task is not assigned to an agent or squad")
		}
	}

	// Re-validate invoke permission on the RESOLVED target before mutating
	// anything (SILI-4525). For a run_id rerun this gates the historical agent,
	// so a since-reassigned task can't be used to re-fire a private agent the
	// operator may only view. A block fails closed: no prior task is cancelled,
	// no new task is created.
	if canInvoke != nil {
		targetAgent, err := s.Queries.GetAgent(ctx, agentID)
		if err != nil {
			return nil, fmt.Errorf("load target agent: %w", err)
		}
		if !canInvoke(targetAgent) {
			return nil, ErrRerunInvokeNotAllowed
		}
	}

	// Cancel only the target agent's active/queued tasks on this task.
	cancelled, err := s.Queries.CancelAgentRunsByTaskAndAgent(ctx, db.CancelAgentRunsByTaskAndAgentParams{
		TaskID:  taskID,
		AgentID: agentID,
	})
	if err != nil {
		slog.Warn("rerun: cancel prior tasks failed",
			"task_id", util.UUIDToString(taskID),
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
	}
	for _, t := range cancelled {
		s.captureRunCancelled(ctx, t)
		s.ReconcileAgentStatus(ctx, t.AgentID)
		s.broadcastRunEvent(ctx, protocol.EventRunCancelled, t)
	}

	// A manual rerun is a NEW direct_human trigger attributed to the rerunning
	// member, not the original run's human (SILI-4302 §5); actorUserID carries them.
	// sourceTaskID is the rerun lineage: it rides the CreateAgentTask insert
	// (rerun_of_run_id) so the queued event / daemon claim never sees a NULL
	// lineage, and it stays distinct from system-retry's retry_of_run_id (§5).
	agentRun, err := s.enqueueRerunRun(ctx, task, agentID, triggerCommentID, coalescedCommentIDs, isLeader, squadID, actorUserID, sourceRunID)
	if err != nil {
		return nil, err
	}
	slog.Info("task rerun enqueued",
		"run_id", util.UUIDToString(agentRun.ID),
		"task_id", util.UUIDToString(taskID),
		"agent_id", util.UUIDToString(agentID),
		"source_run_id", util.UUIDToString(sourceRunID),
		"is_leader", isLeader,
		"cancelled_prior", len(cancelled),
	)
	return &agentRun, nil
}

// promoteNewestSurvivingComment repairs a manual rerun whose original trigger
// was deleted (the FK clears trigger_comment_id while the UUID-array plan
// survives). Promoting before enqueue lets the normal enqueue path recompute
// originator and user-scoped connected-app capabilities from the real comment,
// rather than carrying the deleted trigger's stale security context.
func (s *AgentRunService) promoteNewestSurvivingComment(ctx context.Context, ids []pgtype.UUID) (pgtype.UUID, []pgtype.UUID, error) {
	type survivingComment struct {
		id        pgtype.UUID
		createdAt time.Time
	}
	survivors := make([]survivingComment, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		key := util.UUIDToString(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		comment, err := s.Queries.GetComment(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return pgtype.UUID{}, nil, err
		}
		survivors = append(survivors, survivingComment{id: comment.ID, createdAt: comment.CreatedAt.Time})
	}
	if len(survivors) == 0 {
		return pgtype.UUID{}, nil, nil
	}
	newest := 0
	for i := 1; i < len(survivors); i++ {
		if survivors[i].createdAt.After(survivors[newest].createdAt) ||
			(survivors[i].createdAt.Equal(survivors[newest].createdAt) &&
				util.UUIDToString(survivors[i].id) > util.UUIDToString(survivors[newest].id)) {
			newest = i
		}
	}
	remaining := make([]pgtype.UUID, 0, len(survivors)-1)
	for i, comment := range survivors {
		if i != newest {
			remaining = append(remaining, comment.id)
		}
	}
	return survivors[newest].id, remaining, nil
}

// enqueueRerunTask enqueues a fresh task for the given agent on the task.
// When the target agent is the task's single-agent assignee we use the
// assignee-driven path (enqueueTaskRun) so the task-assignee bookkeeping
// stays in sync; otherwise (squad member, prior assignee that has since been
// reassigned, mention agent) we use the mention path.
//
// force_fresh_session is pinned to true on every rerun row on purpose. It is
// the rollback-safe legacy signal: an OLD claim handler (mid rolling deploy)
// gates the whole resume lookup on !force_fresh_session, so it starts clean
// instead of resuming via the (agent, task) most-recent query — which could
// pick a different execution than the one the user clicked. The NEW claim
// handler ignores this flag for reruns and instead reads the exact source task
// (rerun_of_run_id) to reuse its workdir and, when the failure did not poison
// the conversation, resume its session (SILI-4869).
func (s *AgentRunService) enqueueRerunRun(ctx context.Context, task db.Task, agentID pgtype.UUID, triggerCommentID pgtype.UUID, coalescedCommentIDs []pgtype.UUID, isLeader bool, squadID pgtype.UUID, actorUserID pgtype.UUID, rerunOfRunID pgtype.UUID) (db.AgentRun, error) {
	if task.AssigneeType.String == "agent" && task.AssigneeID.Valid &&
		util.UUIDToString(task.AssigneeID) == util.UUIDToString(agentID) {
		return s.enqueueTaskRunWithCommentPlan(ctx, task, triggerCommentID, coalescedCommentIDs, true, "", actorUserID, rerunOfRunID)
	}
	return s.enqueueMentionRunWithCommentPlan(ctx, task, agentID, triggerCommentID, coalescedCommentIDs, isLeader, squadID, true, "", actorUserID, rerunOfRunID)
}

// HandleFailedTasks runs the post-failure side effects for a batch of
// freshly-failed tasks: optional auto-retry, run:failed event broadcast,
// agent status reconciliation, and (when an task has no remaining active
// task and isn't being retried) resetting the task back to todo so the
// daemon can pick it up again.
//
// All callers that surface a task as failed — sweepers, FailTask,
// recover-orphans — funnel through here so the same UI-consistency
// guarantees apply on every code path.
func (s *AgentRunService) HandleFailedRuns(ctx context.Context, agentRuns []db.AgentRun) int {
	if len(agentRuns) == 0 {
		return 0
	}

	affectedAgents := make(map[string]pgtype.UUID)
	processedTasks := make(map[string]bool)
	retriedTasks := make(map[string]bool)
	retried := 0

	for _, t := range agentRuns {
		// Auto-retry first so the task stays in_progress rather than
		// flapping todo → in_progress within a tick.
		if child, _ := s.MaybeRetryFailedRun(ctx, t); child != nil {
			retried++
			if t.TaskID.Valid {
				retriedTasks[util.UUIDToString(t.TaskID)] = true
			}
		}

		failureReason := "agent_error"
		if t.FailureReason.Valid && t.FailureReason.String != "" {
			failureReason = t.FailureReason.String
		}
		s.captureRunFailed(ctx, t)

		workspaceID := ""
		if t.TaskID.Valid {
			if task, err := s.Queries.GetTask(ctx, t.TaskID); err == nil {
				workspaceID = util.UUIDToString(task.WorkspaceID)
				// Reset stuck in_progress tasks only when no other active
				// task exists for the task and no retry was just enqueued.
				taskKey := util.UUIDToString(t.TaskID)
				if task.Status == "in_progress" && !processedTasks[taskKey] && !retriedTasks[taskKey] {
					processedTasks[taskKey] = true
					hasActive, checkErr := s.Queries.HasActiveRunForTask(ctx, t.TaskID)
					if checkErr != nil {
						slog.Warn("handle failed tasks: active check failed",
							"task_id", taskKey,
							"error", checkErr,
						)
					} else if !hasActive {
						workflowSvc := WorkflowExecutionService{Queries: s.Queries, TxStarter: s.TxStarter}
						updatedTask, updateErr := workflowSvc.UpdateTaskStatusWithWorkflowLock(ctx, task, "todo")
						if updateErr != nil {
							slog.Warn("handle failed tasks: reset stuck task failed",
								"task_id", taskKey,
								"error", updateErr,
							)
						} else {
							// This direct reset bypasses the HTTP UpdateTask
							// handler that normally emits task:updated, so emit
							// it here too. Without it the board / status-filter
							// caches keep showing the task as in_progress until
							// the next write touches it (#4648 / SILI-3782).
							s.broadcastTaskUpdated(updatedTask, task.Status)
						}
					}
				}
			}
		}
		if workspaceID == "" {
			workspaceID = s.ResolveRunWorkspaceID(ctx, t)
		}

		if workspaceID != "" {
			s.Bus.Publish(events.Event{
				Type:        protocol.EventRunFailed,
				WorkspaceID: workspaceID,
				ActorType:   "system",
				Payload: map[string]any{
					"run_id":         util.UUIDToString(t.ID),
					"agent_id":       util.UUIDToString(t.AgentID),
					"task_id":        util.UUIDToString(t.TaskID),
					"status":         "failed",
					"failure_reason": failureReason,
				},
			})
		}

		affectedAgents[util.UUIDToString(t.AgentID)] = t.AgentID
	}

	for _, agentID := range affectedAgents {
		s.ReconcileAgentStatus(ctx, agentID)
	}
	s.notifyRunsFinished(agentRuns)
	return retried
}

// runInTx executes fn inside a single DB transaction. If TxStarter is nil
// (e.g. some tests construct TaskService directly), fn runs against the
// regular Queries handle without transactional guarantees.
func (s *AgentRunService) runInTx(ctx context.Context, fn func(*db.Queries) error) error {
	if s.TxStarter == nil {
		return fn(s.Queries)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// runInTxWithHandle exposes the transaction for relationship locks that must
// remain held through cleanup. Destructive operations require a real transaction.
func (s *AgentRunService) runInTxWithHandle(ctx context.Context, fn func(*db.Queries, pgx.Tx) error) error {
	if s.TxStarter == nil {
		return fmt.Errorf("transaction starter is required for cleanup")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReportProgress broadcasts a progress update via the event bus.
func (s *AgentRunService) ReportProgress(ctx context.Context, agentRun db.AgentRun, workspaceID string, summary string, step, total int) {
	runID := util.UUIDToString(agentRun.ID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventRunProgress,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		RunID:       runID,
		Payload: protocol.RunProgressPayload{
			TaskID:        util.UUIDToString(agentRun.TaskID),
			ChatSessionID: util.UUIDToString(agentRun.ChatSessionID),
			RunID:         runID,
			Summary:       summary,
			Step:          step,
			Total:         total,
		},
	})
}

// ReconcileAgentStatus refreshes agent status from the current active task set.
func (s *AgentRunService) ReconcileAgentStatus(ctx context.Context, agentID pgtype.UUID) {
	agent, err := s.Queries.RefreshAgentStatusFromRuns(ctx, agentID)
	if err != nil {
		return
	}
	slog.Debug("agent status reconciled", "agent_id", util.UUIDToString(agentID), "status", agent.Status)
	s.publishAgentStatus(agent)
}

func (s *AgentRunService) updateAgentStatus(ctx context.Context, agentID pgtype.UUID, status string) {
	agent, err := s.Queries.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     agentID,
		Status: status,
	})
	if err != nil {
		return
	}
	s.publishAgentStatus(agent)
}

func (s *AgentRunService) publishAgentStatus(agent db.Agent) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: util.UUIDToString(agent.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload:     map[string]any{"agent": agentToMap(agent)},
	})
}

// LoadAgentSkills loads an agent's skills with their files for task execution.
func (s *AgentRunService) LoadAgentSkills(ctx context.Context, agentID pgtype.UUID) []AgentSkillData {
	skills, err := s.Queries.ListAgentSkills(ctx, agentID)
	if err != nil || len(skills) == 0 {
		return nil
	}

	result := make([]AgentSkillData, 0, len(skills))
	for _, sk := range skills {
		data := AgentSkillData{
			ID:          util.UUIDToString(sk.ID),
			Name:        sk.Name,
			Description: sk.Description,
			Content:     sk.Content,
		}
		files, _ := s.Queries.ListSkillFiles(ctx, sk.ID)
		for _, f := range files {
			data.Files = append(data.Files, AgentSkillFileData{Path: f.Path, Content: f.Content})
		}
		result = append(result, data)
	}
	return result
}

// LoadAgentSkillBundles returns every skill visible to an agent, including
// built-ins, with stable bundle hashes and lightweight refs for slim claims.
func (s *AgentRunService) LoadAgentSkillBundles(ctx context.Context, agentID pgtype.UUID) ([]AgentSkillData, []AgentSkillRefData) {
	skills := s.LoadAgentSkills(ctx, agentID)
	skills = append(skills, s.BuiltinSkills()...)
	return BuildAgentSkillBundles(skills)
}

// ResolveRequiredSkills resolves the stable references stored on an SOP Stage.
// Built-ins may be referenced as builtin:<name> (preferred) or by their legacy
// name. Workspace skills may be referenced by UUID (preferred) or legacy name.
// The caller must fail closed when missing is non-empty.
func (s *AgentRunService) ResolveRequiredSkills(ctx context.Context, workspaceID pgtype.UUID, required []string) (skills []AgentSkillData, missing []string) {
	builtins := make(map[string]AgentSkillData)
	for _, skill := range s.BuiltinSkills() {
		builtins[skill.Name] = skill
	}

	seen := make(map[string]bool)
	for _, raw := range required {
		ref := strings.TrimSpace(raw)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true

		builtinName := strings.TrimPrefix(ref, "builtin:")
		if skill, ok := builtins[builtinName]; ok && (strings.HasPrefix(ref, "builtin:") || ref == builtinName) {
			skills = append(skills, skill)
			continue
		}

		var row db.Skill
		var err error
		if id, parseErr := util.ParseUUID(ref); parseErr == nil {
			row, err = s.Queries.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
		} else {
			row, err = s.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{WorkspaceID: workspaceID, Name: ref})
		}
		if err != nil {
			missing = append(missing, ref)
			continue
		}
		data := AgentSkillData{ID: util.UUIDToString(row.ID), Source: skillbundle.SourceWorkspace, Name: row.Name, Description: row.Description, Content: row.Content}
		files, _ := s.Queries.ListSkillFiles(ctx, row.ID)
		for _, file := range files {
			data.Files = append(data.Files, AgentSkillFileData{Path: file.Path, Content: file.Content})
		}
		skills = append(skills, data)
	}
	return skills, missing
}

func BuildAgentSkillBundles(skills []AgentSkillData) ([]AgentSkillData, []AgentSkillRefData) {
	bundles := make([]AgentSkillData, 0, len(skills))
	refs := make([]AgentSkillRefData, 0, len(skills))
	for _, skill := range skills {
		source := skill.Source
		id := skill.ID
		if source == "" {
			if id == "" {
				source = skillbundle.SourceBuiltin
			} else {
				source = skillbundle.SourceWorkspace
			}
		}
		if id == "" && source == skillbundle.SourceBuiltin {
			id = "builtin:" + skill.Name
		}
		skill.Source = source
		skill.ID = id

		files := make([]skillbundle.File, 0, len(skill.Files))
		for _, file := range skill.Files {
			files = append(files, skillbundle.File{Path: file.Path, Content: file.Content})
		}
		manifest := skillbundle.BuildManifest(skillbundle.Skill{
			ID:          skill.ID,
			Source:      skill.Source,
			Name:        skill.Name,
			Description: skill.Description,
			Content:     skill.Content,
			Files:       files,
		})
		skill.Hash = manifest.Hash
		skill.SizeBytes = manifest.SizeBytes
		fileRefsByPath := make(map[string]skillbundle.FileRef, len(manifest.Files))
		for _, file := range manifest.Files {
			fileRefsByPath[file.Path] = file
		}
		for i := range skill.Files {
			if ref, ok := fileRefsByPath[skill.Files[i].Path]; ok {
				skill.Files[i].SHA256 = ref.SHA256
				skill.Files[i].SizeBytes = ref.SizeBytes
			}
		}
		bundles = append(bundles, skill)

		refFiles := make([]AgentSkillFileRefData, 0, len(manifest.Files))
		for _, file := range manifest.Files {
			refFiles = append(refFiles, AgentSkillFileRefData{
				Path:      file.Path,
				SHA256:    file.SHA256,
				SizeBytes: file.SizeBytes,
			})
		}
		refs = append(refs, AgentSkillRefData{
			ID:          skill.ID,
			Source:      skill.Source,
			Name:        skill.Name,
			Description: skill.Description,
			Hash:        manifest.Hash,
			SizeBytes:   manifest.SizeBytes,
			FileCount:   manifest.FileCount,
			Files:       refFiles,
		})
	}
	return bundles, refs
}

// AgentSkillData represents a skill for task execution responses.
type AgentSkillData struct {
	ID          string               `json:"id"`
	Source      string               `json:"source,omitempty"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Hash        string               `json:"hash,omitempty"`
	SizeBytes   int64                `json:"size_bytes,omitempty"`
	Content     string               `json:"content"`
	Files       []AgentSkillFileData `json:"files,omitempty"`
}

// AgentSkillFileData represents a supporting file within a skill.
type AgentSkillFileData struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SHA256    string `json:"sha256,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type AgentSkillRefData struct {
	ID          string                  `json:"id"`
	Source      string                  `json:"source"`
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	Hash        string                  `json:"hash"`
	SizeBytes   int64                   `json:"size_bytes"`
	FileCount   int                     `json:"file_count"`
	Files       []AgentSkillFileRefData `json:"files,omitempty"`
}

type AgentSkillFileRefData struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// computeChatElapsedMs returns the wall-clock duration from task creation
// (user hit send) to terminal state (completed/failed). Stored on the
// assistant chat_message so the UI can render "Replied in 38s" /
// "Failed after 12s". Uses created_at — not started_at — because users
// experience total wait time, including queue + dispatch, not just the
// daemon's actual run time.
func computeChatElapsedMs(agentRun db.AgentRun) pgtype.Int8 {
	if !agentRun.CompletedAt.Valid || !agentRun.CreatedAt.Valid {
		return pgtype.Int8{}
	}
	ms := agentRun.CompletedAt.Time.Sub(agentRun.CreatedAt.Time).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return pgtype.Int8{Int64: ms, Valid: true}
}

func priorityToInt(p string) int32 {
	switch p {
	case "urgent":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// NotifyTaskEnqueued is the cross-package shim for callers outside
// TaskService (e.g. AutopilotService.dispatchRunOnly) that insert a
// row into agent_run directly. Invalidates the empty-claim
// cache and kicks the daemon WS so the new task is claimed without
// waiting for the next poll.
func (s *AgentRunService) NotifyRunEnqueued(ctx context.Context, agentRun db.AgentRun) {
	s.captureRunQueued(ctx, agentRun)
	s.notifyRunAvailable(agentRun)
}

// NotifyTaskFinished invalidates a runtime's empty-claim verdict and emits a
// best-effort daemon wakeup after a task reaches a terminal state. The task ID
// is deliberately omitted from the wakeup payload: the completed task itself
// is not available; the hint only means that a queued successor may have
// become claimable because an agent-capacity or serialization barrier cleared.
func (s *AgentRunService) NotifyRunFinished(agentRun db.AgentRun) {
	s.notifyRuntimeMayHaveWork(agentRun.RuntimeID, "")
}

// notifyTasksFinished is the batch form used by bulk terminal transitions.
// Coalesce by runtime so cancelling many tasks on one machine produces one
// cache bump and one websocket hint rather than a burst of identical work.
func (s *AgentRunService) notifyRunsFinished(agentRuns []db.AgentRun) {
	seen := make(map[string]struct{}, len(agentRuns))
	for _, agentRun := range agentRuns {
		if !agentRun.RuntimeID.Valid {
			continue
		}
		runtimeKey := util.UUIDToString(agentRun.RuntimeID)
		if _, ok := seen[runtimeKey]; ok {
			continue
		}
		seen[runtimeKey] = struct{}{}
		s.notifyRuntimeMayHaveWork(agentRun.RuntimeID, "")
	}
}

// notifyTaskAvailable runs after a task has been inserted: bumps the
// runtime's invalidation version so any in-flight claim that is about
// to write an "empty" verdict will have it rejected on read, then
// kicks the daemon WS so the daemon claims without waiting for its
// next poll. Order matters — Bump must happen before the wakeup,
// otherwise the wakeup-driven claim could read the still-current
// empty verdict and return null.
func (s *AgentRunService) notifyRunAvailable(agentRun db.AgentRun) {
	s.notifyRuntimeMayHaveWork(agentRun.RuntimeID, util.UUIDToString(agentRun.ID))
}

// notifyRuntimeMayHaveWork is the shared bump-before-wakeup primitive for both
// fresh enqueues and terminal transitions that can unblock queued work.
func (s *AgentRunService) notifyRuntimeMayHaveWork(runtimeID pgtype.UUID, runID string) {
	if !runtimeID.Valid {
		return
	}
	runtimeKey := util.UUIDToString(runtimeID)
	// Use a background context: the cache bump / wakeup must outlive
	// the request that created the task, otherwise an early client
	// disconnect could leave the empty verdict in place and stall the
	// just-queued task until the TTL expires. The cache itself bounds
	// every Redis call with a short timeout so a wedged Redis cannot
	// block enqueue.
	s.EmptyClaim.Bump(context.Background(), runtimeKey)
	if s.Wakeup == nil {
		return
	}
	s.Wakeup.NotifyRunAvailable(runtimeKey, runID)
}

func (s *AgentRunService) broadcastRunDispatch(ctx context.Context, agentRun db.AgentRun) {
	var payload map[string]any
	if agentRun.Context != nil {
		json.Unmarshal(agentRun.Context, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["run_id"] = util.UUIDToString(agentRun.ID)
	payload["runtime_id"] = util.UUIDToString(agentRun.RuntimeID)
	payload["task_id"] = util.UUIDToString(agentRun.TaskID)
	payload["agent_id"] = util.UUIDToString(agentRun.AgentID)
	// chat_session_id is the routing key the chat window uses to writethrough
	// `chatKeys.pendingTask` to status="running" the moment the daemon claims
	// the task. Without it the pill stays stuck at "Queued" until completion.
	if agentRun.ChatSessionID.Valid {
		payload["chat_session_id"] = util.UUIDToString(agentRun.ChatSessionID)
	}

	workspaceID := s.ResolveRunWorkspaceID(ctx, agentRun)
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventRunDispatch,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     payload,
	})
}

func (s *AgentRunService) broadcastRunEvent(ctx context.Context, eventType string, agentRun db.AgentRun) {
	workspaceID := s.ResolveRunWorkspaceID(ctx, agentRun)
	if workspaceID == "" {
		return
	}
	payload := map[string]any{
		"run_id":   util.UUIDToString(agentRun.ID),
		"agent_id": util.UUIDToString(agentRun.AgentID),
		"task_id":  util.UUIDToString(agentRun.TaskID),
		"status":   agentRun.Status,
	}
	if agentRun.ChatSessionID.Valid {
		payload["chat_session_id"] = util.UUIDToString(agentRun.ChatSessionID)
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     payload,
	})
}

// ResolveRunWorkspaceID determines the workspace ID for an agent run.
// For task runs, it comes from the task. For chat runs, from the chat session.
// For autopilot runs, from the autopilot via its automation run.
// Returns "" when none of the links resolve — callers treat that as "not found".
func (s *AgentRunService) ResolveRunWorkspaceID(ctx context.Context, agentRun db.AgentRun) string {
	if agentRun.TaskID.Valid {
		if task, err := s.Queries.GetTask(ctx, agentRun.TaskID); err == nil {
			return util.UUIDToString(task.WorkspaceID)
		}
	}
	if agentRun.ChatSessionID.Valid {
		if cs, err := s.Queries.GetChatSession(ctx, agentRun.ChatSessionID); err == nil {
			return util.UUIDToString(cs.WorkspaceID)
		}
	}
	if agentRun.AutopilotRunID.Valid {
		if run, err := s.Queries.GetAutopilotRun(ctx, agentRun.AutopilotRunID); err == nil {
			if ap, err := s.Queries.GetAutopilot(ctx, run.AutopilotID); err == nil {
				return util.UUIDToString(ap.WorkspaceID)
			}
		}
	}
	// Quick-create tasks have no task / chat / autopilot link — workspace
	// lives in the context JSONB. Returning "" here is what blocked
	// requireDaemonTaskAccess (404 on /start, /progress, /complete, /fail
	// for the daemon) and silently dropped run:dispatch / run:completed
	// broadcasts, which is why quick-create tasks appeared stuck queued.
	if qc, ok := s.parseQuickCreateContext(agentRun); ok {
		return qc.WorkspaceID
	}
	return ""
}

func (s *AgentRunService) broadcastChatDone(ctx context.Context, agentRun db.AgentRun, msg *db.ChatMessage) {
	workspaceID := s.ResolveRunWorkspaceID(ctx, agentRun)
	if workspaceID == "" {
		return
	}
	payload := protocol.ChatDonePayload{
		ChatSessionID: util.UUIDToString(agentRun.ChatSessionID),
		RunID:         util.UUIDToString(agentRun.ID),
	}
	if msg != nil {
		payload.MessageID = util.UUIDToString(msg.ID)
		payload.Content = msg.Content
		payload.MessageKind = msg.MessageKind
		payload.SenderAgentID = util.UUIDToString(msg.SenderAgentID)
		if msg.CreatedAt.Valid {
			payload.CreatedAt = msg.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if msg.ElapsedMs.Valid {
			payload.ElapsedMs = msg.ElapsedMs.Int64
		}
	}
	s.Bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		WorkspaceID:   workspaceID,
		ActorType:     "system",
		ActorID:       "",
		ChatSessionID: util.UUIDToString(agentRun.ChatSessionID),
		Payload:       payload,
	})
}

// broadcastTaskUpdated publishes the task:updated event the frontend's
// realtime reconcile (onTaskUpdated) relies on to move an task between status
// columns / status filters and reconcile their bucket counts. prevStatus is the
// task's status before the write so the client can gate that reconcile on
// status_changed.
//
// The `task` payload is a map (issueToMap), which the workspace WS fanout
// (listeners.go SubscribeAll) marshals and broadcasts as-is — that is what
// drives the UI reconcile. Note this does NOT cover the full HTTP UpdateTask
// side effects: the activity-log and inbox listeners type-assert `task` to a
// handler.TaskResponse and skip a map, so a background status reset does not
// emit status-change activity / notifications. That is intentional for the
// realtime-staleness fix (#4648 / SILI-3782); folding those side effects in
// would mean unifying the payload type and is left as a follow-up.
func (s *AgentRunService) broadcastTaskUpdated(task db.Task, prevStatus string) {
	prefix := s.getTaskPrefix(task.WorkspaceID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskUpdated,
		WorkspaceID: util.UUIDToString(task.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task":           taskToMap(task, prefix),
			"status_changed": prevStatus != task.Status,
			"prev_status":    prevStatus,
		},
	})
}

func (s *AgentRunService) getTaskPrefix(workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.TaskPrefix
}

func (s *AgentRunService) createAgentComment(ctx context.Context, taskID, agentID pgtype.UUID, content, commentType string, parentID, sourceRunID pgtype.UUID) {
	if content == "" {
		return
	}
	// Look up task to get workspace ID for mention expansion and broadcasting.
	task, err := s.Queries.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	// Resolve the thread root for thread-level side effects without overwriting
	// parentID. The stored parent_id must remain the exact comment being replied
	// to; recursive thread reads recover the root when needed.
	var rootComment *db.Comment
	if parentID.Valid {
		if root, err := s.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{
			CommentID:   parentID,
			WorkspaceID: task.WorkspaceID,
		}); err == nil {
			rootComment = &root
		}
	}
	comment, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		TaskID:      taskID,
		WorkspaceID: task.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    agentID,
		Content:     content,
		Type:        commentType,
		ParentID:    parentID,
		SourceRunID: sourceRunID,
	})
	if err != nil {
		return
	}
	s.CancelDeferredEscalationsForTaskAgent(ctx, taskID, agentID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(task.WorkspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(agentID),
		Payload: map[string]any{
			"comment": map[string]any{
				"id":            util.UUIDToString(comment.ID),
				"task_id":       util.UUIDToString(comment.TaskID),
				"author_type":   comment.AuthorType,
				"author_id":     util.UUIDToString(comment.AuthorID),
				"content":       comment.Content,
				"type":          comment.Type,
				"parent_id":     util.UUIDToPtr(comment.ParentID),
				"source_run_id": util.UUIDToPtr(comment.SourceRunID),
				"created_at":    comment.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
			},
			"task_title":  task.Title,
			"task_status": task.Status,
		},
	})
	s.AutoUnresolveThreadOnReply(ctx, rootComment, util.UUIDToString(task.WorkspaceID), "agent", util.UUIDToString(agentID))
}

// AutoUnresolveThreadOnReply clears resolved_at on the thread root when a
// reply lands in a resolved thread, and broadcasts comment:unresolved. Shared
// between the user-facing Handler.CreateComment path and the agent-facing
// TaskService.createAgentComment path so the resolved-then-replied state can
// never desync (one of the bugs Emacs flagged on PR #2300). Errors are logged
// — the reply itself already committed, the desync is recoverable on next read.
func (s *AgentRunService) AutoUnresolveThreadOnReply(ctx context.Context, parent *db.Comment, workspaceID, actorType, actorID string) {
	if parent == nil || !parent.ResolvedAt.Valid {
		return
	}
	updated, err := s.Queries.UnresolveComment(ctx, parent.ID)
	if err != nil {
		slog.Warn("auto-unresolve on reply failed", "error", err, "comment_id", util.UUIDToString(parent.ID))
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentUnresolved,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload: map[string]any{
			"comment": map[string]any{
				"id":               util.UUIDToString(updated.ID),
				"task_id":          util.UUIDToString(updated.TaskID),
				"author_type":      updated.AuthorType,
				"author_id":        util.UUIDToString(updated.AuthorID),
				"content":          updated.Content,
				"type":             updated.Type,
				"parent_id":        util.UUIDToPtr(updated.ParentID),
				"created_at":       util.TimestampToString(updated.CreatedAt),
				"updated_at":       util.TimestampToString(updated.UpdatedAt),
				"resolved_at":      util.TimestampToPtr(updated.ResolvedAt),
				"resolved_by_type": util.TextToPtr(updated.ResolvedByType),
				"resolved_by_id":   util.UUIDToPtr(updated.ResolvedByID),
			},
		},
	})
}

func taskToMap(task db.Task, taskPrefix string) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(task.ID),
		"workspace_id":   util.UUIDToString(task.WorkspaceID),
		"number":         task.Number,
		"identifier":     taskPrefix + "-" + strconv.Itoa(int(task.Number)),
		"title":          task.Title,
		"description":    util.TextToPtr(task.Description),
		"status":         task.Status,
		"priority":       task.Priority,
		"assignee_type":  util.TextToPtr(task.AssigneeType),
		"assignee_id":    util.UUIDToPtr(task.AssigneeID),
		"creator_type":   task.CreatorType,
		"creator_id":     util.UUIDToString(task.CreatorID),
		"parent_task_id": util.UUIDToPtr(task.ParentTaskID),
		"position":       task.Position,
		"start_date":     util.DateToPtr(task.StartDate),
		"due_date":       util.DateToPtr(task.DueDate),
		"created_at":     util.TimestampToString(task.CreatedAt),
		"updated_at":     util.TimestampToString(task.UpdatedAt),
	}
}

// parseQuickCreateContext returns the quick-create payload if the task's
// context JSONB contains type == "quick_create"; otherwise the bool is
// false so callers can short-circuit. Tasks linked to an task / chat /
// autopilot are never quick-create even if they happen to carry a
// context blob, so those are filtered up front.
func (s *AgentRunService) parseQuickCreateContext(agentRun db.AgentRun) (QuickCreateContext, bool) {
	if agentRun.TaskID.Valid || agentRun.ChatSessionID.Valid || agentRun.AutopilotRunID.Valid {
		return QuickCreateContext{}, false
	}
	if len(agentRun.Context) == 0 {
		return QuickCreateContext{}, false
	}
	var qc QuickCreateContext
	if err := json.Unmarshal(agentRun.Context, &qc); err != nil {
		return QuickCreateContext{}, false
	}
	if qc.Type != QuickCreateContextType {
		return QuickCreateContext{}, false
	}
	return qc, true
}

// maxQuickCreateFailureDetailRunes bounds the failure reason lifted from a
// quick-create task's final output. A genuine CLI error (a duplicate message,
// a validation error) is short; an output far larger than this is a runaway
// raw-stream dump whose head is process narration rather than the real reason
// (same failure mode as GH #5455), so it is dropped to a generic notice rather
// than surfaced as the error.
const maxQuickCreateFailureDetailRunes = 2000

const quickCreateOversizedFailureDetail = "Quick create failed, but the agent's output was too large to show the reason safely. Check the task's execution log for details."

// quickCreateFailureDetail extracts a user-facing failure reason from a
// quick-create task's final output. The quick-create prompt instructs the agent
// to exit with the CLI error as its only output when `silieco task create`
// fails, so this normally carries the real reason (e.g. an active-duplicate
// message naming the existing task). Returns "" when there is no usable output
// so the caller falls back to a generic message; redaction is applied by
// notifyQuickCreateFailed.
func quickCreateFailureDetail(result []byte) string {
	var payload protocol.RunCompletedPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		return ""
	}
	// Same unescape as the comment-fallback path: literal `\n` sequences from
	// agent stdout become real newlines before the reason reaches the user.
	body := strings.TrimSpace(util.UnescapeBackslashEscapes(payload.Output))
	if body == "" {
		return ""
	}
	if utf8.RuneCountInString(body) > maxQuickCreateFailureDetailRunes {
		return quickCreateOversizedFailureDetail
	}
	return body
}

// notifyQuickCreateCompleted writes a success inbox notification to the
// requester pointing at the task the agent just created. The task is
// stamped with origin_type=quick_create + origin_id=<run_id> by the
// daemon-injected SILIECO_QUICK_CREATE_TASK_ID env var, so this lookup is
// deterministic — robust against the same agent creating other tasks in
// parallel (e.g. assignment task running while max_concurrent_runs > 1
// permits another quick-create alongside it).
func (s *AgentRunService) notifyQuickCreateCompleted(ctx context.Context, agentRun db.AgentRun, qc QuickCreateContext, result []byte) {
	requesterID, err := util.ParseUUID(qc.RequesterID)
	if err != nil {
		slog.Warn("quick-create completion: invalid requester id", "run_id", util.UUIDToString(agentRun.ID), "error", err)
		return
	}
	workspaceID, err := util.ParseUUID(qc.WorkspaceID)
	if err != nil {
		slog.Warn("quick-create completion: invalid workspace id", "run_id", util.UUIDToString(agentRun.ID), "error", err)
		return
	}
	task, err := s.Queries.GetTaskByOrigin(ctx, db.GetTaskByOriginParams{
		WorkspaceID: workspaceID,
		OriginType:  pgtype.Text{String: "quick_create", Valid: true},
		OriginID:    agentRun.ID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			// The lookup itself failed (DB fault, timeout, …), not a confirmed
			// "no task": the agent may well have created the task, so a
			// failure inbox would misreport it. But the task is already
			// completed and nothing retries this reconciliation, so returning
			// silently would end the run with NO inbox result at all. Write a
			// neutral, terminal notification instead — the user always gets a
			// result, and it never asserts a failure we did not observe.
			slog.Error("quick-create completion: task lookup failed, writing unconfirmed inbox",
				"run_id", util.UUIDToString(agentRun.ID),
				"agent_id", util.UUIDToString(agentRun.AgentID),
				"workspace_id", qc.WorkspaceID,
				"error", err,
			)
			s.notifyQuickCreateUnconfirmed(ctx, agentRun, qc)
			return
		}
		// No task created — the agent ran to completion but the CLI create
		// call must have failed (most often the active-duplicate guard). The
		// quick-create prompt tells the agent to exit with the CLI error as its
		// only output, so prefer that as the failure reason instead of a
		// generic string; fall back to notifyQuickCreateFailed's own default
		// when the output is empty. This is what turns the opaque "agent
		// finished without creating an task" into the concrete reason (#5885).
		detail := quickCreateFailureDetail(result)
		slog.Warn("quick-create completion: no task found, writing failure inbox",
			"run_id", util.UUIDToString(agentRun.ID),
			"agent_id", util.UUIDToString(agentRun.AgentID),
			"workspace_id", qc.WorkspaceID,
			"has_detail", detail != "",
		)
		s.notifyQuickCreateFailed(ctx, agentRun, qc, detail)
		return
	}

	// Link the new task back to this run so subsequent reads of the run
	// (Activity tab, Recent work, etc.) render it as a normal task run
	// (kind = "direct") instead of staying on the "Creating task" active-
	// wording label. Best-effort: a write failure here doesn't block the
	// inbox notification, which is the more important signal to the user.
	if err := s.Queries.LinkRunToTask(ctx, db.LinkRunToTaskParams{
		ID:     agentRun.ID,
		TaskID: task.ID,
	}); err != nil {
		slog.Warn("quick-create completion: link task→task failed",
			"run_id", util.UUIDToString(agentRun.ID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
	}

	// Subscribe the requester so they receive notifications for follow-up
	// comments and updates. The DB row's creator_type/creator_id is the
	// agent (it ran the CLI), but the human who triggered the quick-create
	// is the semantic creator from a UX perspective — without this they
	// only see the one-shot completion inbox and miss everything after.
	// Best-effort: log on failure but don't block the inbox notification.
	if err := s.Queries.AddTaskSubscriber(ctx, db.AddTaskSubscriberParams{
		TaskID:   task.ID,
		UserType: "member",
		UserID:   requesterID,
		Reason:   "creator",
	}); err != nil {
		slog.Warn("quick-create completion: subscribe requester failed",
			"run_id", util.UUIDToString(agentRun.ID),
			"task_id", util.UUIDToString(task.ID),
			"requester_id", qc.RequesterID,
			"error", err,
		)
	} else {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventSubscriberAdded,
			WorkspaceID: qc.WorkspaceID,
			ActorType:   "agent",
			ActorID:     util.UUIDToString(agentRun.AgentID),
			Payload: map[string]any{
				"task_id":   util.UUIDToString(task.ID),
				"user_type": "member",
				"user_id":   qc.RequesterID,
				"reason":    "creator",
			},
		})
	}
	prefix := s.getTaskPrefix(workspaceID)
	identifier := fmt.Sprintf("%s-%d", prefix, task.Number)
	details, _ := json.Marshal(map[string]any{
		"run_id":          util.UUIDToString(agentRun.ID),
		"agent_id":        util.UUIDToString(agentRun.AgentID),
		"task_id":         util.UUIDToString(task.ID),
		"identifier":      identifier,
		"original_prompt": qc.Prompt,
	})
	item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   requesterID,
		Type:          "quick_create_done",
		Severity:      "info",
		TaskID:        task.ID,
		Title:         task.Title,
		Body:          pgtype.Text{},
		ActorType:     pgtype.Text{String: "agent", Valid: true},
		ActorID:       agentRun.AgentID,
		Details:       details,
	})
	if err != nil {
		slog.Error("quick-create completion: inbox write failed", "run_id", util.UUIDToString(agentRun.ID), "error", err)
		return
	}
	s.publishQuickCreateInbox(item, qc.WorkspaceID, util.UUIDToString(agentRun.AgentID), task.Status)
}

// Inbox types for the two non-success quick-create outcomes. They are distinct
// because clients render them differently: the failed type carries an explicit
// "Failed:" framing, which must not be applied to an outcome we could not
// verify. Older clients that predate the unconfirmed type fall through their
// existing default branch and render the row's title/body unchanged, which is
// already the neutral wording.
const (
	inboxTypeQuickCreateFailed      = "quick_create_failed"
	inboxTypeQuickCreateUnconfirmed = "quick_create_unconfirmed"
)

// notifyQuickCreateFailed writes a failure inbox notification carrying the
// original prompt + agent ID so the frontend can render an "Edit as
// advanced form" entry that pre-fills the legacy create-task modal
// without asking the user to retype. Use this only when the run is KNOWN not
// to have produced an task; when that is merely unverified, use
// notifyQuickCreateUnconfirmed so the user is not told a definite failure.
func (s *AgentRunService) notifyQuickCreateFailed(ctx context.Context, agentRun db.AgentRun, qc QuickCreateContext, errMsg string) {
	if errMsg == "" {
		errMsg = "Quick create did not finish successfully"
	}
	s.writeQuickCreateOutcomeInbox(ctx, agentRun, qc, inboxTypeQuickCreateFailed, "Quick create failed", errMsg)
}

// quickCreateUnconfirmedDetail is the user-facing message for a quick-create
// run whose outcome could not be verified (the completion lookup itself
// failed). It must not claim failure: the agent may well have created the
// task. It points at the one safe next step instead — check before retrying,
// so a retry cannot silently produce the duplicate the guard exists to prevent.
const quickCreateUnconfirmedDetail = "Couldn't confirm whether the task was created. Check your recent tasks before retrying — creating it again may produce a duplicate."

// notifyQuickCreateUnconfirmed writes a NEUTRAL terminal notification for a
// quick-create run whose outcome is unverified. The task is already completed
// and nothing re-runs this reconciliation, so returning without writing here
// would strand the requester with no inbox result at all — the failure mode
// this exists to prevent.
//
// It uses its own inbox type rather than reusing quick_create_failed: every
// client renders the failed type with a "Failed:" prefix, which would assert a
// failure we never observed no matter how neutral the title and body are.
// Severity stays action_required because the user does need to look.
func (s *AgentRunService) notifyQuickCreateUnconfirmed(ctx context.Context, agentRun db.AgentRun, qc QuickCreateContext) {
	s.writeQuickCreateOutcomeInbox(ctx, agentRun, qc, inboxTypeQuickCreateUnconfirmed, "Quick create needs a check", quickCreateUnconfirmedDetail)
}

// quickCreateNotifyTimeout bounds the detached terminal-notification write in
// writeQuickCreateOutcomeInbox. Long enough for a healthy DB round-trip, short
// enough that a wedged pool cannot pin the completion goroutine.
const quickCreateNotifyTimeout = 5 * time.Second

// writeQuickCreateOutcomeInbox writes the shared inbox row used by both
// non-success outcomes (known failure and unverified outcome). Callers own the
// user-facing wording and the row type; the row shape — original prompt, agent
// id, redacted message — is identical so the frontend's recovery affordance
// keeps working for both.
func (s *AgentRunService) writeQuickCreateOutcomeInbox(ctx context.Context, agentRun db.AgentRun, qc QuickCreateContext, inboxType, title, errMsg string) {
	requesterID, err := util.ParseUUID(qc.RequesterID)
	if err != nil {
		return
	}
	workspaceID, err := util.ParseUUID(qc.WorkspaceID)
	if err != nil {
		return
	}
	// The task is already committed as completed and nothing retries this
	// notification, so it must not die with the caller's context. This matters
	// most on the unconfirmed path: the completion lookup may have failed
	// precisely BECAUSE ctx was cancelled or timed out, and reusing that ctx
	// would fail the write for the same reason — leaving the user with no
	// result at all, the exact silent drop this path exists to prevent. Detach
	// from cancellation, but keep a bound so a wedged DB cannot pin us.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), quickCreateNotifyTimeout)
	defer cancel()
	details, _ := json.Marshal(map[string]any{
		"run_id":          util.UUIDToString(agentRun.ID),
		"agent_id":        util.UUIDToString(agentRun.AgentID),
		"original_prompt": qc.Prompt,
		"error":           redact.Text(errMsg),
	})
	item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   requesterID,
		Type:          inboxType,
		Severity:      "action_required",
		TaskID:        pgtype.UUID{},
		Title:         title,
		Body:          pgtype.Text{String: redact.Text(errMsg), Valid: true},
		ActorType:     pgtype.Text{String: "agent", Valid: true},
		ActorID:       agentRun.AgentID,
		Details:       details,
	})
	if err != nil {
		slog.Error("quick-create failure: inbox write failed", "run_id", util.UUIDToString(agentRun.ID), "error", err)
		return
	}
	s.publishQuickCreateInbox(item, qc.WorkspaceID, util.UUIDToString(agentRun.AgentID), "")
}

// publishQuickCreateInbox emits the WS event so the requester's inbox list
// updates immediately. Mirrors the payload shape used by the other inbox
// listeners (notification_listeners.go).
func (s *AgentRunService) publishQuickCreateInbox(item db.InboxItem, workspaceID, agentID, taskStatus string) {
	resp := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"task_id":        util.UUIDToPtr(item.TaskID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
		"task_status":    taskStatus,
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: workspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload:     map[string]any{"item": resp},
	})
}

// agentToMap builds a simple map for broadcasting agent status updates.
func agentToMap(a db.Agent) map[string]any {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	return map[string]any{
		"id":                  util.UUIDToString(a.ID),
		"workspace_id":        util.UUIDToString(a.WorkspaceID),
		"runtime_id":          util.UUIDToString(a.RuntimeID),
		"name":                a.Name,
		"description":         a.Description,
		"avatar_url":          util.TextToPtr(a.AvatarUrl),
		"runtime_mode":        a.RuntimeMode,
		"runtime_config":      rc,
		"visibility":          a.Visibility,
		"status":              a.Status,
		"max_concurrent_runs": a.MaxConcurrentRuns,
		"owner_id":            util.UUIDToPtr(a.OwnerID),
		"skills":              []any{},
		"created_at":          util.TimestampToString(a.CreatedAt),
		"updated_at":          util.TimestampToString(a.UpdatedAt),
		"archived_at":         util.TimestampToPtr(a.ArchivedAt),
		"archived_by":         util.UUIDToPtr(a.ArchivedBy),
	}
}
