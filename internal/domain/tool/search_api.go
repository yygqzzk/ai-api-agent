package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

type SearchAPITool struct {
	kb *KnowledgeBase
}

func NewSearchAPITool(kb *KnowledgeBase) *SearchAPITool {
	return &SearchAPITool{kb: kb}
}

func (t *SearchAPITool) Name() string {
	return "search_api"
}

func (t *SearchAPITool) Description() string {
	return "语义检索 API 接口摘要列表"
}

func (t *SearchAPITool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"top_k":{"type":"integer"},"service":{"type":"string"}}}`)
}

func (t *SearchAPITool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var req SearchAPIArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode search_api args: %w", err)
	}
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}

	hits, err := t.kb.Search(ctx, req.Query, req.TopK, req.Service)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	items := make([]SearchAPIItem, 0, len(hits))
	for _, hit := range hits {
		items = append(items, SearchAPIItem{
			Service:   hit.Chunk.Service,
			Endpoint:  hit.Chunk.Endpoint,
			ChunkType: hit.Chunk.Type,
			Snippet:   hit.Chunk.Content,
			Score:     float64(hit.Score),
		})
	}
	return SearchAPIResult{Items: items}, nil
}
