package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAIClient calls the OpenAI-compatible /v1/embeddings endpoint.
type OpenAIClient struct {
	apiKey  string
	baseURL string
	model   string
	dim     int
	client  *http.Client
}

type OpenAIOption func(*OpenAIClient)

func WithHTTPClient(c *http.Client) OpenAIOption {
	return func(o *OpenAIClient) { o.client = c }
}

func NewOpenAIClient(apiKey, baseURL, model string, dim int, opts ...OpenAIOption) *OpenAIClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	c := &OpenAIClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		dim:     dim,
		client:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *OpenAIClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embeddingRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	vectors := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		vectors[i] = d.Embedding
	}
	return vectors, nil
}

func (c *OpenAIClient) Dimension() int { return c.dim }
