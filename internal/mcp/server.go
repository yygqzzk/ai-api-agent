// Package mcp 提供 `/mcp` JSON-RPC 服务及其 HTTP 中间件。
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

	"github.com/gin-gonic/gin"
)

// ServerOptions MCP Server 配置选项
type ServerOptions struct {
	RateLimitPerMinute int                    // 每分钟请求限制
	Metrics            *observability.Metrics // Prometheus 指标收集器
	Logger             *slog.Logger           // 结构化日志记录器
}

// Server 负责处理 JSON-RPC 请求、SSE 和中间件链。
type Server struct {
	cfg          config.Config          // 配置对象
	registry     *tools.Registry        // 工具注册表
	hooks        Hooks                  // 生命周期钩子
	options      ServerOptions          // 服务器选项
	limiter      RateLimiter            // 限流器
	slog         *slog.Logger           // 日志记录器
	metrics      *observability.Metrics // 指标收集器
	streamRunner StreamRunner           // 流式执行器 (Agent 引擎)
}

// StreamRunner 为 `query_api` 提供流式事件输出。
type StreamRunner interface {
	RunStream(ctx context.Context, userQuery string) <-chan agent.AgentEvent
}

// NewServer 创建带限流、日志和指标能力的 MCP Server。
func NewServer(cfg config.Config, registry *tools.Registry, hooks Hooks, options ServerOptions) *Server {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// 创建限流器 (默认使用固定窗口算法)
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

// rpcRequest JSON-RPC 2.0 请求
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"` // 协议版本 (固定为 "2.0")
	ID      any             `json:"id"`      // 请求 ID (可以是字符串或数字)
	Method  string          `json:"method"`  // 方法名 (工具名称)
	Params  json.RawMessage `json:"params"`  // 参数 (JSON 对象)
}

// rpcError JSON-RPC 2.0 错误
type rpcError struct {
	Code    int    `json:"code"`    // 错误码 (遵循 JSON-RPC 2.0 规范)
	Message string `json:"message"` // 错误消息
}

