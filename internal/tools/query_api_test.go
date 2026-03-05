package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestQueryAPIToolReturnsStructuredTrace(t *testing.T) {
	runner := &stubQueryRunner{summary: "最终汇总\n\n工具调用轨迹:\n1. step=1 tool=search_api status=ok latency=8ms preview={\"items\":[]}\n2. step=2 tool=get_api_detail status=error latency=10ms preview={\"error\":\"not found\"}"}
	tool := NewQueryAPITool(runner)

	out, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"query": "查询登录接口"}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	res, ok := out.(QueryAPIResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", out)
	}
	if len(res.Trace) != 2 {
		t.Fatalf("expected 2 trace items, got %d", len(res.Trace))
	}
	if res.Trace[0].Tool != "search_api" || res.Trace[0].Status != "ok" {
		t.Fatalf("unexpected first trace item: %+v", res.Trace[0])
	}
	if res.Trace[1].Tool != "get_api_detail" || res.Trace[1].Status != "error" {
		t.Fatalf("unexpected second trace item: %+v", res.Trace[1])
	}
	if res.Trace[1].LatencyMS != 10 {
		t.Fatalf("expected latency 10ms, got %d", res.Trace[1].LatencyMS)
	}
	if res.Summary == "" {
		t.Fatalf("expected non-empty summary")
	}
}

func TestQueryAPIToolNoTraceSection(t *testing.T) {
	runner := &stubQueryRunner{summary: "仅有总结内容"}
	tool := NewQueryAPITool(runner)

	out, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"query": "查询"}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	res := out.(QueryAPIResult)
	if len(res.Trace) != 0 {
		t.Fatalf("expected empty trace, got %d", len(res.Trace))
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}
	return b
}

type stubQueryRunner struct {
	summary string
}

func (s *stubQueryRunner) Run(_ context.Context, _ string) (string, error) {
	return s.summary, nil
}
