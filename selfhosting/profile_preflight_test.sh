#!/usr/bin/env bash
set -euo pipefail

if [ "$(basename "$0")" = psql ]; then
	printf 'psql called\n' >>"${PSQL_LOG:?}"
	exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOCKER_BIN=${DOCKER_BIN:-docker}

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

compose_json=$(POSTGRES_PASSWORD=test-password JWT_SECRET=01234567890123456789012345678901 \
	DEV_MACHINE_GATEWAY_DB_PASSWORD=gateway-password DEV_MACHINES_ENABLED=true \
	"$DOCKER_BIN" compose --file "$SCRIPT_DIR/docker-compose.yml" --profile dev-machines config --format json)
preflight=$(printf '%s' "$compose_json" | jq -r '.services["machine-gateway-db-provision"].command[2]')
preflight=${preflight//\$\$/\$}

test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT
fake_bin="$test_root/bin"
psql_log="$test_root/psql.log"
mkdir -p "$fake_bin"
ln -s "$SCRIPT_DIR/profile_preflight_test.sh" "$fake_bin/psql"

run_case() {
	local name=$1
	local enabled=$2
	local environment=$3
	local domain=$4
	local caddyfile=$5
	local expected_status=$6
	local expected_message=$7
	local expect_psql=$8
	local output status

	: >"$psql_log"
	set +e
	output=$(PATH="$fake_bin:$PATH" PSQL_LOG="$psql_log" DEV_MACHINES_ENABLED="$enabled" \
		ENVIRONMENT="$environment" DEV_MACHINE_DOMAIN="$domain" CADDYFILE_PATH="$caddyfile" \
		GATEWAY_DB_PASSWORD=gateway-password POSTGRES_USER=kuayle POSTGRES_DB=kuayle \
		sh -ec "$preflight" 2>&1)
	status=$?
	set -e

	[ "$status" -eq "$expected_status" ] || fail "$name returned $status, expected $expected_status: $output"
	case "$output" in *"$expected_message"*) ;; *) fail "$name did not report $expected_message: $output" ;; esac
	if [ "$expect_psql" = true ]; then
		grep -Fq 'psql called' "$psql_log" || fail "$name did not reach psql"
	elif [ -s "$psql_log" ]; then
		fail "$name reached psql before validation"
	fi
}

certificate_caddyfile="$test_root/certificate.Caddyfile"
commented_caddyfile="$test_root/commented.Caddyfile"
issuer_caddyfile="$test_root/issuer.Caddyfile"
local_certs_caddyfile="$test_root/local-certs.Caddyfile"
printf '*.machines.example.net {\n\ttls /certs/machines.pem /certs/machines.key\n}\n' >"$certificate_caddyfile"
printf '*.machines.example.net {\n\t# tls internal\n\ttls /certs/machines.pem /certs/machines.key\n}\n' >"$commented_caddyfile"
printf '*.machines.example.net {\n\ttls {\n\t\tissuer internal\n\t}\n}\n' >"$issuer_caddyfile"
printf '{\n\tlocal_certs\n}\n' >"$local_certs_caddyfile"

run_case disabled false production machines.example.net "$SCRIPT_DIR/Caddyfile" 1 'DEV_MACHINES_ENABLED must be true' false
run_case production-internal true PRODUCTION MACHINES.EXAMPLE.NET. "$SCRIPT_DIR/Caddyfile" 1 'production Dev Machines cannot use Caddy internal TLS' false
run_case production-internal-issuer true production machines.example.net "$issuer_caddyfile" 1 'production Dev Machines cannot use Caddy internal TLS' false
run_case production-local-certs true production machines.example.net "$local_certs_caddyfile" 1 'production Dev Machines cannot use Caddy internal TLS' false
run_case localhost-exception true production machines.localhost "$SCRIPT_DIR/Caddyfile" 0 '' true
run_case non-production true development machines.example.net "$SCRIPT_DIR/Caddyfile" 0 '' true
run_case production-certificate true production machines.example.net "$certificate_caddyfile" 0 '' true
run_case commented-internal true production machines.example.net "$commented_caddyfile" 0 '' true

printf 'dev-machines profile preflight tests passed\n'
