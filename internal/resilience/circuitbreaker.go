// Package resilience 提供容错机制,包括熔断器、重试、降级等
// 用于保护外部依赖(如 LLM API、向量检索服务等)
//
// # 设计理念
//
// 熔断器模式 (Circuit Breaker Pattern) 用于防止级联故障:
// 1. 当外部服务频繁失败时,自动熔断,快速失败
// 2. 避免浪费资源在注定失败的请求上
// 3. 给外部服务恢复的时间
// 4. 定期尝试恢复,检测服务是否已恢复
//
// # 状态机设计
//
// 熔断器有三种状态:
//
// 1. **Closed (关闭)** - 正常状态
//   - 请求正常通过
//   - 统计失败率
//   - 失败率达到阈值 → Open
//
// 2. **Open (打开)** - 熔断状态
//   - 直接拒绝请求,快速失败
//   - 返回 ErrCircuitBreakerOpen
//   - 超时后 → HalfOpen
//
// 3. **HalfOpen (半开)** - 探测状态
//   - 允许少量请求通过 (maxRequests)
//   - 如果请求成功 → Closed
//   - 如果请求失败 → Open
//
// 状态转换图:
//
//	Closed --[失败率 >= ReadyToTrip]--> Open
//	Open --[超时]--> HalfOpen
//	HalfOpen --[成功]--> Closed
//	HalfOpen --[失败]--> Open
//
// # 并发安全性
//
// - state: 使用 atomic.Value 存储,无锁读取
// - counts/expiry/generation: 使用 mutex 保护
// - beforeRequest/afterRequest: 串行执行,避免竞态条件
//
// # 参考文献
//
// Martin Fowler's Circuit Breaker: https://martinfowler.com/bliki/CircuitBreaker.html
// Netflix Hystrix: https://github.com/Netflix/Hystrix/wiki/How-it-Works
package resilience

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// State 熔断器状态
type State int32

