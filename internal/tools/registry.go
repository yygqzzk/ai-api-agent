package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

type ToolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

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

func (r *Registry) Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return tool.Execute(ctx, args)
}

func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

func (r *Registry) ToolDefinitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      append(json.RawMessage(nil), tool.Schema()...),
		})
	}
	return defs
}

func RegisterDefaultTools(registry *Registry, kb *KnowledgeBase, skillDir string) error {
	tools := []Tool{
		NewSearchAPITool(kb),
		NewGetAPIDetailTool(kb),
		NewAnalyzeDependenciesTool(kb),
		NewGenerateExampleTool(kb),
		NewValidateParamsTool(kb),
		NewMatchSkillTool(skillDir),
		NewParseSwaggerTool(kb),
	}

	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func RegisterQueryTool(registry *Registry, runner QueryRunner) error {
	return registry.Register(NewQueryAPITool(runner))
}
