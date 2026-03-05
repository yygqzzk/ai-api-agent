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

// Package agent 实现基于 ReAct (Reasoning + Acting) 模式的 AI Agent 引擎
//
// # 设计理念
//
// ReAct 模式将推理(Reasoning)和行动(Acting)交织在一起:
// 1. LLM 根据用户查询和历史对话,推理出需要调用哪些工具
// 2. Agent 执行工具调用,获取结果
// 3. 将工具结果反馈给 LLM,继续推理
// 4. 重复 1-3 直到 LLM 给出最终答案或达到最大步数
//
// # 核心设计模式
//
// 1. **Strategy Pattern (策略模式)** - LLMClient 接口
//   - 支持多种 LLM 实现 (OpenAI, RuleBased)
//   - 运行时可切换不同的推理策略
//
// 2. **Middleware Pattern (中间件模式)** - 工具调用链
//   - 支持在工具执行前后插入横切关注点 (日志、指标、缓存)
//   - 通过 Chain 函数组合多个中间件
//
// 3. **Observer Pattern (观察者模式)** - Handler 接口
//   - 支持多种执行模式 (同步、流式、追踪)
//   - 通过 MultiHandler 组合多个观察者
//
// # 并发优化
//
// 当 LLM 返回多个工具调用时,使用 errgroup 并发执行:
// - 单个工具调用: 直接执行,避免 goroutine 开销
// - 多个工具调用: 并发执行,显著降低延迟
//
// 示例: 3 个工具调用,每个耗时 100ms
// - 串行执行: 300ms
// - 并发执行: ~100ms (3x 加速)
//
// # 参考文献
//
// ReAct 论文: https://arxiv.org/abs/2210.03629
// "ReAct: Synergizing Reasoning and Acting in Language Models"

// defaultSystemPrompt 定义 Agent 的默认行为准则
// 强调结构化输出和数据安全,防止泄露内部敏感信息
const defaultSystemPrompt = "你是企业 API 助手，只能输出结构化汇总，不泄露原始内部数据。"

// ToolDispatcher 工具分发器接口
// 负责将工具名称和参数路由到具体的工具实现
//
// 设计考虑:
// - 使用接口而非具体类型,支持不同的工具注册机制
// - Has 方法用于在执行前验证工具是否存在,避免运行时错误
type ToolDispatcher interface {
	Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error)
	Has(name string) bool
}

// AgentEngine ReAct Agent 引擎
//
// 职责:
// 1. 管理 LLM 对话循环 (最多 maxSteps 步)
// 2. 调度工具执行 (支持并发)
// 3. 维护对话历史 (Memory)
// 4. 收集执行指标 (Metrics)
//
// 并发安全性:
// - AgentEngine 本身不是并发安全的,不应在多个 goroutine 中共享同一个实例
// - 但可以安全地并发创建多个 AgentEngine 实例处理不同的请求
// - 内部的 dispatchTools 方法使用 errgroup 实现工具的并发执行
//
// 性能考虑:
// - 单工具调用: 直接执行,避免 goroutine 开销
// - 多工具调用: 并发执行,利用 I/O 等待时间
type AgentEngine struct {
	llmClient     LLMClient                  // LLM 客户端 (Strategy Pattern)
	dispatcher    ToolDispatcher             // 工具分发器
	dispatchFn    ToolHandler                // 实际的工具调用函数 (可能被中间件包装)
	memory        Memory                     // 对话历史管理
	maxSteps      int                        // 最大执行步数 (防止无限循环)
	systemPrompt  string                     // 系统提示词
	toolCatalog   []ToolDefinition           // 工具目录 (传递给 LLM)
	middlewares   []Middleware               // 工具调用中间件链
	extraHandlers []Handler                  // 额外的事件处理器
	metrics       *observability.Metrics     // Prometheus 指标收集器
}

// ToolTrace 工具调用追踪信息
// 用于调试和性能分析
type ToolTrace struct {
	Step       int    // 执行步骤编号
	Tool       string // 工具名称
	Success    bool   // 是否成功
	DurationMS int64  // 执行耗时 (毫秒)
	Preview    string // 结果预览 (截断到 100 字符)
}

// toolResult 内部使用的工具执行结果
// 包含完整的结果内容和追踪信息
type toolResult struct {
	callID  string    // 工具调用 ID (用于关联 LLM 的 tool_call 和 tool 消息)
	content string    // 工具返回的 JSON 字符串
	trace   ToolTrace // 追踪信息
}

// NewAgentEngine 创建 Agent 引擎
//
// 参数:
// - llmClient: LLM 客户端实现
// - dispatcher: 工具分发器 (通常是 tools.Registry)
// - opts: 可选配置 (使用 Functional Options Pattern)
//
// 默认配置:
// - maxSteps: 10 (防止无限循环)
// - memory: BufferMemory(64) (保留最近 64 条消息)
// - systemPrompt: defaultSystemPrompt
//
// 中间件链构建:
// 如果提供了中间件,会通过 Chain 函数将它们组合成一个调用链
// 执行顺序: middleware1 -> middleware2 -> ... -> dispatcher.Dispatch
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

// SetToolCatalog 设置工具目录
// 工具目录会传递给 LLM,让 LLM 知道有哪些工具可用
//
// 注意: 传入 nil 或空切片会清空工具目录
func (e *AgentEngine) SetToolCatalog(catalog []ToolDefinition) {
	if len(catalog) == 0 {
		e.toolCatalog = nil
		return
	}
	e.toolCatalog = make([]ToolDefinition, len(catalog))
	copy(e.toolCatalog, catalog)
}

