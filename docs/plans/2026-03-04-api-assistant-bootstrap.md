# API Assistant Bootstrap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 从 0 到 1 实现可运行的企业级 API 助手 MVP，覆盖 Swagger 导入、检索工具、Agent 汇总、HTTP `/mcp` 服务与基础测试。

**Architecture:** 采用 Go 单体分层结构。`knowledge/rag/tools/agent/mcp` 分层解耦，底层先用内存存储实现 Milvus/Redis 抽象，保证接口稳定。`query_api` 走 Agent 编排，其他工具支持直接调用。

**Tech Stack:** Go 1.25, net/http, encoding/json, testing, 标准库优先（预留外部依赖扩展点）。

### Task 1: 初始化工程与配置骨架

**Files:**
- Create: `go.mod`
- Create: `cmd/server/main.go`
- Create: `internal/config/config.go`
- Create: `config/config.yaml`
- Create: `Makefile`

**Step 1: 写失败测试（配置加载）**
- 新建 `internal/config/config_test.go`，断言默认值与环境变量覆盖逻辑。

**Step 2: 运行测试并确认失败**
- Run: `go test ./internal/config -v`
- Expected: FAIL（配置加载函数不存在）。

**Step 3: 最小实现**
- 实现 `Config` 结构体、默认值构造、环境变量覆盖函数。
- 在 `main.go` 增加 `run`/`ingest` 命令骨架。

**Step 4: 重新运行测试**
- Run: `go test ./internal/config -v`
- Expected: PASS。

### Task 2: Swagger 解析与知识模型

**Files:**
- Create: `internal/knowledge/swagger_parser.go`
- Create: `internal/knowledge/models.go`
- Create: `internal/knowledge/ingestor.go`
- Create: `testdata/petstore.json`
- Create: `internal/knowledge/swagger_parser_test.go`

**Step 1: 写失败测试**
- 用 `testdata/petstore.json` 验证解析接口数量、参数、响应码。

**Step 2: 验证失败**
- Run: `go test ./internal/knowledge -v`
- Expected: FAIL（解析器未实现）。

**Step 3: 最小实现**
- 实现 Swagger v2 核心字段解析（`paths`、`parameters`、`responses`）。
- 生成统一 `Endpoint`/`Operation` 模型。

**Step 4: 重新验证**
- Run: `go test ./internal/knowledge -v`
- Expected: PASS。

### Task 3: RAG 内存检索层

**Files:**
- Create: `internal/rag/chunker.go`
- Create: `internal/rag/store.go`
- Create: `internal/rag/search.go`
- Create: `internal/rag/search_test.go`

**Step 1: 写失败测试**
- 断言分块类型（overview/request/response/dependency）生成正确。
- 断言关键词检索返回排序正确。

**Step 2: 验证失败**
- Run: `go test ./internal/rag -v`
- Expected: FAIL（分块/检索不存在）。

**Step 3: 最小实现**
- 语义分块与简单 BM25-like 打分（关键词命中 + endpoint 命中权重）。
- `top_k` 截断与 service 过滤支持。

**Step 4: 重新验证**
- Run: `go test ./internal/rag -v`
- Expected: PASS。

### Task 4: Tool Registry 与核心工具

**Files:**
- Create: `internal/tools/registry.go`
- Create: `internal/tools/types.go`
- Create: `internal/tools/search_api.go`
- Create: `internal/tools/get_api_detail.go`
- Create: `internal/tools/analyze_deps.go`
- Create: `internal/tools/generate_example.go`
- Create: `internal/tools/validate_params.go`
- Create: `internal/tools/match_skill.go`
- Create: `internal/tools/parse_swagger.go`
- Create: `internal/tools/tools_test.go`
- Create: `skills/auth_flow.yaml`
- Create: `skills/crud_guide.yaml`

**Step 1: 写失败测试**
- 覆盖工具注册、参数解码、`search_api`/`get_api_detail`/`generate_example` 基本行为。

**Step 2: 验证失败**
- Run: `go test ./internal/tools -v`
- Expected: FAIL（工具尚未实现）。

**Step 3: 最小实现**
- 实现统一 `Tool` 接口和 JSON Schema 输出。
- 工具逻辑接入 knowledge/rag 服务。

**Step 4: 重新验证**
- Run: `go test ./internal/tools -v`
- Expected: PASS。

### Task 5: Agent Engine（query_api 编排入口）

**Files:**
- Create: `internal/agent/engine.go`
- Create: `internal/agent/context.go`
- Create: `internal/agent/llm.go`
- Create: `internal/agent/engine_test.go`

**Step 1: 写失败测试**
- 断言 `query_api` 查询时按场景调用工具并汇总结构化结果。
- 断言超步数时返回截断提示。

**Step 2: 验证失败**
- Run: `go test ./internal/agent -v`
- Expected: FAIL。

**Step 3: 最小实现**
- 规则驱动 planner + Agent Loop（消息历史、maxSteps、防无限循环）。
- 汇总层仅输出结构化摘要，不回传原始 chunk。

**Step 4: 重新验证**
- Run: `go test ./internal/agent -v`
- Expected: PASS。

### Task 6: MCP HTTP 服务 + 中间件 + 生命周期

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/middleware.go`
- Create: `internal/mcp/hooks.go`
- Create: `internal/mcp/server_test.go`

**Step 1: 写失败测试**
- 覆盖 Bearer 认证、限流、工具分发、错误码语义。

**Step 2: 验证失败**
- Run: `go test ./internal/mcp -v`
- Expected: FAIL。

**Step 3: 最小实现**
- `/mcp` JSON-RPC 风格入口，method 与 tool 映射。
- 中间件链路：Auth → Logging → RateLimit → Validation。
- 生命周期 Hook：OnInit/BeforeToolCall/AfterToolCall/OnShutdown。

**Step 4: 重新验证**
- Run: `go test ./internal/mcp -v`
- Expected: PASS。

### Task 7: 端到端打通与文档

**Files:**
- Create: `README.md`
- Create: `deploy/docker-compose.yaml`
- Create: `deploy/Dockerfile`
- Modify: `cmd/server/main.go`

**Step 1: 写失败测试（最小 e2e）**
- 新建 `internal/e2e/smoke_test.go`，导入 petstore 后调用 `query_api` 断言返回含接口+示例。

**Step 2: 验证失败**
- Run: `go test ./internal/e2e -v`
- Expected: FAIL。

**Step 3: 最小实现**
- 连通 ingest → tools → agent → mcp。
- 在 README 补充运行与演示命令。

**Step 4: 重新验证**
- Run: `go test ./...`
- Expected: PASS。

### Task 8: 完整回归验证

**Files:**
- Modify: `go.mod`（若需要）

**Step 1: 运行全量验证**
- Run: `go test ./...`
- Expected: PASS。

**Step 2: 静态检查（可用则执行）**
- Run: `go vet ./...`
- Expected: PASS 或无阻断项。

**Step 3: 手动 smoke**
- Run: `go run cmd/server/main.go`
- 使用 `curl` 调用 `/mcp` 验证 `search_api` 与 `query_api`。

