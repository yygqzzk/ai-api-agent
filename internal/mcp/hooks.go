package mcp

import (
	"context"
	"time"
)

type Hooks struct {
	OnInit         func(ctx context.Context) error
	BeforeToolCall func(ctx context.Context, toolName string)
	AfterToolCall  func(ctx context.Context, toolName string, duration time.Duration, err error)
	OnShutdown     func(ctx context.Context) error
}
