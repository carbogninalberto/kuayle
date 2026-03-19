#!/usr/bin/env bash

PIDFILE=".dev.pids"

echo "=== Carbon - Stopping ==="

# Kill dev processes
if [ -f "$PIDFILE" ]; then
    while read -r pid; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null && echo "Stopped process $pid"
        fi
    done < "$PIDFILE"
    rm -f "$PIDFILE"
fi

# Kill any leftover air/vite processes
pkill -f "air" 2>/dev/null && echo "Stopped air" || true
pkill -f "vite.*carbon" 2>/dev/null && echo "Stopped vite" || true

# Stop Docker services
docker compose stop 2>/dev/null && echo "Stopped Postgres and Redis" || true

echo "Done."
