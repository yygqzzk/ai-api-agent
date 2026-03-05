package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type AnalyzeDependenciesTool struct {
	kb *KnowledgeBase
}

func NewAnalyzeDependenciesTool(kb *KnowledgeBase) *AnalyzeDependenciesTool {
	return &AnalyzeDependenciesTool{kb: kb}
}

func (t *AnalyzeDependenciesTool) Name() string {
	return "analyze_dependencies"
}

func (t *AnalyzeDependenciesTool) Description() string {
	return "分析指定接口的上下游依赖关系"
}

func (t *AnalyzeDependenciesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["endpoint"],"properties":{"service":{"type":"string"},"endpoint":{"type":"string"}}}`)
}

func (t *AnalyzeDependenciesTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	_ = ctx
	var req AnalyzeDependenciesArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode analyze_dependencies args: %w", err)
	}
	if req.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	ep, ok := t.kb.GetEndpoint(req.Service, req.Endpoint)
	if !ok {
		return nil, fmt.Errorf("endpoint not found: %s", req.Endpoint)
	}

	deps := inferEndpointDependencies(ep.Path)
	return AnalyzeDependenciesResult{
		Service:      ep.Service,
		Endpoint:     ep.DisplayName(),
		Dependencies: deps,
	}, nil
}

func inferEndpointDependencies(path string) []string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "order"):
		return []string{"GET /store/inventory", "POST /store/order", "POST /pet/{petId}"}
	case strings.Contains(lower, "login"):
		return []string{"GET /user/login", "GET /user/logout"}
	case strings.Contains(lower, "pet"):
		return []string{"POST /pet", "GET /pet/{petId}"}
	default:
		return []string{"no dependency graph available"}
	}
}
