package ingest

type Document struct {
	Path    string
	Content string
}

type RawChunk struct {
	Content     string
	ContentHash string
	Source      string
	HeadingPath string
}

type Chunker interface {
	Chunk(doc Document) ([]RawChunk, error)
}
