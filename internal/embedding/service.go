package embedding

import (
	"angular-codebase-rag/internal/config"
	"angular-codebase-rag/internal/domain"
	"angular-codebase-rag/internal/embedding/ollama"
)

func NewEmbeddingService(cfg config.Config) (domain.EmbeddingService, error) {
	return ollama.NewClient(cfg.Embedding.Ollama, cfg.Embedding.Timeout), nil
}
