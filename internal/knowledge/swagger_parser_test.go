package knowledge

import (
	"path/filepath"
	"testing"
)

func TestParseSwaggerFile(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "petstore.json")
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

func TestIngestorUpsert(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "petstore.json")
	ingestor := NewInMemoryIngestor()
	stats, err := ingestor.IngestFile(path, "petstore")
	if err != nil {
		t.Fatalf("IngestFile failed: %v", err)
	}

	if stats.Endpoints != 3 {
		t.Fatalf("expected 3 endpoints, got %d", stats.Endpoints)
	}
	if stats.Chunks == 0 {
		t.Fatalf("expected chunks > 0")
	}

	if got := len(ingestor.Endpoints()); got != 3 {
		t.Fatalf("expected ingestor hold 3 endpoints, got %d", got)
	}
}

func findEndpoint(endpoints []Endpoint, method, path string) *Endpoint {
	for i := range endpoints {
		if endpoints[i].Method == method && endpoints[i].Path == path {
			return &endpoints[i]
		}
	}
	return nil
}
