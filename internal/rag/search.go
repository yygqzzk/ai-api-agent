package rag

import (
	"context"

	"ai-agent-api/internal/knowledge"
)

// Engine wraps a Store and provides indexing + search.
type Engine struct {
	store Store
}

func NewEngine(store Store) *Engine {
	return &Engine{store: store}
}

func (e *Engine) Index(ctx context.Context, endpoints []knowledge.Endpoint, version string) error {
	chunks := BuildChunks(endpoints, version)
	return e.store.Upsert(ctx, chunks)
}

func (e *Engine) Search(ctx context.Context, query string, topK int, service string) ([]ScoredChunk, error) {
	return e.store.Search(ctx, query, topK, service)
}
