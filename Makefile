.PHONY: dev stop migrate-up migrate-down test build seed proxy lint

dev:
	docker compose up -d

stop:
	docker compose down

migrate-up:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is not set. Run: export $$(cat .env | xargs)"; exit 1)
	goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is not set. Run: export $$(cat .env | xargs)"; exit 1)
	goose -dir db/migrations postgres "$(DATABASE_URL)" down

test:
	go test ./...

build:
	go build -o bin/proxy ./cmd/proxy/
	go build -o bin/api ./cmd/api/

seed:
	go run ./cmd/seed/

proxy:
	go run ./cmd/proxy/

lint:
	go vet ./...
