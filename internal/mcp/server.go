// Package mcp 实现自定义 MCP (Model Context Protocol) JSON-RPC 2.0 服务器
//
// # 协议设计
//
// MCP 是一个基于 JSON-RPC 2.0 的协议,用于 AI Agent 和工具之间的通信:
//
// 请求格式:
//
//	{
//	  "jsonrpc": "2.0",
//	  "id": 1,
//	  "method": "search_api",
//	  "params": {"query": "用户登录"}
//	}
//
// 响应格式:
//
//	{
//	  "jsonrpc": "2.0",
//	  "id": 1,
//	  "result": {...}
//	}
//
// 错误响应:
//
//	{
//	  "jsonrpc": "2.0",
//	  "id": 1,
//	  "error": {"code": -32000, "message": "..."}
//	}
//
// # 中间件链设计
//
// 使用 Middleware Pattern 实现横切关注点:
//
// 执行顺序 (从外到内):
// 1. **RequestID** - 生成追踪 ID,为后续中间件提供追踪能力
// 2. **Auth** - 验证 Bearer Token,拒绝未授权请求
// 3. **RateLimit** - 限流,防止滥用
// 4. **Validation** - 校验请求格式 (Content-Type, Method)
// 5. **Logging** - 记录请求和响应 (最内层,可以看到完整的处理结果)
//
// 中间件链的优势:
// - 关注点分离: 每个中间件只负责一个功能
// - 可组合: 可以灵活添加/删除中间件
// - 可测试: 每个中间件可以独立测试
//
// # SSE 流式响应
//
// 支持 Server-Sent Events (SSE) 流式响应:
// - 客户端发送 Accept: text/event-stream
// - 服务器返回 Content-Type: text/event-stream
// - 实时推送 Agent 执行事件 (StepStart, ToolStart, ToolEnd, Complete)
//
// SSE 事件格式:
//
//	event: step_start
//	data: {"step": 1}
//
//	event: tool_start
//	data: {"step": 1, "tool": "search_api"}
//
//	event: complete
//	data: {"summary": "..."}
//
// # 并发安全性
//
// - Server 本身是并发安全的,可以处理多个并发请求
// - RateLimiter 使用锁保护内部状态
// - Registry 使用 RWMutex 保护工具映射表
//
// # 参考文献
//
// JSON-RPC 2.0 规范: https://www.jsonrpc.org/specification
// Server-Sent Events: https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events
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

// ServerOptions MCP Server 配置选项
type ServerOptions struct {
	RateLimitPerMinute int                      // 每分钟请求限制
	Metrics            *observability.Metrics   // Prometheus 指标收集器
	Logger             *slog.Logger             // 结构化日志记录器
}

// Server MCP JSON-RPC 2.0 服务器
//
// 职责:
// 1. 处理 JSON-RPC 2.0 请求
// 2. 调度工具执行
// 3. 支持 SSE 流式响应
// 4. 执行中间件链 (认证、限流、日志等)
//
// 并发安全性:
// - Server 本身是并发安全的
// - 可以安全地处理多个并发请求
// - 内部依赖 (Registry, RateLimiter) 都是并发安全的
type Server struct {
	cfg          config.Config              // 配置对象
	registry     *tools.Registry            // 工具注册表
	hooks        Hooks                      // 生命周期钩子
	options      ServerOptions              // 服务器选项
	limiter      RateLimiter                // 限流器
	slog         *slog.Logger               // 日志记录器
	metrics      *observability.Metrics     // 指标收集器
	streamRunner StreamRunner               // 流式执行器 (Agent 引擎)
}

// StreamRunner 流式执行器接口
// 用于支持 SSE 流式响应
//
// 设计考虑:
// - 使用接口解耦,避免 Server 直接依赖 Agent
// - 支持不同的流式执行实现
type StreamRunner interface {
	RunStream(ctx context.Context, userQuery string) <-chan agent.AgentEvent
}

