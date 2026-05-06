package scanner

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

var defaultIgnores = []string{
	".git/**",
	"node_modules/**",
	"dist/**",
	"out/**",
	"coverage/**",
	".angular/**",
	".nx/**",
	"tmp/**",
	"*.lock",
	"package-lock.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	".DS_Store",
}

type FileScanner struct {
	root    string
	ignorer *ignore.GitIgnore
}

func New(root string, exclude []string) *FileScanner {
	lines := append(cleanPatterns(exclude), defaultIgnores...)
	gitIgnorePath := filepath.Join(root, ".gitignore")

	ignorer, err := ignore.CompileIgnoreFileAndLines(gitIgnorePath, lines...)
	if err != nil {
		ignorer = ignore.CompileIgnoreLines(lines...)
	}

	return &FileScanner{root: root, ignorer: ignorer}
}

func (s *FileScanner) Discover(ctx context.Context, fileChan chan<- string) error {
	return filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "." {
			return nil
		}

		if s.isIgnored(relPath, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if isSourceFile(d.Name()) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case fileChan <- path:
			}
		}
		return nil
	})
}

func (s *FileScanner) isIgnored(relPath string, isDir bool) bool {
	if s.ignorer.MatchesPath(relPath) {
		return true
	}
	return isDir && s.ignorer.MatchesPath(relPath+"/")
}

func isSourceFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".js", ".ts", ".html", ".css", ".scss", ".less", ".json", ".md", ".yml", ".yaml":
		return true
	default:
		return false
	}
}

func cleanPatterns(patterns []string) []string {
	cleaned := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		cleaned = append(cleaned, filepath.ToSlash(pattern))
	}
	return cleaned
}
