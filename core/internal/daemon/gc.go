package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/silieco-ai/silieco/core/internal/daemon/execenv"
)

// gcLoop periodically scans local workspace directories and removes those
// whose task is done/cancelled and hasn't been updated within the configured TTL.
func (d *Daemon) gcLoop(ctx context.Context) {
	if !d.cfg.GCEnabled {
		d.logger.Info("gc: disabled")
		return
	}
	d.logger.Info("gc: started",
		"interval", d.cfg.GCInterval,
		"ttl", d.cfg.GCTTL,
		"orphan_ttl", d.cfg.GCOrphanTTL,
		"artifact_ttl", d.cfg.GCArtifactTTL,
		"artifact_patterns", d.cfg.GCArtifactPatterns,
		"managed_artifact_subpaths", execenv.ManagedReclaimableArtifactSubpaths(),
	)

	// Run once at startup after a short delay (let the daemon finish initializing).
	if err := sleepWithContext(ctx, 30*time.Second); err != nil {
		return
	}
	d.runGC(ctx)

	ticker := time.NewTicker(d.cfg.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runGC(ctx)
		}
	}
}

// gcStats accumulates byte counts and per-pattern hit counts for one GC cycle.
type gcStats struct {
	cleaned         int            // whole task dirs removed (task done/cancelled)
	orphaned        int            // whole task dirs removed (no meta / unreachable task)
	skipped         int            // task dirs left untouched
	artifactDirs    int            // task dirs that had at least one artifact reclaimed
	artifactRemoved int            // count of removed artifact subdirs
	storesReclaimed int            // per-task Codex session stores reclaimed past their TTL
	bytesReclaimed  int64          // total bytes freed in this cycle
	byPattern       map[string]int // configured basename or managed path label -> reclaim count
}

// runGC performs a single GC scan across all workspace directories.
func (d *Daemon) runGC(ctx context.Context) {
	root := d.cfg.WorkspacesRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		d.logger.Warn("gc: read workspaces root failed", "error", err)
		return
	}

	stats := &gcStats{byPattern: map[string]int{}}
	for _, wsEntry := range entries {
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		wsDir := filepath.Join(root, wsEntry.Name())
		d.gcWorkspace(ctx, wsDir, stats)
	}

	// Prune stale worktree references from all bare repo caches.
	d.pruneRepoWorktrees(root)

	// Reclaim per-task Codex session stores idle past their TTL. These live
	// under the shared ~/.codex home (outside WorkspacesRoot) so resume survives
	// the task GC, which means they need their own bounded lifecycle (SILI-4424).
	if storesRemoved, storeBytes := execenv.PruneCodexSessionStores(d.cfg.Profile, d.cfg.GCCodexSessionTTL, time.Now(), d.reserveCodexStoreForDeletion, d.logger); storesRemoved > 0 {
		stats.storesReclaimed += storesRemoved
		stats.bytesReclaimed += storeBytes
	}

	if stats.cleaned > 0 || stats.orphaned > 0 || stats.artifactDirs > 0 || stats.storesReclaimed > 0 {
		d.logger.Info("gc: cycle complete",
			"cleaned", stats.cleaned,
			"orphaned", stats.orphaned,
			"skipped", stats.skipped,
			"artifact_dirs", stats.artifactDirs,
			"artifact_removed", stats.artifactRemoved,
			"codex_session_stores_reclaimed", stats.storesReclaimed,
			"bytes_reclaimed", stats.bytesReclaimed,
			"by_pattern", stats.byPattern,
		)
	}
}

