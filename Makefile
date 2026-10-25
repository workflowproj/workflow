SHELL := /bin/bash
GOBIN ?= $(shell go env GOPATH)/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/geoffo-dev/workflow-management/internal/telemetry.Version=$(VERSION)
DATABASE_URL ?= postgres://wfm:wfm@localhost:5432/wfm?sslmode=disable

.PHONY: help tools generate generate-check build web web-dev embed-web test test-unit test-integration lint dev db-up db-down image

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

tools: ## Install code generators
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
	go install github.com/pressly/goose/v3/cmd/goose@v3.29.0

generate: ## Regenerate sqlc, Go API server and TypeScript API types from the specs
	$(GOBIN)/sqlc generate
	$(GOBIN)/oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	cd web && npm run generate

generate-check: generate ## Fail if generated code is out of date
	git diff --exit-code -- internal/db/dbq internal/api web/src/api/schema.d.ts

web: ## Build the React UI
	cd web && npm ci && npm run build

embed-web: ## Copy the built UI into the Go embed directory
	rm -rf internal/webui/dist && mkdir -p internal/webui/dist && cp -r web/dist/. internal/webui/dist/ && touch internal/webui/dist/.gitkeep

build: ## Build the wfm binary (with the UI if built)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/wfm ./cmd/wfm

test-unit: ## Unit tests (no database)
	go test ./internal/rank/... ./internal/workflow/... ./internal/search/... ./internal/netguard/... ./internal/scm/... ./internal/identity/

test-integration: ## Integration tests (TEST_DATABASE_URL, or Testcontainers when Docker is available)
	go test -count=1 ./internal/integration/...

test: ## All Go tests
	go test -count=1 ./...

lint: ## go vet + web typecheck
	go vet ./...
	cd web && npm run typecheck

db-up: ## Start a local PostgreSQL 17
	docker run -d --name wfm-pg -p 5432:5432 -e POSTGRES_USER=wfm -e POSTGRES_PASSWORD=wfm -e POSTGRES_DB=wfm postgres:17-alpine

db-down:
	docker rm -f wfm-pg

dev: ## Run the API in "all" mode with dev login (use `cd web && npm run dev` for the UI)
	WFM_ENV=development WFM_DEV_LOGIN=true WFM_LOG_FORMAT=text WFM_DATABASE_URL=$(DATABASE_URL) \
		go run ./cmd/wfm serve --mode=all --migrate

image: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t ghcr.io/geoffo-dev/workflow-management:$(VERSION) .