// NewServer 创建 MCP Server
//
// 参数:
// - cfg: 配置对象
// - registry: 工具注册表
// - hooks: 生命周期钩子
// - options: 服务器选项
//
// 默认配置:
// - 限流器: 固定窗口算法
// - 日志: slog.Default()
//
// 使用示例:
//
//	server := mcp.NewServer(cfg, registry, hooks, mcp.ServerOptions{
//	    RateLimitPerMinute: 120,
//	    Metrics:            metrics,
//	    Logger:             logger,
//	})
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
	JSONRPC string    `json:"jsonrpc"`           // 协议版本
	ID      any       `json:"id"`                // 请求 ID
	Result  any       `json:"result,omitempty"`  // 成功结果
	Error   *rpcError `json:"error,omitempty"`   // 错误信息
}

// Init 初始化 MCP Server
//
// 执行 OnInit 钩子,用于:
// - 预加载数据
// - 初始化连接池
// - 验证配置
func (s *Server) Init(ctx context.Context) error {
	if s.hooks.OnInit != nil {
		return s.hooks.OnInit(ctx)
	}
	return nil
}

// Shutdown 关闭 MCP Server
//
// 执行 OnShutdown 钩子,用于:
// - 关闭连接
// - 清理资源
// - 保存状态
func (s *Server) Shutdown(ctx context.Context) error {
	if s.hooks.OnShutdown != nil {
		return s.hooks.OnShutdown(ctx)
	}
	return nil
}

// SetStreamRunner 设置流式执行器
//
// 用于支持 SSE 流式响应
// 通常传入 Agent 引擎实例
func (s *Server) SetStreamRunner(runner StreamRunner) {
	s.streamRunner = runner
}

// Handler 返回 HTTP 处理器
//
// 中间件链顺序 (从外到内):
// 1. RequestID - 生成追踪 ID
// 2. Auth - 验证身份
// 3. RateLimit - 限流
// 4. Validation - 校验请求
// 5. Logging - 记录日志
// 6. handleRPC - 处理 RPC 请求
//
// 设计考虑:
// - RequestID 必须最先,为后续中间件提供追踪能力
// - Auth 在 RateLimit 之前,避免未授权请求消耗限流配额
// - Logging 在最内层,可以记录完整的处理结果
func (s *Server) Handler() http.Handler {
	// 中间件链顺序:从外到内依次执行
	// 1. RequestID 必须最先,为后续所有中间件提供追踪能力
	// 2. Auth 验证身份,拒绝未授权请求
	// 3. RateLimit 限流,防止滥用
	// 4. Validation 校验请求格式
	// 5. Logging 记录请求和响应 (最内层,可以看到最完整的处理结果)
	var handler http.Handler = http.HandlerFunc(s.handleRPC)
	handler = s.validationMiddleware(handler)
	handler = s.loggingMiddleware(handler)
	handler = s.rateLimitMiddleware(handler)
	handler = s.authMiddleware(handler)
	handler = s.requestIDMiddleware(handler)
	return handler
}

// handleRPC 处理 RPC 请求
//
// 支持两种响应模式:
// 1. 标准 JSON-RPC 2.0 响应
// 2. SSE 流式响应 (如果客户端发送 Accept: text/event-stream)
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	// 检查是否是 SSE 请求
	if isSSERequest(r) && s.streamRunner != nil {
		s.handleSSE(w, r)
		return
	}

	// 解析 JSON-RPC 请求
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	s.handleRPCWithRequest(w, r, req)
}

// handleRPCWithRequest 处理已解析的 RPC 请求
//
// 执行流程:
// 1. 验证请求格式 (jsonrpc, method)
// 2. 执行 BeforeToolCall 钩子
// 3. 调度工具执行
// 4. 执行 AfterToolCall 钩子
// 5. 记录指标
// 6. 返回响应
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

// handleSSE 处理 SSE 流式响应
//
// SSE 流程:
// 1. 解析请求参数
// 2. 设置 SSE 响应头
// 3. 调用 StreamRunner.RunStream
// 4. 逐个推送事件到客户端
//
// 事件类型:
// - step_start: 步骤开始
// - llm_start/llm_end: LLM 调用开始/结束
// - tool_start/tool_end: 工具调用开始/结束
// - complete: 执行完成
// - error: 执行失败
//
// 注意:
// - 只支持 query_api 工具
// - 需要设置 StreamRunner
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
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("encode response failed: %v", err))
	}
}
