#!/usr/bin/env bash
# PicoClaw web development launcher
# Builds picoclaw binary, then starts backend + frontend in parallel.
# Ctrl+C stops both.

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONFIG="${1:-$HOME/.picoclaw/config.json}"

echo "==> Building picoclaw binary..."
(cd "$ROOT" && go generate ./cmd/picoclaw/internal/onboard/ 2>/dev/null || true)
go build -o "$ROOT/web/.bin/picoclaw" "$ROOT/cmd/picoclaw/"
export PATH="$ROOT/web/.bin:$PATH"

echo "==> picoclaw binary ready: $ROOT/web/.bin/picoclaw"
echo "==> Config: $CONFIG"
echo "==> Starting backend (:18800) + frontend (:5173)..."
echo ""

cleanup() {
    echo ""
    echo "==> Stopping..."
    kill $PID_BACKEND $PID_FRONTEND 2>/dev/null || true
    wait $PID_BACKEND $PID_FRONTEND 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Backend
(cd "$ROOT/web/backend" && go run . --no-browser "$CONFIG") &
PID_BACKEND=$!

# Frontend
(cd "$ROOT/web/frontend" && pnpm dev --host 2>/dev/null || npx vite --host) &
PID_FRONTEND=$!

# Decide which URL to open based on config existence
if [ -f "$CONFIG" ]; then
    OPEN_URL="http://localhost:5173"
else
    OPEN_URL="http://localhost:5173/onboard"
fi

echo "  Backend  → http://localhost:18800"
echo "  Frontend → $OPEN_URL"
echo ""
echo "  Press Ctrl+C to stop both."
echo ""

# Auto-open browser once frontend is ready
(
    for i in $(seq 1 30); do
        if curl -s -o /dev/null http://localhost:5173 2>/dev/null; then
            open "$OPEN_URL" 2>/dev/null || xdg-open "$OPEN_URL" 2>/dev/null || true
            break
        fi
        sleep 1
    done
) &

wait
