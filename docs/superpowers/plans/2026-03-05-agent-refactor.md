# Agent 模块架构重构 + 智能增强实施计划

> **For Claude:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重构 `internal/agent/` 模块，消除代码重复、引入中间件/Observer 模式、实现 token 感知内存和并行工具调用。

**Architecture:** 通过 Middleware 链解耦工具调用的横切关注点（重试、日志、metrics），通过 Handler 接口统一 RunWithTrace/RunStream 为单一 runCore 循环，通过 Memory 接口支持多种上下文管理策略，通过 Functional Options 灵活配置 AgentEngine。

**Tech Stack:** Go 1.25, `sync/errgroup`, `log/slog`, Prometheus

**Spec:** `docs/superpowers/specs/2026-03-05-agent-refactor-design.md`

---

## Chunk 1: Memory 接口化（替换 ContextManager）

### Task 1: Memory 接口 + BufferMemory

**Files:**
- Create: `internal/agent/memory.go`
- Create: `internal/agent/memory_test.go`
- Delete later: `internal/agent/context.go` (在 Task 3 完成后)

- [ ] **Step 1: 创建 memory.go 并写 BufferMemory 的失败测试**

在 `internal/agent/memory_test.go` 中：

```go
package agent

import "testing"

func TestBufferMemoryAppendAndMessages(t *testing.T) {
	m := NewBufferMemory(4)
	m.Append(Message{Role: "system", Content: "sys"})
	m.Append(Message{Role: "user", Content: "u1"})
	m.Append(Message{Role: "assistant", Content: "a1"})

	msgs := m.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected first message role=system, got %s", msgs[0].Role)
	}
}

func TestBufferMemoryTrimPreservesSystem(t *testing.T) {
	m := NewBufferMemory(3)
	m.Append(Message{Role: "system", Content: "sys"})
	m.Append(Message{Role: "user", Content: "u1"})
	m.Append(Message{Role: "assistant", Content: "a1"})
	m.Append(Message{Role: "tool", Content: "t1"})
	m.Append(Message{Role: "assistant", Content: "a2"})

	msgs := m.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected trim to 3, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected system message retained, got %s", msgs[0].Role)
	}
}

func TestBufferMemoryReset(t *testing.T) {
	m := NewBufferMemory(10)
	m.Append(Message{Role: "user", Content: "u1"})
	m.Reset()
	if len(m.Messages()) != 0 {
		t.Fatal("expected empty after reset")
	}
}

func TestBufferMemoryMessagesCopiesSlice(t *testing.T) {
	m := NewBufferMemory(10)
	m.Append(Message{Role: "user", Content: "u1"})
	msgs := m.Messages()
	msgs[0].Content = "modified"
	if m.Messages()[0].Content == "modified" {
		t.Fatal("Messages() should return a copy")
	}
}

func TestBufferMemoryConcurrentAccess(t *testing.T) {
	m := NewBufferMemory(100)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m.Append(Message{Role: "user", Content: fmt.Sprintf("msg-%d-%d", id, j)})
				_ = m.Messages()
			}
		}(i)
	}
	wg.Wait()
}
```

注意：`memory_test.go` 需要 import `"sync"` 和 `"fmt"`。

- [ ] **Step 2: 运行测试，确认编译失败**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run TestBufferMemory -v`
Expected: 编译失败 — `NewBufferMemory` 未定义

- [ ] **Step 3: 实现 Memory 接口和 BufferMemory**

在 `internal/agent/memory.go` 中：

```go
package agent

import "sync"

// Memory 管理 agent 对话上下文
type Memory interface {
	Append(msg Message)
	Messages() []Message
	Reset()
}

// BufferMemory 按消息数裁剪的 Memory 实现
// 裁剪策略：保留第一条 system 消息 + 最新的 N-1 条消息
type BufferMemory struct {
	mu          sync.RWMutex
	maxMessages int
	messages    []Message
}

func NewBufferMemory(maxMessages int) *BufferMemory {
	if maxMessages <= 0 {
		maxMessages = 32
	}
	return &BufferMemory{
		maxMessages: maxMessages,
		messages:    make([]Message, 0, maxMessages),
	}
}

func (m *BufferMemory) Append(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	m.trim()
}

func (m *BufferMemory) Messages() []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *BufferMemory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
}

