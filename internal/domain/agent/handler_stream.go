package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// streamHandler 将 agent 事件发送到 channel，用于 RunStream。
type streamHandler struct {
	NoopHandler
	ch  chan<- AgentEvent
	ctx context.Context
}

func newStreamHandler(ctx context.Context, ch chan<- AgentEvent) *streamHandler {
	return &streamHandler{ch: ch, ctx: ctx}
}

func (h *streamHandler) emit(ev AgentEvent) {
	select {
	case h.ch <- ev:
	case <-h.ctx.Done():
	}
}

func (h *streamHandler) OnStepStart(_ context.Context, step int) {
	h.emit(AgentEvent{Kind: EventStepStart, Step: step})
}

func (h *streamHandler) OnLLMStart(_ context.Context, step int) {
	h.emit(AgentEvent{Kind: EventLLMStart, Step: step})
}

func (h *streamHandler) OnLLMEnd(_ context.Context, step int, _ LLMReply) {
	h.emit(AgentEvent{Kind: EventLLMEnd, Step: step})
}

func (h *streamHandler) OnToolEnd(_ context.Context, step int, trace ToolTrace) {
	toolData, _ := json.Marshal(map[string]any{
		"tool":        trace.Tool,
		"success":     trace.Success,
		"duration_ms": trace.DurationMS,
		"preview":     trace.Preview,
	})
	h.emit(AgentEvent{Kind: EventToolEnd, Step: step, Tool: trace.Tool, Data: toolData})
}

func (h *streamHandler) OnComplete(_ context.Context, summary string, _ []ToolTrace) {
	h.emit(AgentEvent{Kind: EventComplete, Content: summary})
}

func (h *streamHandler) OnError(_ context.Context, step int, err error) {
	h.emit(AgentEvent{Kind: EventError, Step: step, Content: fmt.Sprintf("%v", err)})
}
