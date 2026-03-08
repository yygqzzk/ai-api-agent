package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wanzhi/internal/agent"
	"wanzhi/internal/config"
	"wanzhi/internal/tools"
)

type fakeStreamRunner struct{}

func (f *fakeStreamRunner) RunStream(ctx context.Context, query string) <-chan agent.AgentEvent {
	ch := make(chan agent.AgentEvent, 4)
	go func() {
		defer close(ch)
		ch <- agent.AgentEvent{Kind: agent.EventStepStart, Step: 1}
		ch <- agent.AgentEvent{Kind: agent.EventLLMEnd, Step: 1}
		ch <- agent.AgentEvent{Kind: agent.EventComplete, Content: "test summary for: " + query}
	}()
	return ch
}

func TestSSEStreamingEndToEnd(t *testing.T) {
	cfg, _ := config.LoadFromEnv()
	registry := tools.NewRegistry()

	srv := NewServer(cfg, registry, Hooks{}, ServerOptions{})
	srv.SetStreamRunner(&fakeStreamRunner{})

	body := `{"jsonrpc":"2.0","id":1,"method":"query_api","params":{"query":"查宠物接口"}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	w := httptest.NewRecorder()
	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}

	scanner := bufio.NewScanner(w.Body)
	var events []agent.AgentEvent
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var ev agent.AgentEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				t.Fatalf("parse SSE data: %v", err)
			}
			events = append(events, ev)
		}
	}

	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}
	if events[0].Kind != agent.EventStepStart {
		t.Errorf("first event should be step.start, got %s", events[0].Kind)
	}
	last := events[len(events)-1]
	if last.Kind != agent.EventComplete {
		t.Errorf("last event should be complete, got %s", last.Kind)
	}
	if !strings.Contains(last.Content, "查宠物接口") {
		t.Errorf("complete event should contain query, got %q", last.Content)
	}
}

func TestNonSSERequestStillWorksAsJSON(t *testing.T) {
	cfg, _ := config.LoadFromEnv()
	registry := tools.NewRegistry()

	srv := NewServer(cfg, registry, Hooks{}, ServerOptions{})
	srv.SetStreamRunner(&fakeStreamRunner{})

	body := `{"jsonrpc":"2.0","id":1,"method":"unknown_tool","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %s", ct)
	}
}
