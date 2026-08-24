#!/bin/sh
set -eu

if ! command -v helm >/dev/null 2>&1; then
  echo "helm is required to render the chart" >&2
  exit 127
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
chart_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

base_values="$tmp_dir/base-values.yaml"
cat >"$base_values" <<'EOF'
secrets:
  jwtSecret: "test-jwt-secret"
  adminPassword: "test-admin-password"
  encryptionKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  internalApiKey: "test-internal-api-key"
  agentApiKey: "test-agent-api-key"
  postgresPassword: "test-postgres-password"
  storageAccessKeyId: "test-storage-access-key"
  storageSecretAccessKey: "test-storage-secret-key"
EOF

default_output="$tmp_dir/default.yaml"
enabled_output="$tmp_dir/enabled.yaml"
helm template paca "$chart_dir" -f "$base_values" >"$default_output"
helm template paca "$chart_dir" \
  -f "$base_values" \
  -f "$chart_dir/examples/oidc-values.yaml" >"$enabled_output"

assert_contains() {
  file=$1
  text=$2
  if ! grep -Fq "$text" "$file"; then
    echo "missing '$text' in $file" >&2
    exit 1
  fi
}

assert_not_contains() {
  file=$1
  text=$2
  if grep -Fq "$text" "$file"; then
    echo "unexpected '$text' in $file" >&2
    exit 1
  fi
}

assert_not_contains "$default_output" "name: OIDC_ENABLED"
echo "default chart keeps OIDC disabled"

assert_contains "$enabled_output" "name: OIDC_ENABLED"
assert_contains "$enabled_output" "value: \"https://idp.example.com/realms/paca\""
assert_contains "$enabled_output" "value: \"paca-web\""
assert_contains "$enabled_output" "value: \"https://paca.example.com/api/v1/auth/oidc/callback\""
assert_contains "$enabled_output" "value: \"Company SSO\""
assert_contains "$enabled_output" "name: LOCAL_LOGIN_ENABLED"
assert_contains "$enabled_output" "name: OIDC_CLIENT_SECRET"

secret_literal_count=$(grep -Fo "replace-with-client-secret" "$enabled_output" | wc -l | tr -d ' ')
if [ "$secret_literal_count" -ne 1 ]; then
  echo "expected OIDC client secret literal once in the rendered Secret, got $secret_literal_count" >&2
  exit 1
fi

echo "enabled chart places OIDC settings in the API and client secret in the Secret"
