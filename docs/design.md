# 企业级智能 API 助手 — 需求设计文档

> 基于 AI Agent + MCP 协议的企业 API 知识库助手
>
> 版本：v1.0 | 日期：2026-03-04

---

## 一、项目概述

### 1.1 项目定位

基于 Go 语言自研 AI Agent 引擎，通过 MCP (Model Context Protocol) 协议将 Agent 能力封装为标准化远程服务。开发者通过 Claude Code 等 MCP 客户端，以自然语言查询企业 API 文档，Agent 自动完成语义检索、依赖分析与调用示例生成，解决企业内部 API 对接效率低、文档分散难查的痛点。

### 1.2 项目目标

- **可演示的 Demo**：核心链路跑通，展示技术深度
- **面试可讲**：每个技术点有真实代码支撑
- **架构合理**：符合生产级 MCP Server 设计规范

### 1.3 技术栈

| 层级 | 技术选型 |
|------|---------|
| 语言 | Go |
| MCP SDK | mcp-go (mark3labs/mcp-go) |
| 向量数据库 | Milvus |
| 缓存 | Redis |
| LLM 接入 | OpenAI 兼容接口 (支持 Claude/GPT/DeepSeek) |
| Embedding | bge-large-zh-v1.5 (dim=1024) |
| Rerank | bge-reranker-v2-m3 |
| 传输协议 | Streamable HTTP |
| 部署 | Docker Compose |

### 1.4 演示数据

使用 **Swagger Petstore** (OpenAPI 官方标准示例) 作为唯一数据源，约 20 个接口，覆盖 GET/POST/PUT/DELETE 及文件上传等常见场景。

---

## 二、系统架构

### 2.1 整体架构图

```
Claude Code (MCP 客户端)
    │
    │ Streamable HTTP (JSON-RPC)
    │ 只能看到最终汇总结果
    │
    ▼
┌──────────────────────────────────────────────────────┐
│              MCP Server (Go, HTTP 远程服务)            │
│                                                       │
│  ┌─────────────────────────────────────────────────┐  │
│  │              Middleware Chain                     │  │
│  │  Auth → Logging → RateLimit → Validation        │  │
│  └──────────────────────┬──────────────────────────┘  │
│                         │                             │
│  ┌──────────┐  ┌───────▼────────┐  ┌──────────────┐  │
│  │ Resources │  │     Tools      │  │   Prompts    │  │
│  │ (只读数据) │  │  (可执行操作)   │  │ (预设模板)    │  │
│  │           │  │                │  │              │  │
│  │• 服务列表  │  │• query_api ────│──│→ 触发 Agent  │  │
│  │• API 概览  │  │• search_api   │  │• api_query   │  │
│  │• 配置信息  │  │• parse_swagger │  │• dependency  │  │
│  │           │  │• analyze_deps  │  │  _analysis   │  │
│  └──────────┘  │• gen_example   │  │• onboarding  │  │
│                 │• get_api_detail│  └──────────────┘  │
│                 │• match_skill   │                    │
│                 │• validate_params│                   │
│                 └───────┬────────┘                    │
│                         │                             │
│  ┌──────────────────────▼──────────────────────────┐  │
│  │         Agent Engine (信息过滤网关)               │  │
│  │                                                  │  │
│  │  • Agent Loop (ReAct 模式)                       │  │
│  │  • Context Manager (消息历史 + Token 预算)        │  │
│  │  • Function Call Dispatcher (工具调度)            │  │
│  │  • 汇总 + 脱敏 (只返回结构化结果，不暴露原始数据)  │  │
│  └──────────────────────┬──────────────────────────┘  │
│                         │                             │
│  ┌──────────────────────▼──────────────────────────┐  │
│  │              Core Services                       │  │
│  │                                                  │  │
│  │  ┌────────────┐ ┌──────────┐ ┌───────────────┐  │  │
│  │  │ RAG Engine │ │ Swagger  │ │  Skills       │  │  │
│  │  │            │ │ Parser   │ │  Engine       │  │  │
│  │  │ • Embed    │ │          │ │               │  │  │
│  │  │ • Search   │ │ • Parse  │ │ • Template    │  │  │
│  │  │ • Rerank   │ │ • Index  │ │ • Match       │  │  │
│  │  └─────┬──────┘ └────┬─────┘ └───────────────┘  │  │
│  │        │             │                           │  │
│  │  ┌─────▼─────────────▼──────────────────────┐   │  │
│  │  │           Storage Layer                   │   │  │
│  │  │  Milvus (向量检索)  │  Redis (缓存/元数据)  │   │  │
│  │  └──────────────────────────────────────────┘   │  │
│  └─────────────────────────────────────────────────┘  │
│                                                       │
│  ┌─────────────────────────────────────────────────┐  │
│  │              Lifecycle Hooks                      │  │
│  │  OnInit → BeforeToolCall → AfterToolCall         │  │
│  │  → OnShutdown (优雅关闭)                          │  │
│  └─────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────┘
```

