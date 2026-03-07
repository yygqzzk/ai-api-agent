package rag

import (
	"context"
	"sort"
	"strings"
	"sync"

	"ai-agent-api/internal/knowledge"
)

// MemoryStore 使用内存和关键词匹配保存文档块。
type MemoryStore struct {
	mu     sync.RWMutex      // 保护 chunks 的读写锁
	chunks []knowledge.Chunk // 所有文档块
}

// NewMemoryStore 创建内存存储实例
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Upsert 按 chunk ID 插入或更新文档块。
func (s *MemoryStore) Upsert(_ context.Context, chunks []knowledge.Chunk) error {
	// sync.RWMutex 的 Lock 用于“写锁”，写入期间其他读写都会被阻塞。
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

// Search 基于关键词匹配返回相关文档块。
func (s *MemoryStore) Search(_ context.Context, query string, topK int, service string) ([]ScoredChunk, error) {
	// RLock 是“读锁”，允许多个读操作并发进行，但会和写锁互斥。
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	scored := make([]ScoredChunk, 0, len(s.chunks))
	for _, chunk := range s.chunks {
		// strings.EqualFold 做大小写无关比较，常用于用户名、服务名这类不敏感匹配。
		if service != "" && !strings.EqualFold(chunk.Service, service) {
			continue
		}

		score := scoreChunk(chunk, tokens)
		if score == 0 {
			continue
		}

		scored = append(scored, ScoredChunk{Chunk: chunk, Score: float64(score)})
	}

	// sort.Slice 接收一个“less”函数：当返回 true 时，表示 i 应排在 j 前面。
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

// Close 关闭存储 (内存存储无需清理)
func (s *MemoryStore) Close(_ context.Context) error {
	return nil
}

// AllChunks 返回当前文档块快照。
func (s *MemoryStore) AllChunks() []knowledge.Chunk {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]knowledge.Chunk, len(s.chunks))
	// copy 会把底层数组内容复制到新切片里，避免调用方改动内部状态。
	copy(out, s.chunks)
	return out
}

// tokenize 按空白和常见标点切分查询词。
func tokenize(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}

	// strings.FieldsFunc 会逐个 rune 扫描字符串；
	// 当回调返回 true 时，就把该 rune 当作分隔符。
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

// scoreChunk 依据 endpoint、content 和 type 的命中情况计分。
func scoreChunk(chunk knowledge.Chunk, tokens []string) int {
	content := strings.ToLower(chunk.Content)
	endpoint := strings.ToLower(chunk.Endpoint)
	score := 0

	for _, tk := range tokens {
		if tk == "" {
			continue
		}

		// strings.Contains 是子串判断，只要 content 里出现 tk 就返回 true。
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
