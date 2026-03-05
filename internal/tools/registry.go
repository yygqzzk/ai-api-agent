// Package tools 提供工具注册和调度机制
//
// # 设计理念
//
// 工具系统采用注册表模式 (Registry Pattern),实现工具的动态注册和调度:
// 1. 工具通过 Tool 接口统一定义
// 2. Registry 负责工具的注册、查找和调度
// 3. 支持运行时动态添加工具
//
// # 核心设计模式
//
// 1. **Registry Pattern (注册表模式)**
//   - 集中管理所有可用工具
//   - 提供统一的工具查找和调度接口
//   - 支持工具的动态注册和发现
//
// 2. **Strategy Pattern (策略模式)**
//   - Tool 接口定义工具的统一行为
//   - 每个工具实现不同的执行策略
//   - 运行时根据工具名称选择具体实现
//
// # 并发安全性
//
// Registry 使用 sync.RWMutex 保护内部状态:
// - Register: 写锁,修改工具映射表
// - Dispatch/Has/ToolDefinitions: 读锁,并发安全
//
// 性能考虑:
// - 读多写少场景,RWMutex 提供更好的并发性能
// - 工具注册通常在启动时完成,运行时只读取
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Registry 工具注册表
//
// 职责:
// 1. 管理工具的注册和注销
// 2. 根据工具名称分发调用
// 3. 提供工具定义查询
//
// 并发安全性:
// - 使用 RWMutex 保护 tools 映射表
// - 支持并发读取 (Dispatch, Has, ToolDefinitions)
// - 写入操作 (Register) 需要独占锁
//
// 使用示例:
//
//	registry := NewRegistry()
//	registry.Register(NewSearchAPITool(kb))
//	registry.Register(NewGetAPIDetailTool(kb))
//	result, err := registry.Dispatch(ctx, "search_api", args)
type Registry struct {
	mu    sync.RWMutex   // 保护 tools 映射表的读写锁
	tools map[string]Tool // 工具名称 -> 工具实现的映射表
}

// ToolDefinition 工具定义
// 用于向 LLM 描述工具的功能和参数
//
// 字段说明:
// - Name: 工具名称 (唯一标识符)
// - Description: 工具功能描述 (帮助 LLM 理解何时使用该工具)
// - Schema: JSON Schema 格式的参数定义 (描述工具接受的参数)
type ToolDefinition struct {
	Name        string          // 工具名称
	Description string          // 工具描述
	Schema      json.RawMessage // 参数 Schema (JSON Schema 格式)
}

// NewRegistry 创建工具注册表
//
// 返回一个空的注册表,需要通过 Register 方法添加工具
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register 注册工具
//
// 参数:
// - tool: 实现 Tool 接口的工具实例
//
// 返回:
// - error: 如果工具名称为空或已存在,返回错误
//
// 并发安全性:
// - 使用写锁保护,不支持并发注册
// - 通常在启动时顺序注册所有工具
//
// 错误处理:
// - 工具名称为空: 返回错误
// - 工具名称重复: 返回错误 (防止意外覆盖)
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

// Dispatch 分发工具调用
//
// 参数:
// - ctx: 上下文 (用于超时控制和取消)
// - name: 工具名称
// - args: 工具参数 (JSON 格式)
//
// 返回:
// - any: 工具执行结果 (具体类型由工具决定)
// - error: 工具不存在或执行失败时返回错误
//
// 并发安全性:
// - 使用读锁保护,支持并发调用
// - 多个 goroutine 可以同时调用不同的工具
//
// 错误处理:
// - 工具不存在: 返回 "tool not found" 错误
// - 工具执行失败: 返回工具的执行错误
func (r *Registry) Dispatch(ctx context.Context, name string, args json.RawMessage) (any, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return tool.Execute(ctx, args)
}

// Has 检查工具是否存在
//
// 参数:
// - name: 工具名称
//
// 返回:
// - bool: 工具是否已注册
//
// 并发安全性:
// - 使用读锁保护,支持并发查询
//
// 使用场景:
// - Agent 在调用工具前验证工具是否存在
// - 避免调用不存在的工具导致运行时错误
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// ToolDefinitions 获取所有工具定义
//
// 返回:
// - []ToolDefinition: 所有已注册工具的定义列表
//
// 并发安全性:
// - 使用读锁保护,支持并发查询
// - 返回的切片是新分配的,修改不会影响注册表
//
// 使用场景:
// - 将工具定义传递给 LLM,让 LLM 知道有哪些工具可用
// - 生成工具文档
//
// 注意:
// - Schema 字段使用深拷贝,避免外部修改影响注册表
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

// RegisterDefaultTools 注册默认工具集
//
// 参数:
// - registry: 工具注册表
// - kb: 知识库实例 (提供 API 文档查询能力)
// - skillDir: 技能目录路径 (用于技能匹配工具)
//
// 返回:
// - error: 如果任何工具注册失败,返回错误
//
// 默认工具列表:
// 1. search_api: 搜索 API 接口
// 2. get_api_detail: 获取 API 详情
// 3. analyze_dependencies: 分析 API 依赖关系
// 4. generate_example: 生成 API 调用示例
// 5. validate_params: 验证 API 参数
// 6. match_skill: 匹配技能文档
// 7. parse_swagger: 解析 Swagger 文档
//
// 使用场景:
// - 服务启动时初始化工具注册表
// - 提供标准的 API 查询能力
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

// RegisterQueryTool 注册查询工具
//
// 参数:
// - registry: 工具注册表
// - runner: Agent 引擎实例 (实现 QueryRunner 接口)
//
// 返回:
// - error: 如果注册失败,返回错误
//
// 设计考虑:
// - query_api 工具需要 Agent 引擎支持,因此单独注册
// - 避免循环依赖: Agent 依赖 Registry,Registry 不直接依赖 Agent
// - 使用 QueryRunner 接口解耦,提高可测试性
//
// 使用场景:
// - 在 Agent 引擎创建后注册 query_api 工具
// - 提供自然语言查询 API 的能力
func RegisterQueryTool(registry *Registry, runner QueryRunner) error {
	return registry.Register(NewQueryAPITool(runner))
}
