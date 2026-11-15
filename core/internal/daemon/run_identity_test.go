package daemon

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/silieco-ai/silieco/core/pkg/runfailure"
)

func TestExecuteRunRejectsMismatchedAgentIdentityBeforePreparation(t *testing.T) {
	t.Parallel()

	d := &Daemon{}
	_, err := d.executeRun(context.Background(), AgentRun{
		ID:          "task-identity-mismatch",
		WorkspaceID: "workspace-a",
		AgentID:     "agent-a",
		Agent:       &AgentData{ID: "agent-b", Name: "Agent B"},
	}, "claude", 0, slog.Default())
	if !errors.Is(err, errInvalidRunIdentity) {
		t.Fatalf("runTask error = %v, want invalid task identity", err)
	}
	if got := runRunFailureReason(err); got != runfailure.ReasonInvalidRunIdentity.String() {
		t.Fatalf("failure reason = %q, want %q", got, runfailure.ReasonInvalidRunIdentity)
	}
}

func TestValidateRunIdentityRequiresAuthoritativeAgentID(t *testing.T) {
	t.Parallel()

	err := validateRunIdentity(AgentRun{ID: "task-agent-id-missing", Agent: &AgentData{ID: "agent-a"}})
	if !errors.Is(err, errInvalidRunIdentity) {
		t.Fatalf("validateTaskIdentity error = %v, want invalid task identity", err)
	}
}

func TestValidateRunIdentityRequiresExpandedAgent(t *testing.T) {
	t.Parallel()

	err := validateRunIdentity(AgentRun{ID: "task-agent-missing", AgentID: "agent-a"})
	if !errors.Is(err, errInvalidRunIdentity) {
		t.Fatalf("validateTaskIdentity error = %v, want invalid task identity", err)
	}
}

func TestValidateRunIdentityAcceptsMatchingAgent(t *testing.T) {
	t.Parallel()

	if err := validateRunIdentity(AgentRun{ID: "task-agent-match", AgentID: "agent-a", Agent: &AgentData{ID: "agent-a"}}); err != nil {
		t.Fatalf("validateTaskIdentity: %v", err)
	}
}
