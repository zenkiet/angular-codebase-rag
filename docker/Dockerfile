# Stage 1: Build the binary
FROM golang:1.26.2-bookworm AS builder

WORKDIR /app

# Install dependencies
RUN apt-get update && apt-get install -y gcc g++ make

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build with CGO_ENABLED=1
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/bin/indexer ./cmd/indexer/main.go

# Stage 2: Create the final image
FROM debian:bookworm-slim

WORKDIR /app

# SSL certificates
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/bin/indexer /app/indexer

ENTRYPOINT ["/app/indexer"]
