package rerank

import (
	"context"
	"testing"
)

func TestNoopClient(t *testing.T) {
	client := NewNoopClient()

	docs := []Document{
		{Text: "文档1"},
		{Text: "文档2"},
		{Text: "文档3"},
	}

	results, err := client.Rerank(context.Background(), "测试查询", docs, 2)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// 验证顺序保持不变
	for i, r := range results {
		if r.Index != i {
			t.Errorf("Expected index %d, got %d", i, r.Index)
		}
		if r.RelevanceScore != 1.0 {
			t.Errorf("Expected score 1.0, got %f", r.RelevanceScore)
		}
	}
}
