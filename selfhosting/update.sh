#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

REPO_ROOT="$(pwd)"
SELFHOST_DIR="$REPO_ROOT/selfhosting"

echo "=== Kuayle - Self-host Update ==="

# 1. Pull latest code
echo "Pulling latest changes..."
git fetch --all --tags
git pull --ff-only

# 2. Rebuild and recreate containers
echo "Rebuilding images and refreshing containers..."
cd "$SELFHOST_DIR"
docker compose build --pull backend frontend
docker compose up -d --remove-orphans

# 3. Apply pending migrations
echo "Applying database migrations..."
docker compose exec -T backend /app/server migrate up

echo ""
echo "=== Update complete ==="
echo "  Site:    https://localhost (or your DOMAIN)"
echo "  Health:  /health"
echo ""
echo "To tail logs:   docker compose -f selfhosting/docker-compose.yml logs -f"
echo "To restart:     docker compose -f selfhosting/docker-compose.yml restart"
