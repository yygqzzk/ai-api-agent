package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-agent-api/internal/agent"
	"ai-agent-api/internal/config"
	"ai-agent-api/internal/store"
)

func TestHealthzAllHealthy(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "openai"

	checker := newHealthDependencyChecker(cfg, &fakeRedisClient{}, &fakeMilvusClient{}, &fakeLLMClient{healthErr: nil})
	handler := newHealthHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp healthzResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected overall status ok, got %s", resp.Status)
	}
	if resp.Checks["redis"].Status != "ok" || resp.Checks["milvus"].Status != "ok" || resp.Checks["llm"].Status != "ok" {
		t.Fatalf("unexpected checks: %+v", resp.Checks)
	}
}

func TestHealthzDependencyDown(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "openai"

	checker := newHealthDependencyChecker(cfg,
		&fakeRedisClient{setErr: errors.New("redis down")},
		&fakeMilvusClient{queryErr: errors.New("milvus down")},
		&fakeLLMClient{healthErr: errors.New("llm down")},
	)
	handler := newHealthHandler(checker)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}

	var resp healthzResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.Status != "down" {
		t.Fatalf("expected overall status down, got %s", resp.Status)
	}
	if resp.Checks["redis"].Status != "down" || resp.Checks["milvus"].Status != "down" || resp.Checks["llm"].Status != "down" {
		t.Fatalf("unexpected checks: %+v", resp.Checks)
	}
}

func TestHealthzMethodNotAllowed(t *testing.T) {
	checker := newHealthDependencyChecker(config.Default(), &fakeRedisClient{}, nil, &fakeLLMClient{})
	handler := newHealthHandler(checker)

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

type fakeRedisClient struct {
	setErr error
	delErr error
}

func (f *fakeRedisClient) Set(_ context.Context, _ string, _ string, _ time.Duration) error {
	return f.setErr
}
func (f *fakeRedisClient) Get(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil
}
func (f *fakeRedisClient) Del(_ context.Context, _ string) error {
	return f.delErr
}
func (f *fakeRedisClient) Close(_ context.Context) error { return nil }

type fakeMilvusClient struct {
	queryErr error
}

func (f *fakeMilvusClient) Upsert(_ context.Context, _ string, _ []store.VectorDoc) error { return nil }
func (f *fakeMilvusClient) Search(_ context.Context, _ string, _ []float32, _ int, _ map[string]string) ([]store.SearchResult, error) {
	return nil, nil
}
func (f *fakeMilvusClient) Query(_ context.Context, _ string) ([]store.VectorDoc, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return []store.VectorDoc{}, nil
}
func (f *fakeMilvusClient) Close(_ context.Context) error { return nil }

type fakeLLMClient struct {
	healthErr error
}

func (f *fakeLLMClient) Next(_ context.Context, _ []agent.Message, _ []agent.ToolDefinition) (agent.LLMReply, error) {
	return agent.LLMReply{Content: "ok"}, nil
}
func (f *fakeLLMClient) HealthCheck(_ context.Context) error {
	return f.healthErr
}
