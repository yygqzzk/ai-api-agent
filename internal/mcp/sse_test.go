package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-agent-api/internal/agent"
)

func TestWriteSSEEvent(t *testing.T) {
	w := httptest.NewRecorder()
	ev := agent.AgentEvent{Kind: agent.EventStepStart, Step: 1}
	writeSSEEvent(w, ev)

	body := w.Body.String()
	if !strings.HasPrefix(body, "event: agent.step.start\n") {
		t.Fatalf("unexpected prefix: %q", body)
	}
	if !strings.Contains(body, "data: ") {
		t.Fatal("missing data line")
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Fatal("missing trailing blank line")
	}

	dataLine := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
		}
	}
	var parsed agent.AgentEvent
	if err := json.Unmarshal([]byte(dataLine), &parsed); err != nil {
		t.Fatalf("data not valid JSON: %v", err)
	}
	if parsed.Kind != agent.EventStepStart {
		t.Fatalf("unexpected kind: %s", parsed.Kind)
	}
}

func TestSetSSEHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	setSSEHeaders(w)

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("unexpected content-type: %s", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("unexpected cache-control: %s", cc)
	}
	if conn := w.Header().Get("Connection"); conn != "keep-alive" {
		t.Fatalf("unexpected connection: %s", conn)
	}
}

func TestIsSSERequest(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"text/event-stream", true},
		{"text/event-stream, application/json", true},
		{"application/json", false},
		{"", false},
	}
	for _, tt := range tests {
		r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
		if tt.accept != "" {
			r.Header.Set("Accept", tt.accept)
		}
		got := isSSERequest(r)
		if got != tt.want {
			t.Errorf("Accept=%q: got %v, want %v", tt.accept, got, tt.want)
		}
	}
}
