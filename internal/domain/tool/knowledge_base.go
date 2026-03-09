package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"wanzhi/internal/domain/knowledge"
	"wanzhi/internal/domain/model"
	"wanzhi/internal/domain/rag"
)

type KnowledgeBase struct {
	mu       sync.RWMutex
	ingestor knowledge.Ingestor
	engine   *rag.Engine
}

// NewKnowledgeBase 创建知识库
func NewKnowledgeBase(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
	return &KnowledgeBase{
		ingestor: ingestor,
		engine:   rag.NewEngine(ragStore),
	}
}

// NewKnowledgeBaseWithStores 创建知识库（兼容旧名称）
func NewKnowledgeBaseWithStores(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
	return NewKnowledgeBase(ingestor, ragStore)
}

// NewKnowledgeBaseWithIngestor 创建知识库（兼容旧名称）
func NewKnowledgeBaseWithIngestor(ingestor knowledge.Ingestor, ragStore rag.Store) *KnowledgeBase {
	return NewKnowledgeBase(ingestor, ragStore)
}

// IngestBytes 录入 API 规范字节数据
func (kb *KnowledgeBase) IngestBytes(ctx context.Context, data []byte, source string) (model.IngestStats, error) {
	if strings.HasPrefix(string(data), "%PDF") {
		return model.IngestStats{}, fmt.Errorf("PDF ingest not yet implemented: %s", source)
	}
	return kb.ingestJSON(ctx, data, source)
}

func (kb *KnowledgeBase) ingestJSON(ctx context.Context, data []byte, source string) (model.IngestStats, error) {
	doc, err := knowledge.ParseSwaggerDocumentBytes(data, source)
	if err != nil {
		return model.IngestStats{}, fmt.Errorf("parse swagger: %w", err)
	}

	service := doc.Meta.Service
	if service == "" {
		service = strings.TrimSuffix(strings.TrimPrefix(source, "https://"), ".json")
		service = strings.ReplaceAll(service, "/", "-")
	}

	if err := kb.ingestor.SaveSpec(ctx, service, data); err != nil {
		return model.IngestStats{}, fmt.Errorf("save spec: %w", err)
	}

	if err := kb.ingestor.SaveEndpoints(ctx, service, doc.Endpoints); err != nil {
		return model.IngestStats{}, fmt.Errorf("save endpoints: %w", err)
	}

	chunks := knowledge.BuildChunks(doc.Endpoints, doc.Meta.Version)

	if err := kb.ingestor.SaveChunks(ctx, service, chunks); err != nil {
		return model.IngestStats{}, fmt.Errorf("save chunks: %w", err)
	}

	if err := kb.engine.Index(ctx, doc.Endpoints, doc.Meta.Version); err != nil {
		return model.IngestStats{}, fmt.Errorf("index chunks: %w", err)
	}

	return model.IngestStats{
		Endpoints: len(doc.Endpoints),
		Chunks:    len(chunks),
	}, nil
}

// IngestURL 录入远程 API 规范
func (kb *KnowledgeBase) IngestURL(ctx context.Context, url string) (model.IngestStats, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return model.IngestStats{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return model.IngestStats{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return model.IngestStats{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.IngestStats{}, err
	}

	return kb.IngestBytes(ctx, data, url)
}

// IngestFileDocument 录入文件文档
func (kb *KnowledgeBase) IngestFileDocument(ctx context.Context, path string, serviceOverride string) (knowledge.ParsedSpec, model.IngestStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	source := serviceOverride
	if source == "" {
		source = path
	}
	stats, err := kb.IngestBytes(ctx, data, source)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	doc, err := knowledge.ParseSwaggerDocumentBytes(data, source)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	return doc, stats, nil
}

// IngestURLDocument 录入 URL 文档
func (kb *KnowledgeBase) IngestURLDocument(ctx context.Context, url string, serviceOverride string) (knowledge.ParsedSpec, model.IngestStats, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	source := serviceOverride
	if source == "" {
		source = url
	}
	stats, err := kb.IngestBytes(ctx, data, source)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	doc, err := knowledge.ParseSwaggerDocumentBytes(data, source)
	if err != nil {
		return knowledge.ParsedSpec{}, model.IngestStats{}, err
	}
	return doc, stats, nil
}

// Search 搜索相关文档
func (kb *KnowledgeBase) Search(ctx context.Context, query string, topK int, service string) ([]rag.SearchResult, error) {
	return kb.engine.Search(ctx, query, topK, service)
}

// GetEndpoints 获取所有端点
func (kb *KnowledgeBase) GetEndpoints(ctx context.Context, service string) ([]model.Endpoint, error) {
	return kb.ingestor.ListEndpoints(ctx, service)
}

// GetEndpoint 获取单个端点
func (kb *KnowledgeBase) GetEndpoint(ctx context.Context, service, method, path string) (*model.Endpoint, error) {
	endpoints, err := kb.ingestor.ListEndpoints(ctx, service)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s:%s:%s", service, method, path)
	for i := range endpoints {
		if endpoints[i].Key() == key {
			return &endpoints[i], nil
		}
	}
	return nil, fmt.Errorf("endpoint not found: %s %s", method, path)
}

// GetSpec 获取 API 规范
func (kb *KnowledgeBase) GetSpec(ctx context.Context, service string) ([]byte, error) {
	return kb.ingestor.LoadSpec(ctx, service)
}

// GetSpecMeta 获取规范元数据
func (kb *KnowledgeBase) GetSpecMeta(ctx context.Context, service string) (*model.SpecMeta, error) {
	spec, err := kb.ingestor.LoadSpec(ctx, service)
	if err != nil {
		return nil, err
	}
	doc, err := knowledge.ParseSwaggerDocumentBytes(spec, service)
	if err != nil {
		return nil, err
	}
	return &doc.Meta, nil
}

// DeleteService 删除服务
func (kb *KnowledgeBase) DeleteService(ctx context.Context, service string) error {
	return kb.ingestor.DeleteService(ctx, service)
}
