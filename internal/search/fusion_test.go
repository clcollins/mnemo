package search

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/store"
)

type fakeStore struct {
	vecResults []store.Ranked
	ftsResults []store.Ranked
}

func (f *fakeStore) VectorSearch(_ context.Context, _ []float32, _ store.Filter, _ int) ([]store.Ranked, error) {
	return f.vecResults, nil
}

func (f *fakeStore) KeywordSearch(_ context.Context, _ string, _ store.Filter, _ int) ([]store.Ranked, error) {
	return f.ftsResults, nil
}

func makeRanked(id string, content string, importance float64, age time.Duration) store.Ranked {
	return store.Ranked{
		Chunk: store.Chunk{
			ID:         id,
			Content:    content,
			Importance: importance,
			CreatedAt:  time.Now().Add(-age),
		},
	}
}

func TestRRF_VectorOnlyResults(t *testing.T) {
	fs := &fakeStore{
		vecResults: []store.Ranked{
			makeRanked("a", "first result", 0.5, 0),
			makeRanked("b", "second result", 0.5, 0),
		},
		ftsResults: nil,
	}

	results, err := HybridSearch(context.Background(), "test query",
		embed.NewFake(4), fs, store.Filter{NotForgotten: true}, 5, 2000)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Score <= 0 {
		t.Error("expected positive score")
	}
	if results[0].Score <= results[1].Score {
		t.Error("first result should have higher score than second")
	}
}

func TestRRF_BothArms(t *testing.T) {
	shared := makeRanked("shared", "appears in both arms", 0.5, 0)
	vecOnly := makeRanked("vec-only", "vector only result", 0.5, 0)
	ftsOnly := makeRanked("fts-only", "keyword only result", 0.5, 0)

	fs := &fakeStore{
		vecResults: []store.Ranked{shared, vecOnly},
		ftsResults: []store.Ranked{shared, ftsOnly},
	}

	results, err := HybridSearch(context.Background(), "test query",
		embed.NewFake(4), fs, store.Filter{NotForgotten: true}, 5, 2000)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}

	var sharedResult *store.Ranked
	for _, r := range results {
		if r.Chunk.ID == "shared" {
			sharedResult = &r
			break
		}
	}
	if sharedResult == nil {
		t.Fatal("shared result not found")
	}

	for _, r := range results {
		if r.Chunk.ID != "shared" && r.Score >= sharedResult.Score {
			t.Errorf("chunk %s (score %f) should rank below shared (score %f)",
				r.Chunk.ID, r.Score, sharedResult.Score)
		}
	}
}

func TestRRF_ImportanceBoost(t *testing.T) {
	high := makeRanked("high", "important result", 0.9, 0)
	low := makeRanked("low", "unimportant result", 0.1, 0)

	fs := &fakeStore{
		vecResults: []store.Ranked{low, high},
	}

	results, err := HybridSearch(context.Background(), "test query",
		embed.NewFake(4), fs, store.Filter{NotForgotten: true}, 5, 2000)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	var highScore, lowScore float64
	for _, r := range results {
		switch r.Chunk.ID {
		case "high":
			highScore = r.Score
		case "low":
			lowScore = r.Score
		}
	}
	if highScore <= lowScore {
		t.Errorf("high-importance (%f) should score above low-importance (%f)", highScore, lowScore)
	}
}

func TestRRF_RecencyBoost(t *testing.T) {
	recent := makeRanked("recent", "recent content", 0.5, time.Hour)
	old := makeRanked("old", "old content", 0.5, 365*24*time.Hour)

	fs := &fakeStore{
		vecResults: []store.Ranked{old, recent},
	}

	results, err := HybridSearch(context.Background(), "test query",
		embed.NewFake(4), fs, store.Filter{NotForgotten: true}, 5, 2000)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}

	var recentScore, oldScore float64
	for _, r := range results {
		switch r.Chunk.ID {
		case "recent":
			recentScore = r.Score
		case "old":
			oldScore = r.Score
		}
	}
	if recentScore <= oldScore {
		t.Errorf("recent (%f) should score above old (%f)", recentScore, oldScore)
	}
}

func TestTokenBudget(t *testing.T) {
	var vecResults []store.Ranked
	for i := range 5 {
		r := makeRanked(
			string(rune('a'+i)),
			strings.Repeat("word ", 200),
			0.5,
			0,
		)
		vecResults = append(vecResults, r)
	}

	fs := &fakeStore{vecResults: vecResults}

	results, err := HybridSearch(context.Background(), "test query",
		embed.NewFake(4), fs, store.Filter{NotForgotten: true}, 5, 500)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	foundTruncated := false
	for _, r := range results {
		if strings.HasSuffix(r.Chunk.Content, "[truncated]") {
			foundTruncated = true
			break
		}
	}
	if !foundTruncated {
		t.Error("expected at least one truncated result with small token budget")
	}
}

func TestExtractKeyTerms(t *testing.T) {
	terms := extractKeyTerms("How does the alertmanager operator work?")
	if len(terms) == 0 {
		t.Fatal("expected non-empty terms")
	}

	joined := strings.Join(terms, " ")
	if !strings.Contains(joined, "alertmanager") {
		t.Error("expected 'alertmanager' in key terms")
	}
	if !strings.Contains(joined, "operator") {
		t.Error("expected 'operator' in key terms")
	}

	for _, term := range terms {
		if term == "how" || term == "does" || term == "the" {
			t.Errorf("stopword %q should be filtered", term)
		}
	}
}

func TestBuildFTSQuery(t *testing.T) {
	query := buildFTSQuery([]string{"alert", "manager", "config"})
	if query == "" {
		t.Fatal("expected non-empty FTS query")
	}
	if !strings.Contains(query, `"alert"`) {
		t.Error("expected quoted term 'alert'")
	}
}
