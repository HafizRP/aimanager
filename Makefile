# ==============================================================================
# AI Manager (9router-gateway) Makefile
# Developer and AI agent automation commands
# ==============================================================================

BINARY_NAME := 9router-gateway
BIN_DIR := bin
CMD_PKG := ./cmd/gateway
GOPATH_BIN := $(shell go env GOPATH 2>/dev/null)/bin
export PATH := $(GOPATH_BIN):$(PATH)

.PHONY: all build test test-race vet fmt run dev clean verify docker-build help

all: fmt vet test build

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-w -s" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PKG)

test:
	go test -v ./...

test-race:
	go test -race -v ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

run:
	go run $(CMD_PKG)

dev:
	@if command -v air > /dev/null 2>&1; then \
		air; \
	elif [ -x "$(GOPATH_BIN)/air" ]; then \
		"$(GOPATH_BIN)/air"; \
	else \
		echo "air not found, running 'go run $(CMD_PKG)' (install air for hot reload: go install github.com/air-verse/air@latest)"; \
		go run $(CMD_PKG); \
	fi

clean:
	rm -rf $(BIN_DIR) tmp $(BINARY_NAME)
	rm -f *.db-shm *.db-wal

verify:
	chmod +x ./scripts/verify.sh
	./scripts/verify.sh

docker-build:
	docker build -t $(BINARY_NAME):latest .

help:
	@echo "Available commands:"
	@echo "  make build       - Build production binary in bin/"
	@echo "  make test        - Run all unit tests"
	@echo "  make test-race   - Run tests with race detection enabled"
	@echo "  make vet         - Run Go static analysis"
	@echo "  make fmt         - Format all Go source files"
	@echo "  make run         - Run server directly from source"
	@echo "  make dev         - Run server with live reload (air) or fallback to go run"
	@echo "  make clean       - Clean built binaries and temp SQLite files"
	@echo "  make verify      - Run endpoint verification script against :20129"
	@echo "  make docker-build- Build Docker container image"
