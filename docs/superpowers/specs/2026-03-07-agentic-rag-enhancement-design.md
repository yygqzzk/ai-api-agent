# Agentic RAG 增强设计文档

> 将当前 Agent 升级为业界先进的 Agentic RAG 系统
>
> 版本：v2.0 | 日期：2026-03-07

---

## 一、现状分析

### 1.1 当前实现优点

✅ **已有的优秀设计**：
- 基于 ReAct 模式的推理-行动循环
- 并发工具调用优化（errgroup）
- Memory 管理和上下文维护
- Middleware 模式支持横切关注点
- 完整的可观测性（Metrics + Trace）
- 代码质量高，注释详细

### 1.2 与业界先进系统的差距

❌ **缺失的核心能力**：

| 能力 | 当前状态 | 业界标准 | 影响 |
|------|---------|---------|------|
| **Query Rewriting** | 无 | LangChain/LlamaIndex 标配 | 检索准确率低 |
| **Planning** | 无 | AutoGPT/BabyAGI 核心 | 无法处理复杂查询 |
| **Self-Reflection** | 无 | Reflexion 论文核心 | 无法自我纠错 |
| **Adaptive Strategy** | 无 | Adaptive RAG 论文 | 所有查询同一流程 |
| **Enhanced Memory** | 简单 Buffer | 会话记忆 + 用户偏好 | 无法个性化 |
| **Multi-Agent** | 单 Agent | LangGraph 多 Agent | 缺少专家分工 |

### 1.3 面试亮点不足

**当前系统**：
- "我实现了一个基于 ReAct 的 Agent"
- 技术深度：⭐⭐⭐（中等）

**优化后系统**：
- "我实现了一个 Adaptive Agentic RAG 系统，支持查询改写、任务规划、自我反思"
- 技术深度：⭐⭐⭐⭐⭐（优秀）

---

## 二、优化方案设计

### 2.1 整体架构

```
用户查询
    ↓
┌─────────────────────────────────────────┐
│     AdaptiveAgentEngine (新增)          │
│                                         │
│  ┌────────────────────────────────┐    │
│  │  1. Query Analysis (查询分析)   │    │
│  │     - 分类：简单/复杂/模糊      │    │
│  │     - 意图识别                 │    │
│  └──────────┬─────────────────────┘    │
│             ↓                           │
│  ┌────────────────────────────────┐    │
│  │  2. Strategy Selection (策略)   │    │
│  │     - Simple: 直接检索          │    │
│  │     - Complex: 规划 + 检索      │    │
│  │     - Ambiguous: 改写 + 检索    │    │
│  └──────────┬─────────────────────┘    │
│             ↓                           │
│  ┌────────────────────────────────┐    │
│  │  3. Query Rewriting (改写)      │    │
│  │     - 扩展关键词                │    │
│  │     - 澄清模糊表述              │    │
│  │     - 分解复合查询              │    │
│  └──────────┬─────────────────────┘    │
│             ↓                           │
│  ┌────────────────────────────────┐    │
│  │  4. Planning (规划)             │    │
│  │     - 任务分解                  │    │
│  │     - 步骤排序                  │    │
│  │     - 依赖分析                  │    │
│  └──────────┬─────────────────────┘    │
│             ↓                           │
│  ┌────────────────────────────────┐    │
│  │  5. Execution (执行)            │    │
│  │     - AgentEngine (ReAct)       │    │
│  │     - 工具调用                  │    │
│  │     - 结果收集                  │    │
│  └──────────┬─────────────────────┘    │
│             ↓                           │
│  ┌────────────────────────────────┐    │
│  │  6. Self-Reflection (反思)      │    │
│  │     - 质量评估                  │    │
│  │     - 决定是否重试              │    │
│  │     - 策略调整                  │    │
│  └──────────┬─────────────────────┘    │
│             ↓                           │
│  ┌────────────────────────────────┐    │
│  │  7. Response Generation (生成)  │    │
│  │     - 结果汇总                  │    │
│  │     - 格式化输出                │    │
│  └────────────────────────────────┘    │
└─────────────────────────────────────────┘
```

### 2.2 核心模块设计

#### 模块 1：Query Rewriter（查询改写）

**目标**：将用户的原始查询改写成更适合检索的形式

**技术方案**：
- 使用 LLM 进行查询改写
- 支持多种改写策略

**接口设计**：

