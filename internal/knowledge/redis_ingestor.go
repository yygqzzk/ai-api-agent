package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"ai-agent-api/internal/store"
)

// RedisIngestor 使用 Redis 持久化存储知识库数据。
// 采用全量替换策略：每次 UpsertDocument 会完全替换该 service 的所有数据，
// 确保 Redis 中的数据与导入的文档完全一致（包括删除文档中已移除的接口）。
type RedisIngestor struct {
	client  store.RedisClient
	mu      sync.RWMutex
	version string
}

var _ Ingestor = (*RedisIngestor)(nil)

// NewRedisIngestor 创建 Redis 持久化的 Ingestor。
func NewRedisIngestor(client store.RedisClient) *RedisIngestor {
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

func endpointField(ep Endpoint) string {
	return fmt.Sprintf("%s:%s", strings.ToUpper(strings.TrimSpace(ep.Method)), ep.Path)
}

func (r *RedisIngestor) UpsertDocument(doc ParsedSpec) IngestStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	service := extractService(doc)
	if service == "" {
		return IngestStats{}
	}

	if err := r.client.SAdd(ctx, servicesKey(), service); err != nil {
		return IngestStats{}
	}

	// 全量替换：先删除该 service 的所有旧数据
	_ = r.client.Del(ctx, endpointsKey(service))

	stats := IngestStats{}
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
	chunks := make([]Chunk, 0, len(doc.Endpoints)*4)
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

func (r *RedisIngestor) Endpoints() []Endpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.allEndpointsLocked(context.Background())
}

func (r *RedisIngestor) Chunks() []Chunk {
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

func (r *RedisIngestor) SpecMeta(service string) (SpecMeta, bool) {
	key := canonicalServiceKey(service)
	if key == "" {
		return SpecMeta{}, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	value, found, err := r.client.Get(context.Background(), specsKey(key))
	if err != nil || !found {
		return SpecMeta{}, false
	}

	var meta SpecMeta
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return SpecMeta{}, false
	}
	return cloneSpecMeta(meta), true
}

func (r *RedisIngestor) allEndpointsLocked(ctx context.Context) []Endpoint {
	services := r.getAllServices(ctx)
	result := make([]Endpoint, 0)
	for _, service := range services {
		result = append(result, r.getServiceEndpoints(ctx, service)...)
	}
	sort.Slice(result, func(i int, j int) bool {
		return result[i].Key() < result[j].Key()
	})
	return result
}

func (r *RedisIngestor) allChunksLocked(ctx context.Context) []Chunk {
	services := r.getAllServices(ctx)
	result := make([]Chunk, 0)
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

func (r *RedisIngestor) getServiceEndpoints(ctx context.Context, service string) []Endpoint {
	fields, err := r.client.HGetAll(ctx, endpointsKey(service))
	if err != nil || len(fields) == 0 {
		return nil
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]Endpoint, 0, len(keys))
	for _, key := range keys {
		var ep Endpoint
		if err := json.Unmarshal([]byte(fields[key]), &ep); err != nil {
			continue
		}
		result = append(result, ep)
	}
	return result
}

func (r *RedisIngestor) getServiceChunks(ctx context.Context, service string) []Chunk {
	values, err := r.client.LRange(ctx, chunksKey(service), 0, -1)
	if err != nil || len(values) == 0 {
		return nil
	}

	result := make([]Chunk, 0, len(values))
	for _, value := range values {
		var chunk Chunk
		if err := json.Unmarshal([]byte(value), &chunk); err != nil {
			continue
		}
		result = append(result, chunk)
	}
	return result
}

func extractService(doc ParsedSpec) string {
	service := strings.TrimSpace(doc.Meta.Service)
	if service == "" && len(doc.Endpoints) > 0 {
		service = strings.TrimSpace(doc.Endpoints[0].Service)
	}
	return canonicalServiceKey(service)
}