### 2.2 核心设计理念

**Agent 作为信息过滤网关**：内嵌 Agent 不仅做推理编排，更充当安全网关层。企业 API 文档的原始数据（向量检索结果、接口路径、参数细节）不直接暴露给客户端，Agent 层只返回经过汇总和脱敏的结构化结果。

内嵌 Agent 的三重存在意义：

1. **安全网关** — 过滤原始数据，只返回汇总结果，企业数据不出服务端
2. **成本优化** — 用便宜模型 (GPT-4o-mini/DeepSeek) 做编排，减少客户端 Claude token 消耗
3. **技术深度** — 自研 Agent 引擎，展示 Agent Loop、Function Call 等核心能力

### 2.3 通信协议

采用 **Streamable HTTP** 传输模式（MCP 协议当前主流远程方案）：

- 简单工具调用：标准 HTTP 请求-响应
- Agent 长任务：SSE 流式推送进度
- 端点路径：`/mcp`
- 认证方式：Bearer Token

```
客户端配置:
{
  "mcpServers": {
    "api-assistant": {
      "url": "http://your-server:8080/mcp",
      "headers": {
        "Authorization": "Bearer your-token"
      }
    }
  }
}
```

---

## 三、模块详细设计

### 3.1 MCP Protocol Layer

#### 3.1.1 Tools（可执行操作）

| Tool | 触发 Agent | 输入 | 输出 | 说明 |
|------|-----------|------|------|------|
| `query_api` | **是** | query: string | 结构化汇总回答 | 自然语言查询，走完整 Agent Loop |
| `search_api` | 否 | query: string, top_k: int | 匹配的 API 列表（摘要） | 轻量 RAG 语义检索 |
| `get_api_detail` | 否 | service: string, endpoint: string | 完整 API Schema | 精确查询单个接口详情 |
| `analyze_dependencies` | 否 | endpoint: string | 依赖关系图 | 分析上下游调用链 |
| `generate_example` | 否 | endpoint: string, language: string | 代码示例 | 生成接口调用代码 |
| `match_skill` | 否 | query: string | 匹配的 Skill 模板 | 匹配预定义场景 |
| `validate_params` | 否 | endpoint: string, params: object | 校验结果 | 参数合法性检查 |
| `parse_swagger` | 否 | file_path 或 url: string | 导入结果统计 | Swagger 文件解析导入知识库 |

**关键设计**：`query_api` 是唯一触发 Agent Loop 的入口，Agent 内部调度其他工具完成多步推理。其余工具也直接暴露给 Claude Code，支持用户直接调用单个能力。

#### 3.1.2 Resources（只读数据）

| URI | 说明 |
|-----|------|
| `services://list` | 已录入的服务列表及接口数量 |
| `api://{service}/{path}` | 某个接口的结构化详情 |
| `config://server` | 服务器状态、版本、知识库统计 |

#### 3.1.3 Prompts（预设模板）

| Prompt | 说明 |
|--------|------|
| `api_query` | 引导 Claude 使用 query_api 进行智能查询 |
| `dependency_analysis` | 引导分析某个业务流程涉及的全部接口 |
| `onboarding` | 新开发者快速了解系统 API 全景 |

#### 3.1.4 Middleware Chain

