# 📝 注释快速参考卡片

## 🎯 核心原则

1. **说明"为什么"** - 不仅仅是"做什么"
2. **标注设计模式** - Strategy/Middleware/Factory 等
3. **说明并发安全性** - 是否线程安全
4. **提供使用示例** - 完整可运行的代码
5. **列出注意事项** - 边界条件、性能考虑

---

## 📋 注释模板

### 包级注释（简化版）

```go
// Package xxx 实现了 XXX 功能。
//
// 设计思想：[核心理念]
// 设计模式：[使用的模式]
// 核心组件：[主要组件]
package xxx
```

### 类型注释（简化版）

```go
// TypeName 是 XXX，负责 YYY。
//
// 职责：[主要职责]
// 并发安全：[是/否]
// 注意事项：[关键注意点]
type TypeName struct {
    field1 Type1 // [说明]
}
```

### 函数注释（简化版）

```go
// FunctionName 执行 XXX 操作。
//
// 参数：param1 - [说明]
// 返回：Type1 - [说明], error - [说明]
// 注意：[关键注意点]
func FunctionName(param1 Type1) (Type2, error) {
    // 实现...
}
```

---

## 🏷️ 设计模式标注

### Strategy Pattern（策略模式）

```go
// LLMClient 定义了 LLM 客户端接口。
//
// 设计模式：Strategy Pattern
// 支持多种实现：OpenAI/规则式/Mock
type LLMClient interface {
    Next(ctx context.Context, messages []Message) (*LLMReply, error)
}
```

### Middleware Pattern（中间件模式）

```go
// Middleware 定义了中间件接口。
//
// 设计模式：Middleware Pattern (Chain of Responsibility)
// 用于处理：重试/日志/监控/熔断
type Middleware func(ToolHandler) ToolHandler
```

### Factory Pattern（工厂模式）

```go
// NewRateLimiter 根据配置创建限流器。
//
// 设计模式：Factory Pattern
// 支持算法：FixedWindow/SlidingWindow/TokenBucket
func NewRateLimiter(cfg Config) RateLimiter {
    // 实现...
}
```

### State Pattern（状态模式）

```go
// CircuitBreaker 实现了熔断器模式。
//
// 设计模式：State Pattern
// 状态机：Closed → Open → HalfOpen → Closed
type CircuitBreaker struct {
    state atomic.Value // State
}
```

---

## ⚠️ 注意事项标注

### 并发安全性

```go
// AgentEngine 是并发安全的。
//
// 并发安全：
//   - Run 方法：并发安全（每次调用创建独立 Memory）
//   - SetToolCatalog：不是并发安全的（需要在启动前调用）
```

### 性能考虑

```go
// dispatchTools 并发执行工具调用。
//
// 性能考虑：
//   - 单个工具：直接执行（避免 goroutine 开销）
//   - 多个工具：并发执行（降低总延迟）
//   - 时间复杂度：O(max(t1, t2, ..., tn))
```

### 边界条件

```go
// Add 计算两个整数的和。
//
// 注意事项：
//   - 不检查溢出，调用方需确保结果在 int 范围内
//   - 如需溢出检查，请使用 math/big 包
```

---

## 📚 文档位置

| 文档 | 路径 |
|------|------|
| 完整规范 | `docs/COMMENT-GUIDELINES.md` |
| 完整示例 | `docs/COMMENT-EXAMPLES.md` |
| 添加指南 | `docs/ADDING-COMMENTS-GUIDE.md` |
| 工作总结 | `docs/COMMENT-WORK-SUMMARY.md` |

---

## ✅ 检查清单

- [ ] 包级注释（设计思想/设计模式）
- [ ] 类型注释（职责/并发安全性）
- [ ] 函数注释（参数/返回值/注意事项）
- [ ] 内联注释（复杂逻辑）
- [ ] 使用示例（完整可运行）

---

## 🚀 快速开始

```bash
# 1. 阅读规范
cat docs/COMMENT-GUIDELINES.md

# 2. 查看示例
cat docs/COMMENT-EXAMPLES.md

# 3. 开始添加（从核心模块开始）
vim internal/agent/engine.go

# 4. 验证质量
go doc internal/agent.AgentEngine
```

---

## 💡 记住

- 注释是为了**解释设计决策**，不是重复代码
- 注释是为了**未来的自己**，不仅仅是他人
- 注释是为了**面试讲解**，展示技术深度
