package knowledge

import (
	"context"
	"testing"

	"ai-agent-api/internal/store"

	"github.com/alicebob/miniredis/v2"
)

func setupTestRedisClient(t *testing.T) (store.RedisClient, func()) {
	t.Helper()

	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}

	client, err := store.NewRedisClient(store.RedisOptions{
		Mode:    "redis",
		Address: server.Addr(),
	})
	if err != nil {
		server.Close()
		t.Fatalf("new redis client failed: %v", err)
	}

	cleanup := func() {
		_ = client.Close(context.Background())
		server.Close()
	}
	return client, cleanup
}

func sampleParsedSpec(service string, method string, path string) ParsedSpec {
	return ParsedSpec{
		Meta: SpecMeta{
			Service:  service,
			Host:     service + ".example.com",
			BasePath: "/v1",
			Schemes:  []string{"https"},
		},
		Endpoints: []Endpoint{{
			Service: service,
			Method:  method,
			Path:    path,
			Summary: "summary " + path,
		}},
	}
}

func TestRedisIngestor_UpsertDocument(t *testing.T) {
	client, cleanup := setupTestRedisClient(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)
	doc := ParsedSpec{
		Meta: SpecMeta{
			Service:  "petstore",
			Host:     "petstore.swagger.io",
			BasePath: "/v2",
			Schemes:  []string{"https"},
		},
		Endpoints: []Endpoint{
			{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "User login"},
			{Service: "petstore", Method: "POST", Path: "/user/register", Summary: "User register"},
		},
	}

	stats := ingestor.UpsertDocument(doc)
	if stats.Endpoints != 2 {
		t.Fatalf("expected 2 endpoints upserted, got %d", stats.Endpoints)
	}
	if stats.Chunks != 8 {
		t.Fatalf("expected 8 chunks, got %d", stats.Chunks)
	}

	endpoints := ingestor.Endpoints()
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}

	meta, ok := ingestor.SpecMeta("petstore")
	if !ok {
		t.Fatalf("expected spec meta found")
	}
	if meta.Host != "petstore.swagger.io" {
		t.Fatalf("expected petstore host, got %q", meta.Host)
	}

	chunks := ingestor.Chunks()
	if len(chunks) != 8 {
		t.Fatalf("expected 8 chunks, got %d", len(chunks))
	}
}

func TestRedisIngestor_Endpoints(t *testing.T) {
	client, cleanup := setupTestRedisClient(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)
	ingestor.UpsertDocument(sampleParsedSpec("svc1", "GET", "/api/v1"))
	ingestor.UpsertDocument(sampleParsedSpec("svc2", "POST", "/api/v2"))

	endpoints := ingestor.Endpoints()
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}
}

func TestRedisIngestor_SpecMeta(t *testing.T) {
	client, cleanup := setupTestRedisClient(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)
	ingestor.UpsertDocument(sampleParsedSpec("petstore", "GET", "/users"))

	meta, ok := ingestor.SpecMeta("petstore")
	if !ok {
		t.Fatalf("expected meta found")
	}
	if meta.Service != "petstore" || meta.BasePath != "/v1" {
		t.Fatalf("unexpected meta: %+v", meta)
	}

	_, ok = ingestor.SpecMeta("missing")
	if ok {
		t.Fatalf("expected missing service not found")
	}
}

func TestRedisIngestor_Chunks(t *testing.T) {
	client, cleanup := setupTestRedisClient(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)
	ingestor.UpsertDocument(sampleParsedSpec("svc", "GET", "/api"))

	chunks := ingestor.Chunks()
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}
	if chunks[0].Service != "svc" {
		t.Fatalf("expected chunk service svc, got %q", chunks[0].Service)
	}
}
