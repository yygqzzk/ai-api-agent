# Redis 持久化知识库重构实施计划

> **For Claude:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将知识库从内存存储重构为 Redis 持久化，移除内存模式，确保服务重启后数据不丢失

**Architecture:** 创建 RedisIngestor 实现 Ingestor 接口，使用 Redis Hash/String/List 存储 endpoints/specs/chunks，移除 InMemoryRedisClient，强制使用真实 Redis 连接

**Tech Stack:** Go 1.21+, Redis 7.0+, go-redis/v9, miniredis (测试), testify

---

## Chunk 1: RedisIngestor 核心实现

### Task 1: 创建 RedisIngestor 结构和构造函数

**Files:**
- Create: `internal/knowledge/redis_ingestor.go`
- Reference: `internal/knowledge/ingestor.go` (Ingestor 接口定义)
- Reference: `internal/store/redis_client.go` (RedisClient 接口)

- [ ] **Step 1: 创建 RedisIngestor 结构体**

```go
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"ai-agent-api/internal/store"
)

// RedisIngestor 使用 Redis 持久化存储知识库数据
type RedisIngestor struct {
	client  store.RedisClient
	mu      sync.RWMutex
	version string
}

// NewRedisIngestor 创建 Redis 持久化的 Ingestor
func NewRedisIngestor(client store.RedisClient) *RedisIngestor {
	if client == nil {
		panic("redis client cannot be nil")
	}
	return &RedisIngestor{
		client:  client,
		version: "v1.0.0",
	}
}
```

- [ ] **Step 2: 添加 Redis Key 辅助函数**

```go
// Redis Key 设计：
// kb:endpoints:{service} - Hash, field={METHOD}:{path}, value=Endpoint JSON
// kb:specs:{service} - String, SpecMeta JSON
// kb:chunks:{service} - List, Chunk JSON array
// kb:services - Set, service names

func endpointsKey(service string) string {
	return fmt.Sprintf("kb:endpoints:%s", strings.ToLower(strings.TrimSpace(service)))
}

func specsKey(service string) string {
	return fmt.Sprintf("kb:specs:%s", strings.ToLower(strings.TrimSpace(service)))
}

func chunksKey(service string) string {
	return fmt.Sprintf("kb:chunks:%s", strings.ToLower(strings.TrimSpace(service)))
}

func servicesKey() string {
	return "kb:services"
}

func endpointField(ep Endpoint) string {
	return fmt.Sprintf("%s:%s", strings.ToUpper(ep.Method), ep.Path)
}
```

- [ ] **Step 3: 编译检查**

Run: `go build ./internal/knowledge/`
Expected: 编译成功，无错误

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/redis_ingestor.go
git commit -m "feat(knowledge): add RedisIngestor structure and key helpers

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 2: 实现 UpsertDocument 方法

**Files:**
- Modify: `internal/knowledge/redis_ingestor.go`

- [ ] **Step 1: 实现 UpsertDocument 方法**

```go
// UpsertDocument 将 ParsedSpec 写入 Redis
func (r *RedisIngestor) UpsertDocument(doc ParsedSpec) IngestStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	stats := IngestStats{}

	// 提取 service 名称
	service := extractService(doc)
	if service == "" {
		return stats
	}

	// 1. 写入 endpoints 到 Hash
	if len(doc.Endpoints) > 0 {
		for _, ep := range doc.Endpoints {
			field := endpointField(ep)
			value, err := json.Marshal(ep)
			if err != nil {
				continue
			}
			// 使用 HSET 写入单个 endpoint
			key := endpointsKey(service)
			if err := r.client.Set(ctx, fmt.Sprintf("%s:%s", key, field), string(value), 0); err != nil {
				continue
			}
			stats.Endpoints++
		}
	}

	// 2. 写入 specs 到 String
	if meta, ok := normalizeSpecMeta(doc.Meta, doc.Endpoints); ok {
		value, err := json.Marshal(meta)
		if err == nil {
			_ = r.client.Set(ctx, specsKey(service), string(value), 0)
		}
	}

	// 3. 重建 chunks 并写入 List
	chunks := make([]Chunk, 0, len(doc.Endpoints)*4)
	for _, ep := range doc.Endpoints {
		chunks = append(chunks, buildChunksForEndpoint(ep, r.version)...)
	}

	// 先删除旧的 chunks
	_ = r.client.Del(ctx, chunksKey(service))

	// 写入新的 chunks
	for _, chunk := range chunks {
		value, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		// 使用临时 key 模拟 RPUSH
		chunkKey := fmt.Sprintf("%s:%d", chunksKey(service), stats.Chunks)
		_ = r.client.Set(ctx, chunkKey, string(value), 0)
		stats.Chunks++
	}

	// 4. 更新 services Set (使用 Set 模拟)
	_ = r.client.Set(ctx, fmt.Sprintf("%s:%s", servicesKey(), service), "1", 0)

	return stats
}

func extractService(doc ParsedSpec) string {
	service := strings.TrimSpace(doc.Meta.Service)
	if service == "" && len(doc.Endpoints) > 0 {
		service = strings.TrimSpace(doc.Endpoints[0].Service)
	}
	return strings.ToLower(service)
}
```