// gcWorkspace scans task directories inside a single workspace directory.
func (d *Daemon) gcWorkspace(ctx context.Context, wsDir string, stats *gcStats) {
	runEntries, err := os.ReadDir(wsDir)
	if err != nil {
		d.logger.Warn("gc: read workspace dir failed", "dir", wsDir, "error", err)
		return
	}

	cleanedHere := 0
	taskCandidates := make([]taskGCCandidate, 0, len(runEntries))
	for _, entry := range runEntries {
		if ctx.Err() != nil {
			return
		}
		if !entry.IsDir() {
			continue
		}
		runDir := filepath.Join(wsDir, entry.Name())
		if d.isActiveEnvRoot(runDir) {
			stats.skipped++
			continue
		}
		meta, metaErr := execenv.ReadGCMeta(runDir)
		if metaErr == nil && meta.Kind == execenv.GCKindTask && strings.TrimSpace(meta.TaskID) != "" {
			taskCandidates = append(taskCandidates, taskGCCandidate{runDir: runDir, meta: meta})
			continue
		}
		action := d.shouldCleanRunDir(ctx, runDir)
		cleanedHere += d.applyGCAction(runDir, action, stats)
	}
	cleanedHere += d.gcWorkspaceTasks(ctx, filepath.Base(wsDir), taskCandidates, stats)

	// Remove the workspace directory itself if it's now empty.
	if cleanedHere > 0 {
		remaining, _ := os.ReadDir(wsDir)
		if len(remaining) == 0 {
			os.Remove(wsDir)
		}
	}
}

const taskGCBatchSize = 500

type taskGCCandidate struct {
	runDir string
	meta   *execenv.GCMeta
}

// gcWorkspaceTasks resolves all task-backed task dirs with a bounded number
// of workspace-level requests. Multiple task dirs for the same task share one
// result. The client transparently falls back to the legacy per-task endpoint
// when it is connected to an older server.
func (d *Daemon) gcWorkspaceTasks(ctx context.Context, workspaceID string, candidates []taskGCCandidate, stats *gcStats) int {
	if len(candidates) == 0 {
		return 0
	}

	taskIDs := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		taskID := strings.TrimSpace(candidate.meta.TaskID)
		if _, ok := seen[taskID]; ok {
			continue
		}
		seen[taskID] = struct{}{}
		taskIDs = append(taskIDs, taskID)
	}

	results := make(map[string]TaskGCCheckResult, len(taskIDs))
	for start := 0; start < len(taskIDs); start += taskGCBatchSize {
		if ctx.Err() != nil {
			break
		}
		end := min(start+taskGCBatchSize, len(taskIDs))
		chunkResults, err := d.client.GetTaskGCChecks(ctx, workspaceID, taskIDs[start:end])
		if err != nil {
			d.logger.Warn("gc: batch task check failed",
				"workspace", workspaceID,
				"count", end-start,
				"error", err,
			)
			continue
		}
		for taskID, result := range chunkResults {
			results[taskID] = result
		}
	}

	cleaned := 0
	for i, candidate := range candidates {
		if ctx.Err() != nil {
			stats.skipped += len(candidates) - i
			break
		}
		taskID := strings.TrimSpace(candidate.meta.TaskID)
		result, ok := results[taskID]
		if !ok || result.Err != nil {
			stats.skipped++
			continue
		}
		action := d.gcDecisionTaskResult(candidate.runDir, candidate.meta, result)
		action = d.applyLocalDirectoryGCOverride(candidate.meta, action)
		cleaned += d.applyGCAction(candidate.runDir, action, stats)
	}
	return cleaned
}

// applyGCAction performs one decision and updates cycle stats. Each mutation
// atomically reserves the env root because a task can start while the server
// reconciliation request is in flight.
func (d *Daemon) applyGCAction(runDir string, action gcAction, stats *gcStats) int {
	if action != gcActionSkip {
		release, ok := d.reserveEnvRootForGC(runDir)
		if !ok {
			stats.skipped++
			return 0
		}
		defer release()
	}
	switch action {
	case gcActionClean:
		bytes := dirSize(runDir)
		d.cleanRunDir(runDir)
		stats.cleaned++
		stats.bytesReclaimed += bytes
		return 1
	case gcActionOrphan:
		bytes := dirSize(runDir)
		d.cleanRunDir(runDir)
		stats.orphaned++
		stats.bytesReclaimed += bytes
		return 1
	case gcActionCleanArtifacts:
		removed, bytes, perPattern := d.cleanRunArtifacts(runDir, d.cfg.GCArtifactPatterns)
		recordArtifactCleanup(stats, removed, bytes, perPattern)
		stats.skipped++ // task dir itself preserved
	case gcActionCleanManagedArtifacts:
		removed, bytes, perPattern := d.cleanManagedRunArtifacts(runDir)
		recordArtifactCleanup(stats, removed, bytes, perPattern)
		stats.skipped++ // task dir itself preserved
	default:
		stats.skipped++
	}
	return 0
}

