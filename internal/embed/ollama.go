package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OllamaEmbedder struct {
	host   string
	model  string
	dim    int
	client *http.Client
}

func NewOllama(host, model string, dim int, timeout time.Duration) (*OllamaEmbedder, error) {
	return &OllamaEmbedder{
		host:  host,
		model: model,
		dim:   dim,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

func (o *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(embedRequest{
		Model: o.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.host+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed request failed (status %d): %s", resp.StatusCode, respBody)
	}

	var embedResp embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(embedResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embedResp.Embeddings))
	}

	for i, vec := range embedResp.Embeddings {
		if len(vec) != o.dim {
			return nil, fmt.Errorf("embedding %d has dimension %d, expected %d", i, len(vec), o.dim)
		}
	}

	return embedResp.Embeddings, nil
}

func (o *OllamaEmbedder) Dimensions() int { return o.dim }
func (o *OllamaEmbedder) ModelID() string { return o.model }