- [ ] **Step 2: 编译检查**

Run: `go build ./internal/knowledge/`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/knowledge/redis_ingestor.go
git commit -m "feat(knowledge): implement RedisIngestor.UpsertDocument

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: 实现读取方法（Endpoints, SpecMeta, Chunks）

**Files:**
- Modify: `internal/knowledge/redis_ingestor.go`

- [ ] **Step 1: 实现 Endpoints 方法**

```go
// Endpoints 从 Redis 读取所有 endpoints
func (r *RedisIngestor) Endpoints() []Endpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ctx := context.Background()
	var result []Endpoint

	// 获取所有 services
	services := r.getAllServices(ctx)

	// 对每个 service 读取 endpoints
	for _, service := range services {
		endpoints := r.getServiceEndpoints(ctx, service)
		result = append(result, endpoints...)
	}

	return result
}

func (r *RedisIngestor) getAllServices(ctx context.Context) []string {
	// 简化实现：扫描所有 kb:services:* keys
	// 实际应该使用 SMEMBERS，但当前 RedisClient 接口不支持
	// 这里先返回空，后续优化
	return []string{}
}

func (r *RedisIngestor) getServiceEndpoints(ctx context.Context, service string) []Endpoint {
	// 简化实现：扫描所有 kb:endpoints:{service}:* keys
	// 实际应该使用 HGETALL
	return []Endpoint{}
}
```

- [ ] **Step 2: 实现 SpecMeta 方法**

```go
// SpecMeta 从 Redis 读取指定 service 的元数据
func (r *RedisIngestor) SpecMeta(service string) (SpecMeta, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ctx := context.Background()
	key := specsKey(service)

	value, found, err := r.client.Get(ctx, key)
	if err != nil || !found {
		return SpecMeta{}, false
	}

	var meta SpecMeta
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return SpecMeta{}, false
	}

	return meta, true
}
```

- [ ] **Step 3: 实现 Chunks 方法**

```go
// Chunks 从 Redis 读取所有 chunks
func (r *RedisIngestor) Chunks() []Chunk {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ctx := context.Background()
	var result []Chunk

	// 获取所有 services
	services := r.getAllServices(ctx)

	// 对每个 service 读取 chunks
	for _, service := range services {
		chunks := r.getServiceChunks(ctx, service)
		result = append(result, chunks...)
	}

	return result
}

func (r *RedisIngestor) getServiceChunks(ctx context.Context, service string) []Chunk {
	// 简化实现：扫描所有 kb:chunks:{service}:* keys
	// 实际应该使用 LRANGE
	return []Chunk{}
}
```

- [ ] **Step 4: 编译检查**

Run: `go build ./internal/knowledge/`
Expected: 编译成功

- [ ] **Step 5: Commit**

