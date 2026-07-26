#!/bin/bash
# Start a local friendo network for development — the same binary friendo.world
# runs, in network mode, so you can create sites by subdomain and use the operator
# console. Usage: bash dev.sh   (or: npm run dev)
set -e

# Free the port we need.
lsof -ti :3000 | xargs kill 2>/dev/null || true

# Build the admin SPA + SDK (embedded via go:embed) and the binary.
echo "Building friendo (admin SPA + SDK + binary)..."
npm run admin || { echo "Admin build failed"; exit 1; }
npm run sdk   || { echo "SDK build failed"; exit 1; }
go build -o bin/friendo ./cli/cmd/friendo/ || { echo "Go build failed"; exit 1; }

echo ""
echo "============================================"
echo "  friendo network (dev) on http://localhost:3000"
echo ""
echo "  operator console:  http://localhost:3000/"
echo "  a site:            http://<subdomain>.localhost:3000/"
echo "  (dev OTP codes are echoed on the sign-in page)"
echo "============================================"
echo ""
echo "Press Ctrl+C to stop."

# Run the network in the foreground. Dev conveniences: echo OTP codes, and make the
# first console sign-in the operator (no FRIENDO_OPERATOR_EMAIL needed locally).
exec env FRIENDO_OTP_ECHO=1 ./bin/friendo network serve \
  --root ./network --base-domain localhost --port 3000
