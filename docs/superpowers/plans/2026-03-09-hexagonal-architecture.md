# 万知六边形架构重构实施计划

> **For Claude:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将万知从当前的内聚分层架构重构为严格的六边形架构，实现核心业务层零外部依赖（`domain/`），基础设施通过接口隔离（`infra/`），协议适配独立（`transport/`）。

**Architecture:** 采用端口和适配器模式，分层为：domain（核心业务逻辑，零外部依赖）、infra（外部服务适配器，实现 domain 定义的接口）、transport（协议适配器，调用 domain）。依赖方向：transport → domain ← infra。

**Tech Stack:** Go 1.25, Gin, Milvus, Redis, OpenAI-compatible LLM

---

## Chunk 1: 创建目录结构与抽取 domain/model

### Task 1: 创建六边形目录结构

**Files:**
- Create: `internal/domain/model/`
- Create: `internal/domain/agent/`
- Create: `internal/domain/knowledge/`
- Create: `internal/domain/rag/`
- Create: `internal/domain/tool/`
- Create: `internal/infra/llm/`
- Create: `internal/infra/milvus/`
- Create: `internal/infra/redis/`
- Create: `internal/infra/embedding/`
- Create: `internal/infra/rerank/`

- [ ] **Step 1: 创建 domain 层目录**

Run:
```bash
mkdir -p internal/domain/{model,agent,knowledge,rag,tool}
```
Expected: 创建成功，无错误

- [ ] **Step 2: 创建 infra 层目录**

Run:
```bash
mkdir -p internal/infra/{llm,milvus,redis,embedding,rerank}
```
Expected: 创建成功，无错误

- [ ] **Step 3: 验证目录结构**

Run:
```bash
ls -la internal/domain/
ls -la internal/infra/
```
Expected: 看到所有子目录已创建

---

### Task 2: 抽取 domain/model/endpoint.go

**Files:**
- Create: `internal/domain/model/endpoint.go`
- Reference: `internal/knowledge/models.go:10-42`

- [ ] **Step 1: 创建 domain/model/endpoint.go**

```go
package model

import "fmt"

// Endpoint 表示一个 API 接口端点
// Deprecated 标记该接口是否已废弃（从 OpenAPI deprecated 属性解析）
type Endpoint struct {
	Service     string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Parameters  []Parameter
	Responses   []Response
}

// Key 返回端点的唯一标识符
func (e Endpoint) Key() string {
	return fmt.Sprintf("%s:%s:%s", e.Service, e.Method, e.Path)
}

// DisplayName 返回端点的显示名称
func (e Endpoint) DisplayName() string {
	return fmt.Sprintf("%s %s", e.Method, e.Path)
}

// Parameter 表示接口参数
type Parameter struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Description string
	SchemaRef   string
}

// Response 表示接口响应
type Response struct {
	StatusCode  string
	Description string
}
```

---

### Task 3: 抽取 domain/model/chunk.go

**Files:**
- Create: `internal/domain/model/chunk.go`
- Reference: `internal/knowledge/models.go:105-112`

- [ ] **Step 1: 创建 domain/model/chunk.go**

```go
package model

// Chunk 表示文档的一个语义分块
type Chunk struct {
	ID       string
	Service  string
	Endpoint string
	Type     string // ChunkType 的字符串表示
	Content  string
	Version  string
}

// ChunkType 分块类型常量
const (
	ChunkTypeOverview   = "overview"
	ChunkTypeRequest    = "request"
	ChunkTypeResponse   = "response"
	ChunkTypeDependency = "dependency"
)
```

---

### Task 4: 抽取 domain/model/spec.go

**Files:**
- Create: `internal/domain/model/spec.go`
- Reference: `internal/knowledge/models.go:44-117`

- [ ] **Step 1: 创建 domain/model/spec.go**

```go
package model

import (
	"fmt"
	"path"
	"strings"
)

// SpecMeta 表示 API 规范的元数据
type SpecMeta struct {
	Service  string   `json:"service"`
	Title    string   `json:"title,omitempty"`
	Version  string   `json:"version,omitempty"`
	Host     string   `json:"host,omitempty"`
	BasePath string   `json:"base_path,omitempty"`
	Schemes  []string `json:"schemes,omitempty"`
}

// URLForPath 生成指定路径的完整 URL
func (m SpecMeta) URLForPath(endpointPath string) string {
	fullPath := joinURLPath(m.BasePath, endpointPath)
	host := strings.TrimSpace(m.Host)
	if host == "" {
		return fullPath
	}
	scheme := "https"
	for _, item := range m.Schemes {
		candidate := strings.TrimSpace(item)
		if candidate != "" {
			scheme = candidate
			break
		}
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, fullPath)
}

func joinURLPath(basePath string, endpointPath string) string {
	base := normalizePathPrefix(basePath)
	endpoint := normalizePathPrefix(endpointPath)
	switch {
	case base == "" && endpoint == "":
		return "/"
	case base == "":
		return endpoint
	case endpoint == "":
		return base
	default:
		joined := path.Join(base, endpoint)
		if !strings.HasPrefix(joined, "/") {
			joined = "/" + joined
		}
		return joined
	}
}

func normalizePathPrefix(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || v == "/" {
		return ""
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	return strings.TrimRight(v, "/")
}

// ParsedSpec 表示解析后的 API 规范
type ParsedSpec struct {
	Meta      SpecMeta
	Endpoints []Endpoint
}

// IngestStats 表示录入统计信息
type IngestStats struct {
	Endpoints int `json:"endpoints"`
	Chunks    int `json:"chunks"`
}
```

