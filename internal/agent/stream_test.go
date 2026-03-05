package agent

import (
	"context"
	"encoding/json"
	"testing"
)

type collectingDispatcher struct {
	results map[string]any
}

func (d *collectingDispatcher) Dispatch(_ context.Context, name string, _ json.RawMessage) (any, error) {
	if v, ok := d.results[name]; ok {
		return v, nil
	}
	return map[string]string{"result": "ok"}, nil
}

func (d *collectingDispatcher) Has(name string) bool {
	_, ok := d.results[name]
	return ok
}

func TestRunStreamEmitsEvents(t *testing.T) {
	dispatcher := &collectingDispatcher{
		results: map[string]any{
			"search_api": map[string]any{
				"items": []map[string]any{
					{"endpoint": "GET /pets"},
				},
			},
			"get_api_detail": map[string]any{
				"endpoint": map[string]any{"method": "GET", "path": "/pets"},
			},
		},
	}
	engine := NewAgentEngine(NewRuleBasedLLMClient(), dispatcher, WithMaxSteps(5))

	ctx := context.Background()
	ch := engine.RunStream(ctx, "查询宠物接口")

	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	kinds := make(map[EventKind]int)
	for _, ev := range events {
		kinds[ev.Kind]++
	}
	if kinds[EventStepStart] == 0 {
		t.Error("missing agent.step.start events")
	}
	if kinds[EventComplete] != 1 {
		t.Errorf("expected exactly one agent.complete event, got %d", kinds[EventComplete])
	}
}

func TestRunStreamErrorEvent(t *testing.T) {
	dispatcher := &collectingDispatcher{results: map[string]any{}}
	engine := NewAgentEngine(NewRuleBasedLLMClient(), dispatcher, WithMaxSteps(5))

	ch := engine.RunStream(context.Background(), "查询接口")

	var lastEvent AgentEvent
	for ev := range ch {
		lastEvent = ev
	}

	if lastEvent.Kind != EventError {
		t.Errorf("expected last event to be error, got %s", lastEvent.Kind)
	}
}
