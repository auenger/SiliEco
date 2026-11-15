package execenv

import (
	"strings"
	"testing"
)

// TestRenderTaskContext_HandoffNote verifies the handoff note lands in
// task_context.md under its own section, distinct from the comment-reply
// trigger framing.
func TestRenderTaskContext_HandoffNote(t *testing.T) {
	note := "Scope to the auth module only."
	md := renderTaskContext("claude", RunContextForEnv{TaskID: "task-1", HandoffNote: note})

	if !strings.Contains(md, "## Handoff Note") {
		t.Fatalf("expected Handoff Note section:\n%s", md)
	}
	if !strings.Contains(md, note) {
		t.Fatalf("handoff note text missing:\n%s", md)
	}
	if !strings.Contains(md, "**Trigger:** New Assignment") {
		t.Fatalf("handoff note must render under the assignment trigger:\n%s", md)
	}
}

// TestRenderTaskContext_NoHandoffNote keeps the assignment context clean when
// no note is present.
func TestRenderTaskContext_NoHandoffNote(t *testing.T) {
	md := renderTaskContext("claude", RunContextForEnv{TaskID: "task-1"})
	if strings.Contains(md, "## Handoff Note") {
		t.Fatalf("unexpected Handoff Note section when no note set:\n%s", md)
	}
}
