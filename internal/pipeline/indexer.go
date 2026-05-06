package pipeline

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"angular-codebase-rag/internal/config"
	"angular-codebase-rag/internal/domain"
	"angular-codebase-rag/internal/parser"
	"angular-codebase-rag/internal/scanner"
)

type Indexer struct {
	cfg         config.Config
	embedder    domain.EmbeddingService
	vectorStore domain.VectorStore
	parser      parser.ASTParser
	fileScanner *scanner.FileScanner
}

func NewIndexer(cfg config.Config, embedder domain.EmbeddingService,
	vectorStore domain.VectorStore, parser parser.ASTParser) *Indexer {
	return &Indexer{
		cfg: cfg, embedder: embedder, vectorStore: vectorStore, parser: parser,
		fileScanner: scanner.New(cfg.Project.Root, cfg.Project.Exclude),
	}
}

func (i *Indexer) Run(ctx context.Context, clean bool) error {
	log.Println("Starting RAG indexing pipeline...")
	start := time.Now()

	collectionName := i.cfg.VectorStore.Qdrant.Collection
	if clean {
		log.Printf("Resetting collection %q...", collectionName)
		if err := i.vectorStore.DeleteCollection(ctx, collectionName); err != nil {
			return fmt.Errorf("delete collection: %w", err)
		}
	}

	exists, err := i.vectorStore.CollectionExists(ctx, collectionName)
	if err != nil {
		log.Printf("Warning: check collection failed: %v", err)
	} else if !exists {
		dim := i.embedder.ModelInfo().Dim
		if dim == 0 {
			dim = 768
		}

		log.Printf("Collection %q not found. Creating with vector size %d...", collectionName, dim)
		if err := i.vectorStore.CreateCollection(ctx, collectionName, dim); err != nil {
			return fmt.Errorf("auto-create collection failed: %w", err)
		}
		log.Println("Collection created successfully.")
	}

	fileChan := make(chan string, i.cfg.Parser.ChannelBuf)
	chunkChan := make(chan domain.CodeChunk, i.cfg.Parser.ChannelBuf)
	errChan := make(chan error, 1)

	// Producer: File Scanner
	go func() {
		defer close(fileChan)
		if err := i.fileScanner.Discover(ctx, fileChan); err != nil {
			errChan <- fmt.Errorf("scanner error: %w", err)
		}
	}()

	// Workers: AST Parsers
	var wg sync.WaitGroup
	for w := 0; w < i.cfg.Parser.WorkerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i.parserWorker(ctx, fileChan, chunkChan)
		}()
	}

	// Consumer: Embed & Upsert (batched)
	go func() {
		defer close(errChan)
		errChan <- i.batchConsumer(ctx, chunkChan)
	}()

	// Wait for parsers to finish, then close chunkChan
	go func() {
		wg.Wait()
		close(chunkChan)
	}()

	// Final wait
	if err := <-errChan; err != nil {
		return err
	}

	log.Printf("Completed in %v\n", time.Since(start))
	return nil
}

func (i *Indexer) parserWorker(ctx context.Context, in <-chan string, out chan<- domain.CodeChunk) {
	for path := range in {
		log.Printf("Processing file: %s", path)
		chunks, err := i.parser.ParseFile(ctx, path)
		if err != nil {
			log.Printf("Skip %s: %v", path, err)
			continue
		}
		for _, c := range chunks {
			select {
			case out <- c:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (i *Indexer) batchConsumer(ctx context.Context, in <-chan domain.CodeChunk) error {
	batch := make([]domain.CodeChunk, 0, i.cfg.Embedding.BatchSize)

	for chunk := range in {
		batch = append(batch, chunk)
		if len(batch) >= i.cfg.Embedding.BatchSize {
			if err := i.processBatch(ctx, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}

	// Flush remaining
	if len(batch) > 0 {
		return i.processBatch(ctx, batch)
	}
	return nil
}

func (i *Indexer) processBatch(ctx context.Context, chunks []domain.CodeChunk) error {
	// 1. Generate embeddings
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.EmbedText
	}

	vectors, err := i.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("embedding batch: %w%s", err, describeEmbeddingBatch(chunks))
	}

	// 2. Build points
	points := make([]domain.Point, len(chunks))
	for i, c := range chunks {
		points[i] = domain.Point{
			ID:      c.HashID,
			Vector:  vectors[i],
			Payload: c.ToPayload(),
		}
	}

	// 3. Upsert to vector store
	return i.vectorStore.Upsert(ctx, points)
}

func describeEmbeddingBatch(chunks []domain.CodeChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	sorted := append([]domain.CodeChunk(nil), chunks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].EmbedText) > len(sorted[j].EmbedText)
	})
	limit := 3
	if len(sorted) < limit {
		limit = len(sorted)
	}
	details := ""
	for idx := 0; idx < limit; idx++ {
		chunk := sorted[idx]
		details += fmt.Sprintf("\n  candidate %d: embed_chars=%d content_chars=%d %s:%d-%d kind=%s symbol=%s",
			idx+1,
			len(chunk.EmbedText),
			len(chunk.Content),
			chunk.RelativePath,
			chunk.StartLine,
			chunk.EndLine,
			chunk.ChunkKind,
			chunk.SymbolName,
		)
	}
	return details
}
