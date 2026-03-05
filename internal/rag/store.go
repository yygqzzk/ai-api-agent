package rag

import (
	"context"
	"sort"
	"strings"
	"sync"

	"ai-agent-api/internal/knowledge"
)

// MemoryStore implements Store using in-memory keyword matching.
type MemoryStore struct {
	mu     sync.RWMutex
	chunks []knowledge.Chunk
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Upsert(_ context.Context, chunks []knowledge.Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *MemoryStore) Search(_ context.Context, query string, topK int, service string) ([]ScoredChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}
	scored := make([]ScoredChunk, 0, len(s.chunks))
	for _, chunk := range s.chunks {
		if service != "" && !strings.EqualFold(chunk.Service, service) {
			continue
		}
		score := scoreChunk(chunk, tokens)
		if score == 0 {
			continue
		}
		scored = append(scored, ScoredChunk{Chunk: chunk, Score: float64(score)})
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

func (s *MemoryStore) Close(_ context.Context) error {
	return nil
}

func (s *MemoryStore) AllChunks() []knowledge.Chunk {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]knowledge.Chunk, len(s.chunks))
	copy(out, s.chunks)
	return out
}

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

func scoreChunk(chunk knowledge.Chunk, tokens []string) int {
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
