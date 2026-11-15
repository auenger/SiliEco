package execenv

import (
	"strings"
	"testing"
)

// TestClassifyTask pins the precedence rule on classifyTask. All four
// kinds plus tiebreak cases for safety.
func TestClassifyRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  RunContextForEnv
		want runKind
	}{
		{"chat", RunContextForEnv{ChatSessionID: "c"}, kindChat},
		{"quick-create", RunContextForEnv{QuickCreatePrompt: "p"}, kindQuickCreate},
		{"autopilot", RunContextForEnv{AutopilotRunID: "r"}, kindAutopilotRunOnly},
		{"task-comment-triggered", RunContextForEnv{TaskID: "i", TriggerCommentID: "c"}, kindTask},
		{"task-assignment-triggered", RunContextForEnv{TaskID: "i"}, kindTask},
		{"task-bare", RunContextForEnv{}, kindTask},
		{"tiebreak-chat-vs-quick", RunContextForEnv{ChatSessionID: "c", QuickCreatePrompt: "p"}, kindChat},
		{"tiebreak-quick-vs-autopilot", RunContextForEnv{QuickCreatePrompt: "p", AutopilotRunID: "r"}, kindQuickCreate},
		{"tiebreak-autopilot-vs-comment", RunContextForEnv{AutopilotRunID: "r", TaskID: "i", TriggerCommentID: "c"}, kindAutopilotRunOnly},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRun(tc.ctx); got != tc.want {
				t.Errorf("classifyTask: got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestRunKindHasTaskContext pins the predicate that gates Project
// Context / Task Metadata / Sub-task Creation in the slim dispatcher.
func TestRunKindHasTaskContext(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind runKind
		want bool
	}{
		{kindTask, true},
		{kindAutopilotRunOnly, false},
		{kindQuickCreate, false},
		{kindChat, false},
	}
	for _, tc := range cases {
		if got := tc.kind.hasTaskContext(); got != tc.want {
			t.Errorf("kind=%d hasTaskContext: got %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestBuildMetaSkillContentBriefContent pins that buildMetaSkillContent
// renders the (now sole) brief: the `task get` one-liner is present and
// the retired legacy verbose description is not.
func TestBuildMetaSkillContentBriefContent(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", RunContextForEnv{
		TaskID:           "task-1",
		TriggerCommentID: "comment-1",
		AgentName:        "Eve",
		AgentID:          "eve-1",
	})

	if !strings.Contains(out, "- `silieco task get <id> --output json` — full task.\n") {
		t.Errorf("brief is missing the `task get` one-liner\n---\n%s", out)
	}
	if strings.Contains(out, "Get full task details.") {
		t.Errorf("brief still carries the retired legacy `task get` description\n---\n%s", out)
	}
}

// TestBuildMetaSkillContentSlimKindMatrix locks in which sections the
// slim brief emits per task kind, machine-checking the matrix documented
// on `buildMetaSkillContentSlim`. Heading is matched as a discrete line
// (preceded by newline + followed by newline) so inline references like
// "see ## Comment Formatting" do not trip the absence assertions.
func TestBuildMetaSkillContentSlimKindMatrix(t *testing.T) {

	baseRepo := []RepoContextForEnv{{URL: "https://example.com/x.git", Description: "x"}}
	baseSkill := []SkillContextForEnv{{Name: "skill-x", Description: "x"}}

	type sectionCheck struct {
		heading  string
		mustHave map[runKind]bool
	}
	allKinds := map[runKind]bool{
		kindTask: true, kindAutopilotRunOnly: true,
		kindQuickCreate: true, kindChat: true,
	}
	taskKinds := map[runKind]bool{kindTask: true}
	checks := []sectionCheck{
		{"# Silieco Agent Runtime", allKinds},
		{"## Background Task Safety", allKinds},
		{"## Agent Identity", allKinds},
		{"## Available Commands", allKinds},
		{"### Workflow", allKinds},
		{"## Important: Always Use the `silieco` CLI", allKinds},
		{"## Output", allKinds},
		{"## Comment Formatting", taskKinds},
		{"## Repositories", map[runKind]bool{
			kindTask: true, kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Task Metadata", taskKinds},
		{"## Instruction Precedence", taskKinds},
		{"## Sub-task Creation", taskKinds},
		{"## Skills", map[runKind]bool{
			kindTask: true, kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Mentions", taskKinds},
		{"## Attachments", taskKinds},
	}

	fixtures := map[runKind]RunContextForEnv{
		kindChat: {ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindQuickCreate: {QuickCreatePrompt: "p", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindAutopilotRunOnly: {AutopilotRunID: "r-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindTask: {TaskID: "i-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
	}

	for kind, ctx := range fixtures {
		out := buildMetaSkillContent("claude", ctx)
		for _, c := range checks {
			needle := "\n" + c.heading + "\n"
			firstLine := c.heading + "\n"
			present := strings.HasPrefix(out, firstLine) || strings.Contains(out, needle)
			want := c.mustHave[kind]
			if want && !present {
				t.Errorf("kind=%d: expected heading %q in slim brief", kind, c.heading)
			}
			if !want && present {
				t.Errorf("kind=%d: heading %q should NOT be in slim brief (matrix gating regression)", kind, c.heading)
			}
		}
	}
}

// TestSlimQuickCreateAvailableCommands locks the minimal-variant content
// for quick-create's Available Commands: `task create` present, every
// other Core command absent (the hard guardrails forbid the call).
func TestSlimQuickCreateAvailableCommands(t *testing.T) {

	out := buildMetaSkillContent("codex", RunContextForEnv{
		QuickCreatePrompt: "create an task about flaky tests",
		AgentName:         "Eve", AgentID: "eve-1",
	})

	for _, want := range []string{
		"## Available Commands",
		"silieco task create --title",
		"`silieco --help`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("quick_create slim Available Commands missing %q", want)
		}
	}

	for _, banned := range []string{
		"silieco task get <id>",
		"silieco task comment list <task-id>",
		"silieco task update <id>",
		"silieco task status <id> <status>",
		"silieco task comment add <task-id>",
		"silieco task metadata list <task-id>",
		"silieco task metadata set <task-id>",
		"silieco task metadata delete <task-id>",
		"silieco task children <id>",
		"silieco repo checkout <url>",
		"### Squad maintenance",
		"silieco squad member set-role",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("quick_create slim Available Commands should NOT advertise %q (hard guardrails forbid the call)", banned)
		}
	}
}

// TestBackgroundTaskSafetySlimHardPins asserts the slim brief carries the
// same hardened Background Task Safety pins as the legacy brief (SILI-4140).
// The verbose path is covered by
// TestInjectRuntimeConfigBackgroundTaskSafetyProviderAgnostic; this locks
// the compressed slim path so a future slim-brief trim can't quietly drop
// the no-background-and-yield / no-"standing by" guardrails that address
// the SILI-4091 mechanism.
func TestBackgroundRunSafetySlimHardPins(t *testing.T) {

	out := buildMetaSkillContent("claude", RunContextForEnv{
		TaskID: "i-1", TriggerCommentID: "tc-1",
		AgentName: "Eve", AgentID: "eve-1",
	})

	for _, want := range []string{
		"## Background Task Safety",
		"Do NOT end your turn while background tasks",
		"wait for a future notification/reminder",
		"run the work synchronously instead",
		"Never background-and-yield",
		"foreground tool call that blocks",
		// SILI-5274: an explicitly requested persistent local service is a
		// completed handoff, not unfinished run-owned work. Pin the narrow
		// exception and its readiness / cleanup / honesty requirements.
		"persistent service handoff",
		"running service itself is the requested deliverable",
		"stdio redirected to durable logs",
		"PID/profile",
		"verify readiness before replying",
		"survival as best-effort, not guaranteed",
		"does not cover tests, builds, CI polling",
		"are not agent-owned background tasks",
		"GitHub Actions after a successful push",
		"Do not wait for them by default",
		// SILI-5223 pins: named tool-shape bans, merge requirements
		// denied as acceptance criteria, replacement hand-off phrasing,
		// and the scoped escape hatch that keeps an explicitly requested
		// CI result both permitted and executable.
		"do NOT run `gh pr checks --watch`",
		"any sleep / retry loop that polls check status",
		"NOT your delivery acceptance criteria",
		"CI running: <PR link>",
		"unless the explicit exception below applies",
		"The one exception",
		"ONE foreground blocking call (`gh pr checks <pr> --watch`)",
		"running in the background so you can keep working",
		"standing by",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("slim Background Task Safety missing hardened pin %q\n---\n%s", want, out)
		}
	}
	// `gh run watch` may only appear as a banned command, never as the
	// section's example of how to wait properly.
	if strings.Contains(out, "e.g. `gh run watch`") {
		t.Errorf("slim Background Task Safety should not suggest waiting for external GitHub CI\n---\n%s", out)
	}
	// SILI-5274 review: with the persistent-service exception in the list, a
	// "The rules above ..." scoping sentence would sweep in work that is
	// precisely no longer run-owned after handoff.
	if strings.Contains(out, "The rules above") {
		t.Errorf("slim Background Task Safety must not reintroduce the ambiguous \"The rules above\" scoping sentence\n---\n%s", out)
	}
}
