package rag

import (
	"context"
	"fmt"

	"wanzhi/internal/domain/model"
)

// Engine wraps a Store and provides indexing + search.
type Engine struct {
	store Store
}

func NewEngine(store Store) *Engine {
	return &Engine{store: store}
}

func (e *Engine) Index(ctx context.Context, endpoints []model.Endpoint, version string) error {
	chunks := BuildChunks(endpoints, version)
	// TODO: Generate embeddings here when embedding service is available
	// For now, pass nil embeddings
	return e.store.Upsert(ctx, chunks, nil)
}

// DeleteByService 删除向量库中属于指定 service 的所有文档块。
func (e *Engine) DeleteByService(ctx context.Context, service string) error {
	return e.store.DeleteByService(ctx, service)
}

func (e *Engine) DeleteByIDs(ctx context.Context, ids []string) error {
	return e.store.Delete(ctx, ids)
}

func (e *Engine) Search(ctx context.Context, query string, topK int, service string) ([]SearchResult, error) {
	filters := make(map[string]string)
	if service != "" {
		filters["service"] = service
	}
	return e.store.Search(ctx, query, topK, filters)
}

// ScoredChunk is a convenience alias for SearchResult with simpler naming
type ScoredChunk = SearchResult

// GetChunkFromResult extracts the chunk from a search result
func GetChunkFromResult(result SearchResult) model.Chunk {
	return result.Chunk
}

// GetScoreFromResult extracts the score from a search result
func GetScoreFromResult(result SearchResult) float32 {
	return result.Score
}

// FormatResult formats a search result for display
func FormatResult(result SearchResult) string {
	return fmt.Sprintf("[%s] %s (%.4f)", result.Chunk.Endpoint, result.Chunk.Type, result.Score)
}
