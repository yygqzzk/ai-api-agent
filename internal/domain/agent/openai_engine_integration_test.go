package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentEngineWithToolCalls(t *testing.T) {
	// Simulate LLM that makes tool calls then final response
	llm := &scriptedLLMClient{
		replies: []LLMReply{
			{
				ToolCalls: []ToolCall{{
					ID:   "call_1",
					Name: "search_api",
					Args: mustJSON(t, map[string]any{"query": "登录", "top_k": 3}),
				}},
			},
			{
				Content: "最终汇总完成",
			},
		},
	}

	dispatcher := &mockToolDispatcher{results: map[string]any{
		"search_api": map[string]any{"items": []map[string]any{{"endpoint": "GET /user/login"}}},
	}}

	engine := NewAgentEngine(llm, dispatcher, WithMaxSteps(4))
	engine.SetToolCatalog([]ToolDefinition{{
		Name:        "search_api",
		Description: "search api",
		Schema:      json.RawMessage(`{"type":"object"}`),
	}})

	out, err := engine.Run(context.Background(), "查询登录接口")
	if err != nil {
		t.Fatalf("run agent failed: %v", err)
	}
	if !strings.Contains(out, "最终汇总完成") {
		t.Fatalf("unexpected final output: %s", out)
	}
	if !strings.Contains(out, "工具调用轨迹") {
		t.Fatalf("expected tool trace summary, got: %s", out)
	}
	if len(dispatcher.calls) != 1 || dispatcher.calls[0] != "search_api" {
		t.Fatalf("unexpected dispatcher calls: %+v", dispatcher.calls)
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
