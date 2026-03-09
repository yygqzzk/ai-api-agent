package model

// Chunk 表示文档的一个语义分块
type Chunk struct {
	ID       string
	Service  string
	Endpoint string
	Type     string // ChunkType 的字符串表示
	Content  string
	Version  string
}

// ChunkType 分块类型常量
const (
	ChunkTypeOverview   = "overview"
	ChunkTypeRequest    = "request"
	ChunkTypeResponse   = "response"
	ChunkTypeDependency = "dependency"
)
