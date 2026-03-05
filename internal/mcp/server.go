package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"ai-agent-api/internal/agent"
	"ai-agent-api/internal/config"
	"ai-agent-api/internal/observability"
	"ai-agent-api/internal/tools"
)

type ServerOptions struct {
	RateLimitPerMinute int
	Metrics            *observability.Metrics
	Logger             *slog.Logger
}

type Server struct {
	cfg          config.Config
	registry     *tools.Registry
	hooks        Hooks
	options      ServerOptions
	limiter      RateLimiter
	slog         *slog.Logger
	metrics      *observability.Metrics
	streamRunner StreamRunner
}

type StreamRunner interface {
	RunStream(ctx context.Context, userQuery string) <-chan agent.AgentEvent
}

func NewServer(cfg config.Config, registry *tools.Registry, hooks Hooks, options ServerOptions) *Server {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// 创建限流器（默认使用固定窗口算法）
	rateLimitCfg := DefaultConfig()
	if options.RateLimitPerMinute > 0 {
		rateLimitCfg.Limit = options.RateLimitPerMinute
	}

	return &Server{
		cfg:      cfg,
		registry: registry,
		hooks:    hooks,
		options:  options,
		limiter:  NewRateLimiter(rateLimitCfg),
		slog:     logger,
		metrics:  options.Metrics,
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

func (s *Server) Init(ctx context.Context) error {
	if s.hooks.OnInit != nil {
		return s.hooks.OnInit(ctx)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.hooks.OnShutdown != nil {
		return s.hooks.OnShutdown(ctx)
	}
	return nil
}

func (s *Server) SetStreamRunner(runner StreamRunner) {
	s.streamRunner = runner
}

func (s *Server) Handler() http.Handler {
	// 中间件链顺序：从外到内依次执行
	// 1. RequestID 必须最先，为后续所有中间件提供追踪能力
	// 2. Auth 验证身份，拒绝未授权请求
	// 3. RateLimit 限流，防止滥用
	// 4. Validation 校验请求格式
	// 5. Logging 记录请求和响应（最内层，可以看到最完整的处理结果）
	var handler http.Handler = http.HandlerFunc(s.handleRPC)
	handler = s.validationMiddleware(handler)
	handler = s.loggingMiddleware(handler)
	handler = s.rateLimitMiddleware(handler)
	handler = s.authMiddleware(handler)
	handler = s.requestIDMiddleware(handler)
	return handler
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if isSSERequest(r) && s.streamRunner != nil {
		s.handleSSE(w, r)
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	s.handleRPCWithRequest(w, r, req)
}

func (s *Server) handleRPCWithRequest(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	if req.Method == "" {
		writeRPCResponse(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32600,
				Message: "method is required",
			},
		})
		return
	}

	start := time.Now()
	if s.hooks.BeforeToolCall != nil {
		s.hooks.BeforeToolCall(r.Context(), req.Method)
	}

	result, err := s.registry.Dispatch(r.Context(), req.Method, req.Params)
	duration := time.Since(start)
	if s.hooks.AfterToolCall != nil {
		s.hooks.AfterToolCall(r.Context(), req.Method, duration, err)
	}
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.metrics.RecordRequest(req.Method, status, duration.Seconds())
	if err != nil {
		writeRPCResponse(w, http.StatusOK, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32000,
				Message: err.Error(),
			},
		})
		return
	}

	writeRPCResponse(w, http.StatusOK, rpcResponse{
		JSONRPC: req.JSONRPC,
		ID:      req.ID,
		Result:  result,
	})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	if req.Method != "query_api" || s.streamRunner == nil {
		s.handleRPCWithRequest(w, r, req)
		return
	}

	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Query == "" {
		writeRPCResponse(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: "query is required",
			},
		})
		return
	}

	setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch := s.streamRunner.RunStream(r.Context(), params.Query)
	for ev := range ch {
		writeSSEEvent(w, ev)
	}
}

func writeRPCResponse(w http.ResponseWriter, status int, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("encode response failed: %v", err))
	}
}