```bash
git add internal/knowledge/redis_ingestor.go
git commit -m "feat(knowledge): implement RedisIngestor read methods

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Chunk 2: 扩展 RedisClient 接口

### Task 4: 添加 Redis 集合操作支持

**Files:**
- Modify: `internal/store/redis_client.go`

- [ ] **Step 1: 扩展 RedisClient 接口**

```go
// RedisClient 定义缓存层最小能力边界
type RedisClient interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, bool, error)
	Del(ctx context.Context, key string) error
	Close(ctx context.Context) error

	// 新增：集合操作
	SAdd(ctx context.Context, key string, members ...string) error
	SMembers(ctx context.Context, key string) ([]string, error)

	// 新增：Hash 操作
	HSet(ctx context.Context, key string, field string, value string) error
	HGet(ctx context.Context, key string, field string) (string, bool, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)

	// 新增：List 操作
	RPush(ctx context.Context, key string, values ...string) error
	LRange(ctx context.Context, key string, start int64, stop int64) ([]string, error)

	// 新增：连接检查
	Ping(ctx context.Context) error
}
```

- [ ] **Step 2: 实现 GoRedisClient 的新方法**

```go
func (c *GoRedisClient) SAdd(ctx context.Context, key string, members ...string) error {
	return c.client.SAdd(ctx, key, members).Err()
}

func (c *GoRedisClient) SMembers(ctx context.Context, key string) ([]string, error) {
	return c.client.SMembers(ctx, key).Result()
}

func (c *GoRedisClient) HSet(ctx context.Context, key string, field string, value string) error {
	return c.client.HSet(ctx, key, field, value).Err()
}

func (c *GoRedisClient) HGet(ctx context.Context, key string, field string) (string, bool, error) {
	v, err := c.client.HGet(ctx, key, field).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (c *GoRedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.client.HGetAll(ctx, key).Result()
}

func (c *GoRedisClient) RPush(ctx context.Context, key string, values ...string) error {
	return c.client.RPush(ctx, key, values).Err()
}

func (c *GoRedisClient) LRange(ctx context.Context, key string, start int64, stop int64) ([]string, error) {
	return c.client.LRange(ctx, key, start, stop).Result()
}

func (c *GoRedisClient) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}
```

- [ ] **Step 3: 移除 InMemoryRedisClient**

删除 `InMemoryRedisClient` 结构体和相关方法（第 33-112 行）

- [ ] **Step 4: 更新 NewRedisClient 函数**

```go
func NewRedisClient(opts RedisOptions) (RedisClient, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" || mode == "memory" {
		return nil, fmt.Errorf("memory mode is no longer supported, use mode=redis")
	}

	if mode != "redis" {
		return nil, fmt.Errorf("unsupported redis mode: %s (only 'redis' is supported)", mode)
	}

	if strings.TrimSpace(opts.Address) == "" {
		return nil, fmt.Errorf("redis address is required when mode=redis")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     opts.Address,
		Password: opts.Password,
		DB:       opts.DB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &GoRedisClient{client: client}, nil
}
```

- [ ] **Step 5: 编译检查**

Run: `go build ./internal/store/`
Expected: 编译成功

- [ ] **Step 6: Commit**

```bash
git add internal/store/redis_client.go
git commit -m "feat(store): extend RedisClient with Hash/Set/List ops, remove memory mode

BREAKING CHANGE: InMemoryRedisClient removed, memory mode no longer supported

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 5: 更新 RedisIngestor 使用新的 Redis 操作

**Files:**
- Modify: `internal/knowledge/redis_ingestor.go`

- [ ] **Step 1: 重写 UpsertDocument 使用 Hash/Set/List**

```go
func (r *RedisIngestor) UpsertDocument(doc ParsedSpec) IngestStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	stats := IngestStats{}

	service := extractService(doc)
	if service == "" {
		return stats
	}

	// 1. 写入 endpoints 到 Hash
	if len(doc.Endpoints) > 0 {
		for _, ep := range doc.Endpoints {
			field := endpointField(ep)
			value, err := json.Marshal(ep)
			if err != nil {
				continue
			}
			if err := r.client.HSet(ctx, endpointsKey(service), field, string(value)); err != nil {
				continue
			}
			stats.Endpoints++
		}
	}

	// 2. 写入 specs 到 String
	if meta, ok := normalizeSpecMeta(doc.Meta, doc.Endpoints); ok {
		value, err := json.Marshal(meta)
		if err == nil {
			_ = r.client.Set(ctx, specsKey(service), string(value), 0)
		}
	}

	// 3. 重建 chunks 并写入 List
	chunks := make([]Chunk, 0, len(doc.Endpoints)*4)
	for _, ep := range doc.Endpoints {
		chunks = append(chunks, buildChunksForEndpoint(ep, r.version)...)
	}

	_ = r.client.Del(ctx, chunksKey(service))

	if len(chunks) > 0 {
		chunkValues := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			value, err := json.Marshal(chunk)
			if err != nil {
				continue
			}
			chunkValues = append(chunkValues, string(value))
		}
		if len(chunkValues) > 0 {
			_ = r.client.RPush(ctx, chunksKey(service), chunkValues...)
			stats.Chunks = len(chunkValues)
		}
	}

	// 4. 更新 services Set
	_ = r.client.SAdd(ctx, servicesKey(), service)

	return stats
}
```