func (m *BufferMemory) trim() {
	if len(m.messages) <= m.maxMessages {
		return
	}
	keep := make([]Message, 0, m.maxMessages)
	if len(m.messages) > 0 && m.messages[0].Role == "system" {
		keep = append(keep, m.messages[0])
	}
	tailSize := m.maxMessages - len(keep)
	if tailSize <= 0 {
		m.messages = keep[:m.maxMessages]
		return
	}
	start := len(m.messages) - tailSize
	if start < len(keep) {
		start = len(keep)
	}
	keep = append(keep, m.messages[start:]...)
	m.messages = keep
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run TestBufferMemory -v`
Expected: 4 tests PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add internal/agent/memory.go internal/agent/memory_test.go
git commit -m "feat(agent): add Memory interface and BufferMemory implementation"
```

---

### Task 2: Handler 接口 + NoopHandler + MultiHandler

**Files:**
- Create: `internal/agent/handler.go`
- Create: `internal/agent/handler_test.go`

- [ ] **Step 1: 写 Handler 接口和 MultiHandler 的失败测试**

在 `internal/agent/handler_test.go` 中：

```go
package agent

import (
	"context"
	"testing"
)

type recordingHandler struct {
	NoopHandler
	events []string
}

func (h *recordingHandler) OnStepStart(_ context.Context, step int) {
	h.events = append(h.events, "step_start")
}

func (h *recordingHandler) OnToolEnd(_ context.Context, _ int, t ToolTrace) {
	h.events = append(h.events, "tool_end:"+t.Tool)
}

func (h *recordingHandler) OnComplete(_ context.Context, _ string, _ []ToolTrace) {
	h.events = append(h.events, "complete")
}

func TestMultiHandlerDispatchesToAll(t *testing.T) {
	h1 := &recordingHandler{}
	h2 := &recordingHandler{}
	multi := NewMultiHandler(h1, h2)

	ctx := context.Background()
	multi.OnStepStart(ctx, 1)
	multi.OnComplete(ctx, "done", nil)

	for i, h := range []*recordingHandler{h1, h2} {
		if len(h.events) != 2 {
			t.Fatalf("handler %d: expected 2 events, got %d: %v", i, len(h.events), h.events)
		}
		if h.events[0] != "step_start" || h.events[1] != "complete" {
			t.Fatalf("handler %d: unexpected events: %v", i, h.events)
		}
	}
}

func TestNoopHandlerDoesNotPanic(t *testing.T) {
	var h NoopHandler
	ctx := context.Background()
	h.OnStepStart(ctx, 1)
	h.OnLLMStart(ctx, 1)
	h.OnLLMEnd(ctx, 1, LLMReply{})
	h.OnToolStart(ctx, 1, ToolCall{})
	h.OnToolEnd(ctx, 1, ToolTrace{})
	h.OnComplete(ctx, "", nil)
	h.OnError(ctx, 1, nil)
}
```

- [ ] **Step 2: 运行测试确认编译失败**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run "TestMultiHandler|TestNoopHandler" -v`
Expected: 编译失败

- [ ] **Step 3: 实现 Handler 接口**

在 `internal/agent/handler.go` 中：

```go
package agent

import "context"

// Handler 接收 agent 执行过程中的事件回调
type Handler interface {
	OnStepStart(ctx context.Context, step int)
	OnLLMStart(ctx context.Context, step int)
	OnLLMEnd(ctx context.Context, step int, reply LLMReply)
	OnToolStart(ctx context.Context, step int, call ToolCall)
	OnToolEnd(ctx context.Context, step int, trace ToolTrace)
	OnComplete(ctx context.Context, summary string, traces []ToolTrace)
	OnError(ctx context.Context, step int, err error)
}

// NoopHandler 提供空实现，具体 handler 嵌入后只覆盖关心的方法
type NoopHandler struct{}

func (NoopHandler) OnStepStart(context.Context, int)                     {}
func (NoopHandler) OnLLMStart(context.Context, int)                      {}
func (NoopHandler) OnLLMEnd(context.Context, int, LLMReply)              {}
func (NoopHandler) OnToolStart(context.Context, int, ToolCall)           {}
func (NoopHandler) OnToolEnd(context.Context, int, ToolTrace)            {}
func (NoopHandler) OnComplete(context.Context, string, []ToolTrace)      {}
func (NoopHandler) OnError(context.Context, int, error)                  {}

// MultiHandler 将事件分发给多个 handler
type MultiHandler struct {
	handlers []Handler
}

func NewMultiHandler(handlers ...Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) OnStepStart(ctx context.Context, step int) {
	for _, h := range m.handlers {
		h.OnStepStart(ctx, step)
	}
}

func (m *MultiHandler) OnLLMStart(ctx context.Context, step int) {
	for _, h := range m.handlers {
		h.OnLLMStart(ctx, step)
	}
}

func (m *MultiHandler) OnLLMEnd(ctx context.Context, step int, reply LLMReply) {
	for _, h := range m.handlers {
		h.OnLLMEnd(ctx, step, reply)
	}
}

func (m *MultiHandler) OnToolStart(ctx context.Context, step int, call ToolCall) {
	for _, h := range m.handlers {
		h.OnToolStart(ctx, step, call)
	}
}

func (m *MultiHandler) OnToolEnd(ctx context.Context, step int, trace ToolTrace) {
	for _, h := range m.handlers {
		h.OnToolEnd(ctx, step, trace)
	}
}

func (m *MultiHandler) OnComplete(ctx context.Context, summary string, traces []ToolTrace) {
	for _, h := range m.handlers {
		h.OnComplete(ctx, summary, traces)
	}
}

func (m *MultiHandler) OnError(ctx context.Context, step int, err error) {
	for _, h := range m.handlers {
		h.OnError(ctx, step, err)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run "TestMultiHandler|TestNoopHandler" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add internal/agent/handler.go internal/agent/handler_test.go
git commit -m "feat(agent): add Handler interface with NoopHandler and MultiHandler"
```

---

### Task 3: traceHandler + streamHandler

**Files:**
- Create: `internal/agent/handler_trace.go`
- Create: `internal/agent/handler_stream.go`

- [ ] **Step 1: 实现 traceHandler**

在 `internal/agent/handler_trace.go` 中：

```go
package agent

import "context"

// traceHandler 收集 ToolTrace 切片，用于 RunWithTrace
type traceHandler struct {
	NoopHandler
	traces []ToolTrace
}

func newTraceHandler() *traceHandler {
	return &traceHandler{traces: make([]ToolTrace, 0)}
}

func (h *traceHandler) OnToolEnd(_ context.Context, _ int, trace ToolTrace) {
	h.traces = append(h.traces, trace)
}
```

- [ ] **Step 2: 实现 streamHandler**

在 `internal/agent/handler_stream.go` 中：

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// streamHandler 将 agent 事件发送到 channel，用于 RunStream
type streamHandler struct {
	NoopHandler
	ch  chan<- AgentEvent
	ctx context.Context
}

func newStreamHandler(ctx context.Context, ch chan<- AgentEvent) *streamHandler {
	return &streamHandler{ch: ch, ctx: ctx}
}

func (h *streamHandler) emit(ev AgentEvent) {
	select {
	case h.ch <- ev:
	case <-h.ctx.Done():
	}
}

func (h *streamHandler) OnStepStart(_ context.Context, step int) {
	h.emit(AgentEvent{Kind: EventStepStart, Step: step})
}

func (h *streamHandler) OnLLMStart(_ context.Context, step int) {
	h.emit(AgentEvent{Kind: EventLLMStart, Step: step})
}

func (h *streamHandler) OnLLMEnd(_ context.Context, step int, _ LLMReply) {
	h.emit(AgentEvent{Kind: EventLLMEnd, Step: step})
}

func (h *streamHandler) OnToolEnd(_ context.Context, step int, trace ToolTrace) {
	toolData, _ := json.Marshal(map[string]any{
		"tool":        trace.Tool,
		"success":     trace.Success,
		"duration_ms": trace.DurationMS,
		"preview":     trace.Preview,
	})
	h.emit(AgentEvent{Kind: EventToolEnd, Step: step, Tool: trace.Tool, Data: toolData})
}

func (h *streamHandler) OnComplete(_ context.Context, summary string, _ []ToolTrace) {
	h.emit(AgentEvent{Kind: EventComplete, Content: summary})
}

func (h *streamHandler) OnError(_ context.Context, step int, err error) {
	h.emit(AgentEvent{Kind: EventError, Step: step, Content: fmt.Sprintf("%v", err)})
}
```

- [ ] **Step 3: 运行全量编译确认无语法错误**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go build ./internal/agent/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add internal/agent/handler_trace.go internal/agent/handler_stream.go
git commit -m "feat(agent): add traceHandler and streamHandler implementations"
```

---

### Task 4: Functional Options + runCore 统一循环

**Files:**
- Create: `internal/agent/options.go`
- Modify: `internal/agent/engine.go` (全面重写)
- Delete: `internal/agent/context.go`
- Delete: `internal/agent/stream.go`

这是最核心的 task。重写 `engine.go`，将 `RunWithTrace` 和 `runStreamInternal` 合并为 `runCore`，引入 Functional Options。

- [ ] **Step 1: 创建 options.go**

在 `internal/agent/options.go` 中：

```go
package agent

import "ai-agent-api/internal/observability"

// Option 配置 AgentEngine
type Option func(*AgentEngine)

func WithMaxSteps(n int) Option {
	return func(e *AgentEngine) {
		if n > 0 {
			e.maxSteps = n
		}
	}
}

func WithSystemPrompt(prompt string) Option {
	return func(e *AgentEngine) {
		if prompt != "" {
			e.systemPrompt = prompt
		}
	}
}

func WithMemory(m Memory) Option {
	return func(e *AgentEngine) {
		if m != nil {
			e.memory = m
		}
	}
}

func WithHandlers(hs ...Handler) Option {
	return func(e *AgentEngine) {
		e.extraHandlers = append(e.extraHandlers, hs...)
	}
}

func WithMetrics(m *observability.Metrics) Option {
	return func(e *AgentEngine) {
		e.metrics = m
	}
}
```

- [ ] **Step 2: 重写 engine.go**

将 `internal/agent/engine.go` 完全重写为：

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-agent-api/internal/observability"
)

const defaultSystemPrompt = "你是企业 API 助手，只能输出结构化汇总，不泄露原始内部数据。"

type ToolDispatcher interface {
	Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error)
	Has(name string) bool
}

type AgentEngine struct {
	llmClient     LLMClient
	dispatcher    ToolDispatcher
	memory        Memory
	maxSteps      int
	systemPrompt  string
	toolCatalog   []ToolDefinition
	extraHandlers []Handler
	metrics       *observability.Metrics
}

type ToolTrace struct {
	Step       int
	Tool       string
	Success    bool
	DurationMS int64
	Preview    string
}

func NewAgentEngine(llmClient LLMClient, dispatcher ToolDispatcher, opts ...Option) *AgentEngine {
	e := &AgentEngine{
		llmClient:    llmClient,
		dispatcher:   dispatcher,
		maxSteps:     10,
		systemPrompt: defaultSystemPrompt,
		memory:       NewBufferMemory(64),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *AgentEngine) SetToolCatalog(catalog []ToolDefinition) {
	if len(catalog) == 0 {
		e.toolCatalog = nil
		return
	}
	e.toolCatalog = make([]ToolDefinition, len(catalog))
	copy(e.toolCatalog, catalog)
}

func (e *AgentEngine) modelName() string {
	if namer, ok := e.llmClient.(interface{ Model() string }); ok {
		return namer.Model()
	}
	return "unknown"
}

func (e *AgentEngine) recordMetrics() bool {
	return e.metrics != nil
}

// Run 执行 agent 循环并返回汇总文本（包含工具调用轨迹）
func (e *AgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
	summary, traces, err := e.RunWithTrace(ctx, userQuery)
	if err != nil {
		return "", err
	}
	return attachToolTraceSummary(summary, traces), nil
}

// RunWithTrace 执行 agent 循环并分别返回汇总文本和 trace
func (e *AgentEngine) RunWithTrace(ctx context.Context, userQuery string) (string, []ToolTrace, error) {
	th := newTraceHandler()
	var h Handler = th
	if len(e.extraHandlers) > 0 {
		h = NewMultiHandler(append([]Handler{th}, e.extraHandlers...)...)
	}
	summary, err := e.runCore(ctx, userQuery, h)
	return summary, th.traces, err
}

// RunStream 以事件流方式执行 agent 循环
func (e *AgentEngine) RunStream(ctx context.Context, userQuery string) <-chan AgentEvent {
	ch := make(chan AgentEvent, 32)
	go func() {
		defer close(ch)
		sh := newStreamHandler(ctx, ch)
		var h Handler = sh
		if len(e.extraHandlers) > 0 {
			h = NewMultiHandler(append([]Handler{sh}, e.extraHandlers...)...)
		}
		e.runCore(ctx, userQuery, h)
	}()
	return ch
}

// runCore 是唯一的 agent 执行循环
func (e *AgentEngine) runCore(ctx context.Context, userQuery string, h Handler) (string, error) {
	mem := e.memory
	mem.Reset()
	mem.Append(Message{Role: "system", Content: e.systemPrompt})
	mem.Append(Message{Role: "user", Content: userQuery})

	for step := 1; step <= e.maxSteps; step++ {
		h.OnStepStart(ctx, step)

		// LLM 调用
		h.OnLLMStart(ctx, step)
		llmStart := time.Now()
		reply, err := e.llmClient.Next(ctx, mem.Messages(), e.toolCatalog)
		llmDuration := time.Since(llmStart)

		if e.recordMetrics() {
			status := "ok"
			if err != nil {
				status = "error"
			}
			e.metrics.RecordLLMRequest(e.modelName(), status, llmDuration.Seconds())
			if err == nil {
				e.metrics.RecordLLMTokens(e.modelName(), reply.PromptTokens, reply.CompletionTokens)
			}
		}

		if err != nil {
			h.OnError(ctx, step, fmt.Errorf("llm next failed: %w", err))
			return "", fmt.Errorf("llm next failed: %w", err)
		}
		h.OnLLMEnd(ctx, step, reply)

		// 无工具调用 → 结束
		if len(reply.ToolCalls) == 0 {
			content := reply.Content
			if content == "" {
				content = "结构化汇总结果为空。"
			}
			h.OnComplete(ctx, content, nil)
			return content, nil
		}

		mem.Append(Message{Role: "assistant", ToolCalls: reply.ToolCalls})

		// 执行工具调用
		for _, tc := range reply.ToolCalls {
			if !e.dispatcher.Has(tc.Name) {
				err := fmt.Errorf("unknown tool: %s", tc.Name)
				h.OnError(ctx, step, err)
				return "", err
			}

			h.OnToolStart(ctx, step, tc)
			start := time.Now()
			out, execErr := e.dispatcher.Dispatch(ctx, tc.Name, tc.Args)
			duration := time.Since(start)

			if e.recordMetrics() {
				toolStatus := "ok"
				if execErr != nil {
					toolStatus = "error"
				}
				e.metrics.RecordToolCall(tc.Name, toolStatus, duration.Seconds())
			}

			content := encodeToolResult(out, execErr)
			mem.Append(Message{Role: "tool", ToolCallID: tc.ID, Content: content})

			trace := ToolTrace{
				Step:       step,
				Tool:       tc.Name,
				Success:    execErr == nil,
				DurationMS: duration.Milliseconds(),
				Preview:    shortPreview(content, 100),
			}
			h.OnToolEnd(ctx, step, trace)
		}
	}

	msg := fmt.Sprintf("agent stopped: reached max steps (%d)", e.maxSteps)
	h.OnComplete(ctx, msg, nil)
	return msg, nil
}

func encodeToolResult(result any, execErr error) string {
	if execErr != nil {
		return fmt.Sprintf(`{"error":%q}`, execErr.Error())
	}
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal tool result failed: %s"}`, err.Error())
	}
	return string(body)
}

