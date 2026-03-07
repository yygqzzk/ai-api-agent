// Package rag 提供文档分块后的检索接口和存储抽象。
package rag

import (
	"context"

	"ai-agent-api/internal/knowledge"
)

// ScoredChunk 表示带相关性得分的文档块。
type ScoredChunk struct {
	Chunk knowledge.Chunk // 文档块内容
	Score float64         // 相关性评分 (越高越相关)
}

// Store 抽象文档块的写入、检索与关闭能力。
type Store interface {
	Upsert(ctx context.Context, chunks []knowledge.Chunk) error

	Search(ctx context.Context, query string, topK int, service string) ([]ScoredChunk, error)

	Close(ctx context.Context) error
}
