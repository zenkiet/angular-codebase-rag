# angular-codebase-rag

Minimal Go RAG indexer/query CLI for Angular codebases.

## Commands

```sh
go run ./cmd/indexer index --config config.yaml --clean
go run ./cmd/indexer query "user deletion flow" --config config.yaml --limit 8
go run ./cmd/indexer query "user deletion flow" --config config.yaml --json
```

The V1 scope is retrieval first. MCP should be added as a thin layer after CLI
query quality is acceptable.
