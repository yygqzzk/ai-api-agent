//go:build ignore

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type ChatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func main() {
	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")

	if apiKey == "" || baseURL == "" || model == "" {
		fmt.Println("❌ 请先设置环境变量: LLM_API_KEY, LLM_BASE_URL, LLM_MODEL")
		os.Exit(1)
	}

	fmt.Println("🧪 测试 LLM API")
	fmt.Printf("📍 Base URL: %s\n", baseURL)
	fmt.Printf("🤖 Model: %s\n\n", model)

	req := ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: "你好，请用一句话介绍你自己"},
		},
	}

	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		fmt.Printf("❌ 创建请求失败: %v\n", err)
		os.Exit(1)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	fmt.Println("⏳ 发送请求...")
	start := time.Now()
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		fmt.Printf("❌ 请求失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("❌ API 返回错误 (状态码: %d)\n", resp.StatusCode)
		fmt.Printf("响应内容: %s\n", string(respBody))
		os.Exit(1)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		fmt.Printf("❌ 解析响应失败: %v\n", err)
		fmt.Printf("响应内容: %s\n", string(respBody))
		os.Exit(1)
	}

	if chatResp.Error != nil {
		fmt.Printf("❌ API 返回错误: %s (类型: %s)\n", chatResp.Error.Message, chatResp.Error.Type)
		os.Exit(1)
	}

	if len(chatResp.Choices) == 0 {
		fmt.Println("❌ 响应中没有返回内容")
		os.Exit(1)
	}

	fmt.Printf("✅ LLM API 调用成功 (耗时: %v)\n\n", duration)
	fmt.Println("📝 模型回复:")
	fmt.Println(chatResp.Choices[0].Message.Content)
}