func recordArtifactCleanup(stats *gcStats, removed int, bytes int64, perPattern map[string]int) {
	if removed == 0 {
		return
	}
	stats.artifactDirs++
	stats.artifactRemoved += removed
	stats.bytesReclaimed += bytes
	if stats.byPattern == nil {
		stats.byPattern = map[string]int{}
	}
	for pattern, count := range perPattern {
		stats.byPattern[pattern] += count
	}
}

type gcAction int

const (
	gcActionSkip                  gcAction = iota
	gcActionClean                          // task is done/cancelled and stale
	gcActionOrphan                         // no meta or unknown task and dir is old
	gcActionCleanArtifacts                 // task completed long enough ago; drop regenerable artifacts only
	gcActionCleanManagedArtifacts          // preserve the task and drop exact daemon-managed artifacts only
)

// shouldCleanTaskDir decides whether a task directory should be removed.
// Dispatches on meta.Kind so chat / autopilot / quick-create tasks each
// follow the parent record that actually governs their lifecycle.
func (d *Daemon) shouldCleanRunDir(ctx context.Context, runDir string) gcAction {
	// A task currently running on this env root must never be reclaimed —
	// not even on the done/cancelled or orphan-404 paths. A re-dispatched or
	// still-running task can reuse the prior workdir of an already-done task
	// whose updated_at is older than the TTL (a task re-claim doesn't advance
	// updated_at), so the regular TTL check alone wouldn't notice the resumed
	// activity.
	if d.isActiveEnvRoot(runDir) {
		return gcActionSkip
	}

	meta, err := execenv.ReadGCMeta(runDir)
	if err != nil {
		return d.orphanByMTime(runDir, "no meta")
	}

	action := d.shouldCleanRunDirForKind(ctx, runDir, meta)
	return d.applyLocalDirectoryGCOverride(meta, action)
}

func (d *Daemon) applyLocalDirectoryGCOverride(meta *execenv.GCMeta, action gcAction) gcAction {
	if !meta.LocalDirectory {
		return action
	}
	// local_directory tasks keep their envRoot indefinitely so the user
	// can inspect output/ and logs/ for forensic context. The WorkDir is
	// the user's own path and lives outside taskDir. The envRoot contains
	// the daemon's durable logbook plus regenerable tool caches, so keep the
	// former while allowing narrowly scoped cleanup of the latter.
	//
	//   gcActionClean   → demote to artifact-pattern cleanup so envRoot
	//                     (and especially the logbook) survives.
	//   gcActionOrphan  → exact managed-artifact cleanup only; we don't ever
	//                     wipe a local_directory envRoot via the mtime path,
	//                     since the parent task / chat record going away
	//                     should not collateral-delete the user's audit trail.
	//
	// Artifact cleanup remains disabled when GCArtifactTTL is explicitly zero.
	// gcActionCleanArtifacts, gcActionCleanManagedArtifacts, and gcActionSkip obey the
	// "no full envRoot RemoveAll" rule.
	if d.cfg.GCArtifactTTL <= 0 {
		return gcActionSkip
	}
	switch action {
	case gcActionClean:
		return gcActionCleanArtifacts
	case gcActionOrphan:
		return gcActionCleanManagedArtifacts
	default:
		return action
	}
}

// shouldCleanTaskDirForKind runs the per-Kind dispatch without applying the
// local_directory override. Split out so shouldCleanTaskDir can intercept
// the result.
func (d *Daemon) shouldCleanRunDirForKind(ctx context.Context, runDir string, meta *execenv.GCMeta) gcAction {
	switch meta.Kind {
	case execenv.GCKindTask:
		return d.gcDecisionTask(ctx, runDir, meta)
	case execenv.GCKindChat:
		return d.gcDecisionChat(ctx, runDir, meta)
	case execenv.GCKindAutopilotRun:
		return d.gcDecisionAutopilotRun(ctx, runDir, meta)
	case execenv.GCKindQuickCreate:
		return d.gcDecisionQuickCreate(ctx, runDir, meta)
	default:
		// Unknown kind: fall back to mtime-based orphan cleanup so a future
		// daemon writing a kind we don't recognize doesn't get insta-wiped.
		return d.orphanByMTime(runDir, "unknown kind")
	}
}