```
请求进入
  │
  ▼
Auth MW ────→ Bearer Token 验证，拒绝未授权请求
  │
  ▼
Logging MW ──→ 记录请求参数、工具名、来源 IP
  │
  ▼
RateLimit MW → 限制每分钟请求数，防止滥用
  │
  ▼
Validation MW → 校验必填参数、类型检查
  │
  ▼
Tool Handler ──→ 实际工具执行
  │
  ▼
Logging MW ──→ 记录响应结果、耗时、错误信息
```

#### 3.1.5 Lifecycle Hooks

| Hook | 时机 | 行为 |
|------|------|------|
| `OnInit` | 服务启动 | 初始化 Milvus/Redis 连接池、LLM Client |
| `BeforeToolCall` | 工具调用前 | 记录开始时间 |
| `AfterToolCall` | 工具调用后 | 记录耗时、成功/失败指标 |
| `OnShutdown` | 服务关闭 | 优雅关闭连接池、等待进行中请求完成 |

---

### 3.2 Agent Engine

#### 3.2.1 核心接口

```go
// AgentEngine — 引擎入口
type AgentEngine struct {
    llmClient    LLMClient        // LLM API 调用 (OpenAI 兼容)
    toolRegistry *ToolRegistry    // 工具注册表
    ctxManager   *ContextManager  // 上下文管理
    maxSteps     int              // 最大推理步数（防无限循环）
}

// Message — Agent Loop 中的消息
type Message struct {
    Role       string     // system / user / assistant / tool
    Content    string
    ToolCalls  []ToolCall // assistant 的工具调用请求
    ToolCallID string     // tool 响应对应的调用 ID
}

// ToolCall — Function Call 结构
type ToolCall struct {
    ID   string
    Name string          // 工具名称
    Args json.RawMessage // 工具参数
}

// Tool — 工具接口
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage // JSON Schema
    Execute(ctx context.Context, args json.RawMessage) (string, error)
}
```

#### 3.2.2 Agent Loop 执行流程（ReAct 模式）

```
Run(userQuery string) → string

1. 构建初始消息: [system prompt, user query]
2. Loop (最多 maxSteps 轮):
   a. 调用 LLM，传入消息历史 + 工具定义
   b. 如果 LLM 返回纯文本 → 结束，返回结果
   c. 如果 LLM 返回 tool_calls:
      - 遍历每个 tool_call
      - 从 ToolRegistry 查找工具
      - 执行工具，收集结果
      - 将 assistant message + tool results 追加到消息历史
   d. 继续下一轮
3. 超过 maxSteps → 返回已有结果 + 截断提示
```

#### 3.2.3 Agent Loop 示例

```
用户查询: "查询用户登录接口的参数和调用示例"

第1轮 Reason: 需要搜索登录相关 API
第1轮 Act:    search_api(query="用户登录", top_k=5)
第1轮 Observe: 找到 3 个相关接口 [POST /user/login, POST /user/createWithList, ...]

第2轮 Reason: POST /user/login 最匹配，获取完整详情
第2轮 Act:    get_api_detail(service="petstore", endpoint="POST /user/login")
第2轮 Observe: 拿到完整请求/响应 Schema

第3轮 Reason: 信息足够，生成 Go 调用示例
第3轮 Act:    generate_example(endpoint="POST /user/login", language="go")
第3轮 Observe: 生成了完整代码示例

第4轮 Reason: 所有信息齐全，汇总输出
第4轮 Finish: 返回结构化回答（接口说明 + 参数表 + 代码示例）
```

#### 3.2.4 Context Manager

| 功能 | 说明 |
|------|------|
| 消息历史维护 | 维护当前对话的完整 message list |
| Token 预算控制 | 估算当前上下文 token 数，超限时截断早期 tool 结果 |
| System Prompt 注入 | 根据查询场景动态拼接 system prompt（包含 Skills 模板匹配结果） |

---

### 3.3 RAG 检索系统

#### 3.3.1 检索流程

```
用户查询
  │
  ▼
Query 预处理 ──→ 清洗、提取关键词、意图识别
  │
  ▼
Embedding ────→ bge-large-zh-v1.5 向量化 (dim=1024)
  │
  ▼
Milvus 检索 ──→ ANN 近似最近邻搜索, top_k=20
  │
  ▼
Rerank ───────→ bge-reranker-v2-m3 重排序, top_n=5
  │
  ▼
结果组装 ─────→ 拼接文档片段，注入 Agent 上下文
```

