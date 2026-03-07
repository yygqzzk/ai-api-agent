package ingest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-agent-api/internal/knowledge"
)

type BytesIngestor interface {
	IngestBytes(ctx context.Context, body []byte, service string) (knowledge.IngestStats, error)
}

type Service struct {
	ingestor   BytesIngestor
	httpClient *http.Client
}

type SyncFile struct {
	Path       string `json:"path"`
	Action     string `json:"action,omitempty"`
	Service    string `json:"service,omitempty"`
	ContentURL string `json:"content_url,omitempty"`
	Content    string `json:"content,omitempty"`
}

type Result struct {
	File      string `json:"file"`
	Service   string `json:"service"`
	Endpoints int    `json:"endpoints"`
	Chunks    int    `json:"chunks"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func NewService(ingestor BytesIngestor, httpClient *http.Client) *Service {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Service{ingestor: ingestor, httpClient: httpClient}
}

func (s *Service) SyncFiles(ctx context.Context, files []SyncFile) ([]Result, error) {
	results := make([]Result, 0, len(files))
	for _, file := range files {
		result, err := s.ingestFile(ctx, file)
		if err != nil {
			results = append(results, Result{
				File:    filepath.Base(file.Path),
				Service: inferServiceName(file.Service, file.Path),
				Status:  "error",
				Error:   err.Error(),
			})
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) IngestContent(ctx context.Context, content []byte, service string, source string) (Result, error) {
	if s.ingestor == nil {
		return Result{}, fmt.Errorf("ingestor is nil")
	}
	serviceName := inferServiceName(service, source)
	stats, err := s.ingestor.IngestBytes(ctx, content, serviceName)
	if err != nil {
		return Result{}, err
	}
	return Result{
		File:      filepath.Base(source),
		Service:   serviceName,
		Endpoints: stats.Endpoints,
		Chunks:    stats.Chunks,
		Status:    "success",
	}, nil

}

func (s *Service) IngestFromURL(ctx context.Context, rawURL string, service string, source string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Result{}, fmt.Errorf("download file failed: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("read file body: %w", err)
	}
	return s.IngestContent(ctx, body, service, source)
}

func (s *Service) IngestFromFile(ctx context.Context, filePath string, service string) (Result, error) {
	// os.ReadFile 适合这种“读完整配置/文档文件再处理”的场景。
	body, err := os.ReadFile(filePath)
	if err != nil {
		return Result{}, fmt.Errorf("read file: %w", err)
	}
	return s.IngestContent(ctx, body, service, filePath)
}

func (s *Service) ingestFile(ctx context.Context, file SyncFile) (Result, error) {
	serviceName := inferServiceName(file.Service, file.Path)
	switch {
	case strings.TrimSpace(file.Content) != "":
		return s.IngestContent(ctx, []byte(file.Content), serviceName, file.Path)
	case strings.TrimSpace(file.ContentURL) != "":
		return s.IngestFromURL(ctx, file.ContentURL, serviceName, file.Path)
	default:
		return s.IngestFromFile(ctx, file.Path, serviceName)
	}
}

func inferServiceName(service string, path string) string {
	service = strings.TrimSpace(service)
	if service != "" {
		return service
	}
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "default-service"
	}
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "-")
	if name == "" {
		return "default-service"
	}
	return name
}
