package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-agent-api/internal/ingest"
)

func TestWebhookHandlerAcceptsBearerTokenAndProcessesFiles(t *testing.T) {
	service := &stubSyncService{results: []ingest.Result{{File: "user-service.json", Service: "user-service", Status: "success"}}}
	handler := NewHandler(service, HandlerOptions{BearerToken: "demo-token", ProcessAsync: false})

	body := mustJSON(t, map[string]any{
		"event":      "push",
		"repository": "company/api-docs",
		"branch":     "main",
		"files": []map[string]any{{
			"path":    "docs/api/user-service.json",
			"action":  "modified",
			"content": `{"swagger":"2.0","info":{"title":"User Service","version":"1.0.0"},"paths":{}}`,
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook/sync", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer demo-token")
	rr := httptest.NewRecorder()

	handler.HandleSync(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(service.calls) != 1 {
		t.Fatalf("expected 1 sync call, got %d", len(service.calls))
	}
	var response SyncResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if response.Status != "success" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestWebhookHandlerRejectsUnauthorizedRequest(t *testing.T) {
	handler := NewHandler(&stubSyncService{}, HandlerOptions{BearerToken: "demo-token", ProcessAsync: false})
	req := httptest.NewRequest(http.MethodPost, "/webhook/sync", bytes.NewReader([]byte(`{"files":[]}`)))
	rr := httptest.NewRecorder()

	handler.HandleSync(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

type stubSyncService struct {
	calls   [][]ingest.SyncFile
	results []ingest.Result
	err     error
}

func (s *stubSyncService) SyncFiles(_ context.Context, files []ingest.SyncFile) ([]ingest.Result, error) {
	cloned := append([]ingest.SyncFile(nil), files...)
	s.calls = append(s.calls, cloned)
	return append([]ingest.Result(nil), s.results...), s.err
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}
	return b
}
