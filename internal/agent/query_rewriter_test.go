package agent

import (
	"context"
	"testing"
)

func TestRuleBasedQueryRewriterClarifyAndDecompose(t *testing.T) {
	rewriter := NewRuleBasedQueryRewriter()

	clarified, err := rewriter.Rewrite(context.Background(), "查订单", RewriteStrategyClarify)
	if err != nil {
		t.Fatalf("Rewrite clarify failed: %v", err)
	}
	if len(clarified) == 0 || clarified[0] == "查订单" {
		t.Fatalf("expected rewritten clarify queries, got %v", clarified)
	}

	decomposed, err := rewriter.Rewrite(context.Background(), "用户注册到下单流程", RewriteStrategyDecompose)
	if err != nil {
		t.Fatalf("Rewrite decompose failed: %v", err)
	}
	if len(decomposed) < 3 {
		t.Fatalf("expected multiple decomposed queries, got %v", decomposed)
	}
}

func TestLLMQueryRewriterParsesJSONAndFallsBack(t *testing.T) {
	rewriter := NewLLMQueryRewriter(&stubTaskLLMClient{reply: LLMReply{Content: `["用户登录接口","管理员登录接口"]`}}, NewRuleBasedQueryRewriter())

	queries, err := rewriter.Rewrite(context.Background(), "登录", RewriteStrategyClarify)
	if err != nil {
		t.Fatalf("Rewrite failed: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(queries))
	}

	fallback := NewLLMQueryRewriter(&stubTaskLLMClient{reply: LLMReply{Content: `oops`}}, NewRuleBasedQueryRewriter())
	queries, err = fallback.Rewrite(context.Background(), "查订单", RewriteStrategyClarify)
	if err != nil {
		t.Fatalf("fallback Rewrite failed: %v", err)
	}
	if len(queries) == 0 {
		t.Fatalf("expected fallback queries, got %v", queries)
	}
}