---

## Chunk 2: 移动 domain/agent 层

### Task 5: 创建 domain/agent/llm.go（仅接口定义）

**Files:**
- Create: `internal/domain/agent/llm.go`
- Reference: `internal/agent/llm.go:83-96`

- [ ] **Step 1: 创建 domain/agent/llm.go（接口定义）**

```go
package agent

import (
	"context"
	"encoding/json"
)

// Message 对话消息，遵循 OpenAI Chat Completion API 格式
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 工具调用请求
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolDefinition 工具定义，描述工具的功能和参数
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// LLMReply LLM 响应
type LLMReply struct {
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
}

// LLMClient LLM 客户端接口
// 抽象不同的 LLM 实现，支持运行时切换和降级
type LLMClient interface {
	Next(ctx context.Context, messages []Message, tools []ToolDefinition) (LLMReply, error)
}
```

---

### Task 6: 移动 Agent 引擎文件到 domain/agent/

**Files:**
- Move: `internal/agent/*.go` → `internal/domain/agent/`（排除 llm.go 和 openai_llm.go, rule_based_llm.go）

- [ ] **Step 1: 移动 Agent 引擎核心文件**

Run:
```bash
# 移动所有 Agent 文件，但排除 LLM 实现
cd internal/agent
for f in *.go; do
  case "$f" in
    openai_llm.go) continue ;;  # LLM 实现移到 infra
    *) cp "$f" ../domain/agent/ ;;
  esac
done
```
Expected: 所有非 LLM 实现文件已复制到 domain/agent/

- [ ] **Step 2: 更新 domain/agent/llm.go（补充 RuleBasedLLMClient 接口实现位置）**

由于 `RuleBasedLLMClient` 的实现逻辑较长，保留在原 `llm.go` 中作为临时方案，后续任务会将实现移到 `infra/llm/`。

---

## Chunk 3: 移动 domain/knowledge 层

### Task 7: 创建 domain/knowledge/ingestor.go（接口定义）

**Files:**
- Create: `internal/domain/knowledge/ingestor.go`
- Reference: `internal/knowledge/ingestor.go`

- [ ] **Step 1: 创建 Ingestor 接口定义**

```go
package knowledge

import (
	"context"
	"wanzhi/internal/domain/model"
)

// Ingestor 知识录入接口
// 抽象不同的持久化实现（Redis、内存等）
type Ingestor interface {
	// SaveSpec 保存 API 规范到持久化存储
	SaveSpec(ctx context.Context, service string, spec []byte) error

	// LoadSpec 从持久化存储加载 API 规范
	LoadSpec(ctx context.Context, service string) ([]byte, error)

	// DeleteService 删除指定服务的所有数据
	DeleteService(ctx context.Context, service string) error

	// ListEndpoints 列出指定服务的所有端点
	ListEndpoints(ctx context.Context, service string) ([]model.Endpoint, error)

	// SaveEndpoints 保存端点列表
	SaveEndpoints(ctx context.Context, service string, endpoints []model.Endpoint) error

	// SaveChunks 保存文档分块
	SaveChunks(ctx context.Context, service string, chunks []model.Chunk) error

	// LoadChunks 加载文档分块
	LoadChunks(ctx context.Context, service string) ([]model.Chunk, error)
}
```

---

### Task 8: 移动 Knowledge 文件到 domain/knowledge/

**Files:**
- Move: `internal/knowledge/swagger_parser.go` → `internal/domain/knowledge/swagger_parser.go`
- Move: `internal/knowledge/swagger_parser_test.go` → `internal/domain/knowledge/swagger_parser_test.go`
- Move: `internal/knowledge/redis_ingestor.go` → `internal/infra/redis/ingestor.go`
- Move: `internal/knowledge/redis_ingestor_test.go` → `internal/infra/redis/ingestor_test.go`
- Delete: `internal/knowledge/ingestor.go`（已被 domain/knowledge/ingestor.go 接口替代）

