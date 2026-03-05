package rerank

import "context"

// Document 表示待排序的文档
type Document struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
	Video string `json:"video,omitempty"`
}

// Result 表示排序结果
type Result struct {
	Index          int      `json:"index"`
	RelevanceScore float64  `json:"relevance_score"`
	Document       Document `json:"document,omitempty"`
}

// Client 定义 rerank 客户端接口
type Client interface {
	// Rerank 对文档进行重排序
	// query: 查询文本
	// documents: 待排序的文档列表
	// topN: 返回前 N 个结果，0 表示返回全部
	Rerank(ctx context.Context, query string, documents []Document, topN int) ([]Result, error)
}
