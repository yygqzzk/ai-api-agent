package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type RewriteStrategy string

const (
	RewriteStrategyExpand    RewriteStrategy = "expand"
	RewriteStrategyClarify   RewriteStrategy = "clarify"
	RewriteStrategyDecompose RewriteStrategy = "decompose"
)

type QueryRewriter interface {
	Rewrite(ctx context.Context, query string, strategy RewriteStrategy) ([]string, error)
}

type RuleBasedQueryRewriter struct{}

func NewRuleBasedQueryRewriter() *RuleBasedQueryRewriter {
	return &RuleBasedQueryRewriter{}
}

func (r *RuleBasedQueryRewriter) Rewrite(_ context.Context, query string, strategy RewriteStrategy) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	var queries []string
	switch strategy {
	case RewriteStrategyExpand:
		queries = append(queries, query, expandQuery(query), query+" 请求参数", query+" 响应示例")
	case RewriteStrategyClarify:
		queries = append(queries, clarifyQuery(query), query+" 相关接口详情")
	case RewriteStrategyDecompose:
		queries = append(queries, decomposeQuery(query)...)
	default:
		queries = append(queries, query)
	}
	return uniqueNonEmptyStrings(queries), nil
}

type LLMQueryRewriter struct {
	llmClient LLMClient
	fallback  QueryRewriter
}

func NewLLMQueryRewriter(llmClient LLMClient, fallback QueryRewriter) *LLMQueryRewriter {
	if fallback == nil {
		fallback = NewRuleBasedQueryRewriter()
	}
	return &LLMQueryRewriter{llmClient: llmClient, fallback: fallback}
}

func (r *LLMQueryRewriter) Rewrite(ctx context.Context, query string, strategy RewriteStrategy) ([]string, error) {
	if r.llmClient == nil {
		return r.fallback.Rewrite(ctx, query, strategy)
	}
	reply, err := r.llmClient.Next(ctx, []Message{
		{Role: "system", Content: "你是查询改写器。只返回 JSON 数组，例如 [\"查询1\",\"查询2\"]。"},
		{Role: "user", Content: fmt.Sprintf("strategy=%s\nquery=%s", strategy, query)},
	}, nil)
	if err != nil {
		return r.fallback.Rewrite(ctx, query, strategy)
	}
	queries, err := parseRewriteQueries(reply.Content)
	if err != nil || len(queries) == 0 {
		return r.fallback.Rewrite(ctx, query, strategy)
	}
	return uniqueNonEmptyStrings(queries), nil
}

func parseRewriteQueries(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	var array []string
	if err := json.Unmarshal([]byte(trimmed), &array); err == nil {
		return array, nil
	}
	var wrapped struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapped); err == nil {
		return wrapped.Queries, nil
	}
	return nil, fmt.Errorf("invalid rewrite response")
}

func expandQuery(query string) string {
	switch {
	case strings.Contains(query, "登录"):
		return "用户认证相关的 POST 接口，包含 username 和 password 参数"
	case strings.Contains(query, "订单"):
		return "订单相关接口，包含订单查询、订单详情和订单创建能力"
	default:
		return query + " 相关 API 接口"
	}
}

func clarifyQuery(query string) string {
	switch {
	case strings.Contains(query, "订单"):
		return "查询订单详情的 GET 接口，需要订单 ID 参数"
	case strings.Contains(query, "登录"):
		return "用户认证相关的 POST 接口，包含 username 和 password 参数"
	default:
		return query + " 的具体接口定义"
	}
}

func decomposeQuery(query string) []string {
	steps := make([]string, 0, 4)
	if strings.Contains(query, "注册") {
		steps = append(steps, "用户注册接口")
	}
	if strings.Contains(query, "登录") || strings.Contains(query, "注册到下单") || strings.Contains(query, "下单") {
		steps = append(steps, "用户登录接口")
	}
	if strings.Contains(query, "商品") || strings.Contains(query, "下单") {
		steps = append(steps, "商品查询接口")
	}
	if strings.Contains(query, "订单") || strings.Contains(query, "下单") {
		steps = append(steps, "订单创建接口")
	}
	if len(steps) == 0 {
		steps = append(steps, query)
	}
	return steps
}

func uniqueNonEmptyStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
