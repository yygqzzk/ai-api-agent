package rag

import (
	"context"
	"wanzhi/internal/domain/model"
)

// Store 向量存储接口
// 抽象不同的向量数据库实现（Milvus、内存等）
type Store interface {
	// Search 执行语义搜索，返回最相关的文档分块
	Search(ctx context.Context, query string, topK int, filters map[string]string) ([]SearchResult, error)

	// Upsert 插入或更新文档分块
	Upsert(ctx context.Context, chunks []model.Chunk, embeddings [][]float32) error

	// Delete 删除指定 ID 的文档分块
	Delete(ctx context.Context, ids []string) error

	// DeleteByService 删除指定服务的所有数据
	DeleteByService(ctx context.Context, service string) error
}

// SearchResult 搜索结果
type SearchResult struct {
	Chunk    model.Chunk
	Score    float32
	Metadata map[string]string
}