const (
	// StateClosed 正常状态，请求正常通过
	StateClosed State = iota
	// StateHalfOpen 半开状态，允许少量请求通过，检测服务是否恢复
	StateHalfOpen
	// StateOpen 熔断状态，直接拒绝请求，快速失败
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// ErrCircuitBreakerOpen 熔断器打开错误
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

// Config 熔断器配置
type Config struct {
	// Name 熔断器名称（用于日志和指标）
	Name string
	// MaxRequests 半开状态允许的最大请求数
	MaxRequests uint32
	// Interval 统计失败率的时间窗口
	Interval time.Duration
	// Timeout 熔断器打开后，多久尝试恢复
	Timeout time.Duration
	// ReadyToTrip 失败率达到多少时熔断（0.5 表示 50%）
	ReadyToTrip float64
	// OnStateChange 状态变化回调（用于记录指标或日志）
	OnStateChange func(from, to State)
}

// DefaultConfig 返回默认配置
func DefaultConfig(name string) Config {
	return Config{
		Name:         name,
		MaxRequests:   3,
		Interval:      time.Second * 10,
		Timeout:       time.Second * 30,
		ReadyToTrip:   0.5,
		OnStateChange: nil,
	}
}

// CircuitBreaker 熔断器
//
// 核心字段:
// - state: 当前状态 (Closed/Open/HalfOpen),使用 atomic.Value 实现无锁读取
// - generation: 熔断器代数,每次打开后递增,用于检测过期的请求
// - counts: [requests, successes] 统计窗口内的请求数和成功数
// - expiry: 统计窗口或熔断超时的过期时间
//
// 并发安全性:
// - state 使用 atomic.Value,支持无锁并发读取
// - counts/expiry/generation 使用 mutex 保护
// - beforeRequest 和 afterRequest 必须成对调用,且使用相同的 generation
//
// 性能考虑:
// - 读取状态无锁,适合高并发场景
// - 统计计数使用 atomic 操作,减少锁竞争
// - 状态转换使用 mutex 保护,确保一致性
type CircuitBreaker struct {
	name          string
	maxRequests   uint32
	interval      time.Duration
	timeout       time.Duration
	readyToTrip   float64
	onStateChange func(from, to State)

	state atomic.Value // State

	mutex      sync.Mutex
	generation uint64        // 熔断器代数，每次打开后递增
	counts     [2]uint32      // requests, successes
	expiry     time.Time        // 统计窗口过期时间
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(cfg Config) *CircuitBreaker {
	if cfg.Interval == 0 {
		cfg.Interval = time.Second * 10
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Second * 30
	}
	if cfg.ReadyToTrip <= 0 {
		cfg.ReadyToTrip = 0.5
	}
	if cfg.MaxRequests == 0 {
		cfg.MaxRequests = 3
	}
	if cfg.Name == "" {
		cfg.Name = "default"
	}

	cb := &CircuitBreaker{
		name:          cfg.Name,
		maxRequests:   cfg.MaxRequests,
		interval:      cfg.Interval,
		timeout:       cfg.Timeout,
		readyToTrip:   cfg.ReadyToTrip,
		onStateChange: cfg.OnStateChange,
	}
	cb.state.Store(StateClosed)
	return cb
}

// Name 返回熔断器名称
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// State 返回当前状态
func (cb *CircuitBreaker) State() State {
	return cb.state.Load().(State)
}

// Execute 执行函数，带熔断保护
// 如果熔断器打开，返回 ErrCircuitBreakerOpen
// 如果执行失败，增加失败计数，可能触发熔断
// 如果执行成功，增加成功计数
func (cb *CircuitBreaker) Execute(fn func() error) error {
	generation, err := cb.beforeRequest()
	if err != nil {
		return err
	}

	err = fn()
	cb.afterRequest(generation, err == nil)

	return err
}

// Allow 判断是否允许请求通过（不执行函数）
func (cb *CircuitBreaker) Allow() bool {
	_, err := cb.beforeRequest()
	return err == nil
}

// RecordSuccess 记录成功（配合 Allow 使用）
func (cb *CircuitBreaker) RecordSuccess() {
	cb.afterRequest(cb.generation, true)
}

// RecordFailure 记录失败（配合 Allow 使用）
func (cb *CircuitBreaker) RecordFailure() {
	cb.afterRequest(cb.generation, false)
}

// beforeRequest 请求前的检查
func (cb *CircuitBreaker) beforeRequest() (uint64, error) {
	now := time.Now()

	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	state := cb.state.Load().(State)
	generation := cb.generation

	// 根据状态决定是否允许请求
	switch state {
	case StateClosed:
		// 初始化统计窗口
		if cb.expiry.IsZero() {
			cb.expiry = now.Add(cb.interval)
		}
		return generation, nil

	case StateHalfOpen:
		// 半开状态：只允许 maxRequests 个请求
		if atomic.LoadUint32(&cb.counts[0]) < cb.maxRequests {
			return generation, nil
		}
		return generation, ErrCircuitBreakerOpen

	case StateOpen:
		// 检查是否可以尝试恢复
		if now.After(cb.expiry) {
			cb.setState(StateHalfOpen)
			cb.resetCounts(time.Time{})
			return generation, nil
		}
		return generation, ErrCircuitBreakerOpen

	default:
		return generation, ErrCircuitBreakerOpen
	}
}

// afterRequest 请求后的处理
func (cb *CircuitBreaker) afterRequest(generation uint64, success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	// 如果代数不匹配，说明在请求期间熔断器被重置了
	if generation != cb.generation {
		return
	}

	// 增加请求计数
	atomic.AddUint32(&cb.counts[0], 1)

	if success {
		atomic.AddUint32(&cb.counts[1], 1)

		// 半开状态下成功 → 恢复到关闭状态
		if cb.state.Load().(State) == StateHalfOpen {
			cb.setState(StateClosed)
			cb.resetCounts(time.Time{})
		}
	} else {
		// 检查是否需要熔断
		cb.evaluateAndTrip()
	}
}

// evaluateAndTrip 评估是否需要熔断
func (cb *CircuitBreaker) evaluateAndTrip() {
	now := time.Now()

	// 计算失败率
	requests := atomic.LoadUint32(&cb.counts[0])
	successes := atomic.LoadUint32(&cb.counts[1])

	// 如果请求次数不足，不评估
	if requests < cb.maxRequests {
		return
	}

	failureRate := float64(requests-successes) / float64(requests)

	// 如果失败率达到阈值，熔断
	if failureRate >= cb.readyToTrip {
		cb.setState(StateOpen)
		cb.expiry = now.Add(cb.timeout)
		cb.generation++
	}
}

// setState 设置状态
func (cb *CircuitBreaker) setState(to State) {
	from := cb.state.Load().(State)
	if from == to {
		return
	}

	cb.state.Store(to)

	if cb.onStateChange != nil {
		cb.onStateChange(from, to)
	}
}

// resetCounts 重置计数
func (cb *CircuitBreaker) resetCounts(now time.Time) {
	cb.counts[0] = 0
	cb.counts[1] = 0

	// 设置新的过期时间
	if !now.IsZero() {
		cb.expiry = now.Add(cb.interval)
	} else {
		cb.expiry = time.Time{}
	}
}

// ForceReset 强制重置熔断器（用于测试或手动恢复）
func (cb *CircuitBreaker) ForceReset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.resetCounts(time.Time{})
	cb.generation++
	cb.setState(StateClosed)
}

// Metrics 返回熔断器指标
func (cb *CircuitBreaker) Metrics() Metrics {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	requests := atomic.LoadUint32(&cb.counts[0])
	successes := atomic.LoadUint32(&cb.counts[1])
	failureRate := 0.0
	if requests > 0 {
		failureRate = float64(requests-successes) / float64(requests)
	}

	return Metrics{
		Name:         cb.name,
		State:        cb.state.Load().(State).String(),
		Requests:     requests,
		Successes:    successes,
		FailureRate:  failureRate,
		ReadyToTrip:  cb.readyToTrip,
		Expiry:       cb.expiry,
	}
}

// Metrics 熔断器指标
type Metrics struct {
	Name        string
	State       string
	Requests    uint32
	Successes   uint32
	FailureRate float64
	ReadyToTrip float64
	Expiry      time.Time
}

func (m Metrics) String() string {
	return fmt.Sprintf("circuit_breaker{name=%s state=%s requests=%d successes=%d failure_rate=%.2f ready_to_trip=%.2f expiry=%v}",
		m.Name, m.State, m.Requests, m.Successes, m.FailureRate, m.ReadyToTrip, m.Expiry)
}

// Retry 重试配置和执行
type Retry struct {
	maxAttempts int
	backoff     func(attempt int) time.Duration
	onRetry     func(attempt int, err error)
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int
	MaxDelay    time.Duration
	BaseDelay   time.Duration
	Jitter      float64 // 随机因子
	OnRetry     func(attempt int, err error)
}

// DefaultRetryConfig 返回默认重试配置
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		MaxDelay:    time.Second * 5,
		BaseDelay:   time.Millisecond * 200,
		Jitter:      0.5,
		OnRetry:     nil,
	}
}

