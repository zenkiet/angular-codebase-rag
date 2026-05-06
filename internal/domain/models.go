package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

type CodeChunk struct {
	HashID       string
	ProjectName  string
	FilePath     string
	RelativePath string
	Language     string
	ChunkKind    string
	SymbolName   string
	ParentSymbol string
	StartLine    int
	EndLine      int
	Content      string
	EmbedText    string
}

func LanguageFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ts":
		return "typescript"
	case ".js":
		return "javascript"
	case ".html":
		return "html"
	case ".scss", ".css":
		return "css"
	case ".go":
		return "go"
	case ".md":
		return "markdown"
	default:
		return "plaintext"
	}
}

func (c *CodeChunk) ToPayload() map[string]interface{} {
	language := c.Language
	if language == "" {
		language = LanguageFromPath(c.FilePath)
	}

	return map[string]interface{}{
		"project_name":  c.ProjectName,
		"file_path":     c.FilePath,
		"relative_path": c.RelativePath,
		"file_name":     filepath.Base(c.FilePath),
		"language":      language,
		"chunk_kind":    c.ChunkKind,
		"symbol_name":   c.SymbolName,
		"parent_symbol": c.ParentSymbol,
		"start_line":    c.StartLine,
		"end_line":      c.EndLine,
		"content":       c.Content,
	}
}

func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func GenerateCodeChunkID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part + "::"))
	}
	hash := h.Sum(nil)

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		hash[0:4],
		hash[4:6],
		hash[6:8],
		hash[8:10],
		hash[10:16],
	)
}
