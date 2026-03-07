# Agentic RAG Enhancement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为当前 API Assistant 增加 Agentic RAG 核心能力，并补上 webhook/ingest 写入链路的最小可用实现。

**Architecture:** 保留现有 `AgentEngine` 作为底层 ReAct 执行器，在其外层新增 `AdaptiveAgentEngine` 做策略选择、查询改写、规划执行和反思重试。写入侧新增 `internal/ingest` 与 `internal/webhook`，尽量复用现有 `knowledge`、`rag`、`tools.KnowledgeBase` 抽象，避免重复构建解析与索引逻辑。

**Tech Stack:** Go, HTTP, JSON, 现有 `internal/agent` / `internal/tools` / `internal/knowledge` / `internal/rag` 组件。

### Task 1: 修复配置基线并补计划入口

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `config/config.yaml`
- Modify: `cmd/server/health.go`
- Modify: `cmd/server/health_test.go`
- Modify: `test_embedding.go`
- Modify: `test_llm.go`
- Modify: `test_rerank.go`

**Step 1: Write the failing test**

```go
func TestApplyEnvOverrides(t *testing.T) {
    cfg := Default()
    if err := cfg.ApplyEnv(os.LookupEnv); err != nil {
        t.Fatalf("ApplyEnv failed: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config ./cmd/server -run 'TestApplyEnvOverrides|TestHealthzAllHealthy' -v`
Expected: FAIL，原因是 `config.Config` 仍引用已删除的 `Mode` 字段。

**Step 3: Write minimal implementation**

```go
type MilvusConfig struct {
    Address    string
    Collection string
}

type RedisConfig struct {
    Address string
    DB      int
}
```

同时删除对 `MILVUS_MODE` / `REDIS_MODE` 的读取、更新健康检查逻辑，并给根目录临时脚本添加 `//go:build ignore`，避免 `go test ./...` 时多个 `main` 冲突。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config ./cmd/server -run 'TestApplyEnvOverrides|TestHealthzAllHealthy|TestHealthzDependencyDown' -v`
Expected: PASS。

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go config/config.yaml cmd/server/health.go cmd/server/health_test.go test_embedding.go test_llm.go test_rerank.go
git commit -m "refactor: simplify runtime config and test helpers"
```

### Task 2: 实现 Agentic RAG 核心模块

**Files:**
- Create: `internal/agent/query_rewriter.go`
- Create: `internal/agent/query_rewriter_test.go`
- Create: `internal/agent/strategy_selector.go`
- Create: `internal/agent/strategy_selector_test.go`
- Create: `internal/agent/planner.go`
- Create: `internal/agent/planner_test.go`
- Create: `internal/agent/reflector.go`
- Create: `internal/agent/reflector_test.go`

**Step 1: Write the failing test**

```go
func TestRuleBasedStrategySelectorSelectsComplexForWorkflowQuestion(t *testing.T) {
    selector := NewRuleBasedStrategySelector()
    got, err := selector.Select(context.Background(), "分析用户注册到下单的完整流程")
    if err != nil {
        t.Fatalf("Select failed: %v", err)
    }
    if got != StrategyComplex {
        t.Fatalf("expected %q, got %q", StrategyComplex, got)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run 'TestRuleBasedStrategySelector|TestLLMQueryRewriter|TestRuleBasedPlanner|TestRuleBasedReflector' -v`
Expected: FAIL，原因是相关类型和实现尚不存在。

**Step 3: Write minimal implementation**

```go
type Strategy string

const (
    StrategySimple Strategy = "simple"
    StrategyComplex Strategy = "complex"
    StrategyAmbiguous Strategy = "ambiguous"
)
```