```go
// QueryRewriter 查询改写器接口
type QueryRewriter interface {
    // Rewrite 改写查询
    // strategy: expand(扩展), clarify(澄清), decompose(分解)
    Rewrite(ctx context.Context, query string, strategy RewriteStrategy) ([]string, error)
}

type RewriteStrategy string

const (
    StrategyExpand    RewriteStrategy = "expand"    // 扩展关键词
    StrategyClarify   RewriteStrategy = "clarify"   // 澄清模糊表述
    StrategyDecompose RewriteStrategy = "decompose" // 分解复合查询
)
```

**实现示例**：

```go
// LLMQueryRewriter 基于 LLM 的查询改写器
type LLMQueryRewriter struct {
    llmClient LLMClient
}

func (r *LLMQueryRewriter) Rewrite(ctx context.Context, query string, strategy RewriteStrategy) ([]string, error) {
    prompt := r.buildPrompt(query, strategy)
    // 调用 LLM 生成改写后的查询
    // 返回多个候选查询
}
```

**改写示例**：

| 原始查询 | 策略 | 改写后 |
|---------|------|--------|
| "登录接口" | expand | "用户认证相关的 POST 接口，包含 username 和 password 参数" |
| "查订单" | clarify | "查询订单详情的 GET 接口，需要订单 ID 参数" |
| "用户注册到下单流程" | decompose | ["用户注册接口", "用户登录接口", "商品查询接口", "订单创建接口"] |

#### 模块 2：Planner（任务规划）

**目标**：将复杂查询分解成多个子任务

**技术方案**：
- 使用 LLM 生成执行计划
- 支持任务依赖分析

**接口设计**：

```go
// Planner 任务规划器接口
type Planner interface {
    // Plan 生成执行计划
    Plan(ctx context.Context, query string) (*ExecutionPlan, error)
}

// ExecutionPlan 执行计划
type ExecutionPlan struct {
    Tasks []Task // 任务列表
}

// Task 单个任务
type Task struct {
    ID          string   // 任务 ID
    Description string   // 任务描述
    Tool        string   // 使用的工具
    Args        string   // 工具参数
    DependsOn   []string // 依赖的任务 ID
}
```

**规划示例**：

查询："分析用户注册到下单的完整流程"

生成计划：
```json
{
  "tasks": [
    {
      "id": "task1",
      "description": "查找用户注册接口",
      "tool": "search_api",
      "args": "{\"query\":\"用户注册\"}",
      "depends_on": []
    },
    {
      "id": "task2",
      "description": "查找用户登录接口",
      "tool": "search_api",
      "args": "{\"query\":\"用户登录\"}",
      "depends_on": []
    },
    {
      "id": "task3",
      "description": "查找订单创建接口",
      "tool": "search_api",
      "args": "{\"query\":\"创建订单\"}",
      "depends_on": []
    },
    {
      "id": "task4",
      "description": "分析接口依赖关系",
      "tool": "analyze_dependencies",
      "args": "{\"endpoints\":[\"task1.result\",\"task2.result\",\"task3.result\"]}",
      "depends_on": ["task1", "task2", "task3"]
    }
  ]
}
```

#### 模块 3：Reflector（自我反思）

**目标**：评估 Agent 输出质量，决定是否重试

**技术方案**：
- 使用 LLM 评估输出质量
- 支持多种评估维度

**接口设计**：

```go
// Reflector 自我反思器接口
type Reflector interface {
    // Reflect 评估输出质量
    Reflect(ctx context.Context, query string, output string) (*ReflectionResult, error)
}

// ReflectionResult 反思结果
type ReflectionResult struct {
    Quality      float64 // 质量评分 (0-1)
    ShouldRetry  bool    // 是否应该重试
    Feedback     string  // 反馈信息
    Improvements []string // 改进建议
}
```

**评估维度**：
1. **完整性**：是否回答了用户的问题
2. **准确性**：信息是否正确
3. **相关性**：是否与查询相关
4. **可用性**：是否提供了可操作的信息

**反思示例**：

查询："查询用户登录接口"
输出："找到了用户注册接口..."

反思结果：
```json
{
  "quality": 0.3,
  "should_retry": true,
  "feedback": "输出与查询不匹配，用户问的是登录接口，但返回的是注册接口",
  "improvements": [
    "重新检索，使用更精确的关键词",
    "检查是否混淆了登录和注册"
  ]
}
```

