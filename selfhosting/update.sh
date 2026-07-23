#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

REPO_ROOT="$(pwd)"
SELFHOST_DIR="$REPO_ROOT/selfhosting"
UPGRADE_MARKER="$SELFHOST_DIR/runtime/upgrading"
CONTROL_PLANE_STOPPED=false

disable_upgrade_page() {
	rm -f "$UPGRADE_MARKER"
}

handle_exit() {
	status=$?
	trap - EXIT INT TERM
	disable_upgrade_page
	if [ "$status" -ne 0 ]; then
		echo "Update failed with status $status; existing container logs were preserved." >&2
		if [ "$CONTROL_PLANE_STOPPED" = true ]; then
			echo "Restoring previous Dev Machines control plane..." >&2
			if docker compose --profile dev-machines start machine-gateway machine-manager; then
				echo "Previous Dev Machines control plane restored." >&2
			else
				echo "WARNING: failed to restore Dev Machines control plane; inspect preserved container logs." >&2
			fi
		fi
	fi
	exit "$status"
}

handle_interrupt() {
	exit 130
}

handle_terminate() {
	exit 143
}

echo "=== Kuayle - Self-host Update ==="

# 1. Pull latest code
echo "Pulling latest changes..."
git fetch --all --tags
git pull --ff-only

# Detect running Dev Machines services without enabling the optional profile.
DEV_MACHINES_ACTIVE=false
if docker compose -f "$SELFHOST_DIR/docker-compose.yml" --profile dev-machines ps --status running --services 2>/dev/null | grep -qE '^machine-(gateway|manager)$'; then
	DEV_MACHINES_ACTIVE=true
	echo "Dev Machines services detected - including control plane and runtime images"
fi

# 2. Serve the upgrade page while app containers are refreshed
echo "Serving upgrade page during update..."
mkdir -p "$SELFHOST_DIR/runtime"
touch "$UPGRADE_MARKER"
trap handle_exit EXIT
trap handle_interrupt INT
trap handle_terminate TERM

cd "$SELFHOST_DIR"
docker compose up -d --no-deps caddy >/dev/null 2>&1 || true

# 3. Rebuild images
echo "Rebuilding images and refreshing containers..."
docker compose build --pull backend frontend
if [ "$DEV_MACHINES_ACTIVE" = true ]; then
	docker compose --profile dev-machines build --pull machine-gateway machine-manager
	docker compose --profile dev-machine-images build --pull \
		dev-machine-ide dev-machine-browser dev-machine-collector \
		dev-machine-egress dev-machine-agent-claude dev-machine-agent-opencode dev-machine-agent-codex
	CONTROL_PLANE_STOPPED=true
	docker compose --profile dev-machines stop machine-manager machine-gateway
fi

# 4. Apply additive migrations before starting the new application image.
echo "Applying database migrations..."
docker compose run --rm --no-deps backend /app/server migrate up
if [ "$DEV_MACHINES_ACTIVE" = true ]; then
	docker compose --profile dev-machines run --rm machine-gateway-db-provision
fi

# 5. Restart the application without removing optional-profile services.
docker compose up -d caddy backend frontend

# 6. Restart the optional control plane only after migrations are current.
if [ "$DEV_MACHINES_ACTIVE" = true ]; then
	docker compose --profile dev-machines up -d machine-gateway machine-manager
	CONTROL_PLANE_STOPPED=false
fi

disable_upgrade_page
trap - EXIT INT TERM

echo ""
echo "=== Update complete ==="
echo "  Site:    https://<your-domain>"
echo "  Health:  /health"
echo ""
echo "To tail logs:   docker compose -f selfhosting/docker-compose.yml logs -f"
echo "To restart:     docker compose -f selfhosting/docker-compose.yml restart"