// orphanByMTime returns gcActionOrphan if the directory is older than
// GCOrphanTTL, gcActionSkip otherwise. Centralizes the "we have no parent
// record signal so just look at the disk" fallback used by every kind.
func (d *Daemon) orphanByMTime(runDir, reason string) gcAction {
	info, err := os.Stat(runDir)
	if err != nil {
		return gcActionSkip
	}
	if time.Since(info.ModTime()) > d.cfg.GCOrphanTTL {
		d.logger.Info("gc: orphan directory", "dir", runDir, "reason", reason, "age", time.Since(info.ModTime()).Round(time.Hour))
		return gcActionOrphan
	}
	return gcActionSkip
}

// isAccessNotFound detects the 404 returned by gc-check endpoints. The same
// status covers "row deleted" and "daemon token can't see this workspace"
// (the requireDaemonWorkspaceAccess anti-enumeration shape), so callers
// can't tell the two apart from the response alone.
func isAccessNotFound(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusNotFound
}

func (d *Daemon) gcDecisionTask(ctx context.Context, runDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.TaskID) == "" {
		return d.orphanByMTime(runDir, "empty task id")
	}

	status, err := d.client.GetTaskGCCheck(ctx, meta.TaskID)
	if err != nil {
		if isAccessNotFound(err) {
			// 404 is ambiguous: server returns it for both "task deleted"
			// and "daemon token has no access to the workspace". Fall back
			// to the mtime-gated orphan cleanup so a scoped-down token
			// can't instantly wipe dirs whose tasks are still live.
			return d.orphanByMTime(runDir, "task not accessible")
		}
		return gcActionSkip
	}

	return d.gcDecisionTaskResult(runDir, meta, TaskGCCheckResult{
		ID:        meta.TaskID,
		Found:     true,
		Status:    status.Status,
		UpdatedAt: status.UpdatedAt,
	})
}

func (d *Daemon) gcDecisionTaskResult(runDir string, meta *execenv.GCMeta, result TaskGCCheckResult) gcAction {
	if !result.Found {
		return d.orphanByMTime(runDir, "task not accessible")
	}

	if (result.Status == "done" || result.Status == "cancelled") &&
		time.Since(result.UpdatedAt) > d.cfg.GCTTL {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(runDir),
			"kind", "task",
			"task", meta.TaskID,
			"status", result.Status,
			"updated_at", result.UpdatedAt.Format(time.RFC3339),
		)
		return gcActionClean
	}

	if d.cfg.GCArtifactTTL > 0 && !meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCArtifactTTL {
		d.logger.Info("gc: eligible for artifact cleanup",
			"dir", filepath.Base(runDir),
			"kind", "task",
			"task", meta.TaskID,
			"status", result.Status,
			"completed_at", meta.CompletedAt.Format(time.RFC3339),
		)
		return gcActionCleanArtifacts
	}

	// Old metadata may not have completed_at. Keep that case conservative:
	// after the metadata file itself has been idle for the longer orphan TTL,
	// reclaim only the exact daemon-managed cache. WriteGCMeta replaces this
	// file after every completed task, so a recent reuse refreshes the signal
	// even when activity below taskDir leaves the root directory mtime stale.
	if d.cfg.GCArtifactTTL > 0 && meta.CompletedAt.IsZero() {
		if age, ok := gcMetaFileAge(runDir); ok && age > d.cfg.GCOrphanTTL {
			d.logger.Info("gc: legacy task eligible for managed artifact cleanup",
				"dir", filepath.Base(runDir),
				"kind", "task",
				"task", meta.TaskID,
				"status", result.Status,
				"age", age.Round(time.Hour),
			)
			return gcActionCleanManagedArtifacts
		}
	}

	return gcActionSkip
}

func gcMetaFileAge(runDir string) (time.Duration, bool) {
	info, err := os.Stat(filepath.Join(runDir, ".gc_meta.json"))
	if err != nil {
		return 0, false
	}
	return time.Since(info.ModTime()), true
}

