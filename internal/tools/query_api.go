package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type QueryRunner interface {
	Run(ctx context.Context, userQuery string) (string, error)
}

type QueryAPITool struct {
	runner QueryRunner
}

func NewQueryAPITool(runner QueryRunner) *QueryAPITool {
	return &QueryAPITool{runner: runner}
}

func (t *QueryAPITool) Name() string {
	return "query_api"
}

func (t *QueryAPITool) Description() string {
	return "自然语言查询入口，走 Agent Loop 多工具编排"
}

func (t *QueryAPITool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`)
}

func (t *QueryAPITool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var req QueryAPIArgs
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode query_api args: %w", err)
	}
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	summary, err := t.runner.Run(ctx, req.Query)
	if err != nil {
		return nil, err
	}
	return QueryAPIResult{
		Summary: summary,
		Trace:   parseTraceFromSummary(summary),
	}, nil
}

var traceLinePattern = regexp.MustCompile(`^\d+\.\s+step=(\d+)\s+tool=([^\s]+)\s+status=([^\s]+)\s+latency=(\d+)ms\s+preview=(.*)$`)

func parseTraceFromSummary(summary string) []QueryTraceItem {
	idx := strings.Index(summary, "工具调用轨迹:")
	if idx < 0 {
		return nil
	}
	section := summary[idx+len("工具调用轨迹:"):]
	lines := strings.Split(section, "\n")
	items := make([]QueryTraceItem, 0)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := traceLinePattern.FindStringSubmatch(line)
		if len(m) != 6 {
			continue
		}
		step, err1 := strconv.Atoi(m[1])
		latency, err2 := strconv.ParseInt(m[4], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		items = append(items, QueryTraceItem{
			Step:      step,
			Tool:      m[2],
			Status:    m[3],
			LatencyMS: latency,
			Preview:   m[5],
		})
	}
	return items
}
