package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"wanzhi/internal/knowledge"
)

func TestServiceIngestFromContentURLAndFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "user-service.json")
	body := []byte(sampleSwaggerDocument(t, "User Service", "/users/login"))
	if err := os.WriteFile(filePath, body, 0o644); err != nil {
		t.Fatalf("write temp file failed: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	recorder := &stubIngestRecorder{}
	service := NewService(recorder, srv.Client())

	results, err := service.SyncFiles(context.Background(), []SyncFile{
		{Path: "docs/api/user-service.json", ContentURL: srv.URL + "/docs/api/user-service.json"},
		{Path: filePath},
	})
	if err != nil {
		t.Fatalf("SyncFiles failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(recorder.calls) != 2 {
		t.Fatalf("expected 2 ingest calls, got %d", len(recorder.calls))
	}
	if results[0].Service == "" || results[1].Service == "" {
		t.Fatalf("expected inferred service names, got %+v", results)
	}
}

func TestServiceIngestContent(t *testing.T) {
	recorder := &stubIngestRecorder{}
	service := NewService(recorder, http.DefaultClient)

	result, err := service.IngestContent(context.Background(), []byte(sampleSwaggerDocument(t, "Order Service", "/orders")), "order-service", "docs/api/order-service.json")
	if err != nil {
		t.Fatalf("IngestContent failed: %v", err)
	}
	if result.Endpoints == 0 || result.Chunks == 0 {
		t.Fatalf("expected non-empty ingest result, got %+v", result)
	}
}

type stubIngestRecorder struct {
	calls []stubIngestCall
	err   error
}

type stubIngestCall struct {
	service string
	bytes   int
}

func (s *stubIngestRecorder) IngestBytes(_ context.Context, content []byte, service string) (knowledge.IngestStats, error) {
	if s.err != nil {
		return knowledge.IngestStats{}, s.err
	}
	s.calls = append(s.calls, stubIngestCall{service: service, bytes: len(content)})
	endpoints, err := knowledge.ParseSwaggerBytes(content, service)
	if err != nil {
		return knowledge.IngestStats{}, err
	}
	return knowledge.IngestStats{Endpoints: len(endpoints), Chunks: len(endpoints) * 4}, nil
}

func sampleSwaggerDocument(t *testing.T, title string, path string) string {
	t.Helper()
	return fmt.Sprintf(`{"swagger":"2.0","info":{"title":%q,"version":"1.0.0"},"paths":{%q:{"get":{"summary":"query %s","responses":{"200":{"description":"ok"}}}}}}`, title, path, title)
}

var errStubIngest = errors.New("ingest failed")
