// Package main 负责装配并启动 API 服务。
package main

import (
	"context"
	"errors"
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
	ingestsvc "ai-agent-api/internal/ingest"
	"ai-agent-api/internal/mcp"
	"ai-agent-api/internal/observability"
	"ai-agent-api/internal/rag"
	"ai-agent-api/internal/rerank"
	"ai-agent-api/internal/store"
	"ai-agent-api/internal/tools"
	webhooksvc "ai-agent-api/internal/webhook"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// main 根据启动参数执行服务入口。
func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	cmd := "run"
	// os.Args 是进程启动参数切片：
	// os.Args[0] 通常是程序名，后面的元素才是用户传入的参数。
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
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

// runServer 装配依赖并启动 HTTP 服务。
func runServer(cfg config.Config) error {
	ctx := context.Background()

	// 初始化结构化日志
	// os.Stdout 表示进程的标准输出流；把日志写到这里，
	// 在终端、容器日志或 systemd 日志里都更容易统一采集。
	logger := observability.NewLogger(os.Stdout, false)
	slog.SetDefault(logger)

	// 初始化 Prometheus 指标
	// prometheus.NewRegistry 会创建一个独立指标注册表，适合应用自己明确控制要暴露哪些指标。
	promRegistry := prometheus.NewRegistry()
	// collectors.NewGoCollector / NewProcessCollector 是官方内置采集器，分别暴露 Go 运行时和进程级指标。
	promRegistry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics := observability.NewMetrics(promRegistry)

	llmClient := newLLMClient(cfg)

	kb, stores, cleanup, err := newKnowledgeBase(ctx, cfg)
	if err != nil {
		return err
	}
	// defer 会把 cleanup 延迟到当前函数返回时执行，适合做收尾清理。
	defer cleanup()

	// filepath.Join 会按当前操作系统规则拼接路径，避免手写分隔符。
	defaultPetstore := filepath.Join("testdata", "petstore.json")
	// os.Stat 返回文件元信息；这里只是借它判断文件是否存在。
	if _, err := os.Stat(defaultPetstore); err == nil {
		if _, ingestErr := kb.IngestFile(ctx, defaultPetstore, "petstore"); ingestErr != nil {
			return fmt.Errorf("bootstrap ingest default petstore failed: %w", ingestErr)
		}
		logger.Info("default swagger loaded", "path", defaultPetstore)
	}

	registry := tools.NewRegistry()
	if err := tools.RegisterDefaultTools(registry, kb, "skills"); err != nil {
		return fmt.Errorf("register default tools: %w", err)
	}

	baseEngine := agent.NewAgentEngine(
		llmClient,
		registry,
		agent.WithMaxSteps(cfg.Agent.MaxSteps),
		agent.WithMetrics(metrics),
	)
	baseEngine.SetToolCatalog(toAgentToolCatalog(registry.ToolDefinitions()))

	adaptiveEngine := agent.NewAdaptiveAgentEngine(baseEngine, registry, agent.AdaptiveAgentEngineOptions{
		Selector:         agent.NewLLMBasedStrategySelector(llmClient, agent.NewRuleBasedStrategySelector()),
		Rewriter:         agent.NewLLMQueryRewriter(llmClient, agent.NewRuleBasedQueryRewriter()),
		Planner:          agent.NewLLMPlanner(llmClient, agent.NewRuleBasedPlanner()),
		Reflector:        agent.NewLLMReflector(llmClient, agent.NewRuleBasedReflector(0.7), 0.7),
		MaxRetries:       1,
		QualityThreshold: 0.7,
	})

	if err := tools.RegisterQueryTool(registry, adaptiveEngine); err != nil {
		return fmt.Errorf("register query_api tool: %w", err)
	}

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

	mcpServer := mcp.NewServer(cfg, registry, hooks, mcp.ServerOptions{
		RateLimitPerMinute: 120,
		Metrics:            metrics,
		Logger:             logger,
	})
	mcpServer.SetStreamRunner(baseEngine)
	if err := mcpServer.Init(ctx); err != nil {
		return err
	}

	healthChecker := newHealthDependencyChecker(cfg, stores.cache, stores.milvus, llmClient)
	ingestService := ingestsvc.NewService(kb, &http.Client{Timeout: 30 * time.Second})
	webhookHandler := webhooksvc.NewHandler(ingestService, webhooksvc.HandlerOptions{
		// os.Getenv 只返回字符串本身；如果变量不存在，会得到空字符串。
		// 这里适合读取“可选配置”，因为空值本身就代表未配置。
		Secret:       os.Getenv("WEBHOOK_SECRET"),
		BearerToken:  cfg.Server.AuthToken,
		ProcessAsync: true,
	})

	// 使用 Gin 路由
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// MCP 路由组 — 带认证和限流
	mcpGroup := router.Group("/mcp")
	mcpGroup.Use(mcp.RequestIDMiddleware())
	mcpGroup.Use(mcp.AuthMiddleware(cfg.Server.AuthToken))
	mcpGroup.Use(mcp.RateLimitMiddleware(mcpServer.Limiter()))
	mcpGroup.Use(mcp.LoggingMiddleware(logger))
	mcpGroup.POST("", mcpServer.HandleRPC)

	// 公开端点
	router.GET("/healthz", gin.WrapH(newHealthHandler(healthChecker)))
	router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{})))
	router.POST("/webhook/sync", gin.WrapF(webhookHandler.HandleSync))

	// http.Server 是标准库 HTTP 服务对象：
	// Addr 指监听地址，Handler 是请求入口，ReadHeaderTimeout 用来限制读请求头的最长时间。
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second, // 防止 Slowloris 攻击
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api-assistant MCP server listening", "addr", httpServer.Addr)
		// errors.Is 会沿着错误链判断目标错误；这里用它区分“正常关闭”与“真正异常退出”。
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// signal.Notify 会把进程收到的系统信号转发到 channel。
	// 这里缓冲区设为 1，可以避免第一个信号在还未接收时丢失。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", "signal", sig.String())
	case err := <-serverErr:
		return err
	}

	// context.WithTimeout 会派生一个带超时的 context，
	// Shutdown 在超过 10 秒后会被取消，避免优雅关闭无限等待。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown failed: %w", err)
	}

	if err := mcpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("mcp shutdown failed: %w", err)
	}

	return nil
}