#### 3.3.2 文档分块策略

API 文档按**语义结构**分块（非固定长度切分）：

一个 Swagger 接口拆为以下 chunks：

| Chunk 类型 | 内容 | 示例 |
|-----------|------|------|
| `overview` | 接口概要：方法、路径、描述、Tags | POST /user/login - 用户登录接口 |
| `request` | 请求参数：参数名、类型、是否必填、描述 | username: string (required) |
| `response` | 响应结构：状态码、字段、错误码 | 200 OK: { token, user_id } |
| `dependency` | 依赖关系：上下游接口、鉴权要求 | 被依赖: order-service |

#### 3.3.3 Milvus Collection 设计

```
Collection: api_documents

Fields:
├── id          (int64, PK, auto)
├── chunk_id    (varchar)       // "petstore:POST:/user/login:overview"
├── service     (varchar)       // "petstore"
├── endpoint    (varchar)       // "POST /user/login"
├── chunk_type  (varchar)       // overview | request | response | dependency
├── content     (varchar)       // 分块文本内容
├── embedding   (float_vector, dim=1024)  // bge-large-zh-v1.5 输出
└── version     (varchar)       // "v1.0.0"

Index:
├── embedding: IVF_FLAT, nlist=128, metric=IP (内积)
└── service: 标量索引 (用于过滤)
```

#### 3.3.4 Redis 缓存层

| Key 模式 | 用途 | TTL |
|----------|------|-----|
| `api:detail:{service}:{endpoint}` | 接口完整详情缓存 | 1h |
| `api:deps:{endpoint}` | 依赖关系缓存 | 1h |
| `skill:match:{query_hash}` | Skills 匹配结果缓存 | 30min |
| `services:list` | 服务列表缓存 | 5min |

---

### 3.4 知识库管理

#### 3.4.1 数据录入流程

```
Swagger/OpenAPI JSON/YAML 文件
         │
         ▼
┌────────────────┐
│ Swagger Parser │ 解析 paths, schemas, tags
└────────┬───────┘
         ▼
┌────────────────┐
│  Chunk Builder │ 按语义结构拆分 (概要/参数/响应/依赖)
└────────┬───────┘
         ▼
┌────────────────┐
│   Embedding    │ bge-large-zh-v1.5 批量向量化
└────────┬───────┘
         ▼
┌────────────────┐
│ Milvus Upsert  │ 写入向量库 (基于 chunk_id 去重)
└────────┬───────┘
         ▼
┌────────────────┐
│  Redis 刷新    │ 清除受影响的缓存
└────────────────┘

触发方式:
  • MCP Tool: parse_swagger (Claude Code 中触发)
  • HTTP Webhook: POST /webhook/sync
```

#### 3.4.2 Skills 技能模板系统

Skills 是预定义的查询模板，将高频场景封装为结构化引导流程：

```yaml
# skills/auth_flow.yaml
name: "登录鉴权流程"
description: "查询完整的用户登录鉴权链路"
tags: ["auth", "login", "token"]
steps:
  - tool: search_api
    args:
      query: "用户登录认证"
      top_k: 5
  - tool: analyze_dependencies
    args:
      endpoint: "{{matched_endpoint}}"
  - tool: generate_example
    args:
      endpoint: "{{matched_endpoint}}"
      language: "go"
```

```yaml
# skills/crud_guide.yaml
name: "CRUD 接口指南"
description: "查询某个资源的增删改查全套接口"
tags: ["crud", "resource"]
steps:
  - tool: search_api
    args:
      query: "{{resource_name}} 增删改查"
      top_k: 10
  - tool: generate_example
    args:
      endpoint: "{{each_matched}}"
      language: "{{preferred_language}}"
```

Skills 匹配机制：

1. **关键词匹配**：查询关键词 vs skill.tags
2. **语义匹配**：查询 embedding vs skill.description embedding
3. Agent 按匹配到的 Skill.steps 编排工具调用（作为参考，Agent 可动态调整）

---

## 四、安全设计

