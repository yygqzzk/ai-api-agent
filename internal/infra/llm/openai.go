package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wanzhi/internal/domain/agent"
)

type OpenAICompatibleLLMConfig struct {
	APIKey       string
	BaseURL      string
	Model        string
	MaxTokens    int
	Temperature  float64
	MaxRetries   int
	RetryBackoff time.Duration
	HTTPClient   *http.Client
}

type OpenAICompatibleLLMClient struct {
	apiKey       string
	baseURL      string
	model        string
	maxTokens    int
	temperature  float64
	maxRetries   int
	retryBackoff time.Duration
	client       *http.Client
}

func NewOpenAICompatibleLLMClient(cfg OpenAICompatibleLLMConfig) *OpenAICompatibleLLMClient {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		// http.DefaultClient 是 net/http 提供的默认可用客户端；这里做兜底，外部仍可注入自定义超时或测试 client。
		httpClient = http.DefaultClient
	}
	retryBackoff := cfg.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = 200 * time.Millisecond
	}
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &OpenAICompatibleLLMClient{
		apiKey:       strings.TrimSpace(cfg.APIKey),
		baseURL:      baseURL,
		model:        model,
		maxTokens:    cfg.MaxTokens,
		temperature:  cfg.Temperature,
		maxRetries:   maxRetries,
		retryBackoff: retryBackoff,
		client:       httpClient,
	}
}

func (c *OpenAICompatibleLLMClient) Next(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.LLMReply, error) {
	reqBody := openAIChatRequest{
		Model:       c.model,
		Messages:    toOpenAIMessages(messages),
		Tools:       toOpenAITools(tools),
		ToolChoice:  "auto",
		MaxTokens:   c.maxTokens,
		Temperature: c.temperature,
	}

	// json.Marshal 会把 Go struct 按 json tag 编码成请求体字节。
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return agent.LLMReply{}, fmt.Errorf("marshal chat completion request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		reply, retryAfter, retryable, err := c.doRequest(ctx, payload)
		if err == nil {
			return reply, nil
		}
		lastErr = err
		if !retryable || attempt == c.maxRetries {
			return agent.LLMReply{}, lastErr
		}

		delay := retryAfter
		if delay <= 0 {
			delay = c.retryBackoff * time.Duration(attempt+1)
		}
		// time.NewTimer + select 比 time.Sleep 更适合重试等待，因为它还能同时监听 ctx.Done() 提前取消。
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return agent.LLMReply{}, fmt.Errorf("context canceled while waiting retry: %w", ctx.Err())
		case <-timer.C:
		}
	}
	return agent.LLMReply{}, lastErr
}

func (c *OpenAICompatibleLLMClient) doRequest(ctx context.Context, payload []byte) (agent.LLMReply, time.Duration, bool, error) {
	// NewRequestWithContext 会把取消/超时信号绑定到 HTTP 请求；bytes.NewReader 则把 []byte 包装成 io.Reader 作为请求体。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return agent.LLMReply{}, 0, false, fmt.Errorf("create chat completion request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return agent.LLMReply{}, 0, false, fmt.Errorf("send chat completion request: %w", err)
		}
		return agent.LLMReply{}, 0, true, fmt.Errorf("send chat completion request: %w", err)
	}
	defer resp.Body.Close()

	// io.ReadAll 适合这种需要先完整拿到响应体，再统一判断状态码和反序列化的场景。
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.LLMReply{}, 0, false, fmt.Errorf("read chat completion response: %w", err)
	}
	if resp.StatusCode >= 300 {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return agent.LLMReply{}, retryAfter, retryable, fmt.Errorf("chat completion API error %d: %s", resp.StatusCode, string(body))
	}

	var parsed openAIChatResponse
	// json.Unmarshal 会把 JSON 字节解码回 struct，目标字段通过 json tag 对齐。
	if err := json.Unmarshal(body, &parsed); err != nil {
		return agent.LLMReply{}, 0, false, fmt.Errorf("decode chat completion response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return agent.LLMReply{}, 0, false, fmt.Errorf("chat completion response has no choices")
	}

	msg := parsed.Choices[0].Message
	reply := agent.LLMReply{Content: strings.TrimSpace(msg.Content)}
	for _, tc := range msg.ToolCalls {
		reply.ToolCalls = append(reply.ToolCalls, agent.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: normalizeToolArguments(tc.Function.Arguments),
		})
	}
	reply.PromptTokens = parsed.Usage.PromptTokens
	reply.CompletionTokens = parsed.Usage.CompletionTokens
	return reply, 0, false, nil
}

func parseRetryAfter(v string) time.Duration {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return 0
	}
	// strconv.Atoi 用于把 Retry-After 头里的秒数字符串转成 int。
	sec, err := strconv.Atoi(raw)
	if err != nil || sec < 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func (c *OpenAICompatibleLLMClient) Model() string {
	return c.model
}

func (c *OpenAICompatibleLLMClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return fmt.Errorf("create llm health request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("llm health request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusUnauthorized, http.StatusForbidden:
		return nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("llm health status %d: %s", resp.StatusCode, string(body))
	}
}

func normalizeToolArguments(raw string) json.RawMessage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return json.RawMessage(`{}`)
	}
	body := json.RawMessage(raw)
	if json.Valid(body) {
		return body
	}
	fallback, _ := json.Marshal(map[string]any{"raw_arguments": raw})
	return fallback
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  string          `json:"tool_choice,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIToolSpec `json:"function"`
}

type openAIToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func toOpenAIMessages(messages []agent.Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, msg := range messages {
		item := openAIMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			item.ToolCalls = make([]openAIToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				item.ToolCalls = append(item.ToolCalls, openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIFunctionCall{
						Name:      tc.Name,
						Arguments: string(tc.Args),
					},
				})
			}
		}
		out = append(out, item)
	}
	return out
}

func toOpenAITools(tools []agent.ToolDefinition) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIToolSpec{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}
	return out
}
