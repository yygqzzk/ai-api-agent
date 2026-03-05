package agent

import (
	"context"
	"testing"
)

type recordingHandler struct {
	NoopHandler
	events []string
}

func (h *recordingHandler) OnStepStart(_ context.Context, step int) {
	h.events = append(h.events, "step_start")
}

func (h *recordingHandler) OnToolEnd(_ context.Context, _ int, t ToolTrace) {
	h.events = append(h.events, "tool_end:"+t.Tool)
}

func (h *recordingHandler) OnComplete(_ context.Context, _ string, _ []ToolTrace) {
	h.events = append(h.events, "complete")
}

func TestMultiHandlerDispatchesToAll(t *testing.T) {
	h1 := &recordingHandler{}
	h2 := &recordingHandler{}
	multi := NewMultiHandler(h1, h2)

	ctx := context.Background()
	multi.OnStepStart(ctx, 1)
	multi.OnComplete(ctx, "done", nil)

	for i, h := range []*recordingHandler{h1, h2} {
		if len(h.events) != 2 {
			t.Fatalf("handler %d: expected 2 events, got %d: %v", i, len(h.events), h.events)
		}
		if h.events[0] != "step_start" || h.events[1] != "complete" {
			t.Fatalf("handler %d: unexpected events: %v", i, h.events)
		}
	}
}

func TestNoopHandlerDoesNotPanic(t *testing.T) {
	var h NoopHandler
	ctx := context.Background()
	h.OnStepStart(ctx, 1)
	h.OnLLMStart(ctx, 1)
	h.OnLLMEnd(ctx, 1, LLMReply{})
	h.OnToolStart(ctx, 1, ToolCall{})
	h.OnToolEnd(ctx, 1, ToolTrace{})
	h.OnComplete(ctx, "", nil)
	h.OnError(ctx, 1, nil)
}
