.PHONY: toolchain setup lint test build dev images generate test-coverage-gate cover-backend cover-frontend

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

# Coverage gates. Single source of the thresholds; CI enforces them with
# `make cover-backend` and `make cover-frontend` (also part of `make test`).
# Go total is statement coverage from `go test -coverprofile`, after dropping
# generated sqlc and the cmd wiring packages. Frontend is Vitest v8 line coverage.
GO_COVERAGE_MIN := 80
FE_COVERAGE_MIN := 70
GO_COVER_EXCLUDE_RE := /(internal/db/sqlc|cmd)/

test: test-coverage-gate cover-backend cover-frontend ## unit/integration tests and coverage gates

test-coverage-gate:
	scripts/coverage-gate_test.sh

cover-backend: test-coverage-gate ## Go tests; fail if statement coverage < GO_COVERAGE_MIN
	cd backend && go test -race -coverprofile=coverage.out ./...
	scripts/coverage-gate.sh go backend/coverage.out '$(GO_COVERAGE_MIN)' '$(GO_COVER_EXCLUDE_RE)'

cover-frontend: ## Vitest v8; fail if line coverage < FE_COVERAGE_MIN
	cd frontend && npm run test:coverage -- --coverage.reporter=text --coverage.reporter=json-summary --coverage.reportsDirectory=coverage
	scripts/coverage-gate.sh vitest frontend/coverage/coverage-summary.json '$(FE_COVERAGE_MIN)'

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
