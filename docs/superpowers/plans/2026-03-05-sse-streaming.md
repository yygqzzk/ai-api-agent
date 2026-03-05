# SSE 流式输出 Implementation Plan

> **For Claude:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Agent Engine 添加 SSE 流式事件输出，MCP Server 支持 Streamable HTTP 协议，客户端可实时接收 agent 执行过程中的每一步事件。

**Architecture:** 在 `internal/agent/` 新增 `AgentEvent` 类型和 `RunStream` 方法，通过 Go channel 向调用者推送事件。`internal/mcp/server.go` 检测 `Accept: text/event-stream` 请求头，走 SSE 响应路径；否则保持现有 JSON-RPC 行为不变。

**Tech Stack:** Go stdlib `net/http`（SSE 写入）、Go channels（事件传递）、`encoding/json`（事件序列化）

---

## File Structure

| File | Operation | Responsibility |
|------|-----------|----------------|
| `internal/agent/event.go` | Create | AgentEvent 类型定义和 7 种事件常量 |
| `internal/agent/stream.go` | Create | RunStream 方法，channel-based 事件发射 |
| `internal/agent/stream_test.go` | Create | RunStream 单元测试 |
| `internal/mcp/sse.go` | Create | SSE 响应写入器和流式 handler |
| `internal/mcp/sse_test.go` | Create | SSE handler 测试 |
| `internal/mcp/server.go` | Modify | Handler() 添加 SSE 路由分支 |
| `internal/tools/query_api.go` | Modify | 新增 StreamRunner 接口支持 |

---

## Task 1: AgentEvent 类型定义

**Files:**
- Create: `internal/agent/event.go`

- [ ] **Step 1: Create AgentEvent type and event kind constants**

```go
// internal/agent/event.go
package agent

import "encoding/json"

type EventKind string

const (
	EventStepStart EventKind = "agent.step.start"
	EventToolEnd   EventKind = "agent.tool.end"
	EventLLMStart  EventKind = "agent.llm.start"
	EventLLMEnd    EventKind = "agent.llm.end"
	EventComplete  EventKind = "agent.complete"
	EventError     EventKind = "agent.error"
)

type AgentEvent struct {
	Kind    EventKind       `json:"kind"`
	Step    int             `json:"step,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Content string          `json:"content,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go build ./internal/agent/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/agent/event.go
git commit -m "feat(agent): add AgentEvent type for SSE streaming"
```

---

## Task 2: RunStream 方法实现

**Files:**
- Create: `internal/agent/stream.go`
- Create: `internal/agent/stream_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/stream_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"
)

type collectingDispatcher struct {
	results map[string]any
}

func (d *collectingDispatcher) Dispatch(_ context.Context, name string, _ json.RawMessage) (any, error) {
	if v, ok := d.results[name]; ok {
		return v, nil
	}
	return map[string]string{"result": "ok"}, nil
}

func (d *collectingDispatcher) Has(name string) bool {
	_, ok := d.results[name]
	return ok
}

func TestRunStreamEmitsEvents(t *testing.T) {
	dispatcher := &collectingDispatcher{
		results: map[string]any{
			"search_api": map[string]string{"endpoint": "GET /pets"},
		},
	}
	// RuleBasedLLMClient will call search_api then summarize
	engine := NewAgentEngine(NewRuleBasedLLMClient(), dispatcher, 5)
	engine.SetToolCatalog(nil)
	engine.SetMetrics(nil)

	ctx := context.Background()
	ch := engine.RunStream(ctx, "查询宠物接口")

	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	// Must have at least: step.start, llm.end, tool.end, complete
	kinds := make(map[EventKind]int)
	for _, ev := range events {
		kinds[ev.Kind]++
	}
	if kinds[EventStepStart] == 0 {
		t.Error("missing agent.step.start events")
	}
	if kinds[EventComplete] != 1 {
		t.Errorf("expected exactly one agent.complete event, got %d", kinds[EventComplete])
	}
}

