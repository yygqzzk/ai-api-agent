package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"wanzhi/internal/domain/tool"
)

type AdaptiveAgentEngineOptions struct {
	Selector         StrategySelector
	Rewriter         QueryRewriter
	Planner          Planner
	Reflector        Reflector
	MaxRetries       int
	QualityThreshold float64
}

type AdaptiveQueryRunner interface {
	Run(ctx context.Context, userQuery string) (string, error)
}

type AdaptiveAgentEngine struct {
	baseEngine       AdaptiveQueryRunner
	dispatcher       ToolDispatcher
	rewriter         QueryRewriter
	planner          Planner
	reflector        Reflector
	selector         StrategySelector
	maxRetries       int
	qualityThreshold float64
}

func NewAdaptiveAgentEngine(baseEngine AdaptiveQueryRunner, dispatcher ToolDispatcher, opts AdaptiveAgentEngineOptions) *AdaptiveAgentEngine {
	threshold := opts.QualityThreshold
	if threshold <= 0 {
		threshold = 0.7
	}
	selector := opts.Selector
	if selector == nil {
		selector = NewRuleBasedStrategySelector()
	}
	rewriter := opts.Rewriter
	if rewriter == nil {
		rewriter = NewRuleBasedQueryRewriter()
	}
	planner := opts.Planner
	if planner == nil {
		planner = NewRuleBasedPlanner()
	}
	reflector := opts.Reflector
	if reflector == nil {
		reflector = NewRuleBasedReflector(threshold)
	}
	maxRetries := opts.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &AdaptiveAgentEngine{
		baseEngine:       baseEngine,
		dispatcher:       dispatcher,
		rewriter:         rewriter,
		planner:          planner,
		reflector:        reflector,
		selector:         selector,
		maxRetries:       maxRetries,
		qualityThreshold: threshold,
	}
}

func (e *AdaptiveAgentEngine) Run(ctx context.Context, userQuery string) (string, error) {
	return e.runWithAttempt(ctx, userQuery, 0)
}

func (e *AdaptiveAgentEngine) runWithAttempt(ctx context.Context, userQuery string, attempt int) (string, error) {
	strategy, err := e.selector.Select(ctx, userQuery)
	if err != nil {
		return "", err
	}

	var result string
	switch strategy {
	case StrategyComplex:
		result, err = e.runComplex(ctx, userQuery)
	case StrategyAmbiguous:
		result, err = e.runAmbiguous(ctx, userQuery)
	default:
		result, err = e.runSimple(ctx, userQuery)
	}
	if err != nil {
		return "", err
	}

	reflection, err := e.reflector.Reflect(ctx, userQuery, result)
	if err != nil {
		return result, nil
	}
	if reflection.ShouldRetry && attempt < e.maxRetries {
		return e.runWithAttempt(ctx, applyImprovements(userQuery, reflection.Improvements), attempt+1)
	}
	return result, nil
}

func (e *AdaptiveAgentEngine) runSimple(ctx context.Context, query string) (string, error) {
	if e.baseEngine == nil {
		return "", fmt.Errorf("base engine is nil")
	}
	return e.baseEngine.Run(ctx, query)
}

func (e *AdaptiveAgentEngine) runAmbiguous(ctx context.Context, query string) (string, error) {
	queries, err := e.rewriter.Rewrite(ctx, query, RewriteStrategyClarify)
	if err != nil || len(queries) == 0 {
		return e.runSimple(ctx, query)
	}
	bestResult := ""
	bestQuality := -1.0
	for _, rewritten := range queries {
		result, runErr := e.baseEngine.Run(ctx, rewritten)
		if runErr != nil {
			continue
		}
		reflection, reflectErr := e.reflector.Reflect(ctx, query, result)
		quality := 0.0
		if reflectErr == nil && reflection != nil {
			quality = reflection.Quality
		}
		if quality > bestQuality {
			bestQuality = quality
			bestResult = result
		}
	}
	if bestResult == "" {
		return e.runSimple(ctx, query)
	}
	return bestResult, nil
}

