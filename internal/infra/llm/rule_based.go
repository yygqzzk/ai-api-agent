package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"wanzhi/internal/domain/agent"
)

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
