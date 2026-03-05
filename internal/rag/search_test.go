package rag

import (
	"context"
	"testing"

	"ai-agent-api/internal/knowledge"
)

func TestBuildChunks(t *testing.T) {
	endpoints := []knowledge.Endpoint{
		{
			Service: "petstore",
			Method:  "GET",
			Path:    "/user/login",
			Summary: "user login",
			Parameters: []knowledge.Parameter{
				{Name: "username", Type: "string", Required: true},
			},
			Responses: []knowledge.Response{{StatusCode: "200", Description: "ok"}},
		},
	}

	chunks := BuildChunks(endpoints, "v1")
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}

	types := map[string]bool{}
	for _, c := range chunks {
		types[c.Type] = true
	}
	for _, want := range []string{"overview", "request", "response", "dependency"} {
		if !types[want] {
			t.Fatalf("missing chunk type %q", want)
		}
	}
}

func TestSearchRanking(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	chunks := []knowledge.Chunk{
		{ID: "1", Service: "petstore", Endpoint: "GET /user/login", Type: "overview", Content: "login user account"},
		{ID: "2", Service: "petstore", Endpoint: "POST /pet", Type: "overview", Content: "add pet"},
		{ID: "3", Service: "petstore", Endpoint: "POST /store/order", Type: "overview", Content: "place order"},
	}
	if err := store.Upsert(ctx, chunks); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	results, err := store.Search(ctx, "login", 2, "petstore")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least 1 result")
	}
	if results[0].Chunk.Endpoint != "GET /user/login" {
		t.Fatalf("expected login endpoint first, got %s", results[0].Chunk.Endpoint)
	}

	filtered, err := store.Search(ctx, "order", 5, "unknown")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected empty results for unknown service, got %d", len(filtered))
	}
}