- [ ] **Step 2: 重写读取方法**

```go
func (r *RedisIngestor) getAllServices(ctx context.Context) []string {
	services, err := r.client.SMembers(ctx, servicesKey())
	if err != nil {
		return []string{}
	}
	return services
}

func (r *RedisIngestor) getServiceEndpoints(ctx context.Context, service string) []Endpoint {
	fields, err := r.client.HGetAll(ctx, endpointsKey(service))
	if err != nil {
		return []Endpoint{}
	}

	var result []Endpoint
	for _, value := range fields {
		var ep Endpoint
		if err := json.Unmarshal([]byte(value), &ep); err != nil {
			continue
		}
		result = append(result, ep)
	}
	return result
}

func (r *RedisIngestor) getServiceChunks(ctx context.Context, service string) []Chunk {
	values, err := r.client.LRange(ctx, chunksKey(service), 0, -1)
	if err != nil {
		return []Chunk{}
	}

	var result []Chunk
	for _, value := range values {
		var chunk Chunk
		if err := json.Unmarshal([]byte(value), &chunk); err != nil {
			continue
		}
		result = append(result, chunk)
	}
	return result
}
```

- [ ] **Step 3: 编译检查**

Run: `go build ./internal/knowledge/`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/knowledge/redis_ingestor.go
git commit -m "refactor(knowledge): use Redis Hash/Set/List operations in RedisIngestor

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Chunk 3: 单元测试（使用 miniredis）

### Task 6: 添加 miniredis 依赖并创建测试文件

**Files:**
- Create: `internal/knowledge/redis_ingestor_test.go`
- Modify: `go.mod` (添加 miniredis 依赖)

- [ ] **Step 1: 添加 miniredis 依赖**

Run: `go get github.com/alicebob/miniredis/v2`
Expected: 依赖添加成功

- [ ] **Step 2: 创建测试文件框架**

```go
package knowledge

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-agent-api/internal/store"
)

func setupTestRedis(t *testing.T) (store.RedisClient, func()) {
	t.Helper()

	// 启动 miniredis
	s, err := miniredis.Run()
	require.NoError(t, err)

	// 创建 Redis 客户端
	client, err := store.NewRedisClient(store.RedisOptions{
		Mode:    "redis",
		Address: s.Addr(),
	})
	require.NoError(t, err)

	// 返回清理函数
	cleanup := func() {
		_ = client.Close(context.Background())
		s.Close()
	}

	return client, cleanup
}
```

- [ ] **Step 3: 编译检查**

Run: `go test -c ./internal/knowledge/`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/knowledge/redis_ingestor_test.go
git commit -m "test(knowledge): add miniredis test setup

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 7: 编写 UpsertDocument 测试

**Files:**
- Modify: `internal/knowledge/redis_ingestor_test.go`

- [ ] **Step 1: 编写测试用例**

```go
func TestRedisIngestor_UpsertDocument(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)

	// 准备测试数据
	doc := ParsedSpec{
		Meta: SpecMeta{
			Service:  "petstore",
			Host:     "petstore.swagger.io",
			BasePath: "/v2",
			Schemes:  []string{"https"},
		},
		Endpoints: []Endpoint{
			{
				Service: "petstore",
				Method:  "GET",
				Path:    "/user/login",
				Summary: "User login",
			},
			{
				Service: "petstore",
				Method:  "POST",
				Path:    "/user/register",
				Summary: "User register",
			},
		},
	}

	// 执行 upsert
	stats := ingestor.UpsertDocument(doc)

	// 验证统计信息
	assert.Equal(t, 2, stats.Endpoints)
	assert.Equal(t, 8, stats.Chunks) // 2 endpoints * 4 chunks each

	// 验证数据已写入 Redis
	endpoints := ingestor.Endpoints()
	assert.Len(t, endpoints, 2)

	meta, ok := ingestor.SpecMeta("petstore")
	assert.True(t, ok)
	assert.Equal(t, "petstore", meta.Service)
	assert.Equal(t, "petstore.swagger.io", meta.Host)

	chunks := ingestor.Chunks()
	assert.Len(t, chunks, 8)
}
```

