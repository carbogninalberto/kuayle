#!/bin/sh
# Fails when a built-in agent CLI's help no longer lists a flag or a fixed
# value that Kuayle passes it. The images run it when they are built and in
# their smoke tests, so a pin raise that drops a flag fails before it ships.
# The machine manager runs the headless image's copy on the shared agent CLI
# volume (BE/internal/machine/agent_clis.go), so a refreshed CLI, or one a
# previous release installed, that drops a flag is never put on PATH.
#
# The list is the contract: one help command per line, then the flags and
# values its output must contain. TestAgentCLIFlagContractMatchesTheAdapters
# (BE/internal/agent) keeps it equal to the flags BE/internal/agent/providers.go
# builds; change both together.
#
# Usage: check-agent-cli-flags.sh [CLI...]   (every CLI when none is named)
set -eu

contract() {
  cat <<'FLAGS'
claude --help: --print -p --output-format stream-json --verbose --permission-mode bypassPermissions --model --resume --session-id
opencode run --help: --format json --auto --print-logs --log-level warn --standalone --model --session
codex --help: -c
codex exec --help: --json -c
codex exec resume --help: --json -c
qwen --help: --yolo --output-format stream-json --resume
crush run --help: --yolo --quiet
kimi --help: --print --yolo --output-format stream-json --prompt --session
goose --help: session
goose run --help: --no-session --quiet --output-format stream-json --text --name --resume
FLAGS
}

# Help commands write configuration and caches; keep them out of the image.
scratch=$(mktemp -d "${TMPDIR:-/tmp}/kuayle-cli-flags.XXXXXX")
trap 'rm -rf "$scratch"' EXIT INT TERM

contract | {
  status=0
  while IFS= read -r line; do
    command=${line%%:*}
    cli=${command%% *}
    if [ "$#" -gt 0 ]; then
      case " $* " in *" $cli "*) ;; *) continue ;; esac
    fi
    # A wide terminal keeps tables (Kimi's) from truncating option names.
    # shellcheck disable=SC2086
    help=$(HOME="$scratch" XDG_CONFIG_HOME="$scratch/config" XDG_DATA_HOME="$scratch/data" \
      XDG_STATE_HOME="$scratch/state" XDG_CACHE_HOME="$scratch/cache" COLUMNS=250 $command 2>&1 </dev/null || true)
    for token in ${line#*:}; do
      if ! printf '%s\n' "$help" | grep -Eq -- "(^|[^[:alnum:]_-])${token}([^[:alnum:]_-]|\$)"; then
        echo "${command}: missing ${token}" >&2
        status=1
      fi
    done
  done
  exit "$status"
}
