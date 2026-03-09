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
