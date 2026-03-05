package resilience

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerClosed(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.MaxRequests = 2
	cfg.ReadyToTrip = 0.5 // 50% 失败即熔断

	cb := NewCircuitBreaker(cfg)

	// 初始状态应该是关闭
	if cb.State() != StateClosed {
		t.Errorf("Expected state closed, got %v", cb.State())
	}

	// 第一次调用成功
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state still closed after success, got %v", cb.State())
	}

	// 第二次调用失败
	err = cb.Execute(func() error {
		return errors.New("simulated error")
	})
	if err == nil {
		t.Error("Expected error")
	}

	// 失败率达到 50%，应该触发熔断
	if cb.State() != StateOpen {
		t.Errorf("Expected state open after 50%% failures, got %v", cb.State())
	}
}

func TestCircuitBreakerOpen(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.MaxRequests = 2
	cfg.ReadyToTrip = 0.5
	cfg.Timeout = time.Millisecond * 100

	cb := NewCircuitBreaker(cfg)

	// 模拟两次失败，触发熔断
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error {
			return errors.New("error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("Expected circuit to be open, got %v", cb.State())
	}

	// 熔断时应该直接拒绝
	err := cb.Execute(func() error {
		t.Fatal("Should not be called when circuit is open")
		return nil
	})

	if !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Errorf("Expected ErrCircuitBreakerOpen, got %v", err)
	}
}

func TestCircuitBreakerHalfOpen(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.MaxRequests = 2
	cfg.ReadyToTrip = 0.5
	cfg.Timeout = time.Millisecond * 100

	cb := NewCircuitBreaker(cfg)

	// 触发熔断
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error {
			return errors.New("error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("Expected circuit to be open, got %v", cb.State())
	}

	// 等待超时，进入半开状态
	time.Sleep(time.Millisecond * 150)

	// 第一次半开调用成功
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// 成功后应该恢复到关闭状态
	if cb.State() != StateClosed {
		t.Errorf("Expected state closed after successful half-open, got %v", cb.State())
	}
}

func TestCircuitBreakerAllow(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.MaxRequests = 2
	cfg.ReadyToTrip = 0.5 // 50% 失败即熔断

	cb := NewCircuitBreaker(cfg)

	// 允许请求
	if !cb.Allow() {
		t.Error("Expected Allow to return true")
	}
	cb.RecordSuccess()

	if !cb.Allow() {
		t.Error("Expected Allow to return true")
	}
	cb.RecordFailure()

	// 失败率 50%，应该熔断
	if cb.State() != StateOpen {
		t.Errorf("Expected state open, got %v", cb.State())
	}

	// 不应该允许
	if cb.Allow() {
		t.Error("Expected Allow to return false after circuit opens")
	}
}

func TestCircuitBreakerForceReset(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.MaxRequests = 2
	cfg.ReadyToTrip = 0.5

	cb := NewCircuitBreaker(cfg)

	// 触发熔断
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error {
			return errors.New("error")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("Expected circuit to be open, got %v", cb.State())
	}

	// 强制重置
	cb.ForceReset()

	if cb.State() != StateClosed {
		t.Errorf("Expected state closed after ForceReset, got %v", cb.State())
	}

	// 现在应该允许请求
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Unexpected error after ForceReset: %v", err)
	}
}

func TestCircuitBreakerMetrics(t *testing.T) {
	cfg := DefaultConfig("test")
	cfg.MaxRequests = 10
	cfg.ReadyToTrip = 0.5

	cb := NewCircuitBreaker(cfg)

	// 执行一些操作
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error {
			return nil
		})
	}
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error {
			return errors.New("error")
		})
	}

	metrics := cb.Metrics()

	if metrics.Requests != 5 {
		t.Errorf("Expected 5 requests, got %d", metrics.Requests)
	}
	if metrics.Successes != 3 {
		t.Errorf("Expected 3 successes, got %d", metrics.Successes)
	}
	expectedRate := 2.0 / 5.0
	if metrics.FailureRate != expectedRate {
		t.Errorf("Expected failure rate %.2f, got %.2f", expectedRate, metrics.FailureRate)
	}
}

func TestRetry(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxAttempts = 3
	cfg.BaseDelay = time.Millisecond * 10

	r := NewRetry(cfg)

	attempts := 0
	err := r.Execute(func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestRetryExhausted(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxAttempts = 2
	cfg.BaseDelay = time.Millisecond * 5

	r := NewRetry(cfg)

	attempts := 0
	err := r.Execute(func() error {
		attempts++
		return errors.New("persistent error")
	})

	if err == nil {
		t.Error("Expected error after exhausting attempts")
	}
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestRetryDo(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxAttempts = 3
	cfg.BaseDelay = time.Millisecond * 5

	r := NewRetry(cfg)

	attempts := 0
	result, err := Do(r, func() (string, error) {
		attempts++
		if attempts < 2 {
			return "", errors.New("temporary error")
		}
		return "success", nil
	})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("Expected 'success', got %q", result)
	}
}

func TestRetryWithJitter(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxAttempts = 3
	cfg.BaseDelay = time.Millisecond * 10
	cfg.Jitter = 0.5

	retryCount := 0
	cfg.OnRetry = func(attempt int, err error) {
		retryCount++
	}

	r := NewRetry(cfg)

	_ = r.Execute(func() error {
		return errors.New("error")
	})

	// 应该有 2 次重试（总共 3 次尝试）
	if retryCount != 2 {
		t.Errorf("Expected 2 retries, got %d", retryCount)
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateHalfOpen, "half-open"},
		{StateOpen, "open"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
