# 🚀 快速参考卡片

## 一键启动（最简单）

```bash
# 1. 设置 Token
export AUTH_TOKEN="demo-token"

# 2. 启动服务
go run cmd/server/main.go run

# 3. 导入数据
go run cmd/server/main.go ingest --file testdata/petstore.json --service petstore

# 4. 测试查询
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"search_api","params":{"query":"user","top_k":5}}'
```

---

## 环境变量速查

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AUTH_TOKEN` | (空) | **必填** - Bearer Token |
| `LLM_API_KEY` | (空) | LLM API 密钥 |
| `LLM_MODEL` | `gpt-4o-mini` | 模型名称 |
| `MILVUS_MODE` | `memory` | `memory` 或 `milvus` |
| `REDIS_MODE` | `memory` | `memory` 或 `redis` |

---

## 常用命令

```bash
# 启动服务
go run cmd/server/main.go run

# 导入数据
go run cmd/server/main.go ingest --file <file> --service <name>

# 运行测试
go test ./...

# 启动基础设施
make dev

# 健康检查
curl http://localhost:8080/healthz

# 查看指标
curl http://localhost:8080/metrics
```

---

## API 端点

| 端点 | 说明 |
|------|------|
| `POST /mcp` | MCP JSON-RPC 接口 |
| `GET /healthz` | 健康检查 |
| `GET /metrics` | Prometheus 指标 |

---

## 核心工具

| 工具 | 说明 |
|------|------|
| `search_api` | 语义检索 API |
| `query_api` | Agent 智能查询 |
| `get_api_detail` | 获取接口详情 |
| `generate_example` | 生成代码示例 |
| `parse_swagger` | 导入 Swagger 文档 |

---

## 测试用例

```bash
# 基础查询
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"search_api","params":{"query":"user","top_k":5}}'

# Agent 查询
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"query_api","params":{"query":"查询用户登录接口"}}'

# 获取详情
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"get_api_detail","params":{"service":"petstore","endpoint":"POST /user/login"}}'
```

---

## 故障排查

| 问题 | 解决方案 |
|------|----------|
| 连接被拒绝 | 检查 Docker 服务：`make dev` |
| LLM 超时 | 增加超时：`export LLM_TIMEOUT_SECONDS=60` |
| 查询无结果 | 确认已导入数据 |
| 内存占用高 | 使用内存模式：`export MILVUS_MODE=memory` |

---

## 文档链接

- [完整配置指南](docs/local-setup-guide.md)
- [设计文档](docs/design.md)
- [容错机制](docs/resilience-implementation.md)
- [设置完成总结](docs/SETUP-COMPLETE.md)