func TestRunStreamErrorEvent(t *testing.T) {
	// Empty dispatcher - unknown tool will cause error
	dispatcher := &collectingDispatcher{results: map[string]any{}}
	engine := NewAgentEngine(NewRuleBasedLLMClient(), dispatcher, 5)
	engine.SetMetrics(nil)

	ch := engine.RunStream(context.Background(), "查询接口")

	var lastEvent AgentEvent
	for ev := range ch {
		lastEvent = ev
	}

	if lastEvent.Kind != EventError {
		t.Errorf("expected last event to be error, got %s", lastEvent.Kind)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./internal/agent/ -v -run TestRunStream`
Expected: FAIL — `engine.RunStream` undefined

- [ ] **Step 3: Implement RunStream**

```go
// internal/agent/stream.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (e *AgentEngine) RunStream(ctx context.Context, userQuery string) <-chan AgentEvent {
	ch := make(chan AgentEvent, 32)
	go func() {
		defer close(ch)
		e.runStreamInternal(ctx, userQuery, ch)
	}()
	return ch
}

func (e *AgentEngine) runStreamInternal(ctx context.Context, userQuery string, ch chan<- AgentEvent) {
	cm := NewContextManager(64)
	cm.Append(Message{
		Role:    "system",
		Content: "你是企业 API 助手，只能输出结构化汇总，不泄露原始内部数据。",
	})
	cm.Append(Message{
		Role:    "user",
		Content: userQuery,
	})

	emit := func(ev AgentEvent) {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}

	for step := 0; step < e.maxSteps; step++ {
		emit(AgentEvent{Kind: EventStepStart, Step: step + 1})

		emit(AgentEvent{Kind: EventLLMStart, Step: step + 1})
		llmStart := time.Now()
		reply, err := e.llmClient.Next(ctx, cm.Messages(), e.toolCatalog)
		llmDuration := time.Since(llmStart)

		llmStatus := "ok"
		if err != nil {
			llmStatus = "error"
		}
		e.metrics.RecordLLMRequest(e.modelName(), llmStatus, llmDuration.Seconds())
		if err == nil {
			e.metrics.RecordLLMTokens(e.modelName(), reply.PromptTokens, reply.CompletionTokens)
		}

		if err != nil {
			emit(AgentEvent{Kind: EventError, Step: step + 1, Content: fmt.Sprintf("llm next failed: %v", err)})
			return
		}

		emit(AgentEvent{Kind: EventLLMEnd, Step: step + 1})

		if len(reply.ToolCalls) == 0 {
			content := reply.Content
			if content == "" {
				content = "结构化汇总结果为空。"
			}
			emit(AgentEvent{Kind: EventComplete, Content: content})
			return
		}

		cm.Append(Message{
			Role:      "assistant",
			ToolCalls: reply.ToolCalls,
		})

		for _, tc := range reply.ToolCalls {
			if !e.dispatcher.Has(tc.Name) {
				emit(AgentEvent{Kind: EventError, Step: step + 1, Content: fmt.Sprintf("unknown tool: %s", tc.Name)})
				return
			}

			start := time.Now()
			out, execErr := e.dispatcher.Dispatch(ctx, tc.Name, tc.Args)
			duration := time.Since(start)
			toolStatus := "ok"
			if execErr != nil {
				toolStatus = "error"
			}
			e.metrics.RecordToolCall(tc.Name, toolStatus, duration.Seconds())

			content := encodeToolResult(out, execErr)
			cm.Append(Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    content,
			})

			toolData, _ := json.Marshal(map[string]any{
				"tool":       tc.Name,
				"success":    execErr == nil,
				"duration_ms": duration.Milliseconds(),
				"preview":    shortPreview(content, 200),
			})
			emit(AgentEvent{Kind: EventToolEnd, Step: step + 1, Tool: tc.Name, Data: toolData})
		}
	}

	emit(AgentEvent{Kind: EventComplete, Content: fmt.Sprintf("agent stopped: reached max steps (%d)", e.maxSteps)})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./internal/agent/ -v -run TestRunStream`
Expected: PASS

- [ ] **Step 5: Run all agent tests to ensure no regression**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./internal/agent/ -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/stream.go internal/agent/stream_test.go
git commit -m "feat(agent): implement RunStream with channel-based event emission"
```

---

## Task 3: SSE 响应写入器

**Files:**
- Create: `internal/mcp/sse.go`
- Create: `internal/mcp/sse_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/mcp/sse_test.go
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

	// Parse data line
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
		r, _ := http.NewRequest("POST", "/mcp", nil)
		if tt.accept != "" {
			r.Header.Set("Accept", tt.accept)
		}
		got := isSSERequest(r)
		if got != tt.want {
			t.Errorf("Accept=%q: got %v, want %v", tt.accept, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./internal/mcp/ -v -run TestWriteSSE`
Expected: FAIL — `writeSSEEvent` undefined

- [ ] **Step 3: Implement SSE helpers**

```go
// internal/mcp/sse.go
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"ai-agent-api/internal/agent"
)

func isSSERequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeSSEEvent(w http.ResponseWriter, ev agent.AgentEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./internal/mcp/ -v -run "TestWriteSSE|TestSetSSE|TestIsSSE"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/sse.go internal/mcp/sse_test.go
git commit -m "feat(mcp): add SSE response helpers and detection"
```

---

## Task 4: MCP Server SSE 路由集成

**Files:**
- Modify: `internal/mcp/server.go` (lines 81-88 Handler(), lines 90-144 handleRPC())

- [ ] **Step 1: Add StreamRunner interface to server.go**

Add a `StreamRunner` interface and field to `Server`:

In `internal/mcp/server.go`, after the `Server` struct definition (line 30), add:

```go
type StreamRunner interface {
	RunStream(ctx context.Context, userQuery string) <-chan agent.AgentEvent
}
```

Add field to Server struct:

```go
type Server struct {
	cfg           config.Config
	registry      *tools.Registry
	hooks         Hooks
	options       ServerOptions
	limiter       *fixedWindowLimiter
	slog          *slog.Logger
	metrics       *observability.Metrics
	streamRunner  StreamRunner
}
```

Add setter method after `Shutdown`:

```go
func (s *Server) SetStreamRunner(runner StreamRunner) {
	s.streamRunner = runner
}
```

- [ ] **Step 2: Add handleSSE method**

Add to `internal/mcp/server.go` after `handleRPC`:

```go
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	if req.Method != "query_api" || s.streamRunner == nil {
		// Non-streamable methods fall back to normal RPC
		s.handleRPCWithRequest(w, r, req)
		return
	}

	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Query == "" {
		writeRPCResponse(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "query is required"},
		})
		return
	}

	setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch := s.streamRunner.RunStream(r.Context(), params.Query)
	for ev := range ch {
		writeSSEEvent(w, ev)
	}
}
```

- [ ] **Step 3: Refactor handleRPC to extract request parsing**

Rename current `handleRPC` to use a shared request parsing approach. Modify `handleRPC` to call a new internal method:

```go
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if isSSERequest(r) && s.streamRunner != nil {
		s.handleSSE(w, r)
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	s.handleRPCWithRequest(w, r, req)
}

func (s *Server) handleRPCWithRequest(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	if req.Method == "" {
		writeRPCResponse(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error:   &rpcError{Code: -32600, Message: "method is required"},
		})
		return
	}

	start := time.Now()
	if s.hooks.BeforeToolCall != nil {
		s.hooks.BeforeToolCall(r.Context(), req.Method)
	}

	result, err := s.registry.Dispatch(r.Context(), req.Method, req.Params)
	duration := time.Since(start)
	if s.hooks.AfterToolCall != nil {
		s.hooks.AfterToolCall(r.Context(), req.Method, duration, err)
	}
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.metrics.RecordRequest(req.Method, status, duration.Seconds())
	if err != nil {
		writeRPCResponse(w, http.StatusOK, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error:   &rpcError{Code: -32000, Message: err.Error()},
		})
		return
	}

	writeRPCResponse(w, http.StatusOK, rpcResponse{
		JSONRPC: req.JSONRPC,
		ID:      req.ID,
		Result:  result,
	})
}
```

- [ ] **Step 4: Run all mcp tests**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./internal/mcp/ -v`
Expected: all PASS (existing tests should not break since default path unchanged)

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go
git commit -m "feat(mcp): integrate SSE streaming into handleRPC with Accept header detection"
```

---

## Task 5: SSE 端到端集成测试

**Files:**
- Create: `internal/mcp/sse_integration_test.go`

- [ ] **Step 1: Write integration test**

```go
// internal/mcp/sse_integration_test.go
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

	"ai-agent-api/internal/agent"
	"ai-agent-api/internal/config"
	"ai-agent-api/internal/tools"
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
	cfg := config.Default()
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

	// Parse SSE events
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
	cfg := config.Default()
	registry := tools.NewRegistry()

	srv := NewServer(cfg, registry, Hooks{}, ServerOptions{})
	srv.SetStreamRunner(&fakeStreamRunner{})

	body := `{"jsonrpc":"2.0","id":1,"method":"unknown_tool","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	// No Accept: text/event-stream — should get normal JSON-RPC response

	w := httptest.NewRecorder()
	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %s", ct)
	}
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./internal/mcp/ -v -run "TestSSEStreaming|TestNonSSERequest"`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/mcp/sse_integration_test.go
git commit -m "test(mcp): add SSE streaming end-to-end integration tests"
```

---

## Task 6: main.go 接入 StreamRunner

**Files:**
- Modify: `cmd/server/main.go` (around line 117-124, after mcpServer creation)

- [ ] **Step 1: Wire StreamRunner in main.go**

After `mcpServer := mcp.NewServer(...)` and before `mcpServer.Init(ctx)`, add:

```go
mcpServer.SetStreamRunner(engine)
```

This works because `AgentEngine` already has `RunStream(ctx, query) <-chan AgentEvent`.

- [ ] **Step 2: Verify build**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go build ./cmd/server/`
Expected: no errors

- [ ] **Step 3: Run all tests**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./...`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: wire AgentEngine as StreamRunner into MCP server"
```

---

## Task 7: 全量回归测试

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go test ./... -v -count=1`
Expected: all packages PASS

- [ ] **Step 2: Verify build**

Run: `cd /Users/yygqzzk/code/ai-agent-api && go build ./...`
Expected: no errors

- [ ] **Step 3: Verify SSE curl example works (manual, optional)**

```bash
# Start server
MILVUS_MODE=memory go run cmd/server/main.go run &

# SSE request
curl -N -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"query_api","params":{"query":"查询宠物接口"}}'

# Normal JSON-RPC (should still work)
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"search_api","params":{"query":"pets","top_k":3}}'
```

---

## Summary of Changes

1. **`internal/agent/event.go`** — 7 种 AgentEvent 类型常量
2. **`internal/agent/stream.go`** — `RunStream` 方法，channel 推送事件，复用 metrics 和 encodeToolResult
3. **`internal/mcp/sse.go`** — `isSSERequest`、`setSSEHeaders`、`writeSSEEvent` 辅助函数
4. **`internal/mcp/server.go`** — `handleRPC` 检测 SSE 请求头，分流到 `handleSSE`；提取 `handleRPCWithRequest` 复用
5. **`cmd/server/main.go`** — 一行 `mcpServer.SetStreamRunner(engine)`

**向后兼容**：不带 `Accept: text/event-stream` 的请求走原有 JSON-RPC 路径，行为完全不变。
