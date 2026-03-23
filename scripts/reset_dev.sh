#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

echo "=== Kuayle - Reset Dev Environment ==="

docker compose down -v
docker compose up postgres redis -d

echo "Waiting for postgres..."
sleep 3

make migrate-up
bash scripts/seed.sh

echo "=== Done. Ready to dev ==="
