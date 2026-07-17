package agent

import (
	"context"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/metrics"
)

// LLMMetrics records LLM call metrics.
// Implementations must be safe for concurrent use.
type LLMMetrics interface {
	Record(ctx context.Context, event LLMCallEvent)
}

// LLMCallEvent contains metrics from a single LLM call.
type LLMCallEvent struct {
	AgentID      string
	Model        string
	Duration     time.Duration
	InputTokens  int
	OutputTokens int
	Error        error
}

// LogMetrics implements LLMMetrics using the agent Logger.
type LogMetrics struct {
	logger Logger
}

// NewLogMetrics creates a LogMetrics that logs LLM call events.
// If logger is nil, a noop logger is used so Record never panics.
func NewLogMetrics(logger Logger) *LogMetrics {
	if logger == nil {
		logger = noopLogger{}
	}
	return &LogMetrics{logger: logger}
}

// Record logs an LLM call event with structured key-value pairs.
// Errors are logged at Error level; successful calls at Info level.
func (m *LogMetrics) Record(ctx context.Context, event LLMCallEvent) {
	if event.Error != nil {
		m.logger.Error("llm call failed",
			"agent_id", event.AgentID,
			"model", event.Model,
			"duration_ms", event.Duration.Milliseconds(),
			"error", event.Error,
		)
		return
	}
	m.logger.Info("llm call completed",
		"agent_id", event.AgentID,
		"model", event.Model,
		"duration_ms", event.Duration.Milliseconds(),
		"input_tokens", event.InputTokens,
		"output_tokens", event.OutputTokens,
	)
}

// PrometheusMetrics implements LLMMetrics by recording to Prometheus.
// It is safe for concurrent use.
type PrometheusMetrics struct{}

// NewPrometheusMetrics creates a PrometheusMetrics that records LLM call
// events to the Prometheus metrics defined in the metrics package.
func NewPrometheusMetrics() *PrometheusMetrics {
	return &PrometheusMetrics{}
}

// Record updates Prometheus metrics for a single LLM call event.
func (m *PrometheusMetrics) Record(ctx context.Context, event LLMCallEvent) {
	agentID := event.AgentID
	model := event.Model

	// Always increment execution and LLM call counters.
	metrics.AgentExecutions.WithLabelValues(agentID, model).Inc()
	metrics.LLMCallsTotal.WithLabelValues(agentID, model).Inc()

	if event.Error != nil {
		errMsg := event.Error.Error()
		metrics.AgentExecutionsFailed.WithLabelValues(agentID, errMsg).Inc()
		metrics.LLMCallsFailed.WithLabelValues(agentID, model, errMsg).Inc()
		return
	}

	// Record duration, tokens, and active gauge on success.
	metrics.AgentDuration.WithLabelValues(agentID, model).Observe(event.Duration.Seconds())
	metrics.LLMTokensInput.WithLabelValues(agentID, model).Add(float64(event.InputTokens))
	metrics.LLMTokensOutput.WithLabelValues(agentID, model).Add(float64(event.OutputTokens))
}

// MultiMetrics fans out Record calls to multiple LLMMetrics implementations.
// It is safe for concurrent use.
type MultiMetrics struct {
	impls []LLMMetrics
}

// NewMultiMetrics creates a MultiMetrics that delegates Record to all
// provided implementations in order.
func NewMultiMetrics(impls ...LLMMetrics) *MultiMetrics {
	return &MultiMetrics{impls: impls}
}

// Record calls Record on each underlying LLMMetrics implementation.
func (m *MultiMetrics) Record(ctx context.Context, event LLMCallEvent) {
	for _, impl := range m.impls {
		impl.Record(ctx, event)
	}
}
