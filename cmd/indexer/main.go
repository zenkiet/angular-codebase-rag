package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"angular-codebase-rag/internal/config"
	"angular-codebase-rag/internal/domain"
	"angular-codebase-rag/internal/embedding"
	"angular-codebase-rag/internal/parser/treesitter"
	"angular-codebase-rag/internal/pipeline"
	"angular-codebase-rag/internal/vector/qdrant"

	"github.com/spf13/pflag"
)

type queryOutput struct {
	Results []queryResult `json:"results"`
}

type queryResult struct {
	Rank         int     `json:"rank"`
	ID           string  `json:"id"`
	Score        float32 `json:"score"`
	RelativePath string  `json:"relative_path"`
	StartLine    int     `json:"start_line"`
	EndLine      int     `json:"end_line"`
	Kind         string  `json:"kind"`
	Symbol       string  `json:"symbol"`
	ParentSymbol string  `json:"parent_symbol,omitempty"`
	Snippet      string  `json:"snippet"`
}

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "index":
		err = runIndex(os.Args[2:])
	case "query":
		err = runQuery(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runIndex(args []string) error {
	flags := pflag.NewFlagSet("index", pflag.ContinueOnError)
	configPath, name, repo := commonFlags(flags)
	clean := flags.Bool("clean", false, "Delete and recreate the vector collection before indexing")
	if err := flags.Parse(args); err != nil {
		if err == pflag.ErrHelp {
			return nil
		}
		return err
	}

	cfg, err := loadConfig(*configPath, *name, *repo)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	embedder, err := embedding.NewEmbeddingService(*cfg)
	if err != nil {
		return fmt.Errorf("initialize embedder: %w", err)
	}

	vecStore := qdrant.NewClient(cfg.VectorStore.Qdrant.BaseURL, cfg.VectorStore.Qdrant.Collection, cfg.VectorStore.Qdrant.APIKey)
	tsParser := treesitter.NewTypeScriptParser(*cfg)
	idx := pipeline.NewIndexer(*cfg, embedder, vecStore, tsParser)

	log.Printf("Index project %q at: %s", cfg.Project.Name, cfg.Project.Root)
	return idx.Run(ctx, *clean)
}

func runQuery(args []string) error {
	flags := pflag.NewFlagSet("query", pflag.ContinueOnError)
	configPath, name, repo := commonFlags(flags)
	limit := flags.Int("limit", 8, "Maximum number of chunks to return")
	kind := flags.String("type", "", "Optional chunk kind filter, e.g. component, service, route")
	path := flags.String("path", "", "Optional exact relative_path filter")
	jsonOutput := flags.Bool("json", false, "Print machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		if err == pflag.ErrHelp {
			return nil
		}
		return err
	}
	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return fmt.Errorf("query text is required")
	}

	cfg, err := loadConfig(*configPath, *name, *repo)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	embedder, err := embedding.NewEmbeddingService(*cfg)
	if err != nil {
		return fmt.Errorf("initialize embedder: %w", err)
	}
	vecStore := qdrant.NewClient(cfg.VectorStore.Qdrant.BaseURL, cfg.VectorStore.Qdrant.Collection, cfg.VectorStore.Qdrant.APIKey)

	vector, err := embedder.Embed(ctx, query)
	if err != nil {
		return fmt.Errorf("embed query: %w", err)
	}

	filter := domain.Filter{"project_name": cfg.Project.Name}
	if *kind != "" {
		filter["chunk_kind"] = *kind
	}
	if *path != "" {
		filter["relative_path"] = filepath.ToSlash(*path)
	}

	results, err := vecStore.Search(ctx, vector, *limit, filter)
	if err != nil {
		return fmt.Errorf("search qdrant: %w", err)
	}

	formatted := formatResults(results)
	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(queryOutput{Results: formatted})
	}
	printResults(formatted)
	return nil
}

func commonFlags(flags *pflag.FlagSet) (*string, *string, *string) {
	configPath := flags.String("config", "config.yaml", "Path to YAML config")
	name := flags.String("name", "", "Override project name")
	repo := flags.String("repo", "", "Override source code root")
	return configPath, name, repo
}

func loadConfig(configPath, name, repo string) (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if name != "" {
		cfg.Project.Name = name
	}
	if repo != "" {
		absRepo, err := filepath.Abs(repo)
		if err != nil {
			return nil, fmt.Errorf("resolve repo: %w", err)
		}
		cfg.Project.Root = absRepo
	}
	return cfg, nil
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			log.Println("Shutting down...")
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func formatResults(results []domain.Result) []queryResult {
	formatted := make([]queryResult, 0, len(results))
	for idx, result := range results {
		payload := result.Payload
		formatted = append(formatted, queryResult{
			Rank:         idx + 1,
			ID:           result.ID,
			Score:        result.Score,
			RelativePath: payloadString(payload, "relative_path"),
			StartLine:    payloadInt(payload, "start_line"),
			EndLine:      payloadInt(payload, "end_line"),
			Kind:         payloadString(payload, "chunk_kind"),
			Symbol:       payloadString(payload, "symbol_name"),
			ParentSymbol: payloadString(payload, "parent_symbol"),
			Snippet:      snippet(payloadString(payload, "content"), 700),
		})
	}
	return formatted
}

func printResults(results []queryResult) {
	if len(results) == 0 {
		fmt.Println("No results.")
		return
	}
	for _, result := range results {
		fmt.Printf("[%d] score=%.4f %s:%d-%d kind=%s symbol=%s\n", result.Rank, result.Score, result.RelativePath, result.StartLine, result.EndLine, result.Kind, result.Symbol)
		if result.ParentSymbol != "" {
			fmt.Printf("    parent=%s\n", result.ParentSymbol)
		}
		fmt.Printf("    %s\n", indentSnippet(result.Snippet))
	}
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

func indentSnippet(text string) string {
	return strings.ReplaceAll(text, "\n", "\n    ")
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Minimal Angular Codebase RAG

Usage:
  rag index --config config.yaml [--clean] [--name project] [--repo path]
  rag query "search text" --config config.yaml [--limit 8] [--type kind] [--path relative/file.ts] [--json]

`)
}
