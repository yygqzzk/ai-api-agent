package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ai-agent-api/internal/knowledge"
	"ai-agent-api/internal/store"
)

func TestGetEndpointFromCache(t *testing.T) {
	cache, err := store.NewRedisClient(store.RedisOptions{Mode: "memory"})
	if err != nil {
		t.Fatalf("new memory cache failed: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close(context.Background()) })

	ep := knowledge.Endpoint{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "login"}
	body, err := json.Marshal(ep)
	if err != nil {
		t.Fatalf("marshal endpoint failed: %v", err)
	}

	key := "api:detail:petstore:GET /user/login"
	if err := cache.Set(context.Background(), key, string(body), time.Hour); err != nil {
		t.Fatalf("cache set failed: %v", err)
	}

	kb := NewKnowledgeBaseWithCache(cache)
	got, ok := kb.GetEndpoint("petstore", "GET /user/login")
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if got.Path != "/user/login" || got.Method != "GET" {
		t.Fatalf("unexpected endpoint from cache: %+v", got)
	}
}

func TestGetEndpointPopulateCache(t *testing.T) {
	cache, err := store.NewRedisClient(store.RedisOptions{Mode: "memory"})
	if err != nil {
		t.Fatalf("new memory cache failed: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close(context.Background()) })

	kb := NewKnowledgeBaseWithCache(cache)
	stats, err := kb.upsertEndpoints(context.Background(), []knowledge.Endpoint{{
		Service: "petstore",
		Method:  "GET",
		Path:    "/user/login",
		Summary: "login",
	}})
	if err != nil {
		t.Fatalf("upsert endpoints failed: %v", err)
	}
	if stats.Endpoints != 1 {
		t.Fatalf("expected 1 endpoint upserted, got %d", stats.Endpoints)
	}

	_, ok := kb.GetEndpoint("petstore", "GET /user/login")
	if !ok {
		t.Fatalf("expected endpoint found")
	}

	v, found, err := cache.Get(context.Background(), "api:detail:petstore:GET /user/login")
	if err != nil {
		t.Fatalf("cache get failed: %v", err)
	}
	if !found || v == "" {
		t.Fatalf("expected populated cache for endpoint detail")
	}
}
