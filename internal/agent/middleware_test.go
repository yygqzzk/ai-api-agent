package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChainExecutionOrder(t *testing.T) {
	var order []string
	mw1 := func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, name string, args json.RawMessage) (any, error) {
			order = append(order, "mw1_before")
			r, e := next(ctx, name, args)
			order = append(order, "mw1_after")
			return r, e
		}
	}
	mw2 := func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, name string, args json.RawMessage) (any, error) {
			order = append(order, "mw2_before")
			r, e := next(ctx, name, args)
			order = append(order, "mw2_after")
			return r, e
		}
	}

	handler := Chain(mw1, mw2)(func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
		order = append(order, "handler")
		return "ok", nil
	})

	_, _ = handler(context.Background(), "test", nil)
	expected := []string{"mw1_before", "mw2_before", "handler", "mw2_after", "mw1_after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("position %d: expected %s, got %s", i, expected[i], order[i])
		}
	}
}

func TestRetryMiddlewareRetriesOnError(t *testing.T) {
	var calls int32
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: 0})(
		func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			n := atomic.AddInt32(&calls, 1)
			if n < 3 {
				return nil, errors.New("transient error")
			}
			return "success", nil
		},
	)

	result, err := handler(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result != "success" {
		t.Fatalf("expected 'success', got: %v", result)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetryMiddlewareStopsOnPermanentError(t *testing.T) {
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: 0})(
		func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, &permanentError{msg: "fatal"}
		},
	)

	_, err := handler(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRetryMiddlewareRespectsContextCancellation(t *testing.T) {
	var attempts int32
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 5, BaseDelay: 100 * time.Millisecond})(
		func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, errors.New("always fail")
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := handler(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if atomic.LoadInt32(&attempts) > 2 {
		t.Fatalf("expected at most 2 attempts before timeout, got %d", attempts)
	}
}

type permanentError struct{ msg string }

func (e *permanentError) Error() string   { return e.msg }
func (e *permanentError) Permanent() bool { return true }

func TestEngineWithRetryMiddleware(t *testing.T) {
	var attempts int32
	failDispatcher := &mockToolDispatcherFunc{
		hasFn: func(name string) bool { return name == "search_api" },
		dispatchFn: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n < 2 {
				return nil, errors.New("transient")
			}
			return map[string]any{"items": []string{"GET /pets"}}, nil
		},
	}

	llm := &scriptedLLMClient{replies: []LLMReply{
		{ToolCalls: []ToolCall{{ID: "1", Name: "search_api", Args: json.RawMessage(`{"query":"pets"}`)}}},
		{Content: "done"},
	}}

	engine := NewAgentEngine(llm, failDispatcher,
		WithMaxSteps(5),
		WithMiddleware(RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: 0})),
	)

	out, err := engine.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected success with retry, got: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("unexpected output: %s", out)
	}
}

type mockToolDispatcherFunc struct {
	hasFn      func(string) bool
	dispatchFn func(context.Context, string, json.RawMessage) (any, error)
}

func (m *mockToolDispatcherFunc) Has(name string) bool {
	return m.hasFn(name)
}

func (m *mockToolDispatcherFunc) Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error) {
	return m.dispatchFn(ctx, name, args)
}
