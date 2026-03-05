package tools

import (
	"context"
	"encoding/json"

	"ai-agent-api/internal/knowledge"
)

type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) (any, error)
}

type SearchAPIArgs struct {
	Query   string `json:"query"`
	TopK    int    `json:"top_k"`
	Service string `json:"service,omitempty"`
}

type SearchAPIItem struct {
	Service   string  `json:"service"`
	Endpoint  string  `json:"endpoint"`
	ChunkType string  `json:"chunk_type"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
}

type SearchAPIResult struct {
	Items []SearchAPIItem `json:"items"`
}

type APIDetailArgs struct {
	Service  string `json:"service"`
	Endpoint string `json:"endpoint"`
}

type APIDetail struct {
	Service     string                `json:"service"`
	Method      string                `json:"method"`
	Path        string                `json:"path"`
	Summary     string                `json:"summary"`
	Description string                `json:"description"`
	Tags        []string              `json:"tags"`
	Parameters  []knowledge.Parameter `json:"parameters"`
	Responses   []knowledge.Response  `json:"responses"`
}

type APIDetailResult struct {
	Endpoint APIDetail `json:"endpoint"`
}

type AnalyzeDependenciesArgs struct {
	Service  string `json:"service"`
	Endpoint string `json:"endpoint"`
}

type AnalyzeDependenciesResult struct {
	Service      string   `json:"service"`
	Endpoint     string   `json:"endpoint"`
	Dependencies []string `json:"dependencies"`
}

type GenerateExampleArgs struct {
	Service  string `json:"service"`
	Endpoint string `json:"endpoint"`
	Language string `json:"language"`
}

type GenerateExampleResult struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

type MatchSkillArgs struct {
	Query string `json:"query"`
}

type SkillTemplate struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	File        string   `json:"file"`
}

type MatchSkillResult struct {
	Skill SkillTemplate `json:"skill"`
	Score int           `json:"score"`
}

type ValidateParamsArgs struct {
	Service  string         `json:"service"`
	Endpoint string         `json:"endpoint"`
	Params   map[string]any `json:"params"`
}

type ValidateParamsResult struct {
	Valid           bool     `json:"valid"`
	MissingRequired []string `json:"missing_required"`
	UnknownParams   []string `json:"unknown_params"`
}

type ParseSwaggerArgs struct {
	FilePath string `json:"file_path,omitempty"`
	URL      string `json:"url,omitempty"`
	Service  string `json:"service,omitempty"`
}

type ParseSwaggerResult struct {
	Stats knowledge.IngestStats `json:"stats"`
}

type QueryAPIArgs struct {
	Query string `json:"query"`
}

type QueryAPIResult struct {
	Summary string           `json:"summary"`
	Trace   []QueryTraceItem `json:"trace,omitempty"`
}

type QueryTraceItem struct {
	Step      int    `json:"step"`
	Tool      string `json:"tool"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Preview   string `json:"preview"`
}
