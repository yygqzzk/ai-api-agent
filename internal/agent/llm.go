package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type LLMReply struct {
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
}

type LLMClient interface {
	Next(ctx context.Context, messages []Message, tools []ToolDefinition) (LLMReply, error)
}

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
