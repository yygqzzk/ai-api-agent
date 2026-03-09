package model

import "fmt"

// Endpoint 表示一个 API 接口端点
// Deprecated 标记该接口是否已废弃（从 OpenAPI deprecated 属性解析）
type Endpoint struct {
	Service     string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Parameters  []Parameter
	Responses   []Response
}

// Key 返回端点的唯一标识符
func (e Endpoint) Key() string {
	return fmt.Sprintf("%s:%s:%s", e.Service, e.Method, e.Path)
}

// DisplayName 返回端点的显示名称
func (e Endpoint) DisplayName() string {
	return fmt.Sprintf("%s %s", e.Method, e.Path)
}

// Parameter 表示接口参数
type Parameter struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Description string
	SchemaRef   string
}

// Response 表示接口响应
type Response struct {
	StatusCode  string
	Description string
}
