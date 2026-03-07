package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisClient 定义缓存层最小能力边界，当前为内存实现。
type RedisClient interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, bool, error)
	Del(ctx context.Context, key string) error
	Close(ctx context.Context) error
}

type RedisOptions struct {
	Mode     string
	Address  string
	Password string
	DB       int
}

type cacheItem struct {
	value     string
	expiresAt time.Time
}

// InMemoryRedisClient 用于本地开发与测试。
type InMemoryRedisClient struct {
	mu    sync.RWMutex
	items map[string]cacheItem
}

func NewInMemoryRedisClient() *InMemoryRedisClient {
	return &InMemoryRedisClient{items: make(map[string]cacheItem)}
}

func NewRedisClient(opts RedisOptions) (RedisClient, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = "memory"
	}
	switch mode {
	case "memory":
		return NewInMemoryRedisClient(), nil
	case "redis":
		if strings.TrimSpace(opts.Address) == "" {
			return nil, fmt.Errorf("redis address is required when mode=redis")
		}
		// redis.NewClient 是 go-redis 创建客户端的入口；连接参数集中放在 redis.Options 里。
		client := redis.NewClient(&redis.Options{
			Addr:     opts.Address,
			Password: opts.Password,
			DB:       opts.DB,
		})
		// context.Background() 常用于启动/初始化阶段：这里没有上游请求可继承，只需一个基础 context 做连通性检查。
		if err := client.Ping(context.Background()).Err(); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("redis ping failed: %w", err)
		}
		return &GoRedisClient{client: client}, nil
	default:
		return nil, fmt.Errorf("unsupported redis mode: %s", opts.Mode)
	}
}

func (c *InMemoryRedisClient) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	expires := time.Time{}
	// time.Time{} 是零值时间，这里把它当作“永不过期”的哨兵值。
	if ttl > 0 {
		// Add 会返回“当前时间 + ttl”后的绝对过期时刻，后续判断会更直接。
		expires = time.Now().Add(ttl)
	}
	c.items[key] = cacheItem{value: value, expiresAt: expires}
	return nil
}

func (c *InMemoryRedisClient) Get(_ context.Context, key string) (string, bool, error) {
	// sync.RWMutex 的 RLock/RUnlock 允许多个读者并发进入，适合这里的只读查询路径。
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return "", false, nil
	}
	// IsZero 用来区分“永不过期”和“有明确截止时间”；After 判断当前时间是否已越过截止点。
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return "", false, nil
	}
	return item.value, true, nil
}

func (c *InMemoryRedisClient) Del(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
	return nil
}

func (c *InMemoryRedisClient) Close(_ context.Context) error {
	return nil
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

func (c *GoRedisClient) Close(_ context.Context) error {
	return c.client.Close()
}
