package agent

import (
	"context"
	"encoding/json"
)

// ToolHandler 是工具调用的函数签名。
type ToolHandler func(ctx context.Context, name string, args json.RawMessage) (any, error)

// Middleware 包装 ToolHandler。
type Middleware func(next ToolHandler) ToolHandler

// Chain 组合多个中间件。第一个中间件最外层（最先进入、最后退出）。
func Chain(middlewares ...Middleware) Middleware {
	return func(final ToolHandler) ToolHandler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