func attachToolTraceSummary(summary string, traces []ToolTrace) string {
	if len(traces) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(summary))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("工具调用轨迹:\n")
	for i, t := range traces {
		status := "ok"
		if !t.Success {
			status = "error"
		}
		b.WriteString(fmt.Sprintf("%d. step=%d tool=%s status=%s latency=%dms preview=%s\n",
			i+1, t.Step, t.Tool, status, t.DurationMS, t.Preview))
	}
	return strings.TrimSpace(b.String())
}

func shortPreview(s string, max int) string {
	flat := strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if max <= 0 || len(flat) <= max {
		return flat
	}
	return flat[:max] + "..."
}
```

- [ ] **Step 3: 删除旧文件 context.go 和 stream.go**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
rm internal/agent/context.go internal/agent/stream.go
```

- [ ] **Step 4: 更新 engine_test.go 适配新的 NewAgentEngine 签名**

`NewAgentEngine` 签名从 `(llm, dispatcher, maxSteps int)` 变为 `(llm, dispatcher, ...Option)`。

更新 `internal/agent/engine_test.go`：

所有 `NewAgentEngine(llm, registry, N)` 改为 `NewAgentEngine(llm, registry, WithMaxSteps(N))`。

删除 `TestContextManagerTrim`（已由 `TestBufferMemoryTrimPreservesSystem` 覆盖）。

