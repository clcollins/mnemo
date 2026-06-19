package store

import (
	"context"
	"time"
)

type Chunk struct {
	RowID       int64
	ID          string
	SpaceID     int64
	Content     string
	ContentHash string
	Source      string
	Speaker     string
	HeadingPath string
	Category    string
	Importance  float64
	Forgotten   bool
	Metadata    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Embedding   []float32
}

type MemoryWrite struct {
	Content    string
	Space      string
	Source     string
	Speaker    string
	Category   string
	Importance float64
	Embedding  []float32
}

type IngestResult struct {
	FilesProcessed int
	ChunksAdded    int
	ChunksSkipped  int
	ChunksUpdated  int
}

type Ranked struct {
	Chunk    Chunk
	Score    float64
	VecRank  int
	FTSRank  int
	Distance float64
}

type Filter struct {
	Spaces       []string
	Since        *time.Time
	NotForgotten bool
}

type Status struct {
	SpaceCounts map[string]int
	TotalChunks int
}

type Store interface {
	Remember(ctx context.Context, w MemoryWrite) (Chunk, error)
	Forget(ctx context.Context, chunkID string) error
	Status(ctx context.Context) (Status, error)

	UpsertChunks(ctx context.Context, chunks []Chunk) (IngestResult, error)

	VectorSearch(ctx context.Context, queryVec []float32, f Filter, k int) ([]Ranked, error)
	KeywordSearch(ctx context.Context, terms string, f Filter, k int) ([]Ranked, error)

	Migrate(ctx context.Context) error
	Close() error
}
