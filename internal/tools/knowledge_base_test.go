package tools

import (
	"context"
	"reflect"
	"testing"

	"wanzhi/internal/knowledge"
	"wanzhi/internal/rag"
)

func TestSubtract(t *testing.T) {
	tests := []struct {
		name   string
		oldIDs []string
		newIDs []string
		want   []string
	}{
		{
			name:   "部分删除",
			oldIDs: []string{"a", "b", "c", "d"},
			newIDs: []string{"a", "b"},
			want:   []string{"c", "d"},
		},
		{
			name:   "无删除",
			oldIDs: []string{"a", "b"},
			newIDs: []string{"a", "b", "c"},
			want:   []string{},
		},
		{
			name:   "全部删除",
			oldIDs: []string{"a", "b"},
			newIDs: nil,
			want:   []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subtract(tt.oldIDs, tt.newIDs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("subtract() = %v, want %v", got, tt.want)
			}
		})
	}
}

type trackingStore struct {
	chunks               []knowledge.Chunk
	deleteByServiceCalls int
	deletedIDs           []string
}

func (s *trackingStore) Upsert(_ context.Context, chunks []knowledge.Chunk) error {
	index := make(map[string]int, len(s.chunks))
	for idx := range s.chunks {
		index[s.chunks[idx].ID] = idx
	}
	for _, chunk := range chunks {
		if idx, ok := index[chunk.ID]; ok {
			s.chunks[idx] = chunk
			continue
		}
		index[chunk.ID] = len(s.chunks)
		s.chunks = append(s.chunks, chunk)
	}
	return nil
}

func (s *trackingStore) Search(_ context.Context, _ string, _ int, _ string) ([]rag.ScoredChunk, error) {
	return nil, nil
}

func (s *trackingStore) DeleteByService(_ context.Context, _ string) error {
	s.deleteByServiceCalls++
	s.chunks = nil
	return nil
}

func (s *trackingStore) DeleteByIDs(_ context.Context, ids []string) error {
	s.deletedIDs = append(s.deletedIDs, ids...)
	if len(ids) == 0 {
		return nil
	}

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	filtered := s.chunks[:0]
	for _, chunk := range s.chunks {
		if _, ok := idSet[chunk.ID]; ok {
			continue
		}
		filtered = append(filtered, chunk)
	}
	s.chunks = filtered
	return nil
}

func (s *trackingStore) Close(_ context.Context) error {
	return nil
}

func TestKnowledgeBaseUpsertDocumentUsesIDDiff(t *testing.T) {
	ctx := context.Background()
	store := &trackingStore{}
	kb := NewKnowledgeBaseWithIngestor(knowledge.NewInMemoryIngestor(), store)

	initialDoc := knowledge.ParsedSpec{
		Meta: knowledge.SpecMeta{Service: "petstore"},
		Endpoints: []knowledge.Endpoint{
			{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "login"},
			{Service: "petstore", Method: "POST", Path: "/user/register", Summary: "register"},
		},
	}
	if _, err := kb.upsertDocument(ctx, initialDoc); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}
	store.deleteByServiceCalls = 0
	store.deletedIDs = nil

	updatedDoc := knowledge.ParsedSpec{
		Meta: knowledge.SpecMeta{Service: "petstore"},
		Endpoints: []knowledge.Endpoint{
			{Service: "petstore", Method: "GET", Path: "/user/login", Summary: "login updated"},
		},
	}
	if _, err := kb.upsertDocument(ctx, updatedDoc); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	if store.deleteByServiceCalls != 0 {
		t.Fatalf("expected ID diff path without DeleteByService, got %d calls", store.deleteByServiceCalls)
	}

	if len(store.deletedIDs) != 4 {
		t.Fatalf("expected 4 removed chunk IDs, got %d (%v)", len(store.deletedIDs), store.deletedIDs)
	}

	remaining := make(map[string]knowledge.Chunk, len(store.chunks))
	for _, chunk := range store.chunks {
		remaining[chunk.ID] = chunk
	}
	if len(remaining) != 4 {
		t.Fatalf("expected 4 remaining chunks, got %d", len(remaining))
	}
	for _, chunkType := range []string{"overview", "request", "response", "dependency"} {
		id := "petstore:GET:/user/login:" + chunkType
		if _, ok := remaining[id]; !ok {
			t.Fatalf("expected chunk %s to remain", id)
		}
	}
}
