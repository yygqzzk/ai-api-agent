package transport

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"wanzhi/internal/domain/agent"
)

// StreamRunner 为 Chat 提供流式事件输出
type StreamRunner interface {
	RunStream(ctx context.Context, userQuery string) <-chan agent.AgentEvent
}

type ChatRequest struct {
	Message string `json:"message" binding:"required"`
}

type ChatHandler struct {
	streamRunner StreamRunner
}

func NewChatHandler(runner StreamRunner) *ChatHandler {
	return &ChatHandler{streamRunner: runner}
}

func (h *ChatHandler) HandleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	ch := h.streamRunner.RunStream(c.Request.Context(), req.Message)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()

	for ev := range ch {
		// SSE 格式: "event: {type}\ndata: {json}\n\n"
		c.SSEvent(string(ev.Kind), ev.Content)
		c.Writer.Flush()
	}
}
