.PHONY: setup lint test build dev images generate

SQLC_VERSION := v1.31.1

setup:            ## install frontend deps
	cd frontend && npm ci --no-audit --no-fund

lint:             ## all linters + typecheck
	cd backend && golangci-lint run ./...
	cd frontend && npm run lint && npm run typecheck

test:             ## all unit/integration tests (no Docker, no network)
	cd backend && go test -race ./...
	cd frontend && npm test

build:
	cd backend && go build -o bin/arc ./cmd/arc
	cd frontend && npm run build

dev:              ## api on :8080 + vite on :5173 (proxying /api, /auth, /ws)
	@trap 'kill 0' EXIT; \
	(cd backend && APP_ENV=dev go run ./cmd/arc) & \
	(cd frontend && npm run dev) & \
	wait

generate:         ## regenerate sqlc code (idempotent; CI checks the result is committed)
	cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

images:           ## build production images locally
	docker build -t ghcr.io/rhamdeew/dbprc-api:local --build-arg VERSION=local backend
	docker build -t ghcr.io/rhamdeew/dbprc-web:local -f deploy/env/web.Dockerfile .
