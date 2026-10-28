.PHONY: dev dev-all dev-backend dev-frontend dev-start-services dev-reset migrate-up migrate-down seed reset-dev test test-backend test-frontend lint docker-up docker-down ensure-trivy scan scan-backend scan-frontend

# Load .env into shell commands
DOTENV := $(shell [ -f .env ] && echo "set -a && . ./.env && set +a &&" || echo "")
DOCKER_SOCKET_GID = $$(docker run --rm -v /var/run/docker.sock:/docker.sock:ro alpine:3.22 stat -c '%g' /docker.sock)
DEV_MACHINE_COMPOSE = DEV_MACHINE_DOCKER_GID="$(DOCKER_SOCKET_GID)" docker compose

dev:
	$(MAKE) migrate-up
	@echo "Starting backend and frontend..."
	$(MAKE) -j2 dev-backend dev-frontend

dev-all: dev-start-services dev

dev-start-services:
	@echo "Starting Postgres and Redis..."
	$(DEV_MACHINE_COMPOSE) up -d --wait postgres redis
	@echo "Applying migrations required by the Dev Machine gateway role..."
	$(MAKE) migrate-up
	@echo "Ensuring local Dev Machine runtime images exist..."
	$(DEV_MACHINE_COMPOSE) --profile dev-machine-images build dev-machine-ide dev-machine-headless dev-machine-browser dev-machine-collector dev-machine-egress dev-machine-docker dev-machine-agent-claude dev-machine-agent-opencode dev-machine-agent-codex
	@echo "Starting Dev Machine control plane and wildcard TLS proxy (*.machines.localhost)..."
	@echo "Note: this dev proxy binds 127.0.0.1:80 and 127.0.0.1:443; stop the selfhosting stack first if those ports are occupied."
	$(DEV_MACHINE_COMPOSE) --profile dev-machines rm -sf machine-gateway-db-provision >/dev/null 2>&1 || true
	$(DEV_MACHINE_COMPOSE) --profile dev-machines up -d --build --wait machine-gateway-db-provision machine-gateway machine-manager machine-proxy

dev-reset:
	$(MAKE) reset-dev
	$(MAKE) seed
	$(MAKE) dev-start-services
	$(MAKE) dev

dev-smee:
	@echo "Starting smee webhook proxy..."
	$(DOTENV) npx smee-client --url $${GITHUB_WEBHOOK_URL} --target http://localhost:8080/api/github/webhook

dev-full: dev-start-services
	@echo "Starting backend, frontend, and smee..."
	$(MAKE) -j3 dev-backend dev-frontend dev-smee

dev-backend:
	$(DOTENV) cd BE && go run ./cmd/server

dev-frontend:
	$(DOTENV) cd UI && npm run dev

migrate-up:
	$(DOTENV) cd BE && go run ./cmd/server migrate up

migrate-down:
	$(DOTENV) cd BE && go run ./cmd/server migrate down

seed:
	bash scripts/seed.sh

reset-dev:
	bash scripts/reset_dev.sh

test: test-backend test-frontend

test-backend:
	$(DOTENV) cd BE && go test ./...

test-frontend:
	cd UI && npm run test

lint:
	cd BE && golangci-lint run ./...
	cd UI && npm run lint

docker-up:
	docker compose --profile app up --build -d

docker-down:
	docker compose down

ensure-trivy:
	@command -v trivy >/dev/null 2>&1 || { \
		echo "Trivy not found, installing..."; \
		OS=$$(uname -s); \
		if [ "$$OS" = "Darwin" ]; then \
			brew install trivy; \
		elif [ "$$OS" = "Linux" ]; then \
			curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sudo sh -s -- -b /usr/local/bin; \
		else \
			echo "Unsupported OS: $$OS. Install Trivy manually: https://trivy.dev"; \
			exit 1; \
		fi; \
	}

scan: scan-backend scan-frontend

scan-backend: ensure-trivy
	docker build -t kuayle-backend:scan ./BE
	trivy image --severity CRITICAL,HIGH --exit-code 1 kuayle-backend:scan

scan-frontend: ensure-trivy
	docker build -t kuayle-frontend:scan ./UI
	trivy image --severity CRITICAL,HIGH --exit-code 1 kuayle-frontend:scan
