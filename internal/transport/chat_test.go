package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"wanzhi/internal/agent"
)

type mockStreamRunner struct {
	events []agent.AgentEvent
}

func (m *mockStreamRunner) RunStream(ctx context.Context, query string) <-chan agent.AgentEvent {
	ch := make(chan agent.AgentEvent, len(m.events))
	for _, ev := range m.events {
		ch <- ev
	}
	close(ch)
	return ch
}

func TestChatHandler_SSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runner := &mockStreamRunner{
		events: []agent.AgentEvent{
			{Kind: agent.EventStepStart, Step: 1, Content: "searching..."},
			{Kind: agent.EventComplete, Content: "找到登录接口 POST /user/login"},
		},
	}
	handler := NewChatHandler(runner)

	r := gin.New()
	r.POST("/api/chat", handler.HandleChat)

	body := `{"message":"查询登录接口"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected SSE content type, got %s", ct)
	}
	if !strings.Contains(w.Body.String(), "event:agent.complete") {
		t.Errorf("expected SSE complete event in body, got: %s", w.Body.String())
	}
}

func TestChatHandler_EmptyMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewChatHandler(&mockStreamRunner{})

	r := gin.New()
	r.POST("/api/chat", handler.HandleChat)

	body := `{"message":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
