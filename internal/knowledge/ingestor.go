package knowledge

import (
	"fmt"
	"strings"
	"sync"
)

type Ingestor interface {
	UpsertDocument(doc ParsedSpec) IngestStats
	Endpoints() []Endpoint
	Chunks() []Chunk
	ChunkIDs(service string) []string
	SpecMeta(service string) (SpecMeta, bool)
}

type InMemoryIngestor struct {
	mu        sync.RWMutex
	endpoints []Endpoint
	chunks    []Chunk
	specs     map[string]SpecMeta
	version   string
}

var _ Ingestor = (*InMemoryIngestor)(nil)

func NewInMemoryIngestor() *InMemoryIngestor {
	return &InMemoryIngestor{
		specs:   make(map[string]SpecMeta),
		version: "v1.0.0",
	}
}

func (i *InMemoryIngestor) IngestFile(path string, service string) (IngestStats, error) {
	doc, err := ParseSwaggerDocumentFile(path, service)
	if err != nil {
		return IngestStats{}, err
	}
	return i.UpsertDocument(doc), nil
}

func (i *InMemoryIngestor) Upsert(endpoints []Endpoint) IngestStats {
	return i.UpsertDocument(ParsedSpec{Endpoints: endpoints})
}

func (i *InMemoryIngestor) UpsertDocument(doc ParsedSpec) IngestStats {
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

func (i *InMemoryIngestor) Endpoints() []Endpoint {
	i.mu.RLock()
	defer i.mu.RUnlock()

	out := make([]Endpoint, len(i.endpoints))
	copy(out, i.endpoints)
	return out
}

func (i *InMemoryIngestor) Chunks() []Chunk {
	i.mu.RLock()
	defer i.mu.RUnlock()

	out := make([]Chunk, len(i.chunks))
	copy(out, i.chunks)
	return out
}

func (i *InMemoryIngestor) ChunkIDs(service string) []string {
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

func (i *InMemoryIngestor) SpecMeta(service string) (SpecMeta, bool) {
	key := canonicalServiceKey(service)
	if key == "" {
		return SpecMeta{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	meta, ok := i.specs[key]
	if !ok {
		return SpecMeta{}, false
	}
	return cloneSpecMeta(meta), true
}

func (i *InMemoryIngestor) upsertEndpointsLocked(endpoints []Endpoint) {
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

func (i *InMemoryIngestor) rebuildChunksLocked() {
	chunks := make([]Chunk, 0, len(i.endpoints)*4)
	for _, ep := range i.endpoints {
		chunks = append(chunks, buildChunksForEndpoint(ep, i.version)...)
	}
	i.chunks = chunks
}

// IMPORTANT: Chunk ID 生成规则不可变更。
// 格式：{service}:{method}:{path}:{type}
// 如需变更，必须提供数据迁移方案。
func buildChunksForEndpoint(ep Endpoint, version string) []Chunk {
	base := fmt.Sprintf("%s:%s:%s", ep.Service, ep.Method, ep.Path)
	endpointName := ep.DisplayName()
	overview := Chunk{
		ID:       base + ":overview",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     "overview",
		Content:  fmt.Sprintf("%s - %s", endpointName, strings.TrimSpace(ep.Summary)),
		Version:  version,
	}

	requestParts := make([]string, 0, len(ep.Parameters))
	for _, p := range ep.Parameters {
		required := "optional"
		if p.Required {
			required = "required"
		}
		typ := p.Type
		if typ == "" && p.SchemaRef != "" {
			typ = p.SchemaRef
		}
		requestParts = append(requestParts, fmt.Sprintf("%s:%s(%s)", p.Name, typ, required))
	}
	request := Chunk{
		ID:       base + ":request",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     "request",
		Content:  strings.Join(requestParts, ", "),
		Version:  version,
	}

	respParts := make([]string, 0, len(ep.Responses))
	for _, resp := range ep.Responses {
		respParts = append(respParts, fmt.Sprintf("%s %s", resp.StatusCode, strings.TrimSpace(resp.Description)))
	}
	response := Chunk{
		ID:       base + ":response",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     "response",
		Content:  strings.Join(respParts, "; "),
		Version:  version,
	}

	dependency := Chunk{
		ID:       base + ":dependency",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     "dependency",
		Content:  "接口依赖信息暂不可用",
		Version:  version,
	}

	return []Chunk{overview, request, response, dependency}
}

func normalizeSpecMeta(meta SpecMeta, endpoints []Endpoint) (SpecMeta, bool) {
	service := strings.TrimSpace(meta.Service)
	if service == "" && len(endpoints) > 0 {
		service = strings.TrimSpace(endpoints[0].Service)
	}
	if service == "" {
		return SpecMeta{}, false
	}
	meta.Service = service
	meta.Schemes = append([]string(nil), meta.Schemes...)
	return meta, true
}

func cloneSpecMeta(meta SpecMeta) SpecMeta {
	meta.Schemes = append([]string(nil), meta.Schemes...)
	return meta
}

func canonicalServiceKey(service string) string {
	return strings.ToLower(strings.TrimSpace(service))
}
