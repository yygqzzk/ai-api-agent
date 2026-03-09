package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"wanzhi/internal/domain/agent"
	"wanzhi/internal/config"
	"wanzhi/internal/infra/llm"
)

func TestNewLLMClientSelectsOpenAI(t *testing.T) {
	cfg, _ := config.LoadFromEnv()
	cfg.LLM.Provider = "openai"
	cfg.LLM.APIKey = "k1"

	client := newLLMClient(cfg)
	if _, ok := client.(*llm.OpenAICompatibleLLMClient); !ok {
		t.Fatalf("expected OpenAICompatibleLLMClient, got %T", client)
	}
}

func TestNewLLMClientFallsBackToRuleBased(t *testing.T) {
	cfg, _ := config.LoadFromEnv()
	cfg.LLM.Provider = "openai"
	cfg.LLM.APIKey = ""
	cfg.LLM.BaseURL = ""

	client := newLLMClient(cfg)
	if _, ok := client.(*llm.RuleBasedLLMClient); !ok {
		t.Fatalf("expected RuleBasedLLMClient, got %T", client)
	}
}

func TestNewLLMClientAppliesRetryConfig(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "upstream broken", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg, _ := config.LoadFromEnv()
	cfg.LLM.Provider = "openai"
	cfg.LLM.APIKey = "k1"
	cfg.LLM.BaseURL = srv.URL
	cfg.LLM.MaxRetries = 2
	cfg.LLM.RetryBackoffMS = 1

	client := newLLMClient(cfg)
	openaiClient, ok := client.(*llm.OpenAICompatibleLLMClient)
	if !ok {
		t.Fatalf("expected OpenAICompatibleLLMClient, got %T", client)
	}

	_, err := openaiClient.Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected retry-exhausted error")
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 calls based on retry config, got %d", atomic.LoadInt32(&calls))
	}
}
