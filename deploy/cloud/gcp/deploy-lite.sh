#!/usr/bin/env bash
# Deploy Paca (lightweight) on managed Google Cloud services. No VM to patch:
#   Cloud Run  one service with four containers (gateway + API + web + realtime)
#   Cloud SQL  Postgres        Memorystore  Redis        Cloud Storage  files
# Does NOT include the Agent Runner (AI agent sandboxes); use deploy.sh for that.
#
# Meant for Google Cloud Shell (already logged in):
#   bash deploy/cloud/gcp/deploy-lite.sh
# Safe to re-run: existing resources and secrets are reused.
#
# Optional environment variables:
#   PACA_NAME     Name prefix                    (default: paca)
#   PACA_REGION   Region                         (default: us-central1)
#   PACA_VERSION  Image tag for the Paca images  (default: latest)
set -euo pipefail

NAME="${PACA_NAME:-paca}"
REGION="${PACA_REGION:-us-central1}"
TAG="${PACA_VERSION:-latest}"
PROJECT="$(gcloud config get-value project 2>/dev/null || true)"
[[ -n "$PROJECT" && "$PROJECT" != "(unset)" ]] || {
    echo "No project selected. Run: gcloud config set project <PROJECT_ID>" >&2; exit 1; }
PROJECT_NUMBER="$(gcloud projects describe "$PROJECT" --format='value(projectNumber)')"
SA="${NAME}-run@${PROJECT}.iam.gserviceaccount.com"
BUCKET="${PROJECT}-${NAME}-files"
SQL="${NAME}-db"
REDIS="${NAME}-cache"
URL="https://${NAME}-${PROJECT_NUMBER}.${REGION}.run.app"
AR="${REGION}-docker.pkg.dev/${PROJECT}/${NAME}-dockerhub"

exists() { "$@" >/dev/null 2>&1; }
rand()   { openssl rand -hex "$1"; }

echo "Deploying Paca (lightweight) to project ${PROJECT}, region ${REGION}"
gcloud services enable run.googleapis.com sqladmin.googleapis.com redis.googleapis.com \
    secretmanager.googleapis.com artifactregistry.googleapis.com compute.googleapis.com \
    storage.googleapis.com iam.googleapis.com
exists gcloud compute networks describe default ||
    { echo "This project has no 'default' VPC network; create one or use another project." >&2; exit 1; }

# ── Service account ───────────────────────────────────────────────────────────
exists gcloud iam service-accounts describe "$SA" ||
    gcloud iam service-accounts create "${NAME}-run" --display-name "Paca Cloud Run"
for role in roles/cloudsql.client roles/secretmanager.secretAccessor; do
    gcloud projects add-iam-policy-binding "$PROJECT" --member "serviceAccount:${SA}" \
        --role "$role" --condition=None >/dev/null
done

# ── Docker Hub images through an Artifact Registry remote repo ────────────────
exists gcloud artifacts repositories describe "${NAME}-dockerhub" --location "$REGION" ||
    gcloud artifacts repositories create "${NAME}-dockerhub" --location "$REGION" \
        --repository-format docker --mode remote-repository --remote-docker-repo DOCKER-HUB

# ── Postgres ──────────────────────────────────────────────────────────────────
if ! exists gcloud sql instances describe "$SQL"; then
    echo "Creating Cloud SQL (about 10 minutes)…"
    gcloud sql instances create "$SQL" --database-version POSTGRES_16 --edition ENTERPRISE \
        --tier db-f1-micro --region "$REGION" --storage-size 10 --backup
fi
exists gcloud sql databases describe paca --instance "$SQL" ||
    gcloud sql databases create paca --instance "$SQL"

