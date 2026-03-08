# Redis 持久化知识库重构设计

**日期**: 2026-03-08
**状态**: 设计阶段
**作者**: Claude Code

## 1. 背景与目标

### 当前问题

当前系统使用 `InMemoryIngestor` 存储知识库核心数据（endpoints、specs、chunks），导致：
- 服务重启后数据全部丢失
- 需要重新解析和导入所有 Swagger 文档
- 无法实现多实例数据共享

虽然项目已集成 Redis，但仅用于缓存单个 endpoint 详情，核心数据仍在内存中。

### 重构目标

1. **数据持久化**：将 endpoints、specs、chunks 存储到 Redis
2. **移除内存模式**：强制使用真实 Redis，去除 `InMemoryRedisClient`
3. **保持接口兼容**：对上层 `KnowledgeBase` 透明
4. **支持测试**：保留内存实现用于单元测试

## 2. 架构设计

### 2.1 架构变更

**Before**:
```
KnowledgeBase
  ├─ InMemoryIngestor (内存存储)
  ├─ RAG Engine (Milvus/Memory)
  └─ RedisClient (仅缓存 endpoint 详情)
```

**After**:
```
KnowledgeBase
  ├─ RedisIngestor (Redis 持久化)
  ├─ RAG Engine (Milvus)
  └─ [RedisClient 合并到 RedisIngestor]
```

### 2.2 关键决策

- `RedisIngestor` 直接持有 `RedisClient`，不再通过 `KnowledgeBase.cache` 字段
- 移除 `InMemoryRedisClient`，只保留 `GoRedisClient`
- 移除 `NewKnowledgeBase()` 的默认内存模式，强制传入 Redis 配置
- 保留 `InMemoryIngestor` 仅用于单元测试（通过构造函数注入）

## 3. 组件设计

### 3.1 RedisIngestor 结构

```go
type RedisIngestor struct {
    client  store.RedisClient
    mu      sync.RWMutex  // 保护本地缓存
    cache   *ingestorCache // 可选的本地缓存层
}

type ingestorCache struct {
    endpoints map[string][]Endpoint  // service -> endpoints
    specs     map[string]SpecMeta    // service -> meta
    chunks    map[string][]Chunk     // service -> chunks
}
```

### 3.2 Redis Key 设计

| Key Pattern | Type | Description | Example |
|-------------|------|-------------|---------|
| `kb:endpoints:{service}` | Hash | 存储 service 的所有 endpoints | field=`GET:/user/login`, value=Endpoint JSON |
| `kb:specs:{service}` | String | 存储 service 的 SpecMeta | SpecMeta JSON |
| `kb:chunks:{service}` | List | 存储 service 的所有 chunks | [Chunk JSON, ...] |
| `kb:services` | Set | 存储所有 service 名称 | {petstore, user-service, ...} |

**Key 命名规范**：
- 统一前缀 `kb:` (knowledge base)
- service 名称统一小写（`strings.ToLower`）
- endpoint field 格式：`{METHOD}:{path}`（如 `GET:/user/login`）

### 3.3 接口实现

```go
// Ingestor 接口（保持不变）
type Ingestor interface {
    UpsertDocument(doc ParsedSpec) IngestStats
    Endpoints() []Endpoint
    SpecMeta(service string) (SpecMeta, bool)
    Chunks() []Chunk
}

// RedisIngestor 实现
func (r *RedisIngestor) UpsertDocument(doc ParsedSpec) IngestStats {
    // 1. 写入 endpoints 到 Hash
    // 2. 写入 specs 到 String
    // 3. 重建 chunks 并写入 List
    // 4. 更新 services Set
    // 5. 更新本地缓存（如果启用）
}

func (r *RedisIngestor) Endpoints() []Endpoint {
    // 1. 从 kb:services 获取所有 service
    // 2. 对每个 service HGETALL kb:endpoints:{service}
    // 3. 反序列化并合并结果
}

func (r *RedisIngestor) SpecMeta(service string) (SpecMeta, bool) {
    // GET kb:specs:{service}
    // 反序列化 JSON
}

func (r *RedisIngestor) Chunks() []Chunk {
    // 1. 从 kb:services 获取所有 service
    // 2. 对每个 service LRANGE kb:chunks:{service} 0 -1
    // 3. 反序列化并合并结果
}
```

### 3.4 本地缓存层（可选）

为了减少 Redis 查询，可以在 `RedisIngestor` 内部维护一个本地缓存：
- 写入时同步更新缓存
- 读取时优先从缓存读取
- 缓存失效策略：TTL 或手动刷新

**权衡**：
- 优点：减少 Redis 查询，提升读取性能
- 缺点：增加内存占用，多实例时缓存不一致

**建议**：初期不实现，后续根据性能测试结果决定。

## 4. 数据流

### 4.1 写入流程

