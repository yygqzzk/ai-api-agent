package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

var validMethods = map[string]bool{
	"get":     true,
	"post":    true,
	"put":     true,
	"delete":  true,
	"patch":   true,
	"options": true,
	"head":    true,
}

type swaggerDoc struct {
	Swagger  string   `json:"swagger"`
	Host     string   `json:"host"`
	BasePath string   `json:"basePath"`
	Schemes  []string `json:"schemes"`
	Info     struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths map[string]map[string]swaggerOperation `json:"paths"`
}

type swaggerOperation struct {
	Summary     string             `json:"summary"`
	Description string             `json:"description"`
	Tags        []string           `json:"tags"`
	Deprecated  bool               `json:"deprecated"`
	Parameters  []swaggerParameter `json:"parameters"`
	Responses   map[string]struct {
		Description string `json:"description"`
	} `json:"responses"`
}

type swaggerParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Schema      *struct {
		Ref string `json:"$ref"`
	} `json:"schema"`
}

func ParseSwaggerFile(path string, service string) ([]Endpoint, error) {
	doc, err := ParseSwaggerDocumentFile(path, service)
	if err != nil {
		return nil, err
	}
	return doc.Endpoints, nil
}

func ParseSwaggerDocumentFile(path string, service string) (ParsedSpec, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ParsedSpec{}, fmt.Errorf("read swagger file: %w", err)
	}
	return ParseSwaggerDocumentBytes(body, service)
}

func ParseSwaggerBytes(body []byte, service string) ([]Endpoint, error) {
	doc, err := ParseSwaggerDocumentBytes(body, service)
	if err != nil {
		return nil, err
	}
	return doc.Endpoints, nil
}

func ParseSwaggerDocumentBytes(body []byte, service string) (ParsedSpec, error) {
	var doc swaggerDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return ParsedSpec{}, fmt.Errorf("decode swagger: %w", err)
	}

	svc := strings.TrimSpace(service)
	if svc == "" {
		svc = normalizeServiceName(doc.Info.Title)
	}

	paths := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	endpoints := make([]Endpoint, 0, len(paths))
	for _, path := range paths {
		ops := doc.Paths[path]
		methods := make([]string, 0, len(ops))
		for method := range ops {
			if validMethods[strings.ToLower(method)] {
				methods = append(methods, method)
			}
		}
		sort.Strings(methods)

		for _, method := range methods {
			op := ops[method]
			ep := Endpoint{
				Service:     svc,
				Method:      strings.ToUpper(method),
				Path:        path,
				Summary:     strings.TrimSpace(op.Summary),
				Description: strings.TrimSpace(op.Description),
				Tags:        append([]string(nil), op.Tags...),
				Deprecated:  op.Deprecated,
				Parameters:  make([]Parameter, 0, len(op.Parameters)),
			}

			for _, p := range op.Parameters {
				param := Parameter{
					Name:        p.Name,
					In:          p.In,
					Required:    p.Required,
					Type:        p.Type,
					Description: p.Description,
				}
				if p.Schema != nil {
					param.SchemaRef = p.Schema.Ref
				}
				ep.Parameters = append(ep.Parameters, param)
			}

			responseCodes := make([]string, 0, len(op.Responses))
			for code := range op.Responses {
				responseCodes = append(responseCodes, code)
			}
			sort.Strings(responseCodes)
			ep.Responses = make([]Response, 0, len(responseCodes))
			for _, code := range responseCodes {
				ep.Responses = append(ep.Responses, Response{
					StatusCode:  code,
					Description: op.Responses[code].Description,
				})
			}
			endpoints = append(endpoints, ep)
		}
	}

	meta := SpecMeta{
		Service:  svc,
		Title:    strings.TrimSpace(doc.Info.Title),
		Version:  strings.TrimSpace(doc.Info.Version),
		Host:     strings.TrimSpace(doc.Host),
		BasePath: normalizePathPrefix(doc.BasePath),
		Schemes:  normalizeSchemes(doc.Schemes),
	}

	return ParsedSpec{Meta: meta, Endpoints: endpoints}, nil
}

func normalizeServiceName(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return "default-service"
	}
	v = strings.ReplaceAll(v, " ", "-")
	return v
}

func normalizeSchemes(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(raw))
	for _, scheme := range raw {
		v := strings.TrimSpace(strings.ToLower(scheme))
		if v == "" {
			continue
		}
		normalized = append(normalized, v)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