### 4.1 安全架构

```
外部网络 → HTTPS → Auth MW → MCP Server → Agent (信息过滤网关)
                                              ↓ 内部调用
                                         Milvus / Redis
                                      (企业数据不出服务端)
```

### 4.2 安全措施

| 安全层 | 措施 | 说明 |
|--------|------|------|
| 传输层 | HTTPS (TLS) | 防止中间人攻击和数据窃听 |
| 认证层 | Bearer Token | 拒绝未授权访问 |
| 限流层 | 每分钟请求数限制 | 防止滥用和资源耗尽 |
| 数据层 | Agent 信息过滤 | 原始检索数据不直接暴露给客户端 |
| 审计层 | 请求日志 | 记录每次工具调用的来源、参数、结果 |

### 4.3 Agent 作为信息过滤网关

| 对比 | 无 Agent（原子工具直接返回） | 有 Agent（汇总后返回） |
|------|--------------------------|---------------------|
| 客户端可见内容 | 原始向量检索结果、完整文档片段、内部路径 | 仅经过汇总的结构化回答 |
| 数据暴露面 | 大 | 小 |
| 适用场景 | 内部工具 | 对外提供服务 |

---

## 五、项目结构

```
api-assistant/
├── cmd/
│   └── server/
│       └── main.go                # 入口，启动 HTTP MCP Server
│
├── internal/
│   ├── mcp/                       # MCP 协议层
│   │   ├── server.go              # Server 初始化，Tools/Resources/Prompts 注册
│   │   ├── middleware.go           # Auth, Logging, RateLimit, Validation
│   │   └── hooks.go               # 生命周期钩子
│   │
│   ├── agent/                     # Agent 引擎
│   │   ├── engine.go              # AgentEngine 核心，Agent Loop 实现
│   │   ├── context.go             # ContextManager，消息历史 + Token 预算
│   │   └── llm.go                 # LLM Client 封装 (OpenAI 兼容接口)
│   │
│   ├── tools/                     # 工具实现
│   │   ├── registry.go            # ToolRegistry，工具注册与调度
│   │   ├── search_api.go          # RAG 语义检索
│   │   ├── get_api_detail.go      # 精确查询接口详情
│   │   ├── analyze_deps.go        # 依赖关系分析
│   │   ├── generate_example.go    # 代码示例生成
│   │   ├── match_skill.go         # Skills 模板匹配
│   │   ├── validate_params.go     # 参数校验
│   │   └── parse_swagger.go       # Swagger 文件解析导入
│   │
│   ├── rag/                       # RAG 检索引擎
│   │   ├── embedder.go            # Embedding 封装 (bge-large-zh-v1.5)
│   │   ├── milvus.go              # Milvus 向量库操作
│   │   ├── reranker.go            # Rerank 重排序
│   │   └── chunker.go             # 文档分块策略
│   │
│   ├── knowledge/                 # 知识库管理
│   │   ├── swagger_parser.go      # OpenAPI/Swagger 解析器
│   │   ├── ingestor.go            # 文档录入流程编排
│   │   └── skills.go              # Skills 模板加载与匹配
│   │
│   └── store/                     # 存储层
│       ├── milvus_client.go       # Milvus 连接管理
│       └── redis_client.go        # Redis 连接管理
│
├── config/
│   ├── config.go                  # 配置结构体定义
│   └── config.yaml                # 默认配置文件
│
├── skills/                        # Skills 模板文件
│   ├── auth_flow.yaml
│   └── crud_guide.yaml
│
├── testdata/                      # 测试数据
│   └── petstore.json              # Swagger Petstore
│
├── deploy/
│   ├── Dockerfile
│   └── docker-compose.yaml        # 一键部署 (Server + Milvus + Redis)
│
├── docs/
│   └── design.md                  # 本文档
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## 六、配置文件

```yaml
# config/config.yaml

server:
  port: 8080
  auth_token: "${AUTH_TOKEN}"        # 环境变量注入

llm:
  provider: "openai"                 # openai 兼容接口
  api_key: "${LLM_API_KEY}"          # 环境变量注入
  model: "gpt-4o-mini"
  base_url: ""                       # 可选，接入 DeepSeek 等
  max_tokens: 4096

