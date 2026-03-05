package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIClientEmbed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		resp := embeddingResponse{
			Data: make([]struct {
				Embedding []float32 `json:"embedding"`
			}, len(req.Input)),
		}
		for i := range req.Input {
			resp.Data[i].Embedding = make([]float32, 4)
			resp.Data[i].Embedding[0] = float32(i + 1)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewOpenAIClient("test-key", ts.URL, "text-embedding-3-small", 4)
	vectors, err := client.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if vectors[0][0] != 1.0 || vectors[1][0] != 2.0 {
		t.Fatalf("unexpected vectors: %v", vectors)
	}
}

func TestOpenAIClientEmbedError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer ts.Close()

	client := NewOpenAIClient("bad-key", ts.URL, "text-embedding-3-small", 4)
	_, err := client.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatalf("expected error for 401 response")
	}
}

func TestNoopClient(t *testing.T) {
	client := NewNoopClient(8)
	vectors, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vectors) != 2 || len(vectors[0]) != 8 {
		t.Fatalf("unexpected noop vectors: len=%d dim=%d", len(vectors), len(vectors[0]))
	}
	if client.Dimension() != 8 {
		t.Fatalf("expected dimension 8, got %d", client.Dimension())
	}
}
