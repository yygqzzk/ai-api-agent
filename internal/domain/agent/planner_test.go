package agent

import (
	"context"
	"testing"
)

func TestRuleBasedPlannerBuildsWorkflowPlan(t *testing.T) {
	planner := NewRuleBasedPlanner()

	plan, err := planner.Plan(context.Background(), "分析用户注册到下单的完整流程")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Tasks) < 4 {
		t.Fatalf("expected at least 4 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Tool != "search_api" {
		t.Fatalf("expected first task use search_api, got %+v", plan.Tasks[0])
	}
	last := plan.Tasks[len(plan.Tasks)-1]
	if last.Tool != "analyze_dependencies" {
		t.Fatalf("expected final task analyze dependencies, got %+v", last)
	}
	if len(last.DependsOn) == 0 {
		t.Fatalf("expected dependency task to depend on previous tasks")
	}
}

func TestLLMPlannerFallsBackOnInvalidJSON(t *testing.T) {
	planner := NewLLMPlanner(&stubTaskLLMClient{reply: LLMReply{Content: `oops`}}, NewRuleBasedPlanner())

	plan, err := planner.Plan(context.Background(), "分析用户注册到下单流程")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.Tasks) == 0 {
		t.Fatalf("expected fallback plan tasks")
	}
}
