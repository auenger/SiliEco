package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPrepareManagedTaskEnvWritesProvenance verifies Prepare drops the
// reuse-eligibility marker for a normal (non-local) task env, carrying the
// owning workspace/task/agent. This is the artifact shouldReusePriorWorkdir
// keys off so a follow-up claimed before .gc_meta.json is written can still
// prove the workdir is a safe reuse target (SILI-4886).
func TestPrepareManagedTaskEnvWritesProvenance(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-001",
		RunID:          "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName:      "Prov Agent",
		AgentRun: RunContextForEnv{
			TaskID:  "task-prov-1",
			AgentID: "agent-prov-1",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	prov, err := ReadManagedEnvProvenance(env.RootDir)
	if err != nil {
		t.Fatalf("read managed env provenance: %v", err)
	}
	if prov.ManagedBy != ManagedEnvProvenanceManagedBy {
		t.Fatalf("managed_by = %q, want %q", prov.ManagedBy, ManagedEnvProvenanceManagedBy)
	}
	if prov.WorkspaceID != "ws-prov-001" || prov.TaskID != "task-prov-1" || prov.AgentID != "agent-prov-1" {
		t.Fatalf("provenance owner mismatch: %+v", prov)
	}
}

// TestPrepareLocalDirectoryWritesNoProvenance pins the local_directory branch:
// a task bound to a user-supplied path must NOT get a managed-env provenance
// marker, so a squad leader can never treat the user's directory as a reusable
// managed workdir. Absence of the marker is the fail-closed signal.
func TestPrepareLocalDirectoryWritesNoProvenance(t *testing.T) {
	root := t.TempDir()
	localDir := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-local",
		RunID:          "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		AgentName:      "Local Agent",
		LocalWorkDir:   localDir,
		AgentRun: RunContextForEnv{
			TaskID:  "task-prov-local",
			AgentID: "agent-prov-local",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := ReadManagedEnvProvenance(env.RootDir); !os.IsNotExist(err) {
		t.Fatalf("local_directory Prepare must not write managed env provenance; got err = %v", err)
	}
	// The user's own directory must never receive the marker either.
	if _, err := os.Stat(filepath.Join(localDir, managedEnvProvenanceFile)); !os.IsNotExist(err) {
		t.Fatal("managed env provenance leaked into the user's local directory")
	}
}

// TestPrepareManagedChatEnvWritesProvenance confirms chat reuse has the same
// ownership proof as task reuse while remaining scoped to its conversation.
func TestPrepareManagedChatEnvWritesProvenance(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-prov-chat",
		RunID:          "cccccccc-dddd-eeee-ffff-000000000000",
		AgentName:      "Chat Agent",
		AgentRun: RunContextForEnv{
			ChatSessionID: "chat-1",
			AgentID:       "agent-chat",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	prov, err := ReadManagedEnvProvenance(env.RootDir)
	if err != nil {
		t.Fatalf("read chat managed env provenance: %v", err)
	}
	if prov.WorkspaceID != "ws-prov-chat" || prov.ChatSessionID != "chat-1" || prov.AgentID != "agent-chat" || prov.TaskID != "" {
		t.Fatalf("chat provenance owner mismatch: %+v", prov)
	}
}
