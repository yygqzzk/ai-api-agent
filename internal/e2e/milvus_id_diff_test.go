package e2e

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"ai-agent-api/internal/embedding"
	"ai-agent-api/internal/knowledge"
	"ai-agent-api/internal/rag"
	"ai-agent-api/internal/store"
	"ai-agent-api/internal/tools"
)

type recordingMilvusClient struct {
	base                 *store.InMemoryMilvusClient
	deleteByServiceCalls int
	deletedIDs           []string
}

var _ store.MilvusClient = (*recordingMilvusClient)(nil)

func newRecordingMilvusClient() *recordingMilvusClient {
	return &recordingMilvusClient{base: store.NewInMemoryMilvusClient()}
}

func (c *recordingMilvusClient) Upsert(ctx context.Context, collection string, docs []store.VectorDoc) error {
	return c.base.Upsert(ctx, collection, docs)
}

func (c *recordingMilvusClient) Search(ctx context.Context, collection string, vector []float32, topK int, filters map[string]string) ([]store.SearchResult, error) {
	return c.base.Search(ctx, collection, vector, topK, filters)
}

func (c *recordingMilvusClient) Query(ctx context.Context, collection string) ([]store.VectorDoc, error) {
	return c.base.Query(ctx, collection)
}

func (c *recordingMilvusClient) DeleteByService(ctx context.Context, collection string, service string) error {
	c.deleteByServiceCalls++
	return c.base.DeleteByService(ctx, collection, service)
}

func (c *recordingMilvusClient) DeleteByIDs(ctx context.Context, collection string, ids []string) error {
	c.deletedIDs = append(c.deletedIDs, ids...)
	return c.base.DeleteByIDs(ctx, collection, ids)
}

func (c *recordingMilvusClient) Close(ctx context.Context) error {
	return c.base.Close(ctx)
}

func TestKnowledgeBaseMilvusIDDiffDeletesOnlyRemovedChunks(t *testing.T) {
	ctx := context.Background()
	milvus := newRecordingMilvusClient()
	ragStore := rag.NewMilvusStore(milvus, embedding.NewNoopClient(8), "api_documents")
	kb := tools.NewKnowledgeBaseWithIngestor(knowledge.NewInMemoryIngestor(), ragStore)

	initialSpec := []byte(`{
		"swagger":"2.0",
		"info":{"title":"Petstore","version":"1.0.0"},
		"paths":{
			"/user/login":{"get":{"summary":"User login","responses":{"200":{"description":"ok"}}}},
			"/user/register":{"post":{"summary":"User register","responses":{"200":{"description":"ok"}}}}
		}
	}`)
	if _, err := kb.IngestBytes(ctx, initialSpec, "petstore"); err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}

	milvus.deleteByServiceCalls = 0
	milvus.deletedIDs = nil

	updatedSpec := []byte(`{
		"swagger":"2.0",
		"info":{"title":"Petstore","version":"1.0.0"},
		"paths":{
			"/user/login":{"get":{"summary":"User login updated","responses":{"200":{"description":"ok"}}}}
		}
	}`)
	if _, err := kb.IngestBytes(ctx, updatedSpec, "petstore"); err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}

	if milvus.deleteByServiceCalls != 0 {
		t.Fatalf("expected second ingest not to call DeleteByService, got %d", milvus.deleteByServiceCalls)
	}

	gotDeletedIDs := append([]string(nil), milvus.deletedIDs...)
	sort.Strings(gotDeletedIDs)
	wantDeletedIDs := []string{
		"petstore:POST:/user/register:dependency",
		"petstore:POST:/user/register:overview",
		"petstore:POST:/user/register:request",
		"petstore:POST:/user/register:response",
	}
	sort.Strings(wantDeletedIDs)
	if !reflect.DeepEqual(gotDeletedIDs, wantDeletedIDs) {
		t.Fatalf("deleted ids = %v, want %v", gotDeletedIDs, wantDeletedIDs)
	}

	docs, err := milvus.Query(ctx, "api_documents")
	if err != nil {
		t.Fatalf("query milvus docs failed: %v", err)
	}
	if len(docs) != 4 {
		t.Fatalf("expected 4 docs remain after diff update, got %d", len(docs))
	}
	for _, doc := range docs {
		if doc.Service != "petstore" {
			t.Fatalf("expected service petstore, got %s", doc.Service)
		}
		if !strings.HasPrefix(doc.ID, "petstore:GET:/user/login") {
			t.Fatalf("expected only login docs remain, got %s", doc.ID)
		}
	}
}
