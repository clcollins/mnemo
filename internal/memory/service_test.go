package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/store/sqlite"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := sqlite.New(dbPath, 4)
	if err != nil {
		t.Fatalf("New store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	return NewService(embed.NewFake(4), s)
}

func TestRecall(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, "kubernetes operator migration process", "", "", "")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	_, err = svc.Remember(ctx, "alertmanager configuration best practices", "", "", "")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	results, err := svc.Recall(ctx, "operator migration", nil, 5, 2000)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected recall results")
	}
}

func TestRemember(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	chunk, err := svc.Remember(ctx, "We decided to use RRF fusion", "default", "mcp", "human")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if chunk.ID == "" {
		t.Error("expected non-empty chunk ID")
	}
	if chunk.Content != "We decided to use RRF fusion" {
		t.Errorf("expected content preserved, got %q", chunk.Content)
	}

	status, _ := svc.Status(ctx)
	if status.TotalChunks != 1 {
		t.Errorf("expected 1 total chunk, got %d", status.TotalChunks)
	}
}

func TestRemember_DefaultSpace(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, "some fact", "", "", "")
	if err != nil {
		t.Fatalf("Remember with empty space: %v", err)
	}

	status, _ := svc.Status(ctx)
	if status.SpaceCounts["default"] != 1 {
		t.Errorf("expected 1 in default space, got %d", status.SpaceCounts["default"])
	}
}

func TestForget(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	chunk, _ := svc.Remember(ctx, "temporary note", "", "", "")
	err := svc.Forget(ctx, chunk.ID)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}

	results, _ := svc.Recall(ctx, "temporary note", nil, 5, 2000)
	for _, r := range results {
		if r.Chunk.ID == chunk.ID {
			t.Error("forgotten chunk should not appear in recall results")
		}
	}
}

func TestStatus(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.TotalChunks != 0 {
		t.Errorf("expected 0 chunks initially, got %d", status.TotalChunks)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		text       string
		category   string
		importance float64
	}{
		{"We decided to use cosine distance", "decision", 0.8},
		{"I learned that FTS5 needs quoting", "lesson", 0.75},
		{"I prefer shorter variable names", "preference", 0.7},
		{"Always run tests before committing", "pattern", 0.7},
		{"The server runs on port 8080", "fact", 0.5},
	}

	for _, tt := range tests {
		cat, imp := Classify(tt.text)
		if cat != tt.category {
			t.Errorf("Classify(%q): category = %q, want %q", tt.text, cat, tt.category)
		}
		if imp != tt.importance {
			t.Errorf("Classify(%q): importance = %f, want %f", tt.text, imp, tt.importance)
		}
	}
}

func TestRecall_SpaceFilter(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, _ = svc.Remember(ctx, "product doc about operators", "product", "", "")
	_, _ = svc.Remember(ctx, "personal note about operators", "default", "", "")

	results, err := svc.Recall(ctx, "operators", []string{"product"}, 5, 2000)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	for _, r := range results {
		if r.Chunk.SpaceID != 1 {
			t.Errorf("expected only product space results, got space_id=%d", r.Chunk.SpaceID)
		}
	}
}
