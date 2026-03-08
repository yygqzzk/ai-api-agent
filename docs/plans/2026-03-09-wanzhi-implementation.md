# 万知 (WanZhi) 重构实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 ai-agent-api 重构为万知 (WanZhi)——采用六边形架构、Gin 路由、env 配置的企业 API 文档单领域 Agent，新增 Chat SSE 接入面。

**Architecture:** 六边形分层 (domain / transport / infra)，domain 层零外部依赖。对外暴露 MCP JSON-RPC 和 Chat SSE 两种接入面，共享同一个 Agent 核心 (QueryRunner)。

**Tech Stack:** Go 1.25, gin-gonic/gin, caarlos0/env/v11, Milvus, Redis, OpenAI-compatible LLM

---

## Task 1: 引入 Gin 和 caarlos0/env 依赖

**Files:**
- Modify: `go.mod`

**Step 1: 安装依赖**

Run:
```bash
go get github.com/gin-gonic/gin github.com/caarlos0/env/v11
```

Expected: go.mod 和 go.sum 更新，无报错

**Step 2: 验证依赖可用**

Run:
```bash
go build ./...
```

Expected: 编译通过

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add gin and env dependencies"
```

---

## Task 2: 用 caarlos0/env 重写配置加载

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 2.1: 读取现有测试**

先阅读 `internal/config/config_test.go`，了解测试覆盖情况。

**Step 2.2: 给 Config struct 添加 env tag**

将 `config.go` 中的所有 Config struct 字段添加 `env` tag，替换整个 `ApplyEnv` 方法。

```go
package config

import (
	"github.com/caarlos0/env/v11"
)

type Config struct {
	Server ServerConfig
	LLM    LLMConfig
	Agent  AgentConfig
	RAG    RAGConfig
	Milvus MilvusConfig
	Redis  RedisConfig
}

type ServerConfig struct {
	Port      int    `env:"PORT"       envDefault:"8080"`
	AuthToken string `env:"AUTH_TOKEN"`
}

type LLMConfig struct {
	Provider       string `env:"LLM_PROVIDER"        envDefault:"openai"`
	APIKey         string `env:"LLM_API_KEY"`
	Model          string `env:"LLM_MODEL"            envDefault:"gpt-4o-mini"`
	BaseURL        string `env:"LLM_BASE_URL"`
	MaxTokens      int    `env:"LLM_MAX_TOKENS"       envDefault:"4096"`
	TimeoutSeconds int    `env:"LLM_TIMEOUT_SECONDS"  envDefault:"30"`
	MaxRetries     int    `env:"LLM_MAX_RETRIES"      envDefault:"2"`
	RetryBackoffMS int    `env:"LLM_RETRY_BACKOFF_MS" envDefault:"200"`
}

type AgentConfig struct {
	MaxSteps    int     `env:"AGENT_MAX_STEPS"    envDefault:"10"`
	Temperature float64 `env:"AGENT_TEMPERATURE"  envDefault:"0.1"`
}

type RAGConfig struct {
	EmbeddingAPIKey  string `env:"EMBEDDING_API_KEY"`
	EmbeddingBaseURL string `env:"EMBEDDING_BASE_URL"`
	EmbeddingModel   string `env:"EMBEDDING_MODEL"  envDefault:"bge-large-zh-v1.5"`
	EmbeddingDim     int    `env:"EMBEDDING_DIM"    envDefault:"1024"`
	RerankAPIKey     string `env:"RERANK_API_KEY"`
	RerankBaseURL    string `env:"RERANK_BASE_URL"`
	RerankModel      string `env:"RERANK_MODEL"     envDefault:"qwen3-vl-rerank"`
	TopK             int    `env:"RAG_TOP_K"        envDefault:"20"`
	TopN             int    `env:"RAG_TOP_N"        envDefault:"5"`
}

type MilvusConfig struct {
	Address    string `env:"MILVUS_ADDRESS"    envDefault:"localhost:19530"`
	Collection string `env:"MILVUS_COLLECTION" envDefault:"api_documents"`
}

type RedisConfig struct {
	Address  string `env:"REDIS_ADDRESS"  envDefault:"localhost:6379"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB"       envDefault:"0"`
}

// LoadFromEnv 从环境变量加载配置（替代原 Default() + ApplyEnv()）
func LoadFromEnv() (Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
```

注意：删除 `LookupEnvFunc` 类型、`Default()` 函数和整个 `ApplyEnv()` 方法。

**Step 2.3: 更新 config_test.go**

删除旧的 `ApplyEnv` 测试，改写为针对 `LoadFromEnv` 的测试：

```go
func TestLoadFromEnv_Defaults(t *testing.T) {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if cfg.LLM.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", cfg.LLM.Provider)
	}
	if cfg.RAG.EmbeddingDim != 1024 {
		t.Errorf("expected dim 1024, got %d", cfg.RAG.EmbeddingDim)
	}
}