- [ ] **Step 1: 移动 Swagger 解析器**

Run:
```bash
mv internal/knowledge/swagger_parser.go internal/domain/knowledge/
mv internal/knowledge/swagger_parser_test.go internal/domain/knowledge/
```
Expected: 文件已移动

- [ ] **Step 2: 移动 Redis Ingestor 到 infra**

Run:
```bash
mv internal/knowledge/redis_ingestor.go internal/infra/redis/ingestor.go
mv internal/knowledge/redis_ingestor_test.go internal/infra/redis/ingestor_test.go
```
Expected: 文件已移动

- [ ] **Step 3: 删除旧的 ingestor.go**

Run:
```bash
rm internal/knowledge/ingestor.go
```
Expected: 文件已删除

---

## Chunk 4: 移动 domain/rag 层

### Task 9: 创建 domain/rag/store.go（接口定义）

**Files:**
- Create: `internal/domain/rag/store.go`
- Reference: `internal/rag/store.go`

- [ ] **Step 1: 创建 Store 接口定义**

```go
package rag

import (
	"context"
	"wanzhi/internal/domain/model"
)

// Store 向量存储接口
// 抽象不同的向量数据库实现（Milvus、内存等）
type Store interface {
	// Search 执行语义搜索，返回最相关的文档分块
	Search(ctx context.Context, query string, topK int, filters map[string]string) ([]SearchResult, error)

	// Upsert 插入或更新文档分块
	Upsert(ctx context.Context, chunks []model.Chunk, embeddings [][]float32) error

	// Delete 删除指定 ID 的文档分块
	Delete(ctx context.Context, ids []string) error

	// DeleteByService 删除指定服务的所有数据
	DeleteByService(ctx context.Context, service string) error
}

// SearchResult 搜索结果
type SearchResult struct {
	Chunk    model.Chunk
	Score    float32
	Metadata map[string]string
}
```

---

### Task 10: 移动 RAG 文件到 domain/rag/

**Files:**
- Move: `internal/rag/search.go` → `internal/domain/rag/search.go`
- Move: `internal/rag/search_test.go` → `internal/domain/rag/search_test.go`
- Move: `internal/rag/chunker.go` → `internal/domain/rag/chunker.go`
- Delete: `internal/rag/store.go`（已被 domain/rag/store.go 接口替代）

- [ ] **Step 1: 移动搜索和分块逻辑**

Run:
```bash
mv internal/rag/search.go internal/domain/rag/
mv internal/rag/search_test.go internal/domain/rag/
mv internal/rag/chunker.go internal/domain/rag/
```
Expected: 文件已移动

- [ ] **Step 2: 删除旧的 store.go**

Run:
```bash
rm internal/rag/store.go
```
Expected: 文件已删除

---

## Chunk 5: 移动 domain/tool 层

### Task 11: 移动 Tools 文件到 domain/tool/

**Files:**
- Move: `internal/tools/*.go` → `internal/domain/tool/`（排除 match_skill.go）
- Delete: `internal/tools/match_skill.go`（与 API 文档 Agent 定位无关）

- [ ] **Step 1: 移动工具层文件**

Run:
```bash
cd internal/tools
for f in *.go; do
  case "$f" in
    match_skill.go) continue ;;  # 删除无关文件
    *) cp "$f" ../domain/tool/ ;;
  esac
done
```
Expected: 所有工具文件已复制到 domain/tool/

- [ ] **Step 2: 验证 match_skill.go 已排除**

Run:
```bash
ls internal/domain/tool/ | grep match_skill || echo "match_skill.go 正确排除"
```
Expected: match_skill.go 不在 domain/tool/ 中

---

## Chunk 6: 移动 infra/llm 层

### Task 12: 抽取并移动 LLM 实现到 infra/llm/

**Files:**
- Create: `internal/infra/llm/openai.go`
- Create: `internal/infra/llm/rule_based.go`
- Reference: `internal/agent/llm.go:98-268`

- [ ] **Step 1: 创建 infra/llm/openai.go**

从 `internal/agent/openai_llm.go` 移动内容到 `internal/infra/llm/openai.go`，更新 package 声明为 `package llm`，更新 import 路径：
- `wanzhi/internal/agent` → `wanzhi/internal/domain/agent`

- [ ] **Step 2: 创建 infra/llm/rule_based.go**

从 `internal/agent/llm.go` 中提取 `RuleBasedLLMClient` 实现（第 98-268 行）：

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"wanzhi/internal/domain/agent"
)

// RuleBasedLLMClient 基于规则的 LLM 客户端
// 特点: 确定性、无需 API、适用于测试和降级
type RuleBasedLLMClient struct{}

