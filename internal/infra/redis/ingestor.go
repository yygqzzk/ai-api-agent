package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"wanzhi/internal/domain/knowledge"
	"wanzhi/internal/domain/model"
)

// RedisIngestor 使用 Redis 持久化存储知识库数据。
// 采用全量替换策略：每次 UpsertDocument 会完全替换该 service 的所有数据，
// 确保 Redis 中的数据与导入的文档完全一致（包括删除文档中已移除的接口）。
type RedisIngestor struct {
	client  RedisClient
	mu      sync.RWMutex
	version string
}

var _ knowledge.Ingestor = (*RedisIngestor)(nil)

// NewRedisIngestor 创建 Redis 持久化的 Ingestor。
func NewRedisIngestor(client RedisClient) *RedisIngestor {
	if client == nil {
		panic("redis client cannot be nil")
	}
	return &RedisIngestor{
		client:  client,
		version: "v1.0.0",
	}
}

func endpointsKey(service string) string {
	return fmt.Sprintf("kb:endpoints:%s", canonicalServiceKey(service))
}

func specsKey(service string) string {
	return fmt.Sprintf("kb:specs:%s", canonicalServiceKey(service))
}

func chunksKey(service string) string {
	return fmt.Sprintf("kb:chunks:%s", canonicalServiceKey(service))
}

func servicesKey() string {
	return "kb:services"
}

func endpointField(ep model.Endpoint) string {
	return fmt.Sprintf("%s:%s", strings.ToUpper(strings.TrimSpace(ep.Method)), ep.Path)
}

func (r *RedisIngestor) UpsertDocument(doc knowledge.ParsedSpec) knowledge.IngestStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	service := extractService(doc)
	if service == "" {
		return knowledge.IngestStats{}
	}

	if err := r.client.SAdd(ctx, servicesKey(), service); err != nil {
		return knowledge.IngestStats{}
	}

	// 全量替换：先删除该 service 的所有旧数据
	_ = r.client.Del(ctx, endpointsKey(service))

	stats := knowledge.IngestStats{}
	for _, ep := range doc.Endpoints {
		value, err := json.Marshal(ep)
		if err != nil {
			continue
		}
		if err := r.client.HSet(ctx, endpointsKey(service), endpointField(ep), string(value)); err != nil {
			continue
		}
		stats.Endpoints++
	}

	if meta, ok := normalizeSpecMeta(doc.Meta, doc.Endpoints); ok {
		value, err := json.Marshal(meta)
		if err == nil {
			_ = r.client.Set(ctx, specsKey(service), string(value), 0)
		}
	}

	// 基于新导入的 endpoints 重建 chunks（而不是从 Redis 读取）
	chunks := make([]model.Chunk, 0, len(doc.Endpoints)*4)
	for _, ep := range doc.Endpoints {
		chunks = append(chunks, buildChunksForEndpoint(ep, r.version)...)
	}

	_ = r.client.Del(ctx, chunksKey(service))
	if len(chunks) > 0 {
		values := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			body, err := json.Marshal(chunk)
			if err != nil {
				continue
			}
			values = append(values, string(body))
		}
		if len(values) > 0 {
			_ = r.client.RPush(ctx, chunksKey(service), values...)
		}
	}

	stats.Chunks = len(chunks)
	return stats
}

func (r *RedisIngestor) Endpoints() []model.Endpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.allEndpointsLocked(context.Background())
}

func (r *RedisIngestor) Chunks() []model.Chunk {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.allChunksLocked(context.Background())
}

