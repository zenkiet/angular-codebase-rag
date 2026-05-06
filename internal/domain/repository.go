package domain

import "context"

// EmbeddingService abstracts any embedding provider
type EmbeddingService interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	ModelInfo() ModelInfo
}

type ModelInfo struct {
	Name string
	Dim  int
}

// VectorStore abstracts any vector database
type VectorStore interface {
	Upsert(ctx context.Context, points []Point) error
	Search(ctx context.Context, query []float32, limit int, filter Filter) ([]Result, error)
	CollectionExists(ctx context.Context, name string) (bool, error)
	CreateCollection(ctx context.Context, name string, dim int) error
	DeleteCollection(ctx context.Context, name string) error
}

type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]interface{}
}

type Filter map[string]interface{}
type Result struct {
	ID      string
	Score   float32
	Payload map[string]interface{}
}
