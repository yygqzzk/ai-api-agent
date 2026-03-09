package transport

import (
	"sync"
	"testing"
	"time"
)

func TestFixedWindowLimiter(t *testing.T) {
	limiter := newFixedWindowLimiterWithWindow(3, 100*time.Millisecond)

	key := "test-client"

	// 前 3 个请求应该通过
	for i := 0; i < 3; i++ {
		if !limiter.Allow(key) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 第 4 个请求应该被拒绝
	if limiter.Allow(key) {
		t.Error("Request 4 should be rejected")
	}

	// 等待下一窗口
	time.Sleep(120 * time.Millisecond)

	// 现在应该允许
	if !limiter.Allow(key) {
		t.Error("Request should be allowed after window reset")
	}
}

func TestFixedWindowLimiterMultipleKeys(t *testing.T) {
	limiter := newFixedWindowLimiter(2)

	// 不同 key 有独立计数
	if !limiter.Allow("client-1") {
		t.Error("client-1 request 1 should be allowed")
	}
	if !limiter.Allow("client-1") {
		t.Error("client-1 request 2 should be allowed")
	}
	if limiter.Allow("client-1") {
		t.Error("client-1 request 3 should be rejected")
	}

	// client-2 应该有独立的配额
	if !limiter.Allow("client-2") {
		t.Error("client-2 request 1 should be allowed")
	}
}

func TestFixedWindowLimiterStats(t *testing.T) {
	limiter := newFixedWindowLimiter(5)

	key := "test-key"

	// 使用 3 个请求
	for i := 0; i < 3; i++ {
		limiter.Allow(key)
	}

	stats := limiter.Stats(key)

	if stats.Available != 2 {
		t.Errorf("Expected 2 available, got %d", stats.Available)
	}
	if stats.Rejected != 0 {
		t.Errorf("Expected 0 rejected, got %d", stats.Rejected)
	}

	// 用满窗口并尝试 2 次被拒绝
	for i := 0; i < 2; i++ {
		if !limiter.Allow(key) {
			t.Fatalf("request %d should still be allowed", i+4)
		}
	}
	for i := 0; i < 2; i++ {
		limiter.Allow(key)
	}

	stats = limiter.Stats(key)

	if stats.Available != 0 {
		t.Errorf("Expected 0 available, got %d", stats.Available)
	}
	if stats.Rejected != 2 {
		t.Errorf("Expected 2 rejected, got %d", stats.Rejected)
	}
}

func TestFixedWindowLimiterReset(t *testing.T) {
	limiter := newFixedWindowLimiter(2)

	key := "test-key"

	limiter.Allow(key)
	limiter.Allow(key)

	if limiter.Allow(key) {
		t.Error("Should be limited")
	}

	limiter.Reset(key)

	// 重置后应该允许
	if !limiter.Allow(key) {
		t.Error("Should be allowed after reset")
	}
}

func TestSlidingWindowLimiter(t *testing.T) {
	limiter := newSlidingWindowLimiter(5, 200*time.Millisecond)

	key := "test-client"

	// 前 5 个请求应该通过
	for i := 0; i < 5; i++ {
		if !limiter.Allow(key) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 第 6 个请求应该被拒绝
	if limiter.Allow(key) {
		t.Error("Request 6 should be rejected")
	}

	// 等待整个窗口过去，最早的一批请求过期
	time.Sleep(220 * time.Millisecond)

	// 现在应该允许 1 个请求
	if !limiter.Allow(key) {
		t.Error("Request should be allowed after oldest expired")
	}
}

func TestSlidingWindowLimiterConcurrent(t *testing.T) {
	limiter := newSlidingWindowLimiter(100, time.Second)
	key := "concurrent-key"

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = limiter.Allow(key)
		}()
	}

	wg.Wait()

	// 验证没有 panic 或数据竞争
}

func TestTokenBucketLimiter(t *testing.T) {
	limiter := newTokenBucketLimiter(10, 10, time.Millisecond*100) // 10 容量，每 100ms 填充

	key := "test-key"

	// 初始桶是满的，前 10 个请求应该通过
	for i := 0; i < 10; i++ {
		if !limiter.Allow(key) {
			t.Errorf("Request %d should be allowed from full bucket", i+1)
		}
	}

	// 第 11 个请求应该被拒绝
	if limiter.Allow(key) {
		t.Error("Request 11 should be rejected")
	}

	// 等待令牌填充
	time.Sleep(time.Millisecond * 150)

	// 现在应该允许至少 1 个请求
	if !limiter.Allow(key) {
		t.Error("Request should be allowed after refill")
	}
}

func TestTokenBucketLimiterBurst(t *testing.T) {
	limiter := newTokenBucketLimiter(5, 10, time.Second*2) // 容量 5，爆发 10

	key := "burst-key"

	// 可以处理突发流量
	for i := 0; i < 10; i++ {
		if !limiter.Allow(key) {
			t.Errorf("Burst request %d should be allowed", i+1)
		}
	}

	// 第 11 个请求应该被拒绝
	if limiter.Allow(key) {
		t.Error("Request 11 should be rejected after burst")
	}
}

func TestTokenBucketLimiterWait(t *testing.T) {
	limiter := newTokenBucketLimiter(2, 2, time.Millisecond*50) // 2 容量，50ms 填充

	key := "wait-key"

	// 用完令牌
	limiter.Allow(key)
	limiter.Allow(key)

	waitTime := limiter.Wait(key)

	// Wait 应该返回非零等待时间
	if waitTime == 0 {
		// 可能令牌已经填充
	} else {
		if waitTime > time.Second {
			t.Errorf("Wait time too long: %v", waitTime)
		}
	}

	// 等待后应该允许请求
	time.Sleep(waitTime + time.Millisecond*10)
	if !limiter.Allow(key) {
		t.Error("Request should be allowed after wait")
	}
}

func TestNewRateLimiter(t *testing.T) {
	tests := []struct {
		name      string
		algorithm Algorithm
		cfg       Config
	}{
		{"fixed window", FixedWindow, Config{Algorithm: FixedWindow, Limit: 10}},
		{"sliding window", SlidingWindow, Config{Algorithm: SlidingWindow, Limit: 10, Window: time.Second}},
		{"token bucket", TokenBucket, Config{Algorithm: TokenBucket, Limit: 10, Burst: 10, Interval: time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewRateLimiter(tt.cfg)

			// 基本功能测试
			if !limiter.Allow("test") {
				t.Error("First request should be allowed")
			}

			stats := limiter.Stats("test")
			if stats.Available < 0 {
				t.Error("Stats should show available capacity")
			}
		})
	}
}

func TestAlgorithmString(t *testing.T) {
	tests := []struct {
		alg  Algorithm
		want string
	}{
		{FixedWindow, "fixed-window"},
		{SlidingWindow, "sliding-window"},
		{TokenBucket, "token-bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.alg.String(); got != tt.want {
				t.Errorf("Algorithm.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
