// Package mcp 提供增强的限流机制
// 支持：固定窗口、滑动窗口、令牌桶算法
package mcp

import (
	"math"
	"sync"
	"time"
)

// RateLimiter 限流器接口
type RateLimiter interface {
	// Allow 判断是否允许请求通过
	Allow(key string) bool
	// AllowN 判断是否允许 n 个请求通过
	AllowN(key string, n int) bool
	// Wait 阻塞直到允许请求通过（返回等待时间）
	Wait(key string) time.Duration
	// WaitN 阻塞直到允许 n 个请求通过
	WaitN(key string, n int) time.Duration
	// Reset 重置指定 key 的计数
	Reset(key string)
	// Stats 返回限流器统计信息
	Stats(key string) RateLimitStats
}

// RateLimitStats 限流统计
type RateLimitStats struct {
	Available int         // 可用请求数
	WindowEnd time.Time   // 当前窗口结束时间
	Rejected  int         // 拒绝计数
}

// Algorithm 限流算法类型
type Algorithm int

const (
	// FixedWindow 固定窗口算法
	FixedWindow Algorithm = iota
	// SlidingWindow 滑动窗口算法
	SlidingWindow
	// TokenBucket 令牌桶算法
	TokenBucket
)

func (a Algorithm) String() string {
	switch a {
	case FixedWindow:
		return "fixed-window"
	case SlidingWindow:
		return "sliding-window"
	case TokenBucket:
		return "token-bucket"
	default:
		return "unknown"
	}
}

// Config 限流器配置
type Config struct {
	Algorithm Algorithm // 限流算法
	Limit     int        // 每秒最大请求数（令牌桶为桶容量）
	Burst     int        // 突发容量（令牌桶为桶爆发容量）
	Window    time.Duration // 时间窗口（固定/滑动窗口）
	Interval  time.Duration // 令牌填充间隔
}

// DefaultConfig 返回默认配置（固定窗口算法）
func DefaultConfig() Config {
	return Config{
		Algorithm: FixedWindow,
		Limit:     60,
		Burst:     60,
		Window:    time.Minute,
		Interval:  time.Second,
	}
}

// FixedWindowLimiter 固定窗口限流器
// 简单高效，但存在边界问题（窗口边界突发双倍流量）
type FixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	windows map[string]*windowCounter
}

// windowCounter 窗口计数器
type windowCounter struct {
	windowStart time.Time
	count       int
	rejected    int
}

func newFixedWindowLimiter(limit int) *FixedWindowLimiter {
	if limit <= 0 {
		limit = 60
	}
	return &FixedWindowLimiter{
		limit:   limit,
		windows: make(map[string]*windowCounter),
	}
}

func (l *FixedWindowLimiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

func (l *FixedWindowLimiter) AllowN(key string, n int) bool {
	now := time.Now()
	windowStart := now.Truncate(time.Minute)

	l.mu.Lock()
	defer l.mu.Unlock()

	current, ok := l.windows[key]
	if !ok || !current.windowStart.Equal(windowStart) {
		l.windows[key] = &windowCounter{
			windowStart: windowStart,
			count:       n,
		}
		return true
	}
	if current.count+n > l.limit {
		current.rejected++
		return false
	}
	current.count += n
	return true
}

func (l *FixedWindowLimiter) Wait(key string) time.Duration {
	if l.Allow(key) {
		return 0
	}
	// 计算到窗口结束的等待时间
	l.mu.Lock()
	defer l.mu.Unlock()

	if current, ok := l.windows[key]; ok {
		return time.Until(current.windowStart.Add(time.Minute))
	}
	return 0
}

func (l *FixedWindowLimiter) WaitN(key string, n int) time.Duration {
	if l.AllowN(key, n) {
		return 0
	}
	return l.Wait(key)
}

func (l *FixedWindowLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.windows, key)
}

func (l *FixedWindowLimiter) Stats(key string) RateLimitStats {
	l.mu.Lock()
	defer l.mu.Unlock()

	if current, ok := l.windows[key]; ok {
		available := l.limit - current.count
		if available < 0 {
			available = 0
		}
		return RateLimitStats{
			Available: available,
			WindowEnd: current.windowStart.Add(time.Minute),
			Rejected:  current.rejected,
		}
	}
	return RateLimitStats{Available: l.limit}
}

// SlidingWindowLimiter 滑动窗口限流器
// 解决固定窗口的边界问题，更平滑
type SlidingWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string][]time.Time
}

func newSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit <= 0 {
		limit = 60
	}
	if window == 0 {
		window = time.Minute
	}
	return &SlidingWindowLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string][]time.Time),
	}
}

