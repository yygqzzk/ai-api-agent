package rag

import (
	"context"

	"wanzhi/internal/knowledge"
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

// DeleteByService 删除向量库中属于指定 service 的所有文档块。
func (e *Engine) DeleteByService(ctx context.Context, service string) error {
	return e.store.DeleteByService(ctx, service)
}

func (e *Engine) DeleteByIDs(ctx context.Context, ids []string) error {
	return e.store.DeleteByIDs(ctx, ids)
}

func (e *Engine) Search(ctx context.Context, query string, topK int, service string) ([]ScoredChunk, error) {
	return e.store.Search(ctx, query, topK, service)
}
