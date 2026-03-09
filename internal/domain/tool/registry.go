// Package tools 提供工具注册、查找和调度能力。
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Registry 保存可调用工具并负责按名称分发。
type Registry struct {
	mu    sync.RWMutex    // 保护 tools 映射表的读写锁
	tools map[string]Tool // 工具名称 -> 工具实现的映射表
}

// ToolDefinition 描述工具名称、用途和参数 schema。
type ToolDefinition struct {
	Name        string          // 工具名称
	Description string          // 工具描述
	Schema      json.RawMessage // 参数 Schema (JSON Schema 格式)
}

// NewRegistry 返回空的工具注册表。
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register 将工具加入注册表；同名工具会报错。
func (r *Registry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = tool
	return nil
}

// Dispatch 按名称执行已注册工具。
func (r *Registry) Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return tool.Execute(ctx, args)
}

// Has 判断工具是否已注册。
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// ToolDefinitions 返回当前工具定义的快照。
func (r *Registry) ToolDefinitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      append(json.RawMessage(nil), tool.Schema()...), // 深拷贝
		})
	}
	return defs
}

// RegisterDefaultTools 注册服务启动时默认暴露的工具。
func RegisterDefaultTools(registry *Registry, kb *KnowledgeBase, skillDir string) error {
	tools := []Tool{
		NewSearchAPITool(kb),
		NewGetAPIDetailTool(kb),
		NewAnalyzeDependenciesTool(kb),
		NewGenerateExampleTool(kb),
		NewValidateParamsTool(kb),
		NewParseSwaggerTool(kb),
	}

	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

// RegisterQueryTool 在 Agent 就绪后注册 `query_api`。
func RegisterQueryTool(registry *Registry, runner QueryRunner) error {
	return registry.Register(NewQueryAPITool(runner))
}
