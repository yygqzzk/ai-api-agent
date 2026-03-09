package rag

import (
	"context"

	"wanzhi/internal/domain/model"
)

// RerankStore 包装 Store 并添加 rerank 功能
type RerankStore struct {
	store        Store
	rerankClient Reranker
	enableRerank bool
	rerankTopN   int // rerank 后返回的结果数量
}

// NewRerankStore 创建带 rerank 功能的 Store
func NewRerankStore(store Store, rerankClient Reranker, topN int) *RerankStore {
	enableRerank := rerankClient != nil
	return &RerankStore{
		store:        store,
		rerankClient: rerankClient,
		enableRerank: enableRerank,
		rerankTopN:   topN,
	}
}

// Upsert 插入或更新文档块（直接委托给底层 store）
// Note: rerank store doesn't do embedding, requires pre-embedded chunks
func (s *RerankStore) Upsert(ctx context.Context, chunks []model.Chunk, embeddings [][]float32) error {
	return s.store.Upsert(ctx, chunks, embeddings)
}

// Search 检索并重排序文档块
//
// 流程:
// 1. 使用底层 store 进行初步检索（召回阶段）
// 2. 如果启用 rerank，使用 rerank 模型进行精准排序
// 3. 返回重排序后的结果
func (s *RerankStore) Search(ctx context.Context, query string, topK int, filters map[string]string) ([]SearchResult, error) {
	// 第一阶段：召回
	// 召回更多结果以提高 rerank 效果
	recallTopK := topK
	if s.enableRerank && topK > 0 {
		// 召回 2-3 倍的结果用于 rerank
		recallTopK = topK * 3
	}

	results, err := s.store.Search(ctx, query, recallTopK, filters)
	if err != nil {
		return nil, err
	}

	// 如果没有结果或未启用 rerank，直接返回
	if len(results) == 0 || !s.enableRerank {
		// 截断到 topK
		if topK > 0 && topK < len(results) {
			results = results[:topK]
		}
		return results, nil
	}

	// 第二阶段：重排序
	documents := make([]Document, len(results))
	for i, r := range results {
		documents[i] = Document{
			Text: r.Chunk.Content,
		}
	}

	// 调用 rerank API
	rerankTopN := s.rerankTopN
	if rerankTopN <= 0 {
		rerankTopN = topK
	}
	rerankResults, err := s.rerankClient.Rerank(ctx, query, documents, rerankTopN)
	if err != nil {
		// Rerank 失败时降级到原始结果
		if topK > 0 && topK < len(results) {
			results = results[:topK]
		}
		return results, nil
	}

	// 根据 rerank 结果重新排序
	reranked := make([]SearchResult, len(rerankResults))
	for i, r := range rerankResults {
		reranked[i] = SearchResult{
			Chunk: results[r.Index].Chunk,
			Score: float32(r.RelevanceScore),
		}
	}

	return reranked, nil
}

// DeleteByService 删除指定 service 的所有文档块，直接委托给底层 store。
func (s *RerankStore) DeleteByService(ctx context.Context, service string) error {
	return s.store.DeleteByService(ctx, service)
}

func (s *RerankStore) Delete(ctx context.Context, ids []string) error {
	return s.store.Delete(ctx, ids)
}
