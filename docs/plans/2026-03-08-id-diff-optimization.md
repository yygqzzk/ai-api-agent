# ID Diff 增量更新 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将知识库写入路径从 Milvus 全量删除+重建改为基于 chunk ID 的增量删除与幂等 Upsert，降低写放大并消除查询中断窗口。

**Architecture:** 保持 Redis 侧 `RedisIngestor.UpsertDocument` 的全量替换语义不变，仅把向量侧改为“先读取旧 chunk IDs、计算 removedIDs、按主键删除、再 Upsert 新 chunks”。`KnowledgeBase` 负责驱动差异计算，`rag.Store` 与 `store.MilvusClient` 扩展 `DeleteByIDs`，并给 Ingestor 增加 `ChunkIDs` 供 Redis / 内存实现读取旧状态。

**Tech Stack:** Go、Redis 抽象客户端、Milvus Go SDK、现有 `testing` 单元测试。

### Task 1: 为差异计算补齐失败测试

**Files:**
- Modify: `internal/tools/knowledge_base_cache_test.go`
- Create: `internal/tools/knowledge_base_test.go`

**Step 1: 写 `subtract` 的失败测试**
- 覆盖“部分删除 / 无删除 / 全删除”。

**Step 2: 写 `KnowledgeBase.upsertDocument` 的失败测试**
- 先导入两条 endpoint，再导入缩减后的文档。
- 断言向量存储只删除消失的 chunk，保留仍存在的 chunk。
- 断言不再调用 `DeleteByService`。

**Step 3: 运行定向测试确认 RED**
- Run: `go test ./internal/tools -run 'TestSubtract|TestKnowledgeBaseUpsertDocument' -v`
- Expected: 因缺少 `DeleteByIDs` / `ChunkIDs` / 差异逻辑而失败。

### Task 2: 扩展接口与基础实现

**Files:**
- Modify: `internal/rag/rag_store.go`
- Modify: `internal/rag/search.go`
- Modify: `internal/rag/store.go`
- Modify: `internal/rag/milvus_store.go`
- Modify: `internal/knowledge/ingestor.go`
- Modify: `internal/knowledge/redis_ingestor.go`
- Modify: `internal/store/milvus_client.go`
- Modify: `internal/store/milvus_sdk_client.go`
- Modify: `cmd/server/health_test.go`

**Step 1: 为 `Ingestor` 增加 `ChunkIDs(service)`**
- 内存与 Redis 实现都返回指定 service 当前 chunk ID 列表。

**Step 2: 为 `rag.Store` / `MilvusClient` 增加 `DeleteByIDs`**
- `MemoryStore` / `MilvusStore` / `RerankStore` / `InMemoryMilvusClient` / `SDKMilvusClient` 全部补齐实现。
- 更新所有测试桩以满足新接口。

**Step 3: 运行相关包测试确认接口层 GREEN**
- Run: `go test ./internal/knowledge ./internal/rag ./internal/store ./cmd/server -run 'Test|^$'`

### Task 3: 实现 KnowledgeBase ID Diff 写入逻辑

**Files:**
- Modify: `internal/tools/knowledge_base.go`
- Modify: `internal/rag/search.go`

**Step 1: 抽取差异辅助函数**
- `subtract(oldIDs, newIDs)`
- 直接复用 `rag.BuildChunks` 生成新 chunks，避免重复 chunk 构建逻辑。

**Step 2: 改造 `upsertDocument`**
- 读取 service 与旧 IDs。
- `oldIDs == nil` 时降级到 `DeleteByService`。
- 否则只删除 `removedIDs`，再 `Upsert`/`Index` 新 chunks。
- 保持 Redis `UpsertDocument` 仍为最后一步。

**Step 3: 运行定向测试确认 GREEN**
- Run: `go test ./internal/tools -run 'TestSubtract|TestKnowledgeBaseUpsertDocument' -v`

### Task 4: 补充回归测试

**Files:**
- Modify: `internal/knowledge/redis_ingestor_test.go`
- Modify: `internal/rag/store_test.go`（如不存在则创建）

**Step 1: 为 `RedisIngestor.ChunkIDs` 添加测试**
- 断言 service 大小写归一化正确，且返回稳定 ID 集合。

**Step 2: 为 `DeleteByIDs` 添加存储级测试**
- `MemoryStore` 或 `InMemoryMilvusClient` 插入 4 条后删除其中 2 条，验证剩余结果。

**Step 3: 运行相关测试**
- Run: `go test ./internal/knowledge ./internal/rag ./internal/store -v`

### Task 5: 全量验证与收尾

**Files:**
- Modify: `docs/id-diff-optimization.md`（仅在实现与文档偏差需要回填时）

**Step 1: 运行目标回归测试**
- Run: `go test ./...`
- Expected: 所有现有测试通过。

**Step 2: 检查代码格式**
- Run: `gofmt -w <modified files>`

**Step 3: 总结风险与验证结果**
- 记录是否触发降级路径、哪些测试覆盖了 ID Diff 逻辑。
