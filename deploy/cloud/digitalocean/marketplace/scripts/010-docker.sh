#!/usr/bin/env bash
# Install Docker + the compose plugin and a firewall that allows web + SSH.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

apt-get -qq update
apt-get -qq -y upgrade
apt-get -qq -y install curl ca-certificates ufw
curl -fsSL https://get.docker.com | sh
systemctl enable docker
docker compose version

ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable
