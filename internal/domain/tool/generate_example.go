package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"wanzhi/internal/domain/model"
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
	return "为指定接口生成调用示例代码"
}

func (t *GenerateExampleTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["endpoint","language"],"properties":{"endpoint":{"type":"string"},"language":{"type":"string","enum":["go","python","java","javascript","curl"]}}}`)
}

func (t *GenerateExampleTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	_ = ctx
	var req GenerateExampleArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode generate_example args: %w", err)
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

	code, err := buildExampleCode(*ep, *specMeta, req.Language)
	if err != nil {
		return nil, fmt.Errorf("build example: %w", err)
	}

	return GenerateExampleResult{
		Endpoint: ep.DisplayName(),
		Language: req.Language,
		Code:     code,
	}, nil
}

func buildExampleCode(endpoint model.Endpoint, specMeta model.SpecMeta, language string) (string, error) {
	fullURL := specMeta.URLForPath(endpoint.Path)
	switch language {
	case "go":
		return fmt.Sprintf(`// %s %s
req, err := http.NewRequest("%s", "%s", nil)
if err != nil {
    log.Fatal(err)
}
resp, err := http.DefaultClient.Do(req)
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)
fmt.Println(string(body))`, endpoint.Method, endpoint.Path, endpoint.Method, fullURL), nil
	case "curl":
		return fmt.Sprintf(`curl -X %s "%s"`, endpoint.Method, fullURL), nil
	case "python":
		return fmt.Sprintf(`import requests

response = requests.%s("%s")
print(response.json())`, strings.ToLower(endpoint.Method), fullURL), nil
	case "javascript":
		return fmt.Sprintf(`fetch("%s", {method: "%s"})
  .then(r => r.json())
  .then(data => console.log(data))`, fullURL, strings.ToLower(endpoint.Method)), nil
	case "java":
		return fmt.Sprintf(`// Java example for %s %s
// TODO: implement`, endpoint.Method, endpoint.Path), nil
	default:
		return fmt.Sprintf("// %s examples not yet implemented", language), nil
	}
}
