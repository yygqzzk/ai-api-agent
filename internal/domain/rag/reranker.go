package rag

import "context"

// Reranker 重排序接口
// 抽象不同的 rerank 实现（DashScope、其他提供商）
type Reranker interface {
	// Rerank 对文档进行重排序
	Rerank(ctx context.Context, query string, documents []Document, topN int) ([]RerankResult, error)
}

// Document 用于 rerank 的文档表示
type Document struct {
	Text string `json:"text"`
}

// RerankResult rerank 结果
type RerankResult struct {
	Index           int     `json:"index"`
	RelevanceScore  float64 `json:"relevance_score"`
}
