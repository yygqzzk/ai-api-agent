package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DashScopeClient 阿里云百炼 rerank 客户端
type DashScopeClient struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewDashScopeClient 创建阿里云百炼 rerank 客户端
func NewDashScopeClient(apiKey, baseURL, model string) *DashScopeClient {
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com"
	}
	if model == "" {
		model = "qwen3-vl-rerank"
	}
	return &DashScopeClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		model:      model,
		httpClient: http.DefaultClient,
	}
}

type rerankRequest struct {
	Model string `json:"model"`
	Input struct {
		Query     string     `json:"query"`
		Documents []Document `json:"documents"`
	} `json:"input"`
	Parameters struct {
		ReturnDocuments bool    `json:"return_documents"`
		TopN            int     `json:"top_n,omitempty"`
		FPS             float64 `json:"fps,omitempty"`
	} `json:"parameters"`
}

type rerankResponse struct {
	Output struct {
		Results []Result `json:"results"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

func (c *DashScopeClient) Rerank(ctx context.Context, query string, documents []Document, topN int) ([]Result, error) {
	req := rerankRequest{
		Model: c.model,
	}
	req.Input.Query = query
	req.Input.Documents = documents
	req.Parameters.ReturnDocuments = true
	if topN > 0 {
		req.Parameters.TopN = topN
	}
	// 视频帧率设置为 1.0（最高质量）
	req.Parameters.FPS = 1.0

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/api/v1/services/rerank/text-rerank/text-rerank",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result rerankResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Code != "" {
		return nil, fmt.Errorf("rerank API error: %s - %s", result.Code, result.Message)
	}

	return result.Output.Results, nil
}
