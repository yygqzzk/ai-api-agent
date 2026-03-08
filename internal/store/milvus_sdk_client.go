package store

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// SDKMilvusClient implements MilvusClient using the Milvus Go SDK.
type SDKMilvusClient struct {
	client client.Client
	dim    int

	mu      sync.Mutex
	ensured map[string]bool
}

func NewSDKMilvusClient(ctx context.Context, address string, dim int) (*SDKMilvusClient, error) {
	c, err := client.NewClient(ctx, client.Config{Address: address})
	if err != nil {
		return nil, fmt.Errorf("connect milvus at %s: %w", address, err)
	}
	return &SDKMilvusClient{
		client:  c,
		dim:     dim,
		ensured: make(map[string]bool),
	}, nil
}

func (c *SDKMilvusClient) ensureCollection(ctx context.Context, collection string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ensured[collection] {
		return nil
	}

	has, err := c.client.HasCollection(ctx, collection)
	if err != nil {
		return fmt.Errorf("check collection: %w", err)
	}
	if !has {
		schema := entity.NewSchema().WithName(collection).WithAutoID(false).
			WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(256).WithIsPrimaryKey(true)).
			WithField(entity.NewField().WithName("service").WithDataType(entity.FieldTypeVarChar).WithMaxLength(128)).
			WithField(entity.NewField().WithName("endpoint").WithDataType(entity.FieldTypeVarChar).WithMaxLength(256)).
			WithField(entity.NewField().WithName("chunk_type").WithDataType(entity.FieldTypeVarChar).WithMaxLength(64)).
			WithField(entity.NewField().WithName("content").WithDataType(entity.FieldTypeVarChar).WithMaxLength(65535)).
			WithField(entity.NewField().WithName("version").WithDataType(entity.FieldTypeVarChar).WithMaxLength(64)).
			WithField(entity.NewField().WithName("vector").WithDataType(entity.FieldTypeFloatVector).WithDim(int64(c.dim)))

		if err := c.client.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
			return fmt.Errorf("create collection: %w", err)
		}

		idx, err := entity.NewIndexIvfFlat(entity.IP, 128)
		if err != nil {
			return fmt.Errorf("create index params: %w", err)
		}
		if err := c.client.CreateIndex(ctx, collection, "vector", idx, false); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	if err := c.client.LoadCollection(ctx, collection, false); err != nil {
		return fmt.Errorf("load collection: %w", err)
	}

	c.ensured[collection] = true
	return nil
}

func (c *SDKMilvusClient) Upsert(ctx context.Context, collection string, docs []VectorDoc) error {
	if err := c.ensureCollection(ctx, collection); err != nil {
		return err
	}

	ids := make([]string, len(docs))
	services := make([]string, len(docs))
	endpoints := make([]string, len(docs))
	chunkTypes := make([]string, len(docs))
	contents := make([]string, len(docs))
	versions := make([]string, len(docs))
	vectors := make([][]float32, len(docs))

	for i, doc := range docs {
		ids[i] = doc.ID
		services[i] = doc.Service
		endpoints[i] = doc.Meta["endpoint"]
		chunkTypes[i] = doc.Meta["chunk_type"]
		contents[i] = doc.Text
		versions[i] = doc.Meta["version"]
		vectors[i] = doc.Vector
	}

	columns := []entity.Column{
		entity.NewColumnVarChar("id", ids),
		entity.NewColumnVarChar("service", services),
		entity.NewColumnVarChar("endpoint", endpoints),
		entity.NewColumnVarChar("chunk_type", chunkTypes),
		entity.NewColumnVarChar("content", contents),
		entity.NewColumnVarChar("version", versions),
		entity.NewColumnFloatVector("vector", c.dim, vectors),
	}

	if _, err := c.client.Upsert(ctx, collection, "", columns...); err != nil {
		return fmt.Errorf("upsert: %w", err)
	}
	return nil
}

