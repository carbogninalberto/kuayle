#!/usr/bin/env bash
# Optional self-hosted web search (SearXNG) for the assistant's web_search tool.
#
#   selfhosting/searxng.sh enable         generate SEARXNG_SECRET (if missing), set
#                                         WEB_SEARCH_SEARXNG_URL, start searxng and
#                                         recreate the backend
#   selfhosting/searxng.sh disable        clear WEB_SEARCH_SEARXNG_URL, recreate the
#                                         backend and stop searxng
#   selfhosting/searxng.sh ensure-secret  only generate SEARXNG_SECRET if missing
#                                         (used by update.sh)
#
# Edits selfhosting/.env in place (keeping its permissions); never prints the secret.
set -euo pipefail

SELFHOST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${KUAYLE_ENV_FILE:-$SELFHOST_DIR/.env}"
SEARXNG_URL=http://searxng:8080

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

env_value() {
	# Last assignment wins, as in Compose; surrounding quotes are dropped.
	awk -v key="$1" '
		index($0, key "=") == 1 { value = substr($0, length(key) + 2) }
		END { gsub(/^["\047]|["\047]$/, "", value); print value }
	' "$ENV_FILE"
}

env_set() {
	local key=$1 value=$2 temp
	temp=$(mktemp "$ENV_FILE.XXXXXX")
	awk -v key="$key" -v value="$value" '
		index($0, key "=") == 1 { if (!done) print key "=" value; done = 1; next }
		{ print }
		END { if (!done) print key "=" value }
	' "$ENV_FILE" >"$temp"
	# Rewrite in place so the file keeps its owner and mode.
	cat "$temp" >"$ENV_FILE"
	rm -f "$temp"
}

generate_secret() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -hex 32
	else
		od -An -tx1 -N32 /dev/urandom | tr -d ' \n'
	fi
}

ensure_secret() {
	[ -f "$ENV_FILE" ] || die "$ENV_FILE not found: create it from selfhosting/.env.example first"
	local secret
	secret=$(env_value SEARXNG_SECRET)
	if [ "${#secret}" -ge 32 ]; then
		echo "SEARXNG_SECRET is already set."
		return
	fi
	env_set SEARXNG_SECRET "$(generate_secret)"
	echo "Generated SEARXNG_SECRET in $ENV_FILE."
}

compose() {
	docker compose --project-directory "$SELFHOST_DIR" -f "$SELFHOST_DIR/docker-compose.yml" --env-file "$ENV_FILE" "$@"
}

case "${1:-}" in
ensure-secret)
	ensure_secret
	;;
enable)
	ensure_secret
	current=$(env_value WEB_SEARCH_SEARXNG_URL)
	if [ -z "$current" ]; then
		env_set WEB_SEARCH_SEARXNG_URL "$SEARXNG_URL"
		echo "Set WEB_SEARCH_SEARXNG_URL=$SEARXNG_URL."
	elif [ "$current" != "$SEARXNG_URL" ]; then
		echo "Keeping WEB_SEARCH_SEARXNG_URL=$current (not the bundled service)."
	fi
	compose --profile searxng up -d --wait searxng
	compose up -d --no-deps backend
	echo "SearXNG is available: workspace owners and admins can choose it in Settings → Assistant → Research."
	;;
disable)
	[ -f "$ENV_FILE" ] || die "$ENV_FILE not found"
	env_set WEB_SEARCH_SEARXNG_URL ""
	compose up -d --no-deps backend
	compose --profile searxng stop searxng
	echo "SearXNG is off; workspaces that chose it report that web search is not configured."
	;;
*)
	die "usage: $0 enable|disable|ensure-secret"
	;;
esac
