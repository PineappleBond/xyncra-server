package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestAllMetricsRegistered verifies that all expected metric families are
// registered with the default Prometheus registry.
func TestAllMetricsRegistered(t *testing.T) {
	// Touch each Vec metric with a label so it appears in Gather output.
	// Prometheus only exposes Vec metrics after they have at least one
	// label combination.
	MessagesSent.WithLabelValues("_init").Add(0)
	ConnectionsPerUser.WithLabelValues("_init").Set(0)
	ConnectionsPerDevice.WithLabelValues("_init", "_init").Set(0)
	AgentExecutions.WithLabelValues("_init", "_init").Add(0)
	AgentExecutionsFailed.WithLabelValues("_init", "_init").Add(0)
	AgentDuration.WithLabelValues("_init", "_init").Observe(0)
	LLMTokensInput.WithLabelValues("_init", "_init").Add(0)
	LLMTokensOutput.WithLabelValues("_init", "_init").Add(0)
	LLMCallsTotal.WithLabelValues("_init", "_init").Add(0)
	LLMCallsFailed.WithLabelValues("_init", "_init", "_init").Add(0)
	FunctionsRegistered.WithLabelValues("_init").Set(0)
	AsynqQueueSize.WithLabelValues("_init").Set(0)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Build a set of registered metric names.
	registered := make(map[string]bool)
	for _, mf := range families {
		registered[mf.GetName()] = true
	}

	// All 36 expected metric names.
	expected := []string{
		// System (7)
		"xyncra_goroutines",
		"xyncra_memory_alloc_bytes",
		"xyncra_memory_inuse_bytes",
		"xyncra_gc_duration_seconds",
		"xyncra_gc_count",
		"xyncra_cpu_usage",
		"xyncra_open_fds",
		// Connection (5)
		"xyncra_connections_active",
		"xyncra_connections_total",
		"xyncra_connections_per_user",
		"xyncra_connections_per_device",
		"xyncra_connections_duration_seconds",
		// Message (5)
		"xyncra_messages_sent_total",
		"xyncra_messages_received_total",
		"xyncra_messages_per_second",
		"xyncra_message_size_bytes",
		"xyncra_message_latency_seconds",
		// Agent (9)
		"xyncra_agent_executions_total",
		"xyncra_agent_executions_failed_total",
		"xyncra_agent_duration_seconds",
		"xyncra_agent_active",
		"xyncra_agent_queue_depth",
		"xyncra_llm_tokens_input_total",
		"xyncra_llm_tokens_output_total",
		"xyncra_llm_calls_total",
		"xyncra_llm_calls_failed_total",
		// Business (6)
		"xyncra_conversations_active",
		"xyncra_conversations_created_total",
		"xyncra_devices_connected",
		"xyncra_functions_registered",
		"xyncra_reverse_rpc_requests_total",
		"xyncra_reverse_rpc_failed_total",
		// Redis (4)
		"xyncra_redis_connected",
		"xyncra_redis_ping_duration_seconds",
		"xyncra_redis_pool_size",
		"xyncra_asynq_queue_size",
	}

	for _, name := range expected {
		if !registered[name] {
			t.Errorf("metric %q not found in gathered metrics", name)
		}
	}

	if got := len(expected); len(families) < got {
		t.Errorf("expected at least %d metric families, got %d", got, len(families))
	}
}

// TestMetricsCanBeSet verifies that key metrics can be set without panicking.
func TestMetricsCanBeSet(t *testing.T) {
	// Gauges
	Goroutines.Set(42)
	MemoryAlloc.Set(1024)
	MemoryInuse.Set(2048)
	GCCount.Set(5)
	CPUUsage.Set(0.5)
	OpenFDs.Set(100)
	ConnectionsActive.Set(10)
	MessagesPerSecond.Set(50)
	AgentActive.Set(3)
	AgentQueueDepth.Set(7)
	ConversationsActive.Set(15)
	DevicesConnected.Set(8)
	RedisConnected.Set(1)
	RedisPoolSize.Set(5)

	// Counters
	ConnectionsTotal.Inc()
	MessagesReceived.Inc()
	ConversationsCreated.Inc()
	ReverseRPCRequests.Inc()
	ReverseRPCFailed.Inc()

	// Vecs
	MessagesSent.WithLabelValues("conv-1").Inc()
	AgentExecutions.WithLabelValues("agent-1", "gpt-4").Inc()
	LLMCallsTotal.WithLabelValues("agent-1", "gpt-4").Inc()
	LLMTokensInput.WithLabelValues("agent-1", "gpt-4").Add(100)
	LLMTokensOutput.WithLabelValues("agent-1", "gpt-4").Add(50)
	ConnectionsPerUser.WithLabelValues("user-1").Set(2)
	FunctionsRegistered.WithLabelValues("dev-1").Set(5)
	AsynqQueueSize.WithLabelValues("default").Set(3)

	// Histograms
	ConnectionsDuration.Observe(60)
	MessageSizeBytes.Observe(256)
	MessageLatency.Observe(0.05)
	AgentDuration.WithLabelValues("agent-1", "gpt-4").Observe(2.5)
	RedisPingDuration.Observe(0.001)

	// Summary
	GCDuration.Observe(0.0001)
}
