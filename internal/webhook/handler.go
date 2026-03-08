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

	"wanzhi/internal/ingest"
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
	// io.ReadAll 会把请求体一次性读完，适合 webhook 这类负载较小的 JSON 请求。
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
	// json.Unmarshal 把 []byte 中的 JSON 解码到结构体字段；
	// 结构体 tag `json:"..."` 决定每个字段对应哪个 JSON key。
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
		// 这里启动 goroutine 做异步处理，让 HTTP 请求可以先返回 202。
		// `append([]T(nil), apiFiles...)` 会复制一份切片，避免后续共享底层数组。
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
	// Header.Get 从请求头中取值；TrimSpace 用来顺手去掉前后空白字符。
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
	// GitHub 风格的签名长这样：`sha256=<hex>`。
	// 这里先去掉前缀，再把十六进制字符串还原成原始字节。
	want, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	// hmac.New(sha256.New, key) 会创建一个基于 SHA-256 的 HMAC 计算器。
	// Write 把消息内容喂进去，Sum(nil) 得到最终摘要。
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	// hmac.Equal 使用常量时间比较，能避免普通 `==` 带来的时序侧信道问题。
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
	// Encoder 直接把 JSON 写进响应流，适合 HTTP handler 这种边编码边输出的场景。
	_ = json.NewEncoder(w).Encode(v)
}
