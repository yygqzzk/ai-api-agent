# 万知 (WanZhi)

> 企业级 API 文档智能问答助手

万知是一个专为企业 API 文档设计的单领域 Agent，通过自然语言查询接口文档、生成调用示例、分析参数依赖。基于 Go + Gin + Milvus 构建，提供 MCP JSON-RPC 和 Chat SSE 两种接入方式。

## 特性

- **🤖 Adaptive Agentic RAG**：智能策略选择，根据查询复杂度自动路由
- **📚 Swagger 文档导入**：支持 OpenAPI 2.0 规范，自动解析接口元数据
- **🔍 语义检索**：基于向量数据库的高精度 API 搜索
- **🔄 可选重排序**：支持二次精排，提升召回准确率
- **📡 双接入模式**：
  - **MCP JSON-RPC**：标准 JSON-RPC 2.0 协议，适合工具集成
  - **Chat SSE**：Server-Sent Events 流式响应，适合对话式交互
- **🛡️ 企业级特性**：认证、限流、熔断、重试、健康检查

## 架构

```
┌─────────────────┐
│  接入层          │  MCP JSON-RPC  |  Chat SSE
│  (Transport)    │  /mcp          │  /api/chat
└─────────────────┘
        │
┌─────────────────┐
│  Agent 核心      │  QueryRunner (策略选择/改写/规划/反思)
└─────────────────┘
        │
┌─────────────────┐
│  工具层          │  Knowledge Base (API 文档 + RAG 检索)
└─────────────────┘
        │
┌─────────────────┐
│  基础设施        │  Milvus (向量) | Redis (缓存) | LLM
└─────────────────┘
```

## 快速开始

### 1. 启动依赖

```bash
make dev
```

这会启动：
- **Milvus**：向量数据库（localhost:19530）
- **Redis**：缓存和持久化（localhost:6379）
- **MinIO**：对象存储（可选）

### 2. 启动服务

```bash
# 基础模式（规则式 LLM，无需 API Key）
AUTH_TOKEN=demo-token go run cmd/server/main.go run

# 完整模式（OpenAI 兼容 LLM）
AUTH_TOKEN=demo-token \
LLM_API_KEY=your-key \
LLM_BASE_URL=https://api.openai.com \
LLM_MODEL=gpt-4o-mini \
go run cmd/server/main.go run
```

### 3. 测试 API

**方式一：MCP JSON-RPC**

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"query_api",
    "params":{"query":"查询用户登录接口"}
  }'
```

**方式二：Chat SSE**

```bash
curl -N -X POST http://localhost:8080/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"查询用户登录接口的参数和 Go 示例"}'
```

## 配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `PORT` | 8080 | 服务端口 |
| `AUTH_TOKEN` | (空) | MCP 接口认证 Token |
| `LLM_API_KEY` | (空) | LLM API Key |
| `LLM_BASE_URL` | (空) | LLM API Endpoint |
| `LLM_MODEL` | gpt-4o-mini | 模型名称 |
| `REDIS_ADDRESS` | localhost:6379 | Redis 地址 |
| `MILVUS_ADDRESS` | localhost:19530 | Milvus 地址 |

## 运行测试

```bash
# 单元测试
go test ./...

# 覆盖率报告
go test -cover ./...

# E2E 测试（需要真实依赖）
RUN_REAL_REDIS_MILVUS_E2E=1 go test ./internal/e2e/...
```

## 项目结构

```
cmd/server/          # 服务入口
internal/
  ├── agent/         # Agent 引擎（ReAct + Adaptive）
  ├── config/        # 配置加载（caarlos0/env）
  ├── knowledge/     # Swagger 解析和知识模型
  ├── mcp/           # MCP 服务器（Gin 中间件 + JSON-RPC）
  ├── rag/           # RAG 检索（MemoryStore / MilvusStore）
  ├── tools/         # 工具实现（query_api / parse_swagger 等）
  ├── transport/     # 接入层（Chat SSE）
  └── webhook/       # Webhook 同步
```

## 技术栈

- **Go 1.25**
- **Gin**：HTTP 路由框架
- **Milvus**：向量数据库
- **Redis**：缓存和持久化
- **OpenAI-compatible LLM**：大模型（支持 Function Calling）

## 文档

- [设计文档](docs/design.md)
- [本地开发指南](docs/local-setup-guide.md)
- [弹性实现说明](docs/resilience-implementation.md)
- [快速参考](QUICKSTART.md)