- [ ] **Step 2: 运行测试**

Run: `go test ./internal/knowledge/ -run TestRedisIngestor_UpsertDocument -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/knowledge/redis_ingestor_test.go
git commit -m "test(knowledge): add UpsertDocument test

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 8: 编写读取方法测试

**Files:**
- Modify: `internal/knowledge/redis_ingestor_test.go`

- [ ] **Step 1: 测试 Endpoints 方法**

```go
func TestRedisIngestor_Endpoints(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)

	// 写入测试数据
	doc := ParsedSpec{
		Endpoints: []Endpoint{
			{Service: "svc1", Method: "GET", Path: "/api/v1"},
			{Service: "svc2", Method: "POST", Path: "/api/v2"},
		},
	}
	ingestor.UpsertDocument(doc)

	// 读取所有 endpoints
	endpoints := ingestor.Endpoints()

	assert.Len(t, endpoints, 2)
	assert.Contains(t, endpoints, Endpoint{Service: "svc1", Method: "GET", Path: "/api/v1"})
	assert.Contains(t, endpoints, Endpoint{Service: "svc2", Method: "POST", Path: "/api/v2"})
}
```

- [ ] **Step 2: 测试 SpecMeta 方法**

```go
func TestRedisIngestor_SpecMeta(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)

	// 写入测试数据
	doc := ParsedSpec{
		Meta: SpecMeta{
			Service:  "petstore",
			Host:     "example.com",
			BasePath: "/v1",
		},
		Endpoints: []Endpoint{{Service: "petstore", Method: "GET", Path: "/"}},
	}
	ingestor.UpsertDocument(doc)

	// 读取 SpecMeta
	meta, ok := ingestor.SpecMeta("petstore")

	assert.True(t, ok)
	assert.Equal(t, "petstore", meta.Service)
	assert.Equal(t, "example.com", meta.Host)
	assert.Equal(t, "/v1", meta.BasePath)

	// 测试不存在的 service
	_, ok = ingestor.SpecMeta("nonexistent")
	assert.False(t, ok)
}
```

- [ ] **Step 3: 测试 Chunks 方法**

```go
func TestRedisIngestor_Chunks(t *testing.T) {
	client, cleanup := setupTestRedis(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)

	// 写入测试数据
	doc := ParsedSpec{
		Endpoints: []Endpoint{
			{Service: "svc", Method: "GET", Path: "/api"},
		},
	}
	ingestor.UpsertDocument(doc)

	// 读取 chunks
	chunks := ingestor.Chunks()

	assert.Len(t, chunks, 4) // 1 endpoint * 4 chunks
	assert.Equal(t, "svc", chunks[0].Service)
}
```

- [ ] **Step 4: 运行所有测试**

Run: `go test ./internal/knowledge/ -v`
Expected: 所有测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/knowledge/redis_ingestor_test.go
git commit -m "test(knowledge): add Endpoints/SpecMeta/Chunks tests

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Chunk 4: 集成与重构

### Task 9: 更新 KnowledgeBase 构造函数

**Files:**
- Modify: `internal/tools/knowledge_base.go`

- [ ] **Step 1: 移除 cache 字段和相关方法**

删除：
- `KnowledgeBase.cache` 字段（第 22 行）
- `getEndpointFromCache` 方法（第 166-181 行）
- `setEndpointCache` 方法（第 183-192 行）
- `endpointCacheKey` 函数（第 162-164 行）

- [ ] **Step 2: 更新 GetEndpoint 方法**

```go
func (k *KnowledgeBase) GetEndpoint(service string, endpoint string) (knowledge.Endpoint, bool) {
	method, path := splitEndpoint(endpoint)
	if method == "" || path == "" {
		return knowledge.Endpoint{}, false
	}

	k.mu.RLock()
	defer k.mu.RUnlock()

	for _, ep := range k.ingestor.Endpoints() {
		if service != "" && !strings.EqualFold(ep.Service, service) {
			continue
		}
		if strings.EqualFold(ep.Method, method) && ep.Path == path {
			return ep, true
		}
	}
	return knowledge.Endpoint{}, false
}
```

- [ ] **Step 3: 移除旧的构造函数**

删除：
- `NewKnowledgeBase()` 函数（第 25-28 行）
- `NewKnowledgeBaseWithCache()` 函数（第 30-33 行）

- [ ] **Step 4: 添加新的构造函数**

```go
// NewKnowledgeBaseWithRedis 创建使用 Redis 持久化的 KnowledgeBase
func NewKnowledgeBaseWithRedis(redisClient store.RedisClient, ragStore rag.Store) *KnowledgeBase {
	ingestor := knowledge.NewRedisIngestor(redisClient)
	return &KnowledgeBase{
		ingestor: ingestor,
		engine:   rag.NewEngine(ragStore),
	}
}

