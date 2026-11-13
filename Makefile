.PHONY: toolchain setup lint test build dev e2e images generate test-coverage-gate cover-backend cover-frontend \
	ensure-go ensure-golangci-lint ensure-sqlc ensure-node-modules ensure-e2e-modules

# Pinned tool versions (single source: toolchain.env). Tools installed by `make toolchain`
# live in ~/.local; put them first on PATH for every target.
include toolchain.env
export PATH := $(HOME)/.local/go/bin:$(HOME)/.local/bin:$(PATH)

toolchain:        ## install pinned Go, golangci-lint, sqlc + Go modules + npm deps (fresh container: run first)
	scripts/toolchain.sh

setup:            ## install frontend deps only
	cd frontend && npm ci --no-audit --no-fund

# Safeguard: every target installs the pinned tools it needs before running, so nobody
# (human or agent) builds with unpinned versions. When a tool is already at the pinned
# version this is a ~0.1s check; a fresh container installs it once (~15s per tool).
ensure-go:
	@scripts/toolchain.sh go >/dev/null
ensure-golangci-lint:
	@scripts/toolchain.sh golangci-lint >/dev/null
ensure-sqlc:
	@scripts/toolchain.sh sqlc >/dev/null
ensure-node-modules:
	@[ -d frontend/node_modules ] || (cd frontend && npm ci --no-audit --no-fund --loglevel=error --update-notifier=false)

ensure-e2e-modules:
	@[ -d e2e/node_modules ] || (cd e2e && npm ci --no-audit --no-fund --loglevel=error --update-notifier=false)

lint: ensure-go ensure-golangci-lint ensure-node-modules ensure-e2e-modules ## all linters + typecheck
	cd backend && golangci-lint run ./...
	cd frontend && npm run lint && npm run typecheck
	cd e2e && npm run typecheck

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

cover-backend: ensure-go test-coverage-gate ## Go tests; fail if statement coverage < GO_COVERAGE_MIN
	cd backend && go test -race -coverprofile=coverage.out ./...
	scripts/coverage-gate.sh go backend/coverage.out '$(GO_COVERAGE_MIN)' '$(GO_COVER_EXCLUDE_RE)'

cover-frontend: ensure-node-modules ## Vitest v8; fail if line coverage < FE_COVERAGE_MIN
	cd frontend && npm run test:coverage -- --coverage.reporter=text --coverage.reporter=json-summary --coverage.reportsDirectory=coverage
	scripts/coverage-gate.sh vitest frontend/coverage/coverage-summary.json '$(FE_COVERAGE_MIN)'

build: ensure-go ensure-node-modules
	cd backend && go build -o bin/arc ./cmd/arc
	cd frontend && npm run build

e2e: ensure-go ensure-node-modules ## Playwright: build the frontend, mockoidc + api, no Docker
	bash scripts/e2e.sh

dev: ensure-go ensure-node-modules ## mockoidc :9999 + api :8080 + vite :5173 (vite proxies /api, /auth, /ws)
	@trap 'kill 0' EXIT; \
	set -a; . ./.env.dev.example; set +a; \
	(cd backend && go run ./cmd/mockoidc) & \
	(cd backend && APP_ENV=dev go run ./cmd/arc) & \
	(cd frontend && npm run dev) & \
	wait

generate: ensure-sqlc ## regenerate sqlc code (idempotent; CI checks the result is committed)
	cd backend && sqlc generate

images:           ## build production images locally
	docker build -t ghcr.io/rhamdeew/dbprc-api:local --build-arg VERSION=local backend
	docker build -t ghcr.io/rhamdeew/dbprc-web:local -f deploy/env/web.Dockerfile .

.PHONY: lint-backup test-backup test-backup-deploy
lint-backup:      ## backup and deployment shell checks
	shellcheck deploy/backup/*.sh deploy/env/deploy.sh

test-backup-deploy:
	deploy/backup/deploy_test.sh

test-backup: test-backup-deploy ## real restic/SQLite round trip in Docker (no R2 credentials)
	deploy/backup/test.sh
