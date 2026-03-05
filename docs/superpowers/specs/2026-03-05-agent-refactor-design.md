# Agent 模块架构重构 + 智能增强设计文档

> 日期: 2026-03-05
> 状态: 设计中

## 1. 背景与动机

当前 `internal/agent/` 模块实现了一个基于 tool-calling 的 agent 引擎，但存在以下痛点：

1. **代码重复**：`RunWithTrace` 和 `runStreamInternal` 共享 ~80% 的循环逻辑（system prompt 注入、LLM 调用、工具分发、上下文管理），任何修改需要同步两处。
2. **横切关注点耦合**：metrics 记录、日志、重试逻辑直接嵌入 engine 核心循环，难以独立测试和扩展。
3. **上下文管理粗糙**：`ContextManager` 按消息数裁剪，不考虑 token 量。长工具结果可能导致上下文超出模型限制。
4. **工具调用无容错**：工具失败后错误信息直接返回 LLM，没有重试、降级或结构化提示机制。
5. **工具串行执行**：LLM 一次返回多个 `tool_calls` 时逐个执行，浪费延迟。
6. **配置硬编码**：system prompt、maxMessages 等值散布在代码中。

## 2. 设计目标

- 消除 RunWithTrace/RunStream 的代码重复
- 横切关注点（日志、metrics、重试、缓存）通过中间件/回调解耦
- 上下文管理支持 token 级别控制
- 工具调用支持并行执行和错误恢复
- AgentEngine 构造支持 Functional Options 模式
- 保持 Go 惯用风格，不过度抽象

## 3. 核心设计

### 3.1 中间件模式（Tool Dispatch 层）

**借鉴**：Semantic Kernel Filters、Go HTTP middleware

在 `ToolDispatcher` 和实际工具之间引入中间件链，用于注入重试、缓存、日志等横切关注点。

```go
// middleware.go

// ToolHandler 是工具调用的原子函数签名
type ToolHandler func(ctx context.Context, name string, args json.RawMessage) (any, error)

// Middleware 包装 ToolHandler，添加横切逻辑
type Middleware func(next ToolHandler) ToolHandler

// Chain 将多个中间件按顺序组合
// 执行顺序：第一个中间件最外层（最先进入、最后退出）
func Chain(middlewares ...Middleware) Middleware {
    return func(final ToolHandler) ToolHandler {
        for i := len(middlewares) - 1; i >= 0; i-- {
            final = middlewares[i](final)
        }
        return final
    }
}
```

#### 内置中间件

**RetryMiddleware** — 工具调用失败时指数退避重试：

```go
type RetryConfig struct {
    MaxAttempts int           // 最大重试次数，默认 2
    BaseDelay   time.Duration // 基础延迟，默认 200ms
    MaxDelay    time.Duration // 最大延迟，默认 5s
}

func RetryMiddleware(cfg RetryConfig) Middleware
```

判断可重试性：工具返回 error 时默认可重试，除非 error 实现 `interface{ Permanent() bool }` 且返回 true。

**LoggingMiddleware** — 结构化日志记录工具调用：

```go
func LoggingMiddleware(logger *slog.Logger) Middleware
```

记录：tool name、args（截断）、耗时、成功/失败、结果预览。

**MetricsMiddleware** — Prometheus metrics：

```go
func MetricsMiddleware(metrics *observability.Metrics) Middleware
```

从 engine 循环中移出 `RecordToolCall`，放入中间件。

#### 集成方式

`AgentEngine` 持有 `dispatchHandler ToolHandler`（已经包装好中间件的函数），不再直接持有 `ToolDispatcher`：

```go
type AgentEngine struct {
    llmClient      LLMClient
    dispatchFn     ToolHandler     // 已包装中间件的分发函数
    dispatcher     ToolDispatcher  // 保留用于 Has() 检查
    // ...
}
```

### 3.2 Observer/Callback 系统

**借鉴**：LangChain BaseCallbackHandler

定义 `Handler` 接口，统一 RunWithTrace 和 RunStream 的输出方式。

```go
// handler.go

type Handler interface {
    OnStepStart(ctx context.Context, step int)
    OnLLMStart(ctx context.Context, step int)
    OnLLMEnd(ctx context.Context, step int, reply LLMReply)
    OnToolStart(ctx context.Context, step int, call ToolCall)
    OnToolEnd(ctx context.Context, step int, trace ToolTrace)
    OnComplete(ctx context.Context, summary string, traces []ToolTrace)
    OnError(ctx context.Context, step int, err error)
}

// NoopHandler 提供默认空实现，具体 handler 只需嵌入并覆盖关心的方法
type NoopHandler struct{}

// MultiHandler 组合多个 handler，逐一调用
type MultiHandler struct {
    handlers []Handler
}
```

#### 具体 Handler 实现

**traceHandler** — 收集 ToolTrace 切片，替代 RunWithTrace 中的内联逻辑：

