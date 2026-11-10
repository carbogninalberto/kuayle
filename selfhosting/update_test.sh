#!/usr/bin/env bash
set -euo pipefail

command_name="$(basename "$0")"
if [ "$command_name" = git ]; then
	printf 'git %s\n' "$*" >>"${COMMAND_LOG:?}"
	exit 0
fi
if [ "$command_name" = docker ]; then
	printf 'docker %s\n' "$*" >>"${COMMAND_LOG:?}"
	case " $* " in
	*" --profile searxng ps --status running --services "*)
		[ "${SEARXNG_RUNNING:-false}" = true ] && printf '%s\n' searxng
		exit 0
		;;
	*" ps --status running --services "*)
		[ "${DEV_MACHINES_RUNNING:-true}" = true ] && printf '%s\n' machine-gateway machine-manager
		exit 0
		;;
	esac
	case "${FAIL_POINT:-}" in
	runtime-build)
		case " $* " in *" --profile dev-machine-images build --pull "*) exit 41 ;; esac
		;;
	stop-backend)
		case " $* " in *" compose stop backend "*) exit 46 ;; esac
		;;
	migration)
		case " $* " in *" run --rm --no-deps backend /app/server migrate up "*) exit 42 ;; esac
		;;
	provision)
		case " $* " in *" run --rm machine-gateway-db-provision "*) exit 43 ;; esac
		;;
	application-up)
		case " $* " in *" compose up -d caddy backend frontend "*) exit 44 ;; esac
		;;
	searxng-up)
		case " $* " in *" --profile searxng up -d searxng "*) exit 47 ;; esac
		;;
	control-plane-up)
		case " $* " in *" --profile dev-machines up -d machine-gateway machine-manager "*) exit 45 ;; esac
		;;
	esac
	if [ "${FAIL_RESTORE:-false}" = true ]; then
		case " $* " in *" start machine-gateway machine-manager "*) exit 99 ;; esac
	fi
	if [ "${FAIL_APP_RESTORE:-false}" = true ]; then
		case " $* " in *" compose start backend "*) exit 98 ;; esac
	fi
	exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

run_case() {
	local name=$1
	local fail_point=$2
	local expected_status=$3
	local expect_restore=$4
	local fail_restore=${5:-false}
	local expect_app_restore=${6:-false}
	local fail_app_restore=${7:-false}
	local expect_backend_held=${8:-false}

	local test_root repo_root fake_bin command_log output status
	test_root=$(mktemp -d)
	repo_root="$test_root/repo"
	fake_bin="$test_root/bin"
	command_log="$test_root/commands.log"
	mkdir -p "$repo_root/selfhosting/runtime" "$fake_bin"
	cp "$SCRIPT_DIR/update.sh" "$repo_root/selfhosting/update.sh"
	ln -s "$SCRIPT_DIR/update_test.sh" "$fake_bin/git"
	ln -s "$SCRIPT_DIR/update_test.sh" "$fake_bin/docker"
	: >"$command_log"

	set +e
	output=$(PATH="$fake_bin:$PATH" COMMAND_LOG="$command_log" FAIL_POINT="$fail_point" FAIL_RESTORE="$fail_restore" FAIL_APP_RESTORE="$fail_app_restore" \
		bash "$repo_root/selfhosting/update.sh" 2>&1)
	status=$?
	set -e

	[ "$status" -eq "$expected_status" ] || fail "$name returned $status, expected $expected_status: $output"
	if [ "$expect_backend_held" = true ]; then
		# After migrations were attempted the previous backend must stay stopped
		# behind the upgrade page instead of running against the new schema.
		[ -e "$repo_root/selfhosting/runtime/upgrading" ] || fail "$name removed the upgrade page with no backend running"
		case "$output" in *"the backend was left stopped"*) ;; *) fail "$name did not explain the stopped backend" ;; esac
	else
		[ ! -e "$repo_root/selfhosting/runtime/upgrading" ] || fail "$name left the upgrade marker"
	fi
	if [ "$expect_restore" = true ]; then
		grep -Fq 'docker compose --profile dev-machines stop machine-manager machine-gateway' "$command_log" || fail "$name did not stop the control plane"
		grep -Fq 'docker compose --profile dev-machines start machine-gateway machine-manager' "$command_log" || fail "$name did not restore the prior containers"
		case "$output" in *"existing container logs were preserved"*) ;; *) fail "$name did not report preserved logs" ;; esac
	else
		if grep -Fq ' start machine-gateway machine-manager' "$command_log"; then
			fail "$name attempted restoration before stopping the control plane"
		fi
	fi
	if [ "$fail_restore" = true ]; then
		case "$output" in *"WARNING: failed to restore Dev Machines control plane"*) ;; *) fail "$name hid restoration failure" ;; esac
	fi
	if [ "$expected_status" -eq 0 ]; then
		grep -Fq 'docker compose --profile dev-machines up -d machine-gateway machine-manager' "$command_log" || fail "$name did not start the updated control plane"
	fi
	# Migrations must never run beside a previous-version backend writer.
	if grep -Fq 'migrate up' "$command_log"; then
		local stop_line migrate_line
		stop_line=$(grep -Fn 'docker compose stop backend' "$command_log" | head -n1 | cut -d: -f1 || true)
		migrate_line=$(grep -Fn 'migrate up' "$command_log" | head -n1 | cut -d: -f1 || true)
		[ -n "$stop_line" ] && [ "$stop_line" -lt "$migrate_line" ] || fail "$name migrated without stopping the backend"
	fi
	if [ "$expect_app_restore" = true ]; then
		grep -Fq 'docker compose start backend' "$command_log" || fail "$name did not restore the previous backend"
	elif grep -Fq 'docker compose start backend' "$command_log"; then
		fail "$name restarted a previous backend after migrations were attempted"
	fi
	if [ "$fail_app_restore" = true ]; then
		case "$output" in *"WARNING: failed to restore the previous backend"*) ;; *) fail "$name hid backend restoration failure" ;; esac
	fi

	rm -rf "$test_root"
}

