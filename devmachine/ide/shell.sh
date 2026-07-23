#!/bin/sh
set -eu

if [ ! -t 0 ]; then
  exec /bin/bash "$@"
fi

export HISTCONTROL=ignoredups
export PROMPT_COMMAND='kuayle_status=$?; kuayle_command=$(history 1); KUAYLE_EXIT_CODE=$kuayle_status KUAYLE_COMMAND=$kuayle_command node /usr/local/lib/kuayle-command-event.js >/dev/null 2>&1 || true; history -a'
exec /bin/bash "$@"