// runtimeStores 运行时存储依赖
// 用于健康检查和资源清理
type runtimeStores struct {
	cache  store.RedisClient  // Redis 缓存客户端
	milvus store.MilvusClient // Milvus 向量数据库客户端
}

// newKnowledgeBase 创建运行时知识库及其底层依赖。
func newKnowledgeBase(ctx context.Context, cfg config.Config) (*tools.KnowledgeBase, runtimeStores, func(), error) {
	cache, err := store.NewRedisClient(store.RedisOptions{
		Mode:     "redis",
		Address:  cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		return nil, runtimeStores{}, nil, fmt.Errorf("init redis client failed: %w", err)
	}

	var ragStore rag.Store
	var milvusClient store.MilvusClient
	var milvusCleanup func()

	// 使用真实 Milvus + Embedding 组件；本地通过 make dev 启动依赖。
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

	ragStore = rag.NewRerankStore(ragStore, rerankClient, cfg.RAG.TopN)

	kb := tools.NewKnowledgeBaseWithRedis(cache, ragStore)
	cleanup := func() {
		milvusCleanup()
		_ = cache.Close(context.Background())
	}
	return kb, runtimeStores{cache: cache, milvus: milvusClient}, cleanup, nil
}

// newLLMClient 根据配置选择 OpenAI 兼容或规则式实现。
func newLLMClient(cfg config.Config) agent.LLMClient {
	// strings.TrimSpace 去掉首尾空白，ToLower 再统一大小写，
	// 可以把用户输入的 ` OpenAI `、`OPENAI` 这类值归一化后再判断。
	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" {
		provider = "openai"
	}

	hasKey := strings.TrimSpace(cfg.LLM.APIKey) != ""
	hasCustomBase := strings.TrimSpace(cfg.LLM.BaseURL) != ""

	// 如果有 API Key 或自定义 BaseURL,使用 OpenAI Compatible 客户端
	// 兼容所有遵循 OpenAI API 格式的提供商（bailian/dashscope/deepseek 等）
	if hasKey || hasCustomBase {
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

// toAgentToolCatalog 将 tools.ToolDefinition 转为 agent.ToolDefinition。
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
