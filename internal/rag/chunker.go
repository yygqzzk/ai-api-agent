package rag

import (
	"fmt"
	"strings"

	"ai-agent-api/internal/knowledge"
)

func BuildChunks(endpoints []knowledge.Endpoint, version string) []knowledge.Chunk {
	out := make([]knowledge.Chunk, 0, len(endpoints)*4)
	for _, ep := range endpoints {
		out = append(out, buildChunksForEndpoint(ep, version)...)
	}
	return out
}

func buildChunksForEndpoint(ep knowledge.Endpoint, version string) []knowledge.Chunk {
	base := fmt.Sprintf("%s:%s:%s", ep.Service, ep.Method, ep.Path)
	endpointName := fmt.Sprintf("%s %s", ep.Method, ep.Path)

	overview := knowledge.Chunk{
		ID:       base + ":overview",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     "overview",
		Content:  strings.TrimSpace(ep.Summary),
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
	request := knowledge.Chunk{
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
	response := knowledge.Chunk{
		ID:       base + ":response",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     "response",
		Content:  strings.Join(respParts, "; "),
		Version:  version,
	}

	deps := inferDependencies(ep.Path)
	dependency := knowledge.Chunk{
		ID:        base + ":dependency",
		Service:   ep.Service,
		Endpoint:  endpointName,
		Type:      "dependency",
		Content:   strings.Join(deps, ", "),
		Version:   version,
		DependsOn: deps,
	}
	if dependency.Content == "" {
		dependency.Content = "no explicit dependency detected"
	}

	return []knowledge.Chunk{overview, request, response, dependency}
}

func inferDependencies(path string) []string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "order"):
		return []string{"GET /inventory", "POST /store/order"}
	case strings.Contains(lower, "login"):
		return []string{"GET /user/login"}
	default:
		return nil
	}
}