func (e *AdaptiveAgentEngine) runComplex(ctx context.Context, query string) (string, error) {
	if e.dispatcher == nil {
		return "", fmt.Errorf("dispatcher is nil")
	}
	plan, err := e.planner.Plan(ctx, query)
	if err != nil {
		return "", err
	}
	results := make(map[string]any, len(plan.Tasks))
	for _, task := range plan.Tasks {
		if !dependenciesMet(task, results) {
			continue
		}
		result, execErr := e.executeTask(ctx, task, results)
		if execErr != nil {
			return "", execErr
		}
		results[task.ID] = result
	}
	return summarizePlanResults(plan, results), nil
}

func (e *AdaptiveAgentEngine) executeTask(ctx context.Context, task Task, results map[string]any) (any, error) {
	payload := make(map[string]any)
	if strings.TrimSpace(task.Args) != "" {
		if err := json.Unmarshal([]byte(task.Args), &payload); err != nil {
			return nil, fmt.Errorf("decode task args for %s: %w", task.ID, err)
		}
	}
	if ref, ok := payload["endpoint_ref"].(string); ok && payload["endpoint"] == nil {
		payload["endpoint"] = extractEndpointFromResult(results[ref])
		delete(payload, "endpoint_ref")
	}
	if refs, ok := payload["endpoints"].([]any); ok && payload["endpoint"] == nil {
		for i := len(refs) - 1; i >= 0; i-- {
			if ref, ok := refs[i].(string); ok {
				trimmed := strings.TrimSuffix(ref, ".result")
				if endpoint := extractEndpointFromResult(results[trimmed]); endpoint != "" {
					payload["endpoint"] = endpoint
					break
				}
			}
		}
		delete(payload, "endpoints")
	}
	args, _ := json.Marshal(payload)
	return e.dispatcher.Dispatch(ctx, task.Tool, args)
}

func dependenciesMet(task Task, results map[string]any) bool {
	for _, dep := range task.DependsOn {
		if _, ok := results[dep]; !ok {
			return false
		}
	}
	return true
}

func summarizePlanResults(plan *ExecutionPlan, results map[string]any) string {
	if plan == nil || len(plan.Tasks) == 0 {
		return "分析结果为空。"
	}
	lines := []string{"分析结果:"}
	for _, task := range plan.Tasks {
		result, ok := results[task.ID]
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("- 任务 %s（%s）: %s", task.ID, task.Description, formatTaskResult(result)))
	}
	return strings.Join(lines, "\n")
}

func formatTaskResult(result any) string {
	switch value := result.(type) {
	case tool.SearchAPIResult:
		items := make([]string, 0, len(value.Items))
		for _, item := range value.Items {
			items = append(items, item.Endpoint)
		}
		return "找到接口 " + strings.Join(items, ", ")
	case tool.AnalyzeDependenciesResult:
		return fmt.Sprintf("接口依赖 %s -> %s", value.Endpoint, strings.Join(value.Dependencies, ", "))
	case tool.APIDetailResult:
		return fmt.Sprintf("接口详情 %s %s", value.Endpoint.Method, value.Endpoint.Path)
	case string:
		return value
	default:
		body, _ := json.Marshal(value)
		return string(body)
	}
}

func extractEndpointFromResult(result any) string {
	switch value := result.(type) {
	case tool.SearchAPIResult:
		if len(value.Items) > 0 {
			return value.Items[0].Endpoint
		}
	case *tool.SearchAPIResult:
		if value != nil && len(value.Items) > 0 {
			return value.Items[0].Endpoint
		}
	case tool.APIDetailResult:
		return fmt.Sprintf("%s %s", value.Endpoint.Method, value.Endpoint.Path)
	case tool.AnalyzeDependenciesResult:
		return value.Endpoint
	case map[string]any:
		if endpoint, ok := value["endpoint"].(string); ok {
			return endpoint
		}
	}
	return ""
}

func applyImprovements(query string, improvements []string) string {
	parts := uniqueNonEmptyStrings(append([]string{query}, improvements...))
	sort.Strings(parts[1:])
	return strings.Join(parts, "；")
}
