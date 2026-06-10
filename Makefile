SHELL := /bin/bash
-include .env
export

DATABASE_URL ?= postgres://charging:charging@localhost:5433/charging?sslmode=disable
GOOSE_DRIVER ?= postgres
MIGRATIONS_DIR := db/migrations

.PHONY: help db-up db-down db-wait migrate migrate-down sqlc tidy build test run-ingest run-ingest-once run-api fmt vet

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-18s %s\n", $$1, $$2}'

db-up: ## Start PostGIS
	docker compose up -d db

db-down: ## Stop PostGIS (keeps volume)
	docker compose down

db-wait: ## Block until PostGIS is healthy
	@until docker compose exec -T db pg_isready -U charging -d charging >/dev/null 2>&1; do echo "waiting for db..."; sleep 1; done; echo "db ready"

migrate: ## Apply DB migrations
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir $(MIGRATIONS_DIR) $(GOOSE_DRIVER) "$(DATABASE_URL)" up

migrate-down: ## Roll back one migration
	go run github.com/pressly/goose/v3/cmd/goose@latest -dir $(MIGRATIONS_DIR) $(GOOSE_DRIVER) "$(DATABASE_URL)" down

sqlc: ## Regenerate store code from SQL
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate

tidy: ## go mod tidy
	go mod tidy

fmt: ## gofmt
	gofmt -w .

vet: ## go vet
	go vet ./...

build: ## Build all binaries (api, ingest, migrate, chargingctl)
	go build -o bin/ingest ./cmd/ingest
	go build -o bin/api ./cmd/api
	go build -o bin/migrate ./cmd/migrate
	go build -o bin/chargingctl ./cmd/chargingctl

test: ## Run all tests
	go test ./...

run-ingest-once: ## Run one ingestion pass and exit
	go run ./cmd/ingest -once

run-ingest: ## Run ingestion scheduler (cron)
	go run ./cmd/ingest

run-api: ## Run the API server
	go run ./cmd/api

demo-seed: ## Load optional demo data so the API returns results without a key
	docker compose exec -T db psql -U charging -d charging -v ON_ERROR_STOP=1 -f - < db/demo_seed.sql
	@echo "demo data loaded"

# ---- Production (single VM) ----
PROD := docker compose -p charging_prod -f docker-compose.prod.yml

prod-up: ## Build images and start the full prod stack (db + migrate + api + ingest + web)
	$(PROD) up -d --build

prod-demo: ## Like prod-up, plus load demo data so the map shows chargers
	$(PROD) --profile demo up -d --build

prod-down: ## Stop the prod stack (keeps the data volume)
	$(PROD) down

prod-logs: ## Tail prod logs
	$(PROD) logs -f --tail=100

prod-ps: ## Show prod service status
	$(PROD) ps

prod-backup: ## Dump the prod database to backups/ (timestamp passed in as TS=...)
	@mkdir -p backups
	$(PROD) exec -T db pg_dump -U charging -d charging | gzip > "backups/charging-$(or $(TS),manual).sql.gz"
	@echo "backup written to backups/charging-$(or $(TS),manual).sql.gz"
