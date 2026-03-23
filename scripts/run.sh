#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

echo "=== Kuayle - Run ==="

# Start services (postgres, redis, backend, frontend)
./scripts/start.sh &
START_PID=$!

# Wait for start.sh to finish or be interrupted
trap 'echo ""; echo "Shutting down..."; ./scripts/stop.sh; exit 0' INT TERM

wait $START_PID
