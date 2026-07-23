#!/bin/sh
set -eu

operation=start
if [ "${1:-}" = "--close" ]; then
  operation=close
  shift
fi
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
if [ "$operation" = close ]; then
  if ! tmux has-session -t "$session" 2>/dev/null; then
    exit 0
  fi
  if ! tmux kill-session -t "$session" 2>/dev/null && tmux has-session -t "$session" 2>/dev/null; then
    echo "failed to close terminal session" >&2
    exit 1
  fi
  exit 0
fi
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
