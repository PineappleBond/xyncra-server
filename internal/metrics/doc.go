// Package metrics provides Prometheus metrics collection for the Xyncra server.
//
// It defines 36 metrics across six categories: system runtime, WebSocket
// connections, messages, agent executions, business operations, and Redis.
// All metrics are auto-registered with the default Prometheus registry via
// promauto, so importing this package is sufficient to expose them on the
// /metrics endpoint.
//
// The package is optional: when XYNCRA_METRICS_ENABLED is false (or unset),
// the runtime collector is not started and the /metrics route is not
// registered (D-063).
package metrics