// rpcResponse JSON-RPC 2.0 响应
type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`          // 协议版本
	ID      any       `json:"id"`               // 请求 ID
	Result  any       `json:"result,omitempty"` // 成功结果
	Error   *rpcError `json:"error,omitempty"`  // 错误信息
}

// Init 调用可选的 OnInit hook。
func (s *Server) Init(ctx context.Context) error {
	if s.hooks.OnInit != nil {
		return s.hooks.OnInit(ctx)
	}
	return nil
}

// Shutdown 调用可选的 OnShutdown hook。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.hooks.OnShutdown != nil {
		return s.hooks.OnShutdown(ctx)
	}
	return nil
}

// SetStreamRunner 注册 SSE 所需的流式执行器。
func (s *Server) SetStreamRunner(runner StreamRunner) {
	s.streamRunner = runner
}

// Limiter 返回限流器（供 Gin 中间件使用）
func (s *Server) Limiter() RateLimiter {
	return s.limiter
}

// HandleRPC 是 Gin 框架的 RPC 处理器
func (s *Server) HandleRPC(c *gin.Context) {
	// 检查是否是 SSE 请求
	if isSSERequest(c.Request) && s.streamRunner != nil {
		s.handleSSE(c.Writer, c.Request)
		return
	}

	// 解析 JSON-RPC 请求
	var req rpcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	// 设置默认协议版本
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	// 验证 method 字段
	if req.Method == "" {
		c.JSON(http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32600,
				Message: "method is required",
			},
		})
		return
	}

	// 执行工具调用
	start := time.Now()
	if s.hooks.BeforeToolCall != nil {
		s.hooks.BeforeToolCall(c.Request.Context(), req.Method)
	}

	result, err := s.registry.Dispatch(c.Request.Context(), req.Method, req.Params)
	duration := time.Since(start)

	if s.hooks.AfterToolCall != nil {
		s.hooks.AfterToolCall(c.Request.Context(), req.Method, duration, err)
	}

	// 记录指标
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.metrics.RecordRequest(req.Method, status, duration.Seconds())

	// 返回响应
	if err != nil {
		c.JSON(http.StatusOK, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32000,
				Message: err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, rpcResponse{
		JSONRPC: req.JSONRPC,
		ID:      req.ID,
		Result:  result,
	})
}

// Handler 返回 `/mcp` 的 HTTP 处理器。
func (s *Server) Handler() http.Handler {
	// http.Handler 是标准库的 HTTP 处理接口；
	// http.HandlerFunc 则把普通函数适配成实现了该接口的对象。
	var handler http.Handler = http.HandlerFunc(s.handleRPC)
	handler = s.validationMiddleware(handler)
	handler = s.loggingMiddleware(handler)
	handler = s.rateLimitMiddleware(handler)
	handler = s.authMiddleware(handler)
	handler = s.requestIDMiddleware(handler)
	return handler
}

// handleRPC 在普通 JSON-RPC 与 SSE 之间分派请求。
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	// 检查是否是 SSE 请求
	if isSSERequest(r) && s.streamRunner != nil {
		s.handleSSE(w, r)
		return
	}

	// 解析 JSON-RPC 请求
	var req rpcRequest
	// json.NewDecoder 会直接从 io.Reader（这里是请求体）流式读取 JSON，
	// 不需要先把整个 body 读进 []byte 再反序列化。
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	s.handleRPCWithRequest(w, r, req)
}

// handleRPCWithRequest 执行工具调用并写回 JSON-RPC 响应。
func (s *Server) handleRPCWithRequest(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	// 设置默认协议版本
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	// 验证 method 字段
	if req.Method == "" {
		writeRPCResponse(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32600, // Invalid Request
				Message: "method is required",
			},
		})
		return
	}

	// 执行工具调用
	// time.Now 记录起点时间，后面的 time.Since(start) 就能得到耗时。
	start := time.Now()
	if s.hooks.BeforeToolCall != nil {
		s.hooks.BeforeToolCall(r.Context(), req.Method)
	}

	result, err := s.registry.Dispatch(r.Context(), req.Method, req.Params)
	duration := time.Since(start)

	if s.hooks.AfterToolCall != nil {
		s.hooks.AfterToolCall(r.Context(), req.Method, duration, err)
	}

	// 记录指标
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.metrics.RecordRequest(req.Method, status, duration.Seconds())

	// 返回响应
	if err != nil {
		writeRPCResponse(w, http.StatusOK, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32000, // Server error
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

// handleSSE 为 `query_api` 输出事件流。
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	// 只支持 query_api 工具的流式响应
	if req.Method != "query_api" || s.streamRunner == nil {
		s.handleRPCWithRequest(w, r, req)
		return
	}

	// 解析参数
	var params struct {
		Query string `json:"query"`
	}
	// req.Params 是 json.RawMessage，本质上就是“还没解析的原始 JSON 片段”，
	// 这里再用 json.Unmarshal 只把它解析到当前需要的结构体里。
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Query == "" {
		writeRPCResponse(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602, // Invalid params
				Message: "query is required",
			},
		})
		return
	}

	// 设置 SSE 响应头
	setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	// 这里用类型断言检查 ResponseWriter 是否实现了 http.Flusher。
	// Flush 会立刻把缓冲区内容推给客户端，SSE 场景下很常见。
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// 推送事件流
	ch := s.streamRunner.RunStream(r.Context(), params.Query)
	for ev := range ch {
		writeSSEEvent(w, ev)
	}
}

// writeRPCResponse 写入 JSON-RPC 响应
func writeRPCResponse(w http.ResponseWriter, status int, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// json.NewEncoder 会直接把 JSON 写到 ResponseWriter，
	// 比先 Marshal 成 []byte 再 Write 更省一步中间拷贝。
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("encode response failed: %v", err))
	}
}
