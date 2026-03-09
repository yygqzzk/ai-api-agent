package agent

import "sync"

// Memory 管理 agent 对话上下文
type Memory interface {
	Append(msg Message)
	Messages() []Message
	Reset()
}

// BufferMemory 按消息数裁剪的 Memory 实现
// 裁剪策略：保留第一条 system 消息 + 最新的 N-1 条消息
type BufferMemory struct {
	mu          sync.RWMutex
	maxMessages int
	messages    []Message
}

func NewBufferMemory(maxMessages int) *BufferMemory {
	if maxMessages <= 0 {
		maxMessages = 32
	}
	return &BufferMemory{
		maxMessages: maxMessages,
		messages:    make([]Message, 0, maxMessages),
	}
}

func (m *BufferMemory) Append(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	m.trim()
}

func (m *BufferMemory) Messages() []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *BufferMemory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
}

func (m *BufferMemory) trim() {
	if len(m.messages) <= m.maxMessages {
		return
	}
	keep := make([]Message, 0, m.maxMessages)
	if len(m.messages) > 0 && m.messages[0].Role == "system" {
		keep = append(keep, m.messages[0])
	}
	tailSize := m.maxMessages - len(keep)
	if tailSize <= 0 {
		m.messages = keep[:m.maxMessages]
		return
	}
	start := len(m.messages) - tailSize
	if start < len(keep) {
		start = len(keep)
	}
	keep = append(keep, m.messages[start:]...)
	m.messages = keep
}
