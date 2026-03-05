package knowledge

import "fmt"

type Endpoint struct {
	Service     string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
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

type Chunk struct {
	ID        string
	Service   string
	Endpoint  string
	Type      string
	Content   string
	Version   string
	DependsOn []string
}

type IngestStats struct {
	Endpoints int `json:"endpoints"`
	Chunks    int `json:"chunks"`
}
