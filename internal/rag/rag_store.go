// Package rag 实现 RAG (Retrieval-Augmented Generation) 检索增强生成系统
//
// # 设计理念
//
// RAG 系统通过检索相关文档来增强 LLM 的生成能力:
// 1. 将文档切分成小块 (Chunks)
// 2. 存储到向量数据库或内存
// 3. 根据查询检索最相关的文档块
// 4. 将检索结果提供给 LLM 生成答案
//
// # 核心设计模式
//
// 1. **Strategy Pattern (策略模式)** - Store 接口
//   - MemoryStore: 基于关键词匹配的内存存储
//   - MilvusStore: 基于向量相似度的 Milvus 存储
//   - 运行时可切换不同的存储后端
//
// 2. **Adapter Pattern (适配器模式)** - 统一接口
//   - 屏蔽不同存储后端的差异
//   - 提供统一的 Upsert/Search/Close 接口
//
// # 存储模式对比
//
// ## MemoryStore (内存模式)
// - **优势**: 无需外部依赖,启动快,适合开发和测试
// - **劣势**: 基于关键词匹配,召回率较低
// - **适用场景**: 开发环境,小规模数据,快速原型
//
// ## MilvusStore (向量模式)
// - **优势**: 基于语义相似度,召回率高,支持大规模数据
// - **劣势**: 需要 Milvus 和 Embedding API,启动慢
// - **适用场景**: 生产环境,大规模数据,高精度要求
//
// # 评分机制
//
// MemoryStore 使用加权关键词匹配:
// - Endpoint 匹配: +3 分 (最重要)
// - Content 匹配: +2 分
// - Type 匹配: +1 分
//
// MilvusStore 使用余弦相似度:
// - 范围: [0, 1]
// - 越接近 1 表示越相似
//
// # 并发安全性
//
// - MemoryStore: 使用 RWMutex 保护内部状态
// - MilvusStore: Milvus SDK 内部保证并发安全
package rag

import (
	"context"

	"ai-agent-api/internal/knowledge"
)

// ScoredChunk 带评分的文档块
// 用于排序和返回最相关的结果
type ScoredChunk struct {
	Chunk knowledge.Chunk // 文档块内容
	Score float64         // 相关性评分 (越高越相关)
}

// Store RAG 存储接口
//
// 抽象不同的存储后端 (Memory, Milvus):
// - Upsert: 插入或更新文档块
// - Search: 根据查询检索最相关的文档块
// - Close: 关闭连接和清理资源
//
// 设计考虑:
// - 使用接口而非具体类型,支持不同的存储实现
// - 所有方法都接受 context.Context,支持超时和取消
// - Search 支持 service 过滤,只检索特定服务的文档
type Store interface {
	// Upsert 插入或更新文档块
	// 如果 chunk.ID 已存在,则更新;否则插入
	Upsert(ctx context.Context, chunks []knowledge.Chunk) error

	// Search 检索最相关的文档块
	// 参数:
	// - query: 查询文本
	// - topK: 返回前 K 个最相关的结果
	// - service: 服务名称过滤 (空字符串表示不过滤)
	// 返回:
	// - []ScoredChunk: 按相关性降序排列的结果
	Search(ctx context.Context, query string, topK int, service string) ([]ScoredChunk, error)

	// Close 关闭存储连接
	// 清理资源,关闭数据库连接
	Close(ctx context.Context) error
}
