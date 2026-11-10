.PHONY: toolchain setup lint test build dev images generate

# Pinned tool versions (single source: toolchain.env). Tools installed by `make toolchain`
# live in ~/.local; put them first on PATH for every target.
include toolchain.env
export PATH := $(HOME)/.local/go/bin:$(HOME)/.local/bin:$(PATH)

toolchain:        ## install pinned Go, golangci-lint, sqlc + Go modules + npm deps (fresh container: run first)
	scripts/toolchain.sh

setup:            ## install frontend deps only
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
	@sqlc version 2>/dev/null | grep -qx "v$(SQLC_VERSION)" || scripts/toolchain.sh sqlc
	cd backend && sqlc generate

images:           ## build production images locally
	docker build -t ghcr.io/rhamdeew/dbprc-api:local --build-arg VERSION=local backend
	docker build -t ghcr.io/rhamdeew/dbprc-web:local -f deploy/env/web.Dockerfile .
