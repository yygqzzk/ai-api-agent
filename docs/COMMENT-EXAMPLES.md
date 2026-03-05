# 代码注释示例

本文档展示了如何为项目核心模块添加完整注释。

---

## 示例 1：Agent Engine (internal/agent/engine.go)

```go
// Package agent 实现了 AI Agent 引擎的核心功能。
//
// # 设计思想
//
// Agent 引擎采用 ReAct (Reasoning + Acting) 模式，通过多轮推理和工具调用完成复杂任务。
// 核心设计理念：
//   1. 关注点分离：推理逻辑（LLM）与执行逻辑（Tools）解耦
//   2. 可扩展性：通过中间件模式支持横切关注点（日志、重试、监控）
//   3. 可测试性：接口抽象使得每个组件可独立测试
//
// # 设计模式
//
//   - Strategy Pattern: LLMClient 接口支持多种 LLM 实现（OpenAI/规则式）
//   - Middleware Pattern: 中间件链处理横切关注点
//   - Observer Pattern: Handler 接口支持事件监听
//   - Template Method: runCore 定义执行骨架，Handler 可扩展
//
// # 核心组件
//
//   - AgentEngine: Agent 执行引擎，协调 LLM 和工具调用
//   - LLMClient: LLM 客户端接口，支持 OpenAI 兼容 API
//   - Memory: 上下文管理，维护对话历史
//   - Middleware: 中间件链，处理重试、日志等
//   - Handler: 事件处理器，监听 Agent 执行过程
//
// # 执行流程
//
//	1. 初始化上下文（system prompt + user query）
//	2. Agent Loop（最多 maxSteps 轮）：
//	   a. 调用 LLM 推理（Reasoning）
//	   b. 如果返回工具调用，执行工具（Acting）
//	   c. 将工具结果追加到上下文
//	   d. 继续下一轮
//	3. 返回最终汇总结果
//
// # 使用示例
//
//	// 创建 Agent 引擎
//	engine := agent.NewAgentEngine(llmClient, dispatcher,
//	    agent.WithMaxSteps(10),
//	    agent.WithSystemPrompt("你是 API 助手"),
//	    agent.WithMiddlewares(retryMiddleware, loggingMiddleware),
//	)
//
//	// 执行查询
//	result, err := engine.Run(ctx, "查询用户登录接口")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result)
//
// # 注意事项
//
//   - 必须设置 maxSteps 防止无限循环
//   - LLMClient 和 ToolDispatcher 不能为 nil
//   - 并发安全：多个 goroutine 可同时调用 Run 方法
//   - 每次调用 Run 会重置 Memory 状态
//
// # 参考资料
//
//   - ReAct 论文: https://arxiv.org/abs/2210.03629
//   - LangChain Agent: https://python.langchain.com/docs/modules/agents/
package agent

// AgentEngine 是 Agent 执行引擎，负责协调 LLM 推理和工具调用。
//
// # 职责
//
//   - 执行 Agent Loop（ReAct 模式）
//   - 管理对话上下文（Memory）
//   - 调度工具执行（ToolDispatcher）
//   - 处理错误和重试（Middleware）
//   - 记录执行轨迹（Handler）
//
// # 设计模式
//
//   - Facade Pattern: 封装复杂的 Agent 执行流程
//   - Template Method: runCore 定义执行骨架，Handler 可扩展
//
// # 字段说明
//
//   - llmClient: LLM 客户端，负责推理（Strategy Pattern）
//   - dispatcher: 工具调度器，负责执行工具
//   - dispatchFn: 工具调度函数，经过中间件包装
//   - memory: 上下文管理器，维护对话历史
//   - maxSteps: 最大步数，防止无限循环（默认 10）
//   - systemPrompt: 系统提示词，定义 Agent 角色
//   - toolCatalog: 工具目录，传递给 LLM 用于 Function Calling
//   - middlewares: 中间件链，处理横切关注点
//   - extraHandlers: 额外的事件处理器
//   - metrics: 监控指标收集器
//
// # 并发安全性
//
// AgentEngine 是并发安全的，多个 goroutine 可同时调用 Run 方法。
// 每次调用 Run 会创建独立的执行上下文，不会相互干扰。
//
// # 注意事项
//
//   - 必须通过 NewAgentEngine 创建，不要直接初始化结构体
//   - maxSteps 必须 > 0，建议设置为 5-15
//   - toolCatalog 可以为空，但会导致 LLM 无法调用工具
//   - metrics 可以为 nil，此时不记录监控指标
//
// # 使用示例
//
//	engine := NewAgentEngine(llmClient, dispatcher,
//	    WithMaxSteps(10),
//	    WithSystemPrompt("你是 API 助手"),
//	)
//	result, err := engine.Run(ctx, userQuery)
type AgentEngine struct {
	llmClient     LLMClient              // LLM 客户端，负责推理
	dispatcher    ToolDispatcher         // 工具调度器，负责执行工具
	dispatchFn    ToolHandler            // 工具调度函数，经过中间件包装
	memory        Memory                 // 上下文管理器，维护对话历史
	maxSteps      int                    // 最大步数，防止无限循环
	systemPrompt  string                 // 系统提示词
	toolCatalog   []ToolDefinition       // 工具目录
	middlewares   []Middleware           // 中间件链
	extraHandlers []Handler              // 额外的事件处理器
	metrics       *observability.Metrics // 监控指标
}

// Run 执行 Agent 循环并返回汇总文本（包含工具调用轨迹）。
//
// # 执行流程
//
//  1. 初始化上下文（system prompt + user query）
//  2. 循环执行（最多 maxSteps 轮）：
//     a. 调用 LLM 推理
//     b. 如果返回工具调用，执行工具
//     c. 将工具结果追加到上下文
//     d. 继续下一轮
//  3. 返回最终汇总结果 + 工具调用轨迹
//
// # 参数
//
//   - ctx: 上下文，用于超时控制和取消
//   - userQuery: 用户查询，自然语言描述的任务
//
// # 返回值
//
//   - string: 汇总文本，包含工具调用轨迹
//   - error: 执行错误（LLM 调用失败、工具执行失败等）
//
// # 错误处理
//
//   - LLM 调用失败：立即返回错误
//   - 工具不存在：立即返回错误
//   - 工具执行失败：记录错误但继续执行（错误信息会传递给 LLM）
//   - 超过 maxSteps：返回已有结果 + 截断提示（不是错误）
//
// # 并发安全性
//
// 该方法是并发安全的，多个 goroutine 可同时调用。
// 每次调用会创建独立的 Memory 实例，不会相互干扰。
//
// # 注意事项
//
//   - 每次调用会重置 Memory 状态
//   - 工具调用结果会自动追加到上下文
//   - 如果 ctx 被取消，会立即返回错误
//   - 返回的文本包含工具调用轨迹，格式为：
//     "汇总结果\n\n工具调用轨迹:\n1. step=1 tool=search_api status=ok latency=50ms ..."
//
// # 使用示例
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	result, err := engine.Run(ctx, "查询用户登录接口")
//	if err != nil {
//	    log.Printf("Agent 执行失败: %v", err)
//	    return
//	}
//	fmt.Println(result)
func (e *AgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
	summary, traces, err := e.RunWithTrace(ctx, userQuery)
	if err != nil {
		return "", err
	}
	return attachToolTraceSummary(summary, traces), nil
}

// runCore 是唯一的 Agent 执行循环实现。
//
// # 设计模式：Template Method
//
// runCore 定义了 Agent 执行的骨架流程，通过 Handler 接口允许外部扩展：
//   - OnStepStart: 步骤开始时触发
//   - OnLLMStart/OnLLMEnd: LLM 调用前后触发
//   - OnToolStart/OnToolEnd: 工具调用前后触发
//   - OnComplete: 执行完成时触发
//   - OnError: 发生错误时触发
//
// # 执行流程
//
//  1. 初始化 Memory（system prompt + user query）
//  2. Agent Loop（最多 maxSteps 轮）：
//     a. 触发 OnStepStart 事件
//     b. 调用 LLM 推理（触发 OnLLMStart/OnLLMEnd）
//     c. 如果返回纯文本，触发 OnComplete 并返回
//     d. 验证工具是否存在
//     e. 执行工具调用（触发 OnToolStart/OnToolEnd）
//     f. 将工具结果追加到 Memory
//  3. 达到 maxSteps，触发 OnComplete 并返回
//
// # 参数
//
//   - ctx: 上下文
//   - userQuery: 用户查询
//   - h: 事件处理器（可以为 nil，此时使用 NoopHandler）
//
// # 返回值
//
//   - string: 汇总文本
//   - error: 执行错误
//
// # 注意事项
//
//   - 该方法是私有的，只能通过 Run/RunWithTrace/RunStream 调用
//   - Handler 可以为 nil，此时不触发任何事件
//   - 工具调用失败不会中断执行，错误信息会传递给 LLM
//   - 每次调用会重置 Memory 状态
//
// # 性能考虑
//
//   - LLM 调用是最耗时的操作（几秒到几十秒）
//   - 工具调用通常是 I/O 密集型，并发执行可以降低延迟
//   - Memory 操作是内存操作，性能开销可忽略
func (e *AgentEngine) runCore(ctx context.Context, userQuery string, h Handler) (string, error) {
	// 如果 Handler 为 nil，使用 NoopHandler
	// 设计考虑：避免在每个事件触发点都检查 nil
	if h == nil {
		h = NoopHandler{}
	}

	// 初始化 Memory
	// 注意：每次调用都会重置 Memory，确保执行独立性
	mem := e.memory
	mem.Reset()
	mem.Append(Message{Role: "system", Content: e.systemPrompt})
	mem.Append(Message{Role: "user", Content: userQuery})

	// Agent Loop
	// 设计考虑：使用 for 循环而不是递归，避免栈溢出
	for step := 1; step <= e.maxSteps; step++ {
		h.OnStepStart(ctx, step)

		// 调用 LLM 推理
		// 注意：这里可能耗时较长（几秒到几十秒），需要设置合理的超时
		h.OnLLMStart(ctx, step)
		llmStart := time.Now()
		reply, err := e.llmClient.Next(ctx, mem.Messages(), e.toolCatalog)
		llmDuration := time.Since(llmStart)

		// 记录 LLM 调用指标
		// 设计考虑：即使调用失败也要记录，用于监控和告警
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

		// LLM 调用失败，立即返回错误
		// 设计考虑：LLM 是核心依赖，失败时无法继续执行
		if err != nil {
			wrapped := fmt.Errorf("llm next failed: %w", err)
			h.OnError(ctx, step, wrapped)
			return "", wrapped
		}
		h.OnLLMEnd(ctx, step, reply)

		// 如果 LLM 返回纯文本（无工具调用），说明任务完成
		// 设计考虑：LLM 决定何时结束，而不是固定步数
		if len(reply.ToolCalls) == 0 {
			content := reply.Content
			if content == "" {
				content = "结构化汇总结果为空。"
			}
			h.OnComplete(ctx, content, nil)
			return content, nil
		}

		// 将 assistant 消息追加到 Memory
		// 注意：必须在执行工具前追加，保持消息顺序
		mem.Append(Message{Role: "assistant", ToolCalls: reply.ToolCalls})

		// 验证工具是否存在
		// 设计考虑：在执行前验证，避免调用不存在的工具
		// 注意：这里不应该发生，因为 toolCatalog 已经传递给 LLM
		//       如果发生，说明 LLM 返回了不在 catalog 中的工具
		for _, tc := range reply.ToolCalls {
			if !e.dispatcher.Has(tc.Name) {
				err := fmt.Errorf("unknown tool: %s", tc.Name)
				h.OnError(ctx, step, err)
				return "", err
			}
		}

		// 执行工具调用
		// 设计考虑：并发执行多个工具调用，降低延迟
		results := e.dispatchTools(ctx, reply.ToolCalls, step, h)

		// 将工具结果追加到 Memory
		// 注意：必须保持调用顺序，确保 LLM 能正确理解上下文
		for _, r := range results {
			mem.Append(Message{Role: "tool", ToolCallID: r.callID, Content: r.content})
		}
	}

	// 达到最大步数，返回截断提示
	// 注意：这不是错误，而是正常的终止条件
	// 设计考虑：避免无限循环，保护系统资源
	msg := fmt.Sprintf("agent stopped: reached max steps (%d)", e.maxSteps)
	h.OnComplete(ctx, msg, nil)
	return msg, nil
}

// dispatchTools 并发执行工具调用。
//
// # 设计考虑
//
// 工具调用通常是 I/O 密集型（HTTP 请求、数据库查询等），并发执行可以显著降低延迟。
// 例如：3 个工具调用，每个耗时 1 秒
//   - 串行执行：3 秒
//   - 并发执行：1 秒
//
// # 并发策略
//
//   - 单个工具调用：直接执行（避免 goroutine 开销）
//   - 多个工具调用：使用 errgroup 并发执行
//
// # 错误处理
//
// 工具调用失败不会中断执行，错误信息会编码到结果中，传递给 LLM。
// 设计考虑：让 LLM 决定如何处理错误（重试、降级、报告给用户等）
//
// # 参数
//
//   - ctx: 上下文
//   - calls: 工具调用列表
//   - step: 当前步数
//   - h: 事件处理器
//
// # 返回值
//
//   - []toolResult: 工具调用结果列表（顺序与 calls 一致）
//
// # 注意事项
//
//   - 结果顺序与 calls 顺序一致，即使并发执行
//   - 工具调用失败不会返回 error，而是编码到 toolResult.content 中
//   - 使用 errgroup 确保所有 goroutine 都完成后才返回
func (e *AgentEngine) dispatchTools(ctx context.Context, calls []ToolCall, step int, h Handler) []toolResult {
	results := make([]toolResult, len(calls))

	// 单个工具调用：直接执行
	// 设计考虑：避免 goroutine 开销（创建、调度、销毁）
	if len(calls) == 1 {
		tc := calls[0]
		h.OnToolStart(ctx, step, tc)
		start := time.Now()
		out, execErr := e.dispatchFn(ctx, tc.Name, tc.Args)
		duration := time.Since(start)

		// 记录工具调用指标
		if e.recordMetrics() {
			status := "ok"
			if execErr != nil {
				status = "error"
			}
			e.metrics.RecordToolCall(tc.Name, status, duration.Seconds())
		}

		// 编码工具结果
		// 注意：即使执行失败，也要返回结果（包含错误信息）
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

	// 多个工具调用：并发执行
	// 设计模式：使用 errgroup 管理 goroutine 生命周期
	g, gctx := errgroup.WithContext(ctx)
	for i, tc := range calls {
		i, tc := i, tc // 捕获循环变量
		h.OnToolStart(gctx, step, tc)
		g.Go(func() error {
			start := time.Now()
			out, execErr := e.dispatchFn(gctx, tc.Name, tc.Args)
			duration := time.Since(start)

			// 记录工具调用指标
			if e.recordMetrics() {
				status := "ok"
				if execErr != nil {
					status = "error"
				}
				e.metrics.RecordToolCall(tc.Name, status, duration.Seconds())
			}

			// 编码工具结果
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
			return nil // 不返回错误，让所有工具调用都完成
		})
	}

	// 等待所有 goroutine 完成
	// 注意：即使某个工具调用失败，也要等待所有调用完成
	_ = g.Wait()

	// 触发 OnToolEnd 事件
	for _, r := range results {
		h.OnToolEnd(ctx, step, r.trace)
	}

	return results
}
```