```go
type traceHandler struct {
    NoopHandler
    traces []ToolTrace
}
```

**streamHandler** — 往 channel 发送 AgentEvent，替代 runStreamInternal 中的 emit 函数：

```go
type streamHandler struct {
    NoopHandler
    ch  chan<- AgentEvent
    ctx context.Context
}
```

**metricsHandler** — 记录 LLM 请求指标，从 engine 循环中移出：

```go
type metricsHandler struct {
    NoopHandler
    metrics   *observability.Metrics
    modelName string
}
```

#### 统一执行核心

`runCore` 接受 Handler 参数，消除重复：

```go
func (e *AgentEngine) runCore(ctx context.Context, userQuery string, h Handler) (string, []ToolTrace, error) {
    // 唯一的循环实现
    // 通过 h.OnXxx() 回调通知外部
}

func (e *AgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
    th := &traceHandler{}
    summary, traces, err := e.runCore(ctx, userQuery, th)
    // ...
}

func (e *AgentEngine) RunStream(ctx context.Context, userQuery string) <-chan AgentEvent {
    ch := make(chan AgentEvent, 32)
    go func() {
        defer close(ch)
        sh := &streamHandler{ch: ch, ctx: ctx}
        e.runCore(ctx, userQuery, sh)
    }()
    return ch
}
```

### 3.3 Memory 接口化

**借鉴**：LangChain ConversationBufferMemory / ConversationTokenBufferMemory

将 `ContextManager` 提升为 `Memory` 接口，支持多种策略。

```go
// memory.go

type Memory interface {
    // Append 添加消息到记忆
    Append(msg Message)
    // Messages 返回当前消息列表（用于传递给 LLM）
    Messages() []Message
    // Reset 清空记忆
    Reset()
}
```

#### TokenEstimator

```go
// 估算文本的 token 数量
type TokenEstimator func(text string) int

// DefaultTokenEstimator 按字符数/4 粗略估算（适用于英文/中文混合场景）
func DefaultTokenEstimator(text string) int {
    // 中文字符按 1.5 token 估算，ASCII 按 0.25 token
    // 简化实现：len([]rune(text)) 作为近似值
}
```

#### 实现

**BufferMemory** — 当前 ContextManager 的行为，按消息数裁剪：

```go
type BufferMemory struct {
    maxMessages int
    messages    []Message
    mu          sync.RWMutex
}
```

保留 system message + 最新 N 条。与当前行为一致。

**TokenWindowMemory** — 按 token 估算裁剪：

```go
type TokenWindowMemory struct {
    maxTokens int
    estimator TokenEstimator
    messages  []Message
    mu        sync.RWMutex
}
```

裁剪策略：
1. 始终保留第一条 system message
2. 始终保留最后一条 user message
3. 从最旧的非保护消息开始删除，直到总 token 数 <= maxTokens
4. 工具结果消息若单条超过 maxTokens/4，先截断到该阈值

### 3.4 并行工具执行

当 LLM 一次返回多个 `tool_calls` 时，用 `errgroup` 并行执行。

```go
// engine.go runCore 内部

if len(reply.ToolCalls) > 1 {
    results, err := e.dispatchParallel(ctx, reply.ToolCalls, step, h)
    // ...
} else {
    // 单个工具调用走简单路径
}

func (e *AgentEngine) dispatchParallel(
    ctx context.Context,
    calls []ToolCall,
    step int,
    h Handler,
) ([]toolResult, error) {
    type toolResult struct {
        index   int
        callID  string
        content string
        trace   ToolTrace
    }

    results := make([]toolResult, len(calls))
    g, gctx := errgroup.WithContext(ctx)

    for i, tc := range calls {
        g.Go(func() error {
            h.OnToolStart(gctx, step, tc)
            start := time.Now()
            out, err := e.dispatchFn(gctx, tc.Name, tc.Args)
            duration := time.Since(start)
            content := encodeToolResult(out, err)
            results[i] = toolResult{
                index:   i,
                callID:  tc.ID,
                content: content,
                trace:   ToolTrace{Step: step, Tool: tc.Name, Success: err == nil, DurationMS: duration.Milliseconds(), Preview: shortPreview(content, 100)},
            }
            h.OnToolEnd(gctx, step, results[i].trace)
            return nil // 不中断其他并行调用
        })
    }
    g.Wait()
    return results, nil
}
```

注意：工具错误不会终止并行组，而是记录在结果中返回给 LLM。

### 3.5 Functional Options 构造

**借鉴**：Go Functional Options Pattern

