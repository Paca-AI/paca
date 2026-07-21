# Local Plugin Management Scripts

This collection of scripts automates the process of building, installing, and removing Paca plugins.

## Available Scripts

- **`install-local-plugin.sh`** - Build and install a plugin from a local directory
- **`install-plugin.sh`** - Quick wrapper for installation
- **`remove-plugin.sh`** - Remove a plugin from the system
- **`uninstall-plugin.sh`** - Quick wrapper for removal (alias to `remove-plugin.sh`)

## Quick Start

### Install a Plugin

```bash
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example --api-key your-api-key-here
```

### Remove a Plugin

```bash
./scripts/remove-plugin.sh com.paca.example --api-key your-api-key-here
```

### Using Environment Variable

```bash
export API_KEY=your-api-key-here

# Install
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example

# Remove
./scripts/remove-plugin.sh com.paca.example
```

## Usage

```bash
./scripts/install-local-plugin.sh <plugin_dir> [options]
```

### Arguments

- `plugin_dir`: Path to the plugin directory (must contain `plugin.json`)

### Options

- `-h, --help`: Show help message
- `--paca-dir DIR`: Path to Paca project directory (default: auto-detected)
- `--api-url URL`: API base URL (default: http://localhost)
- `--api-key KEY`: API key for authentication (required)
- `--skip-build`: Skip building (only install via API)
- `--skip-install`: Skip API installation (only build)

### Environment Variables

- `PACA_DIR`: Path to Paca project directory
- `API_URL`: API base URL
- `API_KEY`: API key for authentication (required)

## Examples

### Basic Installation

```bash
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example --api-key your-api-key-here
```

### Using Environment Variable

```bash
export API_KEY=your-api-key-here
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example
```

### Custom API URL

```bash
./scripts/install-local-plugin.sh /path/to/plugin --api-url http://localhost:8080 --api-key your-api-key-here
```

### Build Only (No API Installation)

```bash
./scripts/install-local-plugin.sh /path/to/plugin --skip-install
```

### Install Only (No Build)

Useful if you've already built the plugin manually:

```bash
./scripts/install-local-plugin.sh /path/to/plugin --skip-build
```

### Using Environment Variables

```bash
export API_KEY=your-api-key-here
./scripts/install-local-plugin.sh /path/to/plugin
```

### Setting API Key Permanently

Add this to your `~/.bashrc` or `~/.zshrc`:

```bash
echo 'export API_KEY=your-api-key-here' >> ~/.bashrc
source ~/.bashrc
```

## What the Script Does

1. **Validates the plugin directory** - Checks for `plugin.json`, `backend/`, and `frontend/`
2. **Extracts plugin metadata** - Reads plugin ID and version from `plugin.json`
3. **Builds backend WASM** - Compiles Go backend to WASM using `GOOS=wasip1 GOARCH=wasm`
4. **Populates backend store** - Copies WASM binary, migrations, and manifest to `plugins/local/backend/<plugin-id>/`
5. **Builds frontend** - Runs `bun run build` to create frontend bundles
6. **Populates frontend store** - Copies built assets to `plugins/local/frontend/<plugin-id>/`
7. **Builds and populates MCP store** (if a `mcp/` directory exists) - Runs `bun run build` and copies `dist/mcp.js` to `plugins/local/mcp/<plugin-id>/`
8. **Populates skills store** (if a `skills/` directory exists) - Copies `SKILL.md` files as-is to `plugins/local/skills/<plugin-id>/` — no build step, since skills are plain markdown
9. **Authenticates with API** - Validates API key
10. **Checks for existing plugins** - Determines if the plugin is already installed
11. **Installs or updates plugin** - Calls the Paca API to register the plugin

## Directory Structure

The script expects the following structure in your plugin directory:

```
your-plugin/
├── plugin.json
├── backend/
│   ├── main.go
│   ├── go.mod
│   └── migrations/
│       └── 0001_*.sql
└── frontend/
    ├── package.json
    ├── vite.config.ts
    └── src/
        └── *.tsx
```

## Output Locations

After running the script, artifacts are placed in:

- **Backend**: `plugins/local/backend/<plugin-id>/`
  - `backend.wasm` - Compiled WASM binary
  - `plugin.json` - Plugin manifest
  - `migrations/` - SQL migration files

- **Frontend**: `plugins/local/frontend/<plugin-id>/`
  - `assets/` - Built JS/CSS bundles
  - `remoteEntry.js` - Module federation entry point

- **MCP** (if the plugin has an `mcp/` directory): `plugins/local/mcp/<plugin-id>/`
  - `mcp.js` - Built MCP tool bundle

- **Skills** (if the plugin has a `skills/` directory): `plugins/local/skills/<plugin-id>/`
  - `<skill-name>/SKILL.md` - one directory per skill, copied as-is

## Troubleshooting

### Plugin Already Exists

If the plugin is already installed, the script will ask if you want to update it:

```
Plugin com.paca.example already exists in the database
Do you want to update the existing plugin? (y/N)
```

### Build Fails

- Ensure Go and Bun are installed and in your PATH
- Check that `go.mod` exists in the `backend/` directory
- Check that `package.json` exists in the `frontend/` directory
- Verify dependencies can be installed: `cd backend && go mod tidy`, `cd frontend && bun install`

### API Connection Fails

- Ensure Paca services are running: `docker compose -f deploy/docker-compose.dev.yml up -d`
- Verify the API URL is correct: `curl http://localhost/api/v1/health`
- Check API key is valid and not revoked: see [API Key Guide](./API_KEY_GUIDE.md)

### Permissions

The script needs write permissions to the `plugins/local/` directory and its subdirectories.

## Requirements

- **Go** (for building WASM backend)
- **Bun** (for building frontend)
- **jq** (for JSON parsing, used when checking existing plugins)
- **curl** (for API calls)
- **Paca API key** (required - see [API Key Guide](./API_KEY_GUIDE.md))
- Paca API services running and accessible

## Integration with Development Workflow

Add this to your `~/.bashrc` or `~/.zshrc` for quick access:

```bash
# Paca plugin management helpers
alias paca-install='/Volumes/HaiSSD/Projects/paca/scripts/install-local-plugin.sh'
alias paca-remove='/Volumes/HaiSSD/Projects/paca/scripts/remove-plugin.sh'
```

Then use:
```bash
paca-install /path/to/plugin
paca-remove com.paca.example
```

## Removing Plugins

For detailed information about removing plugins, see [Plugin Removal Guide](./REMOVE_PLUGIN_GUIDE.md).

### Quick Examples

```bash
# Basic removal
./scripts/remove-plugin.sh com.paca.example

# Remove with environment variable
export API_KEY=your-api-key
./scripts/remove-plugin.sh com.paca.example

# Remove registration only (keep files)
./scripts/remove-plugin.sh com.paca.example --unregister-only

# Remove artifacts only (keep registration)
./scripts/remove-plugin.sh com.paca.example --remove-artifacts-only
```

### What Gets Removed

- Plugin registration from Paca API
- Backend artifacts (WASM binary, migrations, manifest)
- Frontend artifacts (JS/CSS bundles)
- MCP artifacts (if present)
- Skills artifacts (if present)

### What Does NOT Get Removed

- Database tables created by plugin migrations
- Data stored by the plugin
- Plugin configuration (reused on reinstall)

See [Plugin Removal Guide](./REMOVE_PLUGIN_GUIDE.md) for complete documentation.

Then use:

```bash
paca-install /path/to/plugin
```
