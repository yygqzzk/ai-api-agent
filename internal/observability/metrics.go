package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus collectors. All methods are nil-safe.
type Metrics struct {
	requestsTotal      *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	toolCallsTotal     *prometheus.CounterVec
	toolCallDuration   *prometheus.HistogramVec
	llmRequestsTotal   *prometheus.CounterVec
	llmRequestDuration *prometheus.HistogramVec
	llmTokensTotal     *prometheus.CounterVec
	ragSearchesTotal   *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	// prometheus.Registerer 是 Prometheus Go 客户端定义的注册接口；用接口而不是具体类型，便于测试替换。
	m := &Metrics{
		// NewCounterVec 用于创建“按标签分组”的累加计数器，适合统计请求总量这类只增不减的数据。
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_requests_total",
			Help: "Total MCP JSON-RPC requests",
		}, []string{"method", "status"}),

		// NewHistogramVec 适合记录延迟分布；prometheus.DefBuckets 是官方给出的通用耗时桶配置。
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mcp_request_duration_seconds",
			Help:    "MCP request latency distribution",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),

		toolCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tool_calls_total",
			Help: "Total tool invocations",
		}, []string{"tool", "status"}),

		toolCallDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tool_call_duration_seconds",
			Help:    "Tool call latency distribution",
			Buckets: prometheus.DefBuckets,
		}, []string{"tool"}),

		llmRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_requests_total",
			Help: "Total LLM API calls",
		}, []string{"model", "status"}),

		llmRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "llm_request_duration_seconds",
			Help:    "LLM API call latency distribution",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"model"}),

		llmTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_tokens_total",
			Help: "Total LLM tokens consumed",
		}, []string{"model", "type"}),

		ragSearchesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rag_searches_total",
			Help: "Total RAG search operations",
		}, []string{"mode"}),
	}

	// MustRegister 会在注册冲突时直接 panic，因此适合应用启动期尽早暴露配置错误。
	reg.MustRegister(
		m.requestsTotal, m.requestDuration,
		m.toolCallsTotal, m.toolCallDuration,
		m.llmRequestsTotal, m.llmRequestDuration,
		m.llmTokensTotal, m.ragSearchesTotal,
	)
	return m
}

func (m *Metrics) RecordRequest(method, status string, durationSec float64) {
	if m == nil {
		return
	}
	m.requestsTotal.WithLabelValues(method, status).Inc()
	m.requestDuration.WithLabelValues(method).Observe(durationSec)
}

func (m *Metrics) RecordToolCall(tool, status string, durationSec float64) {
	if m == nil {
		return
	}
	m.toolCallsTotal.WithLabelValues(tool, status).Inc()
	m.toolCallDuration.WithLabelValues(tool).Observe(durationSec)
}

func (m *Metrics) RecordLLMRequest(model, status string, durationSec float64) {
	if m == nil {
		return
	}
	m.llmRequestsTotal.WithLabelValues(model, status).Inc()
	m.llmRequestDuration.WithLabelValues(model).Observe(durationSec)
}

func (m *Metrics) RecordLLMTokens(model string, promptTokens, completionTokens int) {
	if m == nil {
		return
	}
	m.llmTokensTotal.WithLabelValues(model, "prompt").Add(float64(promptTokens))
	m.llmTokensTotal.WithLabelValues(model, "completion").Add(float64(completionTokens))
}

func (m *Metrics) RecordRAGSearch(mode string) {
	if m == nil {
		return
	}
	m.ragSearchesTotal.WithLabelValues(mode).Inc()
}
