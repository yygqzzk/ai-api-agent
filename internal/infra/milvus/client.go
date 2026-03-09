package milvus

import (
	"context"
	"math"
	"sort"
	"sync"
)

// MilvusClient 定义向量库最小能力边界，当前实现为内存版占位。
type MilvusClient interface {
	Upsert(ctx context.Context, collection string, docs []VectorDoc) error
	Search(ctx context.Context, collection string, vector []float32, topK int, filters map[string]string) ([]SearchResult, error)
	Query(ctx context.Context, collection string) ([]VectorDoc, error)
	// DeleteByService 删除指定 collection 中属于某个 service 的所有向量文档。
	DeleteByService(ctx context.Context, collection string, service string) error
	DeleteByIDs(ctx context.Context, collection string, ids []string) error
	Close(ctx context.Context) error
}

// VectorDoc 表示一个向量文档。
type VectorDoc struct {
	ID      string
	Service string
	Text    string
	Vector  []float32
	Meta    map[string]string
}

// SearchResult 包含匹配文档和相似度得分。
type SearchResult struct {
	Doc   VectorDoc
	Score float32
}

// InMemoryMilvusClient 作为开发期替身，接口保持与真实客户端一致。
type InMemoryMilvusClient struct {
	mu          sync.RWMutex
	collections map[string]map[string]VectorDoc
}

func NewInMemoryMilvusClient() *InMemoryMilvusClient {
	return &InMemoryMilvusClient{collections: make(map[string]map[string]VectorDoc)}
}

func (c *InMemoryMilvusClient) DeleteByService(_ context.Context, collection string, service string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	docs, ok := c.collections[collection]
	if !ok {
		return nil
	}
	for id, doc := range docs {
		if doc.Service == service {
			delete(docs, id)
		}
	}
	return nil
}

func (c *InMemoryMilvusClient) DeleteByIDs(_ context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	items, ok := c.collections[collection]
	if !ok {
		return nil
	}

	for _, id := range ids {
		delete(items, id)
	}
	return nil
}

func (c *InMemoryMilvusClient) Upsert(_ context.Context, collection string, docs []VectorDoc) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.collections[collection]; !ok {
		c.collections[collection] = make(map[string]VectorDoc)
	}
	for _, doc := range docs {
		c.collections[collection][doc.ID] = doc
	}
	return nil
}

func (c *InMemoryMilvusClient) Search(_ context.Context, collection string, vector []float32, topK int, filters map[string]string) ([]SearchResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	items := c.collections[collection]
	var results []SearchResult
	for _, doc := range items {
		if !matchFilters(doc, filters) {
			continue
		}
		score := cosineSimilarity(vector, doc.Vector)
		results = append(results, SearchResult{Doc: doc, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (c *InMemoryMilvusClient) Query(_ context.Context, collection string) ([]VectorDoc, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	items := c.collections[collection]
	out := make([]VectorDoc, 0, len(items))
	for _, doc := range items {
		out = append(out, doc)
	}
	return out, nil
}

func (c *InMemoryMilvusClient) Close(_ context.Context) error {
	return nil
}

func matchFilters(doc VectorDoc, filters map[string]string) bool {
	for k, v := range filters {
		switch k {
		case "service":
			if doc.Service != v {
				return false
			}
		default:
			if doc.Meta[k] != v {
				return false
			}
		}
	}
	return true
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}
