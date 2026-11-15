package metrics

import (
	"math"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/silieco-ai/silieco/core/internal/analytics"
	"github.com/silieco-ai/silieco/core/pkg/runfailure"
)

func TestBusinessMetricsLifecycleCountersAndGauge(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordRunEnqueued("task", "local")
	for i := 0; i < 100; i++ {
		m.RecordRunDispatched("task-"+strconv.Itoa(i), "task", "local", 2.5)
	}
	m.RecordRunStarted("task", "local", "codex")
	m.RecordRunTerminal("task-0", "task", "local", "completed", 10, 20, 1)

	if got := testutil.ToFloat64(m.runEnqueued.WithLabelValues("task", "local")); got != 1 {
		t.Fatalf("enqueued counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.runDispatched.WithLabelValues("task", "local")); got != 100 {
		t.Fatalf("dispatched counter = %v, want 100", got)
	}
	if got := testutil.ToFloat64(m.runStarted.WithLabelValues("task", "local", "codex")); got != 1 {
		t.Fatalf("started counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.runTerminal.WithLabelValues("task", "local", "completed")); got != 1 {
		t.Fatalf("terminal counter = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(m.runInProgress); got != 1 {
		t.Fatalf("in_progress series count = %d, want 1 despite 100 task ids", got)
	}
	if got := testutil.ToFloat64(m.runInProgress.WithLabelValues("task", "local")); got != 99 {
		t.Fatalf("in_progress gauge = %v, want 99", got)
	}
	if got := testutil.CollectAndCount(m.runQueueWait); got != 1 {
		t.Fatalf("queue wait series count = %d, want 1", got)
	}
	if got := testutil.CollectAndCount(m.runRunSeconds); got != 1 {
		t.Fatalf("run seconds series count = %d, want 1", got)
	}
	if got := testutil.CollectAndCount(m.runTotalSeconds); got != 1 {
		t.Fatalf("total seconds series count = %d, want 1", got)
	}
}

func TestBusinessMetricsFailureReasonUsesCanonicalClassifier(t *testing.T) {
	m := NewBusinessMetrics()

	rawError := `API Error: 429 {"error":"overloaded"}`
	m.RecordRunFailed("task", "local", rawError)

	wantReason := runfailure.ReasonAgentProviderCapacityOrRateLimit.String()
	if got := testutil.ToFloat64(m.runFailed.WithLabelValues("task", "local", wantReason)); got != 1 {
		t.Fatalf("classified failure counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.runFailed.WithLabelValues("task", "local", runfailure.ReasonAgentUnknown.String())); got != 0 {
		t.Fatalf("unknown failure counter = %v, want 0", got)
	}
}

func TestBusinessMetricsLLMPricingAndUnpricedTokens(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordLLMUsage("chat", "cloud", "codex", "gpt-5.4", 1_000_000, 2_000_000, 3_000_000, 4_000_000, 0)

	if got := testutil.ToFloat64(m.llmTokens.WithLabelValues("openai", "gpt-5.4", "input", "cloud", "chat")); got != 1_000_000 {
		t.Fatalf("priced input tokens = %v, want 1000000", got)
	}
	if got := testutil.ToFloat64(m.llmTokens.WithLabelValues("openai", "gpt-5.4", "output", "cloud", "chat")); got != 2_000_000 {
		t.Fatalf("priced output tokens = %v, want 2000000", got)
	}
	if got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("openai", "gpt-5.4", "input", "cloud", "chat")); got != 2.5 {
		t.Fatalf("priced input cost = %v, want 2.5", got)
	}
	if got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("openai", "gpt-5.4", "output", "cloud", "chat")); got != 30 {
		t.Fatalf("priced output cost = %v, want 30", got)
	}
	if got := testutil.ToFloat64(m.llmRequests.WithLabelValues("openai", "gpt-5.4", "cloud")); got != 1 {
		t.Fatalf("priced request counter = %v, want 1", got)
	}

	m.RecordLLMUsage("task", "local", "custom-provider", "Free Model!!", 7, 0, 0, 0, 0)
	if got := testutil.ToFloat64(m.llmUnpricedTokens.WithLabelValues("other", "free_model_", "input")); got != 7 {
		t.Fatalf("unpriced input tokens = %v, want 7", got)
	}
	if got := testutil.ToFloat64(m.llmRequests.WithLabelValues("other", "unknown", "local")); got != 1 {
		t.Fatalf("unpriced request counter = %v, want 1", got)
	}
}

func TestBusinessMetricsRegistryExposesAllFamilies(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewBusinessMetrics()
	registry.MustRegister(m.Collectors()...)

	m.RecordRunEnqueued("task", "local")
	m.RecordRunDispatched("task-1", "task", "local", 1)
	m.RecordRunStarted("task", "local", "codex")
	m.RecordRunTerminal("task-1", "task", "local", "completed", 2, 3, 1)
	m.RecordRunFailed("task", "local", runfailure.ReasonTimeout.String())
	m.RecordRunQueuedExpired("task", "local")
	m.RecordRunLeaseExpired("task")
	m.RecordLLMUsage("task", "local", "codex", "gpt-5.4", 1, 1, 1, 1, 0)
	m.RecordLLMUsage("task", "local", "custom-provider", "custom-model", 1, 0, 0, 0, 0)

	// PR3 funnel / community / commercial events. Drive every counter
	// with one synthetic value so the gather loop below sees the family.
	exerciseEvent(m, analytics.EventSignup, map[string]any{"signup_source": "test"})
	exerciseEvent(m, analytics.EventWorkspaceCreated, map[string]any{"source": "manual"})
	exerciseEvent(m, analytics.EventTeamInviteSent, nil)
	exerciseEvent(m, analytics.EventTeamInviteAccepted, nil)
	exerciseEvent(m, analytics.EventOnboardingStarted, map[string]any{"platform": "web"})
	exerciseEvent(m, analytics.EventOnboardingQuestionnaireSubmit, nil)
	exerciseEvent(m, analytics.EventOnboardingSourceSubmit, nil)
	exerciseEvent(m, analytics.EventOnboardingCompleted, map[string]any{"completion_path": "full"})
	exerciseEvent(m, analytics.EventCloudWaitlistJoined, nil)
	exerciseEvent(m, analytics.EventTaskCreated, map[string]any{"source": "manual", "platform": "web"})
	exerciseEvent(m, analytics.EventChatMessageSent, map[string]any{"platform": "web"})
	exerciseEvent(m, analytics.EventAgentCreated, map[string]any{"runtime_mode": "local", "source": "manual"})
	exerciseEvent(m, analytics.EventSquadCreated, nil)
	exerciseEvent(m, analytics.EventAutopilotCreated, map[string]any{"cadence": "manual"})
	exerciseEvent(m, analytics.EventTaskExecuted, map[string]any{"source": "manual"})
	exerciseEvent(m, analytics.EventRuntimeRegistered, map[string]any{"runtime_mode": "local", "provider": "claude"})
	exerciseEvent(m, analytics.EventRuntimeReady, map[string]any{"runtime_mode": "local", "provider": "claude", "ready_duration_ms": int64(1000)})
	exerciseEvent(m, analytics.EventRuntimeFailed, map[string]any{"runtime_mode": "local", "provider": "claude", "failure_reason": "timeout", "recoverable": true})
	exerciseEvent(m, analytics.EventRuntimeOffline, map[string]any{"runtime_mode": "local", "provider": "claude"})
	exerciseEvent(m, analytics.EventAutopilotRunStarted, map[string]any{"cadence": "manual", "trigger_kind": "manual"})
	exerciseEvent(m, analytics.EventAutopilotRunCompleted, map[string]any{"cadence": "manual", "trigger_kind": "manual"})
	exerciseEvent(m, analytics.EventAutopilotRunFailed, map[string]any{"cadence": "manual", "trigger_kind": "manual"})
	exerciseEvent(m, analytics.EventFeedbackSubmitted, map[string]any{"kind": "general", "platform": "web"})
	exerciseEvent(m, analytics.EventContactSalesSubmitted, map[string]any{"form_source": "page"})

	// Direct Record* helpers (no PostHog event source).
	m.RecordAutopilotRunSkipped("manual", "throttled")
	m.RecordWebhookDelivery("github", "dispatched")
	m.RecordWebhookRateLimited("absolute_ip")
	m.RecordGithubEventReceived("pull_request", "opened")
	m.RecordGithubPRReview("approved")
	m.ObserveGithubPRMergeSeconds(120)
	m.RecordCloudRuntimeRequest("provision", "ok", 0.5)
	m.RecordDaemonWSMessageReceived("heartbeat")
	m.RecordChatOutputLocalPath("file_url")

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	seen := make(map[string]bool, len(families))
	for _, family := range families {
		seen[family.GetName()] = true
	}
	for metric := range businessMetricLabels {
		if !seen[metric] {
			t.Fatalf("registry did not expose metric family %s", metric)
		}
	}
}

func exerciseEvent(m *BusinessMetrics, name string, props map[string]any) {
	if props == nil {
		props = map[string]any{}
	}
	m.IncForEvent(analytics.Event{Name: name, Properties: props})
}

// TestBusinessMetricsPrefersProviderReportedCost pins the rule that a
// provider's own charge wins over the rate table. The table cannot express
// request-level pricing (xAI bills a Grok request at 2x above a 200K prompt),
// so for those turns the local estimate is structurally low and recording it
// would under-report real spend.
func TestBusinessMetricsPrefersProviderReportedCost(t *testing.T) {
	m := NewBusinessMetrics()

	// 1M input + 1M output on grok-4.5 estimates to $2 + $6 = $8 from the
	// table. The provider says the turn cost $16 — the long-context tier.
	const actualUSD = 16.0
	m.RecordLLMUsage("task", "local", "grok", "grok-4.5",
		1_000_000, 1_000_000, 0, 0, int64(actualUSD*CostUSDTicksPerUSD))

	input := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "input", "local", "task"))
	output := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "output", "local", "task"))
	if total := input + output; math.Abs(total-actualUSD) > 1e-9 {
		t.Fatalf("recorded cost = %v, want %v (the provider's own charge)", total, actualUSD)
	}
	// The per-type split is presentation only, but it must follow the table's
	// own proportions ($2 input : $6 output) so the labels stay meaningful.
	if math.Abs(input-4) > 1e-9 {
		t.Errorf("input cost = %v, want 4 (a quarter of the charge)", input)
	}
	if math.Abs(output-12) > 1e-9 {
		t.Errorf("output cost = %v, want 12 (three quarters of the charge)", output)
	}
	// Token counters are unaffected by the cost source.
	if got := testutil.ToFloat64(m.llmTokens.WithLabelValues("xai", "grok-4.5", "input", "local", "task")); got != 1_000_000 {
		t.Errorf("input tokens = %v, want 1000000", got)
	}
}