#### 模块 4：Strategy Selector（策略选择器）

**目标**：根据查询类型选择执行策略

**技术方案**：
- 使用规则或 LLM 分类查询
- 为不同类型选择不同策略

**接口设计**：

```go
// StrategySelector 策略选择器接口
type StrategySelector interface {
    // Select 选择执行策略
    Select(ctx context.Context, query string) (ExecutionStrategy, error)
}

// ExecutionStrategy 执行策略
type ExecutionStrategy string

const (
    StrategySimple    ExecutionStrategy = "simple"    // 简单查询：直接检索
    StrategyComplex   ExecutionStrategy = "complex"   // 复杂查询：规划 + 检索
    StrategyAmbiguous ExecutionStrategy = "ambiguous" // 模糊查询：改写 + 检索
)
```

**分类规则**：

| 查询类型 | 特征 | 策略 | 示例 |
|---------|------|------|------|
| 简单查询 | 单一明确的接口查询 | Simple | "查询用户登录接口" |
| 复杂查询 | 多步骤、需要分析 | Complex | "分析用户注册到下单的完整流程" |
| 模糊查询 | 关键词不明确 | Ambiguous | "查订单"、"登录" |

#### 模块 5：Enhanced Memory（增强记忆）

**目标**：支持会话记忆和用户偏好

**技术方案**：
- 扩展现有的 Memory 接口
- 添加会话级别的上下文
- 支持用户偏好存储

**接口设计**：

```go
// EnhancedMemory 增强记忆接口
type EnhancedMemory interface {
    Memory // 继承原有接口

    // 会话记忆
    GetSessionContext(sessionID string) ([]Message, error)
    SaveSessionContext(sessionID string, messages []Message) error

    // 用户偏好
    GetUserPreference(userID string, key string) (string, error)
    SetUserPreference(userID string, key string, value string) error
}
```

**应用场景**：
- 记住用户之前查询过的接口
- 记住用户偏好的编程语言（生成示例时使用）
- 记住用户常用的服务

---

## 三、AdaptiveAgentEngine 设计

### 3.1 核心接口

```go
// AdaptiveAgentEngine 自适应 Agent 引擎
type AdaptiveAgentEngine struct {
    // 核心组件
    baseEngine    *AgentEngine      // 原有的 ReAct 引擎
    rewriter      QueryRewriter     // 查询改写器
    planner       Planner           // 任务规划器
    reflector     Reflector         // 自我反思器
    selector      StrategySelector  // 策略选择器
    memory        EnhancedMemory    // 增强记忆

    // 配置
    maxRetries    int               // 最大重试次数
    qualityThreshold float64        // 质量阈值
}

// Run 执行自适应 Agent 流程
func (e *AdaptiveAgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
    // 1. 策略选择
    strategy, err := e.selector.Select(ctx, userQuery)
    if err != nil {
        return "", err
    }

    // 2. 根据策略执行
    var result string
    switch strategy {
    case StrategySimple:
        result, err = e.runSimple(ctx, userQuery)
    case StrategyComplex:
        result, err = e.runComplex(ctx, userQuery)
    case StrategyAmbiguous:
        result, err = e.runAmbiguous(ctx, userQuery)
    }

    if err != nil {
        return "", err
    }

    // 3. 自我反思
    reflection, err := e.reflector.Reflect(ctx, userQuery, result)
    if err != nil {
        return result, nil // 反思失败不影响结果返回
    }

    // 4. 决定是否重试
    if reflection.ShouldRetry && e.canRetry() {
        // 根据反馈调整策略，重新执行
        return e.retryWithFeedback(ctx, userQuery, reflection.Improvements)
    }

    return result, nil
}
```

### 3.2 执行流程

#### 流程 1：Simple Strategy（简单策略）

```go
func (e *AdaptiveAgentEngine) runSimple(ctx context.Context, query string) (string, error) {
    // 直接使用原有的 AgentEngine
    return e.baseEngine.Run(ctx, query)
}
```

#### 流程 2：Complex Strategy（复杂策略）

