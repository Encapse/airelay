.PHONY: dev stop migrate-up migrate-down test build seed proxy lint

dev:
	docker compose up -d

stop:
	docker compose down

migrate-up:
	goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir db/migrations postgres "$(DATABASE_URL)" down

test:
	go test ./...

build:
	go build -o proxy ./cmd/proxy/
	go build -o api ./cmd/api/

seed:
	go run ./cmd/seed/

proxy:
	go run ./cmd/proxy/

lint:
	go vet ./...
