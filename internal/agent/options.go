package agent

import "ai-agent-api/internal/observability"

// Option 配置 AgentEngine。
type Option func(*AgentEngine)

func WithMaxSteps(n int) Option {
	return func(e *AgentEngine) {
		if n > 0 {
			e.maxSteps = n
		}
	}
}

func WithSystemPrompt(prompt string) Option {
	return func(e *AgentEngine) {
		if prompt != "" {
			e.systemPrompt = prompt
		}
	}
}

func WithMemory(m Memory) Option {
	return func(e *AgentEngine) {
		if m != nil {
			e.memory = m
		}
	}
}

func WithHandlers(hs ...Handler) Option {
	return func(e *AgentEngine) {
		e.extraHandlers = append(e.extraHandlers, hs...)
	}
}

func WithMetrics(m *observability.Metrics) Option {
	return func(e *AgentEngine) {
		e.metrics = m
	}
}

func WithMiddleware(mws ...Middleware) Option {
	return func(e *AgentEngine) {
		e.middlewares = append(e.middlewares, mws...)
	}
}
