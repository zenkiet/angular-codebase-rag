package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"angular-codebase-rag/internal/config"
	"angular-codebase-rag/internal/domain"
)

const (
	defaultSearchLimit = 8
	defaultMaxLimit    = 20
	defaultSnippetLen  = 700
)

type SearchService struct {
	cfg         config.Config
	embedder    domain.EmbeddingService
	vectorStore domain.VectorStore
}

func NewSearchService(cfg config.Config, embedder domain.EmbeddingService, vectorStore domain.VectorStore) *SearchService {
	return &SearchService{cfg: cfg, embedder: embedder, vectorStore: vectorStore}
}

func (s *SearchService) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return SearchResponse{}, fmt.Errorf("query text is required")
	}

	limit := s.clampLimit(req.Limit)
	filter := domain.Filter{"project_name": s.cfg.Project.Name}
	if strings.TrimSpace(req.Kind) != "" {
		filter["chunk_kind"] = strings.TrimSpace(req.Kind)
	}
	if strings.TrimSpace(req.Path) != "" {
		filter["relative_path"] = filepath.ToSlash(strings.TrimSpace(req.Path))
	}

	vector, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("embed query: %w", err)
	}

	results, err := s.vectorStore.Search(ctx, vector, limit, filter)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("search qdrant: %w", err)
	}

	formatted := formatResults(results)
	return SearchResponse{
		Summary: summary(s.cfg.Project.Name, len(formatted)),
		Project: s.cfg.Project.Name,
		Query:   query,
		Results: formatted,
	}, nil
}

func (s *SearchService) clampLimit(limit int) int {
	defaultLimit := s.cfg.MCP.DefaultLimit
	if defaultLimit <= 0 {
		defaultLimit = defaultSearchLimit
	}

	maxLimit := s.cfg.MCP.MaxLimit
	if maxLimit <= 0 {
		maxLimit = defaultMaxLimit
	}

	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

func summary(project string, count int) string {
	if count == 0 {
		return fmt.Sprintf("No relevant chunks found for project %s.", project)
	}
	return fmt.Sprintf("Found %d relevant chunks for project %s.", count, project)
}

func formatResults(results []domain.Result) []Result {
	formatted := make([]Result, 0, len(results))
	for idx, result := range results {
		payload := result.Payload
		formatted = append(formatted, Result{
			Rank:         idx + 1,
			ID:           result.ID,
			Score:        result.Score,
			RelativePath: payloadString(payload, "relative_path"),
			StartLine:    payloadInt(payload, "start_line"),
			EndLine:      payloadInt(payload, "end_line"),
			Kind:         payloadString(payload, "chunk_kind"),
			Symbol:       payloadString(payload, "symbol_name"),
			ParentSymbol: payloadString(payload, "parent_symbol"),
			Snippet:      snippet(payloadString(payload, "content"), defaultSnippetLen),
		})
	}
	return formatted
}

func payloadString(payload map[string]interface{}, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func payloadInt(payload map[string]interface{}, key string) int {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

func snippet(content string, max int) string {
	content = strings.TrimSpace(content)
	content = strings.Join(strings.Fields(content), " ")
	if len(content) <= max {
		return content
	}
	if max < 4 {
		return content[:max]
	}
	return content[:max-3] + "..."
}
