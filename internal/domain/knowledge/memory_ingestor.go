package knowledge

import (
	"context"
	"fmt"
	"sync"

	"wanzhi/internal/domain/model"
)

// MemoryIngestor 内存实现的知识录入器
type MemoryIngestor struct {
	mu        sync.RWMutex
	endpoints []model.Endpoint
	chunks    []model.Chunk
	specs     map[string]model.SpecMeta
	rawSpecs  map[string][]byte
	version   string
}

var _ Ingestor = (*MemoryIngestor)(nil)

// NewMemoryIngestor 创建内存录入器实例
func NewMemoryIngestor() *MemoryIngestor {
	return &MemoryIngestor{
		specs:    make(map[string]model.SpecMeta),
		rawSpecs: make(map[string][]byte),
		version:  "v1.0.0",
	}
}

func (i *MemoryIngestor) UpsertDocument(doc ParsedSpec) IngestStats {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.upsertEndpointsLocked(doc.Endpoints)
	if meta, ok := normalizeSpecMeta(doc.Meta, doc.Endpoints); ok {
		i.specs[canonicalServiceKey(meta.Service)] = meta
	}
	i.rebuildChunksLocked()
	return IngestStats{
		Endpoints: len(doc.Endpoints),
		Chunks:    len(i.chunks),
	}
}

func (i *MemoryIngestor) Endpoints() []model.Endpoint {
	i.mu.RLock()
	defer i.mu.RUnlock()

	out := make([]model.Endpoint, len(i.endpoints))
	copy(out, i.endpoints)
	return out
}

func (i *MemoryIngestor) Chunks() []model.Chunk {
	i.mu.RLock()
	defer i.mu.RUnlock()

	out := make([]model.Chunk, len(i.chunks))
	copy(out, i.chunks)
	return out
}

func (i *MemoryIngestor) ChunkIDs(service string) []string {
	key := canonicalServiceKey(service)
	if key == "" {
		return nil
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	ids := make([]string, 0)
	for _, chunk := range i.chunks {
		if canonicalServiceKey(chunk.Service) != key {
			continue
		}
		ids = append(ids, chunk.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func (i *MemoryIngestor) SpecMeta(service string) (model.SpecMeta, bool) {
	key := canonicalServiceKey(service)
	if key == "" {
		return model.SpecMeta{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	meta, ok := i.specs[key]
	if !ok {
		return model.SpecMeta{}, false
	}
	return cloneSpecMeta(meta), true
}

func (i *MemoryIngestor) SaveSpec(ctx context.Context, service string, spec []byte) error {
	key := canonicalServiceKey(service)
	if key == "" {
		return fmt.Errorf("invalid service name")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.rawSpecs[key] = append([]byte(nil), spec...)
	return nil
}

func (i *MemoryIngestor) LoadSpec(ctx context.Context, service string) ([]byte, error) {
	key := canonicalServiceKey(service)
	if key == "" {
		return nil, fmt.Errorf("invalid service name")
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	spec, ok := i.rawSpecs[key]
	if !ok {
		return nil, fmt.Errorf("spec not found for service: %s", service)
	}
	return append([]byte(nil), spec...), nil
}

func (i *MemoryIngestor) DeleteService(ctx context.Context, service string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	key := canonicalServiceKey(service)
	if key == "" {
		return fmt.Errorf("invalid service name")
	}

	// Remove endpoints
	filtered := i.endpoints[:0]
	for _, ep := range i.endpoints {
		if canonicalServiceKey(ep.Service) != key {
			filtered = append(filtered, ep)
		}
	}
	i.endpoints = filtered

	// Remove chunks
	filteredChunks := i.chunks[:0]
	for _, chunk := range i.chunks {
		if canonicalServiceKey(chunk.Service) != key {
			filteredChunks = append(filteredChunks, chunk)
		}
	}
	i.chunks = filteredChunks

	// Remove spec
	delete(i.specs, key)
	delete(i.rawSpecs, key)

	return nil
}

func (i *MemoryIngestor) ListEndpoints(ctx context.Context, service string) ([]model.Endpoint, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	key := canonicalServiceKey(service)
	if key == "" {
		return nil, fmt.Errorf("invalid service name")
	}

	result := make([]model.Endpoint, 0)
	for _, ep := range i.endpoints {
		if canonicalServiceKey(ep.Service) == key {
			result = append(result, ep)
		}
	}
	return result, nil
}

func (i *MemoryIngestor) SaveEndpoints(ctx context.Context, service string, endpoints []model.Endpoint) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.upsertEndpointsLocked(endpoints)
	return nil
}

func (i *MemoryIngestor) SaveChunks(ctx context.Context, service string, chunks []model.Chunk) error {
	key := canonicalServiceKey(service)
	if key == "" {
		return fmt.Errorf("invalid service name")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	filtered := make([]model.Chunk, 0, len(i.chunks))
	for _, c := range i.chunks {
		if canonicalServiceKey(c.Service) != key {
			filtered = append(filtered, c)
		}
	}
	i.chunks = append(filtered, chunks...)
	return nil
}

func (i *MemoryIngestor) LoadChunks(ctx context.Context, service string) ([]model.Chunk, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	key := canonicalServiceKey(service)
	if key == "" {
		return nil, fmt.Errorf("invalid service name")
	}

	result := make([]model.Chunk, 0)
	for _, chunk := range i.chunks {
		if canonicalServiceKey(chunk.Service) == key {
			result = append(result, chunk)
		}
	}
	return result, nil
}

func (i *MemoryIngestor) upsertEndpointsLocked(endpoints []model.Endpoint) {
	index := make(map[string]int, len(i.endpoints))
	for idx := range i.endpoints {
		index[i.endpoints[idx].Key()] = idx
	}
	for _, ep := range endpoints {
		key := ep.Key()
		if idx, ok := index[key]; ok {
			i.endpoints[idx] = ep
			continue
		}
		index[key] = len(i.endpoints)
		i.endpoints = append(i.endpoints, ep)
	}
}

func (i *MemoryIngestor) rebuildChunksLocked() {
	chunks := make([]model.Chunk, 0, len(i.endpoints)*4)
	for _, ep := range i.endpoints {
		chunks = append(chunks, buildChunksForEndpoint(ep, i.version)...)
	}
	i.chunks = chunks
}