- [ ] **Step 5: 更新 stream_test.go 适配新签名**

所有 `NewAgentEngine(llm, dispatcher, N)` 改为 `NewAgentEngine(llm, dispatcher, WithMaxSteps(N))`。

删除 `engine.SetToolCatalog(nil)` 这种不再需要的调用（NewAgentEngine 默认 toolCatalog 为 nil）。

- [ ] **Step 6: 更新 openai_engine_integration_test.go 适配新签名**

`NewAgentEngine(llm, dispatcher, 4)` 改为 `NewAgentEngine(llm, dispatcher, WithMaxSteps(4))`。

`engine.SetMetrics(...)` 不再需要（如果测试中有的话），通过 `WithMetrics(...)` option 传入。

- [ ] **Step 7: 更新 cmd/server/main.go 适配新签名**

将：
```go
engine := agent.NewAgentEngine(llmClient, registry, cfg.Agent.MaxSteps)
engine.SetMetrics(metrics)
```

改为：
```go
engine := agent.NewAgentEngine(llmClient, registry,
    agent.WithMaxSteps(cfg.Agent.MaxSteps),
    agent.WithMetrics(metrics),
)
```

- [ ] **Step 8: 运行全量测试**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./... -v`
Expected: 全部 PASS

- [ ] **Step 9: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add -A
git commit -m "refactor(agent): unify RunWithTrace/RunStream into runCore with Handler pattern

- Replace ContextManager with Memory interface
- Introduce Functional Options for AgentEngine construction
- Eliminate duplicate loop logic between sync and stream execution
- Delete context.go and stream.go"
```

