#!/usr/bin/env bash
# Deploy Paca to a Google Compute Engine VM (full stack, incl. Agent Runner).
# Meant for Google Cloud Shell, which is already logged in:
#   bash deploy/cloud/gcp/deploy.sh
#
# Optional environment variables:
#   PACA_NAME        VM / address name prefix        (default: paca)
#   PACA_ZONE        Compute Engine zone             (default: us-central1-a)
#   PACA_MACHINE     Machine type                    (default: e2-standard-2, 8 GB)
#   PACA_DOMAIN      Domain whose A record you will point at the reserved IP (HTTPS)
#   ADMIN_PASSWORD   Admin password (min 8 chars; generated when unset). Stored in
#                    instance metadata, readable by project viewers.
set -euo pipefail

NAME="${PACA_NAME:-paca}"
ZONE="${PACA_ZONE:-us-central1-a}"
MACHINE="${PACA_MACHINE:-e2-standard-2}"
REGION="${ZONE%-*}"
PROJECT="$(gcloud config get-value project 2>/dev/null || true)"
[[ -n "$PROJECT" && "$PROJECT" != "(unset)" ]] || {
    echo "No project selected. Run: gcloud config set project <PROJECT_ID>" >&2; exit 1; }

echo "Deploying Paca to project ${PROJECT}, zone ${ZONE} (${MACHINE})"
gcloud services enable compute.googleapis.com

gcloud compute addresses create "${NAME}-ip" --region "$REGION"
IP="$(gcloud compute addresses describe "${NAME}-ip" --region "$REGION" --format='value(address)')"

gcloud compute firewall-rules create "${NAME}-web" \
    --allow tcp:80,tcp:443 --target-tags "$NAME"

STARTUP="$(mktemp)"
trap 'rm -f "$STARTUP"' EXIT
cat > "$STARTUP" <<SCRIPT
#!/usr/bin/env bash
# Runs on every boot; only install once.
[ -f /opt/paca/.env ] && exit 0
export PACA_PUBLIC_IP='${IP}'
export PACA_DOMAIN='${PACA_DOMAIN:-}'
export ADMIN_PASSWORD='${ADMIN_PASSWORD:-}'
[ -n "\$PACA_DOMAIN" ] || unset PACA_DOMAIN
[ -n "\$ADMIN_PASSWORD" ] || unset ADMIN_PASSWORD
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/bootstrap.sh | bash
SCRIPT

gcloud compute instances create "$NAME" \
    --zone "$ZONE" --machine-type "$MACHINE" \
    --image-family ubuntu-2404-lts-amd64 --image-project ubuntu-os-cloud \
    --boot-disk-size 40GB --boot-disk-type pd-balanced \
    --address "${NAME}-ip" --tags "$NAME" \
    --metadata-from-file startup-script="$STARTUP"

cat <<MSG

Paca is installing (about 5 minutes: it pulls the images on first boot).
  URL:       ${PACA_DOMAIN:+https://${PACA_DOMAIN} (point its DNS A record at ${IP}) or }http://${IP}
  Progress:  gcloud compute ssh ${NAME} --zone ${ZONE} -- sudo tail -f /var/log/paca-install.log
  Admin:     gcloud compute ssh ${NAME} --zone ${ZONE} -- sudo grep ADMIN_ /opt/paca/.env
MSG
