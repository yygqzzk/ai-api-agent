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
	"ai-agent-api/internal/store"
	"ai-agent-api/internal/tools"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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

func runServer(cfg config.Config) error {
	ctx := context.Background()

	logger := observability.NewLogger(os.Stdout, false)
	slog.SetDefault(logger)

	promRegistry := prometheus.NewRegistry()
	promRegistry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics := observability.NewMetrics(promRegistry)

	llmClient := newLLMClient(cfg)
	kb, stores, cleanup, err := newKnowledgeBase(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	defaultPetstore := filepath.Join("testdata", "petstore.json")
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
	engine := agent.NewAgentEngine(
		llmClient,
		registry,
		agent.WithMaxSteps(cfg.Agent.MaxSteps),
		agent.WithMetrics(metrics),
	)
	engine.SetToolCatalog(toAgentToolCatalog(registry.ToolDefinitions()))
	if err := tools.RegisterQueryTool(registry, engine); err != nil {
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
	mcpServer.SetStreamRunner(engine)
	if err := mcpServer.Init(ctx); err != nil {
		return err
	}

	healthChecker := newHealthDependencyChecker(cfg, stores.cache, stores.milvus, llmClient)
	rootMux := http.NewServeMux()
	rootMux.Handle("/mcp", mcpServer.Handler())
	rootMux.Handle("/healthz", newHealthHandler(healthChecker))
	rootMux.Handle("/metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{}))

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           rootMux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api-assistant MCP server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", "signal", sig.String())
	case err := <-serverErr:
		return err
	}

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

	kb, _, cleanup, err := newKnowledgeBase(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

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

type runtimeStores struct {
	cache  store.RedisClient
	milvus store.MilvusClient
}

func newKnowledgeBase(ctx context.Context, cfg config.Config) (*tools.KnowledgeBase, runtimeStores, func(), error) {
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

	switch cfg.Milvus.Mode {
	case "milvus":
		embedder := embedding.NewOpenAIClient(
			cfg.LLM.APIKey,
			cfg.LLM.BaseURL,
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
		ragStore = rag.NewMemoryStore()
		milvusCleanup = func() {}
	}

	kb := tools.NewKnowledgeBaseWithStoreAndCache(ragStore, cache)
	cleanup := func() {
		milvusCleanup()
		_ = cache.Close(context.Background())
	}
	return kb, runtimeStores{cache: cache, milvus: milvusClient}, cleanup, nil
}

func newLLMClient(cfg config.Config) agent.LLMClient {
	provider := strings.ToLower(strings.TrimSpace(cfg.LLM.Provider))
	if provider == "" {
		provider = "openai"
	}

	hasKey := strings.TrimSpace(cfg.LLM.APIKey) != ""
	hasCustomBase := strings.TrimSpace(cfg.LLM.BaseURL) != ""
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

	slog.Info("llm provider missing usable config, fallback to rule-based llm client", "provider", provider)
	return agent.NewRuleBasedLLMClient()
}

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
