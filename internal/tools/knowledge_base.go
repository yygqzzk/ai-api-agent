package tools

import (
	"context"
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
	ingestor knowledge.Ingestor
	engine   *rag.Engine
}

func NewKnowledgeBaseWithRedis(redisClient store.RedisClient, ragStore rag.Store) *KnowledgeBase {
	return NewKnowledgeBaseWithStores(knowledge.NewRedisIngestor(redisClient), ragStore)
}

func NewKnowledgeBaseWithIngestor(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
	return NewKnowledgeBaseWithStores(ingestor, ragStore)
}

func NewKnowledgeBaseWithStores(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
	if ragStore == nil {
		ragStore = rag.NewMemoryStore()
	}
	return &KnowledgeBase{
		ingestor: ingestor,
		engine:   rag.NewEngine(ragStore),
	}
}

func (k *KnowledgeBase) IngestFile(ctx context.Context, path string, service string) (knowledge.IngestStats, error) {
	_, stats, err := k.IngestFileDocument(ctx, path, service)
	return stats, err
}

func (k *KnowledgeBase) IngestFileDocument(ctx context.Context, path string, service string) (knowledge.ParsedSpec, knowledge.IngestStats, error) {
	doc, err := knowledge.ParseSwaggerDocumentFile(path, service)
	if err != nil {
		return knowledge.ParsedSpec{}, knowledge.IngestStats{}, err
	}
	stats, err := k.upsertDocument(ctx, doc)
	return doc, stats, err
}

func (k *KnowledgeBase) IngestBytes(ctx context.Context, body []byte, service string) (knowledge.IngestStats, error) {
	_, stats, err := k.IngestBytesDocument(ctx, body, service)
	return stats, err
}

func (k *KnowledgeBase) IngestBytesDocument(ctx context.Context, body []byte, service string) (knowledge.ParsedSpec, knowledge.IngestStats, error) {
	doc, err := knowledge.ParseSwaggerDocumentBytes(body, service)
	if err != nil {
		return knowledge.ParsedSpec{}, knowledge.IngestStats{}, err
	}
	stats, err := k.upsertDocument(ctx, doc)
	return doc, stats, err
}

func (k *KnowledgeBase) IngestURL(ctx context.Context, rawURL string, service string) (knowledge.IngestStats, error) {
	_, stats, err := k.IngestURLDocument(ctx, rawURL, service)
	return stats, err
}

func (k *KnowledgeBase) IngestURLDocument(ctx context.Context, rawURL string, service string) (knowledge.ParsedSpec, knowledge.IngestStats, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return knowledge.ParsedSpec{}, knowledge.IngestStats{}, fmt.Errorf("download swagger url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return knowledge.ParsedSpec{}, knowledge.IngestStats{}, fmt.Errorf("download swagger url failed: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return knowledge.ParsedSpec{}, knowledge.IngestStats{}, fmt.Errorf("read swagger body: %w", err)
	}
	return k.IngestBytesDocument(ctx, body, service)
}

func (k *KnowledgeBase) upsertEndpoints(ctx context.Context, endpoints []knowledge.Endpoint) (knowledge.IngestStats, error) {
	return k.upsertDocument(ctx, knowledge.ParsedSpec{Endpoints: endpoints})
}

func (k *KnowledgeBase) upsertDocument(ctx context.Context, doc knowledge.ParsedSpec) (knowledge.IngestStats, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	service := strings.TrimSpace(doc.Meta.Service)
	if service == "" && len(doc.Endpoints) > 0 {
		service = strings.TrimSpace(doc.Endpoints[0].Service)
	}
	service = strings.ToLower(service)

	newChunks := rag.BuildChunks(doc.Endpoints, "v1.0.0")
	newIDs := make([]string, 0, len(newChunks))
	for _, chunk := range newChunks {
		newIDs = append(newIDs, chunk.ID)
	}

	if service != "" {
		oldIDs := k.ingestor.ChunkIDs(service)
		if oldIDs == nil {
			if err := k.engine.DeleteByService(ctx, service); err != nil {
				return knowledge.IngestStats{}, fmt.Errorf("delete stale vectors for service %q: %w", service, err)
			}
		} else {
			removedIDs := subtract(oldIDs, newIDs)
			if err := k.engine.DeleteByIDs(ctx, removedIDs); err != nil {
				return knowledge.IngestStats{}, fmt.Errorf("delete removed vectors for service %q: %w", service, err)
			}
		}
	}

	stats := k.ingestor.UpsertDocument(doc)
	if err := k.engine.Index(ctx, doc.Endpoints, "v1.0.0"); err != nil {
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
			return ep, true
		}
	}
	return knowledge.Endpoint{}, false
}

func (k *KnowledgeBase) GetSpecMeta(service string) (knowledge.SpecMeta, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.ingestor.SpecMeta(service)
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

func subtract(oldIDs []string, newIDs []string) []string {
	if len(oldIDs) == 0 {
		return nil
	}

	newSet := make(map[string]struct{}, len(newIDs))
	for _, id := range newIDs {
		newSet[id] = struct{}{}
	}

	removed := make([]string, 0)
	for _, id := range oldIDs {
		if _, ok := newSet[id]; ok {
			continue
		}
		removed = append(removed, id)
	}
	return removed
}