# Every runtime image in the Compose dev-machine-images profile must be rebuilt on update,
# otherwise upgraded hosts keep a stale or missing image (e.g. dev-machine-headless).
image_services=$(awk '/^  [a-z0-9-]+:$/ { service = substr($1, 1, length($1) - 1) } /profiles: \["dev-machine-images"\]/ { print service }' "$SCRIPT_DIR/docker-compose.yml")
[ -n "$image_services" ] || fail "no dev-machine-images services found in docker-compose.yml"
build_command=$(awk '/--profile dev-machine-images build --pull/ { capture = 1 } capture { print } capture && !/\\$/ { exit }' "$SCRIPT_DIR/update.sh")
for service in $image_services; do
	case " $(printf '%s' "$build_command" | tr -d '\\' | tr -s '[:space:]' ' ') " in
	*" $service "*) ;;
	*) fail "update.sh does not rebuild runtime image $service" ;;
	esac
done

# An active SearXNG keeps its secret, gets its pinned image refreshed and is
# restarted after the application; a SearXNG failure does not fail the update.
run_searxng_case() {
	local name=$1 fail_point=$2 env_content=$3
	local test_root repo_root fake_bin command_log output status
	test_root=$(mktemp -d)
	repo_root="$test_root/repo"
	fake_bin="$test_root/bin"
	command_log="$test_root/commands.log"
	mkdir -p "$repo_root/selfhosting/runtime" "$fake_bin"
	cp "$SCRIPT_DIR/update.sh" "$SCRIPT_DIR/searxng.sh" "$repo_root/selfhosting/"
	printf '%b' "$env_content" >"$repo_root/selfhosting/.env"
	chmod 600 "$repo_root/selfhosting/.env"
	ln -s "$SCRIPT_DIR/update_test.sh" "$fake_bin/git"
	ln -s "$SCRIPT_DIR/update_test.sh" "$fake_bin/docker"
	: >"$command_log"
	set +e
	output=$(PATH="$fake_bin:$PATH" COMMAND_LOG="$command_log" FAIL_POINT="$fail_point" SEARXNG_RUNNING=true \
		bash "$repo_root/selfhosting/update.sh" 2>&1)
	status=$?
	set -e
	[ "$status" -eq 0 ] || fail "$name returned $status: $output"
	grep -Fq 'docker compose --profile searxng pull searxng' "$command_log" || fail "$name did not refresh the SearXNG image"
	local app_line searxng_line
	app_line=$(grep -Fn 'docker compose up -d caddy backend frontend' "$command_log" | head -n1 | cut -d: -f1)
	searxng_line=$(grep -Fn 'docker compose --profile searxng up -d searxng' "$command_log" | head -n1 | cut -d: -f1)
	[ -n "$searxng_line" ] && [ "$app_line" -lt "$searxng_line" ] || fail "$name did not restart SearXNG after the application"
	local secret
	secret=$(sed -n 's/^SEARXNG_SECRET=//p' "$repo_root/selfhosting/.env")
	[ "${#secret}" -ge 32 ] || fail "$name left SEARXNG_SECRET unset"
	[ "$(grep -c '^SEARXNG_SECRET=' "$repo_root/selfhosting/.env")" -eq 1 ] || fail "$name duplicated SEARXNG_SECRET"
	grep -Fq 'JWT_SECRET=keep-me' "$repo_root/selfhosting/.env" || fail "$name lost other settings"
	case "$output" in *"$secret"*) fail "$name printed the secret" ;; esac
	if [ "$fail_point" = searxng-up ]; then
		case "$output" in *"WARNING: SearXNG did not restart"*) ;; *) fail "$name hid the SearXNG failure" ;; esac
	fi
	printf '%s\n' "$secret"
	rm -rf "$test_root"
}

