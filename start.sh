#!/usr/bin/env bash
set -e

PIDFILE=".dev.pids"

echo "=== Kuayle - Dev Setup ==="

# 1. Create .env if missing
if [ ! -f .env ]; then
    echo "Creating .env from .env.example..."
    cp .env.example .env
fi

# Load env vars
set -a && source .env && set +a

# 2. Start Postgres and Redis
echo "Starting Postgres and Redis..."
docker compose up postgres redis -d --wait

# 3. Install dependencies
echo "Installing backend dependencies..."
cd BE && go mod download && cd ..

if [ ! -d UI/node_modules ]; then
    echo "Installing frontend dependencies..."
    cd UI && npm install && cd ..
fi

# 4. Install air if missing
if ! command -v air &>/dev/null; then
    echo "Installing air (Go hot reload)..."
    go install github.com/air-verse/air@latest
fi

# 5. Run migrations
echo "Applying database migrations..."
make migrate-up

# 6. Clean up any previous dev pids
[ -f "$PIDFILE" ] && ./stop.sh 2>/dev/null || true

echo ""
echo "=== Starting Kuayle (dev mode) ==="
echo "  Frontend: http://localhost:5173"
echo "  Backend:  http://localhost:8080 (air hot reload)"
echo "  Stop:     ./stop.sh"
echo ""

# 7. Start backend with air
(cd BE && air) &
BE_PID=$!

# 8. Start frontend dev server
(cd UI && npm run dev) &
FE_PID=$!

# Save pids for stop script
echo "$BE_PID" > "$PIDFILE"
echo "$FE_PID" >> "$PIDFILE"

# Wait for either to exit
trap './stop.sh 2>/dev/null; exit 0' INT TERM
wait