---

## Chunk 2: 中间件系统

### Task 5: Middleware 类型 + Chain + RetryMiddleware

**Files:**
- Create: `internal/agent/middleware.go`
- Create: `internal/agent/middleware_retry.go`
- Create: `internal/agent/middleware_test.go`

- [ ] **Step 1: 写中间件的失败测试**

在 `internal/agent/middleware_test.go` 中：

```go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
)

func TestChainExecutionOrder(t *testing.T) {
	var order []string
	mw1 := func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, name string, args json.RawMessage) (any, error) {
			order = append(order, "mw1_before")
			r, e := next(ctx, name, args)
			order = append(order, "mw1_after")
			return r, e
		}
	}
	mw2 := func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, name string, args json.RawMessage) (any, error) {
			order = append(order, "mw2_before")
			r, e := next(ctx, name, args)
			order = append(order, "mw2_after")
			return r, e
		}
	}

	handler := Chain(mw1, mw2)(func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
		order = append(order, "handler")
		return "ok", nil
	})

	_, _ = handler(context.Background(), "test", nil)
	expected := []string{"mw1_before", "mw2_before", "handler", "mw2_after", "mw1_after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("position %d: expected %s, got %s", i, expected[i], order[i])
		}
	}
}

func TestRetryMiddlewareRetriesOnError(t *testing.T) {
	var calls int32
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: 0})(
		func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			n := atomic.AddInt32(&calls, 1)
			if n < 3 {
				return nil, errors.New("transient error")
			}
			return "success", nil
		},
	)

	result, err := handler(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result != "success" {
		t.Fatalf("expected 'success', got: %v", result)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryMiddlewareStopsOnPermanentError(t *testing.T) {
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: 0})(
		func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, &permanentError{msg: "fatal"}
		},
	)

	_, err := handler(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRetryMiddlewareRespectsContextCancellation(t *testing.T) {
	var attempts int32
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 5, BaseDelay: 100 * time.Millisecond})(
		func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, errors.New("always fail")
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := handler(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if atomic.LoadInt32(&attempts) > 2 {
		t.Fatalf("expected at most 2 attempts before timeout, got %d", attempts)
	}
}

type permanentError struct{ msg string }

func (e *permanentError) Error() string   { return e.msg }
func (e *permanentError) Permanent() bool { return true }
```

- [ ] **Step 2: 运行测试确认编译失败**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run "TestChain|TestRetry" -v`
Expected: 编译失败

- [ ] **Step 3: 实现 middleware.go**

在 `internal/agent/middleware.go` 中：

```go
package agent

import (
	"context"
	"encoding/json"
)

