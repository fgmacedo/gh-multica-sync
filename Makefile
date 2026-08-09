BINARY := gh-multica-sync
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint fmt install clean

build: ## Build the binary into ./bin
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

test: ## Run the tests
	go test ./...

lint: ## gofmt and go vet
	@test -z "$$(gofmt -l . )" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...

fmt: ## Format the code
	gofmt -w .

install: build ## Install as a local gh extension
	gh extension install . --force 2>/dev/null || gh extension install .

clean:
	rm -rf bin
