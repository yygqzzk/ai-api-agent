package knowledge

import (
	"fmt"
	"path"
	"strings"
)

// Deprecated 标记该接口是否已废弃（从 OpenAPI deprecated 属性解析）
type Endpoint struct {
	Service     string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Deprecated  bool
	Parameters  []Parameter
	Responses   []Response
}

func (e Endpoint) Key() string {
	return fmt.Sprintf("%s:%s:%s", e.Service, e.Method, e.Path)
}

func (e Endpoint) DisplayName() string {
	return fmt.Sprintf("%s %s", e.Method, e.Path)
}

type Parameter struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Description string
	SchemaRef   string
}

type Response struct {
	StatusCode  string
	Description string
}

type SpecMeta struct {
	Service  string   `json:"service"`
	Title    string   `json:"title,omitempty"`
	Version  string   `json:"version,omitempty"`
	Host     string   `json:"host,omitempty"`
	BasePath string   `json:"base_path,omitempty"`
	Schemes  []string `json:"schemes,omitempty"`
}

func (m SpecMeta) URLForPath(endpointPath string) string {
	fullPath := joinURLPath(m.BasePath, endpointPath)
	host := strings.TrimSpace(m.Host)
	if host == "" {
		return fullPath
	}
	scheme := "https"
	for _, item := range m.Schemes {
		candidate := strings.TrimSpace(item)
		if candidate != "" {
			scheme = candidate
			break
		}
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, fullPath)
}

func joinURLPath(basePath string, endpointPath string) string {
	base := normalizePathPrefix(basePath)
	endpoint := normalizePathPrefix(endpointPath)
	switch {
	case base == "" && endpoint == "":
		return "/"
	case base == "":
		return endpoint
	case endpoint == "":
		return base
	default:
		joined := path.Join(base, endpoint)
		if !strings.HasPrefix(joined, "/") {
			joined = "/" + joined
		}
		return joined
	}
}

func normalizePathPrefix(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || v == "/" {
		return ""
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	return strings.TrimRight(v, "/")
}

type ParsedSpec struct {
	Meta      SpecMeta
	Endpoints []Endpoint
}

type Chunk struct {
	ID       string
	Service  string
	Endpoint string
	Type     string
	Content  string
	Version  string
}

type IngestStats struct {
	Endpoints int `json:"endpoints"`
	Chunks    int `json:"chunks"`
}
