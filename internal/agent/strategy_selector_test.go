package agent

import (
	"context"
	"testing"
)

func TestRuleBasedStrategySelectorSelectsByQueryShape(t *testing.T) {
	selector := NewRuleBasedStrategySelector()

	tests := []struct {
		name  string
		query string
		want  Strategy
	}{
		{name: "simple", query: "查询用户登录接口", want: StrategySimple},
		{name: "complex", query: "分析用户注册到下单的完整流程", want: StrategyComplex},
		{name: "ambiguous", query: "查订单", want: StrategyAmbiguous},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selector.Select(context.Background(), tt.query)
			if err != nil {
				t.Fatalf("Select failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestLLMBasedStrategySelectorParsesJSON(t *testing.T) {
	selector := NewLLMBasedStrategySelector(&stubTaskLLMClient{reply: LLMReply{Content: `{"strategy":"complex","reason":"需要多步分析"}`}}, NewRuleBasedStrategySelector())

	got, err := selector.Select(context.Background(), "分析用户注册到下单流程")
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if got != StrategyComplex {
		t.Fatalf("expected complex, got %q", got)
	}
}

func TestLLMBasedStrategySelectorFallsBackWhenLLMReturnsInvalidJSON(t *testing.T) {
	selector := NewLLMBasedStrategySelector(&stubTaskLLMClient{reply: LLMReply{Content: `not-json`}}, NewRuleBasedStrategySelector())

	got, err := selector.Select(context.Background(), "查订单")
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if got != StrategyAmbiguous {
		t.Fatalf("expected ambiguous fallback, got %q", got)
	}
}
