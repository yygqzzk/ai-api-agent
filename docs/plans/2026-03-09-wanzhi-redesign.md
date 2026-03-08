# 万知 (WanZhi) — 项目重新定位与重构设计

> 版本：v1.0 | 日期：2026-03-09

---

## 一、项目重新定位

### 1.1 新定位

**万知 (WanZhi)** 是一个面向企业内部 API 文档场景的**单领域 Agent**，专注于接口检索、参数说明、依赖分析与调用示例生成。

系统内部基于 Swagger 解析、RAG 检索与 Agent 编排实现多步回答，对外以服务形式提供统一调用入口，可作为前端问答系统的后端，也可作为多 Agent 架构中的 API 专家角色接入。

### 1.2 核心能力

| 能力 | 说明 |
|------|------|
| 接口检索 | 自然语言语义搜索企业 API 文档 |
| 参数说明 | 返回接口的请求/响应参数详情 |
| 依赖分析 | 分析接口的上下游调用关系 |
| 示例生成 | 生成多语言的接口调用代码示例 |
| 参数校验 | 验证请求参数的合法性 |
| 文档导入 | 解析 Swagger/OpenAPI 文档入库 |

### 1.3 对外接入面

| 接入方式 | 协议 | 场景 |
|---------|------|------|
| MCP 端点 (`/mcp`) | JSON-RPC 2.0 over HTTP | 作为"API 专家"接入 Claude Code 等多 Agent 编排系统 |
| Chat API (`/api/chat`) | REST + SSE 流式返回 | 作为前端聊天机器人的后端 |

两种接入面共享同一个 Agent 核心 (`QueryRunner`)，Chat API 不是独立功能，而是同一个 Agent 的另一种交付形式。

### 1.4 面试叙事

> "万知是一个面向企业 API 文档的单领域 Agent。内部基于 Swagger 解析 + Milvus 向量检索 + Rerank 重排序构建 RAG 管线，Agent 引擎采用 ReAct 模式自主编排 6 个工具完成多步回答。架构上采用六边形分层，业务逻辑不依赖任何外部服务。对外同时提供 MCP 和 Chat SSE 两种接入面。"

---

## 二、六边形架构设计

### 2.1 分层结构

```
internal/
├── domain/                    # 核心业务层 — 零外部依赖
│   ├── model/                 # 领域模型
│   │   ├── endpoint.go        # Endpoint, Parameter, Response
│   │   ├── chunk.go           # Chunk, ChunkType
│   │   └── spec.go            # SpecMeta, IngestStats
│   │
│   ├── agent/                 # Agent 引擎
│   │   ├── engine.go          # ReAct Loop 核心
│   │   ├── adaptive.go        # AdaptiveEngine (策略选择/改写/反思)
│   │   ├── llm.go             # LLMClient 接口定义 (不含实现)
│   │   ├── memory.go          # 上下文管理 + Token 预算
│   │   └── handler.go         # 事件观察者接口
│   │
│   ├── knowledge/             # 知识管理
│   │   ├── parser.go          # Swagger/OpenAPI 解析器
│   │   ├── chunker.go         # 语义分块策略
│   │   └── ingestor.go        # 录入编排接口
│   │
│   ├── rag/                   # RAG 检索
│   │   └── store.go           # Store 接口 + Search 业务逻辑
│   │
│   └── tool/                  # 工具层
│       ├── registry.go        # 工具注册表 + 调度
│       ├── types.go           # Tool 接口 + 请求/响应类型
│       ├── knowledgebase.go   # KnowledgeBase (知识库操作枢纽)
│       ├── search_api.go      # search_api 工具
│       ├── get_detail.go      # get_api_detail 工具
│       ├── analyze_deps.go    # analyze_dependencies 工具
│       ├── gen_example.go     # generate_example 工具
│       ├── validate_params.go # validate_params 工具
│       ├── parse_swagger.go   # parse_swagger 工具
│       └── query_api.go       # query_api 工具 (Agent 入口)
│
├── transport/                 # 接入层 — 协议适配
│   ├── router.go              # Gin 路由总装配
│   ├── mcp.go                 # MCP JSON-RPC handler
│   ├── chat.go                # Chat SSE handler (新增)
│   └── middleware.go          # Auth, RateLimit (Gin 中间件)
│
├── infra/                     # 基础设施层 — 外部服务适配
│   ├── milvus/                # Milvus 向量库客户端 (实现 rag.Store)
│   ├── redis/                 # Redis 客户端 (实现 knowledge.Ingestor)
│   ├── llm/                   # OpenAI 兼容客户端 (实现 agent.LLMClient)
│   ├── embedding/             # Embedding 向量化客户端
│   └── rerank/                # Rerank 重排序客户端
│
├── config/                    # 配置 (caarlos0/env struct tag)
└── observability/             # 日志 + Prometheus 指标
```

### 2.2 依赖方向

```
transport → domain ← infra
                ↑
              config
```

- `domain/` 不 import 任何 `transport/` 或 `infra/` 包
- `infra/` 实现 `domain/` 定义的接口
- `transport/` 调用 `domain/` 的业务逻辑
- `config/` 被所有层引用

### 2.3 与当前代码的映射

