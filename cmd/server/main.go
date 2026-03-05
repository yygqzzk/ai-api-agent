// Package main 是 AI Agent API 服务的入口点
//
// # 架构设计
//
// 本服务采用分层架构和依赖注入模式:
//
// 1. **配置层** - 从环境变量和配置文件加载配置
// 2. **基础设施层** - 初始化 Milvus, Redis, LLM 客户端
// 3. **业务层** - 创建 KnowledgeBase, Registry, AgentEngine
// 4. **接口层** - 启动 MCP Server 和 HTTP 服务器
//
// # 启动流程
//
// ```
// main()
//   ├─> 加载配置 (config.Default + ApplyEnv)
//   ├─> 解析命令 (ingest / run)
//   └─> 执行命令
//       ├─> runIngest: 导入 Swagger 文档
//       └─> runServer: 启动 HTTP 服务
//           ├─> 初始化日志和指标
//           ├─> 创建知识库 (newKnowledgeBase)
//           ├─> 注册工具 (RegisterDefaultTools)
//           ├─> 创建 Agent 引擎
//           ├─> 创建 MCP Server
//           ├─> 启动 HTTP 服务器
//           └─> 优雅关闭 (信号监听)
// ```
//
// # 依赖注入模式
//
// 使用工厂函数实现依赖注入:
// - newKnowledgeBase: 根据配置创建存储后端 (Memory/Milvus)
// - newLLMClient: 根据配置创建 LLM 客户端 (OpenAI/RuleBased)
// - newHealthDependencyChecker: 创建健康检查器
//
// # 优雅关闭
//
// 监听 SIGINT/SIGTERM 信号,执行优雅关闭:
// 1. 停止接受新请求 (httpServer.Shutdown)
// 2. 等待现有请求完成 (10秒超时)
// 3. 关闭 MCP Server (mcpServer.Shutdown)
// 4. 清理资源 (cleanup 函数)
//
// # 配置模式切换
//
// 通过环境变量控制运行模式:
// - MILVUS_MODE=memory: 内存模式,无需外部依赖
// - MILVUS_MODE=milvus: 生产模式,使用 Milvus 向量数据库
// - REDIS_MODE=memory: 内存缓存
// - REDIS_MODE=redis: Redis 缓存
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ai-agent-api/internal/agent"
	"ai-agent-api/internal/config"
	"ai-agent-api/internal/embedding"
	"ai-agent-api/internal/mcp"
	"ai-agent-api/internal/observability"
	"ai-agent-api/internal/rag"
	"ai-agent-api/internal/rerank"
	"ai-agent-api/internal/store"
	"ai-agent-api/internal/tools"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// main 程序入口点