// NewRuleBasedLLMClient 创建规则式 LLM 客户端
func NewRuleBasedLLMClient() *RuleBasedLLMClient {
	return &RuleBasedLLMClient{}
}

// Next 实现 agent.LLMClient 接口
func (c *RuleBasedLLMClient) Next(_ context.Context, messages []agent.Message, _ []agent.ToolDefinition) (agent.LLMReply, error) {
	query := userQuery(messages)
	lastTool := lastToolCallName(messages)
	lastEndpoint := lastEndpointFromTools(messages)
	stepCount := countToolCalls(messages)

	if stepCount == 0 {
		return agent.LLMReply{
			ToolCalls: []agent.ToolCall{
				{
					ID:   "tc-1",
					Name: "search_api",
					Args: mustRawJSON(map[string]any{
						"query": query,
						"top_k": 5,
					}),
				},
			},
		}, nil
	}

	if lastTool == "search_api" && lastEndpoint != "" {
		if strings.Contains(query, "依赖") || strings.Contains(strings.ToLower(query), "dependency") {
			return agent.LLMReply{
				ToolCalls: []agent.ToolCall{{
					ID:   "tc-2",
					Name: "analyze_dependencies",
					Args: mustRawJSON(map[string]any{
						"endpoint": lastEndpoint,
					}),
				}},
			}, nil
		}
		return agent.LLMReply{
			ToolCalls: []agent.ToolCall{{
				ID:   "tc-2",
				Name: "get_api_detail",
				Args: mustRawJSON(map[string]any{
					"endpoint": lastEndpoint,
				}),
			}},
		}, nil
	}

	if lastTool == "get_api_detail" && lastEndpoint != "" &&
		(strings.Contains(query, "示例") || strings.Contains(strings.ToLower(query), "example") || strings.Contains(strings.ToLower(query), "code")) {
		return agent.LLMReply{
			ToolCalls: []agent.ToolCall{{
				ID:   "tc-3",
				Name: "generate_example",
				Args: mustRawJSON(map[string]any{
					"endpoint": lastEndpoint,
					"language": "go",
				}),
			}},
		}, nil
	}

	return agent.LLMReply{Content: summarizeToolMessages(messages)}, nil
}

// 辅助函数（从原 llm.go 复制）
func mustRawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func userQuery(messages []agent.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func lastToolCallName(messages []agent.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			return messages[i].ToolCalls[0].Name
		}
	}
	return ""
}

func countToolCalls(messages []agent.Message) int {
	count := 0
	for _, msg := range messages {
		count += len(msg.ToolCalls)
	}
	return count
}

func lastEndpointFromTools(messages []agent.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "tool" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(messages[i].Content), &obj); err != nil {
			continue
		}
		if endpoint, ok := digEndpoint(obj); ok {
			return endpoint
		}
	}
	return ""
}

func digEndpoint(v map[string]any) (string, bool) {
	if s, ok := v["endpoint"].(string); ok {
		return s, true
	}
	if endpointObj, ok := v["endpoint"].(map[string]any); ok {
		method, mok := endpointObj["method"].(string)
		path, pok := endpointObj["path"].(string)
		if mok && pok {
			return fmt.Sprintf("%s %s", method, path), true
		}
	}
	if items, ok := v["items"].([]any); ok {
		for _, item := range items {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if endpoint, ok := itemMap["endpoint"].(string); ok {
				return endpoint, true
			}
		}
	}
	return "", false
}

func summarizeToolMessages(messages []agent.Message) string {
	parts := make([]string, 0)
	for _, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		parts = append(parts, msg.Content)
	}
	if len(parts) == 0 {
		return "未检索到可用信息。"
	}
	return "结构化汇总结果:\n" + strings.Join(parts, "\n")
}
```

- [ ] **Step 3: 移动测试文件**

Run:
```bash
mv internal/agent/openai_llm_test.go internal/infra/llm/openai_test.go
```
Expected: 测试文件已移动

---

## Chunk 7: 移动 infra/milvus 层

### Task 13: 移动 Milvus 文件到 infra/milvus/

**Files:**
- Move: `internal/rag/milvus_store.go` → `internal/infra/milvus/store.go`
- Move: `internal/rag/rag_store.go` → `internal/infra/milvus/rag_store.go`
- Move: `internal/rag/rerank_store.go` → `internal/infra/milvus/rerank_store.go`
- Move: `internal/store/milvus_client.go` → `internal/infra/milvus/client.go`
- Move: `internal/store/milvus_sdk_client.go` → `internal/infra/milvus/sdk_client.go`
- Move: `internal/rag/store_test.go` → `internal/infra/milvus/store_test.go`

- [ ] **Step 1: 移动 Milvus 相关文件**

Run:
```bash
# RAG 层 Milvus 实现
mv internal/rag/milvus_store.go internal/infra/milvus/store.go
mv internal/rag/rag_store.go internal/infra/milvus/rag_store.go
mv internal/rag/rerank_store.go internal/infra/milvus/rerank_store.go

