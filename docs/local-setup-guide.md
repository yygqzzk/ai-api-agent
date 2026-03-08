# 本地运行完整指南

> **注意（2026-03-08）**：本文中涉及 `MILVUS_MODE`、`REDIS_MODE` 和“纯内存模式”的段落已过时。当前服务运行时要求真实 Redis + Milvus；内存实现仅保留给测试代码使用。

## 📋 前置要求

### 必需
- Go 1.21+
- Docker & Docker Compose（用于运行基础设施）

### 可选（根据运行模式）
- Milvus（向量数据库）- 仅 `MILVUS_MODE=milvus` 时需要
- Redis（缓存）- 仅 `REDIS_MODE=redis` 时需要
- LLM API Key（OpenAI/DeepSeek/Claude）- 仅使用真实 LLM 时需要

---

## 🚀 快速开始（5 分钟）

### 方式一：纯内存模式（无需外部依赖）

```bash
# 1. 设置认证 Token
export AUTH_TOKEN="demo-token"

# 2. 启动服务（使用内存模式）
go run cmd/server/main.go run

# 3. 服务启动时会自动加载默认测试数据
#    如果仓库中存在 testdata/petstore.json，无需手动导入

# 4. 测试查询
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

### 方式二：完整模式（使用真实 LLM + Milvus + Redis）

```bash
# 1. 启动基础设施
make dev
# 等待 Milvus 和 Redis 启动完成（约 30 秒）

# 2. 配置环境变量
export AUTH_TOKEN="demo-token"
export LLM_API_KEY="your-openai-api-key"
export LLM_MODEL="gpt-4o-mini"
export LLM_BASE_URL="https://api.openai.com"  # 可选
export MILVUS_MODE="milvus"
export REDIS_MODE="redis"

# 3. 启动服务
go run cmd/server/main.go run

# 4. 服务启动时会自动加载默认测试数据
#    如果仓库中存在 testdata/petstore.json，无需手动导入

# 5. 测试查询
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

---

## ⚙️ 配置参数详解

### 核心配置（必填）

| 环境变量 | 默认值 | 说明 | 示例 |
|---------|--------|------|------|
| `AUTH_TOKEN` | (空) | Bearer Token 认证 | `demo-token` |
| `LLM_API_KEY` | (空) | LLM API 密钥 | `sk-xxx` |
| `LLM_MODEL` | `gpt-4o-mini` | 模型名称 | `gpt-4o-mini` |

### LLM 配置（可选）

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `LLM_PROVIDER` | `openai` | LLM 提供商 |
| `LLM_BASE_URL` | (空) | 自定义 API 端点 |
| `LLM_MAX_TOKENS` | `4096` | 最大生成 token 数 |
| `LLM_TIMEOUT_SECONDS` | `30` | 请求超时（秒） |
| `LLM_MAX_RETRIES` | `2` | 最大重试次数 |
| `LLM_RETRY_BACKOFF_MS` | `200` | 重试退避时间（毫秒） |

### 存储配置（可选）

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `MILVUS_MODE` | `memory` | `memory` 或 `milvus` |
| `MILVUS_ADDRESS` | `localhost:19530` | Milvus 服务地址 |
| `REDIS_MODE` | `memory` | `memory` 或 `redis` |
| `REDIS_ADDRESS` | `localhost:6379` | Redis 服务地址 |

---

## 🔧 不同场景的配置

### 场景 1：本地开发（最简单）

```bash
# 只需设置 Token，其他全部使用默认值
export AUTH_TOKEN="dev-token"
go run cmd/server/main.go run
```

**特点**：
- ✅ 无需外部依赖
- ✅ 启动速度快
- ⚠️ 使用规则式 LLM（无真实推理能力）
- ⚠️ 数据存储在内存（重启丢失）

---

### 场景 2：测试真实 LLM（推荐）

```bash
# 使用 OpenAI API
export AUTH_TOKEN="test-token"
export LLM_API_KEY="sk-your-openai-key"
export LLM_MODEL="gpt-4o-mini"
go run cmd/server/main.go run
```

**特点**：
- ✅ 真实 LLM 推理能力
- ✅ 无需 Milvus/Redis
- ⚠️ 需要 API Key
- ⚠️ 数据存储在内存

---

### 场景 3：使用 DeepSeek（国内可用）

```bash
export AUTH_TOKEN="test-token"
export LLM_API_KEY="your-deepseek-key"
export LLM_MODEL="deepseek-chat"
export LLM_BASE_URL="https://api.deepseek.com"
go run cmd/server/main.go run
```

