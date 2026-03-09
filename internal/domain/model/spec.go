package model

import (
	"fmt"
	"path"
	"strings"
)

// SpecMeta 表示 API 规范的元数据
type SpecMeta struct {
	Service  string   `json:"service"`
	Title    string   `json:"title,omitempty"`
	Version  string   `json:"version,omitempty"`
	Host     string   `json:"host,omitempty"`
	BasePath string   `json:"base_path,omitempty"`
	Schemes  []string `json:"schemes,omitempty"`
}

// URLForPath 生成指定路径的完整 URL
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

// ParsedSpec 表示解析后的 API 规范
type ParsedSpec struct {
	Meta      SpecMeta
	Endpoints []Endpoint
}

// IngestStats 表示录入统计信息
type IngestStats struct {
	Endpoints int `json:"endpoints"`
	Chunks    int `json:"chunks"`
}
