package agent

import "encoding/json"

type EventKind string

const (
	EventStepStart EventKind = "agent.step.start"
	EventToolEnd   EventKind = "agent.tool.end"
	EventLLMStart  EventKind = "agent.llm.start"
	EventLLMEnd    EventKind = "agent.llm.end"
	EventComplete  EventKind = "agent.complete"
	EventError     EventKind = "agent.error"
)

type AgentEvent struct {
	Kind    EventKind       `json:"kind"`
	Step    int             `json:"step,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Content string          `json:"content,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}
