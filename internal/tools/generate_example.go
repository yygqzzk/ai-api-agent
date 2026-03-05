package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-agent-api/internal/knowledge"
)

type GenerateExampleTool struct {
	kb *KnowledgeBase
}

func NewGenerateExampleTool(kb *KnowledgeBase) *GenerateExampleTool {
	return &GenerateExampleTool{kb: kb}
}

func (t *GenerateExampleTool) Name() string {
	return "generate_example"
}

func (t *GenerateExampleTool) Description() string {
	return "生成 API 调用示例代码"
}

func (t *GenerateExampleTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["endpoint"],"properties":{"service":{"type":"string"},"endpoint":{"type":"string"},"language":{"type":"string"}}}`)
}

func (t *GenerateExampleTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	_ = ctx
	var req GenerateExampleArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode generate_example args: %w", err)
	}
	if req.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	ep, ok := t.kb.GetEndpoint(req.Service, req.Endpoint)
	if !ok {
		return nil, fmt.Errorf("endpoint not found: %s", req.Endpoint)
	}
	lang := strings.ToLower(strings.TrimSpace(req.Language))
	if lang == "" {
		lang = "go"
	}

	code := buildExampleCode(lang, ep)
	return GenerateExampleResult{
		Language: lang,
		Code:     code,
	}, nil
}

func buildExampleCode(language string, ep knowledge.Endpoint) string {
	switch language {
	case "go":
		return fmt.Sprintf(`client := &http.Client{}
req, _ := http.NewRequest("%s", "https://petstore.swagger.io/v2%s", nil)
resp, err := client.Do(req)
if err != nil {
    panic(err)
}
defer resp.Body.Close()
`, ep.Method, ep.Path)
	case "python":
		return fmt.Sprintf(`import requests

resp = requests.request("%s", "https://petstore.swagger.io/v2%s")
print(resp.status_code)
print(resp.text)
`, ep.Method, ep.Path)
	default:
		return fmt.Sprintf(`curl -X %s "https://petstore.swagger.io/v2%s"`, ep.Method, ep.Path)
	}
}
