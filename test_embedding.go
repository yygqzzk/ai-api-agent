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

type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type EmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage *struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func main() {
	apiKey := os.Getenv("EMBEDDING_API_KEY")
	baseURL := os.Getenv("EMBEDDING_BASE_URL")
	model := os.Getenv("EMBEDDING_MODEL")

	if apiKey == "" || baseURL == "" || model == "" {
		fmt.Println("❌ 请先设置环境变量: EMBEDDING_API_KEY, EMBEDDING_BASE_URL, EMBEDDING_MODEL")
		os.Exit(1)
	}

	fmt.Println("🧪 测试 Embedding API")
	fmt.Printf("📍 Base URL: %s\n", baseURL)
	fmt.Printf("🤖 Model: %s\n\n", model)

	req := EmbeddingRequest{
		Model: model,
		Input: []string{"人工智能是计算机科学的一个分支", "机器学习是实现人工智能的重要方法"},
	}

	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/embeddings", bytes.NewReader(body))
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

	var embResp EmbeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		fmt.Printf("❌ 解析响应失败: %v\n", err)
		fmt.Printf("响应内容: %s\n", string(respBody))
		os.Exit(1)
	}

	if embResp.Error != nil {
		fmt.Printf("❌ API 返回错误: %s (类型: %s)\n", embResp.Error.Message, embResp.Error.Type)
		os.Exit(1)
	}

	if len(embResp.Data) == 0 {
		fmt.Println("❌ 响应中没有返回向量")
		os.Exit(1)
	}

	fmt.Printf("✅ Embedding API 调用成功 (耗时: %v)\n\n", duration)
	fmt.Printf("📊 返回结果:\n")
	for i, data := range embResp.Data {
		fmt.Printf("  - 文本 %d: 向量维度 = %d, 前5个值 = %v\n",
			i+1, len(data.Embedding), data.Embedding[:5])
	}
	if embResp.Usage != nil {
		fmt.Printf("  - Token 使用量: %d\n", embResp.Usage.TotalTokens)
	}
}
