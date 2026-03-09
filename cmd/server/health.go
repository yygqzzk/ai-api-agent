package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"wanzhi/internal/domain/agent"
	"wanzhi/internal/config"
	"wanzhi/internal/infra/redis"
	inframsg "wanzhi/internal/infra/milvus"
)

type dependencyHealthChecker struct {
	cfg    config.Config
	redis  redis.RedisClient
	milvus inframsg.MilvusClient
	llm    agent.LLMClient
}

type healthzResponse struct {
	Status    string                  `json:"status"`
	Timestamp string                  `json:"timestamp"`
	Checks    map[string]healthzCheck `json:"checks"`
}

type healthzCheck struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func newHealthDependencyChecker(cfg config.Config, redis redis.RedisClient, milvus inframsg.MilvusClient, llm agent.LLMClient) *dependencyHealthChecker {
	return &dependencyHealthChecker{
		cfg:    cfg,
		redis:  redis,
		milvus: milvus,
		llm:    llm,
	}
}

func newHealthHandler(checker *dependencyHealthChecker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		snapshot := checker.snapshot(r.Context())
		code := http.StatusOK
		if snapshot.Status == "down" {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(snapshot)
	})
}

func (c *dependencyHealthChecker) snapshot(ctx context.Context) healthzResponse {
	checks := map[string]healthzCheck{
		"redis":  c.checkRedis(ctx),
		"milvus": c.checkMilvus(ctx),
		"llm":    c.checkLLM(ctx),
	}
	status := "ok"
	for _, check := range checks {
		if check.Status == "down" {
			status = "down"
			break
		}
		if check.Status == "degraded" {
			status = "degraded"
		}
	}
	return healthzResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Checks:    checks,
	}
}

func (c *dependencyHealthChecker) checkRedis(ctx context.Context) healthzCheck {
	if c.redis == nil {
		return healthzCheck{Status: "down", Message: "redis client is nil"}
	}
	key := fmt.Sprintf("healthz:%d", time.Now().UnixNano())
	if err := c.redis.Set(ctx, key, "ok", 5*time.Second); err != nil {
		return healthzCheck{Status: "down", Message: err.Error()}
	}
	if err := c.redis.Del(ctx, key); err != nil {
		return healthzCheck{Status: "degraded", Message: err.Error()}
	}
	return healthzCheck{Status: "ok"}
}

func (c *dependencyHealthChecker) checkMilvus(ctx context.Context) healthzCheck {
	if c.milvus == nil {
		return healthzCheck{Status: "down", Message: "milvus client is nil"}
	}
	_, err := c.milvus.Query(ctx, c.cfg.Milvus.Collection)
	if err != nil {
		return healthzCheck{Status: "down", Message: err.Error()}
	}
	return healthzCheck{Status: "ok"}
}

func (c *dependencyHealthChecker) checkLLM(ctx context.Context) healthzCheck {
	if c.llm == nil {
		return healthzCheck{Status: "down", Message: "llm client is nil"}
	}

	health, ok := c.llm.(interface {
		HealthCheck(context.Context) error
	})
	if !ok {
		return healthzCheck{Status: "ok", Message: "health check not required"}
	}

	if err := health.HealthCheck(ctx); err != nil {
		return healthzCheck{Status: "down", Message: err.Error()}
	}
	return healthzCheck{Status: "ok"}
}
