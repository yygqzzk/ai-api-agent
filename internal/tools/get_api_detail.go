package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"wanzhi/internal/knowledge"
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
	return "查询单个接口的完整 schema 详情"
}

func (t *GetAPIDetailTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["endpoint"],"properties":{"service":{"type":"string"},"endpoint":{"type":"string"}}}`)
}

func (t *GetAPIDetailTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	_ = ctx
	var req APIDetailArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode get_api_detail args: %w", err)
	}
	if req.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	ep, ok := t.kb.GetEndpoint(req.Service, req.Endpoint)
	if !ok {
		return nil, fmt.Errorf("endpoint not found: %s", req.Endpoint)
	}
	spec, _ := t.kb.GetSpecMeta(ep.Service)

	return APIDetailResult{
		Endpoint: APIDetail{
			Service:     ep.Service,
			Method:      ep.Method,
			Path:        ep.Path,
			Summary:     ep.Summary,
			Description: ep.Description,
			Tags:        append([]string(nil), ep.Tags...),
			Parameters:  append([]knowledge.Parameter(nil), ep.Parameters...),
			Responses:   append([]knowledge.Response(nil), ep.Responses...),
			Spec:        spec,
		},
	}, nil
}
