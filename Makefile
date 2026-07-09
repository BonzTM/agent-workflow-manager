# Makefile - single canonical verification entrypoint.
#
# Humans and CI run the SAME targets; `make verify` is the ordered safety gate.
# golangci-lint and govulncheck are pinned as `go tool` dependencies (see
# go.mod tool directives), never global installs.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

.DEFAULT_GOAL := help

BINARIES := awm awm-mcp awm-web

.PHONY: help tidy tidy-check fmt fmt-check lint vet test race cover vuln build verify

help: ## Show this help.
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

tidy: ## Sync go.mod/go.sum in place and verify the module graph.
	go mod tidy
	go mod verify

tidy-check: ## Fail if go mod tidy would change go.mod/go.sum (CI-safe, no writes).
	go mod tidy -diff
	go mod verify

fmt: ## Format all Go source in place (gofumpt + gci, per .golangci.yml).
	go tool golangci-lint fmt

fmt-check: ## Fail if any file is not formatted (CI-safe, no writes).
	@out="$$(go tool golangci-lint fmt --diff || true)"; \
	if [ -n "$$out" ]; then \
		echo "not formatted (gofumpt/gci):"; echo "$$out"; \
		echo "run: make fmt"; \
		exit 1; \
	fi

lint: ## Run golangci-lint.
	go tool golangci-lint run

vet: ## Run go vet.
	go vet ./...

test: ## Run all tests.
	go test ./...

race: ## Run all tests under the race detector.
	go test -race ./...

cover: ## Write cover.out and print total coverage (atomic mode is race-safe).
	go test -covermode=atomic -coverprofile=cover.out ./...
	go tool cover -func=cover.out | grep '^total:'

vuln: ## Run govulncheck against the module.
	go tool govulncheck ./...

build: ## Compile all packages and the dist/ binaries.
	go build ./...
	@mkdir -p dist
	@for bin in $(BINARIES); do go build -o dist/$$bin ./cmd/$$bin; done

verify: tidy-check fmt-check lint vet test race vuln build ## Full ordered safety gate (run before every push and in CI).
	@echo "verify: OK"
