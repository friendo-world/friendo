#!/bin/bash
# Start the full Friendo local dev environment.
# Usage: bash dev.sh

set -e

cleanup() {
  echo ""
  echo "Shutting down..."
  kill $WORKER_PID $TUNNEL_PID $SERVE_PID 2>/dev/null
  wait $WORKER_PID $TUNNEL_PID $SERVE_PID 2>/dev/null
  echo "Done."
}
trap cleanup EXIT INT TERM

# Build the binary
echo "Building friendo..."
cd binary && go build -o ../bin/friendo ./cmd/friendo/ && cd ..

# Sync testsite to local D1/R2
echo "Syncing testsite to edge..."
cd world && bash sync.sh ../testsite 2>&1 | tail -1 && cd ..

# Start the Worker
echo "Starting Worker on :8787..."
cd world && npm run dev -- --port 8787 > /dev/null 2>&1 &
WORKER_PID=$!
cd ..
sleep 3

# Start the tunnel
echo "Starting Cloudflare tunnel..."
cloudflared tunnel run friendo-local > /dev/null 2>&1 &
TUNNEL_PID=$!

# Start the local binary
echo "Starting friendo serve on :3000..."
cd testsite && ../bin/friendo serve --open-admin > /dev/null 2>&1 &
SERVE_PID=$!
cd ..

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