实现：
- `RuleBasedStrategySelector`：根据“流程/依赖/步骤”等词判定复杂查询，根据“登录接口/这个接口/查订单”等短模糊问句判定模糊查询。
- `LLMQueryRewriter`：优先使用 LLM，失败时退化到规则改写。
- `Planner`：生成 `search_api` / `analyze_dependencies` 子任务。
- `Reflector`：基于输出内容判断质量与是否应重试。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent -run 'TestRuleBasedStrategySelector|TestLLMQueryRewriter|TestRuleBasedPlanner|TestRuleBasedReflector' -v`
Expected: PASS。

**Step 5: Commit**

```bash
git add internal/agent/query_rewriter.go internal/agent/query_rewriter_test.go internal/agent/strategy_selector.go internal/agent/strategy_selector_test.go internal/agent/planner.go internal/agent/planner_test.go internal/agent/reflector.go internal/agent/reflector_test.go
git commit -m "feat: add adaptive rag core modules"
```

### Task 3: 集成 AdaptiveAgentEngine 到查询入口

**Files:**
- Create: `internal/agent/adaptive_engine.go`
- Create: `internal/agent/adaptive_engine_test.go`
- Modify: `cmd/server/main.go`
- Modify: `internal/tools/query_api.go`

**Step 1: Write the failing test**

```go
func TestAdaptiveAgentEngineRunComplexExecutesPlanTasks(t *testing.T) {
    engine := NewAdaptiveAgentEngine(...)
    out, err := engine.Run(context.Background(), "分析用户注册到下单流程")
    if err != nil {
        t.Fatalf("Run failed: %v", err)
    }
    if !strings.Contains(out, "分析结果") {
        t.Fatalf("unexpected output: %s", out)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run 'TestAdaptiveAgentEngine' -v`
Expected: FAIL，原因是 `AdaptiveAgentEngine` 尚未实现。

**Step 3: Write minimal implementation**

```go
func (e *AdaptiveAgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
    strategy, err := e.selector.Select(ctx, userQuery)
    if err != nil {
        return "", err
    }
    // simple -> baseEngine
    // complex -> planner + dispatcher
    // ambiguous -> rewriter + select best
}
```

同时在 `cmd/server/main.go` 中将 `query_api` 的 runner 从 `AgentEngine` 替换为 `AdaptiveAgentEngine`。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent ./internal/tools -run 'TestAdaptiveAgentEngine|TestQueryAPITool' -v`
Expected: PASS。

**Step 5: Commit**

```bash
git add internal/agent/adaptive_engine.go internal/agent/adaptive_engine_test.go cmd/server/main.go internal/tools/query_api.go
git commit -m "feat: integrate adaptive agent engine"
```

### Task 4: 增加 ingest/webhook 写入侧最小实现

**Files:**
- Create: `internal/ingest/service.go`
- Create: `internal/ingest/service_test.go`
- Create: `internal/webhook/handler.go`
- Create: `internal/webhook/handler_test.go`
- Create: `.github/workflows/sync-api-docs.yml`
- Modify: `cmd/server/main.go`

**Step 1: Write the failing test**

```go
func TestWebhookHandlerAcceptsBearerTokenAndQueuesSync(t *testing.T) {
    handler := NewWebhookHandler(service, "", "demo-token")
    req := httptest.NewRequest(http.MethodPost, "/webhook/sync", bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer demo-token")
    rr := httptest.NewRecorder()
    handler.HandleSync(rr, req)
    if rr.Code != http.StatusAccepted {
        t.Fatalf("expected 202, got %d", rr.Code)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/webhook ./internal/ingest -run 'TestWebhookHandler|TestServiceIngest' -v`
Expected: FAIL，原因是服务和处理器尚不存在。

**Step 3: Write minimal implementation**

```go
type Service struct {
    kb         KnowledgeBaseIngestor
    httpClient *http.Client
    cacheTTL   time.Duration
}
```

实现：
- 支持 `content`、`content_url`、本地文件三种输入。
- webhook 支持 GitHub 签名或 Bearer Token 二选一认证。
- 注册 `/webhook/sync` 路由并返回 `202 Accepted`。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/webhook ./internal/ingest ./cmd/server -run 'TestWebhookHandler|TestServiceIngest' -v`
Expected: PASS。

**Step 5: Commit**

```bash
git add internal/ingest/service.go internal/ingest/service_test.go internal/webhook/handler.go internal/webhook/handler_test.go .github/workflows/sync-api-docs.yml cmd/server/main.go
git commit -m "feat: add webhook sync ingest pipeline"
```
