package rag

import (
	"context"
	"testing"

	"ai-agent-api/internal/knowledge"
)

func TestMemoryStore_DeleteByIDs(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	chunks := []knowledge.Chunk{
		{ID: "svc:GET:/a:overview", Service: "svc", Endpoint: "GET /a", Type: "overview", Content: "a"},
		{ID: "svc:GET:/a:request", Service: "svc", Endpoint: "GET /a", Type: "request", Content: "b"},
		{ID: "svc:GET:/b:overview", Service: "svc", Endpoint: "GET /b", Type: "overview", Content: "c"},
		{ID: "svc:GET:/b:request", Service: "svc", Endpoint: "GET /b", Type: "request", Content: "d"},
	}
	if err := store.Upsert(ctx, chunks); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	if err := store.DeleteByIDs(ctx, []string{"svc:GET:/a:overview", "svc:GET:/a:request"}); err != nil {
		t.Fatalf("DeleteByIDs failed: %v", err)
	}

	remaining := store.AllChunks()
	if len(remaining) != 2 {
		t.Fatalf("expected 2 chunks remaining, got %d", len(remaining))
	}
	for _, chunk := range remaining {
		if chunk.Endpoint != "GET /b" {
			t.Fatalf("expected remaining chunks belong to GET /b, got %s", chunk.Endpoint)
		}
	}
}
