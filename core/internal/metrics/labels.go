package metrics

import (
	"regexp"
	"strings"

	"github.com/silieco-ai/silieco/core/pkg/runfailure"
)

const (
	labelSource         = "source"
	labelRuntimeMode    = "runtime_mode"
	labelProvider       = "provider"
	labelTerminalStatus = "terminal_status"
	labelFailureReason  = "failure_reason"
	labelTokenType      = "token_type"
	labelModel          = "model"
	labelModelAlias     = "model_alias"

	// PR3 labels (funnel / community / commercial).
	labelSignupSource = "signup_source"
	labelPlatform     = "platform"
	labelPath         = "path"
	labelCadence      = "cadence"
	labelTriggerKind  = "trigger_kind"
	labelReason       = "reason"
	labelRecoverable  = "recoverable"
	labelKind         = "kind"
	labelStatus       = "status"
	labelEventKind    = "event_kind"
	labelAction       = "action"
	labelResult       = "result"
	labelOp           = "op"
	labelGate         = "gate"
)

var businessMetricLabels = map[string][]string{
	"silieco_agent_run_enqueued_total":     {labelSource, labelRuntimeMode},
	"silieco_agent_run_dispatched_total":   {labelSource, labelRuntimeMode},
	"silieco_agent_run_started_total":      {labelSource, labelRuntimeMode, labelProvider},
	"silieco_agent_run_terminal_total":     {labelSource, labelRuntimeMode, labelTerminalStatus},
	"silieco_agent_run_failed_total":       {labelSource, labelRuntimeMode, labelFailureReason},
	"silieco_agent_run_queue_wait_seconds": {labelSource, labelRuntimeMode},
	"silieco_agent_run_run_seconds":        {labelSource, labelRuntimeMode, labelTerminalStatus},
	"silieco_agent_run_total_seconds":      {labelSource, labelRuntimeMode, labelTerminalStatus},
	"silieco_agent_run_in_progress":        {labelSource, labelRuntimeMode},
	"silieco_agent_run_iteration_count":    {labelSource, labelTerminalStatus},
	"silieco_llm_tokens_total":             {labelProvider, labelModel, labelTokenType, labelRuntimeMode, labelSource},
	"silieco_llm_cost_usd_total":           {labelProvider, labelModel, labelTokenType, labelRuntimeMode, labelSource},
	"silieco_llm_unpriced_tokens_total":    {labelProvider, labelModelAlias, labelTokenType},
	"silieco_llm_request_total":            {labelProvider, labelModel, labelRuntimeMode},
	"silieco_run_queued_expired_total":     {labelSource, labelRuntimeMode},
	"silieco_run_lease_expired_total":      {labelSource},

	// PR3 funnel / community / commercial.
	"silieco_signup_total":                             {labelSignupSource},
	"silieco_workspace_created_total":                  {labelSource},
	"silieco_team_invite_sent_total":                   {},
	"silieco_team_invite_accepted_total":               {},
	"silieco_onboarding_started_total":                 {labelPlatform},
	"silieco_onboarding_questionnaire_submitted_total": {},
	"silieco_onboarding_source_submitted_total":        {},
	"silieco_onboarding_completed_total":               {labelPath},
	"silieco_cloud_waitlist_joined_total":              {},
	"silieco_task_created_total":                       {labelSource, labelPlatform},
	"silieco_chat_message_sent_total":                  {labelPlatform},
	"silieco_agent_created_total":                      {labelRuntimeMode, labelSource},
	"silieco_squad_created_total":                      {},
	"silieco_autopilot_created_total":                  {labelCadence},
	"silieco_task_executed_total":                      {labelSource},
	"silieco_runtime_registered_total":                 {labelRuntimeMode, labelProvider},
	"silieco_runtime_ready_total":                      {labelRuntimeMode, labelProvider},
	"silieco_runtime_ready_seconds":                    {labelRuntimeMode, labelProvider},
	"silieco_runtime_failed_total":                     {labelRuntimeMode, labelProvider, labelFailureReason, labelRecoverable},
	"silieco_runtime_offline_total":                    {labelRuntimeMode, labelProvider},
	"silieco_daemon_ws_message_received_total":         {labelKind},
	"silieco_autopilot_run_started_total":              {labelCadence, labelTriggerKind},
	"silieco_autopilot_run_terminal_total":             {labelCadence, labelTriggerKind, labelTerminalStatus},
	"silieco_autopilot_run_skipped_total":              {labelCadence, labelReason},
	"silieco_webhook_delivery_total":                   {labelProvider, labelStatus},
	"silieco_webhook_rate_limited_total":               {labelGate},
	"silieco_github_event_received_total":              {labelEventKind, labelAction},
	"silieco_github_pr_review_total":                   {labelResult},
	"silieco_cloudruntime_request_total":               {labelOp, labelStatus},
	"silieco_cloudruntime_request_duration_seconds":    {labelOp},
	"silieco_feedback_submitted_total":                 {labelKind, labelPlatform},
	"silieco_contact_sales_submitted_total":            {labelSource},
	"silieco_chat_output_local_path_total":             {labelKind},
}

