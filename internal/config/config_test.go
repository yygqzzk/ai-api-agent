package config

import (
	"testing"
)

func TestLoadFromEnv_Defaults(t *testing.T) {
	cfg, err := LoadFromEnv()
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
	if cfg.Agent.MaxSteps != 10 {
		t.Errorf("expected max steps 10, got %d", cfg.Agent.MaxSteps)
	}
	if cfg.RAG.TopK != 20 || cfg.RAG.TopN != 5 {
		t.Errorf("unexpected rag defaults: %+v", cfg.RAG)
	}
	if cfg.LLM.TimeoutSeconds != 30 || cfg.LLM.MaxRetries != 2 || cfg.LLM.RetryBackoffMS != 200 {
		t.Errorf("unexpected llm retry/timeout defaults: %+v", cfg.LLM)
	}
}

func TestLoadFromEnv_Override(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("AUTH_TOKEN", "test-token")
	t.Setenv("LLM_API_KEY", "test-key")
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("LLM_MODEL", "gpt-x")
	t.Setenv("LLM_BASE_URL", "http://localhost:11434/v1")
	t.Setenv("LLM_MAX_TOKENS", "8192")
	t.Setenv("LLM_TIMEOUT_SECONDS", "15")
	t.Setenv("LLM_MAX_RETRIES", "4")
	t.Setenv("LLM_RETRY_BACKOFF_MS", "120")
	t.Setenv("REDIS_ADDRESS", "redis:6379")
	t.Setenv("REDIS_PASSWORD", "redis-pass")
	t.Setenv("REDIS_DB", "3")
	t.Setenv("MILVUS_ADDRESS", "127.0.0.1:19530")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.AuthToken != "test-token" {
		t.Errorf("expected auth token test-token, got %s", cfg.Server.AuthToken)
	}
	if cfg.LLM.APIKey != "test-key" {
		t.Errorf("expected api key test-key, got %s", cfg.LLM.APIKey)
	}
	if cfg.LLM.Model != "gpt-x" {
		t.Errorf("expected model gpt-x, got %s", cfg.LLM.Model)
	}
	if cfg.LLM.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("expected base URL, got %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.MaxTokens != 8192 {
		t.Errorf("expected max tokens 8192, got %d", cfg.LLM.MaxTokens)
	}
	if cfg.LLM.TimeoutSeconds != 15 || cfg.LLM.MaxRetries != 4 || cfg.LLM.RetryBackoffMS != 120 {
		t.Errorf("expected llm overrides, got %+v", cfg.LLM)
	}
	if cfg.Redis.Address != "redis:6379" {
		t.Errorf("expected redis:6379, got %s", cfg.Redis.Address)
	}
	if cfg.Redis.Password != "redis-pass" || cfg.Redis.DB != 3 {
		t.Errorf("expected redis password/db overrides, got %+v", cfg.Redis)
	}
	if cfg.Milvus.Address != "127.0.0.1:19530" {
		t.Errorf("expected milvus override, got %q", cfg.Milvus.Address)
	}
}

func TestLoadFromEnv_InvalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-number")
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatalf("expected invalid port error")
	}
}

func TestLoadFromEnv_InvalidRedisDB(t *testing.T) {
	t.Setenv("REDIS_DB", "not-a-number")
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatalf("expected invalid REDIS_DB error")
	}
}

func TestLoadFromEnv_InvalidLLMMaxTokens(t *testing.T) {
	t.Setenv("LLM_MAX_TOKENS", "not-a-number")
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatalf("expected invalid LLM_MAX_TOKENS error")
	}
}

func TestLoadFromEnv_InvalidLLMTimeoutSeconds(t *testing.T) {
	t.Setenv("LLM_TIMEOUT_SECONDS", "not-a-number")
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatalf("expected invalid LLM_TIMEOUT_SECONDS error")
	}
}
