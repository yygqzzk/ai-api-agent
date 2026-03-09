package agent

import (
	"context"
	"encoding/json"
	"time"
)

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func RetryMiddleware(cfg RetryConfig) Middleware {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 5 * time.Second
	}

	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, name string, args json.RawMessage) (any, error) {
			var lastErr error
			for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				result, err := next(ctx, name, args)
				if err == nil {
					return result, nil
				}
				lastErr = err
				if isPermanent(err) {
					return nil, err
				}
				if attempt < cfg.MaxAttempts-1 && cfg.BaseDelay > 0 {
					delay := cfg.BaseDelay * time.Duration(attempt+1)
					if delay > cfg.MaxDelay {
						delay = cfg.MaxDelay
					}
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			}
			return nil, lastErr
		}
	}
}

func isPermanent(err error) bool {
	type perm interface{ Permanent() bool }
	if p, ok := err.(perm); ok {
		return p.Permanent()
	}
	return false
}
