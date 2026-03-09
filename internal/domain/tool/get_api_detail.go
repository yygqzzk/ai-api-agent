package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type GetAPIDetailTool struct {
	kb *KnowledgeBase
}

func NewGetAPIDetailTool(kb *KnowledgeBase) *GetAPIDetailTool {
	return &GetAPIDetailTool{kb: kb}
}

func (t *GetAPIDetailTool) Name() string {
	return "get_api_detail"
}

func (t *GetAPIDetailTool) Description() string {
	return "获取指定接口的详细信息"
}

func (t *GetAPIDetailTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["endpoint"],"properties":{"endpoint":{"type":"string"}}}`)
}

func (t *GetAPIDetailTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	_ = ctx
	var req APIDetailArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode get_api_detail args: %w", err)
	}

	parts := strings.SplitN(req.Endpoint, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid endpoint format: %s", req.Endpoint)
	}
	method, path := parts[0], parts[1]

	ep, err := t.kb.GetEndpoint(ctx, req.Service, method, path)
	if err != nil {
		return nil, fmt.Errorf("endpoint not found: %s", req.Endpoint)
	}

	specMeta, err := t.kb.GetSpecMeta(ctx, req.Service)
	if err != nil {
		return nil, fmt.Errorf("spec not found: %s", req.Service)
	}

	return APIDetailResult{
		Endpoint: APIDetail{
			Service:     ep.Service,
			Method:      ep.Method,
			Path:        ep.Path,
			Summary:     ep.Summary,
			Description: ep.Description,
			Tags:        ep.Tags,
			Parameters:  ep.Parameters,
			Responses:   ep.Responses,
			Spec:        *specMeta,
		},
	}, nil
}
