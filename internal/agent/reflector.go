package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Reflector interface {
	Reflect(ctx context.Context, query string, output string) (*ReflectionResult, error)
}

type ReflectionResult struct {
	Quality      float64  `json:"quality"`
	ShouldRetry  bool     `json:"should_retry"`
	Feedback     string   `json:"feedback"`
	Improvements []string `json:"improvements"`
}

type RuleBasedReflector struct {
	qualityThreshold float64
}

func NewRuleBasedReflector(qualityThreshold float64) *RuleBasedReflector {
	if qualityThreshold <= 0 {
		qualityThreshold = 0.7
	}
	return &RuleBasedReflector{qualityThreshold: qualityThreshold}
}

func (r *RuleBasedReflector) Reflect(_ context.Context, query string, output string) (*ReflectionResult, error) {
	query = strings.TrimSpace(query)
	output = strings.TrimSpace(output)
	if output == "" {
		return &ReflectionResult{
			Quality:      0,
			ShouldRetry:  true,
			Feedback:     "输出为空，未回答用户问题",
			Improvements: []string{"补充检索结果", "确保返回与查询直接相关的接口"},
		}, nil
	}

	quality := 0.25
	queryTokens := extractIntentTokens(query)
	matches := 0
	for _, token := range queryTokens {
		if strings.Contains(output, token) {
			matches++
		}
	}
	if len(queryTokens) > 0 {
		quality += 0.6 * float64(matches) / float64(len(queryTokens))
		if quality > 1 {
			quality = 1
		}
	}
	if strings.Contains(output, "未检索到") || strings.Contains(output, "max steps") {
		quality = 0.2
	}
	feedback := "输出基本可用"
	improvements := []string{"保持当前策略"}
	if matches == 0 || hasIntentMismatch(query, output) {
		quality = 0.3
		feedback = "输出与查询不匹配，缺少关键意图"
		improvements = []string{"重新检索，使用更精确的关键词", "检查是否混淆了接口意图"}
	}
	return &ReflectionResult{
		Quality:      quality,
		ShouldRetry:  quality < r.qualityThreshold,
		Feedback:     feedback,
		Improvements: improvements,
	}, nil
}

type LLMReflector struct {
	llmClient        LLMClient
	fallback         Reflector
	qualityThreshold float64
}

func NewLLMReflector(llmClient LLMClient, fallback Reflector, qualityThreshold float64) *LLMReflector {
	if fallback == nil {
		fallback = NewRuleBasedReflector(qualityThreshold)
	}
	if qualityThreshold <= 0 {
		qualityThreshold = 0.7
	}
	return &LLMReflector{llmClient: llmClient, fallback: fallback, qualityThreshold: qualityThreshold}
}

func (r *LLMReflector) Reflect(ctx context.Context, query string, output string) (*ReflectionResult, error) {
	if r.llmClient == nil {
		return r.fallback.Reflect(ctx, query, output)
	}
	reply, err := r.llmClient.Next(ctx, []Message{
		{Role: "system", Content: "你是结果评估器。只返回 JSON：{\"quality\":0.0,\"should_retry\":false,\"feedback\":\"...\",\"improvements\":[\"...\"]}"},
		{Role: "user", Content: fmt.Sprintf("query=%s\noutput=%s", query, output)},
	}, nil)
	if err != nil {
		return r.fallback.Reflect(ctx, query, output)
	}
	var result ReflectionResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(reply.Content)), &result); err != nil {
		return r.fallback.Reflect(ctx, query, output)
	}
	if result.Quality < 0 {
		result.Quality = 0
	}
	if result.Quality > 1 {
		result.Quality = 1
	}
	return &result, nil
}

func extractIntentTokens(query string) []string {
	tokens := make([]string, 0, 4)
	for _, token := range []string{"登录", "注册", "订单", "下单", "商品", "依赖", "流程"} {
		if strings.Contains(query, token) {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		tokens = append(tokens, query)
	}
	return tokens
}

func hasIntentMismatch(query string, output string) bool {
	switch {
	case strings.Contains(query, "登录") && !strings.Contains(output, "登录") && strings.Contains(output, "注册"):
		return true
	case strings.Contains(query, "注册") && !strings.Contains(output, "注册") && strings.Contains(output, "登录"):
		return true
	default:
		return false
	}
}
