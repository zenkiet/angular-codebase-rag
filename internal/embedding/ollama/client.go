package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"angular-codebase-rag/internal/config"
	"angular-codebase-rag/internal/domain"
)

type Client struct {
	baseURL string
	model   string
	client  *http.Client
}

type embedRequest struct {
	Model    string       `json:"model"`
	Input    []string     `json:"input"`
	Truncate bool         `json:"truncate"`
	Options  embedOptions `json:"options"`
}

type embedOptions struct {
	NumCtx int `json:"num_ctx"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func NewClient(cfg config.OllamaConfig, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("ollama returned no vectors")
	}
	return vectors[0], nil
}

func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := embedRequest{
		Model:    c.model,
		Input:    texts,
		Truncate: false,
		Options:  embedOptions{NumCtx: 8192},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("encode ollama embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var response embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(response.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d vectors for %d inputs", len(response.Embeddings), len(texts))
	}
	for idx, vector := range response.Embeddings {
		if len(vector) == 0 {
			return nil, fmt.Errorf("ollama returned empty vector at index %d", idx)
		}
	}
	return response.Embeddings, nil
}

func (c *Client) ModelInfo() domain.ModelInfo {
	return domain.ModelInfo{Name: c.model, Dim: 768}
}

var _ domain.EmbeddingService = (*Client)(nil)