// NewRetry 创建重试器
func NewRetry(cfg RetryConfig) *Retry {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = time.Millisecond * 200
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = time.Second * 5
	}

	return &Retry{
		maxAttempts: cfg.MaxAttempts,
		backoff: func(attempt int) time.Duration {
			// 指数退避
			delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(2, float64(attempt-1)))
			// 加随机抖动
			if cfg.Jitter > 0 {
				delay = time.Duration(float64(delay) * (1 - cfg.Jitter + 2*cfg.Jitter*rand.Float64()))
			}
			// 限制最大延迟
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			return delay
		},
		onRetry: cfg.OnRetry,
	}
}

// Execute 执行函数，失败时重试
func (r *Retry) Execute(fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// 最后一次尝试失败，不再重试
		if attempt == r.maxAttempts {
			break
		}

		// 调用重试回调
		if r.onRetry != nil {
			r.onRetry(attempt, err)
		}

		// 计算退避时间并等待
		delay := r.backoff(attempt)
		time.Sleep(delay)
	}

	return fmt.Errorf("after %d attempts, last error: %w", r.maxAttempts, lastErr)
}

// Do 带返回值的重试执行
func Do[T any](r *Retry, fn func() (T, error)) (T, error) {
	var lastErr error
	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		t, err := fn()
		if err == nil {
			return t, nil
		}

		lastErr = err

		if attempt == r.maxAttempts {
			break
		}

		if r.onRetry != nil {
			r.onRetry(attempt, err)
		}

		delay := r.backoff(attempt)
		time.Sleep(delay)
	}

	var zero T
	return zero, fmt.Errorf("after %d attempts, last error: %w", r.maxAttempts, lastErr)
}
