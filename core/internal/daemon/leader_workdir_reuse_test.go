package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/silieco-ai/silieco/core/internal/daemon/execenv"
)

// TestRunTaskSquadLeaderReusesWorkdirBeforeGCMetaWritten drives two real
// runTask calls and asserts the follow-up reuses the first workdir and provider
// session while NO .gc_meta.json exists. That absence is the whole point: the
// server marks the prior task completed, reconciles the follow-up, and wakes
// the runtime before the prior task's handler writes .gc_meta.json, so a
// successor can be claimed inside that window (SILI-4886). Reuse must therefore
// hinge on the Prepare-time .managed_env.json provenance, not the terminal GC
// file. runTask writes that provenance via execenv.Prepare; this test never
// writes .gc_meta.json, so it fails against the pre-fix GC-meta-keyed gate.
func TestExecuteRunSquadLeaderReusesWorkdirBeforeGCMetaWritten(t *testing.T) {
	t.Parallel()

	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	first := leaderReuseTestRun("task-first")
	firstResult, err := d.executeRun(context.Background(), first, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("first runTask: %v", err)
	}
	if firstResult.SessionID == "" || firstResult.WorkDir == "" {
		t.Fatalf("first result missing resume state: %+v", firstResult)
	}
	// Simulate the race window: the successor is claimed before the prior
	// task's handler writes .gc_meta.json. The Prepare-time provenance is the
	// only reuse signal available.
	if _, err := os.Stat(filepath.Join(firstResult.EnvRoot, ".gc_meta.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .gc_meta.json before the completion handler runs; stat err = %v", err)
	}

	second := leaderReuseTestRun("task-second")
	second.PriorSessionID = firstResult.SessionID
	second.PriorWorkDir = firstResult.WorkDir
	secondResult, err := d.executeRun(context.Background(), second, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("second runTask: %v", err)
	}
	if secondResult.WorkDir != firstResult.WorkDir {
		t.Fatalf("second WorkDir = %q, want reused leader workdir %q", secondResult.WorkDir, firstResult.WorkDir)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read claude args: %v", err)
	}
	if !strings.Contains(string(args), "--resume\nsession-leader-reuse\n") {
		t.Fatalf("second claude invocation did not resume prior session; args:\n%s", args)
	}
}

func TestExecuteRunSquadLeaderDoesNotReuseExternalPriorWorkdir(t *testing.T) {
	t.Parallel()

	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	externalWorkDir := t.TempDir()
	agentRun := leaderReuseTestRun("task-external")
	agentRun.PriorSessionID = "session-leader-reuse"
	agentRun.PriorWorkDir = externalWorkDir

	result, err := d.executeRun(context.Background(), agentRun, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.WorkDir == externalWorkDir {
		t.Fatalf("leader reused external workdir %q without a local-directory lock", externalWorkDir)
	}
}

// TestShouldReusePriorWorkdirNonLeaderRequiresProvenance prevents an ordinary
// task from inheriting another agent's repository and Git HEAD merely because
// the server offered that path.
func TestShouldReusePriorWorkdirNonLeaderRequiresProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	agentRun := leaderReuseTestRun("task-non-leader")
	agentRun.IsLeaderRun = false
	agentRun.PriorWorkDir = filepath.Join(root, "anything", "workdir")
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatal("non-leader task reused a prior workdir without any ownership provenance")
	}
}

func TestShouldReusePriorWorkdirChatAcceptsMatchingConversation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-chat", "12345678", "workdir")
	writeChatRunMarker(t, workDir, "agent-chat", "chat-session")
	writeChatManagedEnvProvenance(t, workDir, "ws-chat", "chat-session", "agent-chat")

	agentRun := leaderReuseTestRun("task-chat")
	agentRun.WorkspaceID = "ws-chat"
	agentRun.TaskID = ""
	agentRun.ChatSessionID = "chat-session"
	agentRun.AgentID = "agent-chat"
	agentRun.IsLeaderRun = false
	agentRun.PriorWorkDir = workDir
	if !shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("chat task did not reuse its fully-provenanced conversation workdir %q", workDir)
	}

	agentRun.ChatSessionID = "another-chat"
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatal("chat task reused a workdir belonging to another conversation")
	}
}

// TestShouldReusePriorWorkdirSquadLeaderAcceptsManagedProvenance is the unit
// positive: managed shape + matching Prepare-time provenance + matching marker.
func TestShouldReusePriorWorkdirSquadLeaderAcceptsManagedProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderRunMarker(t, workDir, "agent-leader", "task-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "task-leader", "agent-leader")

	agentRun := leaderReuseTestRun("task-accept")
	agentRun.PriorWorkDir = workDir
	if !shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("leader did not reuse a fully-provenanced managed workdir %q", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsNonManagedPathUnderRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userDir := filepath.Join(root, "ws-leader", "user-project")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}

	agentRun := leaderReuseTestRun("task-contained-user-dir")
	agentRun.PriorWorkDir = userDir
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("leader reused non-managed path %q merely because it is under WorkspacesRoot", userDir)
	}
}

// TestShouldReusePriorWorkdirSquadLeaderRejectsManagedShapeWithoutProvenance
// covers the race-critical case and the local_directory fail-closed guarantee:
// a workdir with the right shape and a valid marker but NO .managed_env.json is
// rejected. Local_directory envs never get provenance (Prepare skips it), and a
// follow-up claimed before any provenance exists must start fresh rather than
// risk reusing a user path.
func TestShouldReusePriorWorkdirSquadLeaderRejectsManagedShapeWithoutProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderRunMarker(t, workDir, "agent-leader", "task-leader")

	agentRun := leaderReuseTestRun("task-without-provenance")
	agentRun.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("leader reused marked workdir %q without managed-env provenance", workDir)
	}
}

// TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedProvenanceOwner
// rejects a provenance file whose workspace/task/agent does not match the
// claiming task, even when the marker is otherwise well-formed.
func TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedProvenanceOwner(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderRunMarker(t, workDir, "agent-leader", "task-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "task-leader", "other-agent")

	agentRun := leaderReuseTestRun("task-mismatched-provenance")
	agentRun.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("leader reused workdir %q with provenance owned by another agent", workDir)
	}
}

// TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedTaskMarker keeps its
// original intent — a marker for another agent must be refused — now with a
// matching provenance in place so the check reaches the marker comparison.
func TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedRunMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderRunMarker(t, workDir, "other-agent", "task-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "task-leader", "agent-leader")

	agentRun := leaderReuseTestRun("task-mismatched-marker")
	agentRun.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("leader reused workdir %q with a marker for another agent", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsRegularFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	if err := os.MkdirAll(filepath.Dir(workDir), 0o755); err != nil {
		t.Fatalf("mkdir workdir parent: %v", err)
	}
	if err := os.WriteFile(workDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write workdir file: %v", err)
	}

	agentRun := leaderReuseTestRun("task-file-workdir")
	agentRun.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("leader reused regular file %q as a workdir", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsEmptyAgentID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderRunMarker(t, workDir, "agent-leader", "task-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "task-leader", "agent-leader")

	agentRun := leaderReuseTestRun("task-empty-agent")
	agentRun.AgentID = ""
	agentRun.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatal("leader with an empty AgentID must not reuse a prior workdir")
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	external := t.TempDir()
	parent := filepath.Join(root, "ws-leader", "12345678")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	// A managed-shape path whose final segment is a symlink escaping the root.
	// EvalSymlinks + IsLocal must reject it so a symlink can't smuggle a user
	// directory past the containment check.
	workDir := filepath.Join(parent, "workdir")
	if err := os.Symlink(external, workDir); err != nil {
		t.Fatalf("symlink workdir -> external: %v", err)
	}

	agentRun := leaderReuseTestRun("task-symlink-escape")
	agentRun.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(agentRun, nil, root) {
		t.Fatalf("leader reused a workdir symlinked outside WorkspacesRoot (%q -> %q)", workDir, external)
	}
}

func newLeaderReuseTestDaemon(t *testing.T) (*Daemon, string, func()) {
	t.Helper()

	testDir := t.TempDir()
	fakeBin := filepath.Join(testDir, "claude")
	argsFile := filepath.Join(testDir, "claude-args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + argsFile + `"
printf '%s\n' '--invocation-end--' >> "` + argsFile + `"
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-leader-reuse"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-leader-reuse","result":"done"}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         logger,
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-leader": {ID: "rt-leader", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"claude": {Path: fakeBin},
			},
		},
	}
	return d, argsFile, srv.Close
}

func writeLeaderRunMarker(t *testing.T, workDir, agentID, taskID string) {
	t.Helper()

	markerPath := filepath.Join(workDir, execenv.RunContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	marker := []byte(`{"managed_by":"` + execenv.RunContextMarkerManagedBy + `","agent_id":"` + agentID + `","task_id":"` + taskID + `"}`)
	if err := os.WriteFile(markerPath, marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func writeChatRunMarker(t *testing.T, workDir, agentID, chatSessionID string) {
	t.Helper()

	markerPath := filepath.Join(workDir, execenv.RunContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	marker := []byte(`{"managed_by":"` + execenv.RunContextMarkerManagedBy + `","agent_id":"` + agentID + `","chat_session_id":"` + chatSessionID + `"}`)
	if err := os.WriteFile(markerPath, marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func writeLeaderManagedEnvProvenance(t *testing.T, workDir, workspaceID, taskID, agentID string) {
	t.Helper()

	envRoot := filepath.Dir(workDir)
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatalf("mkdir env root: %v", err)
	}
	if err := execenv.WriteManagedEnvProvenance(envRoot, execenv.ManagedEnvProvenance{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		AgentID:     agentID,
	}); err != nil {
		t.Fatalf("write managed env provenance: %v", err)
	}
}

func writeChatManagedEnvProvenance(t *testing.T, workDir, workspaceID, chatSessionID, agentID string) {
	t.Helper()

	envRoot := filepath.Dir(workDir)
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatalf("mkdir env root: %v", err)
	}
	if err := execenv.WriteManagedEnvProvenance(envRoot, execenv.ManagedEnvProvenance{
		WorkspaceID:   workspaceID,
		ChatSessionID: chatSessionID,
		AgentID:       agentID,
	}); err != nil {
		t.Fatalf("write managed env provenance: %v", err)
	}
}

func leaderReuseTestRun(id string) AgentRun {
	return AgentRun{
		ID:          id,
		WorkspaceID: "ws-leader",
		RuntimeID:   "rt-leader",
		TaskID:      "task-leader",
		AgentID:     "agent-leader",
		AuthToken:   "mat_leader_reuse",
		IsLeaderRun: true,
		Agent: &AgentData{
			ID:   "agent-leader",
			Name: "leader-agent",
		},
	}
}
