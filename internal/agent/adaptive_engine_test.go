package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"wanzhi/internal/tools"
)

func TestAdaptiveAgentEngineRunSimpleDelegatesToBaseEngine(t *testing.T) {
	base := &stubQueryRunner{responses: map[string]string{"查询用户登录接口": "登录接口结果"}}
	engine := NewAdaptiveAgentEngine(base, &stubTaskDispatcher{}, AdaptiveAgentEngineOptions{
		Selector:  fixedStrategySelector{strategy: StrategySimple},
		Reflector: fixedReflector{result: &ReflectionResult{Quality: 0.9}},
	})

	out, err := engine.Run(context.Background(), "查询用户登录接口")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out != "登录接口结果" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestAdaptiveAgentEngineRunAmbiguousSelectsBestRewrite(t *testing.T) {
	base := &stubQueryRunner{responses: map[string]string{
		"用户登录接口":  "找到用户登录接口，支持 username/password",
		"管理员登录接口": "找到管理员接口",
	}}
	engine := NewAdaptiveAgentEngine(base, &stubTaskDispatcher{}, AdaptiveAgentEngineOptions{
		Selector:  fixedStrategySelector{strategy: StrategyAmbiguous},
		Rewriter:  fixedRewriter{queries: []string{"管理员登录接口", "用户登录接口"}},
		Reflector: keywordReflector{},
	})

	out, err := engine.Run(context.Background(), "登录")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out, "用户登录接口") {
		t.Fatalf("expected best rewrite result, got %s", out)
	}
}

func TestAdaptiveAgentEngineRunComplexExecutesPlanTasks(t *testing.T) {
	dispatcher := &stubTaskDispatcher{}
	engine := NewAdaptiveAgentEngine(&stubQueryRunner{}, dispatcher, AdaptiveAgentEngineOptions{
		Selector: fixedStrategySelector{strategy: StrategyComplex},
		Planner: fixedPlanner{plan: &ExecutionPlan{Tasks: []Task{
			{ID: "task1", Description: "查找用户注册接口", Tool: "search_api", Args: `{"query":"用户注册"}`},
			{ID: "task2", Description: "查找用户登录接口", Tool: "search_api", Args: `{"query":"用户登录"}`},
			{ID: "task3", Description: "查找订单创建接口", Tool: "search_api", Args: `{"query":"创建订单"}`},
			{ID: "task4", Description: "分析接口依赖关系", Tool: "analyze_dependencies", Args: `{"endpoint_ref":"task3"}`, DependsOn: []string{"task1", "task2", "task3"}},
		}}},
		Reflector: fixedReflector{result: &ReflectionResult{Quality: 0.95}},
	})

	out, err := engine.Run(context.Background(), "分析用户注册到下单流程")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(out, "任务 task1") || !strings.Contains(out, "接口依赖") {
		t.Fatalf("unexpected output: %s", out)
	}
	if len(dispatcher.calls) != 4 {
		t.Fatalf("expected 4 dispatched tasks, got %d", len(dispatcher.calls))
	}
}

type stubTaskLLMClient struct {
	reply LLMReply
	err   error
}

func (s *stubTaskLLMClient) Next(_ context.Context, _ []Message, _ []ToolDefinition) (LLMReply, error) {
	return s.reply, s.err
}

type stubQueryRunner struct {
	responses map[string]string
}

func (s *stubQueryRunner) Run(_ context.Context, query string) (string, error) {
	if s.responses == nil {
		return fmt.Sprintf("base:%s", query), nil
	}
	if out, ok := s.responses[query]; ok {
		return out, nil
	}
	return fmt.Sprintf("missing:%s", query), nil
}

type stubTaskDispatcher struct {
	calls []string
}

func (s *stubTaskDispatcher) Dispatch(_ context.Context, name string, args json.RawMessage) (any, error) {
	s.calls = append(s.calls, name)
	var payload map[string]string
	_ = json.Unmarshal(args, &payload)
	switch name {
	case "search_api":
		query := payload["query"]
		return tools.SearchAPIResult{Items: []tools.SearchAPIItem{{Endpoint: endpointForQuery(query), Snippet: query}}}, nil
	case "analyze_dependencies":
		return tools.AnalyzeDependenciesResult{Endpoint: payload["endpoint"], Dependencies: []string{"接口依赖A", "接口依赖B"}}, nil
	default:
		return map[string]any{"ok": true}, nil
	}
}

func (s *stubTaskDispatcher) Has(_ string) bool { return true }

type fixedStrategySelector struct{ strategy Strategy }

func (f fixedStrategySelector) Select(_ context.Context, _ string) (Strategy, error) {
	return f.strategy, nil
}

type fixedRewriter struct{ queries []string }

func (f fixedRewriter) Rewrite(_ context.Context, _ string, _ RewriteStrategy) ([]string, error) {
	return append([]string(nil), f.queries...), nil
}

type fixedPlanner struct{ plan *ExecutionPlan }

func (f fixedPlanner) Plan(_ context.Context, _ string) (*ExecutionPlan, error) { return f.plan, nil }

type fixedReflector struct{ result *ReflectionResult }

func (f fixedReflector) Reflect(_ context.Context, _ string, _ string) (*ReflectionResult, error) {
	return f.result, nil
}

type keywordReflector struct{}

func (keywordReflector) Reflect(_ context.Context, query string, output string) (*ReflectionResult, error) {
	quality := 0.2
	if strings.Contains(output, "用户登录接口") {
		quality = 0.95
	}
	return &ReflectionResult{Quality: quality}, nil
}

func endpointForQuery(query string) string {
	switch query {
	case "用户注册":
		return "POST /users/register"
	case "用户登录":
		return "POST /users/login"
	case "创建订单":
		return "POST /orders"
	default:
		return "GET /unknown"
	}
}
