package rag

import (
	"context"
	"fmt"

	"wanzhi/internal/embedding"
	"wanzhi/internal/knowledge"
	"wanzhi/internal/store"
)

// MilvusStore implements Store backed by a vector database.
type MilvusStore struct {
	milvus     store.MilvusClient
	embedder   embedding.Client
	collection string
}

func NewMilvusStore(milvus store.MilvusClient, embedder embedding.Client, collection string) *MilvusStore {
	return &MilvusStore{
		milvus:     milvus,
		embedder:   embedder,
		collection: collection,
	}
}

func (s *MilvusStore) Upsert(ctx context.Context, chunks []knowledge.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed chunks: %w", err)
	}

	docs := make([]store.VectorDoc, len(chunks))
	for i, c := range chunks {
		docs[i] = store.VectorDoc{
			ID:      c.ID,
			Service: c.Service,
			Text:    c.Content,
			Vector:  vectors[i],
			Meta: map[string]string{
				"endpoint":   c.Endpoint,
				"chunk_type": c.Type,
				"version":    c.Version,
			},
		}
	}

	return s.milvus.Upsert(ctx, s.collection, docs)
}

func (s *MilvusStore) Search(ctx context.Context, query string, topK int, service string) ([]ScoredChunk, error) {
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	filters := make(map[string]string)
	if service != "" {
		filters["service"] = service
	}

	results, err := s.milvus.Search(ctx, s.collection, vectors[0], topK, filters)
	if err != nil {
		return nil, fmt.Errorf("milvus search: %w", err)
	}

	scored := make([]ScoredChunk, len(results))
	for i, r := range results {
		scored[i] = ScoredChunk{
			Chunk: knowledge.Chunk{
				ID:       r.Doc.ID,
				Service:  r.Doc.Service,
				Endpoint: r.Doc.Meta["endpoint"],
				Type:     r.Doc.Meta["chunk_type"],
				Content:  r.Doc.Text,
				Version:  r.Doc.Meta["version"],
			},
			Score: float64(r.Score),
		}
	}
	return scored, nil
}

// DeleteByService 删除向量库中属于指定 service 的所有文档块，
// 与 RedisIngestor 的全量替换策略配合，保证 Milvus 和 Redis 两侧数据一致。
func (s *MilvusStore) DeleteByService(ctx context.Context, service string) error {
	return s.milvus.DeleteByService(ctx, s.collection, service)
}

func (s *MilvusStore) DeleteByIDs(ctx context.Context, ids []string) error {
	return s.milvus.DeleteByIDs(ctx, s.collection, ids)
}

func (s *MilvusStore) Close(ctx context.Context) error {
	return s.milvus.Close(ctx)
}
