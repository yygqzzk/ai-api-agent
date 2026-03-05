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

type Document struct {
	Text string `json:"text"`
}

type RerankRequest struct {
	Model string `json:"model"`
	Input struct {
		Query     string     `json:"query"`
		Documents []Document `json:"documents"`
	} `json:"input"`
	Parameters struct {
		ReturnDocuments bool `json:"return_documents"`
		TopN            int  `json:"top_n,omitempty"`
	} `json:"parameters"`
}

type RerankResponse struct {
	Output struct {
		Results []struct {
			Index          int      `json:"index"`
			RelevanceScore float64  `json:"relevance_score"`
			Document       Document `json:"document,omitempty"`
		} `json:"results"`
	} `json:"output"`
	Usage *struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

func main() {
	apiKey := os.Getenv("RERANK_API_KEY")
	baseURL := os.Getenv("RERANK_BASE_URL")
	model := os.Getenv("RERANK_MODEL")

	if apiKey == "" || baseURL == "" || model == "" {
		fmt.Println("❌ 请先设置环境变量: RERANK_API_KEY, RERANK_BASE_URL, RERANK_MODEL")
		os.Exit(1)
	}

	fmt.Println("🧪 测试 Rerank API")
	fmt.Printf("📍 Base URL: %s\n", baseURL)
	fmt.Printf("🤖 Model: %s\n\n", model)

	req := RerankRequest{
		Model: model,
	}
	req.Input.Query = "什么是文本排序模型"
	req.Input.Documents = []Document{
		{Text: "文本排序模型广泛用于搜索引擎和推荐系统中，它们根据文本相关性对候选文本进行排序"},
		{Text: "量子计算是计算科学的一个前沿领域"},
		{Text: "预训练语言模型的发展给文本排序模型带来了新的进展"},
	}
	req.Parameters.ReturnDocuments = true
	req.Parameters.TopN = 2

	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		baseURL+"/api/v1/services/rerank/text-rerank/text-rerank",
		bytes.NewReader(body),
	)
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

	var rerankResp RerankResponse
	if err := json.Unmarshal(respBody, &rerankResp); err != nil {
		fmt.Printf("❌ 解析响应失败: %v\n", err)
		fmt.Printf("响应内容: %s\n", string(respBody))
		os.Exit(1)
	}

	if rerankResp.Code != "" {
		fmt.Printf("❌ API 返回错误: %s - %s\n", rerankResp.Code, rerankResp.Message)
		os.Exit(1)
	}

	if len(rerankResp.Output.Results) == 0 {
		fmt.Println("❌ 响应中没有返回排序结果")
		os.Exit(1)
	}

	fmt.Printf("✅ Rerank API 调用成功 (耗时: %v)\n\n", duration)
	fmt.Printf("📊 排序结果 (Top %d):\n", len(rerankResp.Output.Results))
	for i, result := range rerankResp.Output.Results {
		fmt.Printf("  %d. 相关性分数: %.4f\n", i+1, result.RelevanceScore)
		fmt.Printf("     原始索引: %d\n", result.Index)
		fmt.Printf("     文本: %s\n\n", result.Document.Text)
	}
	if rerankResp.Usage != nil {
		fmt.Printf("  Token 使用量: %d\n", rerankResp.Usage.TotalTokens)
	}
	if rerankResp.RequestID != "" {
		fmt.Printf("  Request ID: %s\n", rerankResp.RequestID)
	}
}
