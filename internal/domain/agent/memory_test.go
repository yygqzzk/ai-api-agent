package agent

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestBufferMemoryAppendAndMessages(t *testing.T) {
	m := NewBufferMemory(4)
	m.Append(Message{Role: "system", Content: "sys"})
	m.Append(Message{Role: "user", Content: "u1"})
	m.Append(Message{Role: "assistant", Content: "a1"})

	msgs := m.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected first message role=system, got %s", msgs[0].Role)
	}
}

func TestBufferMemoryTrimPreservesSystem(t *testing.T) {
	m := NewBufferMemory(3)
	m.Append(Message{Role: "system", Content: "sys"})
	m.Append(Message{Role: "user", Content: "u1"})
	m.Append(Message{Role: "assistant", Content: "a1"})
	m.Append(Message{Role: "tool", Content: "t1"})
	m.Append(Message{Role: "assistant", Content: "a2"})

	msgs := m.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected trim to 3, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected system message retained, got %s", msgs[0].Role)
	}
}

func TestBufferMemoryReset(t *testing.T) {
	m := NewBufferMemory(10)
	m.Append(Message{Role: "user", Content: "u1"})
	m.Reset()
	if len(m.Messages()) != 0 {
		t.Fatal("expected empty after reset")
	}
}

func TestBufferMemoryMessagesCopiesSlice(t *testing.T) {
	m := NewBufferMemory(10)
	m.Append(Message{Role: "user", Content: "u1"})
	msgs := m.Messages()
	msgs[0].Content = "modified"
	if m.Messages()[0].Content == "modified" {
		t.Fatal("Messages() should return a copy")
	}
}

func TestBufferMemoryConcurrentAccess(t *testing.T) {
	m := NewBufferMemory(100)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m.Append(Message{Role: "user", Content: fmt.Sprintf("msg-%d-%d", id, j)})
				_ = m.Messages()
			}
		}(i)
	}
	wg.Wait()
}

func TestTokenWindowMemoryTrimsOnTokenLimit(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(20, estimator)

	m.Append(Message{Role: "system", Content: "sys"})
	m.Append(Message{Role: "user", Content: "question"})
	m.Append(Message{Role: "assistant", Content: "answer"})
	m.Append(Message{Role: "user", Content: "followup"})

	msgs := m.Messages()
	if msgs[0].Role != "system" {
		t.Fatal("system message must be preserved")
	}
	if msgs[len(msgs)-1].Content != "followup" {
		t.Fatal("latest user message must be preserved")
	}
	total := 0
	for _, msg := range msgs {
		total += estimator(msg.Content)
	}
	if total > 20 {
		t.Fatalf("total tokens %d exceeds limit 20", total)
	}
}

func TestTokenWindowMemoryPreservesSystemAndLastUser(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(15, estimator)

	m.Append(Message{Role: "system", Content: "system prompt"})
	m.Append(Message{Role: "user", Content: "q"})

	msgs := m.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestTokenWindowMemoryTruncatesLongToolResult(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(100, estimator)

	m.Append(Message{Role: "system", Content: "sys"})
	m.Append(Message{Role: "user", Content: "q"})
	longResult := strings.Repeat("x", 60)
	m.Append(Message{Role: "tool", Content: longResult})

	msgs := m.Messages()
	for _, msg := range msgs {
		if msg.Role == "tool" && len([]rune(msg.Content)) > 25 {
			t.Fatalf("tool result should be truncated, got len=%d", len([]rune(msg.Content)))
		}
	}
}

func TestTokenWindowMemoryOnlySystemMessage(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(2, estimator)
	m.Append(Message{Role: "system", Content: "long system prompt"})

	msgs := m.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestTokenWindowMemoryConcurrentAccess(t *testing.T) {
	estimator := func(text string) int { return len([]rune(text)) }
	m := NewTokenWindowMemory(1000, estimator)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m.Append(Message{Role: "user", Content: fmt.Sprintf("msg-%d-%d", id, j)})
				_ = m.Messages()
			}
		}(i)
	}
	wg.Wait()
}