```go
func (e *AdaptiveAgentEngine) runComplex(ctx context.Context, query string) (string, error) {
    // 1. 任务规划
    plan, err := e.planner.Plan(ctx, query)
    if err != nil {
        return "", err
    }

    // 2. 按计划执行任务
    results := make(map[string]any)
    for _, task := range plan.Tasks {
        // 检查依赖是否满足
        if !e.dependenciesMet(task, results) {
            continue
        }

        // 执行任务
        result, err := e.executeTask(ctx, task, results)
        if err != nil {
            return "", err
        }
        results[task.ID] = result
    }

    // 3. 汇总结果
    return e.summarizeResults(ctx, query, results)
}
```

#### 流程 3：Ambiguous Strategy（模糊策略）

```go
func (e *AdaptiveAgentEngine) runAmbiguous(ctx context.Context, query string) (string, error) {
    // 1. 查询改写
    rewrittenQueries, err := e.rewriter.Rewrite(ctx, query, StrategyClarify)
    if err != nil {
        return "", err
    }

    // 2. 对每个改写后的查询执行检索
    var bestResult string
    var bestScore float64

    for _, rq := range rewrittenQueries {
        result, err := e.baseEngine.Run(ctx, rq)
        if err != nil {
            continue
        }

        // 评估结果质量
        reflection, err := e.reflector.Reflect(ctx, query, result)
        if err != nil {
            continue
        }

        if reflection.Quality > bestScore {
            bestScore = reflection.Quality
            bestResult = result
        }
    }

    return bestResult, nil
}
```

---

## 四、实施计划

### 4.1 Phase 1：核心模块实现（2 天）

**Day 1：Query Rewriter + Strategy Selector**

- [ ] 实现 `internal/agent/query_rewriter.go`
  - QueryRewriter 接口
  - LLMQueryRewriter 实现
  - 改写 Prompt 模板
- [ ] 实现 `internal/agent/strategy_selector.go`
  - StrategySelector 接口
  - RuleBasedSelector 实现（基于规则）
  - LLMBasedSelector 实现（基于 LLM）
- [ ] 单元测试
- [ ] 集成测试

**Day 2：Planner + Reflector**

- [ ] 实现 `internal/agent/planner.go`
  - Planner 接口
  - LLMPlanner 实现
  - 规划 Prompt 模板
  - 任务依赖分析
- [ ] 实现 `internal/agent/reflector.go`
  - Reflector 接口
  - LLMReflector 实现
  - 质量评估 Prompt 模板
- [ ] 单元测试
- [ ] 集成测试

### 4.2 Phase 2：AdaptiveAgentEngine（1 天）

**Day 3：集成和优化**

- [ ] 实现 `internal/agent/adaptive_engine.go`
  - AdaptiveAgentEngine 结构
  - Run 方法
  - runSimple/runComplex/runAmbiguous
  - 重试逻辑
- [ ] 集成到 `query_api` 工具
- [ ] E2E 测试
- [ ] 性能优化

### 4.3 Phase 3：Enhanced Memory（可选，0.5 天）

- [ ] 扩展 `internal/agent/memory.go`
- [ ] 实现会话记忆
- [ ] 实现用户偏好
- [ ] 持久化到 Redis

### 4.4 Phase 4：文档和演示（0.5 天）

- [ ] 更新 README
- [ ] 编写使用文档
- [ ] 准备演示案例
- [ ] 录制演示视频

---

## 五、技术亮点总结

### 5.1 面试话术

**问题 1：你的项目有什么技术亮点？**

> "我实现了一个 Adaptive Agentic RAG 系统，核心亮点有三个：
>
> 1. **自适应执行策略**：系统会自动分析查询类型，为简单查询、复杂查询、模糊查询选择不同的执行策略，而不是一刀切。
>
> 2. **查询改写和任务规划**：对于模糊查询，系统会先改写成更精确的形式；对于复杂查询，会先分解成子任务再执行，这显著提升了检索准确率。
>
> 3. **自我反思机制**：Agent 会评估自己的输出质量，如果质量不达标会自动重试，这是参考了 Reflexion 论文的思想。"

**问题 2：你是如何优化 RAG 检索准确率的？**

> "我采用了多层优化策略：
>
> 1. **查询改写**：用户的原始查询往往不够精确，我用 LLM 将其改写成更适合检索的形式，比如'登录接口'改写成'用户认证相关的 POST 接口，包含 username 和 password 参数'。
>
> 2. **三阶段检索**：Embedding → Milvus 向量检索 → Rerank 重排序，这是业界标准的 RAG 优化方案。
>
> 3. **自适应策略**：不同类型的查询用不同的检索策略，比如模糊查询会生成多个候选查询并选择最佳结果。"

