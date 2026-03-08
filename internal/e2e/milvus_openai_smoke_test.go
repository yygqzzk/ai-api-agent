package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"ai-agent-api/internal/agent"
	"ai-agent-api/internal/config"
	"ai-agent-api/internal/knowledge"
	"ai-agent-api/internal/mcp"
	"ai-agent-api/internal/rag"
	"ai-agent-api/internal/store"
	"ai-agent-api/internal/tools"
)

func TestQueryAPIMilvusStoreWithOpenAI(t *testing.T) {
	cfg, _ := config.LoadFromEnv()
	cfg.Server.AuthToken = "test-token"

	milvus := store.NewInMemoryMilvusClient()
	embedder := &keywordEmbedder{dim: 8}
	ragStore := rag.NewMilvusStore(milvus, embedder, "api_documents")
	kb := tools.NewKnowledgeBaseWithIngestor(knowledge.NewInMemoryIngestor(), ragStore)
	petstorePath := filepath.Join("..", "..", "testdata", "petstore.json")
	if _, err := kb.IngestFile(context.Background(), petstorePath, "petstore"); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	registry := tools.NewRegistry()
	skillDir := filepath.Join("..", "..", "skills")
	if err := tools.RegisterDefaultTools(registry, kb, skillDir); err != nil {
		t.Fatalf("register default tools failed: %v", err)
	}

	var llmCalls int32
	openaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode openai request failed: %v", err)
		}
		call := atomic.AddInt32(&llmCalls, 1)
		if call == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
  "choices": [{
    "message": {
      "role": "assistant",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {
          "name": "search_api",
          "arguments": "{\"query\":\"登录\",\"top_k\":3,\"service\":\"petstore\"}"
        }
      }]
    }
  }]
}`))
			return
		}

		messages, _ := req["messages"].([]any)
		foundToolResult := false
		for _, raw := range messages {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if m["role"] == "tool" && strings.Contains(asString(m["content"]), "user/login") {
				foundToolResult = true
				break
			}
		}
		if !foundToolResult {
			t.Fatalf("expected second llm request contains tool result")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Milvus链路查询完成"}}]}`))
	}))
	defer openaiSrv.Close()

	llmClient := agent.NewOpenAICompatibleLLMClient(agent.OpenAICompatibleLLMConfig{
		APIKey:      "k1",
		BaseURL:     openaiSrv.URL,
		Model:       "gpt-4o-mini",
		MaxTokens:   512,
		Temperature: 0.1,
	})

	engine := agent.NewAgentEngine(llmClient, registry, agent.WithMaxSteps(8))
	engine.SetToolCatalog(toAgentDefs(registry.ToolDefinitions()))
	if err := tools.RegisterQueryTool(registry, engine); err != nil {
		t.Fatalf("register query tool failed: %v", err)
	}

	srv := mcp.NewServer(cfg, registry, mcp.Hooks{}, mcp.ServerOptions{RateLimitPerMinute: 10})
	if err := srv.Init(context.Background()); err != nil {
		t.Fatalf("server init failed: %v", err)
	}
	defer func() { _ = srv.Shutdown(context.Background()) }()

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "query_api",
		"params": map[string]any{
			"query": "查询登录接口",
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
		Result struct {
			Summary string `json:"summary"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
	if !strings.Contains(resp.Result.Summary, "Milvus链路查询完成") {
		t.Fatalf("expected final llm summary, got: %s", resp.Result.Summary)
	}
	if !strings.Contains(resp.Result.Summary, "工具调用轨迹") {
		t.Fatalf("expected tool trace summary, got: %s", resp.Result.Summary)
	}
	if atomic.LoadInt32(&llmCalls) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", atomic.LoadInt32(&llmCalls))
	}
}

type keywordEmbedder struct {
	dim int
}

func (e *keywordEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		v := make([]float32, e.dim)
		lower := strings.ToLower(text)
		if strings.Contains(lower, "登录") || strings.Contains(lower, "login") {
			v[0] = 1
		}
		if strings.Contains(lower, "user") {
			v[1] = 1
		}
		if strings.Contains(lower, "order") {
			v[2] = 1
		}
		vectors[i] = v
	}
	return vectors, nil
}

func (e *keywordEmbedder) Dimension() int {
	return e.dim
}

func toAgentDefs(defs []tools.ToolDefinition) []agent.ToolDefinition {
	out := make([]agent.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, agent.ToolDefinition{
			Name:        d.Name,
			Description: d.Description,
			Schema:      d.Schema,
		})
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
