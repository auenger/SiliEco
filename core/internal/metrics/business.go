package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/silieco-ai/silieco/core/pkg/runfailure"
)

var runDurationBuckets = []float64{1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200, 3600, 7200}

type activeRunLabels struct {
	source      string
	runtimeMode string
}

type BusinessMetrics struct {
	runEnqueued     *prometheus.CounterVec
	runDispatched   *prometheus.CounterVec
	runStarted      *prometheus.CounterVec
	runTerminal     *prometheus.CounterVec
	runFailed       *prometheus.CounterVec
	runQueueWait    *prometheus.HistogramVec
	runRunSeconds   *prometheus.HistogramVec
	runTotalSeconds *prometheus.HistogramVec
	runInProgress   *prometheus.GaugeVec
	runIterations   *prometheus.HistogramVec

	llmTokens         *prometheus.CounterVec
	llmCostUSD        *prometheus.CounterVec
	llmUnpricedTokens *prometheus.CounterVec
	llmRequests       *prometheus.CounterVec

	runQueuedExpired *prometheus.CounterVec
	runLeaseExpired  *prometheus.CounterVec

	activeMu   sync.Mutex
	activeRuns map[string]activeRunLabels

	// PR3 funnel / community / commercial counters. See business_events.go
	// for the field-level docs and labels.
	events *businessEventMetrics
}

func NewBusinessMetrics() *BusinessMetrics {
	validateBusinessMetricLabels()
	m := &BusinessMetrics{
		runEnqueued: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "enqueued_total",
			Help:      "Total agent runs enqueued.",
		}, metricLabels("silieco_agent_run_enqueued_total")),
		runDispatched: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "dispatched_total",
			Help:      "Total agent runs dispatched to a runtime.",
		}, metricLabels("silieco_agent_run_dispatched_total")),
		runStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "started_total",
			Help:      "Total agent runs that reached running state.",
		}, metricLabels("silieco_agent_run_started_total")),
		runTerminal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "terminal_total",
			Help:      "Total agent runs that reached a terminal state.",
		}, metricLabels("silieco_agent_run_terminal_total")),
		runFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "failed_total",
			Help:      "Total failed agent runs by canonical failure reason.",
		}, metricLabels("silieco_agent_run_failed_total")),
		runQueueWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "queue_wait_seconds",
			Help:      "Time agent runs spent queued before dispatch.",
			Buckets:   runDurationBuckets,
		}, metricLabels("silieco_agent_run_queue_wait_seconds")),
		runRunSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "run_seconds",
			Help:      "Time agent runs spent running before a terminal state.",
			Buckets:   runDurationBuckets,
		}, metricLabels("silieco_agent_run_run_seconds")),
		runTotalSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "total_seconds",
			Help:      "Total time from agent run creation to terminal state.",
			Buckets:   runDurationBuckets,
		}, metricLabels("silieco_agent_run_total_seconds")),
		runInProgress: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "in_progress",
			Help:      "Current agent runs dispatched by this process and not yet terminal.",
		}, metricLabels("silieco_agent_run_in_progress")),
		runIterations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "silieco",
			Subsystem: "agent_run",
			Name:      "iteration_count",
			Help:      "Retry attempt count observed when an agent run reaches a terminal state.",
			Buckets:   []float64{1, 2, 3, 4, 5, 10},
		}, metricLabels("silieco_agent_run_iteration_count")),
		llmTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "llm",
			Name:      "tokens_total",
			Help:      "Total priced LLM tokens by provider, model, token type, runtime mode, and task source.",
		}, metricLabels("silieco_llm_tokens_total")),
		llmCostUSD: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "llm",
			Name:      "cost_usd_total",
			Help:      "Total estimated priced LLM token cost in USD.",
		}, metricLabels("silieco_llm_cost_usd_total")),
		llmUnpricedTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "llm",
			Name:      "unpriced_tokens_total",
			Help:      "Total LLM tokens for model aliases without a fixed TSR price.",
		}, metricLabels("silieco_llm_unpriced_tokens_total")),
		llmRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "llm",
			Name:      "request_total",
			Help:      "Total run usage reports by normalized LLM provider and model.",
		}, metricLabels("silieco_llm_request_total")),
		runQueuedExpired: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "run",
			Name:      "queued_expired_total",
			Help:      "Total queued runs expired by the scheduler.",
		}, metricLabels("silieco_run_queued_expired_total")),
		runLeaseExpired: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "silieco",
			Subsystem: "run",
			Name:      "lease_expired_total",
			Help:      "Total dispatched or running run leases expired by the scheduler.",
		}, metricLabels("silieco_run_lease_expired_total")),
		activeRuns: map[string]activeRunLabels{},
		events:     newBusinessEventMetrics(),
	}
	m.prewarmFailureReasons()
	return m
}