```
IngestFile/IngestBytes/IngestURL
  ↓
ParseSwaggerDocument
  ↓
RedisIngestor.UpsertDocument
  ↓
┌─────────────────────────────────┐
│ 1. HSET kb:endpoints:{service}  │
│    field={method}:{path}        │
│    value=Endpoint JSON          │
├─────────────────────────────────┤
│ 2. SET kb:specs:{service}       │
│    value=SpecMeta JSON          │
├─────────────────────────────────┤
│ 3. DEL kb:chunks:{service}      │
│    RPUSH kb:chunks:{service}    │
│    value=Chunk JSON...          │
├─────────────────────────────────┤
│ 4. SADD kb:services {service}   │
└─────────────────────────────────┘
  ↓
RAG Engine.Index (Milvus)
```

### 4.2 读取流程

**GetEndpoint(service, endpoint)**:
```
HGET kb:endpoints:{service} {method}:{path}
  ↓
反序列化 JSON
  ↓
返回 Endpoint
```

**Endpoints()**:
```
SMEMBERS kb:services
  ↓
对每个 service:
  HGETALL kb:endpoints:{service}
  ↓
  反序列化 JSON
  ↓
合并所有结果
  ↓
返回 []Endpoint
```

**SpecMeta(service)**:
```
GET kb:specs:{service}
  ↓
反序列化 JSON
  ↓
返回 SpecMeta
```

## 5. 错误处理

### 5.1 启动时检查

```go
func main() {
    // 创建 Redis 客户端
    redisClient, err := store.NewRedisClient(store.RedisOptions{
        Mode:     "redis",
        Address:  os.Getenv("REDIS_ADDRESS"),
        Password: os.Getenv("REDIS_PASSWORD"),
    })
    if err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
    }

    // Ping 检查连接
    if err := redisClient.Ping(context.Background()); err != nil {
        log.Fatalf("Redis ping failed: %v", err)
    }

    log.Printf("Connected to Redis at %s", os.Getenv("REDIS_ADDRESS"))
}
```

**Fail-fast 原则**：
- 服务启动时必须连接到 Redis
- 连接失败则立即退出，不启动服务
- 记录详细的错误信息和连接参数

### 5.2 运行时降级

**Redis 读取失败**：
- 返回错误，不降级到内存
- 记录错误日志，触发告警
- 使用 circuit breaker 保护 Redis 调用

**Redis 写入失败**：
- 返回错误，不继续写入 Milvus
- 记录错误日志，触发告警
- 考虑重试机制（指数退避）

**为什么不降级到内存？**
- 保证数据一致性：避免部分数据在 Redis，部分在内存
- 明确失败：让调用方知道操作失败，而不是静默降级
- 简化逻辑：不需要维护双写和同步逻辑

### 5.3 数据一致性

**写入顺序**：
1. 先写 Redis（持久化层）
2. 再写 Milvus（索引层）

**失败处理**：
- Redis 写入失败 → 返回错误，不写 Milvus
- Milvus 写入失败 → 记录错误，但不回滚 Redis（最终一致性）

**原因**：
- Redis 是 source of truth，必须先成功
- Milvus 是索引，可以后续重建
- 避免分布式事务的复杂性

## 6. 配置变更

### 6.1 环境变量

新增必需的环境变量：
```bash
# Redis 配置（必需）
REDIS_ADDRESS=127.0.0.1:6379
REDIS_PASSWORD=
REDIS_DB=0
```

### 6.2 配置文件

`config/config.yaml`:
```yaml
redis:
  address: ${REDIS_ADDRESS:127.0.0.1:6379}
  password: ${REDIS_PASSWORD:}
  db: ${REDIS_DB:0}
```

### 6.3 启动检查

服务启动时检查必需的环境变量：
```go
if os.Getenv("REDIS_ADDRESS") == "" {
    log.Fatal("REDIS_ADDRESS is required")
}
```

## 7. 测试策略

### 7.1 单元测试

**使用 miniredis 模拟 Redis**：
```go
func TestRedisIngestor(t *testing.T) {
    // 启动 miniredis
    s, err := miniredis.Run()
    require.NoError(t, err)
    defer s.Close()

    // 创建 Redis 客户端
    client, err := store.NewRedisClient(store.RedisOptions{
        Mode:    "redis",
        Address: s.Addr(),
    })
    require.NoError(t, err)

    // 测试 RedisIngestor
    ingestor := knowledge.NewRedisIngestor(client)
    // ...
}
```

**保留 InMemoryIngestor 用于快速测试**：
```go
func TestKnowledgeBase_WithMemory(t *testing.T) {
    ingestor := knowledge.NewInMemoryIngestor()
    kb := tools.NewKnowledgeBaseWithIngestor(ingestor, ragStore)
    // ...
}
```

### 7.2 集成测试

**使用真实 Redis**：
```go
func TestRedisIngestor_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // 连接真实 Redis
    client, err := store.NewRedisClient(store.RedisOptions{
        Mode:    "redis",
        Address: "127.0.0.1:6379",
    })
    require.NoError(t, err)
    defer client.Close(context.Background())

    // 测试完整流程
    // ...
}
```

**测试场景**：
1. 写入数据 → 读取验证
2. 重启服务 → 数据仍存在
3. 多次 upsert → 数据正确更新
4. 大量数据 → 性能可接受

