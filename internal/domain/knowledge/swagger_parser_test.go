package knowledge

import (
	"path/filepath"
	"strings"
	"testing"

	"wanzhi/internal/domain/model"
)

func TestParseSwaggerFile(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "petstore.json")
	endpoints, err := ParseSwaggerFile(path, "petstore")
	if err != nil {
		t.Fatalf("ParseSwaggerFile failed: %v", err)
	}

	if len(endpoints) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(endpoints))
	}

	login := findEndpoint(endpoints, "GET", "/user/login")
	if login == nil {
		t.Fatalf("expected GET /user/login endpoint")
	}

	if login.Summary == "" {
		t.Fatalf("expected login summary")
	}
	if len(login.Parameters) != 2 {
		t.Fatalf("expected 2 login params, got %d", len(login.Parameters))
	}
	if !login.Parameters[0].Required {
		t.Fatalf("expected first login param to be required")
	}
	if len(login.Responses) != 2 {
		t.Fatalf("expected 2 login responses, got %d", len(login.Responses))
	}

	pet := findEndpoint(endpoints, "POST", "/pet")
	if pet == nil {
		t.Fatalf("expected POST /pet endpoint")
	}
	if pet.Parameters[0].SchemaRef != "#/definitions/Pet" {
		t.Fatalf("unexpected schema ref: %s", pet.Parameters[0].SchemaRef)
	}
}

func TestParseSwaggerDocumentFileIncludesSpecMeta(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "petstore.json")
	doc, err := ParseSwaggerDocumentFile(path, "petstore")
	if err != nil {
		t.Fatalf("ParseSwaggerDocumentFile failed: %v", err)
	}

	if doc.Meta.Service != "petstore" {
		t.Fatalf("expected service petstore, got %q", doc.Meta.Service)
	}
	if doc.Meta.Host != "petstore.swagger.io" {
		t.Fatalf("expected host petstore.swagger.io, got %q", doc.Meta.Host)
	}
	if doc.Meta.BasePath != "/v2" {
		t.Fatalf("expected basePath /v2, got %q", doc.Meta.BasePath)
	}
	if len(doc.Meta.Schemes) != 1 || doc.Meta.Schemes[0] != "https" {
		t.Fatalf("expected https scheme, got %+v", doc.Meta.Schemes)
	}
	if got := doc.Meta.URLForPath("/user/login"); got != "https://petstore.swagger.io/v2/user/login" {
		t.Fatalf("unexpected request url: %s", got)
	}
	if len(doc.Endpoints) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(doc.Endpoints))
	}
}

func TestParseSwaggerDocumentBytesParsesDeprecatedFlag(t *testing.T) {
	body := []byte(strings.TrimSpace(`
{
  "swagger": "2.0",
  "info": {
    "title": "Deprecated API",
    "version": "1.0.0"
  },
  "paths": {
    "/legacy": {
      "get": {
        "summary": "legacy endpoint",
        "deprecated": true,
        "responses": {
          "200": {"description": "ok"}
        }
      }
    },
    "/active": {
      "get": {
        "summary": "active endpoint",
        "responses": {
          "200": {"description": "ok"}
        }
      }
    }
  }
}
`))

	doc, err := ParseSwaggerDocumentBytes(body, "deprecated-api")
	if err != nil {
		t.Fatalf("ParseSwaggerDocumentBytes failed: %v", err)
	}

	legacy := findEndpoint(doc.Endpoints, "GET", "/legacy")
	if legacy == nil {
		t.Fatalf("expected GET /legacy endpoint")
	}
	if !legacy.Deprecated {
		t.Fatalf("expected GET /legacy to be deprecated")
	}

	active := findEndpoint(doc.Endpoints, "GET", "/active")
	if active == nil {
		t.Fatalf("expected GET /active endpoint")
	}
	if active.Deprecated {
		t.Fatalf("expected GET /active not deprecated")
	}
}

func TestIngestorUpsert(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "petstore.json")
	doc, err := ParseSwaggerDocumentFile(path, "petstore")
	if err != nil {
		t.Fatalf("ParseSwaggerDocumentFile failed: %v", err)
	}

	ingestor := NewMemoryIngestor()
	stats := ingestor.UpsertDocument(doc)

	if stats.Endpoints != 3 {
		t.Fatalf("expected 3 endpoints, got %d", stats.Endpoints)
	}
	if stats.Chunks == 0 {
		t.Fatalf("expected chunks > 0")
	}

	if got := len(ingestor.Endpoints()); got != 3 {
		t.Fatalf("expected ingestor hold 3 endpoints, got %d", got)
	}

	meta, ok := ingestor.SpecMeta("PETSTORE")
	if !ok {
		t.Fatalf("expected spec meta found")
	}
	if meta.Host != "petstore.swagger.io" {
		t.Fatalf("expected host petstore.swagger.io, got %q", meta.Host)
	}

	dependency := findChunk(ingestor.Chunks(), "GET /user/login", "dependency")
	if dependency == nil {
		t.Fatalf("expected login dependency chunk")
	}
	if dependency.Content != "接口依赖信息暂不可用" {
		t.Fatalf("expected dependency placeholder, got %q", dependency.Content)
	}
}

func findEndpoint(endpoints []model.Endpoint, method, path string) *model.Endpoint {
	for i := range endpoints {
		if endpoints[i].Method == method && endpoints[i].Path == path {
			return &endpoints[i]
		}
	}
	return nil
}

func findChunk(chunks []model.Chunk, endpoint, chunkType string) *model.Chunk {
	for i := range chunks {
		if chunks[i].Endpoint == endpoint && chunks[i].Type == chunkType {
			return &chunks[i]
		}
	}
	return nil
}
