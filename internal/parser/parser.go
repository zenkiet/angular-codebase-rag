package parser

import (
	"angular-codebase-rag/internal/domain"
	"context"
)

type ASTParser interface {
	ParseFile(ctx context.Context, filePath string) ([]domain.CodeChunk, error)
}