func (d *Daemon) gcDecisionChat(ctx context.Context, runDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.ChatSessionID) == "" {
		return d.orphanByMTime(runDir, "empty chat session id")
	}

	status, err := d.client.GetChatSessionGCCheck(ctx, meta.ChatSessionID)
	if err != nil {
		if isAccessNotFound(err) {
			// 404 means the chat_session row is gone — DeleteChatSession is
			// a real DELETE, so a hard delete propagates here as soon as
			// the user clicks the button. This is the strongest reclaim
			// signal we get and it's exactly acceptance criterion #3:
			// reclaim within one GC cycle (≤ GCInterval), not 72h.
			//
			// We don't gate on mtime: every chat_session_id in a meta file
			// was written by this daemon under its current token, so there
			// is no cross-workspace probe to defend against.
			d.logger.Info("gc: eligible for cleanup",
				"dir", filepath.Base(runDir),
				"kind", "chat",
				"chat_session", meta.ChatSessionID,
				"reason", "session not accessible (hard-deleted)",
			)
			return gcActionClean
		}
		return gcActionSkip
	}

	switch status.Status {
	case "active":
		// An active chat session must never be reclaimed by mtime — that
		// would silently kill a user's idle session and break "PriorWorkDir"
		// resume on their next message. This is the explicit short-circuit
		// the task body called out as verifyable behavior #2.
		return gcActionSkip
	case "archived":
		if time.Since(status.UpdatedAt) > d.cfg.GCTTL {
			d.logger.Info("gc: eligible for cleanup",
				"dir", filepath.Base(runDir),
				"kind", "chat",
				"chat_session", meta.ChatSessionID,
				"status", status.Status,
				"updated_at", status.UpdatedAt.Format(time.RFC3339),
			)
			return gcActionClean
		}
	}
	return gcActionSkip
}

func (d *Daemon) gcDecisionAutopilotRun(ctx context.Context, runDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.AutopilotRunID) == "" {
		return d.orphanByMTime(runDir, "empty autopilot run id")
	}

	status, err := d.client.GetAutopilotRunGCCheck(ctx, meta.AutopilotRunID)
	if err != nil {
		if isAccessNotFound(err) {
			return d.orphanByMTime(runDir, "autopilot run not accessible")
		}
		return gcActionSkip
	}

	// Terminal states per the autopilot_run CHECK constraint:
	//   completed, failed, skipped — the run finished its own work.
	//   task_created            — the run produced an task task that owns
	//                              its own workdir; this run's workdir is
	//                              dead weight from here on.
	// Non-terminal: pending, running. Skip until they reach a terminal state
	// rather than trying to bound them by mtime — long autopilots are real.
	//
	// An autopilot run's workdir is never reused: unlike task/chat tasks there
	// is no PriorWorkDir path that hands a later run the same directory, so every
	// run gets a fresh one. Whatever the run produced already lives server-side
	// (and an task_created run handed its work to an task task that owns its own
	// envRoot). So the moment the run reaches a terminal state the directory is
	// dead weight and we reclaim it immediately, without waiting out GCTTL — the
	// same reasoning gcDecisionQuickCreate applies to quick-create dirs. The
	// active-env-root short-circuit in shouldCleanTaskDir still protects a run
	// that is mid-flight, so this can't pull the rug from under live work.
	if isAutopilotRunTerminal(status.Status) {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(runDir),
			"kind", "autopilot_run",
			"autopilot_run", meta.AutopilotRunID,
			"status", status.Status,
		)
		return gcActionClean
	}
	return gcActionSkip
}

// isAutopilotRunTerminal mirrors the run.status CHECK in
// migrations/042_autopilot.up.sql. Non-terminal states are pending/running;
// every other value the schema allows is a final resting state from the
// daemon's POV (the run is no longer producing work in this workdir).
func isAutopilotRunTerminal(status string) bool {
	switch status {
	case "completed", "failed", "skipped", "task_created":
		return true
	default:
		return false
	}
}

func (d *Daemon) gcDecisionQuickCreate(ctx context.Context, runDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.RunID) == "" {
		return d.orphanByMTime(runDir, "empty task id")
	}

	status, err := d.client.GetRunGCCheck(ctx, meta.RunID)
	if err != nil {
		if isAccessNotFound(err) {
			// Task row was hard-deleted, or token can't see it. Either way,
			// fall back to mtime-gated orphan to stay safe across scoped
			// tokens — same reasoning as the task path.
			return d.orphanByMTime(runDir, "task not accessible")
		}
		return gcActionSkip
	}

	// Quick-create workdirs are not reused by the task task that
	// LinkRunToTask eventually attaches — that task gets its own
	// envRoot. So as soon as the quick-create task itself reaches a
	// terminal state we can reclaim the directory immediately, without
	// waiting for GCTTL. If the user wants to revisit, the linked task
	// has the agent's output already.
	if isAgentRunTerminal(status.Status) {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(runDir),
			"kind", "quick_create",
			"task", meta.RunID,
			"status", status.Status,
		)
		return gcActionClean
	}
	return gcActionSkip
}