var forbiddenMetricLabels = map[string]struct{}{
	"workspace_id": {},
	"user_id":      {},
	"agent_id":     {},
	"run_id":       {},
	"task_id":      {},
	"runtime_id":   {},
	"session_id":   {},
	"ip":           {},
}

var (
	knownSources = map[string]string{
		"task":           "task",
		"chat":           "chat",
		"autopilot":      "autopilot",
		"autopilot_task": "autopilot_task",
		"quick_create":   "quick_create",
		"manual":         "manual",
		"api":            "api",
		"other":          "other",
	}
	knownRuntimeModes = map[string]string{
		"local":   "local",
		"cloud":   "cloud",
		"unknown": "unknown",
	}
	knownRuntimeProviders = map[string]string{
		"antigravity":   "antigravity",
		"claude":        "claude",
		"codebuddy":     "codebuddy",
		"codex":         "codex",
		"copilot":       "copilot",
		"cursor":        "cursor",
		"gemini":        "gemini",
		"grok":          "grok",
		"hermes":        "hermes",
		"kiro":          "kiro",
		"kimi":          "kimi",
		"silieco_agent": "silieco_agent",
		"openclaw":      "openclaw",
		"opencode":      "opencode",
		"deveco":        "deveco",
		"pi":            "pi",
		"qwen":          "qwen",
		"other":         "other",
	}
	knownTerminalStatuses = map[string]string{
		"completed": "completed",
		"failed":    "failed",
		"cancelled": "cancelled",
		"blocked":   "blocked",
		"other":     "other",
	}
	knownTokenTypes = map[string]string{
		"input":       "input",
		"output":      "output",
		"cache_read":  "cache_read",
		"cache_write": "cache_write",
	}
	knownFailureReasons = map[string]string{}
	modelAliasUnsafeRe  = regexp.MustCompile(`[^a-z0-9._:/+-]+`)
)

func init() {
	for _, reason := range runfailure.AllReasons() {
		knownFailureReasons[reason.String()] = reason.String()
	}
}

func validateBusinessMetricLabels() {
	for metric, labels := range businessMetricLabels {
		for _, label := range labels {
			if _, forbidden := forbiddenMetricLabels[label]; forbidden {
				panic("forbidden high-cardinality label " + label + " on " + metric)
			}
		}
	}
}

func metricLabels(metric string) []string {
	labels, ok := businessMetricLabels[metric]
	if !ok {
		panic("missing business metric label definition for " + metric)
	}
	return labels
}

func NormalizeRunSource(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownSources[value]; ok {
		return normalized
	}
	return "other"
}

func NormalizeRuntimeMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownRuntimeModes[value]; ok {
		return normalized
	}
	return "unknown"
}

func NormalizeRuntimeProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownRuntimeProviders[value]; ok {
		return normalized
	}
	return "other"
}

func NormalizeTerminalStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownTerminalStatuses[value]; ok {
		return normalized
	}
	return "other"
}

func NormalizeFailureReason(value string) string {
	value = strings.TrimSpace(value)
	if normalized, ok := knownFailureReasons[value]; ok {
		return normalized
	}
	return runfailure.Classify(value).String()
}

func NormalizeTokenType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownTokenTypes[value]; ok {
		return normalized
	}
	return "input"
}

func NormalizeModelAlias(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	value = modelAliasUnsafeRe.ReplaceAllString(value, "_")
	if len(value) > 128 {
		return value[:128]
	}
	return value
}
