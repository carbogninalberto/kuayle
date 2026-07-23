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
	*" ps --status running --services "*)
		printf '%s\n' machine-gateway machine-manager
		exit 0
		;;
	esac
	case "${FAIL_POINT:-}" in
	runtime-build)
		case " $* " in *" --profile dev-machine-images build --pull "*) exit 41 ;; esac
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
	control-plane-up)
		case " $* " in *" --profile dev-machines up -d machine-gateway machine-manager "*) exit 45 ;; esac
		;;
	esac
	if [ "${FAIL_RESTORE:-false}" = true ]; then
		case " $* " in *" start machine-gateway machine-manager "*) exit 99 ;; esac
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
	output=$(PATH="$fake_bin:$PATH" COMMAND_LOG="$command_log" FAIL_POINT="$fail_point" FAIL_RESTORE="$fail_restore" \
		bash "$repo_root/selfhosting/update.sh" 2>&1)
	status=$?
	set -e

	[ "$status" -eq "$expected_status" ] || fail "$name returned $status, expected $expected_status: $output"
	[ ! -e "$repo_root/selfhosting/runtime/upgrading" ] || fail "$name left the upgrade marker"
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

	rm -rf "$test_root"
}

run_case runtime-build-failure runtime-build 41 false
run_case migration-failure migration 42 true
run_case provisioning-failure provision 43 true
run_case application-start-failure application-up 44 true
run_case control-plane-start-failure control-plane-up 45 true
run_case restoration-failure migration 42 true true
run_case success '' 0 false

printf 'self-host update recovery tests passed\n'
