package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Server.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Agent.MaxSteps != 10 {
		t.Fatalf("expected default max steps 10, got %d", cfg.Agent.MaxSteps)
	}

	if cfg.RAG.TopK != 20 || cfg.RAG.TopN != 5 {
		t.Fatalf("unexpected rag defaults: %+v", cfg.RAG)
	}

	if cfg.LLM.TimeoutSeconds != 30 || cfg.LLM.MaxRetries != 2 || cfg.LLM.RetryBackoffMS != 200 {
		t.Fatalf("unexpected llm retry/timeout defaults: %+v", cfg.LLM)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("AUTH_TOKEN", "test-token")
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("LLM_API_KEY", "k1")
	t.Setenv("LLM_MODEL", "gpt-x")
	t.Setenv("LLM_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("LLM_MAX_TOKENS", "8192")
	t.Setenv("LLM_TIMEOUT_SECONDS", "15")
	t.Setenv("LLM_MAX_RETRIES", "4")
	t.Setenv("LLM_RETRY_BACKOFF_MS", "120")
	t.Setenv("MILVUS_MODE", "milvus")
	t.Setenv("MILVUS_ADDRESS", "127.0.0.1:19530")
	t.Setenv("REDIS_ADDRESS", "127.0.0.1:6379")
	t.Setenv("REDIS_MODE", "redis")

	cfg := Default()
	if err := cfg.ApplyEnv(os.LookupEnv); err != nil {
		t.Fatalf("ApplyEnv failed: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Fatalf("expected overridden port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.AuthToken != "test-token" {
		t.Fatalf("expected auth token override, got %q", cfg.Server.AuthToken)
	}
	if cfg.LLM.APIKey != "k1" || cfg.LLM.Model != "gpt-x" ||
		cfg.LLM.Provider != "openai" || cfg.LLM.BaseURL != "http://localhost:11434/v1" || cfg.LLM.MaxTokens != 8192 ||
		cfg.LLM.TimeoutSeconds != 15 || cfg.LLM.MaxRetries != 4 || cfg.LLM.RetryBackoffMS != 120 {
		t.Fatalf("expected llm overrides, got %+v", cfg.LLM)
	}
	if cfg.Milvus.Mode != "milvus" {
		t.Fatalf("expected milvus mode override, got %q", cfg.Milvus.Mode)
	}
	if cfg.Milvus.Address != "127.0.0.1:19530" {
		t.Fatalf("expected milvus override, got %q", cfg.Milvus.Address)
	}
	if cfg.Redis.Address != "127.0.0.1:6379" {
		t.Fatalf("expected redis override, got %q", cfg.Redis.Address)
	}
	if cfg.Redis.Mode != "redis" {
		t.Fatalf("expected redis mode override, got %q", cfg.Redis.Mode)
	}
}

func TestApplyEnvInvalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-number")
	cfg := Default()
	err := cfg.ApplyEnv(os.LookupEnv)
	if err == nil {
		t.Fatalf("expected invalid port error")
	}
}

func TestApplyEnvInvalidLLMMaxTokens(t *testing.T) {
	t.Setenv("LLM_MAX_TOKENS", "not-a-number")
	cfg := Default()
	err := cfg.ApplyEnv(os.LookupEnv)
	if err == nil {
		t.Fatalf("expected invalid LLM_MAX_TOKENS error")
	}
}

func TestApplyEnvInvalidLLMTimeoutSeconds(t *testing.T) {
	t.Setenv("LLM_TIMEOUT_SECONDS", "not-a-number")
	cfg := Default()
	err := cfg.ApplyEnv(os.LookupEnv)
	if err == nil {
		t.Fatalf("expected invalid LLM_TIMEOUT_SECONDS error")
	}
}
