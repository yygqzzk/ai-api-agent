package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		inputRequestID string
		inputTraceID   string
		expectRequestID bool
	}{
		{
			name:           "无 RequestID 时自动生成",
			inputRequestID: "",
			inputTraceID:   "",
			expectRequestID: true,
		},
		{
			name:           "使用传入的 RequestID",
			inputRequestID: "test-request-123",
			inputTraceID:   "",
			expectRequestID: true,
		},
		{
			name:           "使用传入的 TraceID",
			inputRequestID: "test-request-456",
			inputTraceID:   "trace-789",
			expectRequestID: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{}
			middleware := s.requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// 验证 context 中有 RequestID
				requestID := RequestIDFromContext(r.Context())
				if requestID == "" || requestID == "unknown" {
					t.Errorf("Expected non-empty RequestID in context, got %q", requestID)
				}

				// 如果传入了 RequestID，应该保持一致
				if tt.inputRequestID != "" && requestID != tt.inputRequestID {
					t.Errorf("Expected RequestID %q, got %q", tt.inputRequestID, requestID)
				}

				// 验证 context 中有 TraceID
				traceID := TraceIDFromContext(r.Context())
				if traceID == "" || traceID == "unknown" {
					t.Errorf("Expected non-empty TraceID in context, got %q", traceID)
				}

				// 如果传入了 TraceID，应该保持一致
				if tt.inputTraceID != "" && traceID != tt.inputTraceID {
					t.Errorf("Expected TraceID %q, got %q", tt.inputTraceID, traceID)
				}

				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("POST", "/mcp", nil)
			if tt.inputRequestID != "" {
				req.Header.Set(RequestIDHeader, tt.inputRequestID)
			}
			if tt.inputTraceID != "" {
				req.Header.Set(TraceIDHeader, tt.inputTraceID)
			}

			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			// 验证响应 Header 中有 RequestID
			responseRequestID := w.Header().Get(RequestIDHeader)
			if responseRequestID == "" {
				t.Error("Expected RequestID in response header")
			}

			// 验证响应 Header 中有 TraceID
			responseTraceID := w.Header().Get(TraceIDHeader)
			if responseTraceID == "" {
				t.Error("Expected TraceID in response header")
			}
		})
	}
}

func TestRequestIDFromContext(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
		expect    string
	}{
		{"空 context 返回 unknown", "", "unknown"},
		{"有 RequestID 返回实际值", "test-123", "test-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.requestID != "" {
				ctx = ContextWithRequestID(ctx, tt.requestID)
			}
			result := RequestIDFromContext(ctx)
			if result != tt.expect {
				t.Errorf("Expected %q, got %q", tt.expect, result)
			}
		})
	}
}

func TestTraceIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		traceID  string
		requestID string
		expect   string
	}{
		{"空 context 返回 unknown", "", "", "unknown"},
		{"有 TraceID 返回 TraceID", "trace-123", "", "trace-123"},
		{"无 TraceID 有 RequestID 返回 RequestID", "", "req-456", "req-456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.traceID != "" {
				ctx = ContextWithTraceID(ctx, tt.traceID)
			}
			if tt.requestID != "" {
				ctx = ContextWithRequestID(ctx, tt.requestID)
			}
			result := TraceIDFromContext(ctx)
			if result != tt.expect {
				t.Errorf("Expected %q, got %q", tt.expect, result)
			}
		})
	}
}

func TestGenerateRequestID(t *testing.T) {
	// 生成多个 ID，验证不重复
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateRequestID()
		if id == "" {
			t.Fatal("Generated empty RequestID")
		}
		if ids[id] {
			t.Errorf("Duplicate RequestID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestRequestIDInLogging(t *testing.T) {
	// 验证 RequestID 能正确传递到日志中间件
	s := &Server{}
	middlewares := []http.Handler{
		s.requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := RequestIDFromContext(r.Context())
			w.Header().Set("X-Debug-RequestID", requestID)
			w.WriteHeader(http.StatusOK)
		})),
	}

	req := httptest.NewRequest("POST", "/mcp", nil)
	w := httptest.NewRecorder()
	middlewares[0].ServeHTTP(w, req)

	if w.Header().Get("X-Debug-RequestID") == "" {
		t.Error("Expected RequestID to be available in handler")
	}
}
