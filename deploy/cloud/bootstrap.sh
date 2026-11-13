#!/usr/bin/env bash
# Paca – first-boot bootstrap for a fresh Linux VM (one-click cloud deploys).
#
# Installs Docker, works out the VM's public address, then runs the official
# non-interactive installer (scripts/install.sh with PACA_YES=1). Used as
# user-data by the DigitalOcean Droplet and AWS CloudFormation one-click paths;
# it also works on any other Ubuntu/Debian cloud VM:
#
#   curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/bootstrap.sh | sudo bash
#
# Optional environment variables (all unset by default):
#   PACA_DOMAIN       Domain already pointed at this VM. Enables automatic
#                     HTTPS (Let's Encrypt). Without it Paca is served over
#                     plain HTTP on the VM's public IP.
#   ADMIN_PASSWORD    Admin password (min 8 chars). Generated when unset.
#   PACA_PUBLIC_IP    Public IP to use instead of auto-detecting it (set by the
#                     AWS template, whose Elastic IP is attached after boot).
#   PACA_VERSION      Release tag to install (default: latest).
#   PACA_AGENT_RUNNER yes/no (default: yes).
#   PACA_DIR          Install directory (default: /opt/paca).
#
# Progress and the generated admin credentials are written to
# /var/log/paca-install.log; the full configuration lives in $PACA_DIR/.env.
set -euo pipefail

LOG=/var/log/paca-install.log
exec > >(tee -a "$LOG") 2>&1

[[ $EUID -eq 0 ]] || { echo "bootstrap.sh must run as root" >&2; exit 1; }

PACA_DIR="${PACA_DIR:-/opt/paca}"
INSTALL_URL="${PACA_INSTALL_URL:-https://github.com/Paca-AI/paca/releases/latest/download/install.sh}"

echo "── Paca bootstrap started $(date -u +%FT%TZ)"

# ── Docker ────────────────────────────────────────────────────────────────────
if ! command -v docker >/dev/null 2>&1; then
    echo "Installing Docker…"
    export DEBIAN_FRONTEND=noninteractive
    for _ in 1 2 3 4 5; do
        curl -fsSL https://get.docker.com -o /tmp/get-docker.sh && break
        sleep 5
    done
    sh /tmp/get-docker.sh
fi
systemctl enable --now docker
# Fresh images can still be mid cloud-init apt run; wait for the compose plugin.
for _ in $(seq 1 30); do docker compose version >/dev/null 2>&1 && break; sleep 2; done
docker compose version >/dev/null

# ── Public address ────────────────────────────────────────────────────────────
detect_ip() {
    local ip="" token
    # DigitalOcean metadata
    ip=$(curl -fsS --max-time 3 http://169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address 2>/dev/null || true)
    # AWS IMDSv2
    if [[ -z "$ip" ]]; then
        token=$(curl -fsS --max-time 3 -X PUT http://169.254.169.254/latest/api/token \
            -H 'X-aws-ec2-metadata-token-ttl-seconds: 60' 2>/dev/null || true)
        if [[ -n "$token" ]]; then
            ip=$(curl -fsS --max-time 3 -H "X-aws-ec2-metadata-token: $token" \
                http://169.254.169.254/latest/meta-data/public-ipv4 2>/dev/null || true)
        fi
    fi
    # Anything else: ask an external echo service for this VM's own address.
    [[ -n "$ip" ]] || ip=$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)
    printf '%s' "$ip"
}

if [[ -n "${PACA_DOMAIN:-}" ]]; then
    export PACA_ADDRESS="$PACA_DOMAIN" PACA_HTTPS=yes
else
    PACA_ADDRESS="${PACA_PUBLIC_IP:-$(detect_ip)}"
    [[ -n "$PACA_ADDRESS" ]] || { echo "Could not detect the public IP; set PACA_DOMAIN" >&2; exit 1; }
    # An IP can't get a public certificate; plain HTTP is the honest default.
    export PACA_ADDRESS PACA_HTTPS=no
fi
echo "Paca will be served at ${PACA_ADDRESS}"

# ── Install ───────────────────────────────────────────────────────────────────
mkdir -p "$PACA_DIR"
curl -fsSL --retry 5 "$INSTALL_URL" -o /tmp/paca-install.sh
export PACA_YES=1 PACA_DIR
bash /tmp/paca-install.sh
chmod 600 "$PACA_DIR/.env" 2>/dev/null || true

scheme=http; [[ "$PACA_HTTPS" == yes ]] && scheme=https
echo "── Paca is up. Open ${scheme}://${PACA_ADDRESS}"
echo "── Admin credentials: grep ADMIN_ $PACA_DIR/.env"
