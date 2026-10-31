#!/bin/sh
set -eu

if [ ! -t 0 ]; then
  exec /bin/bash "$@"
fi

# The machine owner's agent sign-ins from Kuayle, for this shell.
if [ -r "${KUAYLE_AGENT_SIGN_IN:-/usr/local/lib/kuayle/kuayle-agent-sign-in.sh}" ]; then
  . "${KUAYLE_AGENT_SIGN_IN:-/usr/local/lib/kuayle/kuayle-agent-sign-in.sh}" || true
fi
export HISTCONTROL=ignoredups
export PROMPT_COMMAND='kuayle_status=$?; kuayle_command=$(history 1); KUAYLE_EXIT_CODE=$kuayle_status KUAYLE_COMMAND=$kuayle_command node /usr/local/lib/kuayle-command-event.js >/dev/null 2>&1 || true; history -a'
exec /bin/bash "$@"