// isAgentTaskTerminal reports whether a value of agent_run.status
// represents a final state. Mirrors the status enum used across the
// task service — see service/task.go for the canonical list.
func isAgentRunTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// cleanTaskDir removes a task directory and logs the result.
func (d *Daemon) cleanRunDir(runDir string) {
	if err := os.RemoveAll(runDir); err != nil {
		d.logger.Warn("gc: remove task dir failed", "dir", runDir, "error", err)
	} else {
		d.logger.Info("gc: removed", "dir", runDir)
	}
}

// cleanTaskArtifacts walks taskDir and deletes every directory whose basename
// matches one of patterns, plus exact daemon-managed artifact paths. Returns
// (removedCount, bytesReclaimed, perPattern).
//
// Safety contract:
//   - patterns are basename-only; entries with a path separator are dropped.
//   - .git subtrees are never descended into, so the agent's git history stays
//     intact even if a pattern would otherwise match.
//   - symlinks are skipped entirely — neither the link nor its target is
//     touched, so a malicious or stale link can't redirect the GC outside the
//     workdir.
//   - every removal target is verified to live inside taskDir, so a tampered
//     .gc_meta.json can't trick the daemon into deleting outside its sandbox.
func (d *Daemon) cleanRunArtifacts(runDir string, patterns []string) (removed int, bytes int64, perPattern map[string]int) {
	return d.cleanRunArtifactsMatching(runDir, newArtifactMatcher(patterns, execenv.ManagedReclaimableArtifactSubpaths()))
}

func (d *Daemon) cleanManagedRunArtifacts(runDir string) (removed int, bytes int64, perPattern map[string]int) {
	return d.cleanRunArtifactsMatching(runDir, newArtifactMatcher(nil, execenv.ManagedReclaimableArtifactSubpaths()))
}

func (d *Daemon) cleanRunArtifactsMatching(runDir string, matcher artifactMatcher) (removed int, bytes int64, perPattern map[string]int) {
	perPattern = map[string]int{}
	if runDir == "" || (len(matcher.basenames) == 0 && len(matcher.exactPaths) == 0) {
		return
	}

	absRoot, err := filepath.Abs(runDir)
	if err != nil {
		return
	}

	walkErr := filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort — keep walking
		}
		if path == absRoot {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		// Never descend into .git — preserves agent commits even if a pattern
		// like "objects" would otherwise match.
		if entry.Name() == ".git" {
			return filepath.SkipDir
		}
		// Refuse to follow symlinked directories. WalkDir reports them as type
		// Dir on some platforms; lstat to be sure.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		pattern, ok := matcher.matchDirectory(absRoot, path, entry)
		if !ok {
			return nil
		}
		size := dirSize(path)
		if rmErr := os.RemoveAll(path); rmErr != nil {
			d.logger.Warn("gc: artifact remove failed", "path", path, "error", rmErr)
			return filepath.SkipDir
		}
		removed++
		bytes += size
		perPattern[pattern]++
		d.logger.Info("gc: artifact removed", "path", path, "bytes", size)
		// Don't descend into the now-deleted subtree.
		return filepath.SkipDir
	})
	if walkErr != nil {
		d.logger.Warn("gc: artifact walk failed", "dir", runDir, "error", walkErr)
	}
	return
}

// dirSize returns the total size of all regular files under root, in bytes.
// Non-fatal: errors during the walk are ignored so callers can report a
// best-effort byte count without aborting the whole GC cycle.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

const (
	gitCmdTimeout         = 30 * time.Second
	gitMaintenanceTimeout = 10 * time.Minute
)

