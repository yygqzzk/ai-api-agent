package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ValidateParamsTool struct {
	kb *KnowledgeBase
}

func NewValidateParamsTool(kb *KnowledgeBase) *ValidateParamsTool {
	return &ValidateParamsTool{kb: kb}
}

func (t *ValidateParamsTool) Name() string {
	return "validate_params"
}

func (t *ValidateParamsTool) Description() string {
	return "校验接口请求参数是否完整且合法"
}

func (t *ValidateParamsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["endpoint","params"],"properties":{"service":{"type":"string"},"endpoint":{"type":"string"},"params":{"type":"object"}}}`)
}

func (t *ValidateParamsTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	_ = ctx
	var req ValidateParamsArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode validate_params args: %w", err)
	}
	if req.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
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

	missingRequired := make([]string, 0)
	known := make(map[string]struct{}, len(ep.Parameters))
	for _, p := range ep.Parameters {
		known[p.Name] = struct{}{}
		if !p.Required {
			continue
		}
		v, exists := req.Params[p.Name]
		if !exists || isEmptyParam(v) {
			missingRequired = append(missingRequired, p.Name)
		}
	}

	unknown := make([]string, 0)
	for key := range req.Params {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(missingRequired)
	sort.Strings(unknown)
	return ValidateParamsResult{
		Valid:           len(missingRequired) == 0,
		MissingRequired: missingRequired,
		UnknownParams:   unknown,
	}, nil
}

func isEmptyParam(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	default:
		return false
	}
}
