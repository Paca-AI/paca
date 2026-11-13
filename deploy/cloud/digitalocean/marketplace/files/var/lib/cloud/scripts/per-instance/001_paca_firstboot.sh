#!/usr/bin/env bash
# Runs once per new Droplet (cloud-init per-instance). Installs Paca with
# generated secrets and this Droplet's own public IP.
set -euo pipefail
# A domain can be supplied through Droplet user-data env vars, e.g.
#   #!/bin/bash
#   echo 'PACA_DOMAIN=paca.example.com' >> /etc/paca-bootstrap.env
[ -f /etc/paca-bootstrap.env ] && set -a && . /etc/paca-bootstrap.env && set +a
exec /opt/paca-bootstrap/bootstrap.sh
