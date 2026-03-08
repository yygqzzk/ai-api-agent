# Released API Spec Metadata Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让 tag 触发的 API 文档同步在入库前注入正确的 `host` / `basePath` / `schemes`，并让知识库保存文档级元数据，供 `generate_example` 等工具输出真实可调用 URL。

**Architecture:** 保持现有 endpoint 级索引不变，在 `knowledge` 层新增文档级 `SpecMeta` 与 `ParsedSpec`，由 `KnowledgeBase` 在导入阶段同时保存 endpoint 与 spec 元数据。GitHub Actions 在 tag 同步前读取文档内容并根据 CI 提供的 overrides 注入元数据，再通过 `/webhook/sync` 发送给服务端。`generate_example` 改为从 `service` 绑定的元数据拼接请求 URL，不再硬编码 petstore 域名。

**Tech Stack:** Go、GitHub Actions、Ruby（workflow 内置 JSON/YAML 处理）、现有内存 ingestor / Redis cache。

### Task 1: 补充失败测试，定义新行为

**Files:**
- Modify: `internal/knowledge/swagger_parser_test.go`
- Modify: `internal/tools/tools_test.go`
- Modify: `internal/tools/knowledge_base_cache_test.go`

**Step 1: 写解析元数据失败测试**
- 为 `petstore.json` 断言解析结果包含 `host=petstore.swagger.io`、`basePath=/v2`、`schemes=[https]`。
- 为 `SpecMeta.URLForPath("/user/login")` 断言输出 `https://petstore.swagger.io/v2/user/login`。

**Step 2: 写知识库存储失败测试**
- 导入 petstore 后断言 `KnowledgeBase.GetSpecMeta("petstore")` 可命中。
- 断言服务名查找大小写不敏感。

**Step 3: 写示例生成失败测试**
- 调用 `generate_example`，断言代码中包含 `https://petstore.swagger.io/v2`，且不再依赖硬编码常量。

**Step 4: 运行局部测试确认失败**
- Run: `go test ./internal/knowledge ./internal/tools -run 'Test(ParseSwagger|GenerateExample|GetSpecMeta)' -v`
- Expected: 编译或断言失败，说明新行为尚未实现。

### Task 2: 在 knowledge 层引入文档级 SpecMeta

**Files:**
- Modify: `internal/knowledge/models.go`
- Modify: `internal/knowledge/swagger_parser.go`
- Modify: `internal/knowledge/ingestor.go`

**Step 1: 新增模型**
- 在 `models.go` 增加 `SpecMeta`、`ParsedSpec`，字段至少包含 `Service`、`Title`、`Version`、`Host`、`BasePath`、`Schemes`。
- 给 `SpecMeta` 增加 URL 拼接辅助方法，统一处理 scheme 默认值和 path join。

**Step 2: 新增带元数据的解析函数**
- 在 `swagger_parser.go` 中给 `swaggerDoc` 增加 `Host` / `BasePath` / `Schemes` 字段。
- 新增 `ParseSwaggerDocumentBytes` / `ParseSwaggerDocumentFile` 返回 `ParsedSpec`。
- 保留现有 `ParseSwaggerBytes` / `ParseSwaggerFile` 作为兼容包装，内部复用新解析函数。

**Step 3: 存储 SpecMeta**
- 在 `InMemoryIngestor` 中增加 `specs map[string]SpecMeta`。
- 新增 `UpsertDocument` 与 `SpecMeta(service string)` 方法；保留 `Upsert([]Endpoint)` 兼容旧调用。

**Step 4: 运行局部测试**
- Run: `go test ./internal/knowledge -v`
- Expected: 全绿。

### Task 3: 接入 KnowledgeBase 与工具层

**Files:**
- Modify: `internal/tools/knowledge_base.go`
- Modify: `internal/tools/generate_example.go`
- Modify: `internal/tools/get_api_detail.go`
- Modify: `internal/tools/types.go`

**Step 1: KnowledgeBase 导入时保存 SpecMeta**
- `IngestFile` / `IngestBytes` / `IngestURL` 改为使用 `ParsedSpec`。
- 新增 `GetSpecMeta(service string)` 供工具层读取。

**Step 2: get_api_detail 暴露元数据**
- 在返回结构中增加 `Spec` 字段，方便调用方直接看到 host/basePath/schemes。

**Step 3: generate_example 使用元数据拼 URL**
- 用 `SpecMeta.URLForPath(ep.Path)` 生成请求地址。
- 当缺少 host 时退化为相对路径，而不是虚构域名。

**Step 4: 运行局部测试**
- Run: `go test ./internal/tools -v`
- Expected: 全绿。

### Task 4: 改造 tag 同步工作流，在上传前注入元数据

**Files:**
- Modify: `.github/workflows/sync-api-docs.yml`
- Modify: `README.md`

**Step 1: 定义 overrides 输入**
- 在 workflow step env 中读取 `API_DOC_META_OVERRIDES_JSON`。
- 约定 JSON 结构支持 `default` 和按 service 名的覆盖，例如：
  `{ "default": {"schemes": ["https"]}, "user-service": {"host": "api.example.com", "basePath": "/user"} }`

**Step 2: 在同步前注入并标准化内容**
- 用 Ruby 读取 `docs/api/*.(json|yaml|yml)`。
- 合并原文档和 overrides 中的 `host` / `basePath` / `schemes`。
- 统一输出 JSON 字符串后再通过 webhook 上传。

**Step 3: 更新 README**
- 说明该 workflow 仅在 tag push 时触发。
- 说明如何配置 `API_DOC_META_OVERRIDES_JSON`。

**Step 4: 校验 workflow**
- Run: `ruby -e 'require "yaml"; YAML.load_file(".github/workflows/sync-api-docs.yml"); puts "YAML OK"'`
- Expected: `YAML OK`

### Task 5: 全量验证

**Files:**
- Modify: 无

**Step 1: 运行目标测试集**
- Run: `go test ./internal/knowledge ./internal/tools ./internal/ingest ./internal/webhook -v`
- Expected: 全绿。

**Step 2: 运行全量测试**
- Run: `go test ./...`
- Expected: 仓库现有测试通过；若有无关失败，单独记录。

**Step 3: 检查最终 diff**
- Run: `git diff --stat && git diff -- .github/workflows/sync-api-docs.yml internal/knowledge internal/tools README.md`
- Expected: 仅包含本次需求相关改动。
