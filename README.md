# 企业级智能 API 助手（MVP）

基于 `docs/design.md` 的 Go 实现，提供 MCP 风格 `/mcp` 接口，支持：
- Swagger 文档导入（`parse_swagger`）
- API 语义检索（`search_api`）
- 详情查询、依赖分析、示例生成、参数校验
- `query_api` Agent 编排查询入口（返回 `summary` + 结构化 `trace`）
- `/healthz` 健康探针（Redis / Milvus / LLM）

## 项目结构

- `cmd/server/main.go`：服务入口与 `ingest` 子命令
- `internal/mcp`：HTTP MCP Server + Middleware + Hooks
- `internal/agent`：Agent Loop、上下文管理、规则式 LLM 编排
- `internal/tools`：全部工具实现与注册
- `internal/knowledge`：Swagger 解析与知识模型
- `internal/rag`：分块与内存检索
- `internal/store`：Milvus/Redis 客户端抽象（Redis 已支持真实连接）

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
# 使用真实 Redis 作为缓存层
AUTH_TOKEN=demo-token \
REDIS_MODE=redis \
REDIS_ADDRESS=127.0.0.1:6379 \
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

## 当前实现说明

- 已实现设计文档中的核心链路与模块分层。
- Agent 已支持 OpenAI 兼容 Chat Completions + Function Calling，并注入工具 schema。
- OpenAI 客户端支持 429/5xx 自动重试（可配 `LLM_MAX_RETRIES`、`LLM_RETRY_BACKOFF_MS`）与请求超时（`LLM_TIMEOUT_SECONDS`）。
- 当未提供可用 LLM 配置时，会自动回退到规则式 LLM（便于本地离线演示）。
- `query_api` 在保持 `summary` 文本兼容的同时，新增结构化 `trace` 数组用于可观测性。
- Redis 已支持 `memory/redis` 双模式；知识库接口详情查询会自动读写缓存。
- Milvus 已支持 `memory/milvus` 双模式；`MILVUS_MODE=milvus` 时走真实向量检索。
- `query_api` 是唯一触发 Agent Loop 的入口，Agent 内部再调度其他工具。
