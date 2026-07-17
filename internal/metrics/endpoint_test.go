package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// TestMetricsEndpoint_PrometheusFormat verifies that the Prometheus HTTP
// handler returns a response body that uses the standard Prometheus text
// exposition format. The response must contain "# HELP" and "# TYPE" lines
// and at least 10 distinct metric families.
//
// Acceptance criteria:
//   - Response body contains "# HELP" markers
//   - Response body contains "# TYPE" markers
//   - At least 10 unique metric families are present
func TestMetricsEndpoint_PrometheusFormat(t *testing.T) {
	// Touch a few Vec metrics so they appear in the Gather output.
	AgentExecutions.WithLabelValues("_endpoint_init", "_endpoint_init").Add(0)
	LLMCallsTotal.WithLabelValues("_endpoint_init", "_endpoint_init").Add(0)

	handler := promhttp.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body := rec.Body.String()

	if !strings.Contains(body, "# HELP") {
		t.Error("response body missing '# HELP' lines; not valid Prometheus text format")
	}
	if !strings.Contains(body, "# TYPE") {
		t.Error("response body missing '# TYPE' lines; not valid Prometheus text format")
	}

	// Count distinct metric families.
	count := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# TYPE") {
			count++
		}
	}
	if count < 10 {
		t.Errorf("expected at least 10 metric families, got %d", count)
	}
}

// TestMetricsEndpoint_ContentType verifies that the Prometheus HTTP handler
// sets the Content-Type header to the Prometheus text exposition format.
//
// Acceptance criteria:
//   - Content-Type starts with "text/plain"
//   - Content-Type includes version=0.0.4
func TestMetricsEndpoint_ContentType(t *testing.T) {
	handler := promhttp.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type missing 'text/plain': got %q", ct)
	}
	if !strings.Contains(ct, "version=0.0.4") {
		t.Errorf("Content-Type missing 'version=0.0.4': got %q", ct)
	}
}
