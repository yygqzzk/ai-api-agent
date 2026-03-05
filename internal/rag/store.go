package rag

import (
	"context"
	"sort"
	"strings"
	"sync"

	"ai-agent-api/internal/knowledge"
)

// MemoryStore 基于内存的 RAG 存储实现
//
// 特点:
// - 使用关键词匹配算法 (而非向量相似度)
// - 无需外部依赖 (Milvus, Embedding API)
// - 适合开发和测试环境
//
// 评分机制:
// - Endpoint 匹配: +3 分 (API 路径最重要)
// - Content 匹配: +2 分 (文档内容次之)
// - Type 匹配: +1 分 (文档类型最低)
//
// 并发安全性:
// - 使用 RWMutex 保护 chunks 切片
// - Upsert: 写锁 (修改数据)
// - Search/AllChunks: 读锁 (只读数据)
//
// 性能考虑:
// - 时间复杂度: O(n*m), n=文档数, m=查询词数
// - 空间复杂度: O(n), 所有文档存储在内存
// - 适用于小规模数据 (< 10000 文档)
type MemoryStore struct {
	mu     sync.RWMutex      // 保护 chunks 的读写锁
	chunks []knowledge.Chunk // 所有文档块
}

// NewMemoryStore 创建内存存储实例
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Upsert 插入或更新文档块
//
// 实现策略:
// 1. 构建 ID -> index 映射表
// 2. 遍历新文档块:
//    - 如果 ID 已存在,更新对应位置
//    - 如果 ID 不存在,追加到末尾
//
// 并发安全性:
// - 使用写锁保护,不支持并发 Upsert
//
// 时间复杂度:
// - O(n + m), n=现有文档数, m=新文档数
func (s *MemoryStore) Upsert(_ context.Context, chunks []knowledge.Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 构建 ID -> index 映射表,用于快速查找
	index := make(map[string]int, len(s.chunks))
	for i := range s.chunks {
		index[s.chunks[i].ID] = i
	}

	// 更新或插入文档块
	for _, chunk := range chunks {
		if idx, ok := index[chunk.ID]; ok {
			// ID 已存在,更新
			s.chunks[idx] = chunk
			continue
		}
		// ID 不存在,追加
		index[chunk.ID] = len(s.chunks)
		s.chunks = append(s.chunks, chunk)
	}
	return nil
}

// Search 基于关键词匹配检索文档块
//
// 检索流程:
// 1. 分词: 将查询文本分割成关键词
// 2. 评分: 计算每个文档块的匹配分数
// 3. 过滤: 只保留分数 > 0 的文档块
// 4. 排序: 按分数降序排列
// 5. 截断: 返回前 topK 个结果
//
// 评分规则:
// - Endpoint 包含关键词: +3 分
// - Content 包含关键词: +2 分
// - Type 包含关键词: +1 分
//
// 并发安全性:
// - 使用读锁保护,支持并发 Search
//
// 时间复杂度:
// - O(n*m*k), n=文档数, m=查询词数, k=平均文档长度
func (s *MemoryStore) Search(_ context.Context, query string, topK int, service string) ([]ScoredChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 分词
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// 评分和过滤
	scored := make([]ScoredChunk, 0, len(s.chunks))
	for _, chunk := range s.chunks {
		// 服务名称过滤
		if service != "" && !strings.EqualFold(chunk.Service, service) {
			continue
		}

		// 计算匹配分数
		score := scoreChunk(chunk, tokens)
		if score == 0 {
			continue // 跳过不匹配的文档
		}

		scored = append(scored, ScoredChunk{Chunk: chunk, Score: float64(score)})
	}

	// 排序: 分数降序,分数相同时按 Endpoint 字典序
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Chunk.Endpoint < scored[j].Chunk.Endpoint
		}
		return scored[i].Score > scored[j].Score
	})

	// 截断到 topK
	if topK <= 0 || topK > len(scored) {
		topK = len(scored)
	}
	return scored[:topK], nil
}

// Close 关闭存储 (内存存储无需清理)
func (s *MemoryStore) Close(_ context.Context) error {
	return nil
}

// AllChunks 返回所有文档块 (用于测试和调试)
//
// 并发安全性:
// - 使用读锁保护
// - 返回深拷贝,避免外部修改影响内部状态
func (s *MemoryStore) AllChunks() []knowledge.Chunk {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]knowledge.Chunk, len(s.chunks))
	copy(out, s.chunks)
	return out
}

// tokenize 将查询文本分割成关键词
//
// 分词规则:
// - 转换为小写
// - 按空格、逗号、分号、中文标点分割
// - 去除空白词
//
// 示例:
// - "用户登录" -> ["用户", "登录"]
// - "user login, register" -> ["user", "login", "register"]
func tokenize(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}

	// 按多种分隔符分割
	parts := strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == '，' || r == '。'
	})

	// 去除空白词
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// scoreChunk 计算文档块的匹配分数
//
// 评分规则:
// - Endpoint 匹配: +3 分 (API 路径最重要)
// - Content 匹配: +2 分 (文档内容次之)
// - Type 匹配: +1 分 (文档类型最低)
//
// 设计考虑:
// - Endpoint 权重最高,因为用户通常通过 API 路径查找
// - Content 权重次之,包含详细的参数和描述
// - Type 权重最低,只是文档类型标识
//
// 示例:
// - 查询 "用户 登录"
// - Endpoint="/api/user/login" -> +3+3=6 分
// - Content="用户登录接口" -> +2+2=4 分
// - 总分: 10 分
func scoreChunk(chunk knowledge.Chunk, tokens []string) int {
	content := strings.ToLower(chunk.Content)
	endpoint := strings.ToLower(chunk.Endpoint)
	score := 0

	for _, tk := range tokens {
		if tk == "" {
			continue
		}

		// Content 匹配: +2 分
		if strings.Contains(content, tk) {
			score += 2
		}

		// Endpoint 匹配: +3 分
		if strings.Contains(endpoint, tk) {
			score += 3
		}

		// Type 匹配: +1 分
		if strings.Contains(strings.ToLower(chunk.Type), tk) {
			score++
		}
	}

	return score
}