# ── Secrets (created once, reused on re-run) ──────────────────────────────────
secret() { # secret <name> <value-command…>; creates only when missing
    local n="${NAME}-$1"; shift
    exists gcloud secrets describe "$n" ||
        { "$@" | gcloud secrets create "$n" --data-file=- --replication-policy=automatic >/dev/null; }
}
secret db-password printf '%s' "$(rand 16)"
secret jwt printf '%s' "$(rand 32)"
secret admin-password printf '%s' "$(rand 12)"
secret internal-key printf '%s' "$(rand 32)"
secret encryption-key printf '%s' "$(rand 32)"   # 64 hex chars, as Paca requires
DB_PASSWORD="$(gcloud secrets versions access latest --secret "${NAME}-db-password")"
exists gcloud sql users describe paca --instance "$SQL" &&
    gcloud sql users set-password paca --instance "$SQL" --password "$DB_PASSWORD" ||
    gcloud sql users create paca --instance "$SQL" --password "$DB_PASSWORD"
secret database-url printf 'postgres://paca:%s@/paca?host=/cloudsql/%s:%s:%s&sslmode=disable' \
    "$DB_PASSWORD" "$PROJECT" "$REGION" "$SQL"

# ── Redis ─────────────────────────────────────────────────────────────────────
if ! exists gcloud redis instances describe "$REDIS" --region "$REGION"; then
    echo "Creating Memorystore (about 5 minutes)…"
    gcloud redis instances create "$REDIS" --region "$REGION" --size 1 --tier basic \
        --redis-version redis_7_2 --network default
fi
REDIS_URL="redis://$(gcloud redis instances describe "$REDIS" --region "$REGION" --format='value(host)'):6379/0"

# ── Storage ───────────────────────────────────────────────────────────────────
exists gcloud storage buckets describe "gs://${BUCKET}" ||
    gcloud storage buckets create "gs://${BUCKET}" --location "$REGION" --uniform-bucket-level-access
gcloud storage buckets add-iam-policy-binding "gs://${BUCKET}" \
    --member "serviceAccount:${SA}" --role roles/storage.objectAdmin >/dev/null
CORS="$(mktemp)"; trap 'rm -f "$CORS"' EXIT
cat > "$CORS" <<JSON
[{"origin": ["${URL}"], "method": ["GET", "PUT", "HEAD"], "responseHeader": ["*"], "maxAgeSeconds": 3000}]
JSON
gcloud storage buckets update "gs://${BUCKET}" --cors-file "$CORS" >/dev/null
if ! exists gcloud secrets describe "${NAME}-storage-key-id"; then
    HMAC="$(gcloud storage hmac create "$SA" --format=json)"
    printf '%s' "$HMAC" | python3 -c 'import json,sys;print(json.load(sys.stdin)["metadata"]["accessId"],end="")' |
        gcloud secrets create "${NAME}-storage-key-id" --data-file=- >/dev/null
    printf '%s' "$HMAC" | python3 -c 'import json,sys;print(json.load(sys.stdin)["secret"],end="")' |
        gcloud secrets create "${NAME}-storage-key-secret" --data-file=- >/dev/null
fi