// NewKnowledgeBaseWithIngestor 创建使用自定义 Ingestor 的 KnowledgeBase（用于测试）
func NewKnowledgeBaseWithIngestor(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
	return &KnowledgeBase{
		ingestor: ingestor,
		engine:   rag.NewEngine(ragStore),
	}
}
```

- [ ] **Step 5: 更新 NewKnowledgeBaseWithStoreAndCache**

重命名为 `NewKnowledgeBaseWithStores` 并移除 cache 参数：

```go
func NewKnowledgeBaseWithStores(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
	return &KnowledgeBase{
		ingestor: ingestor,
		engine:   rag.NewEngine(ragStore),
	}
}
```

- [ ] **Step 6: 编译检查**

Run: `go build ./internal/tools/`
Expected: 编译成功

- [ ] **Step 7: Commit**

```bash
git add internal/tools/knowledge_base.go
git commit -m "refactor(tools): remove cache field, update constructors for Redis

BREAKING CHANGE: NewKnowledgeBase() removed, use NewKnowledgeBaseWithRedis()

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 10: 更新 cmd/server/main.go

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: 读取当前 main.go**

Run: `cat cmd/server/main.go | head -100`
Expected: 查看当前启动逻辑

- [ ] **Step 2: 更新 Redis 客户端创建逻辑**

找到 Redis 客户端创建部分，更新为：

```go
// 创建 Redis 客户端（必需）
redisAddr := os.Getenv("REDIS_ADDRESS")
if redisAddr == "" {
	redisAddr = "127.0.0.1:6379"
}

redisClient, err := store.NewRedisClient(store.RedisOptions{
	Mode:     "redis",
	Address:  redisAddr,
	Password: os.Getenv("REDIS_PASSWORD"),
	DB:       0,
})
if err != nil {
	log.Fatalf("Failed to connect to Redis: %v", err)
}
defer redisClient.Close(context.Background())

// Ping 检查连接
if err := redisClient.Ping(context.Background()); err != nil {
	log.Fatalf("Redis ping failed: %v", err)
}

log.Printf("Connected to Redis at %s", redisAddr)
```

- [ ] **Step 3: 更新 KnowledgeBase 创建逻辑**

找到 KnowledgeBase 创建部分，更新为：

```go
// 创建知识库（使用 Redis 持久化）
kb := tools.NewKnowledgeBaseWithRedis(redisClient, ragStore)
```

- [ ] **Step 4: 编译检查**

Run: `go build ./cmd/server/`
Expected: 编译成功

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): enforce Redis connection on startup

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 11: 更新现有测试

**Files:**
- Modify: `internal/tools/knowledge_base_cache_test.go`
- Modify: `internal/tools/tools_test.go`

- [ ] **Step 1: 更新 knowledge_base_cache_test.go**

