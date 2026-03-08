# 移除假依赖数据实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 移除知识库 Chunk 中的假依赖数据（`DependsOn` 字段和硬编码的 `inferDependencies` 函数），简化 dependency chunk 的生成逻辑，并为未来支持真实依赖查询预留扩展点（添加 `Endpoint.Deprecated` 字段）。

**Background:** 当前 `Chunk.DependsOn` 字段仅包含硬编码的测试数据，不反映真实的 API 依赖关系。`inferDependencies()` 函数只是简单地根据路径关键词返回固定值，对实际使用没有价值。由于系统当前不支持依赖查询功能，保留这些假数据会造成误导。

**Architecture:**
- 删除 `Chunk.DependsOn` 字段（保留 `Task.DependsOn`，因为它用于 Agent 任务编排）
- 删除 `inferDependencies()` 函数
- 简化 dependency chunk 的内容生成，改为通用提示文本
- 添加 `Endpoint.Deprecated` 字段，为未来支持废弃接口标注做准备
- 更新相关测试，移除对假依赖数据的断言

**Tech Stack:** Go、现有测试框架

---

## Task 1: 添加 Endpoint.Deprecated 字段并更新解析器

**Files:**
- Modify: `internal/knowledge/models.go`
- Modify: `internal/knowledge/swagger_parser.go`
- Modify: `internal/knowledge/swagger_parser_test.go`

**Step 1: 在 Endpoint 模型中添加 Deprecated 字段**
- 在 `internal/knowledge/models.go` 的 `Endpoint` 结构体中添加 `Deprecated bool` 字段
- 位置：在 `Tags []string` 之后

**Step 2: 更新 Swagger 解析器**
- 在 `internal/knowledge/swagger_parser.go` 中的 `swaggerOperation` 结构体添加 `Deprecated bool` 字段
- 在 `parseEndpoints()` 函数中解析 `deprecated` 属性并赋值给 `Endpoint.Deprecated`

**Step 3: 添加测试用例**
- 在 `internal/knowledge/swagger_parser_test.go` 中添加测试，验证 `deprecated: true` 的接口被正确解析
- 创建包含废弃接口的测试 Swagger 文档片段

**Step 4: 运行测试**
- Run: `go test ./internal/knowledge -v -run TestParseSwagger`
- Expected: 全绿

---

## Task 2: 移除 Chunk.DependsOn 字段

**Files:**
- Modify: `internal/knowledge/models.go`
- Modify: `internal/knowledge/ingestor.go`
- Modify: `internal/rag/chunker.go`

**Step 1: 从 Chunk 结构体中删除 DependsOn 字段**
- 在 `internal/knowledge/models.go` 中删除 `Chunk` 结构体的 `DependsOn []string` 字段（第 110 行）

**Step 2: 删除 inferDependencies 函数**
- 在 `internal/knowledge/ingestor.go` 中删除 `inferDependencies()` 函数（第 174-184 行）
- 在 `internal/rag/chunker.go` 中删除 `inferDependencies()` 函数（第 82-92 行）

**Step 3: 简化 dependency chunk 生成**
- 在 `internal/knowledge/ingestor.go` 的 `buildChunksForEndpoint()` 中：
  - 删除 `buildDependencyHint()` 函数调用
  - 删除 `DependsOn: inferDependencies(ep)` 赋值
  - 将 dependency chunk 的 `Content` 改为固定文本：`"接口依赖信息暂不可用"`

- 在 `internal/rag/chunker.go` 的 `buildChunksForEndpoint()` 中：
  - 删除 `deps := inferDependencies(ep.Path)` 调用
  - 删除 `DependsOn: deps` 赋值
  - 将 dependency chunk 的 `Content` 改为固定文本：`"接口依赖信息暂不可用"`
  - 删除 `if dependency.Content == ""` 的条件判断

**Step 4: 编译检查**
- Run: `go build ./...`
- Expected: 编译成功，无错误

---

## Task 3: 更新相关测试

**Files:**
- Modify: `internal/agent/planner_test.go`
- Modify: `internal/agent/adaptive_engine_test.go`
- Modify: `internal/knowledge/ingestor_test.go` (如果存在)

