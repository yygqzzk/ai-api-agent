package rerank

import "context"

// NoopClient 空实现，不进行重排序，直接返回原始顺序
type NoopClient struct{}

// NewNoopClient 创建空 rerank 客户端
func NewNoopClient() *NoopClient {
	return &NoopClient{}
}

// Rerank 不进行重排序，按原始顺序返回
func (c *NoopClient) Rerank(ctx context.Context, query string, documents []Document, topN int) ([]Result, error) {
	results := make([]Result, len(documents))
	for i, doc := range documents {
		results[i] = Result{
			Index:          i,
			RelevanceScore: 1.0, // 默认分数
			Document:       doc,
		}
	}

	// 如果指定了 topN，只返回前 N 个
	if topN > 0 && topN < len(results) {
		results = results[:topN]
	}

	return results, nil
}
