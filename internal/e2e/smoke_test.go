package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"wanzhi/internal/domain/agent"
	"wanzhi/internal/config"
	"wanzhi/internal/domain/knowledge"
	"wanzhi/internal/transport"
	"wanzhi/internal/domain/rag"
	llm2 "wanzhi/internal/infra/llm"
	"wanzhi/internal/domain/tool"
)

func TestQueryAPISmoke(t *testing.T) {
	cfg, _ := config.LoadFromEnv()
	cfg.Server.AuthToken = "test-token"

	kb := tool.NewKnowledgeBaseWithStores(knowledge.NewMemoryIngestor(), rag.NewMemoryStore())
	petstorePath := filepath.Join("..", "..", "testdata", "petstore.json")
	if _, _, err := kb.IngestFileDocument(context.Background(), petstorePath, "petstore"); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	registry := tool.NewRegistry()
	skillDir := filepath.Join("..", "..", "skills")
	if err := tool.RegisterDefaultTools(registry, kb, skillDir); err != nil {
		t.Fatalf("register default tools failed: %v", err)
	}

	engine := agent.NewAgentEngine(llm2.NewRuleBasedLLMClient(), registry, agent.WithMaxSteps(10))
	if err := tool.RegisterQueryTool(registry, engine); err != nil {
		t.Fatalf("register query tool failed: %v", err)
	}

	srv := transport.NewServer(cfg, registry, transport.Hooks{}, transport.ServerOptions{RateLimitPerMinute: 10})
	if err := srv.Init(context.Background()); err != nil {
		t.Fatalf("server init failed: %v", err)
	}
	defer func() { _ = srv.Shutdown(context.Background()) }()

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "query_api",
		"params": map[string]any{
			"query": "查询用户登录接口参数和go示例",
		},
	}
	body, _ := json.Marshal(reqBody)
	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer test-token")
	httpReq.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}

	summary, _ := resp.Result["summary"].(string)
	if !strings.Contains(summary, "结构化汇总结果") {
		t.Fatalf("expected structured summary, got: %s", summary)
	}
}
