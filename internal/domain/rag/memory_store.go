package rag

import (
	"context"
	"sort"
	"strings"
	"sync"

	"wanzhi/internal/domain/model"
)

// MemoryStore 使用内存和关键词匹配保存文档块
type MemoryStore struct {
	mu     sync.RWMutex
	chunks []model.Chunk
}

// NewMemoryStore 创建内存存储实例
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Search 基于关键词匹配返回相关文档块
func (s *MemoryStore) Search(_ context.Context, query string, topK int, filters map[string]string) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Apply service filter if provided
	serviceFilter := ""
	if svc, ok := filters["service"]; ok {
		serviceFilter = svc
	}

	scored := make([]SearchResult, 0, len(s.chunks))
	for _, chunk := range s.chunks {
		if serviceFilter != "" && !strings.EqualFold(chunk.Service, serviceFilter) {
			continue
		}

		score := float32(scoreChunk(chunk, tokens))
		if score == 0 {
			continue
		}

		scored = append(scored, SearchResult{
			Chunk:    chunk,
			Score:    score,
			Metadata: map[string]string{"service": chunk.Service},
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Chunk.Endpoint < scored[j].Chunk.Endpoint
		}
		return scored[i].Score > scored[j].Score
	})

	if topK <= 0 || topK > len(scored) {
		topK = len(scored)
	}
	return scored[:topK], nil
}

// Upsert 按 chunk ID 插入或更新文档块
func (s *MemoryStore) Upsert(_ context.Context, chunks []model.Chunk, embeddings [][]float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Note: embeddings parameter is ignored in memory store
	// It's there for interface compatibility with vector stores

	index := make(map[string]int, len(s.chunks))
	for i := range s.chunks {
		index[s.chunks[i].ID] = i
	}

	for _, chunk := range chunks {
		if idx, ok := index[chunk.ID]; ok {
			s.chunks[idx] = chunk
			continue
		}
		index[chunk.ID] = len(s.chunks)
		s.chunks = append(s.chunks, chunk)
	}
	return nil
}

// Delete 删除指定 ID 的文档分块
func (s *MemoryStore) Delete(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return nil
	}

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	filtered := s.chunks[:0]
	for _, chunk := range s.chunks {
		if _, ok := idSet[chunk.ID]; ok {
			continue
		}
		filtered = append(filtered, chunk)
	}
	s.chunks = filtered
	return nil
}

// DeleteByService 删除属于指定 service 的所有文档块
func (s *MemoryStore) DeleteByService(_ context.Context, service string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := s.chunks[:0]
	for _, chunk := range s.chunks {
		if !strings.EqualFold(chunk.Service, service) {
			filtered = append(filtered, chunk)
		}
	}
	s.chunks = filtered
	return nil
}

// tokenize 按空白和常见标点切分查询词
func tokenize(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}

	parts := strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == '，' || r == '。'
	})

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// scoreChunk 依据 endpoint、content 和 type 的命中情况计分
func scoreChunk(chunk model.Chunk, tokens []string) int {
	content := strings.ToLower(chunk.Content)
	endpoint := strings.ToLower(chunk.Endpoint)
	score := 0

	for _, tk := range tokens {
		if tk == "" {
			continue
		}

		if strings.Contains(content, tk) {
			score += 2
		}

		if strings.Contains(endpoint, tk) {
			score += 3
		}

		if strings.Contains(strings.ToLower(chunk.Type), tk) {
			score++
		}
	}

	return score
}
