package agent

import (
	"bytes"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
)

func TestRedactAgentCommandArgsPreservesOnlySafeFlagNames(t *testing.T) {
	t.Parallel()

	args := []string{
		"--api-key", "api-key-secret",
		"--token=token-secret",
		"--header", "Authorization: Bearer header-secret",
		"-c", `model_providers.example.api_key="config-secret"`,
		"prompt-secret",
		"--verbose",
		"-not-a-short-flag",
	}
	want := []string{
		"--api-key", redactedAgentCommandArg,
		"--token",
		"--header", redactedAgentCommandArg,
		"-c", redactedAgentCommandArg,
		redactedAgentCommandArg,
		"--verbose",
		redactedAgentCommandArg,
	}

	got := redactAgentCommandArgs(args)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("redactAgentCommandArgs = %v, want %v", got, want)
	}
}

func TestLogAgentCommandDoesNotExposeArgumentValues(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	cmd := exec.Command("codex",
		"--api-key", "api-key-secret",
		"--token=token-secret",
		"prompt-secret",
	)

	logAgentCommand(logger, "codex", cmd)

	for _, secret := range []string{"api-key-secret", "token-secret", "prompt-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("command log exposed %q: %s", secret, output.String())
		}
	}
	for _, diagnostic := range []string{"agent command", "provider=codex", "--api-key", "--token", redactedAgentCommandArg} {
		if !strings.Contains(output.String(), diagnostic) {
			t.Errorf("command log omitted %q: %s", diagnostic, output.String())
		}
	}
}