func (r *RedisIngestor) ChunkIDs(service string) []string {
	key := canonicalServiceKey(service)
	if key == "" {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	chunks := r.getServiceChunks(context.Background(), key)
	if len(chunks) == 0 {
		return nil
	}

	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	return ids
}

func (r *RedisIngestor) SpecMeta(service string) (model.SpecMeta, bool) {
	key := canonicalServiceKey(service)
	if key == "" {
		return model.SpecMeta{}, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	value, found, err := r.client.Get(context.Background(), specsKey(key))
	if err != nil || !found {
		return model.SpecMeta{}, false
	}

	var meta model.SpecMeta
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return model.SpecMeta{}, false
	}
	return cloneSpecMeta(meta), true
}

func (r *RedisIngestor) allEndpointsLocked(ctx context.Context) []model.Endpoint {
	services := r.getAllServices(ctx)
	result := make([]model.Endpoint, 0)
	for _, service := range services {
		result = append(result, r.getServiceEndpoints(ctx, service)...)
	}
	sort.Slice(result, func(i int, j int) bool {
		return result[i].Key() < result[j].Key()
	})
	return result
}

func (r *RedisIngestor) allChunksLocked(ctx context.Context) []model.Chunk {
	services := r.getAllServices(ctx)
	result := make([]model.Chunk, 0)
	for _, service := range services {
		result = append(result, r.getServiceChunks(ctx, service)...)
	}
	sort.Slice(result, func(i int, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

func (r *RedisIngestor) totalChunksLocked(ctx context.Context) int {
	total := 0
	for _, service := range r.getAllServices(ctx) {
		total += len(r.getServiceChunks(ctx, service))
	}
	return total
}

func (r *RedisIngestor) getAllServices(ctx context.Context) []string {
	services, err := r.client.SMembers(ctx, servicesKey())
	if err != nil {
		return nil
	}
	sort.Strings(services)
	return services
}

func (r *RedisIngestor) getServiceEndpoints(ctx context.Context, service string) []model.Endpoint {
	fields, err := r.client.HGetAll(ctx, endpointsKey(service))
	if err != nil || len(fields) == 0 {
		return nil
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]model.Endpoint, 0, len(keys))
	for _, key := range keys {
		var ep model.Endpoint
		if err := json.Unmarshal([]byte(fields[key]), &ep); err != nil {
			continue
		}
		result = append(result, ep)
	}
	return result
}

func (r *RedisIngestor) getServiceChunks(ctx context.Context, service string) []model.Chunk {
	values, err := r.client.LRange(ctx, chunksKey(service), 0, -1)
	if err != nil || len(values) == 0 {
		return nil
	}

	result := make([]model.Chunk, 0, len(values))
	for _, value := range values {
		var chunk model.Chunk
		if err := json.Unmarshal([]byte(value), &chunk); err != nil {
			continue
		}
		result = append(result, chunk)
	}
	return result
}

func extractService(doc knowledge.ParsedSpec) string {
	service := strings.TrimSpace(doc.Meta.Service)
	if service == "" && len(doc.Endpoints) > 0 {
		service = strings.TrimSpace(doc.Endpoints[0].Service)
	}
	return canonicalServiceKey(service)
}

// Implement the remaining knowledge.Ingestor interface methods

func (r *RedisIngestor) SaveSpec(ctx context.Context, service string, spec []byte) error {
	key := fmt.Sprintf("kb:specs:%s:raw", canonicalServiceKey(service))
	return r.client.Set(ctx, key, string(spec), 0)
}

func (r *RedisIngestor) LoadSpec(ctx context.Context, service string) ([]byte, error) {
	key := fmt.Sprintf("kb:specs:%s:raw", canonicalServiceKey(service))
	value, found, err := r.client.Get(ctx, key)
	if err != nil || !found {
		return nil, fmt.Errorf("spec not found")
	}
	return []byte(value), nil
}

func (r *RedisIngestor) DeleteService(ctx context.Context, service string) error {
	key := canonicalServiceKey(service)
	if key == "" {
		return fmt.Errorf("invalid service name")
	}

	// Delete all keys for this service
	_ = r.client.Del(ctx, endpointsKey(key))
	_ = r.client.Del(ctx, specsKey(key))
	_ = r.client.Del(ctx, chunksKey(key))
	_ = r.client.Del(ctx, fmt.Sprintf("kb:specs:%s:raw", key))

	// Note: Removing from services set would require SRem which is not in RedisClient interface
	// The service will still be listed but will have no data

	return nil
}

func (r *RedisIngestor) ListEndpoints(ctx context.Context, service string) ([]model.Endpoint, error) {
	key := canonicalServiceKey(service)
	if key == "" {
		return nil, fmt.Errorf("invalid service name")
	}
	return r.getServiceEndpoints(ctx, key), nil
}

func (r *RedisIngestor) SaveEndpoints(ctx context.Context, service string, endpoints []model.Endpoint) error {
	// Implemented via UpsertDocument
	return fmt.Errorf("use UpsertDocument instead")
}

func (r *RedisIngestor) SaveChunks(ctx context.Context, service string, chunks []model.Chunk) error {
	// Implemented via UpsertDocument
	return fmt.Errorf("use UpsertDocument instead")
}

func (r *RedisIngestor) LoadChunks(ctx context.Context, service string) ([]model.Chunk, error) {
	key := canonicalServiceKey(service)
	if key == "" {
		return nil, fmt.Errorf("invalid service name")
	}
	return r.getServiceChunks(ctx, key), nil
}

// Helper functions from knowledge package

func canonicalServiceKey(service string) string {
	return strings.ToLower(strings.TrimSpace(service))
}

func normalizeSpecMeta(meta model.SpecMeta, endpoints []model.Endpoint) (model.SpecMeta, bool) {
	service := strings.TrimSpace(meta.Service)
	if service == "" && len(endpoints) > 0 {
		service = strings.TrimSpace(endpoints[0].Service)
	}
	if service == "" {
		return model.SpecMeta{}, false
	}
	meta.Service = service
	meta.Schemes = append([]string(nil), meta.Schemes...)
	return meta, true
}

func cloneSpecMeta(meta model.SpecMeta) model.SpecMeta {
	meta.Schemes = append([]string(nil), meta.Schemes...)
	return meta
}

func buildChunksForEndpoint(ep model.Endpoint, version string) []model.Chunk {
	// Import chunker from domain/rag package
	// For now, use the helper from knowledge package
	chunks := knowledge.BuildChunks([]model.Endpoint{ep}, version)
	return chunks
}
