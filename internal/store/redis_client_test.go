package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestNewRedisClientMemory(t *testing.T) {
	client, err := NewRedisClient(RedisOptions{Mode: "memory"})
	if err != nil {
		t.Fatalf("NewRedisClient(memory) failed: %v", err)
	}

	ctx := context.Background()
	if err := client.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	v, ok, err := client.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok || v != "v" {
		t.Fatalf("expected k=v, got ok=%v v=%q", ok, v)
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

func TestNewRedisClientInvalidMode(t *testing.T) {
	_, err := NewRedisClient(RedisOptions{Mode: "unknown"})
	if err == nil {
		t.Fatalf("expected invalid mode error")
	}
}
