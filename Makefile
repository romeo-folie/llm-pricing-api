.PHONY: setup up down logs migrate-up migrate-down build run worker test tidy install-tools

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

run:
	go run ./cmd/api

worker:
	go run ./cmd/worker

test:
	go test ./...

tidy:
	go mod tidy

install-tools:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