### 7.3 性能测试

**基准测试**：
```go
func BenchmarkRedisIngestor_UpsertDocument(b *testing.B) {
    // 测试写入性能
}

func BenchmarkRedisIngestor_Endpoints(b *testing.B) {
    // 测试读取性能
}
```

**性能目标**：
- 单个 endpoint 写入 < 10ms
- 读取所有 endpoints (1000个) < 100ms
- 内存占用 < 100MB（不含 Milvus）

## 8. 迁移计划

### 8.1 向后兼容

**构造函数变更**：
```go
// 旧的（废弃）
func NewKnowledgeBase() *KnowledgeBase {
    // 使用内存模式
}

// 新的（推荐）
func NewKnowledgeBaseWithRedis(redisClient store.RedisClient, ragStore rag.Store) *KnowledgeBase {
    ingestor := knowledge.NewRedisIngestor(redisClient)
    return &KnowledgeBase{
        ingestor: ingestor,
        engine:   rag.NewEngine(ragStore),
    }
}

// 测试用（可选）
func NewKnowledgeBaseWithIngestor(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
    return &KnowledgeBase{
        ingestor: ingestor,
        engine:   rag.NewEngine(ragStore),
    }
}
```

### 8.2 数据迁移

**首次启动**：
- Redis 为空，需要重新导入数据
- 可以通过 `/webhook/sync` 或 `parse_swagger` 工具导入

**迁移脚本**（可选）：
```bash
# 导出内存数据（如果有）
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -d '{"jsonrpc":"2.0","id":1,"method":"export_knowledge_base"}'

# 导入到 Redis
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer demo-token' \
  -d '{"jsonrpc":"2.0","id":2,"method":"import_knowledge_base","params":{...}}'
```

### 8.3 回滚计划

如果 Redis 持久化出现问题，可以快速回滚：
1. 恢复旧版本代码（使用 `InMemoryIngestor`）
2. 重新导入数据
3. 修复 Redis 问题后再次迁移

## 9. 监控与告警

### 9.1 关键指标

**Redis 连接**：
- `redis_connection_status` - 连接状态（0=断开，1=正常）
- `redis_ping_latency_ms` - Ping 延迟

**数据操作**：
- `kb_upsert_total` - 写入次数
- `kb_upsert_errors_total` - 写入失败次数
- `kb_read_total` - 读取次数
- `kb_read_errors_total` - 读取失败次数

**数据量**：
- `kb_endpoints_total` - 总 endpoint 数量
- `kb_services_total` - 总 service 数量
- `kb_chunks_total` - 总 chunk 数量

### 9.2 告警规则

```yaml
- alert: RedisConnectionDown
  expr: redis_connection_status == 0
  for: 1m
  annotations:
    summary: "Redis connection is down"

- alert: KnowledgeBaseWriteErrors
  expr: rate(kb_upsert_errors_total[5m]) > 0.1
  for: 5m
  annotations:
    summary: "High knowledge base write error rate"
```

## 10. 实施步骤

### Phase 1: 基础实现（2-3天）
1. 创建 `RedisIngestor` 结构和接口实现
2. 实现 Redis Key 设计和序列化逻辑
3. 编写单元测试（使用 miniredis）

### Phase 2: 集成与测试（1-2天）
4. 修改 `KnowledgeBase` 构造函数
5. 更新 `cmd/server/main.go` 启动逻辑
6. 编写集成测试（使用真实 Redis）
7. 性能基准测试

### Phase 3: 清理与文档（1天）
8. 移除 `InMemoryRedisClient`
9. 移除旧的 `NewKnowledgeBase()` 构造函数
10. 更新文档和配置示例
11. 添加监控指标

### Phase 4: 部署与验证（1天）
12. 部署到测试环境
13. 验证数据持久化
14. 性能测试和调优
15. 生产环境部署

**总计**: 5-7 个工作日

## 11. 风险与缓解

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|----------|
| Redis 性能不足 | 高 | 低 | 性能测试，添加本地缓存 |
| 数据序列化开销大 | 中 | 中 | 使用 msgpack 替代 JSON |
| Redis 连接不稳定 | 高 | 低 | Circuit breaker，重试机制 |
| 数据迁移失败 | 高 | 低 | 保留旧代码，快速回滚 |
| 内存占用增加 | 中 | 中 | 监控内存，优化数据结构 |

## 12. 后续优化

1. **本地缓存层**：减少 Redis 查询，提升读取性能
2. **数据压缩**：使用 gzip 压缩 JSON，减少 Redis 内存占用
3. **批量操作**：使用 Pipeline 批量写入，提升写入性能
4. **TTL 管理**：为旧数据设置 TTL，自动清理
5. **多实例支持**：使用 Redis Pub/Sub 同步缓存失效

## 13. 参考资料

- [Redis Hash 文档](https://redis.io/docs/data-types/hashes/)
- [go-redis 使用指南](https://redis.uptrace.dev/)
- [miniredis 测试库](https://github.com/alicebob/miniredis)
- 项目文档：`docs/design.md`