// ToolHandler 是工具调用的函数签名
type ToolHandler func(ctx context.Context, name string, args json.RawMessage) (any, error)

// Middleware 包装 ToolHandler
type Middleware func(next ToolHandler) ToolHandler

// Chain 组合多个中间件。第一个中间件最外层（最先进入、最后退出）
func Chain(middlewares ...Middleware) Middleware {
	return func(final ToolHandler) ToolHandler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
```

- [ ] **Step 4: 实现 middleware_retry.go**

在 `internal/agent/middleware_retry.go` 中：

```go
package agent

import (
	"context"
	"encoding/json"
	"time"
)

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func RetryMiddleware(cfg RetryConfig) Middleware {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 5 * time.Second
	}
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, name string, args json.RawMessage) (any, error) {
			var lastErr error
			for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				result, err := next(ctx, name, args)
				if err == nil {
					return result, nil
				}
				lastErr = err
				if isPermanent(err) {
					return nil, err
				}
				if attempt < cfg.MaxAttempts-1 && cfg.BaseDelay > 0 {
					delay := cfg.BaseDelay * time.Duration(attempt+1)
					if delay > cfg.MaxDelay {
						delay = cfg.MaxDelay
					}
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			}
			return nil, lastErr
		}
	}
}

func isPermanent(err error) bool {
	type perm interface{ Permanent() bool }
	if p, ok := err.(perm); ok {
		return p.Permanent()
	}
	return false
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run "TestChain|TestRetry" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add internal/agent/middleware.go internal/agent/middleware_retry.go internal/agent/middleware_test.go
git commit -m "feat(agent): add Middleware chain and RetryMiddleware"
```

---

### Task 6: 集成中间件到 AgentEngine

**Files:**
- Modify: `internal/agent/options.go`
- Modify: `internal/agent/engine.go`

- [ ] **Step 1: 在 options.go 中添加 WithMiddleware option**

在 `internal/agent/options.go` 中追加：

```go
func WithMiddleware(mws ...Middleware) Option {
	return func(e *AgentEngine) {
		e.middlewares = append(e.middlewares, mws...)
	}
}
```

- [ ] **Step 2: 在 engine.go 的 AgentEngine 中添加 middlewares 字段和 dispatchFn**

在 `AgentEngine` struct 中添加：
```go
middlewares []Middleware
dispatchFn  ToolHandler
```

在 `NewAgentEngine` 的最后（所有 option 应用后），组装中间件链：
```go
e.dispatchFn = e.dispatcher.Dispatch
if len(e.middlewares) > 0 {
    e.dispatchFn = Chain(e.middlewares...)(e.dispatcher.Dispatch)
}
```

在 `runCore` 中，将 `e.dispatcher.Dispatch(ctx, tc.Name, tc.Args)` 改为 `e.dispatchFn(ctx, tc.Name, tc.Args)`。

- [ ] **Step 3: 写中间件集成测试**

在 `internal/agent/middleware_test.go` 中追加：

```go
func TestEngineWithRetryMiddleware(t *testing.T) {
	var attempts int32
	failDispatcher := &mockToolDispatcherFunc{
		hasFn: func(name string) bool { return name == "search_api" },
		dispatchFn: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 2 {
				return nil, errors.New("transient")
			}
			return map[string]any{"items": []string{"GET /pets"}}, nil
		},
	}

	llm := &scriptedLLMClient{replies: []LLMReply{
		{ToolCalls: []ToolCall{{ID: "1", Name: "search_api", Args: json.RawMessage(`{"query":"pets"}`)}}},
		{Content: "done"},
	}}

	engine := NewAgentEngine(llm, failDispatcher,
		WithMaxSteps(5),
		WithMiddleware(RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: 0})),
	)

	out, err := engine.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected success with retry, got: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("unexpected output: %s", out)
	}
}

type mockToolDispatcherFunc struct {
	hasFn      func(string) bool
	dispatchFn func(context.Context, string, json.RawMessage) (any, error)
}

func (m *mockToolDispatcherFunc) Has(name string) bool {
	return m.hasFn(name)
}

func (m *mockToolDispatcherFunc) Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error) {
	return m.dispatchFn(ctx, name, args)
}
```

注意：`middleware_test.go` 中需要增加 `"strings"`, `"time"` import。

- [ ] **Step 4: 运行全量测试**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add internal/agent/options.go internal/agent/engine.go internal/agent/middleware_test.go
git commit -m "feat(agent): integrate middleware chain into AgentEngine dispatch"
```

---

## Chunk 3: 智能增强

### Task 7: TokenWindowMemory

**Files:**
- Create: `internal/agent/memory_token.go`
- Modify: `internal/agent/memory_test.go` (追加测试)

- [ ] **Step 1: 写 TokenWindowMemory 的失败测试**

在 `internal/agent/memory_test.go` 中追加：

