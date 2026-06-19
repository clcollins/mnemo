package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/store"
)

var supportedExtensions = map[string]bool{
	".md":   true,
	".json": true,
	".txt":  true,
	".html": true,
}

func Ingest(ctx context.Context, path string, embedder embed.Embedder, s store.Store, targetTokens, overlap int) (store.IngestResult, error) {
	var allChunks []RawChunk
	filesProcessed := 0

	err := filepath.Walk(path, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(fpath))
		if !supportedExtensions[ext] {
			return nil
		}

		data, err := os.ReadFile(fpath)
		if err != nil {
			slog.Warn("skipping unreadable file", "path", fpath, "error", err)
			return nil
		}

		doc := Document{Path: fpath, Content: string(data)}
		chunker := chunkerForExtension(ext, targetTokens, overlap)
		chunks, err := chunker.Chunk(doc)
		if err != nil {
			slog.Warn("chunking failed", "path", fpath, "error", err)
			return nil
		}

		allChunks = append(allChunks, chunks...)
		filesProcessed++
		return nil
	})
	if err != nil {
		return store.IngestResult{}, fmt.Errorf("walk %s: %w", path, err)
	}

	if len(allChunks) == 0 {
		return store.IngestResult{FilesProcessed: filesProcessed}, nil
	}

	texts := make([]string, len(allChunks))
	for i, c := range allChunks {
		texts[i] = c.Content
	}

	const batchSize = 10
	allEmbeddings := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return store.IngestResult{}, fmt.Errorf("embed batch [%d:%d]: %w", start, end, err)
		}
		copy(allEmbeddings[start:], batch)
	}

	storeChunks := make([]store.Chunk, len(allChunks))
	for i, raw := range allChunks {
		storeChunks[i] = store.Chunk{
			SpaceID:     1, // product space
			Content:     raw.Content,
			ContentHash: raw.ContentHash,
			Source:      raw.Source,
			HeadingPath: raw.HeadingPath,
			Category:    "doc",
			Importance:  0.5,
			Embedding:   allEmbeddings[i],
		}
	}

	result, err := s.UpsertChunks(ctx, storeChunks)
	if err != nil {
		return store.IngestResult{}, fmt.Errorf("upsert: %w", err)
	}
	result.FilesProcessed = filesProcessed

	slog.Info("ingestion complete",
		"files", filesProcessed,
		"added", result.ChunksAdded,
		"skipped", result.ChunksSkipped,
	)

	return result, nil
}

func chunkerForExtension(ext string, targetTokens, overlap int) Chunker {
	switch ext {
	case ".md":
		return NewMarkdownChunker(targetTokens, overlap)
	case ".json":
		return &JSONChunker{}
	default:
		return &PlainTextChunker{}
	}
}

type JSONChunker struct{}

func (j *JSONChunker) Chunk(doc Document) ([]RawChunk, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc.Content), &raw); err != nil {
		return []RawChunk{{
			Content:     doc.Content,
			ContentHash: hashContent(doc.Content),
			Source:      doc.Path,
			HeadingPath: filepath.Base(doc.Path),
		}}, nil
	}

	baseName := strings.TrimSuffix(filepath.Base(doc.Path), filepath.Ext(doc.Path))
	var chunks []RawChunk
	for key, val := range raw {
		content := string(val)
		chunks = append(chunks, RawChunk{
			Content:     content,
			ContentHash: hashContent(content),
			Source:      doc.Path,
			HeadingPath: baseName + " > " + key,
		})
	}
	return chunks, nil
}

type PlainTextChunker struct{}

func (p *PlainTextChunker) Chunk(doc Document) ([]RawChunk, error) {
	content := strings.TrimSpace(doc.Content)
	if content == "" {
		return nil, nil
	}
	return []RawChunk{{
		Content:     content,
		ContentHash: hashContent(content),
		Source:      doc.Path,
		HeadingPath: filepath.Base(doc.Path),
	}}, nil
}
