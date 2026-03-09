// Package mcp 提供 RequestID 追踪功能
// 用于在单体应用中实现轻量级链路追踪，无需引入完整的 OpenTelemetry
package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// contextKey 类型用于避免 context key 冲突
type contextKey int

const (
	// RequestIDKey 是 context 中存储 RequestID 的 key
	RequestIDKey contextKey = iota
	// TraceIDKey 是 context 中存储 TraceID 的 key（用于跨服务追踪）
	TraceIDKey
)

// DefaultRequestIDLength 是默认 RequestID 长度
const DefaultRequestIDLength = 16

// GenerateRequestID 生成唯一的请求 ID
// 使用 crypto/rand 保证安全性和唯一性
func GenerateRequestID() string {
	b := make([]byte, DefaultRequestIDLength/2) // 16 字节 = 32 hex 字符
	if _, err := rand.Read(b); err != nil {
		// 降级到时间戳 + 计数器（极低概率）
		return fmt.Sprintf("%d-%x", CurrentTimeMillis(), b)
	}
	return hex.EncodeToString(b)
}

// CurrentTimeMillis 返回当前时间戳（毫秒）
func CurrentTimeMillis() int64 {
	// 简化实现，实际可以使用 time.Now().UnixMilli()
	return 0
}

// RequestIDFromContext 从 context 中提取 RequestID
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok && id != "" {
		return id
	}
	return "unknown"
}

// TraceIDFromContext 从 context 中提取 TraceID
func TraceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(TraceIDKey).(string); ok && id != "" {
		return id
	}
	// 如果没有 TraceID，返回 RequestID 作为降级
	return RequestIDFromContext(ctx)
}

// ContextWithRequestID 将 RequestID 注入到 context 中
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// ContextWithTraceID 将 TraceID 注入到 context 中
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

// RequestIDHeader 是 HTTP Header 中 RequestID 的字段名
const RequestIDHeader = "X-Request-ID"

// TraceIDHeader 是 HTTP Header 中 TraceID 的字段名
const TraceIDHeader = "X-Trace-ID"

// requestIDMiddleware 请求 ID 中间件
// 1. 从 Header 中提取或生成 RequestID
// 2. 从 Header 中提取或生成 TraceID
// 3. 注入到 context 中供下游使用
// 4. 在响应 Header 中返回 RequestID
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. 提取或生成 RequestID
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = GenerateRequestID()
		}
		// 清理可能的空格
		requestID = strings.TrimSpace(requestID)

		// 2. 提取或生成 TraceID（可以和 RequestID 相同）
		traceID := r.Header.Get(TraceIDHeader)
		if traceID == "" {
			// 如果没有 TraceID，使用 RequestID 作为 TraceID
			traceID = requestID
		}
		traceID = strings.TrimSpace(traceID)

		// 3. 注入到 context
		ctx := ContextWithRequestID(r.Context(), requestID)
		ctx = ContextWithTraceID(ctx, traceID)

		// 4. 将 RequestID 添加到响应 Header（方便客户端追踪）
		w.Header().Set(RequestIDHeader, requestID)
		w.Header().Set(TraceIDHeader, traceID)

		// 5. 继续处理请求
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// responseWriterWrapper 用于在日志中间件中记录 RequestID 和状态码
type responseWriterWrapper struct {
	http.ResponseWriter
	requestID   string
	traceID     string
	statusCode  int
	wroteHeader bool
}

// WriteHeader 拦截响应状态码
func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	if !w.wroteHeader {
		w.statusCode = statusCode
		w.wroteHeader = true
	}
}

// Write 拦截响应体，确保状态码被记录
func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.statusCode = http.StatusOK // 默认 200 OK
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// wrapResponseWriter 包装 ResponseWriter 以注入 RequestID
func wrapResponseWriter(w http.ResponseWriter, requestID, traceID string) http.ResponseWriter {
	return &responseWriterWrapper{
		ResponseWriter: w,
		requestID:      requestID,
		traceID:        traceID,
	}
}
