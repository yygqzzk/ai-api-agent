package agent

import (
	"context"
	"testing"
)

func TestRuleBasedReflectorDetectsIrrelevantOutput(t *testing.T) {
	reflector := NewRuleBasedReflector(0.7)

	result, err := reflector.Reflect(context.Background(), "查询用户登录接口", "找到了用户注册接口")
	if err != nil {
		t.Fatalf("Reflect failed: %v", err)
	}
	if result.Quality >= 0.5 {
		t.Fatalf("expected low quality, got %+v", result)
	}
	if !result.ShouldRetry {
		t.Fatalf("expected retry suggestion, got %+v", result)
	}
}

func TestLLMReflectorParsesJSON(t *testing.T) {
	reflector := NewLLMReflector(&stubTaskLLMClient{reply: LLMReply{Content: `{"quality":0.9,"should_retry":false,"feedback":"很好","improvements":["保持当前策略"]}`}}, NewRuleBasedReflector(0.7), 0.7)

	result, err := reflector.Reflect(context.Background(), "查询登录接口", "找到了用户登录接口")
	if err != nil {
		t.Fatalf("Reflect failed: %v", err)
	}
	if result.Quality != 0.9 || result.ShouldRetry {
		t.Fatalf("unexpected result: %+v", result)
	}
}