**特点**：
- ✅ 国内可用，无需代理
- ✅ 价格便宜（0.14 元/百万 token）
- ✅ 推理能力强

---

### 场景 4：完整生产环境模拟

```bash
# 启动基础设施
make dev

# 配置所有参数
export AUTH_TOKEN="prod-token"
export LLM_API_KEY="your-api-key"
export LLM_MODEL="gpt-4o-mini"
export MILVUS_MODE="milvus"
export REDIS_MODE="redis"

# 启动服务
go run cmd/server/main.go run
```

**特点**：
- ✅ 完整功能
- ✅ 数据持久化
- ✅ 向量检索
- ⚠️ 需要 Docker
- ⚠️ 资源占用较高

---

## 📊 验证服务状态

### 1. 健康检查

```bash
curl http://localhost:8080/healthz
```

**预期输出**：
```json
{
  "status": "healthy",
  "checks": {
    "redis": "ok",
    "milvus": "ok",
    "llm": "ok"
  }
}
```

### 2. Prometheus 指标

```bash
curl http://localhost:8080/metrics
```

**关键指标**：
- `mcp_requests_total` - 总请求数
- `llm_requests_total` - LLM 调用次数
- `llm_tokens_total` - Token 消耗
- `tool_calls_total` - 工具调用次数

---

## 🧪 测试不同功能

### 1. 基础查询

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"search_api",
    "params":{"query":"用户登录","top_k":5}
  }'
```

### 2. Agent 智能查询

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"query_api",
    "params":{"query":"查询用户登录接口的参数和go调用示例"}
  }'
```

### 3. 获取接口详情

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"get_api_detail",
    "params":{"service":"petstore","endpoint":"POST /user/login"}
  }'
```

### 4. 生成代码示例

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"generate_example",
    "params":{"endpoint":"POST /user/login","language":"go"}
  }'
```

---

## 🐛 常见问题

### Q1: 启动时报错 "connection refused"

**原因**：Milvus 或 Redis 未启动

**解决**：
```bash
# 检查服务状态
docker-compose -f deploy/docker-compose.yaml ps

# 重启服务
make dev
```

### Q2: LLM 调用超时

**原因**：网络问题或 API Key 无效

**解决**：
```bash
# 检查 API Key
echo $LLM_API_KEY

# 增加超时时间
export LLM_TIMEOUT_SECONDS=60

# 或使用代理
export LLM_BASE_URL="http://your-proxy:8080"
```

### Q3: 查询结果为空

**原因**：默认示例数据未加载，或尚未在运行中导入自定义文档

**解决**：
```bash
# 确认服务启动日志中包含 default swagger loaded

# 或在运行中的服务里导入测试数据
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"parse_swagger","params":{"file_path":"testdata/petstore.json","service":"petstore"}}'

# 验证数据
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"search_api","params":{"query":"user","top_k":10}}'
```

### Q4: 内存占用过高

**原因**：Milvus 或 Agent 上下文过大

**解决**：
```bash
# 使用内存模式
export MILVUS_MODE="memory"
export REDIS_MODE="memory"

# 减少 Agent 步数
# 修改 config/config.yaml:
# agent:
#   max_steps: 5
```

---

## 📝 推荐配置组合

### 开发环境（快速迭代）
```bash
export AUTH_TOKEN="dev"
# 其他全部默认
```

### 测试环境（验证功能）
```bash
export AUTH_TOKEN="test"
export LLM_API_KEY="sk-xxx"
export LLM_MODEL="gpt-4o-mini"
```

### 演示环境（面试 Demo）
```bash
export AUTH_TOKEN="demo-token"
export LLM_API_KEY="sk-xxx"
export LLM_MODEL="gpt-4o-mini"
export MILVUS_MODE="milvus"
export REDIS_MODE="redis"
make dev && go run cmd/server/main.go run
```

---

## 🎯 下一步

1. ✅ 导入你自己的 Swagger 文档
2. ✅ 配置真实的 LLM API Key
3. ✅ 测试不同的查询场景
4. ✅ 查看 Prometheus 指标
5. ✅ 准备面试 Demo 演示脚本

---

## 📚 相关文档

- [设计文档](docs/design.md) - 完整架构设计
- [容错机制](docs/resilience-implementation.md) - 熔断器、限流、追踪
- [API 文档](README.md) - 工具列表和使用说明
