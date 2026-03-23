.PHONY: dev dev-backend dev-frontend migrate-up migrate-down seed reset-dev test test-backend test-frontend lint docker-up docker-down scan scan-backend scan-frontend

# Load .env into shell commands
DOTENV := $(shell [ -f .env ] && echo "set -a && . ./.env && set +a &&" || echo "")

dev:
	@echo "Starting backend and frontend..."
	$(MAKE) -j2 dev-backend dev-frontend

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

scan: scan-backend scan-frontend

scan-backend:
	docker build -t kuayle-backend:scan ./BE
	trivy image --severity CRITICAL,HIGH --exit-code 1 kuayle-backend:scan

scan-frontend:
	docker build -t kuayle-frontend:scan ./UI
	trivy image --severity CRITICAL,HIGH --exit-code 1 kuayle-frontend:scan
