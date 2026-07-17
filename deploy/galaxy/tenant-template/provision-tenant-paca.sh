#!/usr/bin/env bash
# =============================================================================
# provision-tenant-paca.sh — instance-per-tenant Paca deploys (ADR-038 T7)
# =============================================================================
#
# Usage:
#   ./provision-tenant-paca.sh <tenant_code> <domain>
#   ./provision-tenant-paca.sh vietjet tasks-vietjet.skyplatform.net
#
# Generates ~/Nexus/paca-tenants/<tenant_code>/.env.tenant from
# env.tenant.template (fresh `openssl rand -hex 32` secrets, tenant-scoped
# project name / gateway alias / backup dir / OIDC client id) and prints the
# compose invocation plus the manual onboarding checklist.
#
# Isolation model (VERIFIED against deploy/docker-compose.prod.yml):
#   * Every named volume (postgres_data, valkey_data, minio_data,
#     backend_plugins, frontend_plugins, mcp_plugins, caddy_data,
#     caddy_config) has no explicit `name:`/`external:`, and no service sets
#     `container_name` — so `-p paca-<tenant>` prefixes ALL of them
#     (paca-<tenant>_postgres_data, …) and tenants never share state.
#   * The stack-private `default` network becomes paca-<tenant>_default.
#   * Only the gateway joins the shared external galaxy_network, under the
#     per-tenant alias GATEWAY_NETWORK_ALIAS=paca-<tenant>-gateway
#     (parameterized in deploy/galaxy/docker-compose.galaxy.yml).
#
# The script only writes the per-tenant deploy dir — it never touches Docker,
# Cloudflare, or Vortex. Idempotence: refuses to overwrite an existing
# .env.tenant (delete it yourself if you really want to re-provision; the
# old secrets are the live stack's credentials).
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
TEMPLATE="${SCRIPT_DIR}/env.tenant.template"
TENANTS_ROOT="${PACA_TENANTS_DIR:-${HOME}/Nexus/paca-tenants}"
# Repo root (…/deploy/galaxy/tenant-template → three levels up).
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../../.." >/dev/null 2>&1 && pwd)"

usage() {
	echo "Usage: $0 <tenant_code> <domain>" >&2
	echo "  tenant_code: 2-30 chars, lowercase [a-z0-9-], starts alphanumeric" >&2
	echo "  domain:      public hostname, e.g. tasks-<tenant>.skyplatform.net" >&2
	exit 64
}

[ "$#" -eq 2 ] || usage
TENANT="$1"
DOMAIN="$2"

# Compose project names must be lowercase alphanumeric/-/_ and start with an
# alphanumeric; keep it strict so volume/alias/DNS names stay valid too.
if ! printf '%s' "${TENANT}" | grep -Eq '^[a-z0-9][a-z0-9-]{1,29}$'; then
	echo "ERROR: invalid tenant_code '${TENANT}'" >&2
	usage
fi
if ! printf '%s' "${DOMAIN}" | grep -Eq '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$'; then
	echo "ERROR: invalid domain '${DOMAIN}'" >&2
	usage
fi
[ -f "${TEMPLATE}" ] || {
	echo "ERROR: template not found: ${TEMPLATE}" >&2
	exit 66
}
command -v openssl >/dev/null 2>&1 || {
	echo "ERROR: openssl is required to generate secrets" >&2
	exit 69
}

TENANT_DIR="${TENANTS_ROOT}/${TENANT}"
ENV_FILE="${TENANT_DIR}/.env.tenant"
PROJECT="paca-${TENANT}"
GATEWAY_ALIAS="paca-${TENANT}-gateway"

if [ -e "${ENV_FILE}" ]; then
	echo "ERROR: ${ENV_FILE} already exists — refusing to overwrite live tenant secrets." >&2
	exit 73
fi

mkdir -p "${TENANT_DIR}"

gen_secret() { openssl rand -hex 32; }

# Each placeholder gets its OWN fresh secret. sed replacement values are
# hex-only ([0-9a-f]) and the domain/tenant are validated above, so plain /
# delimiters are safe. The generated file is written once, atomically.
TMP_FILE="$(mktemp "${TENANT_DIR}/.env.tenant.XXXXXX")"
trap 'rm -f "${TMP_FILE}"' EXIT

sed \
	-e "s/__TENANT__/${TENANT}/g" \
	-e "s/__DOMAIN__/${DOMAIN}/g" \
	-e "s/__POSTGRES_PASSWORD__/$(gen_secret)/" \
	-e "s/__JWT_SECRET__/$(gen_secret)/" \
	-e "s/__ADMIN_PASSWORD__/$(gen_secret)/" \
	-e "s/__ENCRYPTION_KEY__/$(gen_secret)/" \
	-e "s/__STORAGE_SECRET_ACCESS_KEY__/$(gen_secret)/" \
	-e "s/__AGENT_API_KEY__/$(gen_secret)/" \
	-e "s/__INTERNAL_API_KEY__/$(gen_secret)/" \
	"${TEMPLATE}" >"${TMP_FILE}"

# Nothing unreplaced left behind (except the deliberate Vortex secret stub)?
LEFTOVER="$(grep -n '__' "${TMP_FILE}" | grep -v '__FILL_ME_FROM_VORTEX__' || true)"
if [ -n "${LEFTOVER}" ]; then
	echo "ERROR: unreplaced placeholders remain — template/script drift?" >&2
	echo "${LEFTOVER}" >&2
	exit 70
fi

chmod 600 "${TMP_FILE}"
mv "${TMP_FILE}" "${ENV_FILE}"
trap - EXIT

cat <<EOF

Provisioned tenant '${TENANT}' → ${ENV_FILE} (mode 600)

Deploy (from the repo root, e.g. ${REPO_ROOT}):

  docker compose -p ${PROJECT} \\
    --env-file ${ENV_FILE} \\
    -f deploy/docker-compose.prod.yml \\
    -f deploy/galaxy/docker-compose.galaxy.yml \\
    up -d --scale ai-agent=0

  (same repo, same compose files for every tenant — isolation comes from the
   project name: volumes/network become ${PROJECT}_postgres_data,
   ${PROJECT}_default, …)

Manual checklist (in order):

  [ ] 1. Vortex identity: register OAuth client id 'paca-${TENANT}'
         (redirect URL https://${DOMAIN}/api/v1/auth/oidc/callback), then
         paste the issued secret into OIDC_CLIENT_SECRET in ${ENV_FILE}.
  [ ] 2. Cloudflare tunnel: add ingress hostname ${DOMAIN}
         -> service http://${GATEWAY_ALIAS}:80 (galaxy-cloudflared-tunnel
         config; the gateway's galaxy_network alias is ${GATEWAY_ALIAS}).
  [ ] 3. DNS: CNAME ${DOMAIN} -> the tunnel's cfargotunnel.com target
         (proxied), same zone flow as tasks.skyplatform.net.
  [ ] 4. Deploy with the compose command above, then log in at
         https://${DOMAIN} and verify OIDC SSO end-to-end.
  [ ] 5. First backup verify: ensure /backup/paca-${TENANT}-postgres receives
         a dump after 02:00 (BACKUP_CRON), and test-restore one dump.

Notes:
  - .env.tenant contains live secrets — never commit it, never paste it into
    chat/tickets. ADMIN_PASSWORD is the local break-glass admin login.
  - Park the stack:    docker compose -p ${PROJECT} ... stop
  - Tear down (keeps volumes):  docker compose -p ${PROJECT} ... down
  - DANGER full wipe:  docker compose -p ${PROJECT} ... down -v
EOF