```go
// options.go

type Option func(*AgentEngine)

func WithMaxSteps(n int) Option {
    return func(e *AgentEngine) { e.maxSteps = n }
}

func WithSystemPrompt(prompt string) Option {
    return func(e *AgentEngine) { e.systemPrompt = prompt }
}

func WithMemory(m Memory) Option {
    return func(e *AgentEngine) { e.memory = m }
}

func WithMiddleware(mws ...Middleware) Option {
    return func(e *AgentEngine) { e.middlewares = append(e.middlewares, mws...) }
}

func WithHandlers(hs ...Handler) Option {
    return func(e *AgentEngine) { e.handlers = append(e.handlers, hs...) }
}

func WithMetrics(m *observability.Metrics) Option {
    return func(e *AgentEngine) { e.metrics = m }
}

func NewAgentEngine(llmClient LLMClient, dispatcher ToolDispatcher, opts ...Option) *AgentEngine {
    e := &AgentEngine{
        llmClient:    llmClient,
        dispatcher:   dispatcher,
        maxSteps:     10,
        systemPrompt: "你是企业 API 助手，只能输出结构化汇总，不泄露原始内部数据。",
        memory:       NewBufferMemory(64),
    }
    for _, opt := range opts {
        opt(e)
    }
    // 组装中间件链
    e.dispatchFn = Chain(e.middlewares...)(dispatcher.Dispatch)
    return e
}
```

### 3.6 重构后的 AgentEngine 结构

```go
type AgentEngine struct {
    llmClient    LLMClient
    dispatcher   ToolDispatcher  // 保留用于 Has() 检查
    dispatchFn   ToolHandler     // 中间件包装后的分发函数
    memory       Memory          // 替代原 ContextManager
    maxSteps     int
    systemPrompt string
    toolCatalog  []ToolDefinition
    middlewares  []Middleware
    handlers     []Handler       // 全局 handler（metrics 等）
    metrics      *observability.Metrics
}
```

## 4. 文件结构

重构后的 `internal/agent/` 目录：

```
internal/agent/
├── engine.go           # AgentEngine 核心 + runCore 统一循环
├── options.go          # Functional Options
├── handler.go          # Handler 接口 + NoopHandler + MultiHandler
├── handler_trace.go    # traceHandler 实现
├── handler_stream.go   # streamHandler 实现
├── handler_metrics.go  # metricsHandler 实现
├── memory.go           # Memory 接口 + BufferMemory
├── memory_token.go     # TokenWindowMemory + TokenEstimator
├── middleware.go       # Middleware 类型 + Chain 函数
├── middleware_retry.go # RetryMiddleware
├── middleware_log.go   # LoggingMiddleware
├── llm.go              # LLMClient 接口 + Message/ToolCall 类型 + RuleBasedLLMClient
├── openai_llm.go       # OpenAICompatibleLLMClient（不变）
├── event.go            # AgentEvent 类型定义（不变）
├── engine_test.go      # 更新测试
├── memory_test.go      # Memory 实现测试
├── middleware_test.go  # 中间件测试
├── handler_test.go     # Handler 测试
├── openai_llm_test.go  # 不变
└── openai_engine_integration_test.go  # 不变
```

## 5. 迁移策略

### 阶段 1: 基础重构（不改变外部行为）
1. 提取 `Memory` 接口 + `BufferMemory`（直接替换 ContextManager）
2. 提取 `Handler` 接口 + `traceHandler` + `streamHandler`
3. 合并 `RunWithTrace` 和 `runStreamInternal` 为 `runCore`
4. 引入 Functional Options 构造函数
5. 更新现有测试

### 阶段 2: 中间件系统
1. 实现 `Middleware` 类型 + `Chain`
2. 实现 `RetryMiddleware` + `LoggingMiddleware`
3. 将 metrics 记录从 engine 循环移到 `MetricsMiddleware`
4. 更新 `cmd/server/main.go` 的 engine 构造

### 阶段 3: 智能增强
1. 实现 `TokenWindowMemory`
2. 实现并行工具执行
3. 实现结构化错误反馈（工具失败时向 LLM 提供重试建议）
4. 添加 `metricsHandler`

## 6. 与现有代码的影响

### 需要更新的外部调用方
- `cmd/server/main.go` — `NewAgentEngine` 签名变更，改用 Options
- `internal/tools/query_api.go` — 使用 `AgentEngine` 的方式可能需要微调
- 测试文件中的 mock 构造需要适配新签名

### 不受影响的模块
- `internal/mcp/` — 不直接依赖 agent 内部结构
- `internal/rag/` — 完全独立
- `internal/tools/`（除 query_api） — 通过 `ToolDispatcher` 接口隔离

## 7. 成功标准

- [ ] `RunWithTrace` 和 `RunStream` 共享同一个 `runCore` 循环
- [ ] 工具调用中间件可独立测试
- [ ] `go test ./internal/agent/... -v` 全部通过
- [ ] `go build ./...` 编译通过
- [ ] Memory 接口支持切换策略而不改 engine
- [ ] 多个 tool_calls 并行执行时总耗时接近最慢的单个工具
