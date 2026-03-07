package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Strategy string

const (
	StrategySimple    Strategy = "simple"
	StrategyComplex   Strategy = "complex"
	StrategyAmbiguous Strategy = "ambiguous"
)

type StrategySelector interface {
	Select(ctx context.Context, query string) (Strategy, error)
}

type RuleBasedStrategySelector struct{}

func NewRuleBasedStrategySelector() *RuleBasedStrategySelector {
	return &RuleBasedStrategySelector{}
}

func (s *RuleBasedStrategySelector) Select(_ context.Context, query string) (Strategy, error) {
	normalized := strings.TrimSpace(query)
	if normalized == "" {
		return StrategySimple, nil
	}
	if isComplexQuery(normalized) {
		return StrategyComplex, nil
	}
	if isAmbiguousQuery(normalized) {
		return StrategyAmbiguous, nil
	}
	return StrategySimple, nil
}

type LLMBasedStrategySelector struct {
	llmClient LLMClient
	fallback  StrategySelector
}

func NewLLMBasedStrategySelector(llmClient LLMClient, fallback StrategySelector) *LLMBasedStrategySelector {
	if fallback == nil {
		fallback = NewRuleBasedStrategySelector()
	}
	return &LLMBasedStrategySelector{llmClient: llmClient, fallback: fallback}
}

func (s *LLMBasedStrategySelector) Select(ctx context.Context, query string) (Strategy, error) {
	if s.llmClient == nil {
		return s.fallback.Select(ctx, query)
	}
	reply, err := s.llmClient.Next(ctx, []Message{
		{Role: "system", Content: "你是查询路由器。只返回 JSON：{\"strategy\":\"simple|complex|ambiguous\",\"reason\":\"...\"}"},
		{Role: "user", Content: query},
	}, nil)
	if err != nil {
		return s.fallback.Select(ctx, query)
	}
	var result struct {
		Strategy Strategy `json:"strategy"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(reply.Content)), &result); err != nil {
		return s.fallback.Select(ctx, query)
	}
	switch result.Strategy {
	case StrategySimple, StrategyComplex, StrategyAmbiguous:
		return result.Strategy, nil
	default:
		return s.fallback.Select(ctx, query)
	}
}

func isComplexQuery(query string) bool {
	complexKeywords := []string{"流程", "步骤", "依赖", "分析", "完整", "整体", "对比", "拆解"}
	for _, keyword := range complexKeywords {
		if strings.Contains(query, keyword) {
			return true
		}
	}
	return strings.Contains(query, "到") && (strings.Contains(query, "从") || strings.Contains(query, "流程"))
}

func isAmbiguousQuery(query string) bool {
	if utf8.RuneCountInString(query) <= 4 {
		return true
	}
	ambiguousKeywords := []string{"查", "看看", "这个", "那个", "登录", "订单"}
	for _, keyword := range ambiguousKeywords {
		if strings.Contains(query, keyword) && !strings.Contains(query, "接口") {
			return true
		}
	}
	return false
}

func parseStrategy(raw string) (Strategy, error) {
	strategy := Strategy(strings.TrimSpace(strings.ToLower(raw)))
	switch strategy {
	case StrategySimple, StrategyComplex, StrategyAmbiguous:
		return strategy, nil
	default:
		return "", fmt.Errorf("unsupported strategy: %s", raw)
	}
}
