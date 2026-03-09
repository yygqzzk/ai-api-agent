package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Planner interface {
	Plan(ctx context.Context, query string) (*ExecutionPlan, error)
}

type ExecutionPlan struct {
	Tasks []Task `json:"tasks"`
}

type Task struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Tool        string   `json:"tool"`
	Args        string   `json:"args"`
	DependsOn   []string `json:"depends_on,omitempty"`
}

type RuleBasedPlanner struct{}

func NewRuleBasedPlanner() *RuleBasedPlanner {
	return &RuleBasedPlanner{}
}

func (p *RuleBasedPlanner) Plan(_ context.Context, query string) (*ExecutionPlan, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	topics := inferPlanTopics(query)
	if len(topics) == 0 {
		topics = []string{query}
	}
	tasks := make([]Task, 0, len(topics)+1)
	dependsOn := make([]string, 0, len(topics))
	for index, topic := range topics {
		id := fmt.Sprintf("task%d", index+1)
		tasks = append(tasks, Task{
			ID:          id,
			Description: fmt.Sprintf("查找%s接口", topic),
			Tool:        "search_api",
			Args:        mustJSONString(map[string]string{"query": topic}),
		})
		dependsOn = append(dependsOn, id)
	}
	if len(tasks) > 1 || strings.Contains(query, "流程") || strings.Contains(query, "依赖") || strings.Contains(query, "分析") {
		tasks = append(tasks, Task{
			ID:          fmt.Sprintf("task%d", len(tasks)+1),
			Description: "分析接口依赖关系",
			Tool:        "analyze_dependencies",
			Args:        mustJSONString(map[string]string{"endpoint_ref": dependsOn[len(dependsOn)-1]}),
			DependsOn:   append([]string(nil), dependsOn...),
		})
	}
	return &ExecutionPlan{Tasks: tasks}, nil
}

type LLMPlanner struct {
	llmClient LLMClient
	fallback  Planner
}

func NewLLMPlanner(llmClient LLMClient, fallback Planner) *LLMPlanner {
	if fallback == nil {
		fallback = NewRuleBasedPlanner()
	}
	return &LLMPlanner{llmClient: llmClient, fallback: fallback}
}

func (p *LLMPlanner) Plan(ctx context.Context, query string) (*ExecutionPlan, error) {
	if p.llmClient == nil {
		return p.fallback.Plan(ctx, query)
	}
	reply, err := p.llmClient.Next(ctx, []Message{
		{Role: "system", Content: "你是任务规划器。只返回 JSON：{\"tasks\":[...]}。每个 task 包含 id/description/tool/args/depends_on。"},
		{Role: "user", Content: query},
	}, nil)
	if err != nil {
		return p.fallback.Plan(ctx, query)
	}
	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(strings.TrimSpace(reply.Content)), &plan); err != nil || len(plan.Tasks) == 0 {
		return p.fallback.Plan(ctx, query)
	}
	return &plan, nil
}

func inferPlanTopics(query string) []string {
	topics := make([]string, 0, 4)
	if strings.Contains(query, "注册") {
		topics = append(topics, "用户注册")
	}
	if strings.Contains(query, "登录") || strings.Contains(query, "注册到下单") || strings.Contains(query, "下单") {
		topics = append(topics, "用户登录")
	}
	if strings.Contains(query, "商品") || strings.Contains(query, "下单") {
		topics = append(topics, "商品查询")
	}
	if strings.Contains(query, "订单") || strings.Contains(query, "下单") {
		topics = append(topics, "创建订单")
	}
	return uniqueNonEmptyStrings(topics)
}

func mustJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
