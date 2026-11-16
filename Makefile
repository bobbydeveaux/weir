DATABASE_URL ?= postgres://weir:weir@localhost:54329/weir?sslmode=disable

.PHONY: help up down migrate build test test-db fmt vet check

help: ## Show this help
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

up: ## Start Postgres and wait until it is ready
	docker compose up -d --wait

down: ## Stop Postgres (data is kept)
	docker compose down

migrate: ## Bring the local database up to the latest schema
	WEIR_DATABASE_URL="$(DATABASE_URL)" go run ./cmd/weir migrate

build: ## Build the weir binary into ./bin
	go build -o bin/weir ./cmd/weir

test: ## Run the tests that need no database
	go test ./...

test-db: up ## Run every test, including those that need Postgres
	WEIR_TEST_DATABASE_URL="$(DATABASE_URL)" go test -count=1 ./...

fmt: ## Format the code
	gofmt -w .

vet: ## Run go vet
	go vet ./...

check: ## What CI runs: formatting, vet and every test
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed on:"; gofmt -l .; exit 1; }
	go vet ./...
	$(MAKE) test-db
