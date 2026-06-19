package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

type FakeEmbedder struct {
	dim int
}

func NewFake(dim int) *FakeEmbedder {
	return &FakeEmbedder{dim: dim}
}

func (f *FakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		result[i] = deterministicVector(text, f.dim)
	}
	return result, nil
}

func (f *FakeEmbedder) Dimensions() int { return f.dim }
func (f *FakeEmbedder) ModelID() string { return "fake-embed" }

func deterministicVector(text string, dim int) []float32 {
	hash := sha256.Sum256([]byte(text))
	vec := make([]float32, dim)
	for i := range dim {
		offset := (i * 4) % len(hash)
		bits := binary.LittleEndian.Uint32([]byte{
			hash[offset%len(hash)],
			hash[(offset+1)%len(hash)],
			hash[(offset+2)%len(hash)],
			hash[(offset+3)%len(hash)],
		})
		vec[i] = float32(bits) / float32(math.MaxUint32)
	}
	return normalizeVec(vec)
}

func normalizeVec(v []float32) []float32 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	mag := float32(math.Sqrt(sum))
	if mag == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = f / mag
	}
	return out
}
