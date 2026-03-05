package mcp

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(s.cfg.Server.AuthToken)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := RequestIDFromContext(r.Context())

		// 使用 responseWriterWrapper 捕获状态码
		wrapped := &responseWriterWrapper{
			ResponseWriter: w,
			requestID:      requestID,
		}

		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)

		s.slog.Info("mcp request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
		)
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r.RemoteAddr)
		if !s.limiter.Allow(ip) {
			writeHTTPError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			writeHTTPError(w, http.StatusNotFound, "not found")
			return
		}
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.Contains(contentType, "application/json") {
			writeHTTPError(w, http.StatusBadRequest, "content-type must be application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeHTTPError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": message,
	})
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
