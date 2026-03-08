package rag

import (
	"fmt"
	"strings"

	"wanzhi/internal/knowledge"
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

	dependency := knowledge.Chunk{
		ID:       base + ":dependency",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     "dependency",
		Content:  "接口依赖信息暂不可用",
		Version:  version,
	}

	return []knowledge.Chunk{overview, request, response, dependency}
}
