package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ---------------------------------------------------------------------------
// System metrics (7)
// ---------------------------------------------------------------------------

var (
	// Goroutines is the current number of goroutines in the process.
	Goroutines = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_goroutines",
		Help: "Current goroutine count",
	})

	// MemoryAlloc is the number of bytes of allocated heap objects.
	MemoryAlloc = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_memory_alloc_bytes",
		Help: "Allocated heap memory in bytes",
	})

	// MemoryInuse is the number of bytes heap objects occupy (in use).
	MemoryInuse = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_memory_inuse_bytes",
		Help: "Heap memory in use in bytes",
	})

	// GCDuration tracks GC pause durations as a summary.
	GCDuration = promauto.NewSummary(prometheus.SummaryOpts{
		Name:       "xyncra_gc_duration_seconds",
		Help:       "GC pause duration",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})

	// GCCount is the number of completed GC cycles.
	GCCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_gc_count",
		Help: "Total GC cycles completed",
	})

	// CPUUsage is the CPU usage ratio of the process.
	CPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_cpu_usage",
		Help: "CPU usage ratio",
	})

	// OpenFDs is the number of open file descriptors.
	OpenFDs = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_open_fds",
		Help: "Open file descriptors",
	})
)

// ---------------------------------------------------------------------------
// Connection metrics (5)
// ---------------------------------------------------------------------------

var (
	// ConnectionsActive is the current number of active WebSocket connections.
	ConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_connections_active",
		Help: "Current active WebSocket connections",
	})

	// ConnectionsTotal is the total number of connections since server start.
	ConnectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xyncra_connections_total",
		Help: "Total connections since start",
	})

	// ConnectionsPerUser tracks active connections per user ID.
	ConnectionsPerUser = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xyncra_connections_per_user",
		Help: "Connections per user",
	}, []string{"user_id"})

	// ConnectionsPerDevice tracks active connections per (user, device) pair.
	ConnectionsPerDevice = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xyncra_connections_per_device",
		Help: "Connections per device",
	}, []string{"user_id", "device_id"})

	// ConnectionsDuration tracks the distribution of connection lifetimes.
	ConnectionsDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xyncra_connections_duration_seconds",
		Help:    "Connection duration distribution",
		Buckets: prometheus.ExponentialBuckets(1, 2, 15),
	})
)

// ---------------------------------------------------------------------------
// Message metrics (5)
// ---------------------------------------------------------------------------

var (
	// MessagesSent counts messages sent, labelled by conversation ID.
	MessagesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xyncra_messages_sent_total",
		Help: "Total messages sent",
	}, []string{"conversation_id"})

	// MessagesReceived counts total messages received by the server.
	MessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xyncra_messages_received_total",
		Help: "Total messages received",
	})

	// MessagesPerSecond is a gauge of the current message throughput.
	MessagesPerSecond = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_messages_per_second",
		Help: "Messages per second",
	})

	// MessageSizeBytes tracks the distribution of message sizes.
	MessageSizeBytes = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xyncra_message_size_bytes",
		Help:    "Message size distribution",
		Buckets: prometheus.ExponentialBuckets(64, 2, 12),
	})

	// MessageLatency tracks end-to-end message delivery latency.
	MessageLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xyncra_message_latency_seconds",
		Help:    "Message delivery latency",
		Buckets: prometheus.DefBuckets,
	})
)

// ---------------------------------------------------------------------------
// Agent metrics (9)
// ---------------------------------------------------------------------------

var (
	// AgentExecutions counts total agent executions by agent ID and model.
	AgentExecutions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xyncra_agent_executions_total",
		Help: "Total agent executions",
	}, []string{"agent_id", "model"})

	// AgentExecutionsFailed counts failed agent executions by agent ID and error.
	AgentExecutionsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xyncra_agent_executions_failed_total",
		Help: "Total failed agent executions",
	}, []string{"agent_id", "error"})

	// AgentDuration tracks agent execution duration distributions.
	AgentDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xyncra_agent_duration_seconds",
		Help:    "Agent execution duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"agent_id", "model"})

	// AgentActive is the number of currently running agent executions.
	AgentActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_agent_active",
		Help: "Currently active agent executions",
	})

	// AgentQueueDepth is the number of pending agent tasks in the queue.
	AgentQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_agent_queue_depth",
		Help: "Agent task queue depth",
	})

	// LLMTokensInput counts total LLM input tokens consumed.
	LLMTokensInput = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xyncra_llm_tokens_input_total",
		Help: "Total LLM input tokens",
	}, []string{"agent_id", "model"})

	// LLMTokensOutput counts total LLM output tokens produced.
	LLMTokensOutput = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xyncra_llm_tokens_output_total",
		Help: "Total LLM output tokens",
	}, []string{"agent_id", "model"})

	// LLMCallsTotal counts total LLM API calls.
	LLMCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xyncra_llm_calls_total",
		Help: "Total LLM calls",
	}, []string{"agent_id", "model"})

	// LLMCallsFailed counts failed LLM API calls.
	LLMCallsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xyncra_llm_calls_failed_total",
		Help: "Total failed LLM calls",
	}, []string{"agent_id", "model", "error"})
)

// ---------------------------------------------------------------------------
// Business metrics (6)
// ---------------------------------------------------------------------------

var (
	// ConversationsActive is the number of currently active conversations.
	ConversationsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_conversations_active",
		Help: "Active conversations",
	})

	// ConversationsCreated counts total conversations created since start.
	ConversationsCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xyncra_conversations_created_total",
		Help: "Total conversations created",
	})

	// DevicesConnected is the number of currently connected devices.
	DevicesConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_devices_connected",
		Help: "Connected devices",
	})

	// FunctionsRegistered tracks registered functions per device.
	FunctionsRegistered = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xyncra_functions_registered",
		Help: "Registered functions per device",
	}, []string{"device_id"})

	// ReverseRPCRequests counts total reverse RPC requests.
	ReverseRPCRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xyncra_reverse_rpc_requests_total",
		Help: "Total reverse RPC requests",
	})

	// ReverseRPCFailed counts total failed reverse RPC requests.
	ReverseRPCFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xyncra_reverse_rpc_failed_total",
		Help: "Total failed reverse RPC requests",
	})
)

// ---------------------------------------------------------------------------
// Redis metrics (4) — total: 36 metrics
// ---------------------------------------------------------------------------

var (
	// RedisConnected indicates whether the Redis connection is alive (1=yes, 0=no).
	RedisConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_redis_connected",
		Help: "Redis connection status (1=connected, 0=disconnected)",
	})

	// RedisPingDuration tracks Redis ping latency.
	RedisPingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xyncra_redis_ping_duration_seconds",
		Help:    "Redis ping latency",
		Buckets: prometheus.DefBuckets,
	})

	// RedisPoolSize is the current Redis connection pool size.
	RedisPoolSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xyncra_redis_pool_size",
		Help: "Redis connection pool size",
	})

	// AsynqQueueSize tracks the depth of each Asynq task queue.
	AsynqQueueSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xyncra_asynq_queue_size",
		Help: "Asynq queue depth",
	}, []string{"queue"})
)
