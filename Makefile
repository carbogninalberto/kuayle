.PHONY: dev dev-backend dev-frontend migrate-up migrate-down seed reset-dev test test-backend test-frontend lint docker-up docker-down ensure-trivy scan scan-backend scan-frontend

# Load .env into shell commands
DOTENV := $(shell [ -f .env ] && echo "set -a && . ./.env && set +a &&" || echo "")

dev:
	@echo "Starting backend and frontend..."
	$(MAKE) -j2 dev-backend dev-frontend

dev-all: dev-start-services dev

dev-start-services:
	@echo "Starting Postgres and Redis..."
	docker compose up postgres redis -d

dev-smee:
	@echo "Starting smee webhook proxy..."
	$(DOTENV) npx smee-client --url $${GITHUB_WEBHOOK_URL} --target http://localhost:8080/api/github/webhook

dev-full: dev-start-services
	@echo "Starting backend, frontend, and smee..."
	$(MAKE) -j3 dev-backend dev-frontend dev-smee

dev-backend:
	$(DOTENV) cd BE && go run ./cmd/server

dev-frontend:
	cd UI && npm run dev

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
	docker compose up --build -d

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