existing=0123456789abcdef0123456789abcdef0123456789abcdef
[ "$(run_searxng_case searxng-keeps-secret '' "JWT_SECRET=keep-me\nSEARXNG_SECRET=$existing\n")" = "$existing" ] || fail "update.sh replaced an existing SEARXNG_SECRET"
run_searxng_case searxng-generates-secret '' 'JWT_SECRET=keep-me\nSEARXNG_SECRET=\n' >/dev/null
run_searxng_case searxng-restart-failure searxng-up 'JWT_SECRET=keep-me\n' >/dev/null

# Published example values are refused before anything is rebuilt or stopped,
# naming the setting and never printing its value.
example_value() {
	sed -n "s/^#\{0,1\} \{0,1\}$2=//p" "$1" | tail -n 1
}
run_placeholder_case() {
	local name=$1 env_content=$2 dev_machines_running=$3 expected=$4
	local test_root repo_root fake_bin command_log output status
	test_root=$(mktemp -d)
	repo_root="$test_root/repo"
	fake_bin="$test_root/bin"
	command_log="$test_root/commands.log"
	mkdir -p "$repo_root/selfhosting/runtime" "$fake_bin"
	cp "$SCRIPT_DIR/update.sh" "$repo_root/selfhosting/update.sh"
	printf '%b' "$env_content" >"$repo_root/selfhosting/.env"
	ln -s "$SCRIPT_DIR/update_test.sh" "$fake_bin/git"
	ln -s "$SCRIPT_DIR/update_test.sh" "$fake_bin/docker"
	: >"$command_log"
	set +e
	output=$(PATH="$fake_bin:$PATH" COMMAND_LOG="$command_log" FAIL_POINT='' DEV_MACHINES_RUNNING="$dev_machines_running" \
		bash "$repo_root/selfhosting/update.sh" 2>&1)
	status=$?
	set -e
	if [ -z "$expected" ]; then
		[ "$status" -eq 0 ] || fail "$name returned $status: $output"
		grep -Fq 'docker compose up -d caddy backend frontend' "$command_log" || fail "$name did not update the application"
	else
		[ "$status" -ne 0 ] || fail "$name updated a stack that still uses a published example value"
		case "$output" in *"$expected is still an example placeholder or development default"*) ;; *) fail "$name did not name $expected: $output" ;; esac
		if grep -Eq ' (build|stop|run|up) ' "$command_log"; then
			fail "$name touched containers before refusing: $(cat "$command_log")"
		fi
		[ ! -e "$repo_root/selfhosting/runtime/upgrading" ] || fail "$name left the upgrade marker"
	fi
	local value
	for value in "$selfhost_jwt" "$selfhost_postgres" "$selfhost_gateway" "$selfhost_updater" "$development_jwt"; do
		case "$output" in *"$value"*) fail "$name printed a configured value" ;; esac
	done
}

