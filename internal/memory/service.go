package memory

import (
	"context"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/search"
	"github.com/clcollins/mnemo/internal/store"
)

type Service struct {
	embedder embed.Embedder
	store    store.Store
}

func NewService(embedder embed.Embedder, s store.Store) *Service {
	return &Service{
		embedder: embedder,
		store:    s,
	}
}

func (s *Service) Recall(ctx context.Context, query string, spaces []string, limit int, maxTokens int) ([]store.Ranked, error) {
	filter := store.Filter{
		Spaces:       spaces,
		NotForgotten: true,
	}
	return search.HybridSearch(ctx, query, s.embedder, s.store, filter, limit, maxTokens)
}

func (s *Service) Remember(ctx context.Context, text, space, source, speaker string) (store.Chunk, error) {
	if space == "" {
		space = "default"
	}
	if source == "" {
		source = "mcp"
	}

	vecs, err := s.embedder.Embed(ctx, []string{text})
	if err != nil {
		return store.Chunk{}, err
	}

	category, importance := Classify(text)

	return s.store.Remember(ctx, store.MemoryWrite{
		Content:    text,
		Space:      space,
		Source:     source,
		Speaker:    speaker,
		Category:   category,
		Importance: importance,
		Embedding:  vecs[0],
	})
}

func (s *Service) Forget(ctx context.Context, chunkID string) error {
	return s.store.Forget(ctx, chunkID)
}

func (s *Service) Status(ctx context.Context) (store.Status, error) {
	return s.store.Status(ctx)
}
