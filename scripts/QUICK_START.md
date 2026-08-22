# Quick Reference: Install Local Plugin

## One-Line Command

Build and install a plugin from `/Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example`:

```bash
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example --api-key your-api-key
```

## With Environment Variable

```bash
export API_KEY=your-api-key
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example
```

## With Custom API URL

```bash
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example \
    --api-url http://localhost \
    --api-key your-api-key
```

## Manual Steps (If Script Fails)

If the script doesn't work, here are the manual steps:

```bash
# Set variables
PLUGIN_DIR="/Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example"
PLUGIN_ID="com.paca.example"
PACA_DIR="/Volumes/HaiSSD/Projects/paca"

# 1. Build backend WASM
cd "$PLUGIN_DIR/backend"
tinygo build -target=wasip1 -buildmode=c-shared -o backend.wasm .

# 2. Populate backend store
BACKEND_DIR="$PACA_DIR/plugins/local/backend/$PLUGIN_ID"
mkdir -p "$BACKEND_DIR/migrations"
cp backend.wasm "$BACKEND_DIR/backend.wasm"
cp "$PLUGIN_DIR"/backend/migrations/*.sql "$BACKEND_DIR/migrations/" 2>/dev/null || true
cp "$PLUGIN_DIR/plugin.json" "$BACKEND_DIR/plugin.json"

# 3. Build frontend
cd "$PLUGIN_DIR/frontend"
bun install && bun run build

# 4. Populate frontend store
FRONTEND_DIR="$PACA_DIR/plugins/local/frontend/$PLUGIN_ID"
mkdir -p "$FRONTEND_DIR"
cp -r dist/. "$FRONTEND_DIR/"

# 5. Register plugin via API
API_KEY="your-api-key-here"
curl -X POST http://localhost/api/v1/admin/plugins \
    -H "X-API-Key: $API_KEY" \
    -H "Content-Type: application/json" \
    -d "{
        \"name\":\"$PLUGIN_ID\",
        \"version\":\"0.1.0\",
        \"manifest\":$(cat "$PLUGIN_DIR/plugin.json"),
        \"enabled\":true
    }"
```

## Requirements

- Go compiler
- Bun package manager
- Paca services running
- API key (required - see [API Key Guide](./API_KEY_GUIDE.md))

## Troubleshooting

**Script not executable:**
```bash
chmod +x ./scripts/install-local-plugin.sh
```

**API connection failed:**
```bash
# Check if services are running
docker compose -f deploy/docker-compose.dev.yml ps
```

**Build failed:**
```bash
# Install dependencies manually
cd /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example/backend
go mod tidy

cd ../frontend
bun install
```

## Alternative: Using Environment Variables

```bash
export API_KEY=your-api-key-here
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example
```

## How to Get an API Key

1. Log in to Paca web interface
2. Go to Settings → API Keys
3. Create a new API key
4. Copy the key and use it with the `--api-key` option or `API_KEY` environment variable

For detailed instructions, see [API Key Guide](./API_KEY_GUIDE.md).