// TestBusinessMetricsFallsBackToRateTableWithoutProviderCost is the other half
// of the rule: every provider that reports no cost must keep being estimated.
func TestBusinessMetricsFallsBackToRateTableWithoutProviderCost(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordLLMUsage("task", "local", "grok", "grok-4.5", 1_000_000, 1_000_000, 0, 0, 0)

	input := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "input", "local", "task"))
	output := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "output", "local", "task"))
	if math.Abs(input-2) > 1e-9 || math.Abs(output-6) > 1e-9 {
		t.Fatalf("estimated cost = (%v, %v), want (2, 6) from the rate table", input, output)
	}
}

func TestDistributeAuthoritativeCost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		actual    float64
		estimated [4]float64
		want      [4]float64
	}{
		{
			name:      "scales the estimate to the real charge",
			actual:    16,
			estimated: [4]float64{2, 6, 0, 0},
			want:      [4]float64{4, 12, 0, 0},
		},
		{
			name:      "scales down when the estimate was too high",
			actual:    4,
			estimated: [4]float64{2, 6, 0, 0},
			want:      [4]float64{1, 3, 0, 0},
		},
		{
			// No proportions to shape it with — the charge still has to land
			// somewhere, or real spend silently vanishes from the total.
			name:      "keeps the charge when there is nothing to scale",
			actual:    7,
			estimated: [4]float64{0, 0, 0, 0},
			want:      [4]float64{7, 0, 0, 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := distributeAuthoritativeCost(tc.actual, tc.estimated)
			var sum float64
			for i := range got {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Errorf("bucket %d = %v, want %v", i, got[i], tc.want[i])
				}
				sum += got[i]
			}
			if math.Abs(sum-tc.actual) > 1e-9 {
				t.Errorf("buckets sum to %v, want the actual charge %v", sum, tc.actual)
			}
		})
	}
}

