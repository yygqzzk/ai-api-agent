package tools

import (
	"context"
	"testing"

	"wanzhi/internal/knowledge"
	"wanzhi/internal/rag"
	"wanzhi/internal/store"

	"github.com/alicebob/miniredis/v2"
)

func setupRedisBackedKnowledgeBase(t *testing.T) (*KnowledgeBase, func()) {
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

	kb := NewKnowledgeBaseWithRedis(client, rag.NewMemoryStore())
	cleanup := func() {
		_ = client.Close(context.Background())
		server.Close()
	}
	return kb, cleanup
}

func TestGetEndpointFromRedisIngestor(t *testing.T) {
	kb, cleanup := setupRedisBackedKnowledgeBase(t)
	defer cleanup()

	stats, err := kb.upsertDocument(context.Background(), knowledge.ParsedSpec{
		Meta: knowledge.SpecMeta{
			Service:  "petstore",
			Host:     "petstore.swagger.io",
			BasePath: "/v2",
			Schemes:  []string{"https"},
		},
		Endpoints: []knowledge.Endpoint{{
			Service: "petstore",
			Method:  "GET",
			Path:    "/user/login",
			Summary: "login",
		}},
	})
	if err != nil {
		t.Fatalf("upsert endpoints failed: %v", err)
	}
	if stats.Endpoints != 1 {
		t.Fatalf("expected 1 endpoint upserted, got %d", stats.Endpoints)
	}

	got, ok := kb.GetEndpoint("petstore", "GET /user/login")
	if !ok {
		t.Fatalf("expected endpoint found")
	}
	if got.Path != "/user/login" || got.Method != "GET" {
		t.Fatalf("unexpected endpoint: %+v", got)
	}
}

func TestGetSpecMetaFromRedisIngestor(t *testing.T) {
	kb, cleanup := setupRedisBackedKnowledgeBase(t)
	defer cleanup()

	_, err := kb.upsertDocument(context.Background(), knowledge.ParsedSpec{
		Meta: knowledge.SpecMeta{
			Service:  "petstore",
			Host:     "petstore.swagger.io",
			BasePath: "/v2",
			Schemes:  []string{"https"},
		},
		Endpoints: []knowledge.Endpoint{{
			Service: "petstore",
			Method:  "GET",
			Path:    "/user/login",
			Summary: "login",
		}},
	})
	if err != nil {
		t.Fatalf("upsert endpoints failed: %v", err)
	}

	meta, ok := kb.GetSpecMeta("PETSTORE")
	if !ok {
		t.Fatalf("expected spec meta found")
	}
	if got := meta.URLForPath("/user/login"); got != "https://petstore.swagger.io/v2/user/login" {
		t.Fatalf("unexpected url from meta: %s", got)
	}
}