func (c *SDKMilvusClient) Search(ctx context.Context, collection string, vector []float32, topK int, filters map[string]string) ([]SearchResult, error) {
	if err := c.ensureCollection(ctx, collection); err != nil {
		return nil, err
	}

	sp, err := entity.NewIndexIvfFlatSearchParam(16)
	if err != nil {
		return nil, fmt.Errorf("search params: %w", err)
	}

	expr := buildFilterExpr(filters)
	outputFields := []string{"id", "service", "endpoint", "chunk_type", "content", "version"}
	queryVectors := []entity.Vector{entity.FloatVector(vector)}

	searchResults, err := c.client.Search(ctx, collection, nil, expr, outputFields, queryVectors, "vector", entity.IP, topK, sp)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	var results []SearchResult
	for _, sr := range searchResults {
		for i := 0; i < sr.ResultCount; i++ {
			doc := VectorDoc{Meta: make(map[string]string)}
			for _, field := range sr.Fields {
				col, ok := field.(*entity.ColumnVarChar)
				if !ok {
					continue
				}
				val, _ := col.ValueByIdx(i)
				switch col.Name() {
				case "id":
					doc.ID = val
				case "service":
					doc.Service = val
				case "endpoint":
					doc.Meta["endpoint"] = val
				case "chunk_type":
					doc.Meta["chunk_type"] = val
				case "content":
					doc.Text = val
				case "version":
					doc.Meta["version"] = val
				}
			}
			results = append(results, SearchResult{Doc: doc, Score: sr.Scores[i]})
		}
	}
	return results, nil
}

func (c *SDKMilvusClient) Query(ctx context.Context, collection string) ([]VectorDoc, error) {
	if err := c.ensureCollection(ctx, collection); err != nil {
		return nil, err
	}

	outputFields := []string{"id", "service", "content"}
	// Newer Milvus versions require a positive limit when expression is empty.
	results, err := c.client.Query(ctx, collection, nil, "", outputFields, client.WithLimit(1))
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	var docs []VectorDoc
	if len(results) == 0 {
		return docs, nil
	}

	// Extract docs from column-based results
	var idCol, serviceCol, contentCol *entity.ColumnVarChar
	for _, col := range results {
		vc, ok := col.(*entity.ColumnVarChar)
		if !ok {
			continue
		}
		switch vc.Name() {
		case "id":
			idCol = vc
		case "service":
			serviceCol = vc
		case "content":
			contentCol = vc
		}
	}

	if idCol == nil {
		return docs, nil
	}

	for i := 0; i < idCol.Len(); i++ {
		doc := VectorDoc{}
		doc.ID, _ = idCol.ValueByIdx(i)
		if serviceCol != nil {
			doc.Service, _ = serviceCol.ValueByIdx(i)
		}
		if contentCol != nil {
			doc.Text, _ = contentCol.ValueByIdx(i)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (c *SDKMilvusClient) DeleteByService(ctx context.Context, collection string, service string) error {
	if err := c.ensureCollection(ctx, collection); err != nil {
		return err
	}
	// Milvus 支持通过表达式批量删除；service 字段已在 schema 中定义为 VarChar，可直接用于过滤。
	expr := fmt.Sprintf(`service == "%s"`, service)
	return c.client.Delete(ctx, collection, "", expr)
}

func (c *SDKMilvusClient) DeleteByIDs(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := c.ensureCollection(ctx, collection); err != nil {
		return err
	}

	quotedIDs := make([]string, len(ids))
	for i, id := range ids {
		quotedIDs[i] = fmt.Sprintf(`"%s"`, strings.ReplaceAll(id, `"`, `\\"`))
	}
	return c.client.Delete(ctx, collection, "", fmt.Sprintf("id in [%s]", strings.Join(quotedIDs, ",")))
}

func (c *SDKMilvusClient) Close(_ context.Context) error {
	return c.client.Close()
}

func buildFilterExpr(filters map[string]string) string {
	if len(filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filters))
	for k, v := range filters {
		parts = append(parts, fmt.Sprintf(`%s == "%s"`, k, v))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	expr := parts[0]
	for _, p := range parts[1:] {
		expr += " && " + p
	}
	return expr
}
