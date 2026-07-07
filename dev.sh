#!/bin/bash
# Start the full Friendo local dev environment.
# Usage: bash dev.sh

# Kill anything already on ports we need.
lsof -ti :3000 | xargs kill 2>/dev/null || true
lsof -ti :8787 | xargs kill 2>/dev/null || true

cleanup() {
  echo ""
  echo "Shutting down..."
  [ -n "$SERVE_PID" ] && kill $SERVE_PID 2>/dev/null
  [ -n "$WORKER_PID" ] && kill $WORKER_PID 2>/dev/null
  [ -n "$TUNNEL_PID" ] && kill $TUNNEL_PID 2>/dev/null
  wait 2>/dev/null
  echo "Done."
}
trap cleanup EXIT INT TERM

# Build admin SPA + binary (this must succeed)
echo "Building friendo..."
npm run admin || { echo "Admin build failed"; exit 1; }
go build -o bin/friendo ./cli/cmd/friendo/ || { echo "Go build failed"; exit 1; }

# Note: to seed a site on the edge, deploy it through the site API:
#   cd testsite && ../bin/friendo deploy   (or `friendo push`)

ROOT="$(pwd)"

# Start the Worker (non-fatal)
WORKER_PID=""
echo "Starting Worker on :8787..."
(cd "$ROOT/platform" && npm run dev -- --port 8787 > /dev/null 2>&1) &
WORKER_PID=$!
sleep 3

# Start the tunnel (non-fatal if cloudflared isn't installed)
TUNNEL_PID=""
echo "Starting Cloudflare tunnel..."
if command -v cloudflared &> /dev/null; then
  cloudflared tunnel run friendo-local > /dev/null 2>&1 &
  TUNNEL_PID=$!
else
  echo "  Skipped (cloudflared not installed)"
fi

# Start the local binary
SERVE_PID=""
echo "Starting friendo serve on :3000..."
(cd "$ROOT/testsite" && "$ROOT/bin/friendo" serve --open-admin > /dev/null 2>&1) &
SERVE_PID=$!

echo ""
echo "============================================"
echo "  Friendo dev environment running"
echo ""
echo "  Local binary:  http://localhost:3000"
echo "  Admin UI:      http://localhost:3000/_/"
echo "  Edge (HTTPS):  https://local.friendo.world"
echo "  Your site:     https://testsite.local.friendo.world"
echo "============================================"
echo ""
echo "Press Ctrl+C to stop all services."

# Wait for any child to exit
wait
