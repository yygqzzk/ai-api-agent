# Observability Implementation Plan

> **For Claude:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Prometheus metrics and slog structured logging to the ai-agent-api, covering HTTP, tool, LLM, and RAG layers.

**Architecture:** Create `internal/observability` package with `Metrics` (Prometheus counters/histograms) and logger factory. Inject into `mcp.Server`, `agent.AgentEngine`, and `cmd/server/main.go` via constructor parameters. Expose `/metrics` endpoint alongside `/mcp` and `/healthz`.

**Tech Stack:** `github.com/prometheus/client_golang`, Go stdlib `log/slog`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/observability/metrics.go` | Create | Metrics struct with all Prometheus collectors |
| `internal/observability/metrics_test.go` | Create | Unit tests for metrics registration and recording |
| `internal/observability/logger.go` | Create | slog.Logger factory (JSON handler) |
| `internal/observability/logger_test.go` | Create | Verify JSON output format |
| `internal/agent/openai_llm.go` | Modify | Parse `usage` from OpenAI response, expose via return value |
| `internal/agent/engine.go` | Modify | Accept Metrics, record LLM token usage + tool call metrics |
| `internal/agent/engine_test.go` | Modify | Pass nil Metrics (noop-safe) |
| `internal/mcp/server.go` | Modify | Accept Metrics + `*slog.Logger`, replace logWrapper |
| `internal/mcp/middleware.go` | Modify | Record HTTP request metrics, use slog |
| `internal/mcp/server_test.go` | Modify | Pass nil Metrics + nil logger |
| `cmd/server/main.go` | Modify | Create Metrics/Logger, register `/metrics`, inject |
| `cmd/server/main_test.go` | Modify | Pass updated constructor args |
| `cmd/server/health.go` | Modify | Use slog if available |

---

## Chunk 1: Observability Package

### Task 1: Add prometheus dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Install prometheus client**

```bash
cd /Users/yygqzzk/code/ai-agent-api && go get github.com/prometheus/client_golang/prometheus github.com/prometheus/client_golang/prometheus/promhttp
```

- [ ] **Step 2: Tidy modules**

```bash
go mod tidy
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add prometheus client_golang"
```

---

### Task 2: Create Metrics struct

**Files:**
- Create: `internal/observability/metrics.go`
- Create: `internal/observability/metrics_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/observability/metrics_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/observability/ -v -run TestNewMetrics
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Write implementation**

```go
// internal/observability/metrics.go
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus collectors. All methods are nil-safe.
type Metrics struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	toolCallsTotal  *prometheus.CounterVec
	toolCallDuration *prometheus.HistogramVec
	llmRequestsTotal *prometheus.CounterVec
	llmRequestDuration *prometheus.HistogramVec
	llmTokensTotal  *prometheus.CounterVec
	ragSearchesTotal *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_requests_total",
			Help: "Total MCP JSON-RPC requests",
		}, []string{"method", "status"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mcp_request_duration_seconds",
			Help:    "MCP request latency distribution",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),

		toolCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tool_calls_total",
			Help: "Total tool invocations",
		}, []string{"tool", "status"}),

		toolCallDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tool_call_duration_seconds",
			Help:    "Tool call latency distribution",
			Buckets: prometheus.DefBuckets,
		}, []string{"tool"}),

		llmRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_requests_total",
			Help: "Total LLM API calls",
		}, []string{"model", "status"}),

		llmRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "llm_request_duration_seconds",
			Help:    "LLM API call latency distribution",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"model"}),

		llmTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_tokens_total",
			Help: "Total LLM tokens consumed",
		}, []string{"model", "type"}),

		ragSearchesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rag_searches_total",
			Help: "Total RAG search operations",
		}, []string{"mode"}),
	}

	reg.MustRegister(
		m.requestsTotal, m.requestDuration,
		m.toolCallsTotal, m.toolCallDuration,
		m.llmRequestsTotal, m.llmRequestDuration,
		m.llmTokensTotal, m.ragSearchesTotal,
	)
	return m
}

func (m *Metrics) RecordRequest(method, status string, durationSec float64) {
	if m == nil {
		return
	}
	m.requestsTotal.WithLabelValues(method, status).Inc()
	m.requestDuration.WithLabelValues(method).Observe(durationSec)
}

func (m *Metrics) RecordToolCall(tool, status string, durationSec float64) {
	if m == nil {
		return
	}
	m.toolCallsTotal.WithLabelValues(tool, status).Inc()
	m.toolCallDuration.WithLabelValues(tool).Observe(durationSec)
}

func (m *Metrics) RecordLLMRequest(model, status string, durationSec float64) {
	if m == nil {
		return
	}
	m.llmRequestsTotal.WithLabelValues(model, status).Inc()
	m.llmRequestDuration.WithLabelValues(model).Observe(durationSec)
}

func (m *Metrics) RecordLLMTokens(model string, promptTokens, completionTokens int) {
	if m == nil {
		return
	}
	m.llmTokensTotal.WithLabelValues(model, "prompt").Add(float64(promptTokens))
	m.llmTokensTotal.WithLabelValues(model, "completion").Add(float64(completionTokens))
}

func (m *Metrics) RecordRAGSearch(mode string) {
	if m == nil {
		return
	}
	m.ragSearchesTotal.WithLabelValues(mode).Inc()
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/observability/ -v
```

Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/observability/
git commit -m "feat(observability): add Metrics struct with Prometheus collectors"
```

---

### Task 3: Create Logger factory

**Files:**
- Create: `internal/observability/logger.go`
- Create: `internal/observability/logger_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/observability/logger_test.go
package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, false)

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Fatalf("expected JSON log output, got: %s", output)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got: %s", output)
	}
	if parsed["key"] != "value" {
		t.Fatalf("expected key=value in log, got: %v", parsed)
	}
}

func TestNewLoggerDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, true)

	logger.Debug("debug msg")

	if !strings.Contains(buf.String(), "debug msg") {
		t.Fatal("expected debug message in output when debug=true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/observability/ -v -run TestNewLogger
```

Expected: FAIL — `NewLogger` undefined.

- [ ] **Step 3: Write implementation**

```go
// internal/observability/logger.go
package observability

import (
	"io"
	"log/slog"
	"os"
)

// NewLogger creates a slog.Logger with JSON output.
// If w is nil, defaults to os.Stdout.
func NewLogger(w io.Writer, debug bool) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/observability/ -v
```

Expected: All tests PASS (metrics + logger).

- [ ] **Step 5: Commit**

```bash
git add internal/observability/logger.go internal/observability/logger_test.go
git commit -m "feat(observability): add slog JSON logger factory"
```

---

## Chunk 2: LLM Token Usage Parsing

### Task 4: Parse usage from OpenAI response

**Files:**
- Modify: `internal/agent/openai_llm.go`
- Modify: `internal/agent/openai_llm_test.go` (if exists)

- [ ] **Step 1: Add Usage to openAIChatResponse**

In `internal/agent/openai_llm.go`, add a `Usage` field to `openAIChatResponse`:

```go
type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
```

- [ ] **Step 2: Add Usage to LLMReply**

In `internal/agent/llm.go`, add usage tracking to `LLMReply`:

```go
type LLMReply struct {
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
}
```

- [ ] **Step 3: Populate usage in doRequest**

In `openai_llm.go`'s `doRequest` method, after parsing `parsed.Choices[0].Message`, add:

```go
reply.PromptTokens = parsed.Usage.PromptTokens
reply.CompletionTokens = parsed.Usage.CompletionTokens
```

(Insert after `reply := LLMReply{Content: strings.TrimSpace(msg.Content)}` and the tool call loop, before the return statement.)

- [ ] **Step 4: Add test for usage parsing**

Add to the existing test file (`openai_llm_test.go`) a test that verifies usage is parsed. The mock server response should include:

```json
{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}
```

Verify `reply.PromptTokens == 10` and `reply.CompletionTokens == 5`.

- [ ] **Step 5: Run all agent tests**

```bash
go test ./internal/agent/ -v
```

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/llm.go internal/agent/openai_llm.go internal/agent/openai_llm_test.go
git commit -m "feat(agent): parse LLM token usage from OpenAI response"
```

---

## Chunk 3: Inject Metrics into Agent Engine

### Task 5: AgentEngine records metrics

**Files:**
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/engine_test.go`

- [ ] **Step 1: Add Metrics field to AgentEngine**

Add `metrics *observability.Metrics` to `AgentEngine` struct and an `import "ai-agent-api/internal/observability"`.

Add a setter method:

```go
func (e *AgentEngine) SetMetrics(m *observability.Metrics) {
	e.metrics = m
}
```

This avoids changing the constructor signature — existing callers don't break.

- [ ] **Step 2: Record metrics in RunWithTrace**

In the `RunWithTrace` method:

After `reply, err := e.llmClient.Next(...)`:

```go
llmDuration := time.Since(llmStart)
status := "ok"
if err != nil {
	status = "error"
}
e.metrics.RecordLLMRequest(e.modelName(), status, llmDuration.Seconds())
if err == nil {
	e.metrics.RecordLLMTokens(e.modelName(), reply.PromptTokens, reply.CompletionTokens)
}
```

Add a `llmStart := time.Now()` before the `Next` call.

Add helper:

```go
func (e *AgentEngine) modelName() string {
	if namer, ok := e.llmClient.(interface{ Model() string }); ok {
		return namer.Model()
	}
	return "unknown"
}
```

After each tool dispatch, record:

```go
toolStatus := "ok"
if err != nil {
	toolStatus = "error"
}
e.metrics.RecordToolCall(tc.Name, toolStatus, duration.Seconds())
```

- [ ] **Step 3: Add Model() method to OpenAICompatibleLLMClient**

In `internal/agent/openai_llm.go`:

```go
func (c *OpenAICompatibleLLMClient) Model() string {
	return c.model
}
```

- [ ] **Step 4: Verify existing tests still pass**

```bash
go test ./internal/agent/ -v
```

Expected: All PASS. (Metrics is nil — nil-safe methods handle this.)

- [ ] **Step 5: Commit**

```bash
git add internal/agent/engine.go internal/agent/openai_llm.go
git commit -m "feat(agent): record LLM and tool call metrics in agent engine"
```

---

## Chunk 4: Inject Metrics + slog into MCP Server

### Task 6: MCP Server uses Metrics + slog

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/middleware.go`
- Modify: `internal/mcp/server_test.go`

- [ ] **Step 1: Add Metrics and slog to Server struct**

In `server.go`, add to `ServerOptions`:

```go
type ServerOptions struct {
	RateLimitPerMinute int
	Metrics            *observability.Metrics
	Logger             *slog.Logger
}
```

Import `"log/slog"` and `"ai-agent-api/internal/observability"`.

In `NewServer`, store them:

```go
type Server struct {
	cfg      config.Config
	registry *tools.Registry
	hooks    Hooks
	options  ServerOptions
	limiter  *fixedWindowLimiter
	slog     *slog.Logger
	metrics  *observability.Metrics
}
```

Initialize in `NewServer`:

```go
logger := options.Logger
if logger == nil {
	logger = slog.Default()
}
return &Server{
	...,
	slog:    logger,
	metrics: options.Metrics,
}
```

Remove the old `logWrapper` type and `loggerInterface` — they are replaced by slog.

- [ ] **Step 2: Update loggingMiddleware to use slog**

In `middleware.go`, replace:

```go
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		s.slog.Info("mcp request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"duration_ms", duration.Milliseconds(),
		)
	})
}
```

Remove `defaultLogger()` function and `"log"` import from middleware.go (if no longer needed).

- [ ] **Step 3: Record request metrics in handleRPC**

In `server.go`'s `handleRPC`, after computing `duration` and calling hooks:

```go
status := "ok"
if err != nil {
	status = "error"
}
s.metrics.RecordRequest(req.Method, status, duration.Seconds())
```

- [ ] **Step 4: Update slog usage in handleRPC for errors**

Replace any remaining `s.logger.Printf(...)` calls with `s.slog.Error(...)` or `s.slog.Info(...)`.

- [ ] **Step 5: Update server_test.go**

The `newTestServer` function creates `ServerOptions` — no change needed since `Metrics` and `Logger` are optional (nil-safe). Verify:

```bash
go test ./internal/mcp/ -v
```

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/
git commit -m "feat(mcp): integrate Prometheus metrics and slog structured logging"
```

---

## Chunk 5: Wire Everything in main.go

### Task 7: main.go creates and injects observability

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Create Metrics and Logger in runServer**

At the top of `runServer`, before other initialization:

```go
logger := observability.NewLogger(os.Stdout, false)
slog.SetDefault(logger)

promRegistry := prometheus.NewRegistry()
promRegistry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
metrics := observability.NewMetrics(promRegistry)
```

Import:
```go
"ai-agent-api/internal/observability"
"github.com/prometheus/client_golang/prometheus"
"github.com/prometheus/client_golang/prometheus/collectors"
"github.com/prometheus/client_golang/prometheus/promhttp"
```

- [ ] **Step 2: Inject into AgentEngine**

After creating `engine`:

```go
engine.SetMetrics(metrics)
```

- [ ] **Step 3: Inject into MCP Server**

Update `ServerOptions`:

```go
mcpServer := mcp.NewServer(cfg, registry, hooks, mcp.ServerOptions{
	RateLimitPerMinute: 120,
	Metrics:            metrics,
	Logger:             logger,
})
```

- [ ] **Step 4: Add /metrics endpoint**

In the `rootMux` setup:

```go
rootMux.Handle("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{}))
```

- [ ] **Step 5: Replace log.Printf calls in main.go with slog**

Replace all `log.Printf(...)` in main.go hooks and startup messages with `logger.Info(...)` / `logger.Error(...)`.

Replace `log.Fatalf(...)` in `main()` with `slog.Error(...)` + `os.Exit(1)`.

- [ ] **Step 6: Run all tests**

```bash
go test ./... 2>&1
```

Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: wire Prometheus metrics and slog logger in server startup"
```

---

### Task 8: Verify end-to-end

- [ ] **Step 1: Build the binary**

```bash
go build ./cmd/server/
```

Expected: Success.

- [ ] **Step 2: Run smoke test**

```bash
go test ./internal/e2e/ -v
```

Expected: All PASS.

- [ ] **Step 3: Manual verification (optional)**

Start the server and check `/metrics` returns Prometheus text format:

```bash
go run cmd/server/main.go run &
sleep 2
curl -s http://localhost:8080/metrics | head -20
kill %1
```

Expected: Output contains `mcp_requests_total`, `tool_calls_total`, `llm_requests_total` metric families.

- [ ] **Step 4: Final commit**

```bash
git add .
git commit -m "feat(observability): complete Prometheus metrics + slog structured logging integration"
```
