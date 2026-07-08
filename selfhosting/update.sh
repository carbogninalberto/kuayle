#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

REPO_ROOT="$(pwd)"
SELFHOST_DIR="$REPO_ROOT/selfhosting"
UPGRADE_MARKER="$SELFHOST_DIR/runtime/upgrading"

disable_upgrade_page() {
	rm -f "$UPGRADE_MARKER"
}

handle_interrupt() {
	disable_upgrade_page
	exit 130
}

handle_terminate() {
	disable_upgrade_page
	exit 143
}

echo "=== Kuayle - Self-host Update ==="

# 1. Pull latest code
echo "Pulling latest changes..."
git fetch --all --tags
git pull --ff-only

# 2. Serve the upgrade page while app containers are refreshed
echo "Serving upgrade page during update..."
mkdir -p "$SELFHOST_DIR/runtime"
touch "$UPGRADE_MARKER"
trap disable_upgrade_page EXIT
trap handle_interrupt INT
trap handle_terminate TERM

cd "$SELFHOST_DIR"
docker compose up -d caddy >/dev/null 2>&1 || true

# 3. Rebuild and recreate containers
echo "Rebuilding images and refreshing containers..."
docker compose build --pull backend frontend
docker compose up -d --remove-orphans

# 4. Apply pending migrations
echo "Applying database migrations..."
docker compose exec -T backend /app/server migrate up

disable_upgrade_page
trap - EXIT INT TERM

echo ""
echo "=== Update complete ==="
echo "  Site:    https://localhost (or your DOMAIN)"
echo "  Health:  /health"
echo ""
echo "To tail logs:   docker compose -f selfhosting/docker-compose.yml logs -f"
echo "To restart:     docker compose -f selfhosting/docker-compose.yml restart"