agent:
  max_steps: 10                      # Agent Loop 最大轮数
  temperature: 0.1

rag:
  embedding_model: "bge-large-zh-v1.5"
  embedding_dim: 1024
  rerank_model: "bge-reranker-v2-m3"
  top_k: 20                          # Milvus 初检数量
  top_n: 5                           # Rerank 后数量

milvus:
  address: "localhost:19530"
  collection: "api_documents"

redis:
  address: "localhost:6379"
  db: 0
```

---

## 七、部署方案

### 7.1 Docker Compose 一键部署

```yaml
# deploy/docker-compose.yaml
version: "3.8"

services:
  api-assistant:
    build: ..
    ports:
      - "8080:8080"
    environment:
      - LLM_API_KEY=${LLM_API_KEY}
      - AUTH_TOKEN=${AUTH_TOKEN}
    depends_on:
      - milvus-standalone
      - redis

  milvus-standalone:
    image: milvusdb/milvus:v2.4-latest
    command: ["milvus", "run", "standalone"]
    ports:
      - "19530:19530"
    volumes:
      - milvus_data:/var/lib/milvus
    depends_on:
      - etcd
      - minio

  etcd:
    image: quay.io/coreos/etcd:v3.5.5
    environment:
      - ETCD_AUTO_COMPACTION_MODE=revision
      - ETCD_AUTO_COMPACTION_RETENTION=1000
      - ETCD_QUOTA_BACKEND_BYTES=4294967296

  minio:
    image: minio/minio:latest
    environment:
      - MINIO_ACCESS_KEY=minioadmin
      - MINIO_SECRET_KEY=minioadmin
    command: minio server /data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

volumes:
  milvus_data:
```

### 7.2 核心依赖

| 依赖 | 用途 |
|------|------|
| `github.com/mark3labs/mcp-go` | MCP SDK，处理协议层 + Streamable HTTP |
| `github.com/milvus-io/milvus-sdk-go` | Milvus 向量库客户端 |
| `github.com/redis/go-redis/v9` | Redis 客户端 |
| `github.com/sashabaranov/go-openai` | OpenAI 兼容 API 客户端 (LLM + Embedding) |
| `gopkg.in/yaml.v3` | 配置文件 + Skills 模板解析 |

### 7.3 开发与演示命令

```makefile
# Makefile

dev:           # 启动基础设施 (Milvus + Redis)
	cd deploy && docker-compose up -d milvus-standalone etcd minio redis

run:           # 启动 MCP Server
	go run cmd/server/main.go run

deploy:        # 全部服务一键部署
	cd deploy && docker-compose up -d

test:          # 运行测试
	go test ./...
```

---

## 八、演示场景

### 场景 1：自然语言查询 API

```
用户在 Claude Code 中:
> 查询宠物商店的用户登录接口，需要参数说明和 Go 调用示例

Agent 执行链路:
  search_api → get_api_detail → generate_example → 汇总输出

预期输出:
  - 接口说明: POST /user/login
  - 参数表: username (string, required), password (string, required)
  - Go 代码示例: 完整的 http.NewRequest 调用代码
```

### 场景 2：接口依赖分析

```
用户:
> 分析下单流程涉及哪些接口

Agent 执行链路:
  search_api → analyze_dependencies → 汇总输出

预期输出:
  - 下单流程: 查询库存 → 创建订单 → 扣减库存
  - 依赖关系图
```

### 场景 3：Skills 模板匹配

```
用户:
> 我想了解 Pet 资源的 CRUD 操作

Agent 执行链路:
  match_skill (匹配 crud_guide) → search_api → generate_example → 汇总输出

预期输出:
  - 匹配到 "CRUD 接口指南" 模板
  - 列出 Pet 的增删改查全部接口 + 代码示例
```

### 场景 4：直接导入 Swagger 文档

```
用户:
> 帮我导入这个 API 文档 https://petstore.swagger.io/v2/swagger.json

直接调用 parse_swagger 工具 (不走 Agent):
  下载 → 解析 → 分块 → 向量化 → 写入 Milvus

预期输出:
  - 导入成功: 13 个接口, 52 个文档片段
```