// pruneRepoWorktrees runs `git worktree prune` on all bare repos in the cache.
func (d *Daemon) pruneRepoWorktrees(workspacesRoot string) {
	reposRoot := filepath.Join(workspacesRoot, ".repos")
	wsEntries, err := os.ReadDir(reposRoot)
	if err != nil {
		return
	}

	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() {
			continue
		}
		wsRepoDir := filepath.Join(reposRoot, wsEntry.Name())
		repoEntries, err := os.ReadDir(wsRepoDir)
		if err != nil {
			continue
		}
		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() {
				continue
			}
			barePath := filepath.Join(wsRepoDir, repoEntry.Name())
			if !isBareRepo(barePath) {
				continue
			}
			d.pruneWorktree(barePath)
		}
	}
}

func (d *Daemon) pruneWorktree(barePath string) {
	if d.repoCache != nil {
		if err := d.repoCache.WithRepoLock(barePath, func() error {
			d.pruneWorktreeLocked(barePath)
			return nil
		}); err != nil {
			d.logger.Warn("gc: repo lock failed", "repo", barePath, "error", err)
			return
		}
		return
	}

	d.pruneWorktreeLocked(barePath)
}

func (d *Daemon) pruneWorktreeLocked(barePath string) {
	if out, err := runGitGCCommand(barePath, "worktree", "prune"); err != nil {
		d.logger.Warn("gc: worktree prune failed",
			"repo", barePath,
			"output", out,
			"error", err,
		)
	}

	activeBranches, err := agentWorktreeBranches(barePath)
	if err != nil {
		d.logger.Warn("gc: worktree branch scan failed", "repo", barePath, "error", err)
		return
	}

	agentBranches, err := listAgentBranches(barePath)
	if err != nil {
		d.logger.Warn("gc: agent branch scan failed", "repo", barePath, "error", err)
		return
	}

	deleted := 0
	for _, branch := range agentBranches {
		if _, ok := activeBranches[branch]; ok {
			continue
		}
		if out, err := runGitGCCommand(barePath, "branch", "-D", "--", branch); err != nil {
			d.logger.Warn("gc: agent branch delete failed",
				"repo", barePath,
				"branch", branch,
				"output", out,
				"error", err,
			)
			continue
		}
		deleted++
	}
	if deleted == 0 {
		return
	}
	d.logger.Info("gc: deleted stale agent branches", "repo", barePath, "count", deleted)

	// Heavier maintenance only runs when we actually removed refs, so we don't
	// turn every GC tick into a full `git gc --prune` on every cached repo. The
	// prune step gets its own longer timeout because it can take minutes on a
	// real bare cache; under the shared 30s budget it would be killed mid-run.
	maintenance := []struct {
		args    []string
		timeout time.Duration
	}{
		{args: []string{"reflog", "expire", "--expire=30.days", "--all"}, timeout: gitCmdTimeout},
		{args: []string{"gc", "--prune=30.days"}, timeout: gitMaintenanceTimeout},
	}
	for _, step := range maintenance {
		if out, err := runGitCommand(barePath, step.timeout, step.args...); err != nil {
			d.logger.Warn("gc: git maintenance failed",
				"repo", barePath,
				"command", strings.Join(step.args, " "),
				"output", out,
				"error", err,
			)
		}
	}
}

func runGitGCCommand(barePath string, args ...string) (string, error) {
	return runGitCommand(barePath, gitCmdTimeout, args...)
}

func runGitCommand(barePath string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdArgs := append([]string{"-C", barePath}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func agentWorktreeBranches(barePath string) (map[string]struct{}, error) {
	out, err := runGitGCCommand(barePath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	branches := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "branch refs/heads/") {
			continue
		}
		branch := strings.TrimPrefix(line, "branch refs/heads/")
		if strings.HasPrefix(branch, "agent/") {
			branches[branch] = struct{}{}
		}
	}
	return branches, nil
}

func listAgentBranches(barePath string) ([]string, error) {
	// Trailing slash narrows the pattern to the `agent/` namespace only. Without
	// it, `for-each-ref` would also return a branch literally named `agent`,
	// which `agentWorktreeBranches` ignores — that branch would then be deleted.
	out, err := runGitGCCommand(barePath, "for-each-ref", "--format=%(refname:short)", "refs/heads/agent/")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		branches = append(branches, branch)
	}
	return branches, nil
}

// isBareRepo checks if a path looks like a bare git repository.
func isBareRepo(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "objects")); err != nil {
		return false
	}
	return true
}
