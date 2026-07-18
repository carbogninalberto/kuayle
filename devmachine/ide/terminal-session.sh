#!/bin/sh
set -eu

session=${1:-workspace}
workspace=${2:-/workspace/tasks}
case "$session" in
  /workspace|/workspace/tasks|/workspace/tasks/* )
    workspace=$session
    session=$(printf '%s' "$workspace" | tr '/.' '__' | tr -cd 'A-Za-z0-9_-')
    ;;
esac
case "$session" in
  ''|*[!A-Za-z0-9_-]* ) echo "invalid terminal session" >&2; exit 1 ;;
esac
case "$workspace" in
  /workspace|/workspace/tasks ) ;;
  /workspace/tasks/* )
    case "$workspace" in *..*|*//*|/workspace/tasks/*/* ) echo "invalid terminal workspace" >&2; exit 1 ;; esac
    workspace_name=${workspace#/workspace/tasks/}
    case "$workspace_name" in ''|.|..|*[!A-Za-z0-9._-]* ) echo "invalid terminal workspace" >&2; exit 1 ;; esac
    ;;
  * ) echo "invalid terminal workspace" >&2; exit 1 ;;
esac
if [ ! -d "$workspace" ]; then
  workspace=/workspace/tasks
fi

exec tmux new-session -A -s "$session" -c "$workspace"