//
// 执行流程:
// 1. 加载配置 (环境变量优先)
// 2. 解析命令行参数 (ingest/run)
// 3. 执行对应的子命令
func main() {
	cfg := config.Default()
	if err := cfg.ApplyEnv(os.LookupEnv); err != nil {
		slog.Error("load env config failed", "error", err)
		os.Exit(1)
	}

	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "ingest":
		if err := runIngest(os.Args[2:], cfg); err != nil {
			slog.Error("ingest failed", "error", err)
			os.Exit(1)
		}
	case "run":
		if err := runServer(cfg); err != nil {
			slog.Error("run server failed", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("unknown command", "command", cmd)
		os.Exit(1)
	}
}

// runServer 启动 HTTP 服务器
//
// 职责:
// 1. 初始化所有依赖 (日志、指标、存储、LLM)
// 2. 创建 Agent 引擎和 MCP Server
// 3. 启动 HTTP 服务器 (MCP 端点、健康检查、指标)
// 4. 监听信号,执行优雅关闭
//
// 端点:
// - POST /mcp: MCP JSON-RPC 2.0 端点
// - GET /healthz: 健康检查端点
// - GET /metrics: Prometheus 指标端点
//
// 优雅关闭:
// - 监听 SIGINT/SIGTERM 信号
// - 10秒超时等待现有请求完成
// - 依次关闭 HTTP Server 和 MCP Server
func runServer(cfg config.Config) error {
	ctx := context.Background()

	// 初始化结构化日志
	logger := observability.NewLogger(os.Stdout, false)
	slog.SetDefault(logger)

	// 初始化 Prometheus 指标
	// 注册 Go 运行时指标和进程指标
	promRegistry := prometheus.NewRegistry()
	promRegistry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics := observability.NewMetrics(promRegistry)

	// 创建 LLM 客户端 (根据配置选择 OpenAI 或 RuleBased)
	llmClient := newLLMClient(cfg)

	// 创建知识库 (根据配置选择 Memory 或 Milvus)
	kb, stores, cleanup, err := newKnowledgeBase(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	// 自动导入默认 Swagger 文档 (如果存在)
	defaultPetstore := filepath.Join("testdata", "petstore.json")
	if _, err := os.Stat(defaultPetstore); err == nil {
		if _, ingestErr := kb.IngestFile(ctx, defaultPetstore, "petstore"); ingestErr != nil {
			return fmt.Errorf("bootstrap ingest default petstore failed: %w", ingestErr)
		}
		logger.Info("default swagger loaded", "path", defaultPetstore)
	}

	// 注册默认工具 (search_api, get_api_detail, 等)
	registry := tools.NewRegistry()
	if err := tools.RegisterDefaultTools(registry, kb, "skills"); err != nil {
		return fmt.Errorf("register default tools: %w", err)
	}

	// 创建 Agent 引擎
	engine := agent.NewAgentEngine(
		llmClient,
		registry,
		agent.WithMaxSteps(cfg.Agent.MaxSteps),
		agent.WithMetrics(metrics),
	)
	engine.SetToolCatalog(toAgentToolCatalog(registry.ToolDefinitions()))

	// 注册 query_api 工具 (需要 Agent 引擎支持)
	if err := tools.RegisterQueryTool(registry, engine); err != nil {
		return fmt.Errorf("register query_api tool: %w", err)
	}

	// 配置 MCP Server 生命周期钩子
	hooks := mcp.Hooks{
		OnInit: func(ctx context.Context) error {
			logger.Info("mcp server init completed")
			return nil
		},
		BeforeToolCall: func(ctx context.Context, toolName string) {
			logger.Info("tool call started", "tool", toolName)
		},
		AfterToolCall: func(ctx context.Context, toolName string, duration time.Duration, err error) {
			if err != nil {
				logger.Error("tool call failed", "tool", toolName, "duration", duration, "error", err)
				return
			}
			logger.Info("tool call finished", "tool", toolName, "duration", duration)
		},
		OnShutdown: func(ctx context.Context) error {
			logger.Info("mcp server shutdown completed")
			return nil
		},
	}

	// 创建 MCP Server
	mcpServer := mcp.NewServer(cfg, registry, hooks, mcp.ServerOptions{
		RateLimitPerMinute: 120,
		Metrics:            metrics,
		Logger:             logger,
	})
	mcpServer.SetStreamRunner(engine)
	if err := mcpServer.Init(ctx); err != nil {
		return err
	}

	// 创建健康检查器
	healthChecker := newHealthDependencyChecker(cfg, stores.cache, stores.milvus, llmClient)

	// 配置 HTTP 路由
	rootMux := http.NewServeMux()
	rootMux.Handle("/mcp", mcpServer.Handler())
	rootMux.Handle("/healthz", newHealthHandler(healthChecker))
	rootMux.Handle("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{}))

	// 创建 HTTP 服务器
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           rootMux,
		ReadHeaderTimeout: 10 * time.Second, // 防止 Slowloris 攻击
	}

	// 在 goroutine 中启动 HTTP 服务器
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api-assistant MCP server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// 监听系统信号 (SIGINT/SIGTERM)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号或服务器错误
	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", "signal", sig.String())
	case err := <-serverErr:
		return err
	}

	// 优雅关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 关闭 HTTP 服务器 (等待现有请求完成)
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown failed: %w", err)
	}

	// 关闭 MCP Server (执行清理钩子)
	if err := mcpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("mcp shutdown failed: %w", err)
	}

	return nil
}