func TestLoadFromEnv_Override(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("LLM_API_KEY", "test-key")
	t.Setenv("REDIS_ADDRESS", "redis:6379")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.LLM.APIKey != "test-key" {
		t.Errorf("expected api key test-key, got %s", cfg.LLM.APIKey)
	}
	if cfg.Redis.Address != "redis:6379" {
		t.Errorf("expected redis:6379, got %s", cfg.Redis.Address)
	}
}
```

**Step 2.4: 更新 cmd/server/main.go 中的配置加载**

将 `main()` 中：

```go
cfg := config.Default()
if err := cfg.ApplyEnv(os.LookupEnv); err != nil { ... }
```

替换为：

```go
cfg, err := config.LoadFromEnv()
if err != nil {
	slog.Error("load config failed", "error", err)
	os.Exit(1)
}
```

**Step 2.5: 运行测试验证**

Run:
```bash
go test ./internal/config/ -v
go build ./...
```

Expected: 全部通过

**Step 2.6: Commit**

```bash
git add internal/config/ cmd/server/main.go
git commit -m "refactor: replace ApplyEnv with caarlos0/env struct tags"
```

---

## Task 3: 用 Gin 替换路由和中间件

**Files:**
- Modify: `cmd/server/main.go` (路由改为 Gin)
- Modify: `internal/mcp/server.go` (Handler 签名适配)
- Modify: `internal/mcp/middleware.go` (改为 Gin 中间件)
- Modify: `internal/webhook/handler.go` (适配 Gin)

**Step 3.1: 改写 cmd/server/main.go 路由部分**

将 `http.NewServeMux` 替换为 Gin router：

```go
import "github.com/gin-gonic/gin"

// 在 runServer 中替换路由部分：
gin.SetMode(gin.ReleaseMode)
router := gin.New()
router.Use(gin.Recovery())

// MCP 路由组 — 带认证和限流
mcpGroup := router.Group("/mcp")
mcpGroup.Use(requestIDMiddleware())
mcpGroup.Use(authMiddleware(cfg.Server.AuthToken))
mcpGroup.Use(rateLimitMiddleware(limiter))
mcpGroup.Use(loggingMiddleware(logger))
mcpGroup.POST("", mcpHandler(mcpServer))

// 公开端点
router.GET("/healthz", ginHealthHandler(healthChecker))
router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{})))
router.POST("/webhook/sync", gin.WrapF(webhookHandler.HandleSync))

httpServer := &http.Server{
	Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
	Handler:           router,
	ReadHeaderTimeout: 10 * time.Second,
}
```

**Step 3.2: 编写 Gin 中间件函数**

在 `internal/mcp/middleware.go` 中改写为返回 `gin.HandlerFunc` 的独立函数：

```go
func authMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(token) == "" {
			c.Next()
			return
		}
		auth := c.GetHeader("Authorization")
		if auth != "Bearer "+token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func rateLimitMiddleware(limiter RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.Allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = GenerateRequestID()
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func loggingMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("mcp request",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
```

**Step 3.3: 简化 Server.Handler()**

`mcp.Server` 不再自己组装中间件链，`Handler()` 方法简化为返回 RPC 处理逻辑：

```go
// 在 server.go 中，Handler 不再包装中间件
func (s *Server) HandleRPC(c *gin.Context) {
	// 检查 SSE
	if isSSERequest(c.Request) && s.streamRunner != nil {
		s.handleSSE(c)
		return
	}
	var req rpcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	s.handleRPCWithGin(c, req)
}
```

**Step 3.4: 删除不再需要的代码**

- 删除 `middleware.go` 中旧的 `func (s *Server) authMiddleware(next http.Handler) http.Handler` 等方法
- 删除 `middleware.go` 中的 `validationMiddleware`（Gin 路由自动处理 path + method）
- 删除 `request_id.go` 中的 `responseWriterWrapper` 和旧的 `requestIDMiddleware` 方法

**Step 3.5: 更新受影响的测试**

更新 `internal/mcp/server_test.go` 中的测试以使用 Gin 的 test router：

```go
import "github.com/gin-gonic/gin"

func setupTestRouter(server *Server) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/mcp", server.HandleRPC)
	return r
}
```

**Step 3.6: 运行测试验证**

Run:
```bash
go build ./...
go test ./... -count=1
```

Expected: 编译通过，测试通过

**Step 3.7: Commit**

```bash
git add -A
git commit -m "refactor: replace http.ServeMux with Gin router and middleware"
```

---

## Task 4: 创建六边形目录结构

**Files:**
- Create directories: `internal/domain/model/`, `internal/domain/agent/`, `internal/domain/knowledge/`, `internal/domain/rag/`, `internal/domain/tool/`, `internal/transport/`, `internal/infra/milvus/`, `internal/infra/redis/`, `internal/infra/llm/`, `internal/infra/embedding/`, `internal/infra/rerank/`

**Step 4.1: 创建新目录**

Run:
```bash
mkdir -p internal/domain/{model,agent,knowledge,rag,tool}
mkdir -p internal/transport
mkdir -p internal/infra/{milvus,redis,llm,embedding,rerank}
```

**Step 4.2: Commit 空目录结构**

在每个新目录中添加空的 `.gitkeep` 或直接在下一步移动文件。

---

## Task 5: 抽出 domain/model 层

**Files:**
- Create: `internal/domain/model/endpoint.go`
- Create: `internal/domain/model/chunk.go`
- Create: `internal/domain/model/spec.go`
- Modify: `internal/knowledge/models.go` (变为转发引用或删除)

**Step 5.1: 移动领域模型到 domain/model**

将 `internal/knowledge/models.go` 中的类型拆分到 `domain/model/` 下：

`internal/domain/model/endpoint.go`:
```go
package model

import "fmt"

type Endpoint struct {
	Service     string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Parameters  []Parameter
	Responses   []Response
}

func (e Endpoint) Key() string {
	return fmt.Sprintf("%s:%s:%s", e.Service, e.Method, e.Path)
}

func (e Endpoint) DisplayName() string {
	return fmt.Sprintf("%s %s", e.Method, e.Path)
}

type Parameter struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Description string
	SchemaRef   string
}

