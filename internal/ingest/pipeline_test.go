package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/store"
	"github.com/clcollins/mnemo/internal/store/sqlite"
)

func newTestStoreAndEmbedder(t *testing.T) (*sqlite.Store, *embed.FakeEmbedder) {
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
	return s, embed.NewFake(4)
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestPipeline_WalksRecursively(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	os.Mkdir(subdir, 0755)

	writeTestFile(t, dir, "top.md", "# Top\n\nContent.\n")
	writeTestFile(t, subdir, "nested.md", "# Nested\n\nNested content.\n")

	s, e := newTestStoreAndEmbedder(t)
	result, err := Ingest(context.Background(), dir, e, s, 400, 0)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.FilesProcessed != 2 {
		t.Errorf("expected 2 files processed, got %d", result.FilesProcessed)
	}
	if result.ChunksAdded < 2 {
		t.Errorf("expected at least 2 chunks added, got %d", result.ChunksAdded)
	}
}

func TestPipeline_SkipsUnsupported(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "doc.md", "# Doc\n\nContent.\n")
	writeTestFile(t, dir, "image.svg", "<svg></svg>")
	writeTestFile(t, dir, "binary.docx", "binary content")

	s, e := newTestStoreAndEmbedder(t)
	result, err := Ingest(context.Background(), dir, e, s, 400, 0)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.FilesProcessed != 1 {
		t.Errorf("expected 1 file processed (only .md), got %d", result.FilesProcessed)
	}
}

func TestPipeline_IncrementalReIngest(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "doc.md", "# Title\n\nSome content.\n")

	s, e := newTestStoreAndEmbedder(t)

	result1, err := Ingest(context.Background(), dir, e, s, 400, 0)
	if err != nil {
		t.Fatalf("First ingest: %v", err)
	}
	if result1.ChunksAdded == 0 {
		t.Fatal("expected chunks added on first ingest")
	}

	result2, err := Ingest(context.Background(), dir, e, s, 400, 0)
	if err != nil {
		t.Fatalf("Second ingest: %v", err)
	}
	if result2.ChunksAdded != 0 {
		t.Errorf("expected 0 chunks added on re-ingest, got %d", result2.ChunksAdded)
	}
	if result2.ChunksSkipped == 0 {
		t.Error("expected chunks to be skipped on re-ingest")
	}
}

func TestPipeline_ChangedContent(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "doc.md", "# Title\n\nOriginal content.\n")

	s, e := newTestStoreAndEmbedder(t)

	result1, _ := Ingest(context.Background(), dir, e, s, 400, 0)
	firstAdded := result1.ChunksAdded

	writeTestFile(t, dir, "doc.md", "# Title\n\nUpdated content with changes.\n")

	result2, err := Ingest(context.Background(), dir, e, s, 400, 0)
	if err != nil {
		t.Fatalf("Second ingest: %v", err)
	}
	if result2.ChunksAdded == 0 {
		t.Error("expected new chunks added after content change")
	}

	status, _ := s.Status(context.Background())
	if status.TotalChunks < firstAdded+result2.ChunksAdded {
		t.Error("total chunks should include both old and new")
	}
}

func TestPipeline_JSONChunking(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "operator.json", `{
		"rbac": {"rules": ["read", "write"]},
		"metrics": {"names": ["up", "errors"]},
		"crds": {"kinds": ["MyResource"]}
	}`)

	s, e := newTestStoreAndEmbedder(t)
	result, err := Ingest(context.Background(), dir, e, s, 400, 0)
	if err != nil {
		t.Fatalf("Ingest JSON: %v", err)
	}
	if result.FilesProcessed != 1 {
		t.Errorf("expected 1 file processed, got %d", result.FilesProcessed)
	}
	if result.ChunksAdded < 3 {
		t.Errorf("expected at least 3 chunks (one per top-level key), got %d", result.ChunksAdded)
	}

	vecResults, err := s.VectorSearch(context.Background(), []float32{1, 0, 0, 0},
		store.Filter{Spaces: []string{"product"}, NotForgotten: true}, 10)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}

	foundWithPath := false
	for _, r := range vecResults {
		if r.Chunk.HeadingPath != "" {
			foundWithPath = true
			break
		}
	}
	if !foundWithPath {
		t.Error("expected JSON chunks to have heading_path set")
	}
}

func TestPipeline_PlainText(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "notes.txt", "Some plain text notes about the system.")

	s, e := newTestStoreAndEmbedder(t)
	result, err := Ingest(context.Background(), dir, e, s, 400, 0)
	if err != nil {
		t.Fatalf("Ingest txt: %v", err)
	}
	if result.FilesProcessed != 1 {
		t.Errorf("expected 1 file processed, got %d", result.FilesProcessed)
	}
	if result.ChunksAdded != 1 {
		t.Errorf("expected 1 chunk for plain text, got %d", result.ChunksAdded)
	}
}