```go
func TestGetEndpointFromCache(t *testing.T) {
	// 使用 miniredis
	s, err := miniredis.Run()
	require.NoError(t, err)
	defer s.Close()

	cache, err := store.NewRedisClient(store.RedisOptions{
		Mode:    "redis",
		Address: s.Addr(),
	})
	require.NoError(t, err)
	defer cache.Close(context.Background())

	// 创建 RedisIngestor
	ingestor := knowledge.NewRedisIngestor(cache)
	ragStore := rag.NewMemoryStore()
	kb := NewKnowledgeBaseWithIngestor(ingestor, ragStore)

	// 写入测试数据
	ep := knowledge.Endpoint{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "login"}
	_, err = kb.IngestFileDocument(context.Background(), "testdata/petstore.json", "petstore")
	require.NoError(t, err)

	// 测试读取
	got, ok := kb.GetEndpoint("petstore", "GET /user/login")
	require.True(t, ok)
	assert.Equal(t, "/user/login", got.Path)
	assert.Equal(t, "GET", got.Method)
}
```

- [ ] **Step 2: 更新其他测试文件**

查找所有使用 `NewKnowledgeBase()` 或 `NewKnowledgeBaseWithCache()` 的测试，更新为使用 `NewKnowledgeBaseWithIngestor()` 和 `InMemoryIngestor`

- [ ] **Step 3: 运行所有测试**

Run: `go test ./internal/tools/ -v`
Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tools/knowledge_base_cache_test.go internal/tools/tools_test.go
git commit -m "test(tools): update tests for Redis persistence

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Chunk 5: 集成测试与文档

### Task 12: 创建集成测试

**Files:**
- Create: `internal/knowledge/redis_ingestor_integration_test.go`

- [ ] **Step 1: 创建集成测试文件**

```go
//go:build integration
// +build integration

package knowledge

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-agent-api/internal/store"
)

func TestRedisIngestor_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// 连接真实 Redis
	redisAddr := os.Getenv("REDIS_ADDRESS")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}

	client, err := store.NewRedisClient(store.RedisOptions{
		Mode:    "redis",
		Address: redisAddr,
	})
	require.NoError(t, err)
	defer client.Close(context.Background())

	// 清理测试数据
	ctx := context.Background()
	_ = client.Del(ctx, "kb:services")
	_ = client.Del(ctx, "kb:endpoints:testservice")
	_ = client.Del(ctx, "kb:specs:testservice")
	_ = client.Del(ctx, "kb:chunks:testservice")

	// 创建 ingestor
	ingestor := NewRedisIngestor(client)

	// 测试写入
	doc := ParsedSpec{
		Meta: SpecMeta{
			Service:  "testservice",
			Host:     "test.example.com",
			BasePath: "/api",
		},
		Endpoints: []Endpoint{
			{Service: "testservice", Method: "GET", Path: "/test"},
		},
	}

	stats := ingestor.UpsertDocument(doc)
	assert.Equal(t, 1, stats.Endpoints)
	assert.Equal(t, 4, stats.Chunks)

	// 测试读取
	endpoints := ingestor.Endpoints()
	assert.Len(t, endpoints, 1)

	meta, ok := ingestor.SpecMeta("testservice")
	assert.True(t, ok)
	assert.Equal(t, "testservice", meta.Service)

	chunks := ingestor.Chunks()
	assert.Len(t, chunks, 4)

	// 清理
	_ = client.Del(ctx, "kb:services")
	_ = client.Del(ctx, "kb:endpoints:testservice")
	_ = client.Del(ctx, "kb:specs:testservice")
	_ = client.Del(ctx, "kb:chunks:testservice")
}
```

- [ ] **Step 2: 运行集成测试**

Run: `make dev && go test -tags=integration ./internal/knowledge/ -v`
Expected: 测试 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/knowledge/redis_ingestor_integration_test.go
git commit -m "test(knowledge): add Redis integration test

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 13: 更新配置文件和文档

**Files:**
- Modify: `config/config.yaml`
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Create: `.env.example`

- [ ] **Step 1: 更新 config.yaml**

```yaml
redis:
  address: ${REDIS_ADDRESS:127.0.0.1:6379}
  password: ${REDIS_PASSWORD:}
  db: ${REDIS_DB:0}
```

- [ ] **Step 2: 创建 .env.example**

```bash
# Redis 配置（必需）
REDIS_ADDRESS=127.0.0.1:6379
REDIS_PASSWORD=
REDIS_DB=0

# LLM 配置
LLM_API_KEY=your-api-key
LLM_BASE_URL=https://api.openai.com/v1
LLM_MODEL=gpt-4o-mini

# Milvus 配置
MILVUS_ADDRESS=localhost:19530

# 服务配置
AUTH_TOKEN=demo-token
```