# ── Cloud Run service ─────────────────────────────────────────────────────────
sec() { printf '{valueFrom: {secretKeyRef: {name: %s-%s, key: latest}}}' "$NAME" "$1"; }
SPEC="$(mktemp)"; trap 'rm -f "$CORS" "$SPEC"' EXIT
cat > "$SPEC" <<YAML
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: ${NAME}
  annotations:
    run.googleapis.com/ingress: all
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/minScale: "1"
        autoscaling.knative.dev/maxScale: "1"
        run.googleapis.com/cpu-throttling: "false"
        run.googleapis.com/execution-environment: gen2
        run.googleapis.com/cloudsql-instances: ${PROJECT}:${REGION}:${SQL}
        run.googleapis.com/network-interfaces: '[{"network":"default","subnetwork":"default"}]'
        run.googleapis.com/vpc-access-egress: private-ranges-only
    spec:
      serviceAccountName: ${SA}
      timeoutSeconds: 3600
      volumes:
        - {name: plugins, emptyDir: {}}
        - {name: plugins-frontend, emptyDir: {}}
        - {name: plugins-mcp, emptyDir: {}}
        - {name: plugins-skills, emptyDir: {}}
      containers:
        # Containers share localhost. The API listens on 8081 because the
        # gateway's internal plugin-bundle block already uses 8080.
        - name: gateway
          image: ${AR}/library/caddy:2-alpine
          ports: [{containerPort: 80}]
          command: [sh, -c]
          args:
            - wget -qO /etc/caddy/Caddyfile https://github.com/Paca-AI/paca/releases/latest/download/Caddyfile && exec caddy run --config /etc/caddy/Caddyfile
          env:
            - {name: SITE_ADDRESS, value: ":80"}
            - {name: API_UPSTREAM, value: "localhost:8081"}
            - {name: WEB_UPSTREAM, value: "localhost:3000"}
            - {name: REALTIME_UPSTREAM, value: "localhost:3001"}
          volumeMounts:
            - {name: plugins-frontend, mountPath: /var/www/plugins}
            - {name: plugins-mcp, mountPath: /var/www/plugins-mcp}
            - {name: plugins-skills, mountPath: /var/www/plugins-skills}
        - name: api
          image: ${AR}/pacaai/paca-api:${TAG}
          env:
            - {name: ENV, value: production}
            - {name: PORT, value: "8081"}
            - {name: REDIS_URL, value: "${REDIS_URL}"}
            - {name: PUBLIC_URL, value: "${URL}"}
            - {name: COOKIE_SECURE, value: "true"}
            - {name: TRUSTED_PROXY_HOPS, value: "2"}
            - {name: ADMIN_USERNAME, value: admin}
            - {name: STORAGE_PROVIDER, value: s3}
            - {name: STORAGE_ENDPOINT, value: storage.googleapis.com}
            - {name: STORAGE_REGION, value: auto}
            - {name: STORAGE_USE_SSL, value: "true"}
            - {name: STORAGE_BUCKET, value: "${BUCKET}"}
            - {name: PLUGINS_WASM_DIR, value: /plugins}
            - {name: PLUGINS_FRONTEND_DIR, value: /plugins-frontend}
            - {name: PLUGINS_MCP_DIR, value: /plugins-mcp}
            - {name: PLUGINS_SKILLS_DIR, value: /plugins-skills}
            - {name: DATABASE_URL, $(sec database-url | sed 's/^{//;s/}$//')}
            - {name: JWT_SECRET, $(sec jwt | sed 's/^{//;s/}$//')}
            - {name: ADMIN_PASSWORD, $(sec admin-password | sed 's/^{//;s/}$//')}
            - {name: AI_AGENT_INTERNAL_KEY, $(sec internal-key | sed 's/^{//;s/}$//')}
            - {name: ENCRYPTION_KEY, $(sec encryption-key | sed 's/^{//;s/}$//')}
            - {name: STORAGE_ACCESS_KEY_ID, $(sec storage-key-id | sed 's/^{//;s/}$//')}
            - {name: STORAGE_SECRET_ACCESS_KEY, $(sec storage-key-secret | sed 's/^{//;s/}$//')}
          volumeMounts:
            - {name: plugins, mountPath: /plugins}
            - {name: plugins-frontend, mountPath: /plugins-frontend}
            - {name: plugins-mcp, mountPath: /plugins-mcp}
            - {name: plugins-skills, mountPath: /plugins-skills}
        - name: web
          image: ${AR}/pacaai/paca-web:${TAG}
        - name: realtime
          image: ${AR}/pacaai/paca-realtime:${TAG}
          env:
            - {name: PORT, value: "3001"}
            - {name: NODE_ENV, value: production}
            - {name: API_URL, value: "http://localhost:8081"}
            - {name: REDIS_URL, value: "${REDIS_URL}"}
            - {name: CORS_ORIGINS, value: "${URL}"}
YAML
gcloud run services replace "$SPEC" --region "$REGION"
gcloud run services add-iam-policy-binding "$NAME" --region "$REGION" \
    --member allUsers --role roles/run.invoker >/dev/null

cat <<MSG

Paca is starting at ${URL}  (give it a minute or two, then sign in as "admin").
  Admin password:  gcloud secrets versions access latest --secret ${NAME}-admin-password
  Encryption key (back it up, losing it makes stored secrets unreadable):
                   gcloud secrets versions access latest --secret ${NAME}-encryption-key
MSG
