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

	"wanzhi/internal/domain/knowledge"
)

// SyncFile represents a file to be synced
type SyncFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Result represents the result of syncing a file
type Result struct {
	Path    string `json:"path"`
	Service string `json:"service,omitempty"`
	Count   int    `json:"count,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SyncService interface for syncing files
type SyncService interface {
	SyncFiles(ctx context.Context, files []SyncFile) ([]Result, error)
}

// HandlerOptions configures the webhook handler
type HandlerOptions struct {
	Secret       string
	BearerToken  string
	ProcessAsync bool
}

// Handler handles webhook requests
type Handler struct {
	service      SyncService
	secret       string
	bearerToken  string
	processAsync bool
}

// Payload represents a webhook payload
type Payload struct {
	Event      string   `json:"event"`
	Repository string   `json:"repository"`
	Branch     string   `json:"branch"`
	Commit     string   `json:"commit,omitempty"`
	Files      []SyncFile `json:"files"`
}

// SyncResponse represents the sync response
type SyncResponse struct {
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Details []Result `json:"details,omitempty"`
}

// NewHandler creates a new webhook handler
func NewHandler(service SyncService, opts HandlerOptions) *Handler {
	return &Handler{
		service:      service,
		secret:       strings.TrimSpace(opts.Secret),
		bearerToken:  strings.TrimSpace(opts.BearerToken),
		processAsync: opts.ProcessAsync,
	}
}

// HandleSync handles the sync webhook
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
		go func(files []SyncFile) {
			_, _ = h.service.SyncFiles(context.Background(), files)
		}(append([]SyncFile(nil), apiFiles...))
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

// authorize validates the request authorization
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

// verifySignature verifies GitHub HMAC signature
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

// filterAPIFiles filters API documentation files
func filterAPIFiles(files []SyncFile) []SyncFile {
	out := make([]SyncFile, 0, len(files))
	for _, file := range files {
		lower := strings.ToLower(file.Path)
		if strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
			out = append(out, file)
		}
	}
	return out
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// IngestorAdapter adapts knowledge.Ingestor to SyncService
type IngestorAdapter struct {
	ingestor knowledge.Ingestor
}

// NewIngestorAdapter creates a new adapter
func NewIngestorAdapter(ingestor knowledge.Ingestor) *IngestorAdapter {
	return &IngestorAdapter{
		ingestor: ingestor,
	}
}

// SyncFiles implements SyncService
func (a *IngestorAdapter) SyncFiles(ctx context.Context, files []SyncFile) ([]Result, error) {
	results := make([]Result, 0, len(files))

	for _, file := range files {
		doc, err := knowledge.ParseSwaggerDocumentBytes([]byte(file.Content), file.Path)
		if err != nil {
			results = append(results, Result{
				Path:  file.Path,
				Error: err.Error(),
			})
			continue
		}

		stats := a.ingestor.UpsertDocument(doc)
		results = append(results, Result{
			Path:    file.Path,
			Service: doc.Meta.Service,
			Count:   stats.Chunks,
		})
	}

	return results, nil
}
