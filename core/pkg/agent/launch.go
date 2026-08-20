package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
)

// Command identifies a runtime executable plus the argv prefix configured by
// its custom runtime profile. Prefix is always inserted before provider-owned
// protocol arguments.
type Command struct {
	Path   string
	Prefix []string
	logger *slog.Logger
}

// NewCommand creates a command value for probes outside a Backend.
func NewCommand(path string, prefix []string) Command {
	return Command{Path: path, Prefix: append([]string(nil), prefix...)}
}

// Argv returns a fresh argv with the runtime prefix before invocation args.
func (c Command) Argv(args ...string) []string {
	argv := make([]string, 0, len(c.Prefix)+len(args))
	argv = append(argv, c.Prefix...)
	return append(argv, args...)
}

func (c Command) exec(ctx context.Context, args ...string) *exec.Cmd {
	warnLaunchPrefixOverlap(c.Prefix, args, c.logger)
	return exec.CommandContext(ctx, c.Path, c.Argv(args...)...)
}

type invocationChooser func(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string)

func (c Command) execVia(ctx context.Context, choose invocationChooser, lookedUp string, args []string, logger *slog.Logger) (*exec.Cmd, string, []string) {
	warnLaunchPrefixOverlap(c.Prefix, args, logger)
	argv0, cmdArgs := choose(c.Path, lookedUp, c.Argv(args...), logger)
	return exec.CommandContext(ctx, argv0, cmdArgs...), argv0, cmdArgs
}

func (c Config) commandAt(path string) Command {
	return Command{Path: path, Prefix: c.LaunchPrefix, logger: c.Logger}
}

var launchPrefixBlockedArgs = map[string]map[string]blockedArgMode{
	"antigravity": antigravityBlockedArgs,
	"claude":      claudeBlockedArgs,
	"codebuddy":   codebuddyBlockedArgs,
	"codex":       codexBlockedArgs,
	"copilot":     copilotBlockedArgs,
	"cursor":      cursorBlockedArgs,
	"deveco":      devecoBlockedArgs,
	"grok":        grokBlockedArgs,
	"hermes":      hermesBlockedArgs,
	"kimi":        kimiBlockedArgs,
	"kiro":        kiroBlockedArgs,
	"opencode":    opencodeBlockedArgs,
	"openclaw":    openclawBlockedArgs,
	"pi":          piBlockedArgs,
	"qoder":       qoderBlockedArgs,
	"qwen":        qwenBlockedArgs,
	"traecli":     traecliBlockedArgs,
}

// FilterLaunchPrefix removes protocol-critical flags while retaining
// positional wrapper/subcommand tokens that define the custom runtime.
func FilterLaunchPrefix(agentType string, prefix []string, logger *slog.Logger) []string {
	if len(prefix) == 0 {
		return nil
	}
	blocked, ok := launchPrefixBlockedArgs[agentType]
	if !ok {
		return append([]string(nil), prefix...)
	}
	if logger == nil {
		logger = slog.Default()
	}
	filtered := make([]string, 0, len(prefix))
	for i := 0; i < len(prefix); i++ {
		arg := unshellQuoteArg(prefix[i])
		flag, isFlag := launchPrefixFlagName(arg)
		if !isFlag {
			filtered = append(filtered, arg)
			continue
		}
		mode, denied := blocked[flag]
		if !denied {
			filtered = append(filtered, arg)
			continue
		}
		logger.Warn("custom runtime fixed_args: blocked protocol-critical flag, skipping", "flag", flag)
		if !strings.Contains(arg, "=") && (mode == blockedWithValue ||
			(mode == blockedOptionalValue && i+1 < len(prefix) && !strings.HasPrefix(unshellQuoteArg(prefix[i+1]), "-"))) {
			i++
		}
	}
	return filtered
}

func launchPrefixFlagName(arg string) (string, bool) {
	arg = unshellQuoteArg(arg)
	if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
		return "", false
	}
	if idx := strings.IndexByte(arg, '='); idx > 0 {
		return arg[:idx], true
	}
	return arg, true
}

func warnLaunchPrefixOverlap(prefix, args []string, logger *slog.Logger) {
	if len(prefix) == 0 || len(args) == 0 || logger == nil {
		return
	}
	later := make(map[string]struct{}, len(args))
	for _, arg := range args {
		if flag, ok := launchPrefixFlagName(arg); ok {
			later[flag] = struct{}{}
		}
	}
	for _, arg := range prefix {
		flag, ok := launchPrefixFlagName(arg)
		if _, overlaps := later[flag]; ok && overlaps {
			logger.Warn("custom runtime fixed_args set a flag the runtime also sets; the runtime value wins", "flag", flag)
		}
	}
}
