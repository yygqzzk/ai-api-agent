# 代码注释规范

本文档定义了项目中代码注释的标准格式和内容要求。

---

## 📋 注释层次

### 1. 包级注释（Package-Level Comments）

每个包的主文件应包含包级注释，说明：
- 包的职责和功能
- 核心设计思想
- 使用的设计模式
- 与其他包的关系

**示例**：
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
//
// # 核心组件
//
//   - AgentEngine: Agent 执行引擎，协调 LLM 和工具调用
//   - LLMClient: LLM 客户端接口，支持 OpenAI 兼容 API
//   - Memory: 上下文管理，维护对话历史
//   - Middleware: 中间件链，处理重试、日志等
//
// # 使用示例
//
//	engine := agent.NewAgentEngine(llmClient, dispatcher,
//	    agent.WithMaxSteps(10),
//	    agent.WithMiddlewares(retryMiddleware, loggingMiddleware),
//	)
//	result, err := engine.Run(ctx, "查询用户登录接口")
package agent
```

---

### 2. 类型级注释（Type-Level Comments）

每个导出的类型应包含：
- 类型的职责
- 关键字段说明
- 使用场景
- 注意事项

**示例**：
```go
// AgentEngine 是 Agent 执行引擎，负责协调 LLM 推理和工具调用。
//
// # 职责
//
//   - 执行 Agent Loop（ReAct 模式）
//   - 管理对话上下文
//   - 调度工具执行
//   - 处理错误和重试
//
// # 设计模式
//
//   - Facade Pattern: 封装复杂的 Agent 执行流程
//   - Template Method: runCore 定义执行骨架，子类可扩展
//
// # 注意事项
//
//   - 必须设置 maxSteps 防止无限循环
//   - LLMClient 和 ToolDispatcher 不能为 nil
//   - 并发安全：多个 goroutine 可同时调用 Run 方法
//
// # 使用示例
//
//	engine := NewAgentEngine(llmClient, dispatcher,
//	    WithMaxSteps(10),
//	    WithSystemPrompt("你是 API 助手"),
//	)
//	result, err := engine.Run(ctx, userQuery)
type AgentEngine struct {
    llmClient     LLMClient      // LLM 客户端，负责推理
    dispatcher    ToolDispatcher // 工具调度器，负责执行工具
    memory        Memory         // 上下文管理器，维护对话历史
    maxSteps      int            // 最大步数，防止无限循环
    systemPrompt  string         // 系统提示词
    toolCatalog   []ToolDefinition // 工具目录，传递给 LLM
    middlewares   []Middleware   // 中间件链
    metrics       *observability.Metrics // 监控指标
}
```

---

### 3. 函数级注释（Function-Level Comments）

每个导出的函数应包含：
- 功能说明
- 参数说明
- 返回值说明
- 错误处理
- 使用示例（复杂函数）

**示例**：
```go
// Run 执行 Agent 循环并返回汇总文本。
//
// # 执行流程
//
//  1. 初始化上下文（system prompt + user query）
//  2. 循环执行（最多 maxSteps 轮）：
//     a. 调用 LLM 推理
//     b. 如果返回工具调用，执行工具
//     c. 将工具结果追加到上下文
//     d. 继续下一轮
//  3. 返回最终汇总结果
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
//   - 工具调用失败：记录错误但继续执行
//   - 超过 maxSteps：返回已有结果 + 截断提示
//
// # 注意事项
//
//   - 该方法是并发安全的
//   - 每次调用会重置 Memory 状态
//   - 工具调用结果会自动追加到上下文
//
// # 使用示例
//
//	result, err := engine.Run(ctx, "查询用户登录接口")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result)
func (e *AgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
    // 实现...
}
```

---

### 4. 内联注释（Inline Comments）

关键逻辑应添加内联注释，说明：
- 为什么这样实现
- 潜在的陷阱
- 性能考虑
- 边界条件处理

**示例**：
```go
func (e *AgentEngine) runCore(ctx context.Context, userQuery string, h Handler) (string, error) {
    mem := e.memory
    mem.Reset() // 重置 Memory 状态，确保每次调用独立
    mem.Append(Message{Role: "system", Content: e.systemPrompt})
    mem.Append(Message{Role: "user", Content: userQuery})

    for step := 1; step <= e.maxSteps; step++ {
        h.OnStepStart(ctx, step)

        // 调用 LLM 推理
        // 注意：这里可能耗时较长（几秒到几十秒），需要设置合理的超时
        reply, err := e.llmClient.Next(ctx, mem.Messages(), e.toolCatalog)
        if err != nil {
            return "", fmt.Errorf("llm next failed: %w", err)
        }

        // 如果 LLM 返回纯文本（无工具调用），说明任务完成
        if len(reply.ToolCalls) == 0 {
            return reply.Content, nil
        }

        // 验证工具是否存在
        // 注意：必须在执行前验证，避免调用不存在的工具
        for _, tc := range reply.ToolCalls {
            if !e.dispatcher.Has(tc.Name) {
                return "", fmt.Errorf("unknown tool: %s", tc.Name)
            }
        }

        // 并发执行工具调用（如果有多个）
        // 设计考虑：工具调用通常是 I/O 密集型，并发执行可以显著降低延迟
        results := e.dispatchTools(ctx, reply.ToolCalls, step, h)

        // 将工具结果追加到上下文
        // 注意：必须保持调用顺序，确保 LLM 能正确理解上下文
        for _, r := range results {
            mem.Append(Message{Role: "tool", ToolCallID: r.callID, Content: r.content})
        }
    }

    // 达到最大步数，返回截断提示
    // 注意：这不是错误，而是正常的终止条件
    return fmt.Sprintf("agent stopped: reached max steps (%d)", e.maxSteps), nil
}
```

---

## 🎯 注释重点

### 必须注释的内容

1. **设计决策**：为什么选择这种实现方式
2. **性能考虑**：时间/空间复杂度、并发安全性
3. **边界条件**：nil 检查、空值处理、边界情况
4. **错误处理**：什么情况下返回错误、如何恢复
5. **并发安全**：是否线程安全、需要加锁的地方
6. **依赖关系**：与其他模块的交互

### 不需要注释的内容

1. **显而易见的代码**：`i++` 不需要注释"递增 i"
2. **自解释的变量名**：`userID` 不需要注释"用户 ID"
3. **标准库用法**：`json.Marshal` 不需要注释"序列化为 JSON"

---

## 📝 特殊注释标记

### TODO

标记待完成的工作：
```go
// TODO(username): 添加缓存支持以提升性能
// TODO: 支持批量查询
```

### FIXME

标记需要修复的问题：
```go
// FIXME: 这里存在并发竞争，需要加锁
// FIXME(username): 内存泄漏，需要及时释放资源
```

### NOTE

标记重要提示：
```go
// NOTE: 该方法不是并发安全的，调用方需要自行加锁
// NOTE: 修改此处逻辑时，需要同步更新 XXX 模块
```

### HACK

标记临时解决方案：
```go
// HACK: 临时方案，等待上游修复后移除
// HACK: 绕过 XXX 库的 bug，详见 issue #123
```

---

## 🏗️ 设计模式注释模板

### Strategy Pattern（策略模式）

```go
// LLMClient 定义了 LLM 客户端接口。
//
// # 设计模式：Strategy Pattern
//
// 通过接口抽象，支持多种 LLM 实现：
//   - OpenAICompatibleLLMClient: 真实 LLM（OpenAI/Claude/DeepSeek）
//   - RuleBasedLLMClient: 规则式 LLM（用于测试和降级）
//
// 这种设计使得：
//   1. 可以在运行时切换 LLM 实现
//   2. 便于测试（使用 Mock LLM）
//   3. 支持降级策略（LLM 不可用时使用规则式）
type LLMClient interface {
    Next(ctx context.Context, messages []Message, tools []ToolDefinition) (*LLMReply, error)
}
```

### Middleware Pattern（中间件模式）

```go
// Middleware 定义了中间件接口。
//
// # 设计模式：Middleware Pattern（Chain of Responsibility）
//
// 中间件链模式用于处理横切关注点：
//   - 重试：自动重试失败的工具调用
//   - 日志：记录工具调用的输入输出
//   - 监控：记录工具调用的耗时和成功率
//   - 熔断：保护外部依赖
//
// 执行顺序：
//   Request → Middleware1 → Middleware2 → Handler → Middleware2 → Middleware1 → Response
//
// 使用示例：
//	handler := Chain(retryMiddleware, loggingMiddleware)(actualHandler)
type Middleware func(ToolHandler) ToolHandler
```

### Factory Pattern（工厂模式）

```go
// NewRateLimiter 根据配置创建限流器。
//
// # 设计模式：Factory Pattern
//
// 根据配置的算法类型，创建不同的限流器实现：
//   - FixedWindow: 固定窗口算法
//   - SlidingWindow: 滑动窗口算法
//   - TokenBucket: 令牌桶算法
//
// 这种设计使得：
//   1. 客户端无需关心具体实现
//   2. 便于添加新的限流算法
//   3. 配置驱动，运行时可切换
func NewRateLimiter(cfg Config) RateLimiter {
    switch cfg.Algorithm {
    case FixedWindow:
        return newFixedWindowLimiter(cfg.Limit)
    case SlidingWindow:
        return newSlidingWindowLimiter(cfg.Limit, cfg.Window)
    case TokenBucket:
        return newTokenBucketLimiter(cfg.Limit, cfg.Burst, cfg.Interval)
    default:
        return newFixedWindowLimiter(cfg.Limit)
    }
}
```

---

## 🎓 学习项目特殊要求

作为学习项目，注释应该：

1. **教学性**：解释为什么这样设计，而不仅仅是做什么
2. **完整性**：包含设计思想、设计模式、注意事项
3. **示例性**：提供使用示例和常见错误
4. **可追溯**：引用相关文档、论文、最佳实践

**示例**：
```go
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
// # 设计模式
//
//   - State Pattern: 三种状态（Closed/Open/HalfOpen）有不同的行为
//   - Proxy Pattern: 熔断器作为代理，控制对外部服务的访问
//
// # 参数调优
//
//   - MaxRequests: 半开状态允许的探测请求数（建议 3-5）
//   - Interval: 统计窗口（建议 10-60 秒）
//   - Timeout: 熔断时长（建议 30-60 秒）
//   - ReadyToTrip: 失败率阈值（建议 0.5-0.7）
//
// # 注意事项
//
//   - 熔断器是有状态的，不要在每次请求时创建新实例
//   - 状态变化时会触发回调，可用于记录日志或发送告警
//   - 并发安全：多个 goroutine 可同时调用 Execute
//
// # 参考资料
//
//   - Martin Fowler: https://martinfowler.com/bliki/CircuitBreaker.html
//   - Netflix Hystrix: https://github.com/Netflix/Hystrix/wiki
type CircuitBreaker struct {
    // ...
}
```

---

## ✅ 注释检查清单

在提交代码前，检查：

- [ ] 所有导出的包、类型、函数都有注释
- [ ] 注释说明了"为什么"而不仅仅是"做什么"
- [ ] 复杂逻辑有内联注释
- [ ] 设计模式有明确标注
- [ ] 注意事项已列出
- [ ] 使用示例完整可运行
- [ ] 没有过时的注释
- [ ] 没有注释掉的代码（应该删除）

---

## 📚 参考资源

- [Effective Go - Commentary](https://go.dev/doc/effective_go#commentary)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments#doc-comments)
- [Design Patterns: Elements of Reusable Object-Oriented Software](https://en.wikipedia.org/wiki/Design_Patterns)
