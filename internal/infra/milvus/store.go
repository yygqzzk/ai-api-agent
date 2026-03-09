package milvus

import (
	"context"
	"fmt"

	"wanzhi/internal/infra/embedding"
	"wanzhi/internal/domain/model"
	"wanzhi/internal/domain/rag"
)

// MilvusStore implements rag.Store backed by a vector database.
type MilvusStore struct {
	milvus     MilvusClient
	embedder   embedding.Client
	collection string
}

func NewMilvusStore(milvus MilvusClient, embedder embedding.Client, collection string) *MilvusStore {
	return &MilvusStore{
		milvus:     milvus,
		embedder:   embedder,
		collection: collection,
	}
}

func (s *MilvusStore) Upsert(ctx context.Context, chunks []model.Chunk, embeddings [][]float32) error {
	if len(chunks) == 0 {
		return nil
	}

	docs := make([]VectorDoc, len(chunks))
	for i, c := range chunks {
		docs[i] = VectorDoc{
			ID:      c.ID,
			Service: c.Service,
			Text:    c.Content,
			Vector:  embeddings[i],
			Meta: map[string]string{
				"endpoint":   c.Endpoint,
				"chunk_type": c.Type,
				"version":    c.Version,
			},
		}
	}

	return s.milvus.Upsert(ctx, s.collection, docs)
}

func (s *MilvusStore) Search(ctx context.Context, query string, topK int, filters map[string]string) ([]rag.SearchResult, error) {
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	results, err := s.milvus.Search(ctx, s.collection, vectors[0], topK, filters)
	if err != nil {
		return nil, fmt.Errorf("milvus search: %w", err)
	}

	scored := make([]rag.SearchResult, len(results))
	for i, r := range results {
		scored[i] = rag.SearchResult{
			Chunk: model.Chunk{
				ID:       r.Doc.ID,
				Service:  r.Doc.Service,
				Endpoint: r.Doc.Meta["endpoint"],
				Type:     r.Doc.Meta["chunk_type"],
				Content:  r.Doc.Text,
				Version:  r.Doc.Meta["version"],
			},
			Score: r.Score,
		}
	}
	return scored, nil
}

// DeleteByService 删除向量库中属于指定 service 的所有文档块，
// 与 RedisIngestor 的全量替换策略配合，保证 Milvus 和 Redis 两侧数据一致。
func (s *MilvusStore) DeleteByService(ctx context.Context, service string) error {
	return s.milvus.DeleteByService(ctx, s.collection, service)
}

func (s *MilvusStore) Delete(ctx context.Context, ids []string) error {
	return s.milvus.DeleteByIDs(ctx, s.collection, ids)
}
