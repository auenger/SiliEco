package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/silieco-ai/silieco/core/internal/cli"
	"github.com/silieco-ai/silieco/core/internal/util"
)

// resolveTextFlag picks between a `--<name>` inline value, a `--<name>-stdin`
// flag, and a `--<name>-file <path>` flag, mirroring the existing `--content`
// / `--content-stdin` pattern. It returns the resolved string and an error
// when more than one source is set, or when stdin/file is requested but
// produces no body. Inline flag values are passed through
// util.UnescapeBackslashEscapes so bash-double-quoted `\n` becomes a real
// newline; stdin and file bodies are returned verbatim so literal backslashes
// survive intact.
//
// The `-file` source exists for Windows agents: piping HEREDOC content to
// `--<name>-stdin` from Windows PowerShell silently drops non-ASCII bytes
// (PowerShell 5.1's `$OutputEncoding` defaults to ASCIIEncoding when piping
// to a native command), so Chinese / Cyrillic / any non-ASCII content
// arrives as `?`. Reading a UTF-8 file directly bypasses the shell's pipe
// re-encoding entirely. See tasks #2198 / #2236 / #2376.
func resolveTextFlag(cmd *cobra.Command, flagName string) (string, bool, error) {
	stdinFlag := flagName + "-stdin"
	fileFlag := flagName + "-file"
	useStdin, _ := cmd.Flags().GetBool(stdinFlag)
	inline, _ := cmd.Flags().GetString(flagName)
	filePath, _ := cmd.Flags().GetString(fileFlag)

	sources := 0
	if useStdin {
		sources++
	}
	if inline != "" {
		sources++
	}
	if filePath != "" {
		sources++
	}
	if sources > 1 {
		return "", false, fmt.Errorf("--%s, --%s, and --%s are mutually exclusive", flagName, stdinFlag, fileFlag)
	}

	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --%s: %w", stdinFlag, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, fmt.Errorf("stdin content for --%s is empty", stdinFlag)
		}
		return body, true, nil
	}
	if filePath != "" {
		if err := ensureFileFlagWithinWorkdir(cmd, fileFlag, flagName, filePath); err != nil {
			return "", false, err
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", false, fmt.Errorf("read file for --%s: %w", fileFlag, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, fmt.Errorf("file content for --%s is empty", fileFlag)
		}
		return body, true, nil
	}
	if inline == "" {
		return "", false, nil
	}
	return util.UnescapeBackslashEscapes(inline), true, nil
}

// ensureFileFlagWithinWorkdir fails closed when a --<name>-file path resolves
// outside the current working directory, unless --allow-external-file is set.
//
// Agent task workdirs are isolated per profile and per task; machine-shared
// scratch paths like /tmp are not. SILI-4252 traced a cross-environment context
// leak to exactly this gap: a quick-create run wrote its description to a fixed
// /tmp/desc.md, the write silently failed because a *different* environment's
// run had left a stale file there minutes earlier, and --description-file then
// fed that stale content into the new task. Requiring the file to live under
// the workdir turns "silently read another run's file" into a loud command
// failure — an "incorrect content" bug becomes a "command errored" bug.
func ensureFileFlagWithinWorkdir(cmd *cobra.Command, fileFlag, flagName, filePath string) error {
	if allow, _ := cmd.Flags().GetBool("allow-external-file"); allow {
		return nil
	}
	within, err := fileWithinWorkingDir(filePath)
	if err != nil {
		return fmt.Errorf("resolve --%s path %q: %w", fileFlag, filePath, err)
	}
	if !within {
		return fmt.Errorf(
			"--%s path %q resolves outside the current working directory; "+
				"write agent temp files inside the task workdir (e.g. ./%s.md) rather than machine-shared "+
				"paths like /tmp, where another run's stale file can be read by mistake. "+
				"Pass --allow-external-file to override.",
			fileFlag, filePath, flagName)
	}
	return nil
}

// fileWithinWorkingDir reports whether filePath resolves to a location inside
// the process working directory. Both sides are symlink-resolved so aliased
// roots (e.g. macOS /tmp -> /private/tmp) and symlinks planted inside the
// workdir fail closed. A path that does not exist yet is judged on its cleaned
// absolute form so the caller's os.ReadFile still surfaces the real not-found
// error afterwards.
func fileWithinWorkingDir(filePath string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	base := cwd
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		base = resolved
	}
	abs := filePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else {
		// The file may not exist yet (the caller's os.ReadFile surfaces that).
		// Resolve symlinks on the parent directory instead so the comparison
		// base and the candidate share the same canonical prefix — otherwise a
		// workdir under a symlinked root (e.g. macOS temp dirs) would falsely
		// read as "outside". A missing parent falls back to a plain clean.
		if resolvedParent, perr := filepath.EvalSymlinks(filepath.Dir(abs)); perr == nil {
			abs = filepath.Join(resolvedParent, filepath.Base(abs))
		} else {
			abs = filepath.Clean(abs)
		}
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Work with tasks",
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks in the workspace",
	RunE:  runTaskList,
}

var taskGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get task details",
	Args:  exactArgs(1),
	RunE:  runTaskGet,
}

var taskPullRequestsCmd = &cobra.Command{
	Use:     "pull-requests <id>",
	Aliases: []string{"prs"},
	Short:   "List pull requests linked to a task",
	Args:    exactArgs(1),
	RunE:    runTaskPullRequests,
}

var taskChildrenCmd = &cobra.Command{
	Use: "children <id>",

	Short: "List a task's sub-tasks grouped by stage",
	Args:  exactArgs(1),
	RunE:  runTaskChildren,
}

var taskCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new task",
	RunE:  runTaskCreate,
}

var taskUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a task",
	Args:  exactArgs(1),
	RunE:  runTaskUpdate,
}

var taskAssignCmd = &cobra.Command{
	Use:   "assign <id>",
	Short: "Assign a task to a member, agent, or squad",
	Args:  exactArgs(1),
	RunE:  runTaskAssign,
}

var taskStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Change task status",
	Long: "Change a task's status. Valid statuses: " +
		"backlog, todo, in_progress, in_review, done, blocked, cancelled.",
	Args: exactArgs(2),
	RunE: runTaskStatus,
}

var taskReorderCmd = &cobra.Command{
	Use:   "reorder <id>",
	Short: "Move a task within its status column",
	Long: "Reposition a task inside its current status column by computing a new\n" +
		"ordering position, the same value the board's drag-and-drop sets.\n\n" +
		"Pick exactly one target:\n" +
		"  --before <id>  place it directly above another task in the same column\n" +
		"  --after  <id>  place it directly below another task in the same column\n" +
		"  --top          move it to the top of its column\n" +
		"  --bottom       move it to the bottom of its column\n\n" +
		"Reorder stays inside the task's current column. To move an task to a\n" +
		"different column, change its status first with `silieco task status`.",
	Args: exactArgs(1),
	RunE: runTaskReorder,
}

// Comment subcommands.

var taskCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Work with task comments",
}

var taskCommentListCmd = &cobra.Command{
	Use:   "list <task-id>",
	Short: "List comments on a task",
	Args:  exactArgs(1),
	RunE:  runTaskCommentList,
}

var taskCommentAddCmd = &cobra.Command{
	Use:   "add <task-id>",
	Short: "Add a comment to a task",
	Args:  exactArgs(1),
	RunE:  runTaskCommentAdd,
}

var taskCommentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Delete a comment",
	Args:  exactArgs(1),
	RunE:  runTaskCommentDelete,
}

var taskCommentResolveCmd = &cobra.Command{
	Use:   "resolve <comment-id>",
	Short: "Resolve a comment thread",
	Args:  exactArgs(1),
	RunE:  runTaskCommentResolve,
}

var taskCommentUnresolveCmd = &cobra.Command{
	Use:   "unresolve <comment-id>",
	Short: "Unresolve a comment thread",
	Args:  exactArgs(1),
	RunE:  runTaskCommentUnresolve,
}

// Subscriber subcommands.

var taskSubscriberCmd = &cobra.Command{
	Use:   "subscriber",
	Short: "Work with task subscribers",
}

var taskSubscriberListCmd = &cobra.Command{
	Use:   "list <task-id>",
	Short: "List subscribers of a task",
	Args:  exactArgs(1),
	RunE:  runTaskSubscriberList,
}

var taskSubscriberAddCmd = &cobra.Command{
	Use:   "add <task-id>",
	Short: "Subscribe a user or agent to a task (defaults to the caller)",
	Args:  exactArgs(1),
	RunE:  runTaskSubscriberAdd,
}

var taskSubscriberRemoveCmd = &cobra.Command{
	Use:   "remove <task-id>",
	Short: "Unsubscribe a user or agent from a task (defaults to the caller)",
	Args:  exactArgs(1),
	RunE:  runTaskSubscriberRemove,
}

// Execution history subcommands.

var taskRunsCmd = &cobra.Command{
	Use:   "runs <task-id>",
	Short: "List execution history for a task",
	Args:  exactArgs(1),
	RunE:  runTaskRuns,
}

var taskRunMessagesCmd = &cobra.Command{
	Use:   "run-messages <run-id>",
	Short: "List messages for a run",
	Args:  exactArgs(1),
	RunE:  runTaskRunMessages,
}

var taskUsageCmd = &cobra.Command{
	Use:   "usage <task-id>",
	Short: "Show aggregated token usage for a task",
	Args:  exactArgs(1),
	RunE:  runTaskUsage,
}

var taskRerunCmd = &cobra.Command{
	Use:   "rerun <id>",
	Short: "Re-enqueue a task's current agent assignment as a fresh run",
	Args:  exactArgs(1),
	RunE:  runTaskRerun,
}

var taskCancelRunCmd = &cobra.Command{
	Use:   "cancel-run <run-id>",
	Short: "Cancel a running or queued run (interrupts in-flight agent)",
	Long: "Cancel a single run by its ID. Accepts the short ID prefix shown by `task runs`. " +
		"Use --task to scope short-ID resolution to a specific task when ambiguous. " +
		"Triggers daemon-side interrupt of any in-flight agent so it stops emitting tool calls promptly.",
	Args: exactArgs(1),
	RunE: runTaskCancelRun,
}

var taskSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search tasks by title or description",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskSearch,
}

var validTaskStatuses = []string{
	"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled",
}

var validTaskPriorities = []string{
	"urgent", "high", "medium", "low", "none",
}

// validTaskSortColumns are the sort keys `task list --sort` accepts. They
// mirror the server's ListTasks handler. "position" is the default and is
// always sorted ascending (the board's manual drag order), so --direction is
// only meaningful for the other columns.
var validTaskSortColumns = []string{
	"position", "title", "created_at", "start_date", "due_date", "priority",
}

// directionalTaskSortColumns are the sort keys for which --direction is
// meaningful: every valid column except "position". Derived from
// validTaskSortColumns so the two stay in sync.
var directionalTaskSortColumns = func() []string {
	cols := make([]string, 0, len(validTaskSortColumns)-1)
	for _, c := range validTaskSortColumns {
		if c != "position" {
			cols = append(cols, c)
		}
	}
	return cols
}()

func validateTaskStatus(status string) error {
	return validateTaskEnum("status", status, validTaskStatuses)
}

func validateTaskPriority(priority string) error {
	return validateTaskEnum("priority", priority, validTaskPriorities)
}

func validateTaskEnum(field, value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", "))
}

func init() {
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskPullRequestsCmd)
	taskCmd.AddCommand(taskChildrenCmd)
	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskUpdateCmd)
	taskCmd.AddCommand(taskAssignCmd)
	taskCmd.AddCommand(taskStatusCmd)
	taskCmd.AddCommand(taskReorderCmd)
	taskCmd.AddCommand(taskCommentCmd)
	taskCmd.AddCommand(taskSubscriberCmd)
	taskCmd.AddCommand(taskRunsCmd)
	taskCmd.AddCommand(taskRunMessagesCmd)
	taskCmd.AddCommand(taskUsageCmd)
	taskCmd.AddCommand(taskRerunCmd)
	taskCmd.AddCommand(taskCancelRunCmd)
	taskCmd.AddCommand(taskSearchCmd)

	taskCommentCmd.AddCommand(taskCommentListCmd)
	taskCommentCmd.AddCommand(taskCommentAddCmd)
	taskCommentCmd.AddCommand(taskCommentDeleteCmd)
	taskCommentCmd.AddCommand(taskCommentResolveCmd)
	taskCommentCmd.AddCommand(taskCommentUnresolveCmd)

	taskSubscriberCmd.AddCommand(taskSubscriberListCmd)
	taskSubscriberCmd.AddCommand(taskSubscriberAddCmd)
	taskSubscriberCmd.AddCommand(taskSubscriberRemoveCmd)

	// task list
	taskListCmd.Flags().String("output", "table", "Output format: table or json")
	taskListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	taskListCmd.Flags().String("status", "", "Filter by status")
	taskListCmd.Flags().String("priority", "", "Filter by priority")
	taskListCmd.Flags().String("assignee", "", "Filter by assignee name (member, agent, or squad; fuzzy match)")
	taskListCmd.Flags().String("assignee-id", "", "Filter by assignee UUID — member, agent, or squad (mutually exclusive with --assignee)")
	taskListCmd.Flags().String("project", "", "Filter by project ID")
	taskListCmd.Flags().StringSlice("metadata", nil, "Filter by metadata key=value (repeatable; combined with AND). Value is JSON-parsed: 'true'/'false' → bool, numbers → number, otherwise string. Wrap as '\"42\"' to force a string when the value would otherwise sniff as a number.")
	taskListCmd.Flags().Int("limit", 50, "Maximum number of tasks to return")
	taskListCmd.Flags().Int("offset", 0, "Number of tasks to skip (for pagination)")
	taskListCmd.Flags().String("sort", "", "Sort column: position (default, manual board order), title, created_at, start_date, due_date, priority")
	taskListCmd.Flags().String("direction", "", "Sort direction (asc or desc); requires --sort to be a non-position column (position is always ascending)")

	// task get
	taskGetCmd.Flags().String("output", "json", "Output format: table or json")

	// task pull-requests
	taskPullRequestsCmd.Flags().String("output", "table", "Output format: table or json")

	taskChildrenCmd.Flags().String("output", "table", "Output format: table or json")
	taskChildrenCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// task create
	taskCreateCmd.Flags().String("title", "", "Task title (required)")
	taskCreateCmd.Flags().String("description", "", "Task description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	taskCreateCmd.Flags().Bool("description-stdin", false, "Read task description from stdin (preserves multi-line content verbatim)")
	taskCreateCmd.Flags().String("description-file", "", "Read task description from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes). The path must be inside the current working directory unless --allow-external-file is set.")
	taskCreateCmd.Flags().Bool("allow-external-file", false, "Allow --description-file / --attachment to read a path outside the current working directory. Off by default so a stale file from another run/environment can't be picked up (SILI-4252).")
	taskCreateCmd.Flags().String("status", "", "Task status")
	taskCreateCmd.Flags().String("priority", "", "Task priority")
	taskCreateCmd.Flags().String("assignee", "", "Assignee name (member, agent, or squad; fuzzy match)")
	taskCreateCmd.Flags().String("assignee-id", "", "Assignee UUID — member, agent, or squad (mutually exclusive with --assignee)")
	taskCreateCmd.Flags().String("parent", "", "Parent task ID")
	taskCreateCmd.Flags().Int("stage", 0, "Stage ordinal (>=1) grouping this sub-task into an ordered barrier group under its parent; omit for unstaged. The parent assignee is woken only when every sub-task in a stage finishes.")
	taskCreateCmd.Flags().String("project", "", "Project ID")
	taskCreateCmd.Flags().String("workflow-instance", "", "SOP workflow run UUID (requires --workflow-stage)")
	taskCreateCmd.Flags().String("workflow-stage", "", "SOP Stage UUID (requires --workflow-instance)")
	taskCreateCmd.Flags().String("start-date", "", "Start date (calendar day, YYYY-MM-DD)")
	taskCreateCmd.Flags().String("due-date", "", "Due date (calendar day, YYYY-MM-DD)")
	taskCreateCmd.Flags().Bool("allow-duplicate", false, "Allow creating an task even when an active duplicate exists")
	taskCreateCmd.Flags().String("output", "json", "Output format: table or json")
	taskCreateCmd.Flags().StringSlice("attachment", nil, "File path(s) to attach (can be specified multiple times)")
	taskCreateCmd.Flags().StringSlice("attachment-id", nil, "Existing attachment UUID(s) to bind to the created task (can be specified multiple times)")

	// task update
	taskUpdateCmd.Flags().String("title", "", "New title")
	taskUpdateCmd.Flags().String("description", "", "New description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	taskUpdateCmd.Flags().Bool("description-stdin", false, "Read new description from stdin (preserves multi-line content verbatim)")
	taskUpdateCmd.Flags().String("description-file", "", "Read new description from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes). The path must be inside the current working directory unless --allow-external-file is set.")
	taskUpdateCmd.Flags().Bool("allow-external-file", false, "Allow --description-file to read a path outside the current working directory. Off by default so a stale temp file from another run/environment can't be picked up (SILI-4252).")
	taskUpdateCmd.Flags().String("status", "", "New status")
	taskUpdateCmd.Flags().String("priority", "", "New priority")
	taskUpdateCmd.Flags().String("assignee", "", "New assignee name (member, agent, or squad; fuzzy match)")
	taskUpdateCmd.Flags().String("assignee-id", "", "New assignee UUID — member, agent, or squad (mutually exclusive with --assignee)")
	taskUpdateCmd.Flags().String("project", "", "Project ID")
	taskUpdateCmd.Flags().String("workflow-instance", "", "SOP workflow run UUID (requires --workflow-stage; pass both flags as empty strings to detach from the SOP)")
	taskUpdateCmd.Flags().String("workflow-stage", "", "SOP Stage UUID (requires --workflow-instance; pass both flags as empty strings to detach from the SOP)")
	taskUpdateCmd.Flags().String("start-date", "", "New start date (calendar day, YYYY-MM-DD; pass empty string to clear)")
	taskUpdateCmd.Flags().String("due-date", "", "New due date (calendar day, YYYY-MM-DD)")
	taskUpdateCmd.Flags().String("parent", "", "Parent task ID (use --parent \"\" to clear)")
	taskUpdateCmd.Flags().Int("stage", 0, "Stage ordinal (>=1) for this sub-task; see `task create --stage`")
	taskUpdateCmd.Flags().Float64("position", 0, "Ordering position within the board column (lower sorts first); prefer `task reorder` for relative moves")
	taskUpdateCmd.Flags().Bool("no-start", false, "Apply the update without starting an agent run")
	taskUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// task status
	taskStatusCmd.Flags().Bool("no-start", false, "Change status without starting an agent run")
	taskStatusCmd.Flags().String("output", "table", "Output format: table or json")

	// task reorder
	registerTaskReorderFlags(taskReorderCmd)

	// task assign
	taskAssignCmd.Flags().String("to", "", "Assignee name (member, agent, or squad; fuzzy match)")
	taskAssignCmd.Flags().String("to-id", "", "Assignee UUID — member, agent, or squad (mutually exclusive with --to)")
	taskAssignCmd.Flags().Bool("unassign", false, "Remove current assignee")
	taskAssignCmd.Flags().Bool("no-start", false, "Assign ownership without starting an agent run")
	taskAssignCmd.Flags().String("output", "json", "Output format: table or json")

	// task comment list
	taskCommentListCmd.Flags().String("output", "table", "Output format: table or json")
	taskCommentListCmd.Flags().String("since", "", "Only return comments created after this timestamp (RFC3339)")
	taskCommentListCmd.Flags().String("thread", "", "Comment UUID — return the thread containing this comment (root + every descendant). May be a root or a reply id.")
	taskCommentListCmd.Flags().Int("tail", 0, "Only valid with --thread. Cap reply count to the N most recent replies; the thread root is always included (even with --tail 0). Use --before/--before-id to scroll to older replies.")
	taskCommentListCmd.Flags().Int("recent", 0, "Return the N most recently active threads (root + descendants per thread). Use --before/--before-id from the previous response to scroll to older threads.")
	taskCommentListCmd.Flags().Bool("roots-only", false, "Only return top-level comments (parent_id is null). Each root also carries reply_count + last_activity_at so you can triage which thread to open.")
	taskCommentListCmd.Flags().Bool("summary", false, "Clip each comment's content to a short preview (sets content_truncated) so you can scan a list without pulling full bodies. Composes with any mode.")
	taskCommentListCmd.Flags().Bool("full", false, "Escape hatch: return every comment in resolved threads verbatim. By default the complete-thread reads (default list, --recent, --thread without --tail) are folded — a resolved thread collapses to its root + conclusion, with the dropped count reported on the root — so you do not pay tokens for settled discussion. Pass --full when you need the folded discussion. No effect on --since/--tail/--roots-only reads, which are never folded.")
	taskCommentListCmd.Flags().String("before", "", "Cursor (RFC3339Nano timestamp). With --recent: thread cursor (last_activity_at). With --thread + --tail: reply cursor (reply created_at). Read from the X-Silieco-Next-Before response header; must be paired with --before-id.")
	taskCommentListCmd.Flags().String("before-id", "", "Cursor UUID. With --recent: thread root UUID. With --thread + --tail: oldest reply UUID. Read from the X-Silieco-Next-Before-Id response header; must be paired with --before.")

	// task runs
	taskRunsCmd.Flags().String("output", "table", "Output format: table or json")
	taskRunsCmd.Flags().Bool("full-id", false, "Show full task UUIDs in table output")

	// task usage
	taskUsageCmd.Flags().String("output", "table", "Output format: table or json")

	// task rerun
	taskRerunCmd.Flags().String("output", "json", "Output format: table or json")
	// task cancel-run
	taskCancelRunCmd.Flags().String("output", "json", "Output format: table or json")
	taskCancelRunCmd.Flags().String("task", "", "Task ID/key to scope short run ID prefix resolution")
	// task run-messages
	taskRunMessagesCmd.Flags().String("output", "json", "Output format: table or json")
	taskRunMessagesCmd.Flags().Int("since", 0, "Only return messages after this sequence number")
	taskRunMessagesCmd.Flags().String("task", "", "Task ID/key to scope short run ID prefix resolution")

	// task comment add
	taskCommentAddCmd.Flags().String("content", "", "Comment content (decodes \\n, \\r, \\t, \\\\; pipe via --content-stdin for multi-line bodies or to preserve literal backslashes)")
	taskCommentAddCmd.Flags().Bool("content-stdin", false, "Read comment content from stdin (preserves multi-line content verbatim)")
	taskCommentAddCmd.Flags().String("content-file", "", "Read comment content from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes). The path must be inside the current working directory unless --allow-external-file is set.")
	taskCommentAddCmd.Flags().Bool("allow-external-file", false, "Allow --content-file / --attachment to read a path outside the current working directory. Off by default so a stale file from another run/environment can't be picked up (SILI-4252).")
	taskCommentAddCmd.Flags().String("parent", "", "Parent comment ID to reply under. A comment-triggered agent task must reply under its trigger comment; omitting --parent to post a top-level comment is rejected")
	taskCommentAddCmd.Flags().StringSlice("attachment", nil, "File path(s) to attach (can be specified multiple times)")
	taskCommentAddCmd.Flags().String("output", "json", "Output format: table or json")

	// task comment resolve/unresolve
	taskCommentResolveCmd.Flags().String("output", "json", "Output format: table or json")
	taskCommentUnresolveCmd.Flags().String("output", "json", "Output format: table or json")

	// task search
	taskSearchCmd.Flags().Int("limit", 20, "Maximum number of results to return")
	taskSearchCmd.Flags().Bool("include-closed", false, "Include done and cancelled tasks")
	taskSearchCmd.Flags().String("output", "table", "Output format: table or json")

	// task subscriber list
	taskSubscriberListCmd.Flags().String("output", "table", "Output format: table or json")

	// task subscriber add
	taskSubscriberAddCmd.Flags().String("user", "", "Member or agent name to subscribe (fuzzy match; defaults to the caller)")
	taskSubscriberAddCmd.Flags().String("user-id", "", "Member or agent UUID to subscribe (mutually exclusive with --user)")
	taskSubscriberAddCmd.Flags().String("output", "json", "Output format: table or json")

	// task subscriber remove
	taskSubscriberRemoveCmd.Flags().String("user", "", "Member or agent name to unsubscribe (fuzzy match; defaults to the caller)")
	taskSubscriberRemoveCmd.Flags().String("user-id", "", "Member or agent UUID to unsubscribe (mutually exclusive with --user)")
	taskSubscriberRemoveCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Task commands
// ---------------------------------------------------------------------------

func runTaskList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		params.Set("priority", v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	_, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", taskAssigneeKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		params.Set("assignee_id", aID)
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		params.Set("project_id", project.ID)
	}
	if mdFlags, _ := cmd.Flags().GetStringSlice("metadata"); len(mdFlags) > 0 {
		filter, err := buildMetadataFilterQueryParam(mdFlags)
		if err != nil {
			return err
		}
		params.Set("metadata", filter)
	}
	sortVal, _ := cmd.Flags().GetString("sort")
	if sortVal != "" {
		valid := false
		for _, c := range validTaskSortColumns {
			if c == sortVal {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid --sort %q; valid values: %s", sortVal, strings.Join(validTaskSortColumns, ", "))
		}
		params.Set("sort", sortVal)
	}
	if v, _ := cmd.Flags().GetString("direction"); v != "" {
		d := strings.ToLower(v)
		if d != "asc" && d != "desc" {
			return fmt.Errorf("invalid --direction %q; valid values: asc, desc", v)
		}
		// position (the manual board order) is always ascending, so the server
		// ignores --direction for it. Reject the combination up front rather
		// than silently dropping the flag — a passed-but-ignored flag is a
		// footgun, especially in scripts.
		if sortVal == "" || sortVal == "position" {
			return fmt.Errorf("--direction requires --sort to be one of %s; position (the default manual board order) is always ascending", strings.Join(directionalTaskSortColumns, ", "))
		}
		params.Set("direction", d)
	}

	path := "/api/tasks"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	tasksRaw, _ := result["tasks"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		total, _ := result["total"].(float64)
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		hasMore := offset+len(tasksRaw) < int(total)
		wrapped := map[string]any{
			"tasks":    tasksRaw,
			"total":    int(total),
			"limit":    limit,
			"offset":   offset,
			"has_more": hasMore,
		}
		return cli.PrintJSON(os.Stdout, wrapped)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	if fullID {
		headers = []string{"KEY", "ID", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	}
	actors := loadActorDisplayLookup(ctx, client)
	rows := make([][]string, 0, len(tasksRaw))
	for _, raw := range tasksRaw {
		task, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		assignee := formatAssignee(task, actors)
		startDate := strVal(task, "start_date")
		if startDate != "" && len(startDate) >= 10 {
			startDate = startDate[:10]
		}
		dueDate := strVal(task, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		row := []string{
			taskDisplayKey(task),
			strVal(task, "title"),
			strVal(task, "status"),
			strVal(task, "priority"),
			assignee,
			startDate,
			dueDate,
		}
		if fullID {
			row = []string{
				taskDisplayKey(task),
				strVal(task, "id"),
				strVal(task, "title"),
				strVal(task, "status"),
				strVal(task, "priority"),
				assignee,
				startDate,
				dueDate,
			}
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTaskPullRequests(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(taskRef.ID)+"/pull-requests", &result); err != nil {
		return fmt.Errorf("list task pull requests: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	prs, _ := result["pull_requests"].([]any)
	printTaskPullRequestsTable(normalizePullRequestList(prs))
	return nil
}

func normalizePullRequestList(raw []any) []map[string]any {
	prs := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		pr, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prs = append(prs, pr)
	}
	return prs
}

func printTaskPullRequestsTable(prs []map[string]any) {
	headers := []string{"NUMBER", "STATE", "TITLE", "URL"}
	rows := make([][]string, 0, len(prs))
	for _, pr := range prs {
		rows = append(rows, []string{
			strVal(pr, "number"),
			strVal(pr, "state"),
			strVal(pr, "title"),
			pullRequestURL(pr),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func pullRequestURL(pr map[string]any) string {
	if url := strVal(pr, "url"); url != "" {
		return url
	}
	return strVal(pr, "html_url")
}

func runTaskGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var task map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID, &task); err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		assignee := formatAssignee(task, actors)
		startDate := strVal(task, "start_date")
		if startDate != "" && len(startDate) >= 10 {
			startDate = startDate[:10]
		}
		dueDate := strVal(task, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE", "DESCRIPTION"}
		rows := [][]string{{
			taskDisplayKey(task),
			strVal(task, "title"),
			strVal(task, "status"),
			strVal(task, "priority"),
			assignee,
			startDate,
			dueDate,
			strVal(task, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, task)
}

// childStage extracts the integer stage from a child task response map.
// Returns ok=false when the child is unstaged (stage null/absent).
func childStage(m map[string]any) (int, bool) {
	v, ok := m["stage"]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func runTaskChildren(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var resp struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/children", &resp); err != nil {
		return fmt.Errorf("list child tasks: %w", err)
	}
	children := resp.Tasks

	// Order by stage ascending (unstaged last), preserving the API's
	// within-stage order (position, then created_at desc).
	sort.SliceStable(children, func(i, j int) bool {
		si, oki := childStage(children[i])
		sj, okj := childStage(children[j])
		if oki != okj {
			return oki // staged before unstaged
		}
		return si < sj
	})

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		headers := []string{"STAGE", "KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE"}
		rows := make([][]string, 0, len(children))
		for _, c := range children {
			stageCell := "-"
			if s, ok := childStage(c); ok {
				stageCell = strconv.Itoa(s)
			}
			rows = append(rows, []string{
				stageCell,
				taskDisplayKey(c),
				strVal(c, "title"),
				strVal(c, "status"),
				strVal(c, "priority"),
				formatAssignee(c, actors),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	// JSON: group by stage so an agent can see, at a glance, how many
	// sub-tasks there are and which stage each belongs to.
	type stageGroup struct {
		Stage int              `json:"stage"`
		Total int              `json:"total"`
		Done  int              `json:"done"`
		Tasks []map[string]any `json:"tasks"`
	}
	stages := []stageGroup{}
	unstaged := []map[string]any{}
	idxByStage := map[int]int{}
	for _, c := range children {
		s, ok := childStage(c)
		if !ok {
			unstaged = append(unstaged, c)
			continue
		}
		gi, seen := idxByStage[s]
		if !seen {
			stages = append(stages, stageGroup{Stage: s})
			gi = len(stages) - 1
			idxByStage[s] = gi
		}
		stages[gi].Tasks = append(stages[gi].Tasks, c)
		stages[gi].Total++
		if st := strVal(c, "status"); st == "done" || st == "cancelled" {
			stages[gi].Done++
		}
	}
	return cli.PrintJSON(os.Stdout, map[string]any{
		"total":    len(children),
		"stages":   stages,
		"unstaged": unstaged,
	})
}

// isHTTPURL reports whether path is an http:// or https:// URL.
// Used to skip URL-shaped values passed to --attachment, which only
// accepts local file paths. Trims surrounding whitespace because
// agent-generated commands sometimes copy URLs with stray spaces.
func isHTTPURL(path string) bool {
	p := strings.TrimSpace(path)
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

// ensureAttachmentWithinWorkdir applies the same workdir containment guard as
// --description-file / --content-file (SILI-4252) to a local --attachment path.
// An agent that writes a chart/report to a machine-shared path like /tmp and
// then attaches it could otherwise pick up another run's — possibly another
// workspace's — stale file (the image version of the /tmp/desc.md leak). URL
// values are filtered by the caller and never reach here. --allow-external-file
// overrides, mirroring the text-flag escape hatch.
func ensureAttachmentWithinWorkdir(cmd *cobra.Command, filePath string) error {
	if allow, _ := cmd.Flags().GetBool("allow-external-file"); allow {
		return nil
	}
	within, err := fileWithinWorkingDir(filePath)
	if err != nil {
		return fmt.Errorf("resolve --attachment path %q: %w", filePath, err)
	}
	if !within {
		return fmt.Errorf(
			"--attachment path %q resolves outside the current working directory; "+
				"attach files generated inside the task workdir rather than machine-shared "+
				"paths like /tmp, where another run's stale file can be attached by mistake. "+
				"Pass --allow-external-file to override.",
			filePath)
	}
	return nil
}

// pendingAttachment is a local --attachment file that passed URL filtering and
// the workdir guard and has been read into memory, ready to upload.
type pendingAttachment struct {
	path string
	data []byte
}

// collectLocalAttachments validates and reads ALL local --attachment paths up
// front, before any upload. URL-shaped values are warned and skipped (the API
// only accepts local paths). Each remaining path is run through the SILI-4252
// workdir guard and read into memory; the first invalid or unreadable path
// returns an error with nothing uploaded. Both `task create` and
// `comment add` share this so an invalid attachment can never leave an earlier
// one uploaded as an orphaned task attachment while the task/comment is never
// created (which would duplicate on retry).
func collectLocalAttachments(cmd *cobra.Command, attachments []string) ([]pendingAttachment, error) {
	pending := make([]pendingAttachment, 0, len(attachments))
	for _, filePath := range attachments {
		if isHTTPURL(filePath) {
			fmt.Fprintf(os.Stderr, "Skipping --attachment %q: URLs are not supported here, only local file paths.\n", filePath)
			continue
		}
		if err := ensureAttachmentWithinWorkdir(cmd, filePath); err != nil {
			return nil, err
		}
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("read attachment %s: %w", filePath, readErr)
		}
		pending = append(pending, pendingAttachment{path: filePath, data: data})
	}
	return pending, nil
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	out := make([]string, 0, len(dst)+len(values))
	for _, v := range append(dst, values...) {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func quickCreateAttachmentIDsFromEnv() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("SILIECO_QUICK_CREATE_ATTACHMENT_IDS"))
	if raw == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("parse SILIECO_QUICK_CREATE_ATTACHMENT_IDS: %w", err)
	}
	return appendUniqueStrings(nil, ids...), nil
}

func runTaskCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	statusFlag, _ := cmd.Flags().GetString("status")
	if statusFlag != "" {
		if err := validateTaskStatus(statusFlag); err != nil {
			return err
		}
	}
	priorityFlag, _ := cmd.Flags().GetString("priority")
	if priorityFlag != "" {
		if err := validateTaskPriority(priorityFlag); err != nil {
			return err
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	// Use a longer timeout when attachments are present (file uploads can be slow).
	timeout := cli.APITimeout()
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = cli.AtLeastAPITimeout(60 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body := map[string]any{"title": title}
	desc, hasDesc, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return err
	}
	if hasDesc {
		if err := guardLocalPathLinks(desc, "task description",
			"Deliver the file itself with `silieco task create --attachment <path>` (repeatable) and drop the link."); err != nil {
			return err
		}
		body["description"] = desc
	}
	if statusFlag != "" {
		body["status"] = statusFlag
	}
	if priorityFlag != "" {
		body["priority"] = priorityFlag
	}
	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		parent, err := resolveTaskRef(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve parent task: %w", err)
		}
		body["parent_task_id"] = parent.ID
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve project: %w", err)
		}
		body["project_id"] = project.ID
	}
	workflowInstanceID, _ := cmd.Flags().GetString("workflow-instance")
	workflowStageID, _ := cmd.Flags().GetString("workflow-stage")
	if (workflowInstanceID == "") != (workflowStageID == "") {
		return fmt.Errorf("--workflow-instance and --workflow-stage must be provided together")
	}
	if workflowInstanceID != "" {
		body["workflow_instance_id"] = workflowInstanceID
		body["workflow_stage_id"] = workflowStageID
	}
	if cmd.Flags().Changed("stage") {
		stage, _ := cmd.Flags().GetInt("stage")
		if stage < 1 {
			return fmt.Errorf("--stage must be >= 1")
		}
		body["stage"] = stage
	}
	if v, _ := cmd.Flags().GetString("start-date"); v != "" {
		body["start_date"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}
	if v, _ := cmd.Flags().GetBool("allow-duplicate"); v {
		body["allow_duplicate"] = true
	}
	aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", taskAssigneeKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}

	// Quick-create stamp: when the daemon sets SILIECO_QUICK_CREATE_TASK_ID
	// before invoking the agent, the agent's `silieco task create` call
	// inherits the env var and tags the new task with origin_type=
	// quick_create + origin_id=<run_id>. The completion handler then
	// locates the task deterministically by origin instead of "most
	// recent task by this agent", which is racy when max_concurrent_runs
	// > 1 and the agent is creating other tasks in parallel.
	if taskID := os.Getenv("SILIECO_QUICK_CREATE_TASK_ID"); taskID != "" {
		body["origin_type"] = "quick_create"
		body["origin_id"] = taskID
	}
	attachmentIDs, _ := cmd.Flags().GetStringSlice("attachment-id")
	envAttachmentIDs, err := quickCreateAttachmentIDsFromEnv()
	if err != nil {
		return err
	}
	attachmentIDs = appendUniqueStrings(attachmentIDs, envAttachmentIDs...)
	if len(attachmentIDs) > 0 {
		body["attachment_ids"] = attachmentIDs
	}

	// Pre-validate attachments BEFORE creating the task so a bad path can
	// never produce a half-created task (which would otherwise trigger
	// callers — especially the agent doing quick-create — to retry the whole
	// `task create` and end up with duplicates). URLs are warned+skipped, the
	// workdir guard is applied, and every local path is read upfront; a failure
	// here surfaces pre-create so the task never lands.
	pending, err := collectLocalAttachments(cmd, attachments)
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/tasks", body, &result); err != nil {
		if msg, ok := activeDuplicateTaskCreateMessage(err); ok {
			return errors.New(msg)
		}
		return fmt.Errorf("create task: %w", err)
	}

	// Upload attachments and link them to the newly created task.
	// Failures here are partial-success: the task exists already, so
	// turning a non-zero exit on the caller would invite a retry that
	// duplicates the task. Warn on stderr and continue.
	taskID := strVal(result, "id")
	for _, att := range pending {
		if _, uploadErr := client.UploadFile(ctx, att.data, att.path, taskID); uploadErr != nil {
			fmt.Fprintf(os.Stderr, "warning: upload attachment %s failed (task already created, %s): %v\n",
				att.path, strVal(result, "identifier"), uploadErr)
			continue
		}
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", att.path)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			taskDisplayKey(result),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func activeDuplicateTaskCreateMessage(err error) (string, bool) {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		return "", false
	}
	var payload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(httpErr.Body), &payload) != nil {
		return "", false
	}
	if payload.Code != "active_duplicate_task" || payload.Error == "" {
		return "", false
	}
	return payload.Error, true
}

func runTaskUpdate(cmd *cobra.Command, args []string) error {
	noStart, _ := cmd.Flags().GetBool("no-start")
	statusChanged := cmd.Flags().Changed("status")
	statusFlag, _ := cmd.Flags().GetString("status")
	if statusChanged {
		if err := validateTaskStatus(statusFlag); err != nil {
			return err
		}
	}
	priorityChanged := cmd.Flags().Changed("priority")
	priorityFlag, _ := cmd.Flags().GetString("priority")
	if priorityChanged {
		if err := validateTaskPriority(priorityFlag); err != nil {
			return err
		}
	}
	workflowInstanceChanged := cmd.Flags().Changed("workflow-instance")
	workflowStageChanged := cmd.Flags().Changed("workflow-stage")
	if workflowInstanceChanged != workflowStageChanged {
		return fmt.Errorf("--workflow-instance and --workflow-stage must be provided together")
	}
	workflowInstanceID, workflowStageID := "", ""
	if workflowInstanceChanged {
		workflowInstanceID, _ = cmd.Flags().GetString("workflow-instance")
		workflowStageID, _ = cmd.Flags().GetString("workflow-stage")
		if (workflowInstanceID == "") != (workflowStageID == "") {
			return fmt.Errorf("--workflow-instance and --workflow-stage must both be non-empty to attach, or both be empty to detach")
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") || cmd.Flags().Changed("description-file") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return err
		}
		// `task update` has no --attachment flag, so the hint must point at the
		// command that does. Telling the agent to "pass --attachment" here would
		// name an argument this command rejects.
		if err := guardLocalPathLinks(desc, "task description",
			"`silieco task update` cannot carry files — deliver the file with `silieco task comment add <task-id> --attachment <path>` instead, and drop the link."); err != nil {
			return err
		}
		body["description"] = desc
	}
	if statusChanged {
		body["status"] = statusFlag
	}
	if priorityChanged {
		body["priority"] = priorityFlag
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		if v == "" {
			body["project_id"] = nil
		} else {
			project, err := resolveProjectID(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve project: %w", err)
			}
			body["project_id"] = project.ID
		}
	}
	if workflowInstanceChanged {
		if workflowInstanceID == "" {
			body["workflow_instance_id"] = nil
			body["workflow_stage_id"] = nil
		} else {
			body["workflow_instance_id"] = workflowInstanceID
			body["workflow_stage_id"] = workflowStageID
		}
	}
	if cmd.Flags().Changed("start-date") {
		v, _ := cmd.Flags().GetString("start-date")
		body["start_date"] = v
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		body["due_date"] = v
	}
	if cmd.Flags().Changed("assignee") || cmd.Flags().Changed("assignee-id") {
		aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", taskAssigneeKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		if hasAssignee {
			body["assignee_type"] = aType
			body["assignee_id"] = aID
		}
	}
	if cmd.Flags().Changed("parent") {
		v, _ := cmd.Flags().GetString("parent")
		if v == "" {
			body["parent_task_id"] = nil
		} else {
			parent, err := resolveTaskRef(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve parent task: %w", err)
			}
			body["parent_task_id"] = parent.ID
		}
	}
	if cmd.Flags().Changed("stage") {
		stage, _ := cmd.Flags().GetInt("stage")
		if stage < 1 {
			return fmt.Errorf("--stage must be >= 1")
		}
		body["stage"] = stage
	}
	if cmd.Flags().Changed("position") {
		v, _ := cmd.Flags().GetFloat64("position")
		body["position"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --priority, --assignee, etc.")
	}
	if noStart {
		body["suppress_run"] = true
	}
	if taskRef.Revision != "" {
		body["expected_revision"] = taskRef.Revision
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/tasks/"+taskRef.ID, body, &result); err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			taskDisplayKey(result),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runTaskAssign(cmd *cobra.Command, args []string) error {
	toName, _ := cmd.Flags().GetString("to")
	unassign, _ := cmd.Flags().GetBool("unassign")
	noStart, _ := cmd.Flags().GetBool("no-start")
	toNameSet := cmd.Flags().Changed("to")
	toIDSet := cmd.Flags().Changed("to-id")

	if !toNameSet && !toIDSet && !unassign {
		return fmt.Errorf("provide --to <name>, --to-id <uuid>, or --unassign")
	}
	if (toNameSet || toIDSet) && unassign {
		return fmt.Errorf("--to/--to-id and --unassign are mutually exclusive")
	}
	if noStart && unassign {
		return fmt.Errorf("--no-start cannot be used with --unassign")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	body := map[string]any{}
	displayTarget := toName
	if unassign {
		body["assignee_type"] = nil
		body["assignee_id"] = nil
	} else {
		aType, aID, _, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "to", "to-id", taskAssigneeKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
		if displayTarget == "" {
			displayTarget = loadActorDisplayLookup(ctx, client).actor(aType, aID)
		}
		if noStart {
			body["suppress_run"] = true
		}
	}
	if taskRef.Revision != "" {
		body["expected_revision"] = taskRef.Revision
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/tasks/"+taskRef.ID, body, &result); err != nil {
		return fmt.Errorf("assign task: %w", err)
	}

	if unassign {
		fmt.Fprintf(os.Stderr, "Task %s unassigned.\n", taskDisplayKey(result))
	} else {
		fmt.Fprintf(os.Stderr, "Task %s assigned to %s.\n", taskDisplayKey(result), displayTarget)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	status := args[1]
	noStart, _ := cmd.Flags().GetBool("no-start")

	if err := validateTaskStatus(status); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, id)
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	body := map[string]any{"status": status}
	if noStart {
		body["suppress_run"] = true
	}
	if taskRef.Revision != "" {
		body["expected_revision"] = taskRef.Revision
	}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/tasks/"+taskRef.ID, body, &result); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Task %s status changed to %s.\n", taskDisplayKey(result), status)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Reorder command
// ---------------------------------------------------------------------------

// registerTaskReorderFlags wires the reorder command's flags and declares its
// target selector as a cobra flag group: mutually exclusive so at most one of
// --before/--after/--top/--bottom is accepted, and one-required so at least one
// must be given. Declaring the rule (instead of counting the flags by hand in
// runTaskReorder) lets cobra reject zero-target and multi-target invocations
// before RunE with a canonical message, and makes shell completion group-aware
// — it hides the sibling target flags once one of them is set. It is shared with
// the command's tests so they exercise the exact flag-group wiring that ships.
func registerTaskReorderFlags(cmd *cobra.Command) {
	cmd.Flags().String("before", "", "Place the task directly above this task (same column)")
	cmd.Flags().String("after", "", "Place the task directly below this task (same column)")
	cmd.Flags().Bool("top", false, "Move the task to the top of its status column")
	cmd.Flags().Bool("bottom", false, "Move the task to the bottom of its status column")
	cmd.Flags().String("output", "json", "Output format: table or json")
	cmd.MarkFlagsMutuallyExclusive("before", "after", "top", "bottom")
	cmd.MarkFlagsOneRequired("before", "after", "top", "bottom")
}

// runTaskReorder repositions an task inside its current status column. The
// new position is computed client-side by computeReorderPosition, which mirrors
// the board/list drag-and-drop math (computePosition in
// packages/views/tasks/utils/drag-utils.ts) so the CLI and UI agree on where
// an task lands. Only the task's own position changes; its column membership
// (status) is left untouched, so cross-column moves still go through
// `task status`.
func runTaskReorder(cmd *cobra.Command, args []string) error {
	before, _ := cmd.Flags().GetString("before")
	after, _ := cmd.Flags().GetString("after")
	top, _ := cmd.Flags().GetBool("top")
	bottom, _ := cmd.Flags().GetBool("bottom")

	// "Exactly one of --before/--after/--top/--bottom" is enforced declaratively
	// by the command's mutually-exclusive, one-required flag group (see
	// registerTaskReorderFlags), so cobra rejects zero-target and multi-target
	// invocations before RunE runs. Cobra keys off flag *presence* (Changed),
	// not value, so guard the cases it cannot see: a --before/--after passed
	// empty (e.g. an unset shell variable), or a --top/--bottom explicitly set
	// to false (e.g. `--top=false` from a generated command). Each counts as
	// "set" for the group yet selects no real target, and would otherwise fall
	// through to a confusing "not found in column" error.
	if cmd.Flags().Changed("before") && before == "" {
		return fmt.Errorf("--before requires an task ID or key")
	}
	if cmd.Flags().Changed("after") && after == "" {
		return fmt.Errorf("--after requires an task ID or key")
	}
	if cmd.Flags().Changed("top") && !top {
		return fmt.Errorf("--top cannot be set to false; pass it on its own to move the task to the top of its column")
	}
	if cmd.Flags().Changed("bottom") && !bottom {
		return fmt.Errorf("--bottom cannot be set to false; pass it on its own to move the task to the bottom of its column")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wsID := client.WorkspaceID
	if wsID == "" {
		wsID, err = requireWorkspaceID(cmd)
		if err != nil {
			return err
		}
	}

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}
	target, err := fetchTask(ctx, client, taskRef.ID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	status := strVal(target, "status")
	if status == "" {
		return fmt.Errorf("task %s has no status, cannot determine its column", taskRef.Display)
	}

	// Resolve the relative target up front, before any no-op shortcut, so a bad
	// --before/--after value (typo, self-reference) always errors instead of
	// being swallowed by the single-task-column fast path below.
	relative := before != "" || after != ""
	var otherRef resolvedID
	if relative {
		otherInput := before
		if after != "" {
			otherInput = after
		}
		otherRef, err = resolveTaskRef(ctx, client, otherInput)
		if err != nil {
			return fmt.Errorf("resolve target task: %w", err)
		}
		if otherRef.ID == taskRef.ID {
			return fmt.Errorf("cannot reorder task %s relative to itself", taskRef.Display)
		}
	}

	// Scope the column to the task's own project (when it has one) so the
	// computed position lands relative to the project board the task lives on,
	// not a blend of every project's tasks in this status.
	projectID := strVal(target, "project_id")
	column, err := fetchTaskColumn(ctx, client, wsID, projectID, status)
	if err != nil {
		return fmt.Errorf("list %s column: %w", status, err)
	}

	// Build the column order with the target removed, plus a lookup of every
	// position in the column (the target's own included, for the no-op check).
	positions := make(map[string]float64, len(column))
	ordered := make([]string, 0, len(column))
	for _, raw := range column {
		id := strVal(raw, "id")
		if id == "" {
			continue
		}
		positions[id] = floatVal(raw, "position")
		if id != taskRef.ID {
			ordered = append(ordered, id)
		}
	}
	if len(ordered) == 0 {
		// The active task is alone in its column. A relative move cannot
		// succeed here (its target is necessarily in another column), so report
		// that rather than a misleading "nothing to reorder".
		if relative {
			return reorderTargetNotInColumnError(ctx, client, otherRef, taskRef, status)
		}
		fmt.Fprintf(os.Stderr, "Task %s is the only task in the %s column; nothing to reorder.\n", taskRef.Display, status)
		return taskReorderOutput(cmd, target)
	}

	insertIdx := 0
	switch {
	case top:
		insertIdx = 0
	case bottom:
		insertIdx = len(ordered)
	default:
		idx := indexOfString(ordered, otherRef.ID)
		if idx == -1 {
			return reorderTargetNotInColumnError(ctx, client, otherRef, taskRef, status)
		}
		if before != "" {
			insertIdx = idx
		} else {
			insertIdx = idx + 1
		}
	}

	reordered := make([]string, 0, len(ordered)+1)
	reordered = append(reordered, ordered[:insertIdx]...)
	reordered = append(reordered, taskRef.ID)
	reordered = append(reordered, ordered[insertIdx:]...)

	currentPos := positions[taskRef.ID]
	newPos := computeReorderPosition(reordered, taskRef.ID, positions, currentPos)
	if newPos == currentPos {
		fmt.Fprintf(os.Stderr, "Task %s is already in that position.\n", taskRef.Display)
		return taskReorderOutput(cmd, target)
	}

	var result map[string]any
	body := map[string]any{"position": newPos}
	if taskRef.Revision != "" {
		body["expected_revision"] = taskRef.Revision
	}
	if err := client.PutJSON(ctx, "/api/tasks/"+url.PathEscape(taskRef.ID), body, &result); err != nil {
		return fmt.Errorf("reorder task: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Task %s reordered.\n", taskDisplayKey(result))
	return taskReorderOutput(cmd, result)
}

// issueReorderOutput prints an task map as a table or JSON, matching the
// update command's output contract.
func taskReorderOutput(cmd *cobra.Command, task map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			taskDisplayKey(task),
			strVal(task, "title"),
			strVal(task, "status"),
			strVal(task, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, task)
}

// reorderTargetNotInColumnError explains why a --before/--after target could
// not be used. It fetches the target only to report its actual column in the
// message, so the common mistake (target lives in a different column) gets a
// precise, actionable error instead of a bare "not found".
func reorderTargetNotInColumnError(ctx context.Context, client *cli.APIClient, otherRef, taskRef resolvedID, status string) error {
	if other, err := fetchTask(ctx, client, otherRef.ID); err == nil {
		if otherStatus := strVal(other, "status"); otherStatus != "" && otherStatus != status {
			return fmt.Errorf("task %s is in the %q column but %s is in %q; move one with `silieco task status` first, or pick a target in the same column", otherRef.Display, otherStatus, taskRef.Display, status)
		}
	}
	return fmt.Errorf("task %s was not found in the %q column", otherRef.Display, status)
}

// fetchTask retrieves a single task by canonical ID.
func fetchTask(ctx context.Context, client *cli.APIClient, id string) (map[string]any, error) {
	var task map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(id), &task); err != nil {
		return nil, err
	}
	return task, nil
}

// fetchTaskColumn returns every task in a status column ordered by position
// ascending, paginating through the list endpoint so columns larger than one
// page (the server caps a page at 100) still produce a complete, correctly
// ordered set. A non-empty projectID scopes the column to that project,
// matching a project board; an empty projectID lists the whole workspace
// column.
func fetchTaskColumn(ctx context.Context, client *cli.APIClient, workspaceID, projectID, status string) ([]map[string]any, error) {
	var all []map[string]any
	offset := 0
	for {
		params := url.Values{}
		params.Set("workspace_id", workspaceID)
		params.Set("status", status)
		if projectID != "" {
			params.Set("project_id", projectID)
		}
		params.Set("sort", "position")
		params.Set("limit", "100")
		params.Set("offset", fmt.Sprintf("%d", offset))

		var result map[string]any
		if err := client.GetJSON(ctx, "/api/tasks?"+params.Encode(), &result); err != nil {
			return nil, err
		}
		page, _ := result["tasks"].([]any)
		for _, raw := range page {
			if m, ok := raw.(map[string]any); ok {
				all = append(all, m)
			}
		}
		total, _ := result["total"].(float64)
		offset += len(page)
		if len(page) == 0 || offset >= int(total) {
			break
		}
	}
	return all, nil
}

// computeReorderPosition computes the position that places activeID at its
// index in ids. It mirrors computePosition in
// packages/views/tasks/utils/drag-utils.ts: the top of a column is one less
// than the next item, the bottom is one more than the previous, and any
// interior slot is the midpoint of its neighbours. fallback is returned when
// activeID is alone in (or absent from) the column, leaving its position
// unchanged.
func computeReorderPosition(ids []string, activeID string, positions map[string]float64, fallback float64) float64 {
	idx := indexOfString(ids, activeID)
	if idx == -1 || len(ids) == 1 {
		return fallback
	}
	if idx == 0 {
		return positions[ids[1]] - 1
	}
	if idx == len(ids)-1 {
		return positions[ids[idx-1]] + 1
	}
	return (positions[ids[idx-1]] + positions[ids[idx+1]]) / 2
}

func indexOfString(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

func floatVal(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

// ---------------------------------------------------------------------------
// Comment commands
// ---------------------------------------------------------------------------

func runTaskCommentList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	since, _ := cmd.Flags().GetString("since")
	thread, _ := cmd.Flags().GetString("thread")
	recent, _ := cmd.Flags().GetInt("recent")
	tail, _ := cmd.Flags().GetInt("tail")
	rootsOnly, _ := cmd.Flags().GetBool("roots-only")
	summary, _ := cmd.Flags().GetBool("summary")
	full, _ := cmd.Flags().GetBool("full")
	// Flags().Changed distinguishes "user did not pass --recent" from
	// "user explicitly passed --recent 0" (or a negative value). The
	// GetInt zero-value collapses both cases, which would otherwise
	// cause us to silently drop an invalid value and fall back to the
	// default unparameterized list — exactly the drift Elon flagged in
	// the PR #2787 second review. --tail follows the same pattern, and
	// also keeps "--tail 0" (root-only) distinguishable from "no --tail".
	recentSet := cmd.Flags().Changed("recent")
	tailSet := cmd.Flags().Changed("tail")
	before, _ := cmd.Flags().GetString("before")
	beforeID, _ := cmd.Flags().GetString("before-id")

	// Mirror the server-side combination rules client-side so the user gets
	// a clear local error instead of a 400 round-trip. These match the
	// validation in handler.ListComments (core/internal/handler/comment.go).
	if recentSet && recent <= 0 {
		return fmt.Errorf("--recent must be a positive integer")
	}
	if tailSet && tail < 0 {
		return fmt.Errorf("--tail must be a non-negative integer (0 returns just the thread root)")
	}
	if thread != "" && recentSet {
		return fmt.Errorf("--thread and --recent are mutually exclusive")
	}
	if rootsOnly && thread != "" {
		return fmt.Errorf("--roots-only and --thread are mutually exclusive")
	}
	if rootsOnly && recentSet {
		return fmt.Errorf("--roots-only and --recent are mutually exclusive")
	}
	if rootsOnly && tailSet {
		return fmt.Errorf("--roots-only and --tail are mutually exclusive")
	}
	if rootsOnly && before != "" {
		return fmt.Errorf("--roots-only does not support --before / --before-id")
	}
	if tailSet && thread == "" {
		return fmt.Errorf("--tail requires --thread (it is a thread-scoped limit)")
	}
	if (before == "") != (beforeID == "") {
		return fmt.Errorf("--before and --before-id must be set together (composite cursor for stable pagination)")
	}
	if before != "" && !recentSet && !(thread != "" && tailSet) {
		return fmt.Errorf("--before / --before-id require --recent (thread cursor) or --thread + --tail (reply cursor)")
	}

	params := url.Values{}
	if since != "" {
		params.Set("since", since)
	}
	if rootsOnly {
		params.Set("roots_only", "true")
	}
	if summary {
		params.Set("summary", "true")
	}
	// Resolve-aware folding is the default on the complete-thread reads (default
	// list, --recent, --thread without --tail): a resolved thread collapses to
	// root + conclusion so an agent does not pay tokens for settled discussion.
	// --full opts out. The partial-thread reads (--since / --tail) and
	// --roots-only cannot be folded safely (server rejects fold there), so we
	// never send fold for them and --full is a harmless no-op. Sending fold only
	// when eligible keeps every existing read command — including the prompt's
	// `--thread <id> --tail 30` cold path — working unchanged.
	foldEligible := !rootsOnly && since == "" && !tailSet
	if foldEligible && !full {
		params.Set("fold", "true")
	}
	if thread != "" {
		params.Set("thread", thread)
	}
	if tailSet {
		params.Set("tail", fmt.Sprintf("%d", tail))
	}
	if recentSet {
		params.Set("recent", fmt.Sprintf("%d", recent))
	}
	if before != "" {
		params.Set("before", before)
		params.Set("before_id", beforeID)
	}

	path := "/api/tasks/" + taskRef.ID + "/comments"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var comments []map[string]any
	respHeaders, err := client.GetJSONWithHeaders(ctx, path, &comments)
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}
	// The server emits the next-page cursor in headers when there is likely
	// an older page. Surface it on stderr so an operator (and the agent
	// prompt update that follows this PR) can scroll deeper without having
	// to dig into the raw HTTP response. Label depends on which paging mode
	// the caller is in — under --recent the cursor is a thread cursor;
	// under --thread + --tail it is a reply cursor inside that thread.
	if nb := respHeaders.Get("X-Silieco-Next-Before"); nb != "" {
		if nbid := respHeaders.Get("X-Silieco-Next-Before-Id"); nbid != "" {
			label := "Next thread cursor"
			if thread != "" && tailSet {
				label = "Next reply cursor"
			}
			fmt.Fprintf(os.Stderr, "%s: --before %s --before-id %s\n", label, nb, nbid)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, comments)
	}

	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "PARENT", "AUTHOR", "TYPE", "CONTENT", "CREATED"}
	rows := make([][]string, 0, len(comments))
	for _, c := range comments {
		content := strVal(c, "content")
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		created := strVal(c, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		parentID := strVal(c, "parent_id")
		if parentID == "" {
			parentID = "—"
		}
		rows = append(rows, []string{
			strVal(c, "id"),
			parentID,
			actors.actor(strVal(c, "author_type"), strVal(c, "author_id")),
			strVal(c, "type"),
			content,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTaskCommentAdd(cmd *cobra.Command, args []string) error {
	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	if !hasContent {
		return fmt.Errorf("--content, --content-stdin, or --content-file is required")
	}
	if err := guardLocalPathLinks(content, "comment body",
		"Deliver the file itself with `silieco task comment add <task-id> --attachment <path>` (repeatable) and drop the link."); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	// Use a longer timeout when attachments are present (file uploads can be slow).
	timeout := cli.APITimeout()
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = cli.AtLeastAPITimeout(60 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}
	taskID := taskRef.ID

	// Validate and read ALL attachments before uploading any. URLs are skipped
	// with a warning — `--attachment` only accepts local file paths. Reading
	// everything up front means a later invalid path (external / symlink escape
	// caught by the workdir guard) aborts the call with ZERO uploads, instead
	// of leaving an earlier file uploaded as an orphaned task attachment while
	// the comment is never posted (which would duplicate on retry — SILI-4252).
	pending, err := collectLocalAttachments(cmd, attachments)
	if err != nil {
		return err
	}
	var attachmentIDs []string
	for _, att := range pending {
		id, uploadErr := client.UploadFile(ctx, att.data, att.path, taskID)
		if uploadErr != nil {
			return fmt.Errorf("upload attachment %s: %w", att.path, uploadErr)
		}
		attachmentIDs = append(attachmentIDs, id)
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", att.path)
	}

	body := map[string]any{"content": content}
	if parentID, _ := cmd.Flags().GetString("parent"); parentID != "" {
		body["parent_id"] = parentID
	}
	if len(attachmentIDs) > 0 {
		body["attachment_ids"] = attachmentIDs
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/tasks/"+taskID+"/comments", body, &result); err != nil {
		return fmt.Errorf("add comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment added to task %s.\n", taskRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runTaskCommentDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/comments/"+args[0]); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment %s deleted.\n", args[0])
	return nil
}

func runTaskCommentResolve(cmd *cobra.Command, args []string) error {
	return runTaskCommentResolution(cmd, args[0], true)
}

func runTaskCommentUnresolve(cmd *cobra.Command, args []string) error {
	return runTaskCommentResolution(cmd, args[0], false)
}

func runTaskCommentResolution(cmd *cobra.Command, commentID string, resolve bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/comments/" + url.PathEscape(commentID) + "/resolve"
	var result map[string]any
	if resolve {
		if err := client.PostJSON(ctx, path, nil, &result); err != nil {
			return fmt.Errorf("resolve comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s resolved.\n", commentID)
	} else {
		if err := client.DeleteJSONResponse(ctx, path, &result); err != nil {
			return fmt.Errorf("unresolve comment: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Comment %s unresolved.\n", commentID)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Execution history commands
// ---------------------------------------------------------------------------

func runTaskRuns(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var runs []map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/runs", &runs); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, runs)
	}

	actors := loadActorDisplayLookup(ctx, client)
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "AGENT", "STATUS", "STARTED", "COMPLETED", "ERROR"}
	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		started := strVal(r, "started_at")
		if len(started) >= 16 {
			started = started[:16]
		}
		completed := strVal(r, "completed_at")
		if len(completed) >= 16 {
			completed = completed[:16]
		}
		errMsg := strVal(r, "error")
		if utf8.RuneCountInString(errMsg) > 50 {
			runes := []rune(errMsg)
			errMsg = string(runes[:47]) + "..."
		}
		rows = append(rows, []string{
			displayID(strVal(r, "id"), fullID),
			actors.agent(strVal(r, "agent_id")),
			strVal(r, "status"),
			started,
			completed,
			errMsg,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTaskUsage(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(taskRef.ID)+"/usage", &result); err != nil {
		return fmt.Errorf("get task usage: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	// JSON numbers decode to float64; formatMetadataValue renders them as clean
	// integers (no scientific notation for large cache-token counts).
	headers := []string{"INPUT_TOKENS", "OUTPUT_TOKENS", "CACHE_READ", "CACHE_WRITE", "RUNS"}
	rows := [][]string{{
		formatMetadataValue(result["total_input_tokens"]),
		formatMetadataValue(result["total_output_tokens"]),
		formatMetadataValue(result["total_cache_read_tokens"]),
		formatMetadataValue(result["total_cache_write_tokens"]),
		formatMetadataValue(result["task_count"]),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTaskRunMessages(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskID := ""
	runInput, _ := cmd.Flags().GetString("task")
	if runInput != "" {
		taskRef, err := resolveTaskRef(ctx, client, runInput)
		if err != nil {
			return fmt.Errorf("resolve run: %w", err)
		}
		taskID = taskRef.ID
	}
	taskRef, err := resolveAgentRunID(ctx, client, taskID, args[0])
	if err != nil {
		return fmt.Errorf("resolve task run: %w", err)
	}

	path := "/api/runs/" + url.PathEscape(taskRef.ID) + "/messages"
	if since, _ := cmd.Flags().GetInt("since"); since > 0 {
		path += fmt.Sprintf("?since=%d", since)
	}

	var messages []map[string]any
	if err := client.GetJSON(ctx, path, &messages); err != nil {
		return fmt.Errorf("list run messages: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, messages)
	}

	headers := []string{"SEQ", "TYPE", "TOOL", "CONTENT"}
	rows := make([][]string, 0, len(messages))
	for _, m := range messages {
		content := strVal(m, "content")
		if content == "" {
			content = strVal(m, "output")
		}
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		seq := ""
		if v, ok := m["seq"]; ok {
			seq = fmt.Sprintf("%v", v)
		}
		rows = append(rows, []string{
			seq,
			strVal(m, "type"),
			strVal(m, "tool"),
			content,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Search command
// ---------------------------------------------------------------------------

func runTaskRerun(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var task map[string]any
	if err := client.PostJSON(ctx, "/api/tasks/"+taskRef.ID+"/rerun", map[string]any{}, &task); err != nil {
		return fmt.Errorf("rerun task: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, task)
	}
	agent := loadActorDisplayLookup(ctx, client).agent(strVal(task, "agent_id"))
	fmt.Fprintf(os.Stdout, "Re-enqueued task %s on agent %s\n", strVal(task, "id"), agent)
	return nil
}

// runTaskCancelRun cancels a single task by ID. It accepts the short ID
// prefix shown by `task runs` (resolved through resolveTaskRunID), and uses
// /api/runs/{runId}/cancel which updates the run row to status=cancelled
// and triggers the daemon-side interrupt path (#2107) so an in-flight agent
// stops emitting tool calls promptly instead of running until its own timeout.
func runTaskCancelRun(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskScope := ""
	runInput, _ := cmd.Flags().GetString("task")
	if runInput != "" {
		taskRef, err := resolveTaskRef(ctx, client, runInput)
		if err != nil {
			return fmt.Errorf("resolve run: %w", err)
		}
		taskScope = taskRef.ID
	}
	taskRef, err := resolveAgentRunID(ctx, client, taskScope, args[0])
	if err != nil {
		return fmt.Errorf("resolve task run: %w", err)
	}

	var result map[string]any
	path := "/api/runs/" + url.PathEscape(taskRef.ID) + "/cancel"
	if err := client.PostJSON(ctx, path, map[string]any{}, &result); err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	status := strVal(result, "status")
	if status == "" {
		status = "cancelled"
	}
	fmt.Fprintf(os.Stdout, "Task %s -> status=%s\n", taskRef.ID, status)
	return nil
}

func runTaskSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	params.Set("q", args[0])
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetBool("include-closed"); v {
		params.Set("include_closed", "true")
	}

	path := "/api/tasks/search?" + params.Encode()

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("search tasks: %w", err)
	}

	tasksRaw, _ := result["tasks"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	headers := []string{"KEY", "TITLE", "STATUS", "MATCH"}
	rows := make([][]string, 0, len(tasksRaw))
	for _, raw := range tasksRaw {
		task, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		matchInfo := strVal(task, "match_source")
		if snippet := strVal(task, "matched_snippet"); snippet != "" {
			if utf8.RuneCountInString(snippet) > 50 {
				runes := []rune(snippet)
				snippet = string(runes[:47]) + "..."
			}
			matchInfo += ": " + snippet
		}
		rows = append(rows, []string{
			strVal(task, "identifier"),
			strVal(task, "title"),
			strVal(task, "status"),
			matchInfo,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Subscriber commands
// ---------------------------------------------------------------------------

func runTaskSubscriberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var subscribers []map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/subscribers", &subscribers); err != nil {
		return fmt.Errorf("list subscribers: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, subscribers)
	}

	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"USER", "REASON", "CREATED"}
	rows := make([][]string, 0, len(subscribers))
	for _, s := range subscribers {
		created := strVal(s, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		rows = append(rows, []string{
			actors.actor(strVal(s, "user_type"), strVal(s, "user_id")),
			strVal(s, "reason"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTaskSubscriberAdd(cmd *cobra.Command, args []string) error {
	return runTaskSubscriberMutation(cmd, args[0], "subscribe")
}

func runTaskSubscriberRemove(cmd *cobra.Command, args []string) error {
	return runTaskSubscriberMutation(cmd, args[0], "unsubscribe")
}

// runTaskSubscriberMutation shares subscribe/unsubscribe logic — both endpoints
// take the same request body and only differ in the path.
func runTaskSubscriberMutation(cmd *cobra.Command, taskID, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, taskID)
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	body := map[string]any{}
	userName, _ := cmd.Flags().GetString("user")
	uType, uID, hasUser, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "user", "user-id", memberOrAgentKinds)
	if resolveErr != nil {
		return fmt.Errorf("resolve user: %w", resolveErr)
	}
	if hasUser {
		body["user_type"] = uType
		body["user_id"] = uID
	}

	var result map[string]any
	path := "/api/tasks/" + taskRef.ID + "/" + action
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("%s task: %w", action, err)
	}

	target := "caller"
	if userName != "" {
		target = userName
	} else if hasUser {
		target = loadActorDisplayLookup(ctx, client).actor(uType, uID)
	}
	if action == "subscribe" {
		fmt.Fprintf(os.Stderr, "Subscribed %s to task %s.\n", target, taskRef.Display)
	} else {
		fmt.Fprintf(os.Stderr, "Unsubscribed %s from task %s.\n", target, taskRef.Display)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type assigneeMatch struct {
	Type string // "member", "agent", or "squad"
	ID   string // user_id for members, agent id for agents, squad id for squads
	Name string
}

// assigneeKinds is the set of entity types a given flag is allowed to resolve
// to. Task assignees accept all three (`issueAssigneeKinds`), while
// project lead and task subscribers are member-or-agent only
// (`memberOrAgentKinds`) — the DB CHECK on `project.lead_type` and the
// `isWorkspaceEntity` switch in the subscriber handler both reject `squad`,
// so resolving to (squad, ...) for those callers would surface as a 500 /
// 403 instead of a clean CLI-side resolution error (SILI-2165 follow-up).
type assigneeKinds struct {
	member, agent, squad bool
}

var (
	taskAssigneeKinds  = assigneeKinds{member: true, agent: true, squad: true}
	memberOrAgentKinds = assigneeKinds{member: true, agent: true}
)

var assigneeResolveRetrySleep = func(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}

func getAssigneeJSON(ctx context.Context, client *cli.APIClient, path string, out any) error {
	delays := []time.Duration{100 * time.Millisecond, 250 * time.Millisecond}
	var err error
	for attempt := 0; attempt <= len(delays); attempt++ {
		err = client.GetJSON(ctx, path, out)
		if err == nil || !isRetryableAssigneeResolveError(err) || attempt == len(delays) {
			return err
		}
		if assigneeResolveRetrySleep(ctx, delays[attempt]) {
			return ctx.Err()
		}
	}
	return err
}

func isRetryableAssigneeResolveError(err error) bool {
	var netErr *cli.NetworkError
	return errors.As(err, &netErr)
}

func (k assigneeKinds) describe() string {
	parts := make([]string, 0, 3)
	if k.member {
		parts = append(parts, "member")
	}
	if k.agent {
		parts = append(parts, "agent")
	}
	if k.squad {
		parts = append(parts, "squad")
	}
	switch len(parts) {
	case 0:
		return "<none>"
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " or " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", or " + parts[len(parts)-1]
	}
}

func resolveAssignee(ctx context.Context, client *cli.APIClient, name string, kinds assigneeKinds) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set SILIECO_WORKSPACE_ID")
	}

	input := normalizeAssigneeLookupInput(name)
	if input == "" {
		return "", "", fmt.Errorf("no %s found matching %q", kinds.describe(), name)
	}
	inputLower := strings.ToLower(input)

	// Matches are collected into three priority buckets. Higher-priority buckets
	// short-circuit lower-priority matching so that, e.g., an exact name match
	// always wins over a substring collision with another candidate.
	//   1. idMatches        — full UUID or 8-char ShortID (as shown by `truncateID`).
	//   2. exactMatches     — case-insensitive full name equality.
	//   3. substringMatches — preserves the existing partial-name UX.
	var idMatches, exactMatches, substringMatches []assigneeMatch
	var errs []error
	var fetchAttempts int

	classify := func(entityType, id, displayName string) {
		match := assigneeMatch{Type: entityType, ID: id, Name: displayName}
		if id != "" && (strings.EqualFold(id, input) || strings.EqualFold(truncateID(id), input)) {
			idMatches = append(idMatches, match)
			return
		}
		if strings.EqualFold(displayName, input) {
			exactMatches = append(exactMatches, match)
			return
		}
		if strings.Contains(strings.ToLower(displayName), inputLower) {
			substringMatches = append(substringMatches, match)
		}
	}

	// Search members.
	if kinds.member {
		fetchAttempts++
		var members []map[string]any
		if err := getAssigneeJSON(ctx, client, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err != nil {
			errs = append(errs, fmt.Errorf("fetch members: %w", err))
		} else {
			for _, m := range members {
				classify("member", strVal(m, "user_id"), strVal(m, "name"))
			}
		}
	}

	// Search agents.
	if kinds.agent {
		fetchAttempts++
		var agents []map[string]any
		agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
		if err := getAssigneeJSON(ctx, client, agentPath, &agents); err != nil {
			errs = append(errs, fmt.Errorf("fetch agents: %w", err))
		} else {
			for _, a := range agents {
				classify("agent", strVal(a, "id"), strVal(a, "name"))
			}
		}
	}

	// Search squads. The platform allows tasks to be assigned to a squad
	// (the leader agent then coordinates delegation), so squad names must
	// resolve here too for task-assignee callers — otherwise a user saying
	// "assign to <SquadName>" silently falls through and the autopilot
	// prompt emits "Unrecognized assignee: <SquadName>" (SILI-2165). Callers
	// whose target schema is member-or-agent only (project lead, subscriber)
	// must opt out via `kinds.squad = false`.
	if kinds.squad {
		fetchAttempts++
		var squads []map[string]any
		if err := getAssigneeJSON(ctx, client, "/api/squads", &squads); err != nil {
			errs = append(errs, fmt.Errorf("fetch squads: %w", err))
		} else {
			for _, s := range squads {
				if strVal(s, "archived_at") != "" {
					continue
				}
				classify("squad", strVal(s, "id"), strVal(s, "name"))
			}
		}
	}

	// If every fetch failed, report the errors instead of a misleading "not found".
	if fetchAttempts > 0 && len(errs) == fetchAttempts {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return "", "", fmt.Errorf("failed to resolve assignee: %s", strings.Join(msgs, "; "))
	}

	for _, bucket := range [][]assigneeMatch{idMatches, exactMatches, substringMatches} {
		switch len(bucket) {
		case 0:
			continue
		case 1:
			return bucket[0].Type, bucket[0].ID, nil
		default:
			return "", "", ambiguousAssigneeError(input, bucket)
		}
	}
	return "", "", fmt.Errorf("no %s found matching %q", kinds.describe(), input)
}

func normalizeAssigneeLookupInput(raw string) string {
	input := strings.TrimSpace(raw)
	if m := util.MentionRe.FindStringSubmatch(input); len(m) == 4 && m[0] == input {
		switch m[2] {
		case "member", "agent", "squad":
			return m[3]
		}
	}
	input = strings.TrimLeftFunc(input, func(r rune) bool {
		return r == '@' || r == '＠'
	})
	return strings.TrimSpace(input)
}

func ambiguousAssigneeError(input string, matches []assigneeMatch) error {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		parts = append(parts, fmt.Sprintf("  %s %q (%s)", m.Type, m.Name, truncateID(m.ID)))
	}
	return fmt.Errorf("ambiguous assignee %q; matches:\n%s", input, strings.Join(parts, "\n"))
}

// resolveAssigneeByID strictly resolves a canonical UUID to (assignee_type,
// assignee_id) by looking it up against the workspace's members, agents, and
// (when allowed) squads. It is the deterministic counterpart to
// resolveAssignee: callers that already hold a UUID (e.g. agents reading IDs
// from `silieco workspace member list --output json`) should use this instead of
// round-tripping through name matching, which can be ambiguous in workspaces
// with overlapping names.
func resolveAssigneeByID(ctx context.Context, client *cli.APIClient, id string, kinds assigneeKinds) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set SILIECO_WORKSPACE_ID")
	}
	input := strings.TrimSpace(id)
	if !uuidRegexp.MatchString(input) {
		return "", "", fmt.Errorf("expected a canonical UUID, got %q", id)
	}

	var members []map[string]any
	var memberErr error
	if kinds.member {
		memberErr = getAssigneeJSON(ctx, client, "/api/workspaces/"+client.WorkspaceID+"/members", &members)
	}

	var agents []map[string]any
	var agentErr error
	if kinds.agent {
		agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
		agentErr = getAssigneeJSON(ctx, client, agentPath, &agents)
	}

	var squads []map[string]any
	var squadErr error
	if kinds.squad {
		squadErr = getAssigneeJSON(ctx, client, "/api/squads", &squads)
	}

	allFailed := true
	hasFetch := false
	for _, pair := range []struct {
		enabled bool
		err     error
	}{{kinds.member, memberErr}, {kinds.agent, agentErr}, {kinds.squad, squadErr}} {
		if !pair.enabled {
			continue
		}
		hasFetch = true
		if pair.err == nil {
			allFailed = false
		}
	}
	if hasFetch && allFailed {
		return "", "", fmt.Errorf("failed to resolve assignee: %v; %v; %v", memberErr, agentErr, squadErr)
	}

	for _, m := range members {
		if strings.EqualFold(strVal(m, "user_id"), input) {
			return "member", strVal(m, "user_id"), nil
		}
	}
	for _, a := range agents {
		if strings.EqualFold(strVal(a, "id"), input) {
			return "agent", strVal(a, "id"), nil
		}
	}
	for _, s := range squads {
		if strings.EqualFold(strVal(s, "id"), input) {
			return "squad", strVal(s, "id"), nil
		}
	}

	return "", "", fmt.Errorf("no %s found with ID %q", kinds.describe(), input)
}

// pickAssigneeFromFlags reads a (name-flag, id-flag) pair off cmd and resolves
// it to (assignee_type, assignee_id), restricted to the entity types in
// kinds. The third return reports whether either flag was *explicitly set*;
// callers use it to decide whether to write `assignee_*` into the request
// body. The two flags are mutually exclusive — passing both is rejected
// up-front so a script that accidentally sets both never silently applies one
// over the other.
//
// Presence is detected via Flags().Changed (not value-emptiness): a script
// that interpolates an empty env var (`--assignee-id "$MAYBE_UUID"`) must
// fail loudly through resolveAssignee/resolveAssigneeByID rather than silently
// degrade to "no filter / unassigned / subscribe caller", which would defeat
// the strict-UUID guarantee the new flags exist for.
func pickAssigneeFromFlags(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, nameFlag, idFlag string, kinds assigneeKinds) (string, string, bool, error) {
	nameSet := cmd.Flags().Changed(nameFlag)
	idSet := cmd.Flags().Changed(idFlag)
	if nameSet && idSet {
		return "", "", false, fmt.Errorf("--%s and --%s are mutually exclusive", nameFlag, idFlag)
	}
	if idSet {
		idVal, _ := cmd.Flags().GetString(idFlag)
		t, i, err := resolveAssigneeByID(ctx, client, idVal, kinds)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	if nameSet {
		name, _ := cmd.Flags().GetString(nameFlag)
		t, i, err := resolveAssignee(ctx, client, name, kinds)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	return "", "", false, nil
}

func formatAssignee(task map[string]any, actors actorDisplayLookup) string {
	aType := strVal(task, "assignee_type")
	aID := strVal(task, "assignee_id")
	if aType == "" || aID == "" {
		return ""
	}
	return actors.actor(aType, aID)
}

func truncateID(id string) string {
	if utf8.RuneCountInString(id) > 8 {
		runes := []rune(id)
		return string(runes[:8])
	}
	return id
}