```go
func TestTokenWindowMemoryTrimsOnTokenLimit(t *testing.T) {
	// 使用简单估算器：每个 rune 算 1 token
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(20, estimator)

	m.Append(Message{Role: "system", Content: "sys"})     // 3 tokens
	m.Append(Message{Role: "user", Content: "question"})   // 8 tokens
	m.Append(Message{Role: "assistant", Content: "answer"}) // 6 tokens
	m.Append(Message{Role: "user", Content: "followup"})   // 8 tokens

	// 总 25 tokens > 20，应裁剪
	msgs := m.Messages()
	if msgs[0].Role != "system" {
		t.Fatal("system message must be preserved")
	}
	if msgs[len(msgs)-1].Content != "followup" {
		t.Fatal("latest user message must be preserved")
	}
	// 总 token 应 <= 20
	total := 0
	for _, msg := range msgs {
		total += estimator(msg.Content)
	}
	if total > 20 {
		t.Fatalf("total tokens %d exceeds limit 20", total)
	}
}

func TestTokenWindowMemoryPreservesSystemAndLastUser(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(15, estimator)

	m.Append(Message{Role: "system", Content: "system prompt"}) // 13 tokens
	m.Append(Message{Role: "user", Content: "q"})               // 1 token

	msgs := m.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestTokenWindowMemoryTruncatesLongToolResult(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(100, estimator)

	m.Append(Message{Role: "system", Content: "sys"})
	m.Append(Message{Role: "user", Content: "q"})
	// 工具结果 60 chars > maxTokens/4 = 25
	longResult := strings.Repeat("x", 60)
	m.Append(Message{Role: "tool", Content: longResult})

	msgs := m.Messages()
	for _, msg := range msgs {
		if msg.Role == "tool" && len([]rune(msg.Content)) > 25 {
			t.Fatalf("tool result should be truncated, got len=%d", len([]rune(msg.Content)))
		}
	}
}

func TestTokenWindowMemoryOnlySystemMessage(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	// maxTokens 小于 system 消息本身，不应 panic
	m := NewTokenWindowMemory(2, estimator)
	m.Append(Message{Role: "system", Content: "long system prompt"})

	msgs := m.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestTokenWindowMemoryConcurrentAccess(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(1000, estimator)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m.Append(Message{Role: "user", Content: fmt.Sprintf("msg-%d-%d", id, j)})
				_ = m.Messages()
			}
		}(i)
	}
	wg.Wait()
}
```

注意：`memory_test.go` 需要增加 `"strings"` import。

- [ ] **Step 2: 运行测试确认编译失败**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run TestTokenWindow -v`
Expected: 编译失败

- [ ] **Step 3: 实现 TokenWindowMemory**

在 `internal/agent/memory_token.go` 中：

```go
package agent

import "sync"

// TokenEstimator 估算文本的 token 数量
type TokenEstimator func(text string) int

// DefaultTokenEstimator 使用 rune 数量作为粗略的 token 估算
func DefaultTokenEstimator(text string) int {
	return len([]rune(text))
}

// TokenWindowMemory 按 token 数量裁剪的 Memory 实现
type TokenWindowMemory struct {
	mu        sync.RWMutex
	maxTokens int
	estimator TokenEstimator
	messages  []Message
}

func NewTokenWindowMemory(maxTokens int, estimator TokenEstimator) *TokenWindowMemory {
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	if estimator == nil {
		estimator = DefaultTokenEstimator
	}
	return &TokenWindowMemory{
		maxTokens: maxTokens,
		estimator: estimator,
		messages:  make([]Message, 0),
	}
}

func (m *TokenWindowMemory) Append(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 截断过长的工具结果
	if msg.Role == "tool" {
		maxSingle := m.maxTokens / 4
		if m.estimator(msg.Content) > maxSingle {
			runes := []rune(msg.Content)
			if len(runes) > maxSingle {
				msg.Content = string(runes[:maxSingle])
			}
		}
	}

	m.messages = append(m.messages, msg)
	m.trim()
}

func (m *TokenWindowMemory) Messages() []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *TokenWindowMemory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
}

func (m *TokenWindowMemory) totalTokens() int {
	total := 0
	for _, msg := range m.messages {
		total += m.estimator(msg.Content)
	}
	return total
}