func (m *BusinessMetrics) Collectors() []prometheus.Collector {
	return append([]prometheus.Collector{
		m.runEnqueued,
		m.runDispatched,
		m.runStarted,
		m.runTerminal,
		m.runFailed,
		m.runQueueWait,
		m.runRunSeconds,
		m.runTotalSeconds,
		m.runInProgress,
		m.runIterations,
		m.llmTokens,
		m.llmCostUSD,
		m.llmUnpricedTokens,
		m.llmRequests,
		m.runQueuedExpired,
		m.runLeaseExpired,
	}, m.events.collectors()...)
}

func (m *BusinessMetrics) RecordRunEnqueued(source, runtimeMode string) {
	if m == nil {
		return
	}
	m.runEnqueued.WithLabelValues(NormalizeRunSource(source), NormalizeRuntimeMode(runtimeMode)).Inc()
}

func (m *BusinessMetrics) RecordRunDispatched(runID, source, runtimeMode string, queueWaitSeconds float64) {
	if m == nil {
		return
	}
	source = NormalizeRunSource(source)
	runtimeMode = NormalizeRuntimeMode(runtimeMode)
	m.runDispatched.WithLabelValues(source, runtimeMode).Inc()
	if queueWaitSeconds >= 0 {
		m.runQueueWait.WithLabelValues(source, runtimeMode).Observe(queueWaitSeconds)
	}
	m.markRunInProgress(runID, source, runtimeMode)
}

func (m *BusinessMetrics) RecordRunStarted(source, runtimeMode, provider string) {
	if m == nil {
		return
	}
	m.runStarted.WithLabelValues(
		NormalizeRunSource(source),
		NormalizeRuntimeMode(runtimeMode),
		NormalizeRuntimeProvider(provider),
	).Inc()
}

func (m *BusinessMetrics) RecordRunTerminal(runID, source, runtimeMode, terminalStatus string, runSeconds, totalSeconds float64, attempt int32) {
	if m == nil {
		return
	}
	source = NormalizeRunSource(source)
	runtimeMode = NormalizeRuntimeMode(runtimeMode)
	terminalStatus = NormalizeTerminalStatus(terminalStatus)
	m.runTerminal.WithLabelValues(source, runtimeMode, terminalStatus).Inc()
	if runSeconds >= 0 {
		m.runRunSeconds.WithLabelValues(source, runtimeMode, terminalStatus).Observe(runSeconds)
	}
	if totalSeconds >= 0 {
		m.runTotalSeconds.WithLabelValues(source, runtimeMode, terminalStatus).Observe(totalSeconds)
	}
	if attempt < 1 {
		attempt = 1
	}
	m.runIterations.WithLabelValues(source, terminalStatus).Observe(float64(attempt))
	m.clearRunInProgress(runID)
}

func (m *BusinessMetrics) RecordRunFailed(source, runtimeMode, failureReason string) {
	if m == nil {
		return
	}
	m.runFailed.WithLabelValues(
		NormalizeRunSource(source),
		NormalizeRuntimeMode(runtimeMode),
		NormalizeFailureReason(failureReason),
	).Inc()
}

func (m *BusinessMetrics) RecordRunQueuedExpired(source, runtimeMode string) {
	if m == nil {
		return
	}
	m.runQueuedExpired.WithLabelValues(NormalizeRunSource(source), NormalizeRuntimeMode(runtimeMode)).Inc()
}

func (m *BusinessMetrics) RecordRunLeaseExpired(source string) {
	if m == nil {
		return
	}
	m.runLeaseExpired.WithLabelValues(NormalizeRunSource(source)).Inc()
}