func (l *SlidingWindowLimiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

func (l *SlidingWindowLimiter) AllowN(key string, n int) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[key]

	// 清理过期的请求
	valid := make([]time.Time, 0, len(bucket))
	for _, t := range bucket {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	bucket = valid

	// 检查是否超限
	if len(bucket)+n > l.limit {
		return false
	}

	// 添加新请求
	for i := 0; i < n; i++ {
		bucket = append(bucket, now)
	}
	l.buckets[key] = bucket

	return true
}

func (l *SlidingWindowLimiter) Wait(key string) time.Duration {
	if l.Allow(key) {
		return 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[key]
	if len(bucket) > 0 {
		// 最旧请求的时间 + 窗口时间 = 可用时间
		oldest := bucket[0]
		available := oldest.Add(l.window)
		now := time.Now()
		if available.After(now) {
			return available.Sub(now)
		}
	}
	return 0
}

func (l *SlidingWindowLimiter) WaitN(key string, n int) time.Duration {
	if l.AllowN(key, n) {
		return 0
	}
	return l.Wait(key)
}

func (l *SlidingWindowLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

func (l *SlidingWindowLimiter) Stats(key string) RateLimitStats {
	l.mu.Lock()
	defer l.mu.Unlock()

	if bucket, ok := l.buckets[key]; ok {
		now := time.Now()
		cutoff := now.Add(-l.window)

		// 计算有效请求数
		validCount := 0
		for _, t := range bucket {
			if t.After(cutoff) {
				validCount++
			}
		}

		return RateLimitStats{
			Available: l.limit - validCount,
		}
	}
	return RateLimitStats{Available: l.limit}
}

// TokenBucketLimiter 令牌桶限流器
// 适合处理突发流量，平滑限流
type TokenBucketLimiter struct {
	mu       sync.Mutex
	capacity int64       // 桶容量
	tokens   int64       // 当前令牌数
	lastRefill time.Time  // 上次填充时间
	interval time.Duration // 填充间隔
	fillRate float64      // 每次填充的令牌数
}

func newTokenBucketLimiter(capacity, burst int, interval time.Duration) *TokenBucketLimiter {
	if capacity <= 0 {
		capacity = 60
	}
	if burst <= 0 {
		burst = capacity
	}
	if interval == 0 {
		interval = time.Second
	}

	return &TokenBucketLimiter{
		capacity: int64(capacity),
		tokens:   int64(capacity), // 初始满桶
		interval: interval,
		fillRate: float64(capacity) / float64(time.Second) * float64(interval),
	}
}

func (l *TokenBucketLimiter) Allow(key string) bool {
	// 令牌桶通常是全局的，这里简化实现
	return l.AllowN(key, 1)
}

func (l *TokenBucketLimiter) AllowN(key string, n int) bool {
	if n <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 填充令牌
	l.refill()

	// 检查令牌是否足够
	if l.tokens >= int64(n) {
		l.tokens -= int64(n)
		return true
	}
	return false
}

func (l *TokenBucketLimiter) refill() {
	now := time.Now()
	if l.lastRefill.IsZero() {
		l.lastRefill = now
		return
	}

	elapsed := now.Sub(l.lastRefill)
	if elapsed < l.interval {
		return
	}

	// 计算需要填充的次数
	intervals := float64(elapsed) / float64(l.interval)
	tokensToAdd := int64(intervals * l.fillRate)

	l.tokens += tokensToAdd
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}

	l.lastRefill = now
}

func (l *TokenBucketLimiter) Wait(key string) time.Duration {
	if l.Allow(key) {
		return 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.refill()

	if l.tokens <= 0 {
		// 计算等待一个令牌的时间
		return l.interval
	}
	return 0
}

func (l *TokenBucketLimiter) WaitN(key string, n int) time.Duration {
	if l.AllowN(key, n) {
		return 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.refill()

	if l.tokens < int64(n) {
		// 计算等待 n 个令牌的时间
		needed := int64(n) - l.tokens
		intervals := math.Ceil(float64(needed) / l.fillRate)
		return time.Duration(float64(l.interval) * intervals)
	}
	return 0
}

func (l *TokenBucketLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tokens = l.capacity
	l.lastRefill = time.Time{}
}

func (l *TokenBucketLimiter) Stats(key string) RateLimitStats {
	l.mu.Lock()
	defer l.mu.Unlock()

	return RateLimitStats{
		Available: int(l.tokens),
	}
}

// NewRateLimiter 根据配置创建限流器
func NewRateLimiter(cfg Config) RateLimiter {
	switch cfg.Algorithm {
	case FixedWindow:
		return newFixedWindowLimiter(cfg.Limit)
	case SlidingWindow:
		return newSlidingWindowLimiter(cfg.Limit, cfg.Window)
	case TokenBucket:
		return newTokenBucketLimiter(cfg.Limit, cfg.Burst, cfg.Interval)
	default:
		return newFixedWindowLimiter(cfg.Limit)
	}
}
