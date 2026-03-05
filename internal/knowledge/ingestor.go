package knowledge

import (
	"fmt"
	"strings"
	"sync"
)

type InMemoryIngestor struct {
	mu        sync.RWMutex
	endpoints []Endpoint
	chunks    []Chunk
	version   string
}

func NewInMemoryIngestor() *InMemoryIngestor {
	return &InMemoryIngestor{version: "v1.0.0"}
}

func (i *InMemoryIngestor) IngestFile(path string, service string) (IngestStats, error) {
	endpoints, err := ParseSwaggerFile(path, service)
	if err != nil {
		return IngestStats{}, err
	}
	return i.Upsert(endpoints), nil
}

func (i *InMemoryIngestor) Upsert(endpoints []Endpoint) IngestStats {
	i.mu.Lock()
	defer i.mu.Unlock()

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

	i.rebuildChunksLocked()
	return IngestStats{
		Endpoints: len(endpoints),
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

func (i *InMemoryIngestor) rebuildChunksLocked() {
	chunks := make([]Chunk, 0, len(i.endpoints)*4)
	for _, ep := range i.endpoints {
		chunks = append(chunks, buildChunksForEndpoint(ep, i.version)...)
	}
	i.chunks = chunks
}

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
		ID:        base + ":dependency",
		Service:   ep.Service,
		Endpoint:  endpointName,
		Type:      "dependency",
		Content:   buildDependencyHint(ep),
		Version:   version,
		DependsOn: inferDependencies(ep),
	}

	return []Chunk{overview, request, response, dependency}
}

func buildDependencyHint(ep Endpoint) string {
	deps := inferDependencies(ep)
	if len(deps) == 0 {
		return "no explicit dependency detected"
	}
	return "depends on: " + strings.Join(deps, ", ")
}

func inferDependencies(ep Endpoint) []string {
	path := strings.ToLower(ep.Path)
	switch {
	case strings.Contains(path, "order"):
		return []string{"GET /inventory", "POST /order"}
	case strings.Contains(path, "login"):
		return []string{"POST /user/login"}
	default:
		return nil
	}
}
