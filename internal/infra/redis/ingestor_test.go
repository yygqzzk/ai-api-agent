package redis

import (
	"context"
	"testing"

	"wanzhi/internal/domain/knowledge"
	"wanzhi/internal/domain/model"

	"github.com/alicebob/miniredis/v2"
)

func setupTestRedisClient(t *testing.T) (RedisClient, func()) {
	t.Helper()

	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}

	client, err := NewRedisClient(RedisOptions{
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

func sampleParsedSpec(service string, method string, path string) knowledge.ParsedSpec {
	return knowledge.ParsedSpec{
		Meta: model.SpecMeta{
			Service:  service,
			Host:     service + ".example.com",
			BasePath: "/v1",
			Schemes:  []string{"https"},
		},
		Endpoints: []model.Endpoint{{
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
	doc := knowledge.ParsedSpec{
		Meta: model.SpecMeta{
			Service:  "petstore",
			Host:     "petstore.swagger.io",
			BasePath: "/v2",
			Schemes:  []string{"https"},
		},
		Endpoints: []model.Endpoint{
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

func TestRedisIngestor_FullReplacement(t *testing.T) {
	client, cleanup := setupTestRedisClient(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)

	// 第一次导入：包含 login 和 register 两个接口
	doc1 := knowledge.ParsedSpec{
		Meta: model.SpecMeta{
			Service:  "petstore",
			Host:     "petstore.swagger.io",
			BasePath: "/v2",
			Schemes:  []string{"https"},
		},
		Endpoints: []model.Endpoint{
			{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "User login"},
			{Service: "petstore", Method: "POST", Path: "/user/register", Summary: "User register"},
		},
	}
	stats1 := ingestor.UpsertDocument(doc1)
	if stats1.Endpoints != 2 {
		t.Fatalf("expected 2 endpoints after first upsert, got %d", stats1.Endpoints)
	}
	if stats1.Chunks != 8 {
		t.Fatalf("expected 8 chunks after first upsert, got %d", stats1.Chunks)
	}

	// 第二次导入：只包含 login 接口（register 已废弃）
	doc2 := knowledge.ParsedSpec{
		Meta: model.SpecMeta{
			Service:  "petstore",
			Host:     "petstore.swagger.io",
			BasePath: "/v2",
			Schemes:  []string{"https"},
		},
		Endpoints: []model.Endpoint{
			{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "User login"},
		},
	}
	stats2 := ingestor.UpsertDocument(doc2)
	if stats2.Endpoints != 1 {
		t.Fatalf("expected 1 endpoint after second upsert, got %d", stats2.Endpoints)
	}
	if stats2.Chunks != 4 {
		t.Fatalf("expected 4 chunks after second upsert, got %d", stats2.Chunks)
	}

	// 验证：只剩下 login 接口，register 已被删除
	endpoints := ingestor.Endpoints()
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint after replacement, got %d", len(endpoints))
	}
	if endpoints[0].Path != "/user/login" {
		t.Fatalf("expected /user/login, got %s", endpoints[0].Path)
	}

	// 验证：chunks 也只包含 login 相关的 4 个块
	chunks := ingestor.Chunks()
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks after replacement, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.Endpoint != "GET /user/login" {
			t.Fatalf("expected all chunks belong to GET /user/login, got %s", chunk.Endpoint)
		}
	}
}

func TestRedisIngestor_ChunkIDs(t *testing.T) {
	client, cleanup := setupTestRedisClient(t)
	defer cleanup()

	ingestor := NewRedisIngestor(client)
	ingestor.UpsertDocument(knowledge.ParsedSpec{
		Meta: model.SpecMeta{Service: "petstore"},
		Endpoints: []model.Endpoint{
			{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "User login"},
			{Service: "petstore", Method: "POST", Path: "/user/register", Summary: "User register"},
		},
	})

	ids := ingestor.ChunkIDs("PETSTORE")
	if len(ids) != 8 {
		t.Fatalf("expected 8 chunk ids, got %d", len(ids))
	}
	want := map[string]bool{
		"petstore:GET:/user/login:overview":       true,
		"petstore:GET:/user/login:request":        true,
		"petstore:GET:/user/login:response":       true,
		"petstore:GET:/user/login:dependency":     true,
		"petstore:POST:/user/register:overview":   true,
		"petstore:POST:/user/register:request":    true,
		"petstore:POST:/user/register:response":   true,
		"petstore:POST:/user/register:dependency": true,
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("unexpected chunk id %s", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing chunk ids: %v", want)
	}
}
