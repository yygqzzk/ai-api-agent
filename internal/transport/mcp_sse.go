package transport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"wanzhi/internal/domain/agent"
)

func isSSERequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeSSEEvent(w http.ResponseWriter, ev agent.AgentEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
