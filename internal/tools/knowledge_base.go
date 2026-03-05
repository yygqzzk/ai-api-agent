package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-agent-api/internal/knowledge"
	"ai-agent-api/internal/rag"
	"ai-agent-api/internal/store"
)

type KnowledgeBase struct {
	mu       sync.RWMutex
	ingestor *knowledge.InMemoryIngestor
	engine   *rag.Engine
	cache    store.RedisClient
}

func NewKnowledgeBase() *KnowledgeBase {
	cache, _ := store.NewRedisClient(store.RedisOptions{Mode: "memory"})
	return NewKnowledgeBaseWithCache(cache)
}

func NewKnowledgeBaseWithCache(cache store.RedisClient) *KnowledgeBase {
	ragStore := rag.NewMemoryStore()
	return NewKnowledgeBaseWithStoreAndCache(ragStore, cache)
}

func NewKnowledgeBaseWithStoreAndCache(ragStore rag.Store, cache store.RedisClient) *KnowledgeBase {
	if cache == nil {
		cache = store.NewInMemoryRedisClient()
	}
	return &KnowledgeBase{
		ingestor: knowledge.NewInMemoryIngestor(),
		engine:   rag.NewEngine(ragStore),
		cache:    cache,
	}
}

func (k *KnowledgeBase) IngestFile(ctx context.Context, path string, service string) (knowledge.IngestStats, error) {
	endpoints, err := knowledge.ParseSwaggerFile(path, service)
	if err != nil {
		return knowledge.IngestStats{}, err
	}
	return k.upsertEndpoints(ctx, endpoints)
}

func (k *KnowledgeBase) IngestURL(ctx context.Context, rawURL string, service string) (knowledge.IngestStats, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return knowledge.IngestStats{}, fmt.Errorf("download swagger url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return knowledge.IngestStats{}, fmt.Errorf("download swagger url failed: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return knowledge.IngestStats{}, fmt.Errorf("read swagger body: %w", err)
	}
	endpoints, err := knowledge.ParseSwaggerBytes(body, service)
	if err != nil {
		return knowledge.IngestStats{}, err
	}
	return k.upsertEndpoints(ctx, endpoints)
}

func (k *KnowledgeBase) upsertEndpoints(ctx context.Context, endpoints []knowledge.Endpoint) (knowledge.IngestStats, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	stats := k.ingestor.Upsert(endpoints)
	if err := k.engine.Index(ctx, endpoints, "v1.0.0"); err != nil {
		return stats, fmt.Errorf("index endpoints: %w", err)
	}
	return stats, nil
}

func (k *KnowledgeBase) Search(ctx context.Context, query string, topK int, service string) ([]rag.ScoredChunk, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.engine.Search(ctx, query, topK, service)
}

func (k *KnowledgeBase) GetEndpoint(service string, endpoint string) (knowledge.Endpoint, bool) {
	if strings.TrimSpace(service) != "" {
		if ep, ok := k.getEndpointFromCache(service, endpoint); ok {
			return ep, true
		}
	}

	method, path := splitEndpoint(endpoint)
	if method == "" || path == "" {
		return knowledge.Endpoint{}, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	for _, ep := range k.ingestor.Endpoints() {
		if service != "" && !strings.EqualFold(ep.Service, service) {
			continue
		}
		if strings.EqualFold(ep.Method, method) && ep.Path == path {
			_ = k.setEndpointCache(ep.Service, endpoint, ep)
			return ep, true
		}
	}
	return knowledge.Endpoint{}, false
}

func (k *KnowledgeBase) Endpoints() []knowledge.Endpoint {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.ingestor.Endpoints()
}

func splitEndpoint(endpoint string) (method string, path string) {
	parts := strings.Fields(strings.TrimSpace(endpoint))
	if len(parts) < 2 {
		return "", ""
	}
	return strings.ToUpper(parts[0]), parts[1]
}

func endpointCacheKey(service string, endpoint string) string {
	return fmt.Sprintf("api:detail:%s:%s", strings.ToLower(strings.TrimSpace(service)), strings.TrimSpace(endpoint))
}

func (k *KnowledgeBase) getEndpointFromCache(service string, endpoint string) (knowledge.Endpoint, bool) {
	if k.cache == nil {
		return knowledge.Endpoint{}, false
	}
	ctx := context.Background()
	key := endpointCacheKey(service, endpoint)
	v, found, err := k.cache.Get(ctx, key)
	if err != nil || !found {
		return knowledge.Endpoint{}, false
	}
	var ep knowledge.Endpoint
	if err := json.Unmarshal([]byte(v), &ep); err != nil {
		return knowledge.Endpoint{}, false
	}
	return ep, true
}

func (k *KnowledgeBase) setEndpointCache(service string, endpoint string, ep knowledge.Endpoint) error {
	if k.cache == nil || strings.TrimSpace(service) == "" {
		return nil
	}
	body, err := json.Marshal(ep)
	if err != nil {
		return err
	}
	return k.cache.Set(context.Background(), endpointCacheKey(service, endpoint), string(body), time.Hour)
}
