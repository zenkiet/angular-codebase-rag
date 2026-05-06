.PHONY: build index query docker-build docker-up docker-down test lint tidy

build:
	go build -o bin/rag ./cmd/indexer

index:
	go run ./cmd/indexer index --config config.yaml

query:
	go run ./cmd/indexer query --config config.yaml "$(q)"

docker-build:
	docker build -f docker/Dockerfile -t zen-indexer:latest .

docker-up:
	docker-compose -f docker/docker-compose.yml up -d

docker-down:
	docker-compose -f docker/docker-compose.yml down

lint:
	golangci-lint run

test:
	go test ./...

tidy:
	go mod tidy
