package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m == nil {
		t.Fatal("expected non-nil Metrics")
	}

	// Verify all collectors registered without panic
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	// At least our custom metrics should be registered
	// (they won't appear in Gather until observations are recorded)
	_ = families
}

func TestMetricsRecordRequest(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordRequest("search_api", "ok", 0.045)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "mcp_requests_total" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected mcp_requests_total metric after recording")
	}
}

func TestNilMetricsSafe(t *testing.T) {
	var m *Metrics
	// All methods must be nil-safe (no panic)
	m.RecordRequest("test", "ok", 0.1)
	m.RecordToolCall("search_api", "ok", 0.05)
	m.RecordLLMRequest("gpt-4o-mini", "ok", 0.5)
	m.RecordLLMTokens("gpt-4o-mini", 100, 50)
	m.RecordRAGSearch("memory")
}