// TestBusinessMetricsRecordsProviderCostForUnpricedModel covers the gap where
// a model has no rate row but the provider priced the turn itself
// (`grok-composer-*` is in the Grok Build catalog and absent from xAI's price
// sheet). Returning early on "no rates" would drop real spend from
// llm_cost_usd for want of a rate the charge does not need.
func TestBusinessMetricsRecordsProviderCostForUnpricedModel(t *testing.T) {
	m := NewBusinessMetrics()

	const actualUSD = 1.23456789
	m.RecordLLMUsage("task", "local", "grok", "grok-composer-2.5-fast",
		500, 100, 0, 0, int64(actualUSD*CostUSDTicksPerUSD))

	// No rates means no way to split by token type, so the whole charge lands
	// in one bucket — but it must be the whole charge.
	got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues(
		"grok", "grok-composer-2.5-fast", "input", "local", "task"))
	if math.Abs(got-actualUSD) > 1e-9 {
		t.Fatalf("recorded cost = %v, want %v", got, actualUSD)
	}
	// The tokens still have no rate, so they stay in the unpriced counter —
	// "unpriced" describes the rate table, not the money.
	if got := testutil.ToFloat64(m.llmUnpricedTokens.WithLabelValues("grok", "grok-composer-2.5-fast", "input")); got != 500 {
		t.Errorf("unpriced input tokens = %v, want 500", got)
	}
}

// TestBusinessMetricsUnpricedModelWithoutCostStaysAtZero is the control: no
// rates and no provider cost must record no spend, not a fabricated one.
func TestBusinessMetricsUnpricedModelWithoutCostStaysAtZero(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordLLMUsage("task", "local", "grok", "grok-composer-2.5-fast", 500, 100, 0, 0, 0)

	if got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues(
		"grok", "grok-composer-2.5-fast", "input", "local", "task")); got != 0 {
		t.Fatalf("recorded cost = %v, want 0", got)
	}
}