// modelName 获取 LLM 模型名称 (用于指标标签)
// 使用类型断言检查 LLMClient 是否实现了 Model() 方法
func (e *AgentEngine) modelName() string {
	if namer, ok := e.llmClient.(interface{ Model() string }); ok {
		return namer.Model()
	}
	return "unknown"
}

// recordMetrics 检查是否启用了指标收集
func (e *AgentEngine) recordMetrics() bool {
	return e.metrics != nil
}

// Run 执行 Agent 循环并返回汇总文本 (包含工具调用轨迹)
//
// 这是最常用的同步执行方法,适用于:
// - HTTP API 请求响应
// - CLI 工具
// - 批处理任务
//
// 返回格式:
// ```
// [LLM 生成的汇总文本]
//
// 工具调用轨迹:
// 1. step=1 tool=search_api status=ok latency=120ms preview={"results":[...]}
// 2. step=1 tool=get_api_detail status=ok latency=80ms preview={"endpoint":...}
// ```
func (e *AgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
	summary, traces, err := e.RunWithTrace(ctx, userQuery)
	if err != nil {
		return "", err
	}
	return attachToolTraceSummary(summary, traces), nil
}

// RunWithTrace 执行 Agent 循环并分别返回汇总文本和 trace
//
// 适用于需要单独处理追踪信息的场景:
// - 性能分析
// - 调试工具
// - 自定义日志格式
func (e *AgentEngine) RunWithTrace(ctx context.Context, userQuery string) (string, []ToolTrace, error) {
	th := newTraceHandler()
	var h Handler = th
	if len(e.extraHandlers) > 0 {
		h = NewMultiHandler(append([]Handler{th}, e.extraHandlers...)...)
	}
	summary, err := e.runCore(ctx, userQuery, h)
	return summary, th.traces, err
}

// RunStream 以事件流方式执行 Agent 循环
//
// 适用于需要实时反馈的场景:
// - SSE (Server-Sent Events) 流式响应
// - WebSocket 实时通信
// - 进度条显示
//
// 事件类型:
// - StepStart: 步骤开始
// - LLMStart/LLMEnd: LLM 调用开始/结束
// - ToolStart/ToolEnd: 工具调用开始/结束
// - Complete: 执行完成
// - Error: 执行失败
//
// 并发安全性:
// 返回的 channel 在 goroutine 中异步填充,调用者可以安全地在主 goroutine 中读取
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

// runCore 是唯一的 Agent 执行循环实现
//
// ReAct 循环流程:
// 1. 初始化 Memory,添加 system 和 user 消息
// 2. 循环 (最多 maxSteps 步):
//    a. 调用 LLM,获取推理结果
//    b. 如果 LLM 返回文本 (无工具调用),结束循环
//    c. 如果 LLM 返回工具调用,执行工具
//    d. 将工具结果添加到 Memory
//    e. 继续下一步
// 3. 如果达到 maxSteps,返回超时消息
//
// 错误处理:
// - LLM 调用失败: 立即返回错误
// - 工具不存在: 立即返回错误
// - 工具执行失败: 将错误编码为 JSON 返回给 LLM,让 LLM 决定如何处理
//
// 性能优化:
// - 单工具调用: 直接执行
// - 多工具调用: 并发执行 (见 dispatchTools)
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

// dispatchTools 调度工具执行 (支持并发优化)
//
// 并发策略:
// 1. **单工具调用**: 直接执行,避免 goroutine 开销
//    - 适用于大多数场景 (LLM 通常一次只调用一个工具)
//    - 零开销,无需创建 goroutine 和 channel
//
// 2. **多工具调用**: 使用 errgroup 并发执行
//    - 适用于 LLM 同时调用多个工具的场景
//    - 显著降低总延迟 (利用 I/O 等待时间)
//    - 使用 errgroup.WithContext 支持取消传播
//
// 性能分析:
// - 单工具: 直接调用,延迟 = 工具执行时间
// - 多工具并发: 延迟 ≈ max(工具执行时间),而非 sum(工具执行时间)
//
// 示例:
// - 3 个工具,每个 100ms
// - 串行: 300ms
// - 并发: ~100ms (3x 加速)
//
// 错误处理:
// - 工具执行失败不会中断其他工具
// - 所有工具都会执行完成
// - 错误会编码到结果中返回给 LLM
//

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

// encodeToolResult 将工具执行结果编码为 JSON 字符串
//
// 错误处理:
// - 如果工具执行失败,返回 {"error": "错误消息"}
// - 如果结果序列化失败,返回 {"error": "marshal tool result failed: ..."}
//
// 设计考虑:
// - 始终返回有效的 JSON 字符串,即使发生错误
// - LLM 可以解析错误信息并决定如何处理
//

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

// attachToolTraceSummary 将工具调用轨迹附加到汇总文本
//
// 格式:
// ```
// [汇总文本]
//
// 工具调用轨迹:
// 1. step=1 tool=search_api status=ok latency=120ms preview={"results":[...]}
// 2. step=1 tool=get_api_detail status=ok latency=80ms preview={"endpoint":...}
// ```
//
// 用途:
// - 调试: 查看工具调用顺序和耗时
// - 性能分析: 识别慢工具
// - 审计: 记录 Agent 的行为
//

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

// shortPreview 生成结果的简短预览
//
// 处理:
// - 去除首尾空白
// - 将换行符替换为空格
// - 截断到指定长度
//
// 用途:
// - 在日志中显示工具结果的摘要
// - 避免日志过长影响可读性
//

func shortPreview(s string, max int) string {
	flat := strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if max <= 0 || len(flat) <= max {
		return flat
	}
	return flat[:max] + "..."
}
