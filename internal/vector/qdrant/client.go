package qdrant

import (
	"angular-codebase-rag/internal/domain"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	collection string
	apiKey     string
	client     *http.Client
}

type qdrantPoint struct {
	ID      string                 `json:"id"`
	Vector  []float32              `json:"vector"`
	Payload map[string]interface{} `json:"payload"`
}

type queryResponse struct {
	Result struct {
		Points []queryPoint `json:"points"`
	} `json:"result"`
}

type queryPoint struct {
	ID      interface{}            `json:"id"`
	Score   float32                `json:"score"`
	Payload map[string]interface{} `json:"payload"`
}

func NewClient(baseUrl, collection, apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseUrl, "/"),
		collection: collection,
		apiKey:     apiKey,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Upsert(ctx context.Context, points []domain.Point) error {
	if len(points) == 0 {
		return nil
	}

	qPoints := make([]qdrantPoint, len(points))
	for i, p := range points {
		qPoints[i] = qdrantPoint{ID: p.ID, Vector: p.Vector, Payload: p.Payload}
	}
	return c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/collections/%s/points?wait=true", c.collection), map[string]interface{}{"points": qPoints}, nil)
}

func (c *Client) Search(ctx context.Context, query []float32, limit int, filter domain.Filter) ([]domain.Result, error) {
	if limit <= 0 {
		limit = 10
	}

	reqBody := map[string]interface{}{
		"query":        query,
		"limit":        limit,
		"with_payload": true,
		"with_vector":  false,
	}
	if qFilter := buildQdrantFilter(filter); qFilter != nil {
		reqBody["filter"] = qFilter
	}

	var response queryResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/collections/%s/points/query", c.collection), reqBody, &response); err != nil {
		return nil, err
	}

	results := make([]domain.Result, 0, len(response.Result.Points))
	for _, point := range response.Result.Points {
		results = append(results, domain.Result{
			ID:      fmt.Sprint(point.ID),
			Score:   point.Score,
			Payload: point.Payload,
		})
	}
	return results, nil
}

func (c *Client) CollectionExists(ctx context.Context, name string) (bool, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/collections/%s", name), nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err := checkStatus("check collection", resp); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) CreateCollection(ctx context.Context, name string, vectorSize int) error {
	payload := map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	return c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/collections/%s", name), payload, nil)
}

func (c *Client) DeleteCollection(ctx context.Context, name string) error {
	resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/collections/%s", name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return checkStatus("delete collection", resp)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out interface{}) error {
	resp, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if err := checkStatus(method+" "+path, resp); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode qdrant response: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant request: %w", err)
	}
	return resp, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body interface{}) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode qdrant request: %w", err)
		}
		reader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("create qdrant request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("api-key", c.apiKey)
	}
	return req, nil
}

func checkStatus(operation string, resp *http.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("qdrant rejected %s (status %d): %s", operation, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
}

func buildQdrantFilter(filter domain.Filter) map[string]interface{} {
	if len(filter) == 0 {
		return nil
	}
	must := make([]map[string]interface{}, 0, len(filter))
	for key, value := range filter {
		if key == "" || value == nil {
			continue
		}
		if str, ok := value.(string); ok && str == "" {
			continue
		}
		must = append(must, map[string]interface{}{
			"key": key,
			"match": map[string]interface{}{
				"value": value,
			},
		})
	}
	if len(must) == 0 {
		return nil
	}
	return map[string]interface{}{"must": must}
}

var _ domain.VectorStore = (*Client)(nil)