selfhost_jwt=$(example_value "$SCRIPT_DIR/.env.example" JWT_SECRET)
selfhost_postgres=$(example_value "$SCRIPT_DIR/.env.example" POSTGRES_PASSWORD)
selfhost_gateway=$(example_value "$SCRIPT_DIR/.env.example" DEV_MACHINE_GATEWAY_DB_PASSWORD)
selfhost_updater=$(example_value "$SCRIPT_DIR/.env.example" SYSTEM_UPDATER_TOKEN)
development_jwt=$(example_value "$SCRIPT_DIR/../.env.example" JWT_SECRET)
for value in "$selfhost_jwt" "$selfhost_postgres" "$selfhost_gateway" "$selfhost_updater" "$development_jwt"; do
	[ -n "$value" ] || fail "example env files no longer provide the expected placeholders"
done
generated=0123456789abcdef0123456789abcdef0123456789abcdef
gateway_url="postgres://kuayle_gateway"
gateway_url="$gateway_url:$selfhost_gateway"
gateway_url="$gateway_url@postgres:5432/kuayle"
run_placeholder_case unedited-example "$(cat "$SCRIPT_DIR/.env.example")\n" false JWT_SECRET
run_placeholder_case placeholder-jwt "POSTGRES_PASSWORD=$generated\nJWT_SECRET=$selfhost_jwt\n" false JWT_SECRET
run_placeholder_case quoted-development-jwt "ENVIRONMENT=production\nJWT_SECRET=\"$development_jwt\"\n" false JWT_SECRET
run_placeholder_case placeholder-database-password "POSTGRES_PASSWORD=$selfhost_postgres\nJWT_SECRET=$generated\n" false POSTGRES_PASSWORD
run_placeholder_case placeholder-updater-token "JWT_SECRET=$generated\nSYSTEM_UPDATER_TOKEN=$selfhost_updater\n" false SYSTEM_UPDATER_TOKEN
run_placeholder_case placeholder-gateway-password "JWT_SECRET=$generated\nDEV_MACHINE_GATEWAY_DB_PASSWORD=$selfhost_gateway\n" true DEV_MACHINE_GATEWAY_DB_PASSWORD
run_placeholder_case placeholder-gateway-url "JWT_SECRET=$generated\nDEV_MACHINE_GATEWAY_DATABASE_URL=$gateway_url\n" true DEV_MACHINE_GATEWAY_DATABASE_URL
# The example ships a gateway placeholder for a profile that is off by default.
run_placeholder_case unused-gateway-placeholder "JWT_SECRET=$generated\nPOSTGRES_PASSWORD=$generated\nDEV_MACHINE_GATEWAY_DB_PASSWORD=$selfhost_gateway\n" false ''
run_placeholder_case generated-values "JWT_SECRET=$generated\nPOSTGRES_PASSWORD=$generated\nDEV_MACHINE_GATEWAY_DB_PASSWORD=$generated\n# SYSTEM_UPDATER_TOKEN=$selfhost_updater\n" true ''
run_placeholder_case development-placeholders "ENVIRONMENT=development\nJWT_SECRET=$development_jwt\nPOSTGRES_PASSWORD=$selfhost_postgres\n" false ''

# name fail-point status control-plane-restore control-restore-fails backend-restore backend-restore-fails backend-held
run_case runtime-build-failure runtime-build 41 false
run_case stop-backend-failure stop-backend 46 true false true
run_case migration-failure migration 42 true false false false true
run_case provisioning-failure provision 43 true false false false true
run_case application-start-failure application-up 44 true false false false true
run_case control-plane-start-failure control-plane-up 45 true
run_case restoration-failure migration 42 true true false false true
run_case backend-restoration-failure stop-backend 46 true false true true
run_case success '' 0 false

printf 'self-host update recovery tests passed\n'

# Release deployments use a detached checkout and published image digests.
python3 -m unittest discover -s "$(dirname "$0")/tests" -v