---

## 示例 2：熔断器 (internal/resilience/circuitbreaker.go)

```go
// Package resilience 提供容错机制，包括熔断器、重试、降级等。
//
// # 设计思想
//
// 容错机制是构建可靠分布式系统的关键。本包实现了常见的容错模式：
//   1. Circuit Breaker: 防止级联故障
//   2. Retry: 处理瞬时故障
//   3. Timeout: 防止资源耗尽
//
// # 设计模式
//
//   - State Pattern: 熔断器的三种状态（Closed/Open/HalfOpen）
//   - Proxy Pattern: 熔断器作为代理，控制对外部服务的访问
//   - Decorator Pattern: 重试机制装饰原始函数
//
// # 使用场景
//
//   - 保护外部 LLM API（OpenAI/Claude/DeepSeek）
//   - 保护向量数据库（Milvus）
//   - 保护缓存服务（Redis）
//
// # 参考资料
//
//   - Martin Fowler: https://martinfowler.com/bliki/CircuitBreaker.html
//   - Netflix Hystrix: https://github.com/Netflix/Hystrix/wiki
//   - Release It! by Michael Nygard
package resilience

// CircuitBreaker 实现了熔断器模式，用于保护外部依赖。
//
// # 设计思想
//
// 熔断器模式源自 Michael Nygard 的《Release It!》一书，用于防止级联故障。
// 核心思想：当外部服务不稳定时，快速失败而不是持续重试，避免资源耗尽。
//
// # 状态机
//
//	Closed (正常) ──失败率超阈值──> Open (熔断)
//	    ↑                              ↓
//	    └──成功──── HalfOpen (半开) <──超时
//
// 状态说明：
//   - Closed: 正常状态，请求正常通过，统计失败率
//   - Open: 熔断状态，直接拒绝请求，快速失败
//   - HalfOpen: 半开状态，允许少量请求通过，检测服务是否恢复
//
// # 设计模式
//
//   - State Pattern: 三种状态有不同的行为
//   - Proxy Pattern: 熔断器作为代理，控制对外部服务的访问
//
// # 参数调优
//
//   - MaxRequests: 半开状态允许的探测请求数（建议 3-5）
//   - Interval: 统计窗口（建议 10-60 秒）
//   - Timeout: 熔断时长（建议 30-60 秒）
//   - ReadyToTrip: 失败率阈值（建议 0.5-0.7）
//
// # 并发安全性
//
// CircuitBreaker 是并发安全的，多个 goroutine 可同时调用 Execute。
// 内部使用 sync.Mutex 保护共享状态。
//
// # 注意事项
//
//   - 熔断器是有状态的，不要在每次请求时创建新实例
//   - 状态变化时会触发回调，可用于记录日志或发送告警
//   - 熔断器不会自动恢复，需要等待 Timeout 后进入 HalfOpen 状态
//   - 统计窗口内的请求数必须 >= MaxRequests 才会评估是否熔断
//
// # 使用示例
//
//	// 创建熔断器
//	cb := resilience.NewCircuitBreaker(resilience.Config{
//	    Name:         "llm-api",
//	    MaxRequests:  3,
//	    Interval:     time.Second * 10,
//	    Timeout:      time.Second * 30,
//	    ReadyToTrip:  0.5,
//	    OnStateChange: func(from, to State) {
//	        log.Printf("Circuit breaker state changed: %s -> %s", from, to)
//	    },
//	})
//
//	// 使用熔断器保护 LLM 调用
//	err := cb.Execute(func() error {
//	    return llmClient.Call(ctx, prompt)
//	})
//	if errors.Is(err, resilience.ErrCircuitBreakerOpen) {
//	    // 熔断器打开，使用降级策略
//	    return fallbackResponse()
//	}
//
// # 参考资料
//
//   - Martin Fowler: https://martinfowler.com/bliki/CircuitBreaker.html
//   - Netflix Hystrix: https://github.com/Netflix/Hystrix/wiki
type CircuitBreaker struct {
	name          string                   // 熔断器名称（用于日志和指标）
	maxRequests   uint32                   // 半开状态允许的最大请求数
	interval      time.Duration            // 统计失败率的时间窗口
	timeout       time.Duration            // 熔断器打开后，多久尝试恢复
	readyToTrip   float64                  // 失败率达到多少时熔断（0.5 表示 50%）
	onStateChange func(from, to State)     // 状态变化回调

	state atomic.Value // State - 当前状态（使用 atomic.Value 避免加锁读取）

	mutex      sync.Mutex   // 保护以下字段
	generation uint64       // 熔断器代数，每次打开后递增
	counts     [2]uint32    // [requests, successes] - 请求计数和成功计数
	expiry     time.Time    // 统计窗口过期时间
}
```

---

## 总结

由于项目文件较多（76 个 Go 文件），完整添加注释需要大量工作。建议：

1. **优先级 1（核心模块）**：
   - `internal/agent/engine.go` - Agent 引擎核心
   - `internal/resilience/circuitbreaker.go` - 熔断器
   - `internal/mcp/server.go` - MCP Server
   - `internal/tools/registry.go` - 工具注册表

2. **优先级 2（重要模块）**：
   - `internal/rag/` - RAG 检索系统
   - `internal/agent/memory.go` - 上下文管理
   - `internal/agent/middleware.go` - 中间件
   - `internal/mcp/ratelimit.go` - 限流器

3. **优先级 3（辅助模块）**：
   - `internal/tools/` - 各个工具实现
   - `internal/store/` - 存储抽象
   - `internal/embedding/` - Embedding 客户端

建议使用上述注释规范，逐步为核心模块添加完整注释。