// costUSDTicks is the provider's own price for this usage in 1e-10 USD, or 0
// when it reported none. When present it wins over the rate table: the table
// cannot express request-level rules such as xAI's 2x surcharge above a 200K
// prompt, so for those providers the local estimate is structurally low.
func (m *BusinessMetrics) RecordLLMUsage(source, runtimeMode, rawProvider, modelAlias string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, costUSDTicks int64) {
	if m == nil {
		return
	}
	source = NormalizeRunSource(source)
	runtimeMode = NormalizeRuntimeMode(runtimeMode)
	price, priced := PriceForModelAlias(modelAlias)
	if !priced {
		provider := NormalizeRuntimeProvider(rawProvider)
		alias := NormalizeModelAlias(modelAlias)
		m.recordUnpricedTokens(provider, alias, "input", inputTokens)
		m.recordUnpricedTokens(provider, alias, "output", outputTokens)
		m.recordUnpricedTokens(provider, alias, "cache_read", cacheReadTokens)
		m.recordUnpricedTokens(provider, alias, "cache_write", cacheWriteTokens)
		// Having no rate row does not mean having no cost: the provider may
		// have priced the turn itself (`grok-composer-*` is in the Grok Build
		// catalog but absent from xAI's price sheet). Dropping the charge here
		// would under-report real spend purely for lack of a rate we no longer
		// need. Without rates there is nothing to split the total by, so it
		// lands whole in the `input` bucket — the same fallback
		// distributeAuthoritativeCost uses when it has no shape to scale.
		if costUSDTicks > 0 {
			m.llmCostUSD.
				WithLabelValues(provider, alias, NormalizeTokenType("input"), runtimeMode, source).
				Add(float64(costUSDTicks) / CostUSDTicksPerUSD)
		}
		m.llmRequests.WithLabelValues(provider, "unknown", runtimeMode).Inc()
		return
	}

	costs := [4]float64{
		tokenCostUSD(inputTokens, price.InputPerM),
		tokenCostUSD(outputTokens, price.OutputPerM),
		tokenCostUSD(cacheReadTokens, price.CacheReadPerM),
		tokenCostUSD(cacheWriteTokens, price.CacheWritePerM),
	}
	if costUSDTicks > 0 {
		costs = distributeAuthoritativeCost(float64(costUSDTicks)/CostUSDTicksPerUSD, costs)
	}

	m.recordPricedTokens(price.Provider, price.Model, "input", runtimeMode, source, inputTokens, costs[0])
	m.recordPricedTokens(price.Provider, price.Model, "output", runtimeMode, source, outputTokens, costs[1])
	m.recordPricedTokens(price.Provider, price.Model, "cache_read", runtimeMode, source, cacheReadTokens, costs[2])
	m.recordPricedTokens(price.Provider, price.Model, "cache_write", runtimeMode, source, cacheWriteTokens, costs[3])
	m.llmRequests.WithLabelValues(price.Provider, price.Model, runtimeMode).Inc()
}

// distributeAuthoritativeCost rescales the per-token-type estimates so they
// sum to the provider's actual charge. `llm_cost_usd` is broken down by
// token_type and the provider reports one number for the whole turn, so the
// split has to come from somewhere; the rate table's own proportions are the
// best available guess and keep the total exact. Only the total is
// authoritative — the per-type split remains an estimate, which is why this
// scales rather than inventing a new label value.
//
// A zero estimate (unknown rates, or a turn recorded with no tokens) has no
// proportions to scale, so the charge lands on `input` to avoid dropping real
// spend from the total.
func distributeAuthoritativeCost(actual float64, estimated [4]float64) [4]float64 {
	total := estimated[0] + estimated[1] + estimated[2] + estimated[3]
	if total <= 0 {
		return [4]float64{actual, 0, 0, 0}
	}
	scale := actual / total
	for i := range estimated {
		estimated[i] *= scale
	}
	return estimated
}

func (m *BusinessMetrics) recordPricedTokens(provider, model, tokenType, runtimeMode, source string, tokens int64, cost float64) {
	if tokens <= 0 {
		return
	}
	tokenType = NormalizeTokenType(tokenType)
	m.llmTokens.WithLabelValues(provider, model, tokenType, runtimeMode, source).Add(float64(tokens))
	if cost > 0 {
		m.llmCostUSD.WithLabelValues(provider, model, tokenType, runtimeMode, source).Add(cost)
	}
}

func (m *BusinessMetrics) recordUnpricedTokens(provider, modelAlias, tokenType string, tokens int64) {
	if tokens <= 0 {
		return
	}
	m.llmUnpricedTokens.WithLabelValues(provider, modelAlias, NormalizeTokenType(tokenType)).Add(float64(tokens))
}

func (m *BusinessMetrics) markRunInProgress(runID, source, runtimeMode string) {
	if runID == "" {
		m.runInProgress.WithLabelValues(source, runtimeMode).Inc()
		return
	}
	m.activeMu.Lock()
	defer m.activeMu.Unlock()
	if _, ok := m.activeRuns[runID]; ok {
		return
	}
	m.activeRuns[runID] = activeRunLabels{source: source, runtimeMode: runtimeMode}
	m.runInProgress.WithLabelValues(source, runtimeMode).Inc()
}

func (m *BusinessMetrics) clearRunInProgress(runID string) {
	if runID == "" {
		return
	}
	m.activeMu.Lock()
	labels, ok := m.activeRuns[runID]
	if ok {
		delete(m.activeRuns, runID)
	}
	m.activeMu.Unlock()
	if ok {
		m.runInProgress.WithLabelValues(labels.source, labels.runtimeMode).Dec()
	}
}

func (m *BusinessMetrics) prewarmFailureReasons() {
	for _, source := range []string{"task", "chat", "autopilot", "autopilot_task", "quick_create", "other"} {
		for _, runtimeMode := range []string{"local", "cloud", "unknown"} {
			for _, reason := range runfailure.AllReasons() {
				m.runFailed.WithLabelValues(source, runtimeMode, reason.String()).Add(0)
			}
		}
	}
}
