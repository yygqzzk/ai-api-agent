package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAgentEngineWithOpenAIClientAndToolCalls(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode openai request failed: %v", err)
		}

		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			if _, ok := req["tools"].([]any); !ok {
				t.Fatalf("expected tools in first request")
			}
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
          "arguments": "{\"query\":\"登录\",\"top_k\":3}"
        }
      }]
    }
  }]
}`))
			return
		}

		messages, ok := req["messages"].([]any)
		if !ok {
			t.Fatalf("expected messages in second request")
		}
		foundToolResult := false
		for _, m := range messages {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if mm["role"] == "tool" && strings.Contains(toString(mm["content"]), "GET /user/login") {
				foundToolResult = true
				break
			}
		}
		if !foundToolResult {
			t.Fatalf("expected tool result message in second request")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"最终汇总完成"}}]}`))
	}))
	defer srv.Close()

	llm := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		APIKey:      "k1",
		BaseURL:     srv.URL,
		Model:       "gpt-4o-mini",
		MaxTokens:   512,
		Temperature: 0.1,
	})

	dispatcher := &mockToolDispatcher{results: map[string]any{
		"search_api": map[string]any{"items": []map[string]any{{"endpoint": "GET /user/login"}}},
	}}

	engine := NewAgentEngine(llm, dispatcher, WithMaxSteps(4))
	engine.SetToolCatalog([]ToolDefinition{{
		Name:        "search_api",
		Description: "search api",
		Schema:      json.RawMessage(`{"type":"object"}`),
	}})

	out, err := engine.Run(context.Background(), "查询登录接口")
	if err != nil {
		t.Fatalf("run agent failed: %v", err)
	}
	if !strings.Contains(out, "最终汇总完成") {
		t.Fatalf("unexpected final output: %s", out)
	}
	if !strings.Contains(out, "工具调用轨迹") {
		t.Fatalf("expected tool trace summary, got: %s", out)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "search_api" {
		t.Fatalf("unexpected dispatcher calls: %+v", dispatcher.calls)
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
