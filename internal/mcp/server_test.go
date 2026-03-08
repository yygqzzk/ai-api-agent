package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-agent-api/internal/config"
	"ai-agent-api/internal/tools"
)

func TestAuthMiddleware(t *testing.T) {
	srv := newTestServer(t, 10)
	rr := performRPC(t, srv.Handler(), "", rpcRequest{JSONRPC: "2.0", ID: 1, Method: "ping", Params: mustRawJSON(t, map[string]any{})})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDispatchSuccess(t *testing.T) {
	srv := newTestServer(t, 10)
	rr := performRPC(t, srv.Handler(), "Bearer test-token", rpcRequest{JSONRPC: "2.0", ID: 2, Method: "ping", Params: mustRawJSON(t, map[string]any{"msg": "ok"})})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp rpcResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected nil error, got %+v", resp.Error)
	}

	resultMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	if resultMap["pong"] != "ok" {
		t.Fatalf("expected pong=ok, got %+v", resultMap)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	srv := newTestServer(t, 1)
	handler := srv.Handler()

	r1 := performRPC(t, handler, "Bearer test-token", rpcRequest{JSONRPC: "2.0", ID: 1, Method: "ping", Params: mustRawJSON(t, map[string]any{"msg": "a"})})
	if r1.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", r1.Code)
	}

	r2 := performRPC(t, handler, "Bearer test-token", rpcRequest{JSONRPC: "2.0", ID: 2, Method: "ping", Params: mustRawJSON(t, map[string]any{"msg": "b"})})
	if r2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d", r2.Code)
	}
}

func TestLifecycleHooks(t *testing.T) {
	called := struct {
		onInit     bool
		onShutdown bool
	}{
		onInit:     false,
		onShutdown: false,
	}

	cfg, _ := config.LoadFromEnv()
	cfg.Server.AuthToken = "test-token"
	reg := tools.NewRegistry()
	if err := reg.Register(&pingTool{}); err != nil {
		t.Fatalf("register ping tool failed: %v", err)
	}

	hooks := Hooks{
		OnInit: func(context.Context) error {
			called.onInit = true
			return nil
		},
		OnShutdown: func(context.Context) error {
			called.onShutdown = true
			return nil
		},
	}

	srv := NewServer(cfg, reg, hooks, ServerOptions{RateLimitPerMinute: 10})
	if err := srv.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if !called.onInit {
		t.Fatalf("expected onInit called")
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
	if !called.onShutdown {
		t.Fatalf("expected onShutdown called")
	}
}

func newTestServer(t *testing.T, limit int) *Server {
	t.Helper()
	cfg, _ := config.LoadFromEnv()
	cfg.Server.AuthToken = "test-token"
	reg := tools.NewRegistry()
	if err := reg.Register(&pingTool{}); err != nil {
		t.Fatalf("register ping tool failed: %v", err)
	}
	return NewServer(cfg, reg, Hooks{}, ServerOptions{RateLimitPerMinute: limit})
}

func performRPC(t *testing.T, handler http.Handler, auth string, req rpcRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request failed: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if auth != "" {
		httpReq.Header.Set("Authorization", auth)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httpReq)
	return rr
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return b
}

type pingTool struct{}

func (p *pingTool) Name() string { return "ping" }
func (p *pingTool) Description() string {
	return "ping"
}
func (p *pingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (p *pingTool) Execute(_ context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Msg string `json:"msg"`
	}
	_ = json.Unmarshal(args, &req)
	return map[string]any{"pong": req.Msg}, nil
}
