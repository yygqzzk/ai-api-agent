package agent

import "context"

// Handler 接收 agent 执行过程中的事件回调
type Handler interface {
	OnStepStart(ctx context.Context, step int)
	OnLLMStart(ctx context.Context, step int)
	OnLLMEnd(ctx context.Context, step int, reply LLMReply)
	OnToolStart(ctx context.Context, step int, call ToolCall)
	OnToolEnd(ctx context.Context, step int, trace ToolTrace)
	OnComplete(ctx context.Context, summary string, traces []ToolTrace)
	OnError(ctx context.Context, step int, err error)
}

// NoopHandler 提供空实现，具体 handler 嵌入后只覆盖关心的方法
type NoopHandler struct{}

func (NoopHandler) OnStepStart(context.Context, int)                {}
func (NoopHandler) OnLLMStart(context.Context, int)                 {}
func (NoopHandler) OnLLMEnd(context.Context, int, LLMReply)         {}
func (NoopHandler) OnToolStart(context.Context, int, ToolCall)      {}
func (NoopHandler) OnToolEnd(context.Context, int, ToolTrace)       {}
func (NoopHandler) OnComplete(context.Context, string, []ToolTrace) {}
func (NoopHandler) OnError(context.Context, int, error)             {}

// MultiHandler 将事件分发给多个 handler
type MultiHandler struct {
	handlers []Handler
}

func NewMultiHandler(handlers ...Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) OnStepStart(ctx context.Context, step int) {
	for _, h := range m.handlers {
		h.OnStepStart(ctx, step)
	}
}

func (m *MultiHandler) OnLLMStart(ctx context.Context, step int) {
	for _, h := range m.handlers {
		h.OnLLMStart(ctx, step)
	}
}

func (m *MultiHandler) OnLLMEnd(ctx context.Context, step int, reply LLMReply) {
	for _, h := range m.handlers {
		h.OnLLMEnd(ctx, step, reply)
	}
}

func (m *MultiHandler) OnToolStart(ctx context.Context, step int, call ToolCall) {
	for _, h := range m.handlers {
		h.OnToolStart(ctx, step, call)
	}
}

func (m *MultiHandler) OnToolEnd(ctx context.Context, step int, trace ToolTrace) {
	for _, h := range m.handlers {
		h.OnToolEnd(ctx, step, trace)
	}
}

func (m *MultiHandler) OnComplete(ctx context.Context, summary string, traces []ToolTrace) {
	for _, h := range m.handlers {
		h.OnComplete(ctx, summary, traces)
	}
}

func (m *MultiHandler) OnError(ctx context.Context, step int, err error) {
	for _, h := range m.handlers {
		h.OnError(ctx, step, err)
	}
}
