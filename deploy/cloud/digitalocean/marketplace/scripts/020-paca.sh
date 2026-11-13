#!/usr/bin/env bash
# Stage the first-boot installer. Nothing Paca-specific (secrets, IP, DB) is
# created here: every Droplet must generate its own on first boot.
set -euo pipefail
install -D -m 0755 /tmp/bootstrap.sh /opt/paca-bootstrap/bootstrap.sh
chmod 0755 /var/lib/cloud/scripts/per-instance/001_paca_firstboot.sh
chmod 0755 /etc/update-motd.d/99-one-click
rm -f /tmp/bootstrap.sh