# Store 层 Milvus 客户端
mv internal/store/milvus_client.go internal/infra/milvus/client.go
mv internal/store/milvus_sdk_client.go internal/infra/milvus/sdk_client.go

# 测试文件
mv internal/rag/store_test.go internal/infra/milvus/store_test.go
```
Expected: 所有 Milvus 相关文件已移动

---

## Chunk 8: 移动 infra/embedding 和 infra/rerank 层

### Task 14: 移动 Embedding 文件到 infra/embedding/

**Files:**
- Move: `internal/embedding/*.go` → `internal/infra/embedding/`

- [ ] **Step 1: 移动 Embedding 文件**

Run:
```bash
mv internal/embedding/*.go internal/infra/embedding/
```
Expected: 所有 Embedding 文件已移动

---

### Task 15: 移动 Rerank 文件到 infra/rerank/

**Files:**
- Move: `internal/rerank/*.go` → `internal/infra/rerank/`

- [ ] **Step 1: 移动 Rerank 文件**

Run:
```bash
mv internal/rerank/*.go internal/infra/rerank/
```
Expected: 所有 Rerank 文件已移动

---

## Chunk 9: 移动 transport 层

### Task 16: 移动 MCP 文件到 transport/

**Files:**
- Move: `internal/mcp/server.go` → `internal/transport/mcp.go`
- Move: `internal/mcp/server_test.go` → `internal/transport/mcp_test.go`
- Move: `internal/mcp/hooks.go` → `internal/transport/hooks.go`
- Move: `internal/mcp/sse.go` → `internal/transport/sse.go`
- Move: `internal/mcp/sse_test.go` → `internal/transport/sse_test.go`
- Move: `internal/mcp/sse_integration_test.go` → `internal/transport/sse_integration_test.go`
- Move: `internal/mcp/middleware.go` → `internal/transport/middleware.go`（已存在，需合并内容）
- Move: `internal/mcp/ratelimit.go` → `internal/transport/ratelimit.go`
- Move: `internal/mcp/ratelimit_test.go` → `internal/transport/ratelimit_test.go`
- Move: `internal/mcp/request_id.go` → `internal/transport/request_id.go`
- Move: `internal/mcp/request_id_test.go` → `internal/transport/request_id_test.go`

- [ ] **Step 1: 移动 MCP 核心文件**

Run:
```bash
# 跳过已存在的 chat.go 和 chat_test.go
for f in server.go hooks.go sse.go sse_test.go sse_integration_test.go ratelimit.go ratelimit_test.go request_id.go request_id_test.go; do
  [ -f "internal/mcp/$f" ] && mv "internal/mcp/$f" internal/transport/
done

# 测试文件
mv internal/mcp/server_test.go internal/transport/mcp_test.go
```
Expected: MCP 文件已移动

- [ ] **Step 2: 合并 middleware.go**

检查 `internal/mcp/middleware.go` 和 `internal/transport/middleware.go` 是否有重复，如果有则合并：
- 保留 `internal/transport/middleware.go` 中的 Gin 中间件
- 从 `internal/mcp/middleware.go` 中提取任何独有的功能

---

## Chunk 10: 批量更新 import 路径

### Task 17: 更新 tools → domain/tool 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/tools` import

- [ ] **Step 1: 批量替换 import 路径**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/tools|wanzhi/internal/domain/tool|g' {} +
```
Expected: 所有 import 已更新

---

### Task 18: 更新 agent → domain/agent 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/agent` import（排除 LLM 实现相关）

- [ ] **Step 1: 批量替换 import 路径**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/agent|wanzhi/internal/domain/agent|g' {} +
```
Expected: 所有 import 已更新

---

### Task 19: 更新 knowledge → domain/knowledge 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/knowledge` import

- [ ] **Step 1: 批量替换 import 路径**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/knowledge|wanzhi/internal/domain/knowledge|g' {} +
```
Expected: 所有 import 已更新

---

### Task 20: 更新 rag → domain/rag 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/rag` import

- [ ] **Step 1: 批量替换 import 路径**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/rag|wanzhi/internal/domain/rag|g' {} +
```
Expected: 所有 import 已更新

---

### Task 21: 更新 store → infra 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/store` import

- [ ] **Step 1: 批量替换 Redis 相关 import**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/store|wanzhi/internal/infra/redis|g' {} +
```
Expected: Redis store import 已更新

- [ ] **Step 2: 手动检查 Milvus 相关 import**

由于 Milvus 文件已移动到 `infra/milvus`，需要手动检查并更新：
- `wanzhi/internal/store` → `wanzhi/internal/infra/milvus`（针对 Milvus client）

---

### Task 22: 更新 embedding → infra/embedding 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/embedding` import

- [ ] **Step 1: 批量替换 import 路径**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/embedding|wanzhi/internal/infra/embedding|g' {} +
```
Expected: 所有 import 已更新

---

### Task 23: 更新 rerank → infra/rerank 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/rerank` import

- [ ] **Step 1: 批量替换 import 路径**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/rerank|wanzhi/internal/infra/rerank|g' {} +
```
Expected: 所有 import 已更新

---

### Task 24: 更新 mcp → transport 的 import

**Files:**
- Modify: 所有 `wanzhi/internal/mcp` import

- [ ] **Step 1: 批量替换 import 路径**

Run:
```bash
find . -name "*.go" -type f -exec sed -i '' \
  's|wanzhi/internal/mcp|wanzhi/internal/transport|g' {} +
```
Expected: 所有 import 已更新

---

## Chunk 11: 添加 domain/model import

### Task 25: 添加 model 包的 import

**Files:**
- Modify: 所有使用 Endpoint, Chunk, SpecMeta 等类型的文件

- [ ] **Step 1: 查找需要添加 import 的文件**

Run:
```bash
grep -r "type Endpoint" --include="*.go" internal/
grep -r "type Chunk" --include="*.go" internal/
```
Expected: 找到定义这些类型的文件

- [ ] **Step 2: 更新 domain/knowledge 文件添加 model import**

对于 `internal/domain/knowledge/` 中的文件，添加：
```go
import "wanzhi/internal/domain/model"
```

并将类型引用更新为 `model.Endpoint`, `model.Chunk` 等。

- [ ] **Step 3: 更新 domain/rag 文件添加 model import**

对于 `internal/domain/rag/` 中的文件，添加 model import 并更新类型引用。

- [ ] **Step 4: 更新 domain/tool 文件添加 model import**

对于 `internal/domain/tool/` 中的文件，添加 model import 并更新类型引用。

- [ ] **Step 5: 更新 infra 层文件添加 model import**

对于 `internal/infra/` 中的文件，添加 model import 并更新类型引用。

---

## Chunk 12: 更新 cmd/server/main.go 依赖注入

### Task 26: 重构 main.go 的 import

**Files:**
- Modify: `cmd/server/main.go:1-34`

- [ ] **Step 1: 更新 import 声明**

将：
```go
"wanzhi/internal/agent"
"wanzhi/internal/embedding"
ingestsvc "wanzhi/internal/ingest"
"wanzhi/internal/mcp"
"wanzhi/internal/rag"
"wanzhi/internal/rerank"
"wanzhi/internal/store"
"wanzhi/internal/tools"
```

更新为：
```go
"wanzhi/internal/domain/agent"
"wanzhi/internal/domain/tool"
infraagent "wanzhi/internal/infra/agent"
"wanzhi/internal/infra/embedding"
"wanzhi/internal/infra/milvus"
infrarerank "wanzhi/internal/infra/rerank"
"wanzhi/internal/transport"
```

---

### Task 27: 重构 newLLMClient 函数

**Files:**
- Modify: `cmd/server/main.go`（查找 newLLMClient 函数）

- [ ] **Step 1: 更新 LLM 客户端创建逻辑**

将现有的 `newLLMClient` 函数中的实现引用更新为 infra 层：

```go
func newLLMClient(cfg config.Config) agent.LLMClient {
	if cfg.LLM.APIKey != "" {
		return infraagent.NewOpenAIClient(agent.OpenAIConfig{
			APIKey:      cfg.LLM.APIKey,
			BaseURL:     cfg.LLM.BaseURL,
			Model:       cfg.LLM.Model,
			MaxTokens:   cfg.LLM.MaxTokens,
			Temperature: cfg.LLM.Temperature,
		})
	}
	return infraagent.NewRuleBasedLLMClient()
}
```

---

### Task 28: 重构 newKnowledgeBase 函数

**Files:**
- Modify: `cmd/server/main.go`（查找 newKnowledgeBase 函数）

- [ ] **Step 1: 更新知识库装配逻辑**

将现有的 `newKnowledgeBase` 函数重构为使用新的分层结构：

```go
func newKnowledgeBase(ctx context.Context, cfg config.Config) (*tool.KnowledgeBase, rag.Store, func(), error) {
	// Infrastructure 层客户端
	redisClient := redis.NewClient(redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	milvusClient, err := milvus.NewClient(ctx, cfg.Milvus.Address, cfg.RAG.EmbeddingDim)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create milvus client: %w", err)
	}

	embeddingClient := embedding.NewClient(cfg.Embedding)
	rerankClient := infrarerank.NewClient(cfg.Rerank)

	// Domain 层
	ingestor := redis.NewIngestor(redisClient)
	ragStore := milvus.NewStore(milvusClient, embeddingClient, rerankClient, cfg.Milvus.Collection)

	kb := tool.NewKnowledgeBase(ingestor, ragStore)

	cleanup := func() {
		_ = milvusClient.Close(ctx)
		_ = redisClient.Close()
	}

	return kb, ragStore, cleanup, nil
}
```

---

### Task 29: 更新 main.go 中的工具注册

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: 更新工具注册调用**

将：
```go
registry := tools.NewRegistry()
if err := tools.RegisterDefaultTools(registry, kb, "skills"); err != nil {
```

更新为：
```go
registry := tool.NewRegistry()
if err := tool.RegisterDefaultTools(registry, kb, "skills"); err != nil {
```

---

### Task 30: 更新 Agent 引擎创建

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: 更新 Agent 引擎引用**

将所有 `agent.` 前缀更新为使用 domain 层的类型，确保引用的是 `wanzhi/internal/domain/agent`。

---

### Task 31: 更新 MCP Server 创建

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: 更新 MCP Server 调用**

将：
```go
srv := mcp.NewServer(cfg, registry, mcp.Hooks{}, mcp.ServerOptions{...})
```

更新为：
```go
srv := transport.NewServer(cfg, registry, transport.Hooks{}, transport.ServerOptions{...})
```

---

### Task 32: 创建 transport/router.go

**Files:**
- Create: `internal/transport/router.go`

- [ ] **Step 1: 创建路由装配文件**

```go
package transport

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"wanzhi/internal/config"
	"wanzhi/internal/domain/agent"
	"wanzhi/internal/domain/tool"
	"wanzhi/internal/observability"
)

// Router 装配所有路由
func NewRouter(
	cfg config.Config,
	mcpServer *Server,
	agentEngine agent.AdaptiveEngine,
	kb *tool.KnowledgeBase,
	promRegistry *prometheus.Registry,
	logger *observability.Logger,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// 中间件
	router.Use(gin.Recovery())
	router.Use(RequestIDMiddleware())

	// MCP 端点
	mcpGroup := router.Group("/mcp")
	mcpGroup.Use(AuthMiddleware(cfg.Server.AuthToken))
	mcpGroup.Use(RateLimitMiddleware(mcpServer.Limiter()))
	mcpGroup.Use(LoggingMiddleware(logger))
	mcpGroup.POST("", mcpServer.HandleRPC)

	// Chat SSE 端点
	chatHandler := NewChatHandler(agentEngine)
	router.POST("/api/chat", chatHandler.HandleChat)

	// 健康检查和指标
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{})))

	return router
}
```

---

## Chunk 13: 清理与验证

### Task 33: 删除空目录

**Files:**
- Delete: 空的旧目录

- [ ] **Step 1: 删除旧的空目录**

Run:
```bash
# 尝试删除已清空的目录
rmdir internal/tools 2>/dev/null || echo "internal/tools 不为空或不存在"
rmdir internal/agent 2>/dev/null || echo "internal/agent 不为空或不存在"
rmdir internal/knowledge 2>/dev/null || echo "internal/knowledge 不为空或不存在"
rmdir internal/rag 2>/dev/null || echo "internal/rag 不为空或不存在"
rmdir internal/store 2>/dev/null || echo "internal/store 不为空或不存在"
rmdir internal/embedding 2>/dev/null || echo "internal/embedding 不为空或不存在"
rmdir internal/rerank 2>/dev/null || echo "internal/rerank 不为空或不存在"
rmdir internal/mcp 2>/dev/null || echo "internal/mcp 不为空或不存在"
rmdir internal/ingest 2>/dev/null || echo "internal/ingest 不为空或不存在"
```
Expected: 空目录已删除

---

### Task 34: 编译检查

- [ ] **Step 1: 运行编译检查**

Run:
```bash
go build ./...
```
Expected: 编译成功，无错误

- [ ] **Step 2: 检查编译错误并修复**

如果有编译错误：
1. 检查错误信息，定位缺失的 import
2. 添加正确的 import 路径
3. 重新运行 `go build ./...`
4. 重复直到编译通过

---

### Task 35: 运行测试

- [ ] **Step 1: 运行所有测试**

Run:
```bash
go test ./... -count=1
```
Expected: 所有测试通过

- [ ] **Step 2: 修复失败的测试**

如果有测试失败：
1. 检查失败的测试和错误信息
2. 更新 import 路径或类型引用
3. 重新运行测试
4. 重复直到测试通过

---

### Task 36: 静态分析

- [ ] **Step 1: 运行 go vet**

Run:
```bash
go vet ./...
```
Expected: 无警告

---

### Task 37: 验证 domain 层零外部依赖

- [ ] **Step 1: 检查 domain 层 import**

Run:
```bash
grep -r "wanzhi/internal/infra" internal/domain/ || echo "✓ domain 层不依赖 infra"
grep -r "wanzhi/internal/transport" internal/domain/ || echo "✓ domain 层不依赖 transport"
```
Expected: 无输出（domain 层不依赖 infra 或 transport）

---

## Chunk 14: 最终验证与提交

### Task 38: 构建并运行服务

- [ ] **Step 1: 构建服务**

Run:
```bash
go build -o bin/wanzhi cmd/server/main.go
```
Expected: 构建成功

- [ ] **Step 2: 启动服务测试**

Run:
```bash
# 基础模式测试
AUTH_TOKEN=test-token ./bin/wanzhi run &
PID=$!
sleep 2

# 测试 MCP 端点
curl -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer test-token' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}'

# 测试 Chat 端点
curl -N -X POST http://localhost:8080/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"测试"}'

# 测试健康检查
curl http://localhost:8080/healthz

# 停止服务
kill $PID
```
Expected: 所有端点响应正常

---

### Task 39: 提交变更

- [ ] **Step 1: 查看变更**

Run:
```bash
git status
git diff --stat
```
Expected: 看到所有重构变更

- [ ] **Step 2: 提交重构**

Run:
```bash
git add .
git commit -m "refactor: implement hexagonal architecture

- Create domain/ layer with zero external dependencies
  - domain/model/: Endpoint, Chunk, SpecMeta
  - domain/agent/: Agent engine + LLMClient interface
  - domain/knowledge/: Swagger parser + Ingestor interface
  - domain/rag/: Search + Chunker + Store interface
  - domain/tool/: All tools (tools → tool)

- Move infrastructure implementations to infra/
  - infra/llm/: OpenAI, RuleBased LLM clients
  - infra/milvus/: Milvus client and Store implementation
  - infra/redis/: Redis client and Ingestor implementation
  - infra/embedding/: Embedding service
  - infra/rerank/: Rerank service

- Move protocol adapters to transport/
  - transport/mcp.go: MCP JSON-RPC handler
  - transport/chat.go: Chat SSE handler (existing)

- Update cmd/server/main.go with new dependency injection
- Dependency direction: transport → domain ← infra
- Remove match_skill tool (unrelated to API docs agent)
- All tests passing, domain layer has zero external deps
"
```
Expected: 提交成功

---

## 验收标准确认

在完成所有任务后，确认以下标准已满足：

- [ ] **标准 1**: `go build ./...` 编译通过
- [ ] **标准 2**: `go test ./... -count=1` 所有测试通过
- [ ] **标准 3**: `go vet ./...` 静态分析通过
- [ ] **标准 4**: `domain/` 层无外部依赖（不 import infra/transport）
- [ ] **标准 5**: 所有接口定义在 `domain/`，实现在 `infra/`
- [ ] **标准 6**: 服务可以正常启动和响应请求

---

## 架构验证

重构完成后，新的目录结构应为：

```
internal/
├── domain/
│   ├── model/          ✓ 领域模型（Endpoint, Chunk, SpecMeta）
│   ├── agent/          ✓ Agent 引擎 + LLMClient 接口
│   ├── knowledge/      ✓ Swagger 解析器 + Ingestor 接口
│   ├── rag/            ✓ Search + Chunker + Store 接口
│   └── tool/           ✓ 所有工具（tools → tool）
│
├── infra/
│   ├── llm/            ✓ OpenAI, RuleBased LLM 实现
│   ├── milvus/         ✓ Milvus 客户端 + Store 实现
│   ├── redis/          ✓ Redis 客户端 + Ingestor 实现
│   ├── embedding/      ✓ Embedding 服务
│   └── rerank/         ✓ Rerank 服务
│
├── transport/
│   ├── router.go       ✓ 路由装配（新建）
│   ├── mcp.go          ✓ MCP JSON-RPC handler
│   ├── chat.go         ✓ Chat SSE handler
│   └── middleware.go   ✓ Gin 中间件
│
├── config/             ✓ 配置加载
├── observability/      ✓ 日志 + 指标
├── resilience/         ✓ 熔断器
├── webhook/            ✓ Webhook 同步
└── e2e/                ✓ E2E 测试
```

---

## 回滚计划

如果重构遇到无法解决的问题，可以回滚到重构前的状态：

```bash
# 查看重构前的提交
git log --oneline -10

# 回滚到重构前的提交
git reset --hard <commit-hash>

# 或者创建新分支保留重构
git checkout -b refactor/hexagonal-architecture-backup
git checkout main
```
