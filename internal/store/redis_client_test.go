package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestNewRedisClientMemoryUnsupported(t *testing.T) {
	_, err := NewRedisClient(RedisOptions{Mode: "memory"})
	if err == nil {
		t.Fatalf("expected memory mode unsupported error")
	}
}

func TestNewRedisClientGoRedisRoundTrip(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mr.Close()

	client, err := NewRedisClient(RedisOptions{Mode: "redis", Address: mr.Addr()})
	if err != nil {
		t.Fatalf("NewRedisClient(redis) failed: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	ctx := context.Background()
	if err := client.Set(ctx, "x", "1", time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	v, ok, err := client.Get(ctx, "x")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok || v != "1" {
		t.Fatalf("expected x=1, got ok=%v v=%q", ok, v)
	}
	if err := client.Del(ctx, "x"); err != nil {
		t.Fatalf("Del failed: %v", err)
	}
	_, ok, err = client.Get(ctx, "x")
	if err != nil {
		t.Fatalf("Get after Del failed: %v", err)
	}
	if ok {
		t.Fatalf("expected key deleted")
	}
}

func TestGoRedisClientDataStructures(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mr.Close()

	client, err := NewRedisClient(RedisOptions{Mode: "redis", Address: mr.Addr()})
	if err != nil {
		t.Fatalf("NewRedisClient(redis) failed: %v", err)
	}
	defer func() { _ = client.Close(context.Background()) }()

	ctx := context.Background()
	if err := client.SAdd(ctx, "services", "petstore", "billing"); err != nil {
		t.Fatalf("SAdd failed: %v", err)
	}
	services, err := client.SMembers(ctx, "services")
	if err != nil {
		t.Fatalf("SMembers failed: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	if err := client.HSet(ctx, "endpoints:petstore", "GET:/user/login", `{"path":"/user/login"}`); err != nil {
		t.Fatalf("HSet failed: %v", err)
	}
	v, ok, err := client.HGet(ctx, "endpoints:petstore", "GET:/user/login")
	if err != nil {
		t.Fatalf("HGet failed: %v", err)
	}
	if !ok || v == "" {
		t.Fatalf("expected hash value, got ok=%v v=%q", ok, v)
	}
	all, err := client.HGetAll(ctx, "endpoints:petstore")
	if err != nil {
		t.Fatalf("HGetAll failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 hash item, got %d", len(all))
	}

	if err := client.RPush(ctx, "chunks:petstore", "c1", "c2"); err != nil {
		t.Fatalf("RPush failed: %v", err)
	}
	chunks, err := client.LRange(ctx, "chunks:petstore", 0, -1)
	if err != nil {
		t.Fatalf("LRange failed: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestNewRedisClientInvalidMode(t *testing.T) {
	_, err := NewRedisClient(RedisOptions{Mode: "unknown"})
	if err == nil {
		t.Fatalf("expected invalid mode error")
	}
}
