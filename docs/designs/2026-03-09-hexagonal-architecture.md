# 万知六边形架构重构设计

> 版本：v1.0 | 日期：2026-03-09 | 状态：待实施

---

## 一、重构目标

将万知从当前的内聚分层架构重构为严格的六边形架构（端口和适配器模式），实现：

1. **核心业务层零外部依赖** - `domain/` 层完全独立，可单独测试
2. **清晰的依赖方向** - `transport → domain ← infra`
3. **可替换的基础设施** - Milvus、Redis、LLM 等外部服务通过接口隔离
4. **可测试性** - 业务逻辑可用 mock 实现进行单元测试

---

## 二、架构设计

### 2.1 分层结构

```
internal/
├── domain/                    # 核心业务层（零外部依赖）
│   ├── model/                 # 领域模型
│   ├── agent/                 # Agent 引擎 + LLM 接口
│   ├── knowledge/             # 知识管理 + Ingestor 接口
│   ├── rag/                   # RAG 检索 + Store 接口
│   └── tool/                  # 工具层（tools → tool）
│
├── transport/                 # 接入层（协议适配）
│   ├── router.go              # Gin 路由装配
│   ├── mcp.go                 # MCP JSON-RPC handler
│   ├── chat.go                # Chat SSE handler
│   └── middleware.go          # Auth, RateLimit
│
└── infra/                     # 基础设施层（外部服务实现）
    ├── llm/                   # OpenAI, RuleBased LLM
    ├── milvus/                # Milvus 向量库
    ├── redis/                 # Redis 客户端
    ├── embedding/             # Embedding 服务
    └── rerank/                # Rerank 服务
```

### 2.2 依赖方向

```
┌─────────────────────────────────────────────────────────┐
│                   transport/                            │
│  MCP, Chat, Middleware (协议适配)                        │
└──────────────────┬──────────────────────────────────────┘
                   │ 调用
                   ↓
┌─────────────────────────────────────────────────────────┐
│                   domain/                               │
│  model, agent, knowledge, rag, tool                     │
│  (核心业务逻辑，零外部依赖)                               │
│              ┌──────────────────────┐                  │
│              │ LLMClient            │                  │
│              │ Store, Ingestor      │ (接口定义)        │
│              └──────────────────────┘                  │
└───────────────────┼───────────────┼────────────────────┘
                    │ ↑             │ ↑
                    │ │ 实现        │ │ 实现
                    ↓ │             ↓ │
┌─────────────────────────────────────────────────────────┐
│                   infra/                                │
│  llm, milvus, redis, embedding, rerank                  │
│  (外部服务适配器)                                         │
└─────────────────────────────────────────────────────────┘
```

**规则**：
- `domain/` 不 import 任何 `transport/` 或 `infra/` 包
- `infra/` 实现 `domain/` 定义的接口
- `transport/` 调用 `domain/` 的业务逻辑
- `config/` 被所有层引用

---

## 三、核心接口定义

### 3.1 Agent 层接口

```go
// domain/agent/llm.go
package agent

type LLMClient interface {
    Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
    GenerateStream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)
}

type GenerateRequest struct {
    Messages      []Message
    Tools         []ToolDefinition
    MaxTokens     int
    Temperature   float32
}

type GenerateResponse struct {
    Content   string
    ToolCalls []ToolCall
    Usage     Usage
}
```

### 3.2 RAG 层接口

```go
// domain/rag/store.go
package rag

type Store interface {
    Search(ctx context.Context, query string, topK int, filters map[string]string) ([]SearchResult, error)
    Upsert(ctx context.Context, docs []Document) error
    Delete(ctx context.Context, ids []string) error
}

type SearchResult struct {
    ID       string
    Content  string
    Score    float32
    Metadata map[string]string
}
```

### 3.3 Knowledge 层接口

```go
// domain/knowledge/ingestor.go
package knowledge

type Ingestor interface {
    SaveSpec(ctx context.Context, service string, spec []byte) error
    LoadSpec(ctx context.Context, service string) ([]byte, error)
    DeleteService(ctx context.Context, service string) error
    ListEndpoints(ctx context.Context, service string) ([]Endpoint, error)
}
```

---

## 四、目录映射

### 4.1 新增文件

| 文件 | 说明 |
|------|------|
| `domain/model/endpoint.go` | 从 `knowledge/` 抽取 Endpoint, Parameter, Response |
| `domain/model/chunk.go` | 从 `knowledge/` 抽取 Chunk, ChunkType |
| `domain/model/spec.go` | 从 `knowledge/` 抽取 SpecMeta, IngestStats |
| `domain/agent/llm.go` | LLMClient 接口定义（从 openai_llm.go 抽取） |
| `domain/rag/store.go` | Store 接口（从 milvus_store.go 抽取） |
| `domain/knowledge/ingestor.go` | Ingestor 接口定义 |
| `infra/llm/openai.go` | 从 `agent/openai_llm.go` 移动 |
| `infra/llm/rule_based.go` | 从 `agent/rule_based_llm.go` 移动 |
| `infra/milvus/store.go` | 从 `rag/milvus_store.go` 移动 |
| `infra/redis/ingestor.go` | 新建，实现 knowledge.Ingestor |
| `transport/mcp.go` | 从 `mcp/server.go` 移动 |

### 4.2 删除文件

| 文件 | 原因 |
|------|------|
| `internal/domain/tool/match_skill.go` | 与 API 文档 Agent 定位无关 |
| 空的旧目录 | 移动后清理 |

### 4.3 Import 路径变更

| 原路径 | 新路径 |
|--------|--------|
| `wanzhi/internal/tools` | `wanzhi/internal/domain/tool` |
| `wanzhi/internal/agent` | `wanzhi/internal/domain/agent` |
| `wanzhi/internal/knowledge` | `wanzhi/internal/domain/knowledge` |
| `wanzhi/internal/rag` | `wanzhi/internal/domain/rag` |
| `wanzhi/internal/store` | `wanzhi/internal/infra/{milvus,redis}` |
| `wanzhi/internal/embedding` | `wanzhi/internal/infra/embedding` |
| `wanzhi/internal/rerank` | `wanzhi/internal/infra/rerank` |
| `wanzhi/internal/mcp` | `wanzhi/internal/transport` |

---

## 五、实施步骤

### Phase 1: 创建目录结构

```bash
mkdir -p internal/domain/{model,agent,knowledge,rag,tool}
mkdir -p internal/infra/{llm,milvus,redis,embedding,rerank}
```

### Phase 2: 抽取 domain/model

从 `internal/knowledge/` 抽取模型定义到 `domain/model/`：

- `endpoint.go`: Endpoint, Parameter, Response
- `chunk.go`: Chunk, ChunkType
- `spec.go`: SpecMeta, IngestStats

更新所有引用这些类型的文件。

### Phase 3: 移动 domain 层

```bash
# 工具层
mv internal/tools/*.go internal/domain/tool/

# Agent 层（保留接口定义，实现移到 infra）
mv internal/agent/*.go internal/domain/agent/

# 知识层
mv internal/knowledge/*.go internal/domain/knowledge/

# RAG 接口
mv internal/rag/store.go internal/domain/rag/
```

### Phase 4: 移动 infra 层

```bash
# LLM 实现
mv internal/agent/openai_llm.go internal/infra/llm/openai.go
mv internal/agent/rule_based_llm.go internal/infra/llm/rule_based.go

# Milvus
mv internal/rag/milvus_store.go internal/infra/milvus/store.go
mv internal/store/milvus_*.go internal/infra/milvus/

# Redis
mv internal/store/redis_*.go internal/infra/redis/

# Embedding
mv internal/embedding/*.go internal/infra/embedding/

# Rerank
mv internal/rerank/*.go internal/infra/rerank/
```

### Phase 5: 移动 transport 层

```bash
mv internal/mcp/server.go internal/transport/mcp.go
# middleware.go 已在 transport/
```

### Phase 6: 更新 import 路径

使用批量替换更新所有文件的 import 路径：

```bash
# 示例
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/tools|wanzhi/internal/domain/tool|g' {} +
```

需要手动检查的关键文件：
- `cmd/server/main.go`
- `internal/transport/router.go`（需新建）
- `internal/domain/tool/knowledgebase.go`

### Phase 7: 依赖注入重构

在 `cmd/server/main.go` 中组装各层依赖：

```go
// Infrastructure 层
llmClient := infra_llm.NewOpenAIClient(cfg.LLM)
milvusClient := infra_milvus.NewClient(cfg.Milvus)
redisClient := infra_redis.NewClient(cfg.Redis)
embeddingClient := infra_embedding.NewOpenAIClient(cfg.Embedding)
rerankClient := infra_rerank.NewDashScopeClient(cfg.Rerank)

// Domain 层
ragStore := infra_milvus.NewStore(milvusClient, embeddingClient, rerankClient)
ingestor := infra_redis.NewIngestor(redisClient)
kb := domain_tool.NewKnowledgeBase(ingestor, ragStore)
registry := domain_tool.NewRegistry()
domain_tool.RegisterDefaultTools(registry, kb)

// Agent 层
agentEngine := domain_agent.NewAdaptiveEngine(llmClient, registry)

// Transport 层
router := transport.NewRouter(cfg, agentEngine)
```

### Phase 8: 清理与验证

```bash
# 删除空目录
rmdir internal/tools internal/agent internal/knowledge internal/rag \
      internal/store internal/embedding internal/rerank internal/mcp 2>/dev/null

# 验证
go build ./...
go test ./... -count=1
go vet ./...
```

---

## 六、预期影响

| 指标 | 数值 |
|------|------|
| 移动文件数 | ~50 |
| 更新 import 的文件 | ~80-100 |
| 新增接口定义 | 3-5 |
| 删除无关文件 | 1 |
| 预计耗时 | 60-90 分钟 |

---

## 七、验收标准

1. ✅ `go build ./...` 编译通过
2. ✅ `go test ./... -count=1` 所有测试通过
3. ✅ `go vet ./...` 静态分析通过
4. ✅ `domain/` 层无外部依赖（不 import infra/transport）
5. ✅ 所有接口定义在 `domain/`，实现在 `infra/`

---

## 八、风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| Import 路径遗漏 | 中 | 高 | 批量替换 + 编译错误检查 |
| 循环依赖 | 低 | 高 | 检查 `domain/` 不依赖 infra/transport |
| 接口不匹配 | 中 | 中 | 移动前确认接口分离 |
| 测试失败 | 低 | 低 | 重构后统一修复 |

---

## 九、后续优化

重构完成后可考虑的改进：

1. **添加单元测试** - 为 domain 层添加纯逻辑单元测试
2. **依赖注入框架** - 考虑引入 wire 或 dig 简化组装
3. **接口扩展** - 根据需要抽取更多可替换组件
4. **文档更新** - 更新 CLAUDE.md 和 README.md 的架构说明