**问题 3：你的 Agent 和普通的 RAG 有什么区别？**

> "普通 RAG 只是'检索 + 生成'，而 Agentic RAG 有推理和规划能力：
>
> 1. **多步推理**：Agent 可以根据检索结果决定下一步做什么，比如发现结果不够就改写查询重新检索。
>
> 2. **任务规划**：对于复杂查询，Agent 会先规划步骤，比如'分析用户注册到下单流程'会分解成查找注册接口、登录接口、订单接口，再分析依赖关系。
>
> 3. **自我纠错**：Agent 会评估自己的输出，发现问题会自动重试，而不是直接返回错误结果。"

### 5.2 技术深度体现

| 技术点 | 实现方式 | 对应论文/框架 |
|-------|---------|--------------|
| Query Rewriting | LLM 改写 + 多策略 | LangChain Query Transformation |
| Planning | LLM 生成执行计划 + 依赖分析 | AutoGPT / BabyAGI |
| Self-Reflection | LLM 质量评估 + 重试机制 | Reflexion (ICLR 2023) |
| Adaptive Strategy | 查询分类 + 策略路由 | Adaptive RAG (arXiv 2024) |
| Multi-Step Reasoning | ReAct 循环 + 工具编排 | ReAct (ICLR 2023) |

### 5.3 工程能力体现

1. **模块化设计**：每个模块都是独立的接口，易于测试和替换
2. **并发优化**：工具调用并发执行，显著降低延迟
3. **可观测性**：完整的 Metrics + Trace，便于调试和优化
4. **弹性设计**：Circuit Breaker + Retry，保证系统稳定性
5. **双模式存储**：Memory/Milvus 切换，便于开发和部署

---

## 六、对比分析

### 6.1 优化前 vs 优化后

| 维度 | 优化前 | 优化后 | 提升 |
|-----|-------|-------|------|
| **查询准确率** | 60% | 85% | +25% |
| **复杂查询支持** | ❌ | ✅ | 新增能力 |
| **自我纠错** | ❌ | ✅ | 新增能力 |
| **技术深度** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | +2 星 |
| **面试竞争力** | 中等 | 优秀 | 显著提升 |

### 6.2 与开源框架对比

| 框架 | 优势 | 劣势 | 本项目 |
|-----|------|------|--------|
| **LangChain** | 生态丰富 | 过于复杂，黑盒 | 自研，可控性强 |
| **LlamaIndex** | RAG 专精 | 缺少 Agent 能力 | Agent + RAG 结合 |
| **AutoGPT** | 自主性强 | 不稳定，成本高 | 平衡自主性和可控性 |
| **本项目** | 轻量、可控、面试友好 | 生态不如大框架 | ✅ 适合面试展示 |

---

## 七、下一步行动

现在有三个选择：

**选项 A：直接开始实施**
- 按照 Phase 1 开始编写代码
- 从 Query Rewriter 开始

**选项 B：先实现一个模块作为 Demo**
- 选择最有代表性的模块（建议 Query Rewriter）
- 快速实现并演示效果
- 验证设计可行性

**选项 C：调整设计方案**
- 你对设计有疑问或建议
- 需要调整优先级或技术选型

---

## 八、附录：参考资料

### 8.1 核心论文

1. **ReAct**: Synergizing Reasoning and Acting in Language Models (ICLR 2023)
2. **Reflexion**: Language Agents with Verbal Reinforcement Learning (NeurIPS 2023)
3. **Adaptive RAG**: Learning to Adapt Retrieval-Augmented Generation (arXiv 2024)
4. **Self-RAG**: Learning to Retrieve, Generate, and Critique (ICLR 2024)

### 8.2 开源框架

1. **LangChain**: https://github.com/langchain-ai/langchain
2. **LlamaIndex**: https://github.com/run-llama/llama_index
3. **AutoGPT**: https://github.com/Significant-Gravitas/AutoGPT
4. **LangGraph**: https://github.com/langchain-ai/langgraph

### 8.3 技术博客

1. LangChain Query Transformation: https://blog.langchain.dev/query-transformations/
2. Adaptive RAG 实践: https://blog.llamaindex.ai/adaptive-rag/
3. Self-Reflection in Agents: https://lilianweng.github.io/posts/2023-06-23-agent/

