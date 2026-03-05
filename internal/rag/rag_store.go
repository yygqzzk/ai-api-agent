package rag

import (
	"context"

	"ai-agent-api/internal/knowledge"
)

// ScoredChunk pairs a chunk with its relevance score.
type ScoredChunk struct {
	Chunk knowledge.Chunk
	Score float64
}

// Store abstracts RAG storage for both memory and vector-db backends.
type Store interface {
	Upsert(ctx context.Context, chunks []knowledge.Chunk) error
	Search(ctx context.Context, query string, topK int, service string) ([]ScoredChunk, error)
	Close(ctx context.Context) error
}