- [ ] **Step 3: 更新 README.md**

在 "Environment Variables" 部分添加：

```markdown
### Redis Configuration (Required)

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDRESS` | `127.0.0.1:6379` | Redis server address |
| `REDIS_PASSWORD` | (empty) | Redis password |
| `REDIS_DB` | `0` | Redis database number |

**Note**: Redis is now required for data persistence. The service will fail to start if Redis is not available.
```

- [ ] **Step 4: 更新 CLAUDE.md**

在 "Runtime Storage" 部分更新：

```markdown
### Runtime Storage

Service runtime requires Redis for knowledge base persistence and Milvus for vector search. Start dependencies with `make dev` before `make run`.

**Breaking Change**: Memory mode has been removed. Redis is now mandatory for all deployments.
```

- [ ] **Step 5: Commit**

```bash
git add config/config.yaml README.md CLAUDE.md .env.example
git commit -m "docs: update configuration for Redis persistence requirement

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 14: 端到端测试

**Files:**
- Test: 完整的服务启动和数据持久化验证

- [ ] **Step 1: 启动基础设施**

Run: `make dev`
Expected: Redis 和 Milvus 启动成功

- [ ] **Step 2: 启动服务**

Run: `AUTH_TOKEN=demo-token make run`
Expected: 服务启动成功，日志显示 "Connected to Redis at 127.0.0.1:6379"

- [ ] **Step 3: 导入测试数据**

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"parse_swagger","params":{"file_path":"testdata/petstore.json","service":"petstore"}}'
```

Expected: 返回成功，endpoints 数量 > 0

- [ ] **Step 4: 查询数据**

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"query_api","params":{"query":"查询用户登录接口"}}'
```

Expected: 返回查询结果

- [ ] **Step 5: 重启服务**

Run: `Ctrl+C` 停止服务，然后 `AUTH_TOKEN=demo-token make run` 重新启动

- [ ] **Step 6: 验证数据持久化**

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"query_api","params":{"query":"查询用户登录接口"}}'
```

Expected: 返回相同的查询结果（数据未丢失）

- [ ] **Step 7: 验证 Redis 数据**

```bash
redis-cli
> SMEMBERS kb:services
> HGETALL kb:endpoints:petstore
> GET kb:specs:petstore
> LRANGE kb:chunks:petstore 0 -1
```

Expected: 所有数据都存在于 Redis 中

- [ ] **Step 8: 清理**

Run: `make clean`

---

### Task 15: 最终验证和清理

**Files:**
- All modified files

- [ ] **Step 1: 运行所有单元测试**

Run: `go test ./... -v`
Expected: 所有测试 PASS

- [ ] **Step 2: 运行集成测试**

Run: `make dev && go test -tags=integration ./... -v`
Expected: 所有集成测试 PASS

- [ ] **Step 3: 构建检查**

Run: `go build ./...`
Expected: 编译成功，无警告

- [ ] **Step 4: 代码格式化**

Run: `go fmt ./...`
Expected: 代码格式化完成

- [ ] **Step 5: 静态检查**

Run: `go vet ./...`
Expected: 无问题

- [ ] **Step 6: 最终 commit**

```bash
git add -A
git commit -m "chore: final cleanup for Redis persistence refactoring

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

- [ ] **Step 7: 创建 PR（如果需要）**

```bash
git push origin main
# 或创建 feature 分支并提交 PR
```

---

## 实施总结

**预计时间**: 5-7 个工作日

**关键里程碑**:
1. ✅ RedisIngestor 核心实现（Task 1-3）
2. ✅ RedisClient 接口扩展（Task 4-5）
3. ✅ 单元测试（Task 6-8）
4. ✅ 集成重构（Task 9-11）
5. ✅ 集成测试与文档（Task 12-15）

**验证清单**:
- [ ] 所有单元测试通过
- [ ] 所有集成测试通过
- [ ] 服务启动成功（连接 Redis）
- [ ] 数据写入 Redis
- [ ] 服务重启后数据仍存在
- [ ] 文档已更新
- [ ] 配置示例已更新

**回滚计划**:
如果出现问题，可以回滚到 commit `2118ee6`（重构前的版本）