**Step 1: 检查测试中对 Chunk.DependsOn 的引用**
- Run: `grep -r "DependsOn" internal/ --include="*_test.go"`
- 识别所有测试文件中对 `Chunk.DependsOn` 的断言

**Step 2: 更新 planner_test.go**
- 在 `internal/agent/planner_test.go` 中，确认测试只检查 `Task.DependsOn`（这是合理的）
- 如果有对 `Chunk.DependsOn` 的断言，删除相关测试代码

**Step 3: 更新 adaptive_engine_test.go**
- 在 `internal/agent/adaptive_engine_test.go` 中，确认测试只使用 `Task.DependsOn`
- 保持现有测试不变（因为 `Task.DependsOn` 仍然存在）

**Step 4: 运行测试**
- Run: `go test ./internal/agent -v`
- Expected: 全绿

---

## Task 4: 更新文档和注释

**Files:**
- Modify: `docs/design.md`
- Modify: `CLAUDE.md`
- Modify: `internal/knowledge/models.go`

**Step 1: 更新设计文档**
- 在 `docs/design.md` 中搜索 "dependency" 或 "依赖"
- 更新相关章节，说明：
  - dependency chunk 类型仍然存在，但内容为占位文本
  - 系统当前不支持接口依赖查询
  - `Endpoint.Deprecated` 字段已添加，可用于标注废弃接口

**Step 2: 更新 CLAUDE.md**
- 在项目 CLAUDE.md 中的 "Key Layers" 部分更新 `internal/knowledge/` 的描述
- 说明 Swagger 解析器现在支持 `deprecated` 属性

**Step 3: 添加代码注释**
- 在 `internal/knowledge/models.go` 的 `Endpoint` 结构体上方添加注释：
  ```go
  // Deprecated 标记该接口是否已废弃（从 OpenAPI deprecated 属性解析）
  ```

**Step 4: 检查文档一致性**
- Run: `grep -r "DependsOn" docs/ README.md CLAUDE.md`
- 确保没有遗漏的文档引用

---

## Task 5: 全量验证

**Files:**
- Modify: 无

**Step 1: 运行全量测试**
- Run: `go test ./...`
- Expected: 所有测试通过

**Step 2: 运行构建检查**
- Run: `go build ./...`
- Expected: 编译成功

**Step 3: 检查最终 diff**
- Run: `git diff --stat`
- Run: `git diff internal/knowledge internal/rag internal/agent docs/`
- Expected:
  - 删除了 `Chunk.DependsOn` 字段
  - 删除了 `inferDependencies()` 函数
  - 添加了 `Endpoint.Deprecated` 字段
  - 简化了 dependency chunk 生成逻辑
  - 更新了相关文档

**Step 4: 验证运行时行为**
- Run: `AUTH_TOKEN=demo-token go run cmd/server/main.go run`
- 测试查询接口，确认返回结果中不再包含假的依赖信息
- 检查 dependency chunk 的内容是否为占位文本

---

## 验收标准

1. ✅ `Chunk.DependsOn` 字段已删除
2. ✅ `inferDependencies()` 函数已删除
3. ✅ `Endpoint.Deprecated` 字段已添加并可从 OpenAPI 规范解析
4. ✅ dependency chunk 生成逻辑已简化，使用占位文本
5. ✅ 所有测试通过
6. ✅ 文档已更新，反映当前实现状态
7. ✅ `Task.DependsOn` 保持不变（用于 Agent 任务编排）

---

## 未来扩展点

完成本计划后，如果需要支持真实的接口依赖查询，可以：

1. **实现智能依赖推断**：
   - 分析请求参数中的 ID 引用（如 `userId` 可能依赖用户创建接口）
   - 分析响应模型的关联关系
   - 支持通过配置文件定义业务依赖规则

2. **添加依赖查询工具**：
   - 新增 `analyze_dependencies` 工具
   - 支持查询某个接口依赖哪些其他接口
   - 支持查询哪些接口依赖某个接口（反向依赖）

3. **利用 Deprecated 字段**：
   - 在搜索结果中标注废弃接口
   - 在生成示例时添加废弃警告
   - 提供废弃接口迁移建议

4. **从外部数据源导入依赖关系**：
   - 支持从配置文件导入依赖关系
   - 支持从 API 网关配置推断依赖
   - 支持从调用链追踪数据分析依赖
