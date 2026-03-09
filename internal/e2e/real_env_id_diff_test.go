package e2e

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"wanzhi/internal/config"
	"wanzhi/internal/domain/rag"
	"wanzhi/internal/infra/milvus"
	"wanzhi/internal/infra/redis"
	"wanzhi/internal/domain/tool"

	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func TestKnowledgeBaseRedisMilvusIDDiffWithRealEnv(t *testing.T) {
	if os.Getenv("RUN_REAL_REDIS_MILVUS_E2E") != "1" {
		t.Skip("set RUN_REAL_REDIS_MILVUS_E2E=1 to run against real Redis/Milvus from environment")
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	ctx := context.Background()
	redisClient, err := redis.NewRedisClient(redis.RedisOptions{
		Mode:     "redis",
		Address:  cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		t.Fatalf("new redis client failed: %v", err)
	}
	defer func() { _ = redisClient.Close(ctx) }()

	milvusBase, err := milvus.NewSDKMilvusClient(ctx, cfg.Milvus.Address, cfg.RAG.EmbeddingDim)
	if err != nil {
		t.Fatalf("new milvus client failed: %v", err)
	}
	defer func() { _ = milvusBase.Close(ctx) }()

	milvusReal := &recordingMilvusProxy{base: milvusBase}
	ingestor := redis.NewRedisIngestor(redisClient)
	kb := tool.NewKnowledgeBaseWithStores(ingestor, rag.NewMemoryStore())

	service := fmt.Sprintf("id-diff-real-e2e-%d", time.Now().UnixNano())
	cleanupRealEnvArtifacts(t, ctx, redisClient, milvusReal, cfg.Milvus.Collection, service)
	defer cleanupRealEnvArtifacts(t, ctx, redisClient, milvusReal, cfg.Milvus.Collection, service)

	initialSpec := []byte(`{
		"swagger":"2.0",
		"info":{"title":"Petstore","version":"1.0.0"},
		"paths":{
			"/user/login":{"get":{"summary":"User login","responses":{"200":{"description":"ok"}}}},
			"/user/register":{"post":{"summary":"User register","responses":{"200":{"description":"ok"}}}}
		}
	}`)
	if _, err := kb.IngestBytes(ctx, initialSpec, service); err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}

	milvusReal.deleteByServiceCalls = 0
	milvusReal.deletedIDs = nil

	updatedSpec := []byte(`{
		"swagger":"2.0",
		"info":{"title":"Petstore","version":"1.0.0"},
		"paths":{
			"/user/login":{"get":{"summary":"User login updated","responses":{"200":{"description":"ok"}}}}
		}
	}`)
	if _, err := kb.IngestBytes(ctx, updatedSpec, service); err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}

	if milvusReal.deleteByServiceCalls != 0 {
		t.Fatalf("expected second ingest not to call DeleteByService, got %d", milvusReal.deleteByServiceCalls)
	}
	if len(milvusReal.deletedIDs) != 4 {
		t.Fatalf("expected 4 deleted ids, got %d (%v)", len(milvusReal.deletedIDs), milvusReal.deletedIDs)
	}
	for _, id := range milvusReal.deletedIDs {
		if !strings.HasPrefix(id, service+":POST:/user/register:") {
			t.Fatalf("expected deleted register ids, got %s", id)
		}
	}

	ids := waitForMilvusIDsByService(t, ctx, cfg.Milvus.Address, cfg.Milvus.Collection, service, 4)
	for _, id := range ids {
		if !strings.HasPrefix(id, service+":GET:/user/login:") {
			t.Fatalf("expected only login chunks remain in milvus, got %s", id)
		}
	}

	if _, err := kb.GetEndpoint(ctx, service, "GET", "/user/login"); err != nil {
		t.Fatalf("expected login endpoint still exists in redis: %v", err)
	}
	if _, err := kb.GetEndpoint(ctx, service, "POST", "/user/register"); err == nil {
		t.Fatalf("expected register endpoint removed from redis")
	}
}

type recordingMilvusProxy struct {
	base                 milvus.MilvusClient
	deleteByServiceCalls int
	deletedIDs           []string
}

var _ milvus.MilvusClient = (*recordingMilvusProxy)(nil)

func (c *recordingMilvusProxy) Upsert(ctx context.Context, collection string, docs []milvus.VectorDoc) error {
	return c.base.Upsert(ctx, collection, docs)
}

func (c *recordingMilvusProxy) Search(ctx context.Context, collection string, vector []float32, topK int, filters map[string]string) ([]milvus.SearchResult, error) {
	return c.base.Search(ctx, collection, vector, topK, filters)
}

func (c *recordingMilvusProxy) Query(ctx context.Context, collection string) ([]milvus.VectorDoc, error) {
	return c.base.Query(ctx, collection)
}

func (c *recordingMilvusProxy) DeleteByService(ctx context.Context, collection string, service string) error {
	c.deleteByServiceCalls++
	return c.base.DeleteByService(ctx, collection, service)
}

func (c *recordingMilvusProxy) DeleteByIDs(ctx context.Context, collection string, ids []string) error {
	c.deletedIDs = append(c.deletedIDs, ids...)
	return c.base.DeleteByIDs(ctx, collection, ids)
}

func (c *recordingMilvusProxy) Close(ctx context.Context) error {
	return c.base.Close(ctx)
}

func cleanupRealEnvArtifacts(t *testing.T, ctx context.Context, redisClient redis.RedisClient, milvusClient milvus.MilvusClient, collection string, service string) {
	t.Helper()
	_ = milvusClient.DeleteByService(ctx, collection, service)
	_ = redisClient.Del(ctx, fmt.Sprintf("kb:endpoints:%s", strings.ToLower(service)))
	_ = redisClient.Del(ctx, fmt.Sprintf("kb:chunks:%s", strings.ToLower(service)))
	_ = redisClient.Del(ctx, fmt.Sprintf("kb:specs:%s", strings.ToLower(service)))
}

func waitForMilvusIDsByService(t *testing.T, ctx context.Context, address string, collection string, service string, wantCount int) []string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		ids, err := queryMilvusIDsByService(ctx, address, collection, service)
		if err == nil && len(ids) == wantCount {
			sort.Strings(ids)
			return ids
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for milvus docs timed out: service=%s count=%d want=%d err=%v", service, len(ids), wantCount, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func queryMilvusIDsByService(ctx context.Context, address string, collection string, service string) ([]string, error) {
	client, err := milvusclient.NewClient(ctx, milvusclient.Config{Address: address})
	if err != nil {
		return nil, err
	}
	defer client.Close()

	if err := client.LoadCollection(ctx, collection, false); err != nil {
		return nil, err
	}

	results, err := client.Query(ctx, collection, nil, fmt.Sprintf(`service == "%s"`, service), []string{"id"}, milvusclient.WithLimit(64))
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0)
	for _, field := range results {
		col, ok := field.(*entity.ColumnVarChar)
		if !ok || col.Name() != "id" {
			continue
		}
		for i := 0; i < col.Len(); i++ {
			id, _ := col.ValueByIdx(i)
			ids = append(ids, id)
		}
		break
	}
	return ids, nil
}
