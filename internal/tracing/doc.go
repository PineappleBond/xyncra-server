// Package tracing provides OpenTelemetry distributed tracing for Xyncra Server.
//
// Architecture:
//
//	InitTracer(cfg) → install TracerProvider (no-op when disabled)
//	config.go       → TracingConfig from XYNCRA_TRACING_* env vars
//	middleware.go    → DebugSampler for debug user/device forced sampling
//	mq_propagation.go → W3C Trace Context inject/extract for MQ
//	attributes.go   → span name and attribute key constants
//
// Span hierarchy:
//
//	ws.connection
//	├── ws.message.receive
//	│   └── handler.invoke
//	│       ├── handler.broker.enqueue
//	│       │   └── mq.process (via MQ trace context propagation)
//	│       │       └── agent.execute
//	│       │           ├── agent.build
//	│       │           └── agent.run
//	│       │               ├── agent.llm.call
//	│       │               ├── agent.tool.call
//	│       │               └── agent.stream
//	│       └── handler.broadcast
//	└── ws.message.send
//
// No-op guarantee: when TracingConfig.Enabled is false, a no-op TracerProvider
// is installed. All tracer.Start() calls return zero-allocation noop spans.
// The TracingMiddleware is also excluded from the agent middleware chain.
//
// Silent degradation: all tracing operations must not block business paths.
// Errors are logged but never returned to callers.
package tracing
