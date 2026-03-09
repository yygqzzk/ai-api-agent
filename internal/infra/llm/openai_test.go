package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wanzhi/internal/domain/agent"
)

func TestOpenAICompatibleLLMClientTextReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "choices": [{"message": {"role": "assistant", "content": "ok-summary"}}]
}`))
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		APIKey:      "k1",
		BaseURL:     srv.URL,
		Model:       "gpt-4o-mini",
		MaxTokens:   256,
		Temperature: 0.1,
	})

	reply, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if reply.Content != "ok-summary" {
		t.Fatalf("expected content ok-summary, got %q", reply.Content)
	}
	if len(reply.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(reply.ToolCalls))
	}
}

func TestOpenAICompatibleLLMClientToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if req["model"] != "gpt-4o-mini" {
			t.Fatalf("unexpected model: %v", req["model"])
		}
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("expected one tool in request")
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
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		APIKey:      "k1",
		BaseURL:     srv.URL,
		Model:       "gpt-4o-mini",
		MaxTokens:   256,
		Temperature: 0.1,
	})

	reply, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "查登录接口"}}, []agent.ToolDefinition{{
		Name:        "search_api",
		Description: "search",
		Schema:      json.RawMessage(`{"type":"object"}`),
	}})
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if len(reply.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(reply.ToolCalls))
	}
	if reply.ToolCalls[0].Name != "search_api" {
		t.Fatalf("unexpected tool name: %s", reply.ToolCalls[0].Name)
	}
	if !strings.Contains(string(reply.ToolCalls[0].Args), `"query":"登录"`) {
		t.Fatalf("unexpected tool args: %s", string(reply.ToolCalls[0].Args))
	}
}

func TestOpenAICompatibleLLMClientHTTPErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{BaseURL: srv.URL, Model: "gpt-4o-mini"})
	_, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected http error")
	}
}

func TestOpenAICompatibleLLMClientRetryOn429ThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"retry-ok"}}]}`))
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		APIKey:       "k1",
		BaseURL:      srv.URL,
		Model:        "gpt-4o-mini",
		MaxRetries:   2,
		RetryBackoff: 5 * time.Millisecond,
	})
	reply, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected retry success, got err: %v", err)
	}
	if reply.Content != "retry-ok" {
		t.Fatalf("unexpected content: %q", reply.Content)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestOpenAICompatibleLLMClientRetryExhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "upstream broken", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		BaseURL:      srv.URL,
		Model:        "gpt-4o-mini",
		MaxRetries:   2,
		RetryBackoff: 5 * time.Millisecond,
	})
	_, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected retry exhausted error")
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 calls (1 + 2 retries), got %d", atomic.LoadInt32(&calls))
	}
}

func TestOpenAICompatibleLLMClientContextDeadlineNoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"late"}}]}`))
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		BaseURL:      srv.URL,
		Model:        "gpt-4o-mini",
		MaxRetries:   3,
		RetryBackoff: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.Next(ctx, []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected context deadline error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected no retry when context canceled, got calls=%d", atomic.LoadInt32(&calls))
	}
}

func TestOpenAICompatibleLLMClientHealthCheckAllowUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected health path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{BaseURL: srv.URL, APIKey: "k1"})
	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected unauthorized to be treated as healthy reachability, got err: %v", err)
	}
}

func TestOpenAICompatibleLLMClientHealthCheckFailOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server broken", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{BaseURL: srv.URL, APIKey: "k1"})
	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatalf("expected 5xx health check error")
	}
}

func TestOpenAICompatibleLLMClientUsageParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer srv.Close()

	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4o-mini",
	})
	reply, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if reply.PromptTokens != 10 {
		t.Fatalf("expected PromptTokens=10, got %d", reply.PromptTokens)
	}
	if reply.CompletionTokens != 5 {
		t.Fatalf("expected CompletionTokens=5, got %d", reply.CompletionTokens)
	}
}

func TestOpenAICompatibleLLMClientModel(t *testing.T) {
	client := NewOpenAICompatibleLLMClient(OpenAICompatibleLLMConfig{
		Model: "deepseek-chat",
	})
	if client.Model() != "deepseek-chat" {
		t.Fatalf("expected Model()=deepseek-chat, got %s", client.Model())
	}
}
