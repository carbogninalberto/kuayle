#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

REPO_ROOT="$(pwd)"
SELFHOST_DIR="$REPO_ROOT/selfhosting"
UPGRADE_MARKER="$SELFHOST_DIR/runtime/upgrading"
CONTROL_PLANE_STOPPED=false
APPLICATION_STOPPED=false
MIGRATION_ATTEMPTED=false

disable_upgrade_page() {
	rm -f "$UPGRADE_MARKER"
}

handle_exit() {
	status=$?
	trap - EXIT INT TERM
	keep_upgrade_page=false
	if [ "$status" -ne 0 ]; then
		echo "Update failed with status $status; existing container logs were preserved." >&2
		if [ "$APPLICATION_STOPPED" = true ] && [ "$MIGRATION_ATTEMPTED" = false ]; then
			echo "Restoring previous backend..." >&2
			if docker compose start backend; then
				echo "Previous backend restored." >&2
			else
				echo "WARNING: failed to restore the previous backend; inspect preserved container logs." >&2
			fi
		elif [ "$APPLICATION_STOPPED" = true ]; then
			# Earlier backends cannot run against a partially or fully migrated schema.
			keep_upgrade_page=true
			echo "WARNING: the backend was left stopped and the upgrade page remains enabled." >&2
			echo "Migrations may have been applied; previous backend versions are unsupported against them." >&2
			echo "Inspect 'docker compose run --rm --no-deps backend /app/server migrate version', resolve the failure, then rerun this script." >&2
		fi
		if [ "$CONTROL_PLANE_STOPPED" = true ]; then
			echo "Restoring previous Dev Machines control plane..." >&2
			if docker compose --profile dev-machines start machine-gateway machine-manager; then
				echo "Previous Dev Machines control plane restored." >&2
			else
				echo "WARNING: failed to restore Dev Machines control plane; inspect preserved container logs." >&2
			fi
		fi
	fi
	if [ "$keep_upgrade_page" = false ]; then
		disable_upgrade_page
	fi
	exit "$status"
}

handle_interrupt() {
	exit 130
}

handle_terminate() {
	exit 143
}

# Value of a setting as Compose resolves it: the caller's environment wins
# over selfhosting/.env. Values are only compared, never printed.
env_setting() {
	local name=$1 line value
	if [ -n "${!name+x}" ]; then
		printf '%s' "${!name}"
		return 0
	fi
	[ -f "$SELFHOST_DIR/.env" ] || return 0
	line=$(grep -E "^[[:space:]]*(export[[:space:]]+)?$name=" "$SELFHOST_DIR/.env" | tail -n 1 || true)
	value=${line#*=}
	value=${value%$'\r'}
	case "$value" in
	\"*\") value=${value#\"} value=${value%\"} ;;
	\'*\') value=${value#\'} value=${value%\'} ;;
	esac
	printf '%s' "$value"
}

# Matches the values published in the example env files, the development
# Compose defaults and the documentation (same rule as the backend).
is_placeholder_secret() {
	case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
	change_me* | change-me* | changeme* | local_dev_* | *_do_not_use_prod | encoded_password | "<"*">") return 0 ;;
	esac
	return 1
}

# The backend refuses to start outside development while a secret is still a
# published example value. Refuse here first, before anything is stopped.
refuse_placeholder_secrets() {
	local environment names name value found=false
	environment=$(env_setting ENVIRONMENT | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
	if [ "$environment" = development ]; then
		return 0
	fi
	names="JWT_SECRET POSTGRES_PASSWORD SYSTEM_UPDATER_TOKEN VOICE_INTERNAL_TOKEN DEV_MACHINE_ENCRYPTION_KEY SMTP_PASSWORD S3_SECRET_KEY GITHUB_APP_CLIENT_SECRET GITHUB_APP_WEBHOOK_SECRET"
	if [ "$DEV_MACHINES_ACTIVE" = true ]; then
		names="$names DEV_MACHINE_GATEWAY_DB_PASSWORD DEV_MACHINE_GATEWAY_DATABASE_URL"
	fi
	for name in $names; do
		value=$(env_setting "$name")
		if [ "$name" = DEV_MACHINE_GATEWAY_DATABASE_URL ]; then
			# Only the password between "user:" and the last "@" matters.
			value=${value#*://}
			case "$value" in *:*@*) value=${value%@*} value=${value#*:} ;; *) value= ;; esac
		fi
		if is_placeholder_secret "$value"; then
			echo "$name is still an example placeholder or development default." >&2
			found=true
		elif [ "$name" = POSTGRES_PASSWORD ] && { [ -z "$value" ] || [ "$value" = kuayle ]; }; then
			# Unset means the Compose files fall back to a password published
			# in this repository.
			echo "$name is still an example placeholder or development default." >&2
			echo "  (it is unset or equals the Compose fallback, so the database uses a published password)" >&2
			found=true
		fi
	done
	if [ "$found" = true ]; then
		echo "Update refused: nothing was stopped or changed. Generate a unique value for each setting above" >&2
		echo "(for example: openssl rand -hex 32), set it in selfhosting/.env and rerun this script." >&2
		echo "See 'Secrets must be generated' in docs/self-hosting.md before changing POSTGRES_PASSWORD or a gateway password." >&2
		exit 1
	fi
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

refuse_placeholder_secrets

# Detect a running SearXNG (optional "searxng" profile) the same way.
SEARXNG_ACTIVE=false
if docker compose -f "$SELFHOST_DIR/docker-compose.yml" --profile searxng ps --status running --services 2>/dev/null | grep -qx searxng; then
	SEARXNG_ACTIVE=true
	echo "SearXNG detected - refreshing its pinned image"
	# Keeps an existing secret; generates one for an instance enabled by hand.
	bash "$SELFHOST_DIR/searxng.sh" ensure-secret
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
		dev-machine-ide dev-machine-headless dev-machine-browser dev-machine-collector \
		dev-machine-egress dev-machine-docker dev-machine-agent-claude dev-machine-agent-opencode dev-machine-agent-codex
	CONTROL_PLANE_STOPPED=true
	docker compose --profile dev-machines stop machine-manager machine-gateway
fi
if [ "$SEARXNG_ACTIVE" = true ]; then
	docker compose --profile searxng pull searxng
fi

# 4. Stop the running backend so no previous-version writer (requests or
# background jobs) runs during or after migrations; mixed versions are unsupported.
echo "Stopping application writers..."
APPLICATION_STOPPED=true
docker compose stop backend

# Apply migrations before starting the new application image.
echo "Applying database migrations..."
MIGRATION_ATTEMPTED=true
docker compose run --rm --no-deps backend /app/server migrate up
if [ "$DEV_MACHINES_ACTIVE" = true ]; then
	docker compose --profile dev-machines run --rm machine-gateway-db-provision
fi

# 5. Restart the application without removing optional-profile services.
docker compose up -d caddy backend frontend
APPLICATION_STOPPED=false

# 6. Restart the optional control plane only after migrations are current.
if [ "$DEV_MACHINES_ACTIVE" = true ]; then
	docker compose --profile dev-machines up -d machine-gateway machine-manager
	CONTROL_PLANE_STOPPED=false
fi

# 7. Web search is optional: a SearXNG failure is reported, not fatal.
if [ "$SEARXNG_ACTIVE" = true ]; then
	docker compose --profile searxng up -d searxng ||
		echo "WARNING: SearXNG did not restart; web_search reports it cannot reach it. Inspect 'docker compose --profile searxng logs searxng'." >&2
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
