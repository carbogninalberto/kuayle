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

# ttyd sets TERM from --terminal-type; tmux reads it to detect the outer terminal features.
export TERM="${TERM:-xterm-256color}"
export COLORTERM=truecolor
# Kuayle names a task's Claude Code session
# ktask-<8 hex>-<task>[-<native session>]: it opens Claude Code on the task
# instead of a shell. Reopening it attaches to the same Claude Code.
case "$session" in
  ktask-* )
    task_session=${session#ktask-}
    case "$task_session" in
      [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-* ) task_session=${task_session#*-} ;;
      * ) echo "invalid terminal session" >&2; exit 1 ;;
    esac
    task=$(printf '%s' "$task_session" | cut -c1-36)
    native=$(printf '%s' "$task_session" | cut -c38-)
    uuid_pattern='[0-9a-f]\{8\}-[0-9a-f]\{4\}-[0-9a-f]\{4\}-[0-9a-f]\{4\}-[0-9a-f]\{12\}'
    if ! printf '%s' "$task" | grep -qx "$uuid_pattern" || { [ -n "$native" ] && ! printf '%s' "$native" | grep -qx "$uuid_pattern"; }; then
      echo "invalid terminal session" >&2
      exit 1
    fi
    case "$task_session" in
      "$task" | "$task-$native" ) ;;
      * ) echo "invalid terminal session" >&2; exit 1 ;;
    esac
    exec tmux -u new-session -A -s "$session" -c "$workspace" "${KUAYLE_TASK_CLAUDE:-/usr/local/bin/kuayle-task-claude} $task $native"
    ;;
esac
exec tmux -u new-session -A -s "$session" -c "$workspace"
