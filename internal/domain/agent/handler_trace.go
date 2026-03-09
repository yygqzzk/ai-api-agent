package agent

import "context"

// traceHandler 收集 ToolTrace 切片，用于 RunWithTrace。
type traceHandler struct {
	NoopHandler
	traces []ToolTrace
}

func newTraceHandler() *traceHandler {
	return &traceHandler{traces: make([]ToolTrace, 0)}
}

func (h *traceHandler) OnToolEnd(_ context.Context, _ int, trace ToolTrace) {
	h.traces = append(h.traces, trace)
}