// runIngest 导入 Swagger 文档到知识库
//
// 支持两种导入方式:
// 1. 从本地文件导入: --file=path/to/swagger.json
// 2. 从 URL 导入: --url=https://example.com/swagger.json
//
// 参数:
// - args: 命令行参数
// - cfg: 配置对象
//
// 使用示例:
//
//	go run cmd/server/main.go ingest --file=testdata/petstore.json --service=petstore
//	go run cmd/server/main.go ingest --url=https://petstore.swagger.io/v2/swagger.json --service=petstore
func runIngest(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	file := fs.String("file", "", "swagger file path")
	url := fs.String("url", "", "swagger url")
	service := fs.String("service", "petstore", "service name")
	_ = fs.Parse(args)

	if *file == "" && *url == "" {
		return fmt.Errorf("file or url is required")
	}

	ctx := context.Background()

	// 创建知识库 (使用与 runServer 相同的工厂函数)
	kb, _, cleanup, err := newKnowledgeBase(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	// 执行导入
	var (
		stats tools.ParseSwaggerResult
		opErr error
	)
	if *file != "" {
		ingestStats, ingestErr := kb.IngestFile(ctx, *file, *service)
		stats = tools.ParseSwaggerResult{Stats: ingestStats}
		opErr = ingestErr
	} else {
		ingestStats, ingestErr := kb.IngestURL(ctx, *url, *service)
		stats = tools.ParseSwaggerResult{Stats: ingestStats}
		opErr = ingestErr
	}
	if opErr != nil {
		return opErr
	}
	fmt.Printf("ingest done: %+v\n", stats)
	return nil
}

// runtimeStores 运行时存储依赖
// 用于健康检查和资源清理
type runtimeStores struct {
	cache  store.RedisClient  // Redis 缓存客户端
	milvus store.MilvusClient // Milvus 向量数据库客户端
}

// newKnowledgeBase 创建知识库实例 (工厂函数)
//
// 根据配置创建不同的存储后端:
//
// 1. **Memory 模式** (MILVUS_MODE=memory)
//   - 使用 MemoryStore (关键词匹配)
//   - 使用 InMemoryRedisClient
//   - 无需外部依赖,适合开发和测试
//
// 2. **Milvus 模式** (MILVUS_MODE=milvus)
//   - 使用 MilvusStore (向量检索)
//   - 使用 OpenAI Embeddings
//   - 需要 Milvus 和 Embedding API
//
// 返回:
// - kb: KnowledgeBase 实例
// - stores: 运行时存储依赖 (用于健康检查)
// - cleanup: 清理函数 (关闭连接)
// - error: 初始化错误
//
// 设计模式:
// - Factory Pattern: 根据配置创建不同实现
// - Dependency Injection: 通过参数注入配置
func newKnowledgeBase(ctx context.Context, cfg config.Config) (*tools.KnowledgeBase, runtimeStores, func(), error) {
	// 创建 Redis 客户端
	cache, err := store.NewRedisClient(store.RedisOptions{
		Mode:    cfg.Redis.Mode,
		Address: cfg.Redis.Address,
		DB:      cfg.Redis.DB,
	})
	if err != nil {
		return nil, runtimeStores{}, nil, fmt.Errorf("init redis client failed: %w", err)
	}

	var ragStore rag.Store
	var milvusClient store.MilvusClient
	var milvusCleanup func()

	// 根据配置选择存储后端
	switch cfg.Milvus.Mode {
	case "milvus":
		// 生产模式: 使用 Milvus + OpenAI Embeddings
		// 优先使用独立的 embedding 配置，否则回退到 LLM 配置
		embeddingAPIKey := cfg.RAG.EmbeddingAPIKey
		if embeddingAPIKey == "" {
			embeddingAPIKey = cfg.LLM.APIKey
		}
		embeddingBaseURL := cfg.RAG.EmbeddingBaseURL
		if embeddingBaseURL == "" {
			embeddingBaseURL = cfg.LLM.BaseURL
		}
		embedder := embedding.NewOpenAIClient(
			embeddingAPIKey,
			embeddingBaseURL,
			cfg.RAG.EmbeddingModel,
			cfg.RAG.EmbeddingDim,
		)
		sdkMilvusClient, err := store.NewSDKMilvusClient(ctx, cfg.Milvus.Address, cfg.RAG.EmbeddingDim)
		if err != nil {
			_ = cache.Close(context.Background())
			return nil, runtimeStores{}, nil, fmt.Errorf("init milvus client failed: %w", err)
		}
		ragStore = rag.NewMilvusStore(sdkMilvusClient, embedder, cfg.Milvus.Collection)
		milvusClient = sdkMilvusClient
		milvusCleanup = func() {
			_ = ragStore.Close(context.Background())
		}
	default:
		// 开发模式: 使用内存存储
		ragStore = rag.NewMemoryStore()
		milvusCleanup = func() {}
	}

	// 创建 rerank 客户端并包装 ragStore
	var rerankClient rerank.Client
	rerankAPIKey := cfg.RAG.RerankAPIKey
	if rerankAPIKey == "" {
		rerankAPIKey = cfg.RAG.EmbeddingAPIKey
	}
	if rerankAPIKey == "" {
		rerankAPIKey = cfg.LLM.APIKey
	}
	
	rerankBaseURL := cfg.RAG.RerankBaseURL
	if rerankBaseURL == "" {
		rerankBaseURL = cfg.RAG.EmbeddingBaseURL
	}

	// 如果配置了 rerank API Key，使用真实的 rerank 客户端
	if rerankAPIKey != "" && cfg.RAG.RerankModel != "" {
		rerankClient = rerank.NewDashScopeClient(rerankAPIKey, rerankBaseURL, cfg.RAG.RerankModel)
		slog.Info("rerank enabled", "model", cfg.RAG.RerankModel)
	} else {
		// 否则使用 noop 客户端（不进行重排序）
		rerankClient = rerank.NewNoopClient()
		slog.Info("rerank disabled, using noop client")
	}

	// 使用 RerankStore 包装原始 ragStore
	ragStore = rag.NewRerankStore(ragStore, rerankClient, cfg.RAG.TopN)

	kb := tools.NewKnowledgeBaseWithStoreAndCache(ragStore, cache)
	cleanup := func() {
		milvusCleanup()
		_ = cache.Close(context.Background())
	}
	return kb, runtimeStores{cache: cache, milvus: milvusClient}, cleanup, nil
}

// newLLMClient 创建 LLM 客户端 (工厂函数)
//
// 根据配置创建不同的 LLM 客户端:
//
// 1. **OpenAI Compatible** (有 API Key 或 BaseURL)
//   - 支持 OpenAI API
//   - 支持兼容 OpenAI 的 API (如 DeepSeek, Kimi)
//   - 配置重试和超时
//
// 2. **RuleBased** (无 API Key)
//   - 基于规则的确定性 LLM
//   - 用于测试和演示
//   - 无需外部 API
//
// 设计模式:
// - Strategy Pattern: LLMClient 接口统一不同实现
// - Factory Pattern: 根据配置选择实现
func newLLMClient(cfg config.Config) agent.LLMClient {
	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" {
		provider = "openai"
	}

	hasKey := strings.TrimSpace(cfg.LLM.APIKey) != ""
	hasCustomBase := strings.TrimSpace(cfg.LLM.BaseURL) != ""

	// 如果有 API Key 或自定义 BaseURL,使用 OpenAI Compatible 客户端
	if (provider == "openai" || provider == "openai-compatible") && (hasKey || hasCustomBase) {
		timeout := time.Duration(cfg.LLM.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		return agent.NewOpenAICompatibleLLMClient(agent.OpenAICompatibleLLMConfig{
			APIKey:       cfg.LLM.APIKey,
			BaseURL:      cfg.LLM.BaseURL,
			Model:        cfg.LLM.Model,
			MaxTokens:    cfg.LLM.MaxTokens,
			Temperature:  cfg.Agent.Temperature,
			MaxRetries:   cfg.LLM.MaxRetries,
			RetryBackoff: time.Duration(cfg.LLM.RetryBackoffMS) * time.Millisecond,
			HTTPClient: &http.Client{
				Timeout: timeout,
			},
		})
	}

	// 降级到基于规则的 LLM 客户端
	slog.Info("llm provider missing usable config, fallback to rule-based llm client", "provider", provider)
	return agent.NewRuleBasedLLMClient()
}

// toAgentToolCatalog 转换工具定义格式
//
// 将 tools.ToolDefinition 转换为 agent.ToolDefinition
// 两者结构相同,但类型不同 (避免循环依赖)
func toAgentToolCatalog(defs []tools.ToolDefinition) []agent.ToolDefinition {
	out := make([]agent.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, agent.ToolDefinition{
			Name:        d.Name,
			Description: d.Description,
			Schema:      d.Schema,
		})
	}
	return out
}