type Response struct {
	StatusCode  string
	Description string
}
```

`internal/domain/model/chunk.go`:
```go
package model

type Chunk struct {
	ID       string
	Service  string
	Endpoint string
	Type     string
	Content  string
	Version  string
}

type IngestStats struct {
	Endpoints int `json:"endpoints"`
	Chunks    int `json:"chunks"`
}
```

`internal/domain/model/spec.go`:
```go
package model

// 从 knowledge/models.go 移入 SpecMeta, ParsedSpec 及其方法
// 保持所有方法签名不变
```

**Step 5.2: 更新所有引用**

全局搜索并替换 `knowledge.Endpoint` → `model.Endpoint` 等引用，更新 import 路径。

Run:
```bash
grep -rn "knowledge\.Endpoint\|knowledge\.Parameter\|knowledge\.Response\|knowledge\.Chunk\|knowledge\.SpecMeta\|knowledge\.IngestStats\|knowledge\.ParsedSpec" internal/ --include="*.go"
```

根据搜索结果逐文件更新 import。

**Step 5.3: 验证编译**

Run:
```bash
go build ./...
```

**Step 5.4: Commit**

```bash
git add -A
git commit -m "refactor: extract domain models to internal/domain/model"
```

---

## Task 6: 移动 knowledge 到 domain/knowledge

**Files:**
- Move: `internal/knowledge/swagger_parser.go` → `internal/domain/knowledge/parser.go`
- Move: `internal/knowledge/ingestor.go` → `internal/domain/knowledge/ingestor.go`
- Move: `internal/knowledge/redis_ingestor.go` → `internal/infra/redis/ingestor.go`
- Move tests accordingly
- Delete: `internal/knowledge/models.go`（已迁移到 domain/model）

**Step 6.1: 移动解析器和 ingestor 接口**

将 Swagger 解析逻辑和 Ingestor 接口移到 `domain/knowledge/`。
将 `RedisIngestor`（依赖 Redis）移到 `infra/redis/`。

**Step 6.2: 更新 import 路径**

Run:
```bash
grep -rn '"ai-agent-api/internal/knowledge"' internal/ cmd/ --include="*.go"
```

逐文件更新。

**Step 6.3: 验证**

Run:
```bash
go build ./...
go test ./internal/domain/knowledge/ -v
```

**Step 6.4: Commit**

```bash
git add -A
git commit -m "refactor: move knowledge to domain/knowledge, redis ingestor to infra"
```

---

## Task 7: 移动 agent 到 domain/agent，LLM 实现到 infra/llm

**Files:**
- Move: `internal/agent/*.go` → `internal/domain/agent/` (接口和引擎)
- Move: `internal/agent/openai_llm.go` → `internal/infra/llm/openai.go`
- Move: `internal/agent/openai_llm_test.go` → `internal/infra/llm/openai_test.go`

**Step 7.1: 移动文件**

Agent 引擎（engine.go, adaptive.go, memory.go, handler.go, llm.go 接口）移到 `domain/agent/`。
`openai_llm.go`（OpenAI 兼容客户端实现）移到 `infra/llm/`。
`RuleBasedLLMClient` 保留在 `domain/agent/` 中（它不依赖外部服务）。

**Step 7.2: 确保 domain/agent/llm.go 只定义接口**

```go
// domain/agent/llm.go
package agent

type LLMClient interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*LLMResponse, error)
}
```

实现类在 `infra/llm/`。

**Step 7.3: 更新 import 并验证**

Run:
```bash
go build ./...
go test ./internal/domain/agent/ -v
go test ./internal/infra/llm/ -v
```

**Step 7.4: Commit**

```bash
git add -A
git commit -m "refactor: move agent to domain/agent, LLM impl to infra/llm"
```

---

## Task 8: 移动 rag 接口到 domain/rag，实现到 infra

**Files:**
- Move: `internal/rag/store.go` (Store 接口 + Search) → `internal/domain/rag/store.go`
- Move: `internal/rag/milvus_store.go` → `internal/infra/milvus/store.go`
- Move: `internal/rag/rerank_store.go` → `internal/infra/rerank/store.go` 或保留在 domain（取决于是否依赖外部）
- Move: `internal/rag/chunker.go` → `internal/domain/knowledge/chunker.go`
- Move: `internal/embedding/` → `internal/infra/embedding/`
- Move: `internal/rerank/` → `internal/infra/rerank/`
- Move: `internal/store/milvus_*.go` → `internal/infra/milvus/`
- Move: `internal/store/redis_*.go` → `internal/infra/redis/`

**Step 8.1: 移动 Store 接口到 domain/rag**

`domain/rag/store.go` 只保留接口定义和不依赖外部的搜索逻辑。

**Step 8.2: 移动 Milvus 实现到 infra/milvus**

合并 `internal/store/milvus_*.go` 和 `internal/rag/milvus_store.go` 到 `internal/infra/milvus/`。

**Step 8.3: 移动 embedding/rerank 到 infra**

```bash
# embedding 客户端
mv internal/embedding/*.go internal/infra/embedding/
# rerank 客户端
mv internal/rerank/*.go internal/infra/rerank/
# redis 客户端
mv internal/store/redis_*.go internal/infra/redis/
```

**Step 8.4: 更新 import 并验证**

Run:
```bash
go build ./...
go test ./...
```

**Step 8.5: Commit**

```bash
git add -A
git commit -m "refactor: move rag/store/embedding/rerank to domain and infra layers"
```

---

## Task 9: 移动 tools 到 domain/tool，移动 transport

**Files:**
- Move: `internal/tools/*.go` → `internal/domain/tool/`
- Move: `internal/mcp/server.go` + `middleware.go` + `sse.go` → `internal/transport/`
- Delete: `internal/tools/match_skill.go`（与 API 文档 Agent 定位无关）
- Move: `internal/webhook/handler.go` → `internal/transport/webhook.go`

**Step 9.1: 移动 tools 到 domain/tool**

包名从 `tools` 改为 `tool`。删除 `match_skill.go`。
更新 `registry.go` 中的 `RegisterDefaultTools` 去掉 `NewMatchSkillTool` 调用。

**Step 9.2: 移动 transport 层**

将 MCP server、中间件、SSE、webhook handler 统一到 `internal/transport/`。

**Step 9.3: 更新 import 并验证**

Run:
```bash
go build ./...
go test ./...
```

**Step 9.4: Commit**

```bash
git add -A
git commit -m "refactor: move tools to domain/tool, MCP+webhook to transport"
```

---

## Task 10: 清理旧目录

**Step 10.1: 删除空的旧目录**

确认所有文件已移动后：

Run:
```bash
# 检查是否还有文件残留
find internal/tools internal/mcp internal/store internal/embedding internal/rerank internal/knowledge internal/agent -name "*.go" 2>/dev/null
```

如果为空，删除旧目录。

**Step 10.2: 验证整体编译和测试**

Run:
```bash
go build ./...
go test ./... -count=1
```

Expected: 全部通过

**Step 10.3: Commit**

```bash
git add -A
git commit -m "refactor: remove legacy directories after hexagonal restructure"
```

---

## Task 11: 新增 Chat SSE 端点

**Files:**
- Create: `internal/transport/chat.go`
- Create: `internal/transport/chat_test.go`
- Modify: `cmd/server/main.go` (注册路由)

**Step 11.1: 编写 Chat handler 测试**

`internal/transport/chat_test.go`:

```go
package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"wanzhi/internal/domain/agent"
)

type mockStreamRunner struct {
	events []agent.AgentEvent
}

func (m *mockStreamRunner) RunStream(ctx context.Context, query string) <-chan agent.AgentEvent {
	ch := make(chan agent.AgentEvent, len(m.events))
	for _, ev := range m.events {
		ch <- ev
	}
	close(ch)
	return ch
}

func TestChatHandler_SSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runner := &mockStreamRunner{
		events: []agent.AgentEvent{
			{Type: "thinking", Data: "searching..."},
			{Type: "message", Data: "找到登录接口 POST /user/login"},
		},
	}
	handler := NewChatHandler(runner)

	r := gin.New()
	r.POST("/api/chat", handler.HandleChat)

	body := `{"message":"查询登录接口"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected SSE content type, got %s", ct)
	}
	if !strings.Contains(w.Body.String(), "event:message") {
		t.Errorf("expected SSE message event in body, got: %s", w.Body.String())
	}
}

func TestChatHandler_EmptyMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewChatHandler(&mockStreamRunner{})

	r := gin.New()
	r.POST("/api/chat", handler.HandleChat)

	body := `{"message":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

**Step 11.2: 运行测试确认失败**

Run:
```bash
go test ./internal/transport/ -run TestChatHandler -v
```

Expected: FAIL (ChatHandler 未实现)

**Step 11.3: 实现 Chat handler**

`internal/transport/chat.go`:

```go
package transport

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ChatRequest struct {
	Message string `json:"message" binding:"required"`
}

type ChatHandler struct {
	streamRunner StreamRunner
}

func NewChatHandler(runner StreamRunner) *ChatHandler {
	return &ChatHandler{streamRunner: runner}
}

func (h *ChatHandler) HandleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

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

**Step 11.4: 运行测试确认通过**

Run:
```bash
go test ./internal/transport/ -run TestChatHandler -v
```

Expected: PASS

**Step 11.5: 注册路由**

在 `cmd/server/main.go` 的 Gin router 中添加：

```go
chatHandler := transport.NewChatHandler(baseEngine)
router.POST("/api/chat", chatHandler.HandleChat)
```

**Step 11.6: 验证编译**

Run:
```bash
go build ./...
```

**Step 11.7: Commit**

```bash
git add -A
git commit -m "feat: add Chat SSE endpoint POST /api/chat"
```

---

## Task 12: 项目重命名

**Files:**
- Modify: `go.mod` (module path)
- Modify: 所有 `.go` 文件的 import 路径

**Step 12.1: 更新 go.mod**

```
module wanzhi
```

**Step 12.2: 全量替换 import 路径**

Run:
```bash
find . -name "*.go" -exec sed -i '' 's|"ai-agent-api/|"wanzhi/|g' {} +
```

**Step 12.3: 验证**

Run:
```bash
go build ./...
go test ./... -count=1
```

Expected: 全部通过

**Step 12.4: Commit**

```bash
git add -A
git commit -m "refactor: rename module from ai-agent-api to wanzhi"
```

---

## Task 13: 更新文档

**Files:**
- Rewrite: `README.md`
- Rewrite: `CLAUDE.md`
- Update: `docs/design.md`

**Step 13.1: 重写 README.md**

以"万知"的新定位重写 README，包括：
- 项目名和一句话定位
- 六边形架构图
- 两种接入方式（MCP + Chat SSE）示例
- 快速开始

**Step 13.2: 更新 CLAUDE.md**

更新项目概述、架构描述、目录结构、module path 等所有引用。

**Step 13.3: Commit**

```bash
git add README.md CLAUDE.md docs/
git commit -m "docs: update documentation for WanZhi rebrand"
```

---

## Task 14: 最终验证

**Step 14.1: 完整测试**

Run:
```bash
go build ./...
go test ./... -count=1
go vet ./...
```

Expected: 全部通过，无 warning

**Step 14.2: 手动冒烟测试（可选）**

```bash
# 启动依赖
make dev

# 启动服务
AUTH_TOKEN=demo-token go run cmd/server/main.go run

# 测试 MCP 端点
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"query_api","params":{"query":"查询用户登录接口"}}'

# 测试 Chat SSE 端点
curl -N -X POST http://localhost:8080/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"查询用户登录接口的参数和 Go 示例"}'

# 健康检查
curl http://localhost:8080/healthz
```

**Step 14.3: 最终 Commit（如有调整）**

```bash
git add -A
git commit -m "chore: final cleanup after WanZhi refactor"
```
