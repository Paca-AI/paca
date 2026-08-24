# Plugin Management Scripts - Summary

I've created a complete solution for building, installing, and removing Paca plugins from local directories. Here's what was added:

## Created Files

1. **`scripts/install-local-plugin.sh`** - Main installation script with full features
2. **`scripts/install-plugin.sh`** - Lightweight wrapper for installation
3. **`scripts/remove-plugin.sh`** - Main removal script with full features
4. **`scripts/uninstall-plugin.sh`** - Lightweight wrapper for removal
5. **`scripts/README.md`** - Comprehensive documentation
6. **`scripts/QUICK_START.md`** - Quick reference guide
7. **`scripts/REMOVE_PLUGIN_GUIDE.md`** - Plugin removal documentation
8. **`scripts/API_KEY_GUIDE.md`** - API key creation and management
9. **`scripts/CHANGES.md`** - Recent changes and migration guide

## Quick Start

### Install a plugin:
```bash
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example --api-key your-api-key
```

### Remove a plugin:
```bash
./scripts/remove-plugin.sh com.paca.example --api-key your-api-key
```

### Or use the wrappers:
```bash
./scripts/install-plugin.sh /path/to/plugin --api-key your-api-key
./scripts/uninstall-plugin.sh com.paca.example --api-key your-api-key
```

### Using environment variable:
```bash
export API_KEY=your-api-key
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example
```

## What the Script Does

The script automates the complete plugin installation process:

1. ✅ **Validates plugin structure** - Checks for required files
2. ✅ **Builds backend WASM** - Compiles Go backend using TinyGo (`tinygo build -target=wasip1 -buildmode=c-shared`)
3. ✅ **Populates backend store** - Copies artifacts to `plugins/local/backend/<plugin-id>/`
4. ✅ **Builds frontend** - Runs `bun run build` to create bundles
5. ✅ **Populates frontend store** - Copies assets to `plugins/local/frontend/<plugin-id>/`
6. ✅ **Authenticates with API** - Validates API key
7. ✅ **Checks existing plugins** - Determines if plugin is already installed
8. ✅ **Installs or updates plugin** - Calls Paca API to register the plugin

## Features

- **Smart plugin detection** - Extracts plugin ID and version from `plugin.json`
- **API key authentication** - Secure authentication using API keys (required)
- **Build skip option** - `--skip-build` for installing pre-built plugins
- **Install skip option** - `--skip-install` for building without API registration
- **Update existing plugins** - Detects and offers to update already-installed plugins
- **Colored output** - Easy-to-read progress messages
- **Error handling** - Validates each step and provides helpful error messages
- **Flexible configuration** - Supports custom API URLs

## Usage Examples

### Basic installation:
```bash
./scripts/install-local-plugin.sh /path/to/plugin --api-key your-api-key
```

### Using environment variable:
```bash
export API_KEY=your-api-key
./scripts/install-local-plugin.sh /path/to/plugin
```

### Custom API settings:
```bash
./scripts/install-local-plugin.sh /path/to/plugin \
    --api-url http://localhost:8080 \
    --api-key your-api-key
```

### Build only (no API call):
```bash
./scripts/install-local-plugin.sh /path/to/plugin --skip-install
```

### Install only (no build):
```bash
./scripts/install-local-plugin.sh /path/to/plugin --skip-build
```

## Environment Variables

You can configure the script using environment variables:

```bash
export API_KEY=your-api-key-here
./scripts/install-local-plugin.sh /path/to/plugin
```

For persistent configuration, add to your shell config:

```bash
echo 'export API_KEY=your-api-key-here' >> ~/.bashrc
source ~/.bashrc
```

## Requirements

- Go compiler (for building WASM)
- Bun package manager (for building frontend)
- jq (for JSON parsing)
- curl (for API calls)
- Paca API key (required - see [API_KEY_GUIDE.md](./API_KEY_GUIDE.md))
- Paca services running and accessible

## Adding to Your Shell

For convenient access from anywhere, add this alias to your shell config:

```bash
# Add to ~/.bashrc or ~/.zshrc
alias paca-install='/Volumes/HaiSSD/Projects/paca/scripts/install-local-plugin.sh'
```

Then use:
```bash
paca-install /path/to/plugin
```

## Troubleshooting

See `scripts/README.md` for detailed troubleshooting guide.

Common issues:
- **Script not executable**: `chmod +x scripts/install-local-plugin.sh`
- **API connection failed**: Ensure Paca services are running
- **Build failed**: Install dependencies manually in backend/frontend directories
- **Authentication failed**: Verify API key is valid and not revoked
- **How to get API key**: Log in to Paca → Settings → API Keys → Create new key (see [API_KEY_GUIDE.md](./API_KEY_GUIDE.md))

## Documentation

- **Detailed documentation**: `scripts/README.md`
- **Quick reference**: `scripts/QUICK_START.md`
- **Help message**: Run `./scripts/install-local-plugin.sh --help`

## Manual Steps (If Script Fails)

If the automated script doesn't work, see `QUICK_START.md` for the complete manual command sequence.

---

The scripts are ready to use. Try running:
```bash
./scripts/install-local-plugin.sh /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example
```
