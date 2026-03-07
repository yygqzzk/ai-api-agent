package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"ai-agent-api/internal/ingest"
)

type SyncService interface {
	SyncFiles(ctx context.Context, files []ingest.SyncFile) ([]ingest.Result, error)
}

type HandlerOptions struct {
	Secret       string
	BearerToken  string
	ProcessAsync bool
}

type Handler struct {
	service      SyncService
	secret       string
	bearerToken  string
	processAsync bool
}

type Payload struct {
	Event      string            `json:"event"`
	Repository string            `json:"repository"`
	Branch     string            `json:"branch"`
	Commit     string            `json:"commit,omitempty"`
	Files      []ingest.SyncFile `json:"files"`
}

type SyncResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Details []ingest.Result `json:"details,omitempty"`
}

func NewHandler(service SyncService, opts HandlerOptions) *Handler {
	return &Handler{
		service:      service,
		secret:       strings.TrimSpace(opts.Secret),
		bearerToken:  strings.TrimSpace(opts.BearerToken),
		processAsync: opts.ProcessAsync,
	}
}

func (h *Handler) HandleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request body failed", http.StatusBadRequest)
		return
	}
	if err := h.authorize(r, body); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	apiFiles := filterAPIFiles(payload.Files)
	if len(apiFiles) == 0 {
		writeJSON(w, http.StatusOK, SyncResponse{Status: "skipped", Message: "No API documents changed"})
		return
	}
	if h.processAsync {
		go func(files []ingest.SyncFile) {
			_, _ = h.service.SyncFiles(context.Background(), files)
		}(append([]ingest.SyncFile(nil), apiFiles...))
		writeJSON(w, http.StatusAccepted, SyncResponse{Status: "accepted", Message: fmt.Sprintf("Processing %d files", len(apiFiles))})
		return
	}
	results, err := h.service.SyncFiles(r.Context(), apiFiles)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, SyncResponse{Status: "error", Message: err.Error(), Details: results})
		return
	}
	writeJSON(w, http.StatusOK, SyncResponse{Status: "success", Message: fmt.Sprintf("Synced %d API documents", len(results)), Details: results})
}

func (h *Handler) authorize(r *http.Request, body []byte) error {
	if h.secret == "" && h.bearerToken == "" {
		return nil
	}
	if signature := strings.TrimSpace(r.Header.Get("X-Hub-Signature-256")); signature != "" && h.secret != "" {
		if verifySignature(body, h.secret, signature) {
			return nil
		}
	}
	if h.bearerToken != "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		expected := "Bearer " + h.bearerToken
		if auth == expected {
			return nil
		}
	}
	return fmt.Errorf("unauthorized")
}

func verifySignature(body []byte, secret string, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

func filterAPIFiles(files []ingest.SyncFile) []ingest.SyncFile {
	out := make([]ingest.SyncFile, 0, len(files))
	for _, file := range files {
		lower := strings.ToLower(file.Path)
		if strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
			out = append(out, file)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
