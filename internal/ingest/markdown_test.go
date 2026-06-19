package ingest

import (
	"strings"
	"testing"
)

func TestMarkdownChunker_HeadingSplit(t *testing.T) {
	doc := Document{
		Path: "test.md",
		Content: `# Title

Introduction paragraph.

## Section One

Content of section one.

## Section Two

Content of section two.

### Subsection

Subsection content.
`,
	}

	c := NewMarkdownChunker(2000, 0)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}
}

func TestMarkdownChunker_HeadingPath(t *testing.T) {
	doc := Document{
		Path: "test.md",
		Content: `# Top Level

Intro.

## Middle

Middle content.

### Deep

Deep content.
`,
	}

	c := NewMarkdownChunker(2000, 0)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}

	foundDeep := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "Deep content") {
			foundDeep = true
			if chunk.HeadingPath != "Top Level > Middle > Deep" {
				t.Errorf("expected heading path 'Top Level > Middle > Deep', got %q", chunk.HeadingPath)
			}
		}
	}
	if !foundDeep {
		t.Error("did not find chunk with deep content")
	}
}

func TestMarkdownChunker_CodeFencePreservation(t *testing.T) {
	doc := Document{
		Path:    "test.md",
		Content: "# Code Example\n\nSome text.\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nMore text after code.\n",
	}

	c := NewMarkdownChunker(2000, 0)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}

	for _, chunk := range chunks {
		openCount := strings.Count(chunk.Content, "```")
		if openCount%2 != 0 {
			t.Errorf("chunk has unbalanced code fences (count=%d): %q", openCount, chunk.Content[:min(100, len(chunk.Content))])
		}
	}
}

func TestMarkdownChunker_LargeSection(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# Big Section\n\n")
	for range 50 {
		sb.WriteString("This is a paragraph with enough words to contribute to the token count. ")
		sb.WriteString("It contains multiple sentences to make it realistic.\n\n")
	}

	doc := Document{Path: "test.md", Content: sb.String()}
	c := NewMarkdownChunker(100, 0)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 2 {
		t.Errorf("expected large section to split into multiple chunks, got %d", len(chunks))
	}
}

func TestMarkdownChunker_FrontmatterStripped(t *testing.T) {
	doc := Document{
		Path: "test.md",
		Content: `---
title: Test Document
date: 2026-06-17
tags: [test, example]
---

# Actual Content

The real content starts here.
`,
	}

	c := NewMarkdownChunker(2000, 0)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}

	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "title: Test Document") {
			t.Error("frontmatter should be stripped from chunk content")
		}
		if strings.Contains(chunk.Content, "tags: [test") {
			t.Error("frontmatter tags should be stripped")
		}
	}
}

func TestMarkdownChunker_ContentHash(t *testing.T) {
	doc := Document{
		Path:    "test.md",
		Content: "# Title\n\nSome content.\n",
	}

	c := NewMarkdownChunker(2000, 0)
	chunks1, _ := c.Chunk(doc)
	chunks2, _ := c.Chunk(doc)

	if len(chunks1) != len(chunks2) {
		t.Fatalf("chunk counts differ: %d vs %d", len(chunks1), len(chunks2))
	}
	for i := range chunks1 {
		if chunks1[i].ContentHash != chunks2[i].ContentHash {
			t.Errorf("chunk %d hash mismatch: %s vs %s", i, chunks1[i].ContentHash, chunks2[i].ContentHash)
		}
	}

	doc2 := Document{
		Path:    "test.md",
		Content: "# Title\n\nDifferent content.\n",
	}
	chunks3, _ := c.Chunk(doc2)
	if chunks1[0].ContentHash == chunks3[0].ContentHash {
		t.Error("different content should produce different hash")
	}
}

func TestMarkdownChunker_EmptyDocument(t *testing.T) {
	doc := Document{Path: "empty.md", Content: ""}
	c := NewMarkdownChunker(2000, 0)
	chunks, err := c.Chunk(doc)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty doc, got %d", len(chunks))
	}
}

func TestMarkdownChunker_Source(t *testing.T) {
	doc := Document{
		Path:    "/path/to/doc.md",
		Content: "# Title\n\nContent.\n",
	}

	c := NewMarkdownChunker(2000, 0)
	chunks, _ := c.Chunk(doc)
	for _, chunk := range chunks {
		if chunk.Source != "/path/to/doc.md" {
			t.Errorf("expected source '/path/to/doc.md', got %q", chunk.Source)
		}
	}
}
