package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-agent-api/internal/knowledge"
)

type ParseSwaggerTool struct {
	kb *KnowledgeBase
}

func NewParseSwaggerTool(kb *KnowledgeBase) *ParseSwaggerTool {
	return &ParseSwaggerTool{kb: kb}
}

func (t *ParseSwaggerTool) Name() string {
	return "parse_swagger"
}

func (t *ParseSwaggerTool) Description() string {
	return "导入 Swagger/OpenAPI 文档到知识库"
}

func (t *ParseSwaggerTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string"},"url":{"type":"string"},"service":{"type":"string"}}}`)
}

func (t *ParseSwaggerTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var req ParseSwaggerArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode parse_swagger args: %w", err)
	}

	hasFile := strings.TrimSpace(req.FilePath) != ""
	hasURL := strings.TrimSpace(req.URL) != ""
	if !hasFile && !hasURL {
		return nil, fmt.Errorf("file_path or url is required")
	}

	var (
		doc   knowledge.ParsedSpec
		stats knowledge.IngestStats
		err   error
	)
	if hasFile {
		doc, stats, err = t.kb.IngestFileDocument(ctx, req.FilePath, req.Service)
	} else {
		doc, stats, err = t.kb.IngestURLDocument(ctx, req.URL, req.Service)
	}
	if err != nil {
		return nil, err
	}

	return ParseSwaggerResult{Stats: stats, Spec: doc.Meta}, nil
}
