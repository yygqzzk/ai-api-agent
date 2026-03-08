package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ai-agent-api/internal/knowledge"
	"ai-agent-api/internal/rag"
)

func TestRegistryAndCoreTools(t *testing.T) {
	registry := setupRegistry(t)

	searchOut := dispatch[SearchAPIResult](t, registry, "search_api", map[string]any{
		"query": "login",
		"top_k": 3,
	})
	if len(searchOut.Items) == 0 {
		t.Fatalf("expected search result items")
	}

	detailOut := dispatch[APIDetailResult](t, registry, "get_api_detail", map[string]any{
		"service":  "petstore",
		"endpoint": "GET /user/login",
	})
	if detailOut.Endpoint.Method != "GET" || detailOut.Endpoint.Path != "/user/login" {
		t.Fatalf("unexpected api detail endpoint: %+v", detailOut.Endpoint)
	}
	if detailOut.Endpoint.Spec.Host != "petstore.swagger.io" {
		t.Fatalf("expected spec host petstore.swagger.io, got %+v", detailOut.Endpoint.Spec)
	}
	if detailOut.Endpoint.Spec.BasePath != "/v2" {
		t.Fatalf("expected spec basePath /v2, got %+v", detailOut.Endpoint.Spec)
	}

	exampleOut := dispatch[GenerateExampleResult](t, registry, "generate_example", map[string]any{
		"service":  "petstore",
		"endpoint": "GET /user/login",
		"language": "go",
	})
	if !strings.Contains(exampleOut.Code, "http.NewRequest") {
		t.Fatalf("expected go sample include http.NewRequest, got: %s", exampleOut.Code)
	}
	if !strings.Contains(exampleOut.Code, "https://petstore.swagger.io/v2/user/login") {
		t.Fatalf("expected generated example use spec meta url, got: %s", exampleOut.Code)
	}

	validateOut := dispatch[ValidateParamsResult](t, registry, "validate_params", map[string]any{
		"service":  "petstore",
		"endpoint": "GET /user/login",
		"params": map[string]any{
			"username": "u1",
		},
	})
	if validateOut.Valid {
		t.Fatalf("expected params invalid when password missing")
	}
	if len(validateOut.MissingRequired) == 0 || validateOut.MissingRequired[0] != "password" {
		t.Fatalf("expected missing password, got %+v", validateOut.MissingRequired)
	}

	skillOut := dispatch[MatchSkillResult](t, registry, "match_skill", map[string]any{
		"query": "pet crud",
	})
	if skillOut.Skill.Name == "" {
		t.Fatalf("expected matched skill")
	}

	depsOut := dispatch[AnalyzeDependenciesResult](t, registry, "analyze_dependencies", map[string]any{
		"service":  "petstore",
		"endpoint": "POST /store/order",
	})
	if len(depsOut.Dependencies) == 0 {
		t.Fatalf("expected non-empty dependencies")
	}
}

func TestParseSwaggerTool(t *testing.T) {
	registry := setupRegistry(t)
	petstorePath := filepath.Join("..", "..", "testdata", "petstore.json")
	out := dispatch[ParseSwaggerResult](t, registry, "parse_swagger", map[string]any{
		"file_path": petstorePath,
		"service":   "petstore",
	})
	if out.Stats.Endpoints != 3 {
		t.Fatalf("expected 3 endpoints, got %d", out.Stats.Endpoints)
	}
	if out.Stats.Chunks == 0 {
		t.Fatalf("expected chunks > 0")
	}
	if out.Spec.Host != "petstore.swagger.io" {
		t.Fatalf("expected parsed spec host, got %+v", out.Spec)
	}
}

func TestRegistryToolDefinitions(t *testing.T) {
	registry := setupRegistry(t)
	defs := registry.ToolDefinitions()
	if len(defs) == 0 {
		t.Fatalf("expected non-empty tool definitions")
	}

	found := false
	for _, d := range defs {
		if d.Name == "search_api" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected search_api in tool definitions")
	}
}

func setupRegistry(t *testing.T) *Registry {
	t.Helper()
	petstorePath := filepath.Join("..", "..", "testdata", "petstore.json")
	skillDir := filepath.Join("..", "..", "skills")

	kb := NewKnowledgeBaseWithIngestor(knowledge.NewInMemoryIngestor(), rag.NewMemoryStore())
	if _, err := kb.IngestFile(context.Background(), petstorePath, "petstore"); err != nil {
		t.Fatalf("ingest file failed: %v", err)
	}

	registry := NewRegistry()
	if err := RegisterDefaultTools(registry, kb, skillDir); err != nil {
		t.Fatalf("register default tools failed: %v", err)
	}
	return registry
}

func dispatch[T any](t *testing.T, registry *Registry, toolName string, args any) T {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args failed: %v", err)
	}

	out, err := registry.Dispatch(context.Background(), toolName, body)
	if err != nil {
		t.Fatalf("dispatch %s failed: %v", toolName, err)
	}

	val, ok := out.(T)
	if !ok {
		t.Fatalf("unexpected response type for %s: %T", toolName, out)
	}
	return val
}
