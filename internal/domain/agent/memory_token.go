package agent

import "sync"

// TokenEstimator 估算文本的 token 数量。
type TokenEstimator func(text string) int

// DefaultTokenEstimator 使用 rune 数量作为粗略 token 估算。
func DefaultTokenEstimator(text string) int {
	return len([]rune(text))
}

// TokenWindowMemory 按 token 数量裁剪的 Memory 实现。
type TokenWindowMemory struct {
	mu        sync.RWMutex
	maxTokens int
	estimator TokenEstimator
	messages  []Message
}

func NewTokenWindowMemory(maxTokens int, estimator TokenEstimator) *TokenWindowMemory {
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	if estimator == nil {
		estimator = DefaultTokenEstimator
	}
	return &TokenWindowMemory{
		maxTokens: maxTokens,
		estimator: estimator,
		messages:  make([]Message, 0),
	}
}

func (m *TokenWindowMemory) Append(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if msg.Role == "tool" {
		maxSingle := m.maxTokens / 4
		if maxSingle > 0 && m.estimator(msg.Content) > maxSingle {
			runes := []rune(msg.Content)
			if len(runes) > maxSingle {
				msg.Content = string(runes[:maxSingle])
			}
		}
	}

	m.messages = append(m.messages, msg)
	m.trim()
}

func (m *TokenWindowMemory) Messages() []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *TokenWindowMemory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
}

func (m *TokenWindowMemory) totalTokens() int {
	total := 0
	for _, msg := range m.messages {
		total += m.estimator(msg.Content)
	}
	return total
}

func (m *TokenWindowMemory) trim() {
	if m.totalTokens() <= m.maxTokens {
		return
	}
	if len(m.messages) <= 1 {
		return
	}

	protected := make(map[int]bool)
	if len(m.messages) > 0 && m.messages[0].Role == "system" {
		protected[0] = true
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "user" {
			protected[i] = true
			break
		}
	}

	for m.totalTokens() > m.maxTokens {
		removed := false
		for i := 0; i < len(m.messages); i++ {
			if protected[i] {
				continue
			}
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			newProtected := make(map[int]bool)
			for k := range protected {
				if k < i {
					newProtected[k] = true
				} else if k > i {
					newProtected[k-1] = true
				}
			}
			protected = newProtected
			removed = true
			break
		}
		if !removed {
			break
		}
	}
}
