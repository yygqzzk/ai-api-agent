package knowledge

import (
	"context"
	"wanzhi/internal/domain/model"
)

// Ingestor 知识录入接口
// 抽象不同的持久化实现（Redis、内存等）
type Ingestor interface {
	// UpsertDocument 插入或更新 API 规范文档
	UpsertDocument(doc ParsedSpec) IngestStats

	// Endpoints 返回所有端点
	Endpoints() []model.Endpoint

	// Chunks 返回所有文档分块
	Chunks() []model.Chunk

	// ChunkIDs 返回指定服务的文档分块 ID 列表
	ChunkIDs(service string) []string

	// SpecMeta 返回指定服务的元数据
	SpecMeta(service string) (model.SpecMeta, bool)

	// SaveSpec 保存 API 规范到持久化存储（可选实现）
	SaveSpec(ctx context.Context, service string, spec []byte) error

	// LoadSpec 从持久化存储加载 API 规范（可选实现）
	LoadSpec(ctx context.Context, service string) ([]byte, error)

	// DeleteService 删除指定服务的所有数据
	DeleteService(ctx context.Context, service string) error

	// ListEndpoints 列出指定服务的所有端点
	ListEndpoints(ctx context.Context, service string) ([]model.Endpoint, error)

	// SaveEndpoints 保存端点列表
	SaveEndpoints(ctx context.Context, service string, endpoints []model.Endpoint) error

	// SaveChunks 保存文档分块
	SaveChunks(ctx context.Context, service string, chunks []model.Chunk) error

	// LoadChunks 加载文档分块
	LoadChunks(ctx context.Context, service string) ([]model.Chunk, error)
}
