package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-agent-api/internal/observability"
	"golang.org/x/sync/errgroup"
)

const defaultSystemPrompt = "你是企业 API 助手，只能输出结构化汇总，不泄露原始内部数据。"

type ToolDispatcher interface {
	Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error)
	Has(name string) bool
}

type AgentEngine struct {
	llmClient     LLMClient
	dispatcher    ToolDispatcher
	dispatchFn    ToolHandler
	memory        Memory
	maxSteps      int
	systemPrompt  string
	toolCatalog   []ToolDefinition
	middlewares   []Middleware
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

type toolResult struct {
	callID  string
	content string
	trace   ToolTrace
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
	e.dispatchFn = e.dispatcher.Dispatch
	if len(e.middlewares) > 0 {
		e.dispatchFn = Chain(e.middlewares...)(e.dispatcher.Dispatch)
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

// Run 执行 agent 循环并返回汇总文本（包含工具调用轨迹）。
func (e *AgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
	summary, traces, err := e.RunWithTrace(ctx, userQuery)
	if err != nil {
		return "", err
	}
	return attachToolTraceSummary(summary, traces), nil
}

// RunWithTrace 执行 agent 循环并分别返回汇总文本和 trace。
func (e *AgentEngine) RunWithTrace(ctx context.Context, userQuery string) (string, []ToolTrace, error) {
	th := newTraceHandler()
	var h Handler = th
	if len(e.extraHandlers) > 0 {
		h = NewMultiHandler(append([]Handler{th}, e.extraHandlers...)...)
	}
	summary, err := e.runCore(ctx, userQuery, h)
	return summary, th.traces, err
}

// RunStream 以事件流方式执行 agent 循环。
func (e *AgentEngine) RunStream(ctx context.Context, userQuery string) <-chan AgentEvent {
	ch := make(chan AgentEvent, 32)
	go func() {
		defer close(ch)
		sh := newStreamHandler(ctx, ch)
		var h Handler = sh
		if len(e.extraHandlers) > 0 {
			h = NewMultiHandler(append([]Handler{sh}, e.extraHandlers...)...)
		}
		_, _ = e.runCore(ctx, userQuery, h)
	}()
	return ch
}

// runCore 是唯一的 agent 执行循环。
func (e *AgentEngine) runCore(ctx context.Context, userQuery string, h Handler) (string, error) {
	if h == nil {
		h = NoopHandler{}
	}

	mem := e.memory
	mem.Reset()
	mem.Append(Message{Role: "system", Content: e.systemPrompt})
	mem.Append(Message{Role: "user", Content: userQuery})

	for step := 1; step <= e.maxSteps; step++ {
		h.OnStepStart(ctx, step)

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
			wrapped := fmt.Errorf("llm next failed: %w", err)
			h.OnError(ctx, step, wrapped)
			return "", wrapped
		}
		h.OnLLMEnd(ctx, step, reply)

		if len(reply.ToolCalls) == 0 {
			content := reply.Content
			if content == "" {
				content = "结构化汇总结果为空。"
			}
			h.OnComplete(ctx, content, nil)
			return content, nil
		}

		mem.Append(Message{Role: "assistant", ToolCalls: reply.ToolCalls})

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
	}

	msg := fmt.Sprintf("agent stopped: reached max steps (%d)", e.maxSteps)
	h.OnComplete(ctx, msg, nil)
	return msg, nil
}

func (e *AgentEngine) dispatchTools(ctx context.Context, calls []ToolCall, step int, h Handler) []toolResult {
	results := make([]toolResult, len(calls))
	if len(calls) == 1 {
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
			trace: ToolTrace{
				Step:       step,
				Tool:       tc.Name,
				Success:    execErr == nil,
				DurationMS: duration.Milliseconds(),
				Preview:    shortPreview(content, 100),
			},
		}
		h.OnToolEnd(ctx, step, results[0].trace)
		return results
	}

	g, gctx := errgroup.WithContext(ctx)
	for i, tc := range calls {
		i, tc := i, tc
		h.OnToolStart(gctx, step, tc)
		g.Go(func() error {
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
				trace: ToolTrace{
					Step:       step,
					Tool:       tc.Name,
					Success:    execErr == nil,
					DurationMS: duration.Milliseconds(),
					Preview:    shortPreview(content, 100),
				},
			}
			return nil
		})
	}
	_ = g.Wait()
	for _, r := range results {
		h.OnToolEnd(ctx, step, r.trace)
	}
	return results
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
