package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Message 对话消息
// 遵循 OpenAI Chat Completion API 格式
//
// 角色类型:
// - "system": 系统提示词,定义 Agent 行为
// - "user": 用户查询
// - "assistant": LLM 回复 (可能包含工具调用)
// - "tool": 工具执行结果
//
// 工具调用流程:
// 1. user: "查询用户登录接口"
// 2. assistant: ToolCalls=[{Name:"search_api", Args:{...}}]
// 3. tool: ToolCallID="tc-1", Content="{...}"
// 4. assistant: Content="找到用户登录接口..."
//

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 工具调用请求
// LLM 决定调用哪个工具以及传递什么参数
//

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolDefinition 工具定义
// 描述工具的功能和参数,传递给 LLM
//
// Schema 格式: JSON Schema
// 示例:
// {
//   "type": "object",
//   "properties": {
//     "query": {"type": "string", "description": "搜索关键词"}
//   },
//   "required": ["query"]
// }
//

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// LLMReply LLM 响应
// 包含文本内容或工具调用请求
//
// 两种响应模式:
// 1. 文本响应: Content 非空, ToolCalls 为空
// 2. 工具调用: ToolCalls 非空, Content 可能为空
//
// Token 统计:
// - PromptTokens: 输入 token 数
// - CompletionTokens: 输出 token 数
// - 用于成本计算和性能分析
//

type LLMReply struct {
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
}

// LLMClient LLM 客户端接口
//
// 抽象不同的 LLM 实现:
// - OpenAICompatibleLLMClient: 调用 OpenAI API 或兼容 API
// - RuleBasedLLMClient: 基于规则的确定性实现 (用于测试)
//
// 设计模式: Strategy Pattern
// - 运行时可切换不同的 LLM 实现
// - 支持降级: OpenAI 不可用时降级到 RuleBased
//

type LLMClient interface {
	Next(ctx context.Context, messages []Message, tools []ToolDefinition) (LLMReply, error)
}

// RuleBasedLLMClient 基于规则的 LLM 客户端
//
// 特点:
// - 确定性: 相同输入总是产生相同输出
// - 无需 API: 不依赖外部 LLM 服务
// - 适用场景: 测试、演示、降级
//
// 决策规则:
// 1. 第一步: 调用 search_api 搜索
// 2. 搜索后: 调用 get_api_detail 获取详情
// 3. 详情后: 如果查询包含"示例",调用 generate_example
// 4. 其他情况: 返回文本总结
//
// 限制:
// - 只支持固定的工具调用流程
// - 无法理解复杂的自然语言
// - 不支持多轮对话推理
//

type RuleBasedLLMClient struct{}

func NewRuleBasedLLMClient() *RuleBasedLLMClient {
	return &RuleBasedLLMClient{}
}

func (c *RuleBasedLLMClient) Next(_ context.Context, messages []Message, _ []ToolDefinition) (LLMReply, error) {
	query := userQuery(messages)
	lastTool := lastToolCallName(messages)
	lastEndpoint := lastEndpointFromTools(messages)
	stepCount := countToolCalls(messages)

	if stepCount == 0 {
		return LLMReply{
			ToolCalls: []ToolCall{
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
			return LLMReply{
				ToolCalls: []ToolCall{{
					ID:   "tc-2",
					Name: "analyze_dependencies",
					Args: mustRawJSON(map[string]any{
						"endpoint": lastEndpoint,
					}),
				}},
			}, nil
		}
		return LLMReply{
			ToolCalls: []ToolCall{{
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
		return LLMReply{
			ToolCalls: []ToolCall{{
				ID:   "tc-3",
				Name: "generate_example",
				Args: mustRawJSON(map[string]any{
					"endpoint": lastEndpoint,
					"language": "go",
				}),
			}},
		}, nil
	}

	return LLMReply{Content: summarizeToolMessages(messages)}, nil
}

func mustRawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func userQuery(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func lastToolCallName(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			return messages[i].ToolCalls[0].Name
		}
	}
	return ""
}

func countToolCalls(messages []Message) int {
	count := 0
	for _, msg := range messages {
		count += len(msg.ToolCalls)
	}
	return count
}

func lastEndpointFromTools(messages []Message) string {
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

func summarizeToolMessages(messages []Message) string {
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
