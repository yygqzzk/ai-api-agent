package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, false)

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Fatalf("expected JSON log output, got: %s", output)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got: %s", output)
	}
	if parsed["key"] != "value" {
		t.Fatalf("expected key=value in log, got: %v", parsed)
	}
}

func TestNewLoggerDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, true)

	logger.Debug("debug msg")

	if !strings.Contains(buf.String(), "debug msg") {
		t.Fatal("expected debug message in output when debug=true")
	}
}