| 当前位置 | 新位置 | 变化说明 |
|---------|--------|---------|
| `internal/tools/types.go` | `domain/model/` + `domain/tool/types.go` | 模型抽出，工具类型留在 tool |
| `internal/tools/knowledge_base.go` | `domain/tool/knowledgebase.go` | 包名 tools→tool |
| `internal/tools/*.go` (各工具) | `domain/tool/` | 包名 tools→tool |
| `internal/tools/match_skill.go` | 删除 | 与 API 文档 Agent 定位无关 |
| `internal/agent/` | `domain/agent/` | 平移，LLM 实现移到 infra |
| `internal/agent/openai_llm.go` | `infra/llm/` | LLM 实现剥离出 domain |
| `internal/mcp/server.go` | `transport/mcp.go` | MCP handler |
| `internal/mcp/middleware.go` | `transport/middleware.go` | Gin 中间件重写 |
| `internal/mcp/sse.go` | `transport/chat.go` | SSE 能力复用到 Chat handler |
| `internal/rag/store.go` (接口) | `domain/rag/store.go` | 接口留在 domain |
| `internal/rag/milvus_store.go` | `infra/milvus/` | Milvus 实现移到 infra |
| `internal/store/milvus_*.go` | `infra/milvus/` | 合并到 infra |
| `internal/store/redis_*.go` | `infra/redis/` | 合并到 infra |
| `internal/embedding/` | `infra/embedding/` | 平移 |
| `internal/rerank/` | `infra/rerank/` | 平移 |
| `internal/knowledge/` | `domain/knowledge/` + `domain/model/` | 模型抽出 |

---

## 三、Chat SSE API 设计

### 3.1 接口规范

```
POST /api/chat
Content-Type: application/json
Accept: text/event-stream

Request Body:
{
  "message": "查询用户登录接口的参数和 Go 调用示例"
}
```

### 3.2 SSE 事件流

| 事件类型 | 说明 | Data 结构 |
|---------|------|----------|
| `thinking` | Agent 正在调用工具 | `{"step":1,"tool":"search_api","status":"calling"}` |
| `message` | 最终回答内容 | `{"content":"## POST /user/login\n..."}` |
| `done` | 请求完成 | `{"trace":[...]}` |

### 3.3 实现要点

```go
// transport/chat.go
func (h *ChatHandler) HandleChat(c *gin.Context) {
    var req ChatRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": "invalid request"})
        return
    }

    // 复用同一个 Agent 核心 (QueryRunner.RunStream)
    ch := h.streamRunner.RunStream(c.Request.Context(), req.Message)

    c.Writer.Header().Set("Content-Type", "text/event-stream")
    c.Writer.Header().Set("Cache-Control", "no-cache")
    c.Writer.Header().Set("Connection", "keep-alive")
    c.Writer.Flush()

    for ev := range ch {
        c.SSEvent(ev.Type, ev.Data)
        c.Writer.Flush()
    }
}
```

---

## 四、库引入

### 4.1 新增依赖

| 库 | 用途 | 替换内容 |
|----|------|---------|
| `github.com/gin-gonic/gin` | HTTP 路由 + 中间件 + SSE | 替换 `http.NewServeMux` + 手写中间件链 |
| `github.com/caarlos0/env/v11` | 环境变量绑定 | 替换 90+ 行 `ApplyEnv` 方法 |

### 4.2 配置简化示例

```go
// 替换前: 90+ 行手动解析
func (c *Config) ApplyEnv(lookup LookupEnvFunc) error {
    if v, ok := lookup("LLM_TIMEOUT_SECONDS"); ok && v != "" {
        n, err := strconv.Atoi(v)
        // ... 每个变量重复 5-6 行
    }
}

// 替换后: struct tag
type LLMConfig struct {
    Provider       string `env:"LLM_PROVIDER"       envDefault:"openai"`
    APIKey         string `env:"LLM_API_KEY"`
    Model          string `env:"LLM_MODEL"           envDefault:"gpt-4o-mini"`
    BaseURL        string `env:"LLM_BASE_URL"`
    MaxTokens      int    `env:"LLM_MAX_TOKENS"      envDefault:"4096"`
    TimeoutSeconds int    `env:"LLM_TIMEOUT_SECONDS"  envDefault:"30"`
    MaxRetries     int    `env:"LLM_MAX_RETRIES"      envDefault:"2"`
    RetryBackoffMS int    `env:"LLM_RETRY_BACKOFF_MS" envDefault:"200"`
}
```

---

## 五、渐进式实施计划

### Step 1: 引入 Gin + caarlos0/env

- `go get github.com/gin-gonic/gin github.com/caarlos0/env/v11`
- `config/config.go`: struct tag 替换 `ApplyEnv`
- `cmd/server/main.go`: 路由改为 Gin
- `transport/middleware.go`: Gin 中间件重写 auth/rateLimit
- 验证: `go build && go test ./...`

### Step 2: 六边形分层重组

- 创建 `domain/`, `transport/`, `infra/` 目录
- 按映射表移动文件
- 抽出 `domain/model/` (Endpoint, Chunk, SpecMeta)
- 更新所有 import 路径
- 删除 `match_skill` 工具
- 验证: `go build && go test ./...`

### Step 3: 新增 Chat SSE 端点

- 新增 `transport/chat.go`
- 路由注册 `POST /api/chat`
- 复用 `StreamRunner.RunStream()`
- 验证: curl 测试 SSE 流式输出

### Step 4: 项目重命名 + 文档更新

- `go.mod` module path: `ai-agent-api` → `wanzhi`
- 全量 import 路径替换
- 重写 `README.md`, `CLAUDE.md`, `docs/design.md`
- 验证: `go build && go test ./...`

---

## 六、项目信息

| 项目 | 值 |
|------|---|
| 项目名 | 万知 (WanZhi) |
| Go Module | `wanzhi` |
| 定位 | 企业 API 文档单领域 Agent |
| 架构 | 六边形架构 (domain / transport / infra) |
| 接入面 | MCP JSON-RPC + Chat SSE |
| 新增库 | gin-gonic/gin, caarlos0/env |
| 重构级别 | 中度 (包结构重组 + 库替换 + 新增功能) |