func (m *TokenWindowMemory) trim() {
	if m.totalTokens() <= m.maxTokens {
		return
	}

	// 不足 2 条消息时无法裁剪
	if len(m.messages) <= 1 {
		return
	}

	// 标记保护位：system(index 0) 和最后一条 user
	protected := make(map[int]bool)
	if len(m.messages) > 0 && m.messages[0].Role == "system" {
		protected[0] = true
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "user" {
			protected[i] = true
			break
		}
	}

	// 从最旧的非保护消息开始删除
	for m.totalTokens() > m.maxTokens {
		removed := false
		for i := 0; i < len(m.messages); i++ {
			if protected[i] {
				continue
			}
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			// 重新计算保护位索引
			newProtected := make(map[int]bool)
			for k := range protected {
				if k < i {
					newProtected[k] = true
				} else if k > i {
					newProtected[k-1] = true
				}
			}
			protected = newProtected
			removed = true
			break
		}
		if !removed {
			break
		}
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -run TestTokenWindow -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add internal/agent/memory_token.go internal/agent/memory_test.go
git commit -m "feat(agent): add TokenWindowMemory with token-aware context trimming"
```

---

### Task 8: 并行工具执行

**Files:**
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/engine_test.go` (追加并行测试)

- [ ] **Step 1: 确认 errgroup 依赖可用**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && grep -q "golang.org/x/sync" go.mod || go get golang.org/x/sync`

- [ ] **Step 2: 写并行执行的失败测试**

在 `internal/agent/engine_test.go` 中追加：

```go
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

	// 用 slow dispatcher 包装来检测并发
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

	time.Sleep(50 * time.Millisecond) // 模拟延迟

	d.mu.Lock()
	d.concurrent--
	d.mu.Unlock()

	return d.inner.Dispatch(ctx, name, args)
}
```

注意：`engine_test.go` 需要增加 `"sync"`, `"time"` import。

- [ ] **Step 3: 在 engine.go 中实现 dispatchTools 方法支持并行**

在 `engine.go` 中添加：

```go
import "sync/errgroup"
```

添加 `dispatchTools` 方法并在 `runCore` 中替换工具调用循环：

```go
type toolResult struct {
	callID  string
	content string
	trace   ToolTrace
}

func (e *AgentEngine) dispatchTools(ctx context.Context, calls []ToolCall, step int, h Handler) []toolResult {
	results := make([]toolResult, len(calls))

	if len(calls) == 1 {
		// 单个工具调用，简单路径
		tc := calls[0]
		h.OnToolStart(ctx, step, tc)
		start := time.Now()
		out, execErr := e.dispatchFn(ctx, tc.Name, tc.Args)
		duration := time.Since(start)
		if e.recordMetrics() {
			status := "ok"
			if execErr != nil {
				status = "error"
			}
			e.metrics.RecordToolCall(tc.Name, status, duration.Seconds())
		}
		content := encodeToolResult(out, execErr)
		results[0] = toolResult{
			callID:  tc.ID,
			content: content,
			trace:   ToolTrace{Step: step, Tool: tc.Name, Success: execErr == nil, DurationMS: duration.Milliseconds(), Preview: shortPreview(content, 100)},
		}
		h.OnToolEnd(ctx, step, results[0].trace)
		return results
	}

	// 多个工具调用，并行执行
	g, gctx := errgroup.WithContext(ctx)
	for i, tc := range calls {
		g.Go(func() error {
			h.OnToolStart(gctx, step, tc)
			start := time.Now()
			out, execErr := e.dispatchFn(gctx, tc.Name, tc.Args)
			duration := time.Since(start)
			if e.recordMetrics() {
				status := "ok"
				if execErr != nil {
					status = "error"
				}
				e.metrics.RecordToolCall(tc.Name, status, duration.Seconds())
			}
			content := encodeToolResult(out, execErr)
			results[i] = toolResult{
				callID:  tc.ID,
				content: content,
				trace:   ToolTrace{Step: step, Tool: tc.Name, Success: execErr == nil, DurationMS: duration.Milliseconds(), Preview: shortPreview(content, 100)},
			}
			h.OnToolEnd(gctx, step, results[i].trace)
			return nil
		})
	}
	g.Wait()
	return results
}
```

在 `runCore` 中替换工具调用部分为：

```go
// 执行工具调用（单个串行，多个并行）
for _, tc := range reply.ToolCalls {
    if !e.dispatcher.Has(tc.Name) {
        err := fmt.Errorf("unknown tool: %s", tc.Name)
        h.OnError(ctx, step, err)
        return "", err
    }
}

results := e.dispatchTools(ctx, reply.ToolCalls, step, h)
for _, r := range results {
    mem.Append(Message{Role: "tool", ToolCallID: r.callID, Content: r.content})
}
```

- [ ] **Step 4: 运行全量测试**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./internal/agent/ -v -race`
Expected: 全部 PASS，无 race condition

- [ ] **Step 5: Commit**

```bash
cd /Users/yygqzzk/Code/ai-agent-api
git add internal/agent/engine.go internal/agent/engine_test.go
git commit -m "feat(agent): parallel tool execution for multiple tool_calls

Uses errgroup for concurrent dispatch when LLM returns >1 tool call.
Single tool calls use simple sequential path."
```

---

### Task 9: 最终集成验证

**Files:**
- No new files

- [ ] **Step 1: 运行全量测试（含 race detector）**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go test ./... -v -race`
Expected: 全部 PASS

- [ ] **Step 2: 编译检查**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go build ./...`
Expected: 编译成功

- [ ] **Step 3: 验证 go vet**

Run: `cd /Users/yygqzzk/Code/ai-agent-api && go vet ./...`
Expected: 无警告

- [ ] **Step 4: 确认删除了 context.go 和 stream.go**

Run: `ls internal/agent/context.go internal/agent/stream.go 2>&1`
Expected: "No such file"

- [ ] **Step 5: 确认文件结构符合设计**

Run: `ls internal/agent/*.go | sort`
Expected output:
```
internal/agent/engine.go
internal/agent/engine_test.go
internal/agent/event.go
internal/agent/handler.go
internal/agent/handler_stream.go
internal/agent/handler_test.go
internal/agent/handler_trace.go
internal/agent/llm.go
internal/agent/memory.go
internal/agent/memory_test.go
internal/agent/memory_token.go
internal/agent/middleware.go
internal/agent/middleware_retry.go
internal/agent/middleware_test.go
internal/agent/openai_engine_integration_test.go
internal/agent/openai_llm.go
internal/agent/openai_llm_test.go
internal/agent/options.go
```
