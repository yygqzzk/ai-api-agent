package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisClient 定义缓存层最小能力边界。
type RedisClient interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, bool, error)
	Del(ctx context.Context, key string) error
	Close(ctx context.Context) error
	SAdd(ctx context.Context, key string, members ...string) error
	SMembers(ctx context.Context, key string) ([]string, error)
	HSet(ctx context.Context, key string, field string, value string) error
	HGet(ctx context.Context, key string, field string) (string, bool, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	RPush(ctx context.Context, key string, values ...string) error
	LRange(ctx context.Context, key string, start int64, stop int64) ([]string, error)
	Ping(ctx context.Context) error
}

type RedisOptions struct {
	Mode     string
	Address  string
	Password string
	DB       int
}

func NewRedisClient(opts RedisOptions) (RedisClient, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" || mode == "memory" {
		return nil, fmt.Errorf("memory mode is no longer supported, use mode=redis")
	}
	if mode != "redis" {
		return nil, fmt.Errorf("unsupported redis mode: %s (only 'redis' is supported)", opts.Mode)
	}
	if strings.TrimSpace(opts.Address) == "" {
		return nil, fmt.Errorf("redis address is required when mode=redis")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     opts.Address,
		Password: opts.Password,
		DB:       opts.DB,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return &GoRedisClient{client: client}, nil
}

type GoRedisClient struct {
	client *redis.Client
}

func (c *GoRedisClient) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *GoRedisClient) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := c.client.Get(ctx, key).Result()
	// redis.Nil 是 go-redis 约定的哨兵错误，表示 key 不存在，不应按真正故障处理。
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (c *GoRedisClient) Del(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *GoRedisClient) SAdd(ctx context.Context, key string, members ...string) error {
	return c.client.SAdd(ctx, key, stringArgsToAny(members)...).Err()
}

func (c *GoRedisClient) SMembers(ctx context.Context, key string) ([]string, error) {
	return c.client.SMembers(ctx, key).Result()
}

func (c *GoRedisClient) HSet(ctx context.Context, key string, field string, value string) error {
	return c.client.HSet(ctx, key, field, value).Err()
}

func (c *GoRedisClient) HGet(ctx context.Context, key string, field string) (string, bool, error) {
	v, err := c.client.HGet(ctx, key, field).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (c *GoRedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return c.client.HGetAll(ctx, key).Result()
}

func (c *GoRedisClient) RPush(ctx context.Context, key string, values ...string) error {
	return c.client.RPush(ctx, key, stringArgsToAny(values)...).Err()
}

func (c *GoRedisClient) LRange(ctx context.Context, key string, start int64, stop int64) ([]string, error) {
	return c.client.LRange(ctx, key, start, stop).Result()
}

func (c *GoRedisClient) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *GoRedisClient) Close(_ context.Context) error {
	return c.client.Close()
}

func stringArgsToAny(values []string) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
