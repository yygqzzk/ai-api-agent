# 企业级智能 API 助手（MVP）

基于 `docs/design.md` 的 Go 实现，提供 MCP 风格 `/mcp` 接口，支持：
- Swagger 文档导入（`parse_swagger`）
- API 语义检索（`search_api`）
- 详情查询、依赖分析、示例生成、参数校验
- `query_api` Adaptive Agentic RAG 查询入口（策略选择、改写、规划、反思）
- `/healthz` 健康探针（Redis / Milvus / LLM）
- `/webhook/sync` API 文档自动同步入口

## 项目结构

- `cmd/server/main.go`：服务入口与 `ingest` 子命令
- `internal/mcp`：HTTP MCP Server + Middleware + Hooks
- `internal/agent`：基础 ReAct 引擎 + Adaptive Agentic RAG 核心模块
- `internal/tools`：全部工具实现与注册
- `internal/knowledge`：Swagger 解析与知识模型
- `internal/rag`：分块与内存检索
- `internal/store`：Milvus/Redis 客户端抽象（服务入口默认走真实依赖）

## 依赖准备

服务入口默认连接 Redis 与 Milvus，本地开发前请先启动依赖：

```bash
make dev
```

## 快速开始

```bash
go test ./...
```

```bash
# 启动服务（默认端口 8080）
AUTH_TOKEN=demo-token go run cmd/server/main.go run
```

```bash
# 使用 OpenAI 兼容 LLM（Function Calling）
AUTH_TOKEN=demo-token \
LLM_PROVIDER=openai \
LLM_API_KEY=your-key \
LLM_MODEL=gpt-4o-mini \
LLM_BASE_URL=https://api.openai.com \
LLM_TIMEOUT_SECONDS=30 \
LLM_MAX_RETRIES=2 \
LLM_RETRY_BACKOFF_MS=200 \
go run cmd/server/main.go run
```

```bash
# 使用 OpenAI 兼容 LLM + Redis/Milvus 依赖运行服务
AUTH_TOKEN=demo-token \
WEBHOOK_SECRET=demo-webhook-secret \
REDIS_ADDRESS=127.0.0.1:6379 \
MILVUS_ADDRESS=127.0.0.1:19530 \
go run cmd/server/main.go run
```

```bash
# 导入 Swagger 文件
go run cmd/server/main.go ingest --file testdata/petstore.json --service petstore
```

## 调用示例

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"query_api",
    "params":{"query":"查询用户登录接口参数和go示例"}
  }'
```

健康检查：

```bash
curl http://localhost:8080/healthz
```

Webhook 同步：

```bash
curl -X POST http://localhost:8080/webhook/sync \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "event":"push",
    "repository":"company/api-docs",
    "branch":"main",
    "files":[{
      "path":"docs/api/user-service.json",
      "service":"user-service",
      "content":"{\"swagger\":\"2.0\",\"info\":{\"title\":\"User Service\",\"version\":\"1.0.0\"},\"paths\":{}}"
    }]
  }'
```

## 当前实现说明

- 已实现设计文档中的核心链路与模块分层。
- Agent 已支持 OpenAI 兼容 Chat Completions + Function Calling，并注入工具 schema。
- `query_api` 已切换为 Adaptive Agentic RAG runner：支持简单/复杂/模糊查询的差异化处理。
- 新增写入侧 `internal/ingest` + `internal/webhook`，支持通过 webhook 自动同步 API 文档。
- OpenAI 客户端支持 429/5xx 自动重试（可配 `LLM_MAX_RETRIES`、`LLM_RETRY_BACKOFF_MS`）与请求超时（`LLM_TIMEOUT_SECONDS`）。
- 当未提供可用 LLM 配置时，会自动回退到规则式 LLM（便于本地离线演示）。
- `query_api` 在保持 `summary` 文本兼容的同时，新增结构化 `trace` 数组用于可观测性。
- 知识库接口详情查询会自动读写 Redis 缓存；服务入口默认使用真实 Redis/Milvus。
- `query_api` 是唯一触发 Agent Loop 的入口，Agent 内部再调度其他工具。
