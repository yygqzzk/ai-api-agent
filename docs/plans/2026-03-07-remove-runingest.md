# Remove runIngest Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 删除 `runIngest` CLI 子命令，并同步更新当前项目中所有相关命令、注释与文档，避免出现失效入口或误导说明。

**Architecture:** 该改动只收口命令入口与文档，不改变服务运行期的知识库、MCP 工具或 webhook 导入能力。实现上先删除 `cmd/server/main.go` 中的 `ingest` 分支和 `runIngest` 函数，再更新 `Makefile`、README、Quickstart、本地部署指南以及 agent 说明文档，把导入路径统一为“服务启动自动加载默认 petstore”与“运行中通过 `parse_swagger` / `/webhook/sync` 导入”。

**Tech Stack:** Go、Makefile、Markdown 文档

### Task 1: 删除 CLI ingest 代码入口

**Files:**
- Modify: `cmd/server/main.go`

**Step 1: 删除 `main` 中的 `ingest` 分支**

删除 `switch cmd` 里的 `case "ingest"`，保留 `run` 为唯一支持的子命令。

**Step 2: 删除 `runIngest` 函数实现**

移除 `runIngest(args []string, cfg config.Config) error` 及其相关注释。

**Step 3: 清理未使用 import**

删除 `flag` 等仅由 `runIngest` 使用的 import，确保 `go build` 不报 unused。

**Step 4: 运行构建检查**

Run: `go build ./...`
Expected: 构建通过，无 unused import 或未定义符号错误。

### Task 2: 删除失效命令入口

**Files:**
- Modify: `Makefile`

**Step 1: 删除 `ingest` 目标**

移除 `make ingest` 规则，保留 `dev`、`run`、`test`。

**Step 2: 检查命令引用是否仍存在**

Run: `rg -n "make ingest|go run cmd/server/main.go ingest|api-assistant ingest" README.md QUICKSTART.md AGENTS.md CLAUDE.md docs Makefile cmd`
Expected: 只剩将要继续修改的文档命中，代码与 Makefile 中不再保留命令入口。

### Task 3: 更新用户文档

**Files:**
- Modify: `README.md`
- Modify: `QUICKSTART.md`
- Modify: `docs/local-setup-guide.md`
- Modify: `docs/design.md`

**Step 1: 改写项目结构和快速开始**

把“`cmd/server/main.go`：服务入口与 `ingest` 子命令”改成纯服务入口描述；把手动 CLI 导入步骤改成“默认自动加载 `testdata/petstore.json`”。

**Step 2: 改写自定义导入说明**

把所有 `go run cmd/server/main.go ingest ...` 替换成运行中调用 `parse_swagger` 或 `/webhook/sync` 的说明。

**Step 3: 改写排障和设计文档**

将“无结果时执行 `make ingest`”改成“确认默认数据已加载，或通过 `parse_swagger` / `/webhook/sync` 导入”。

### Task 4: 更新开发代理说明

**Files:**
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`

**Step 1: 更新 Build & Test Commands**

删除 `make ingest` 说明，避免 agent 推荐失效命令。

**Step 2: 更新 Testing / Troubleshooting 章节**

用服务启动默认导入与在线导入方式替代 `ingest` 子命令描述。

### Task 5: 验证改动完整性

**Files:**
- Test: `cmd/server/main.go`
- Test: `Makefile`
- Test: `README.md`
- Test: `QUICKSTART.md`
- Test: `docs/local-setup-guide.md`
- Test: `docs/design.md`
- Test: `AGENTS.md`
- Test: `CLAUDE.md`

**Step 1: 运行针对性搜索**

Run: `rg -n "make ingest|go run cmd/server/main.go ingest|api-assistant ingest|ingest 子命令" README.md QUICKSTART.md AGENTS.md CLAUDE.md docs Makefile cmd`
Expected: 不再出现删除后的命令描述；若仍保留 `parse_swagger` / `/webhook/sync` 说明则属预期。

**Step 2: 运行测试和构建**

Run: `go test ./...`
Expected: 现有测试通过。

Run: `go build ./...`
Expected: 构建通过。
