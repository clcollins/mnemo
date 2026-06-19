package sqlite

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/clcollins/mnemo/internal/store"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath, 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrate(t *testing.T) {
	s := newTestStore(t)

	ctx := context.Background()
	status, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.SpaceCounts["product"] != 0 {
		t.Errorf("expected 0 product chunks, got %d", status.SpaceCounts["product"])
	}
	if status.SpaceCounts["default"] != 0 {
		t.Errorf("expected 0 default chunks, got %d", status.SpaceCounts["default"])
	}

	var count int
	err = s.db.QueryRow("SELECT count(*) FROM spaces").Scan(&count)
	if err != nil {
		t.Fatalf("query spaces: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 spaces, got %d", count)
	}
}

func TestVec0RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chunk, err := s.Remember(ctx, store.MemoryWrite{
		Content:    "test vector storage",
		Space:      "default",
		Source:     "test",
		Category:   "fact",
		Importance: 0.5,
		Embedding:  []float32{1.0, 0.0, 0.0, 0.0},
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	results, err := s.VectorSearch(ctx, []float32{1.0, 0.0, 0.0, 0.0}, store.Filter{NotForgotten: true}, 5)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Chunk.ID != chunk.ID {
		t.Errorf("expected chunk ID %s, got %s", chunk.ID, results[0].Chunk.ID)
	}
}

func TestRememberAndForget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chunk, err := s.Remember(ctx, store.MemoryWrite{
		Content:    "remember this fact",
		Space:      "default",
		Source:     "test",
		Category:   "fact",
		Importance: 0.5,
		Embedding:  []float32{0.5, 0.5, 0.5, 0.5},
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if chunk.ID == "" {
		t.Fatal("expected non-empty chunk ID")
	}
	if chunk.Content != "remember this fact" {
		t.Errorf("expected content 'remember this fact', got %q", chunk.Content)
	}

	err = s.Forget(ctx, chunk.ID)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}

	var forgotten int
	err = s.db.QueryRow("SELECT forgotten FROM chunks WHERE id = ?", chunk.ID).Scan(&forgotten)
	if err != nil {
		t.Fatalf("query forgotten: %v", err)
	}
	if forgotten != 1 {
		t.Errorf("expected forgotten=1, got %d", forgotten)
	}

	var importance float64
	err = s.db.QueryRow("SELECT importance FROM chunks WHERE id = ?", chunk.ID).Scan(&importance)
	if err != nil {
		t.Fatalf("query importance: %v", err)
	}
	if importance != 0 {
		t.Errorf("expected importance=0, got %f", importance)
	}
}

func TestUpsertChunks_Dedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chunks := []store.Chunk{
		{Content: "chunk one", ContentHash: "hash1", Source: "file1.md", HeadingPath: "Title", Category: "doc", Importance: 0.5, SpaceID: 1, Embedding: []float32{1, 0, 0, 0}},
		{Content: "chunk two", ContentHash: "hash2", Source: "file1.md", HeadingPath: "Title > Sub", Category: "doc", Importance: 0.5, SpaceID: 1, Embedding: []float32{0, 1, 0, 0}},
	}

	result, err := s.UpsertChunks(ctx, chunks)
	if err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	if result.ChunksAdded != 2 {
		t.Errorf("expected 2 added, got %d", result.ChunksAdded)
	}

	result, err = s.UpsertChunks(ctx, chunks)
	if err != nil {
		t.Fatalf("UpsertChunks second run: %v", err)
	}
	if result.ChunksSkipped != 2 {
		t.Errorf("expected 2 skipped, got %d", result.ChunksSkipped)
	}
	if result.ChunksAdded != 0 {
		t.Errorf("expected 0 added, got %d", result.ChunksAdded)
	}
}

func TestVectorSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	vecs := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
		{0.9, 0.1, 0, 0},
	}
	for i, v := range vecs {
		_, err := s.Remember(ctx, store.MemoryWrite{
			Content:    "chunk " + string(rune('A'+i)),
			Space:      "default",
			Source:     "test",
			Category:   "fact",
			Importance: 0.5,
			Embedding:  v,
		})
		if err != nil {
			t.Fatalf("Remember %d: %v", i, err)
		}
	}

	results, err := s.VectorSearch(ctx, []float32{1, 0, 0, 0}, store.Filter{NotForgotten: true}, 3)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Chunk.Content != "chunk A" {
		t.Errorf("expected closest match 'chunk A', got %q", results[0].Chunk.Content)
	}
	if results[1].Chunk.Content != "chunk E" {
		t.Errorf("expected second closest 'chunk E', got %q", results[1].Chunk.Content)
	}
}

func TestKeywordSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	contents := []string{
		"kubernetes operator migration guide",
		"alertmanager configuration settings",
		"vector log forwarding architecture",
	}
	for i, c := range contents {
		_, err := s.Remember(ctx, store.MemoryWrite{
			Content:    c,
			Space:      "default",
			Source:     "test",
			Category:   "fact",
			Importance: 0.5,
			Embedding:  []float32{float32(i), 0, 0, 0},
		})
		if err != nil {
			t.Fatalf("Remember %d: %v", i, err)
		}
	}

	results, err := s.KeywordSearch(ctx, `"alertmanager"`, store.Filter{NotForgotten: true}, 5)
	if err != nil {
		t.Fatalf("KeywordSearch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Chunk.Content != "alertmanager configuration settings" {
		t.Errorf("expected alertmanager chunk, got %q", results[0].Chunk.Content)
	}
}

func TestKeywordSearch_SpecialChars(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Remember(ctx, store.MemoryWrite{
		Content:    "a normal document",
		Space:      "default",
		Source:     "test",
		Category:   "fact",
		Importance: 0.5,
		Embedding:  []float32{1, 0, 0, 0},
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	_, err = s.KeywordSearch(ctx, `"what's" "the" "status?"`, store.Filter{NotForgotten: true}, 5)
	if err != nil {
		t.Fatalf("KeywordSearch with special chars should not error: %v", err)
	}
}

func TestFilter_SpaceAndForgotten(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Remember(ctx, store.MemoryWrite{
		Content: "product doc", Space: "product", Source: "test",
		Category: "doc", Importance: 0.5, Embedding: []float32{1, 0, 0, 0},
	})
	if err != nil {
		t.Fatalf("Remember product: %v", err)
	}

	chunk, err := s.Remember(ctx, store.MemoryWrite{
		Content: "episodic memory", Space: "default", Source: "test",
		Category: "fact", Importance: 0.5, Embedding: []float32{0, 1, 0, 0},
	})
	if err != nil {
		t.Fatalf("Remember default: %v", err)
	}

	results, err := s.VectorSearch(ctx, []float32{1, 0, 0, 0}, store.Filter{
		Spaces:       []string{"product"},
		NotForgotten: true,
	}, 10)
	if err != nil {
		t.Fatalf("VectorSearch product filter: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 product result, got %d", len(results))
	}

	err = s.Forget(ctx, chunk.ID)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}

	results, err = s.VectorSearch(ctx, []float32{0, 1, 0, 0}, store.Filter{NotForgotten: true}, 10)
	if err != nil {
		t.Fatalf("VectorSearch after forget: %v", err)
	}
	for _, r := range results {
		if r.Chunk.ID == chunk.ID {
			t.Error("forgotten chunk should not appear in results with NotForgotten=true")
		}
	}
}

func TestVectorSearch_RecencyOrdering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Remember(ctx, store.MemoryWrite{
		Content: "old memory", Space: "default", Source: "test",
		Category: "fact", Importance: 0.5, Embedding: []float32{1, 0, 0, 0},
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	since := time.Now().Add(time.Hour)
	results, err := s.VectorSearch(ctx, []float32{1, 0, 0, 0}, store.Filter{
		Since:        &since,
		NotForgotten: true,
	}, 10)
	if err != nil {
		t.Fatalf("VectorSearch with since filter: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results with future since, got %d", len(results))
	}
}

func TestStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.Remember(ctx, store.MemoryWrite{
		Content: "a", Space: "default", Source: "test",
		Category: "fact", Importance: 0.5, Embedding: []float32{1, 0, 0, 0},
	})
	_, _ = s.Remember(ctx, store.MemoryWrite{
		Content: "b", Space: "product", Source: "test",
		Category: "doc", Importance: 0.5, Embedding: []float32{0, 1, 0, 0},
	})
	_, _ = s.Remember(ctx, store.MemoryWrite{
		Content: "c", Space: "product", Source: "test",
		Category: "doc", Importance: 0.5, Embedding: []float32{0, 0, 1, 0},
	})

	status, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.TotalChunks != 3 {
		t.Errorf("expected 3 total chunks, got %d", status.TotalChunks)
	}
	if status.SpaceCounts["default"] != 1 {
		t.Errorf("expected 1 default chunk, got %d", status.SpaceCounts["default"])
	}
	if status.SpaceCounts["product"] != 2 {
		t.Errorf("expected 2 product chunks, got %d", status.SpaceCounts["product"])
	}
}

func normalizeVec(v []float32) []float32 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	mag := float32(math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = f / mag
	}
	return out
}
