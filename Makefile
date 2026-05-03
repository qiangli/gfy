MODULE   := github.com/qiangli/gfy
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"
BINARY   := gfy

.PHONY: help build test tidy lint install clean diff

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the gfy binary
	go build $(LDFLAGS) -o $(BINARY) ./cmd/gfy

tidy: ## Run mod tidy, fmt, and vet
	go mod tidy
	go fmt ./cmd/... ./internal/...
	go vet ./cmd/... ./internal/...

test: ## Run all tests
	go test ./cmd/... ./internal/...

lint: ## Run golangci-lint
	golangci-lint run

install: ## Install to $GOPATH/bin
	go install $(LDFLAGS) ./cmd/gfy

diff: build ## Compare local working tree against remote tracking branch
	./$(BINARY) diff .

clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf .gfy-out
