-include .env
export

.PHONY: setup up down logs migrate-up migrate-down build run worker test tidy install-tools

# The worker binds APP_PORT for its health endpoint and METRICS_PORT for
# Prometheus — the same two variables the API uses. Offset them so that
# `make run` and `make worker` can run side by side locally; in production each
# Railway service sets its own values.
WORKER_APP_PORT ?= 8081
WORKER_METRICS_PORT ?= 9092

setup:
	@test -f .env && echo "✅ .env already exists" || (cp .env.example .env && echo "✅ .env created from .env.example")

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

migrate-up:
	@test -n "$$DATABASE_URL" || (echo "❌ DATABASE_URL not set — run: source .env" && exit 1)
	migrate -path migrations -database "$$DATABASE_URL" up

migrate-down:
	@test -n "$$DATABASE_URL" || (echo "❌ DATABASE_URL not set — run: source .env" && exit 1)
	migrate -path migrations -database "$$DATABASE_URL" down 1

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker

run:
	go run ./cmd/api

worker:
	APP_PORT=$(WORKER_APP_PORT) METRICS_PORT=$(WORKER_METRICS_PORT) go run ./cmd/worker

test:
	go test ./...

tidy:
	go mod tidy

install-tools:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
