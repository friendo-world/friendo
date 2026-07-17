#!/usr/bin/env bash
#
# Provision a Hetzner Cloud box + firewall and install Coolify on it.
# This is DEPLOY.md "Option B" (self-hosting the Coolify control plane).
#
# You do NOT need this if you use Coolify Cloud + the Hetzner integration ("Option A") —
# there, Coolify provisions and connects the server for you from its dashboard.
#
# Prereqs: the hcloud CLI, installed and authenticated:
#   brew install hcloud
#   hcloud context create friendo      # paste a Read & Write API token for your project
#
# Usage:
#   ./scripts/provision-hetzner.sh                 # uses the defaults below
#   SERVER_TYPE=cpx31 LOCATION=hil ./scripts/provision-hetzner.sh
#
set -euo pipefail

# --- config (override via env) ---
SERVER_NAME="${SERVER_NAME:-friendo}"
SERVER_TYPE="${SERVER_TYPE:-cpx21}"          # 4 GB starter; cpx31 = 8 GB. US = cpx/ccx only.
LOCATION="${LOCATION:-ash}"                  # ash = Ashburn VA, hil = Hillsboro OR
IMAGE="${IMAGE:-ubuntu-24.04}"
SSH_KEY_NAME="${SSH_KEY_NAME:-friendo-key}"
SSH_PUBKEY="${SSH_PUBKEY:-$HOME/.ssh/id_ed25519.pub}"
FIREWALL_NAME="${FIREWALL_NAME:-friendo-fw}"
MY_IP="${MY_IP:-$(curl -fsS4 ifconfig.me)}"  # your public IP — opens SSH + the dashboard to you only

echo "→ Provisioning '${SERVER_NAME}' (${SERVER_TYPE} @ ${LOCATION}); your IP: ${MY_IP}"

# --- SSH key (idempotent) ---
hcloud ssh-key describe "$SSH_KEY_NAME" >/dev/null 2>&1 \
  || hcloud ssh-key create --name "$SSH_KEY_NAME" --public-key-from-file "$SSH_PUBKEY"

# --- Firewall: SSH + Coolify dashboard (8000) from your IP; HTTP/HTTPS from anywhere; rest denied.
# Hetzner firewalls are network-edge, so they reliably block Docker-published ports (unlike UFW).
hcloud firewall describe "$FIREWALL_NAME" >/dev/null 2>&1 \
  || hcloud firewall create --name "$FIREWALL_NAME"
hcloud firewall add-rule "$FIREWALL_NAME" --direction in --protocol tcp --port 22   --source-ips "${MY_IP}/32"               2>/dev/null || true
hcloud firewall add-rule "$FIREWALL_NAME" --direction in --protocol tcp --port 8000 --source-ips "${MY_IP}/32"               2>/dev/null || true
hcloud firewall add-rule "$FIREWALL_NAME" --direction in --protocol tcp --port 80   --source-ips 0.0.0.0/0 --source-ips ::/0 2>/dev/null || true
hcloud firewall add-rule "$FIREWALL_NAME" --direction in --protocol tcp --port 443  --source-ips 0.0.0.0/0 --source-ips ::/0 2>/dev/null || true

# --- cloud-init: add swap (Coolify builds can OOM at 4 GB) + install Coolify at first boot.
CLOUD_INIT="$(mktemp)"
trap 'rm -f "$CLOUD_INIT"' EXIT
cat > "$CLOUD_INIT" <<'CLOUDINIT'
#cloud-config
package_update: true
packages:
  - curl
runcmd:
  - sh -c 'if [ ! -f /swapfile ]; then fallocate -l 4G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo "/swapfile none swap sw 0 0" >> /etc/fstab; fi'
  - bash -c 'curl -fsSL https://cdn.coollabs.io/coolify/install.sh | bash'
CLOUDINIT

# --- Create the server (firewall enforced from first boot; --user-data-from-file is the correct flag).
hcloud server create \
  --name "$SERVER_NAME" \
  --image "$IMAGE" \
  --type "$SERVER_TYPE" \
  --location "$LOCATION" \
  --ssh-key "$SSH_KEY_NAME" \
  --firewall "$FIREWALL_NAME" \
  --user-data-from-file "$CLOUD_INIT"

IP="$(hcloud server ip "$SERVER_NAME")"
cat <<DONE

✓ ${SERVER_NAME} is up at ${IP}. Coolify installs in the background (~3–5 min).
  Dashboard:  http://${IP}:8000   (open only from your IP ${MY_IP} — update the rule if your IP changes)
  Next: DEPLOY.md §2 (Cloudflare) → §3 (the friendo app).
DONE
