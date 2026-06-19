package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaEmbed_Unit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		embeddings := make([][]float32, len(req.Input))
		for i := range req.Input {
			embeddings[i] = []float32{float32(i) * 0.1, 0.5, 0.3, 0.2}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"model":      req.Model,
			"embeddings": embeddings,
		})
	}))
	defer server.Close()

	e, err := NewOllama(server.URL, "test-model", 4, 10*time.Second)
	if err != nil {
		t.Fatalf("NewOllama: %v", err)
	}

	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != 4 {
		t.Errorf("expected dimension 4, got %d", len(vecs[0]))
	}
}

func TestOllamaEmbed_BatchPreservesOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		embeddings := make([][]float32, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, 4)
			vec[0] = float32(i + 1)
			embeddings[i] = vec
		}

		json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	}))
	defer server.Close()

	e, _ := NewOllama(server.URL, "test-model", 4, 10*time.Second)
	inputs := []string{"first", "second", "third"}
	vecs, err := e.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	for i, v := range vecs {
		expected := float32(i + 1)
		if v[0] != expected {
			t.Errorf("vec[%d][0] = %f, expected %f — order not preserved", i, v[0], expected)
		}
	}
}

func TestOllamaEmbed_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	e, _ := NewOllama(server.URL, "test-model", 4, 10*time.Second)
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

func TestOllamaEmbed_DimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.1, 0.2, 0.3}},
		})
	}))
	defer server.Close()

	e, _ := NewOllama(server.URL, "test-model", 4, 10*time.Second)
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected dimension mismatch error, got nil")
	}
}

func TestOllamaEmbed_EmptyInput(t *testing.T) {
	e, _ := NewOllama("http://unused", "test-model", 4, 10*time.Second)
	vecs, err := e.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Embed empty: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("expected 0 vectors for empty input, got %d", len(vecs))
	}
}

func TestOllama_Dimensions(t *testing.T) {
	e, _ := NewOllama("http://unused", "test-model", 768, 10*time.Second)
	if e.Dimensions() != 768 {
		t.Errorf("expected 768, got %d", e.Dimensions())
	}
}

func TestOllama_ModelID(t *testing.T) {
	e, _ := NewOllama("http://unused", "nomic-embed-text", 768, 10*time.Second)
	if e.ModelID() != "nomic-embed-text" {
		t.Errorf("expected nomic-embed-text, got %s", e.ModelID())
	}
}

func TestFakeEmbedder_Deterministic(t *testing.T) {
	f := NewFake(4)
	v1, _ := f.Embed(context.Background(), []string{"hello"})
	v2, _ := f.Embed(context.Background(), []string{"hello"})

	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Errorf("fake embedder not deterministic at index %d: %f != %f", i, v1[0][i], v2[0][i])
		}
	}
}

func TestFakeEmbedder_DifferentInputs(t *testing.T) {
	f := NewFake(4)
	vecs, _ := f.Embed(context.Background(), []string{"hello", "world"})
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}

	same := true
	for i := range vecs[0] {
		if vecs[0][i] != vecs[1][i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different inputs should produce different vectors")
	}
}
