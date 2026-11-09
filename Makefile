.PHONY: dev down build test test-backend test-frontend lint lint-backend lint-layers lint-frontend \
        migrate-up migrate-down seed gen-api e2e fmt

COMPOSE ?= docker compose

dev: ## Start everything (postgres, migrate, seed, api, worker, web)
	$(COMPOSE) up --build

down: ## Stop the stack (add -v manually to drop the database volume)
	$(COMPOSE) down

build: ## Build the Go binary and the frontend
	cd backend && CGO_ENABLED=0 go build -trimpath -o bin/hookrelay ./cmd/server
	cd frontend && npm run build

test: test-backend test-frontend ## Backend + frontend tests

test-backend:
	cd backend && go test -race -tags integration ./...

test-frontend:
	cd frontend && npm test

lint: lint-backend lint-layers lint-frontend ## All linters

lint-backend:
	cd backend && gofmt -l . | (! grep .) && go vet -tags integration ./... && golangci-lint run

lint-layers: ## Clean Architecture dependency-rule greps
	cd backend && sh scripts/check-layers.sh

lint-frontend:
	cd frontend && npm run lint && npm run typecheck

fmt:
	cd backend && gofmt -w .

migrate-up: ## Apply migrations to the compose database
	$(COMPOSE) run --rm migrate /hookrelay migrate up

migrate-down: ## Revert ONE migration step
	$(COMPOSE) run --rm migrate /hookrelay migrate down

seed: ## Create the demo project (idempotent)
	$(COMPOSE) run --rm seed

gen-api: ## Regenerate frontend API types from api/openapi.yaml
	cd frontend && npm run gen:api

e2e: ## Playwright against the running compose stack
	cd frontend && npx playwright test
