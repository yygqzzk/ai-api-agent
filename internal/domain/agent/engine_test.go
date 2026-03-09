package agent

import (
	"context"
	"encoding/json"
	"sync"
	"strings"
	"testing"
	"time"
)

func TestAgentEngineRun(t *testing.T) {
	llm := &scriptedLLMClient{
		replies: []LLMReply{
			{ToolCalls: []ToolCall{{ID: "1", Name: "search_api", Args: mustJSON(t, map[string]any{"query": "login", "top_k": 3})}}},
			{ToolCalls: []ToolCall{{ID: "2", Name: "get_api_detail", Args: mustJSON(t, map[string]any{"service": "petstore", "endpoint": "GET /user/login"})}}},
			{Content: "已完成汇总"},
		},
	}

	registry := &mockToolDispatcher{results: map[string]any{
		"search_api":     map[string]any{"items": []string{"GET /user/login"}},
		"get_api_detail": map[string]any{"endpoint": "GET /user/login"},
	}}

	engine := NewAgentEngine(llm, registry, WithMaxSteps(5))
	out, err := engine.Run(context.Background(), "查询登录接口")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out, "已完成汇总") {
		t.Fatalf("unexpected engine output: %s", out)
	}
	if !strings.Contains(out, "工具调用轨迹") {
		t.Fatalf("expected tool trace summary in output, got: %s", out)
	}
	if !strings.Contains(out, "search_api") {
		t.Fatalf("expected trace includes search_api, got: %s", out)
	}
	if len(registry.calls) != 2 {
		t.Fatalf("expected 2 tool dispatch calls, got %d", len(registry.calls))
	}
}

func TestAgentEngineMaxSteps(t *testing.T) {
	llm := &scriptedLLMClient{
		replies: []LLMReply{
			{ToolCalls: []ToolCall{{ID: "1", Name: "search_api", Args: mustJSON(t, map[string]any{"query": "login"})}}},
			{ToolCalls: []ToolCall{{ID: "2", Name: "search_api", Args: mustJSON(t, map[string]any{"query": "login"})}}},
			{ToolCalls: []ToolCall{{ID: "3", Name: "search_api", Args: mustJSON(t, map[string]any{"query": "login"})}}},
		},
	}

	registry := &mockToolDispatcher{results: map[string]any{
		"search_api": map[string]any{"items": []string{"GET /user/login"}},
	}}

	engine := NewAgentEngine(llm, registry, WithMaxSteps(2))
	out, err := engine.Run(context.Background(), "查询登录接口")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out, "max steps") {
		t.Fatalf("expected max steps hint, got: %s", out)
	}
}

func TestAgentEnginePassesToolCatalogToLLM(t *testing.T) {
	llm := &capturingLLMClient{}
	registry := &mockToolDispatcher{results: map[string]any{}}
	engine := NewAgentEngine(llm, registry, WithMaxSteps(1))
	engine.SetToolCatalog([]ToolDefinition{{
		Name:        "search_api",
		Description: "search",
		Schema:      json.RawMessage(`{"type":"object"}`),
	}})

	_, _ = engine.Run(context.Background(), "查询接口")
	if len(llm.lastTools) != 1 {
		t.Fatalf("expected llm receive 1 tool definition, got %d", len(llm.lastTools))
	}
	if llm.lastTools[0].Name != "search_api" {
		t.Fatalf("unexpected tool passed to llm: %+v", llm.lastTools[0])
	}
}

func TestAgentEngineParallelToolCalls(t *testing.T) {
	llm := &scriptedLLMClient{
		replies: []LLMReply{
			{ToolCalls: []ToolCall{
				{ID: "1", Name: "search_api", Args: mustJSON(t, map[string]any{"query": "a"})},
				{ID: "2", Name: "get_api_detail", Args: mustJSON(t, map[string]any{"endpoint": "GET /pets"})},
			}},
			{Content: "done"},
		},
	}

	dispatcher := &mockToolDispatcher{results: map[string]any{
		"search_api":     map[string]any{"items": []string{"GET /pets"}},
		"get_api_detail": map[string]any{"endpoint": "GET /pets", "method": "GET"},
	}}

	slowDispatcher := &concurrencyTrackingDispatcher{
		inner: dispatcher,
	}

	engine := NewAgentEngine(llm, slowDispatcher, WithMaxSteps(5))
	out, err := engine.Run(context.Background(), "test parallel")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("unexpected output: %s", out)
	}
	if slowDispatcher.maxConcurrent < 2 {
		t.Fatalf("expected parallel execution (max concurrent >= 2), got %d", slowDispatcher.maxConcurrent)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}
	return b
}

type scriptedLLMClient struct {
	replies []LLMReply
	idx     int
}

func (s *scriptedLLMClient) Next(_ context.Context, _ []Message, _ []ToolDefinition) (LLMReply, error) {
	if s.idx >= len(s.replies) {
		return LLMReply{Content: ""}, nil
	}
	out := s.replies[s.idx]
	s.idx++
	return out, nil
}

type mockToolDispatcher struct {
	mu      sync.Mutex
	calls   []string
	results map[string]any
}

func (m *mockToolDispatcher) Dispatch(_ context.Context, name string, _ json.RawMessage) (any, error) {
	m.mu.Lock()
	m.calls = append(m.calls, name)
	m.mu.Unlock()
	if out, ok := m.results[name]; ok {
		return out, nil
	}
	return map[string]any{"ok": true}, nil
}

func (m *mockToolDispatcher) Has(name string) bool {
	_, ok := m.results[name]
	return ok
}

type capturingLLMClient struct {
	lastTools []ToolDefinition
}

func (c *capturingLLMClient) Next(_ context.Context, _ []Message, tools []ToolDefinition) (LLMReply, error) {
	c.lastTools = append([]ToolDefinition(nil), tools...)
	return LLMReply{Content: "ok"}, nil
}

type concurrencyTrackingDispatcher struct {
	inner         *mockToolDispatcher
	mu            sync.Mutex
	concurrent    int
	maxConcurrent int
}

func (d *concurrencyTrackingDispatcher) Has(name string) bool {
	return d.inner.Has(name)
}

func (d *concurrencyTrackingDispatcher) Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error) {
	d.mu.Lock()
	d.concurrent++
	if d.concurrent > d.maxConcurrent {
		d.maxConcurrent = d.concurrent
	}
	d.mu.Unlock()

	time.Sleep(50 * time.Millisecond)

	d.mu.Lock()
	d.concurrent--
	d.mu.Unlock()

	return d.inner.Dispatch(ctx, name, args)
}
