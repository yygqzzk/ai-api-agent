package rag

import (
	"fmt"
	"strings"

	"wanzhi/internal/domain/model"
)

func BuildChunks(endpoints []model.Endpoint, version string) []model.Chunk {
	out := make([]model.Chunk, 0, len(endpoints)*4)
	for _, ep := range endpoints {
		out = append(out, buildChunksForEndpoint(ep, version)...)
	}
	return out
}

// IMPORTANT: Chunk ID 生成规则不可变更。
// 格式：{service}:{method}:{path}:{type}
// 如需部更，必须提供数据迁移方案。
func buildChunksForEndpoint(ep model.Endpoint, version string) []model.Chunk {
	base := fmt.Sprintf("%s:%s:%s", ep.Service, ep.Method, ep.Path)
	endpointName := ep.DisplayName()

	overview := model.Chunk{
		ID:       base + ":overview",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     model.ChunkTypeOverview,
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
	request := model.Chunk{
		ID:       base + ":request",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     model.ChunkTypeRequest,
		Content:  strings.Join(requestParts, ", "),
		Version:  version,
	}

	respParts := make([]string, 0, len(ep.Responses))
	for _, resp := range ep.Responses {
		respParts = append(respParts, fmt.Sprintf("%s %s", resp.StatusCode, strings.TrimSpace(resp.Description)))
	}
	response := model.Chunk{
		ID:       base + ":response",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     model.ChunkTypeResponse,
		Content:  strings.Join(respParts, "; "),
		Version:  version,
	}

	dependency := model.Chunk{
		ID:       base + ":dependency",
		Service:  ep.Service,
		Endpoint: endpointName,
		Type:     model.ChunkTypeDependency,
		Content:  "接口依赖信息暂不可用",
		Version:  version,
	}

	return []model.Chunk{overview, request, response, dependency}
}
