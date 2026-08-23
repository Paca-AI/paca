#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
PACA_DIR="${PACA_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
API_URL="${API_URL:-http://localhost}"
API_ENDPOINT="${API_URL}/api/v1"
API_KEY="${API_KEY:-}"

# Function to print colored messages
print_step() {
    echo -e "${BLUE}▶ $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Function to show usage
show_usage() {
    cat << EOF
Usage: $(basename "$0") <plugin_dir> [options]

Build and install a Paca plugin from a local directory.

Arguments:
  plugin_dir          Path to the plugin directory (must contain plugin.json)

Options:
  -h, --help          Show this help message
  --paca-dir DIR      Path to Paca project directory (default: auto-detected)
  --api-url URL       API base URL (default: http://localhost)
  --api-key KEY       API key for authentication (required)
  --skip-build        Skip building (only install)
  --skip-install      Skip installing (only build)
  --go-toolchain TOOL "tinygo" (default) or "go" — most plugins build with
                      TinyGo for a much smaller binary; use "go" (the
                      standard compiler) only if your plugin hits a TinyGo
                      stdlib limitation, e.g. it uses html/template or
                      text/template — TinyGo's reflect package doesn't fully
                      support what those need and the plugin will panic at
                      runtime the first time it's actually invoked, not at
                      build time.

Environment Variables:
  PACA_DIR            Same as --paca-dir
  API_URL             Same as --api-url
  API_KEY             Same as --api-key (required)
  GO_TOOLCHAIN        Same as --go-toolchain

Examples:
  # Basic usage with API key
  $(basename "$0") /Volumes/HaiSSD/Projects/paca-plugins/paca-plugin-example --api-key your-api-key

  # Custom API URL with API key
  $(basename "$0") /path/to/plugin --api-url http://localhost:8080 --api-key your-api-key

  # Using environment variable for API key
  export API_KEY=your-api-key
  $(basename "$0") /path/to/plugin

  # Build only (don't install)
  $(basename "$0") /path/to/plugin --api-key your-api-key --skip-install

EOF
    exit 0
}

# Parse arguments
SKIP_BUILD=false
SKIP_INSTALL=false
GO_TOOLCHAIN="${GO_TOOLCHAIN:-tinygo}"

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_usage
            ;;
        --paca-dir)
            PACA_DIR="$2"
            shift 2
            ;;
        --api-url)
            API_URL="$2"
            API_ENDPOINT="${API_URL}/api/v1"
            shift 2
            ;;
        --api-key)
            API_KEY="$2"
            shift 2
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        --skip-install)
            SKIP_INSTALL=true
            shift
            ;;
        --go-toolchain)
            GO_TOOLCHAIN="$2"
            shift 2
            ;;
        -*)
            print_error "Unknown option: $1"
            show_usage
            ;;
        *)
            PLUGIN_DIR="$1"
            shift
            ;;
    esac
done

# Validate required arguments
if [[ -z "$PLUGIN_DIR" ]]; then
    print_error "Plugin directory is required"
    show_usage
fi

# Convert to absolute path
PLUGIN_DIR="$(cd "$PLUGIN_DIR" && pwd)"

# Check plugin directory structure
if [[ ! -f "$PLUGIN_DIR/plugin.json" ]]; then
    print_error "plugin.json not found in $PLUGIN_DIR"
    exit 1
fi

if [[ ! -d "$PLUGIN_DIR/backend" ]]; then
    print_error "backend directory not found in $PLUGIN_DIR"
    exit 1
fi

if [[ ! -d "$PLUGIN_DIR/frontend" ]]; then
    print_error "frontend directory not found in $PLUGIN_DIR"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    print_error "jq is required but not installed"
    exit 1
fi

if [[ "$GO_TOOLCHAIN" != "tinygo" && "$GO_TOOLCHAIN" != "go" ]]; then
    print_error "--go-toolchain must be \"tinygo\" or \"go\", got: $GO_TOOLCHAIN"
    exit 1
fi

# Extract plugin info
PLUGIN_ID=$(jq -r '.id // empty' "$PLUGIN_DIR/plugin.json")
PLUGIN_VERSION=$(jq -r '.version // empty' "$PLUGIN_DIR/plugin.json")

if [[ -z "$PLUGIN_ID" ]]; then
    print_error "Could not extract plugin ID from plugin.json"
    exit 1
fi

if [[ -z "$PLUGIN_VERSION" ]]; then
    print_error "Could not extract plugin version from plugin.json"
    exit 1
fi

print_info "Plugin: $PLUGIN_ID"
print_info "Version: $PLUGIN_VERSION"
print_info "Plugin directory: $PLUGIN_DIR"
print_info "Paca directory: $PACA_DIR"
print_info "API URL: $API_URL"
echo ""

# Check if Paca directory exists
if [[ ! -d "$PACA_DIR" ]]; then
    print_error "Paca directory not found: $PACA_DIR"
    exit 1
fi

BACKEND_DIR="$PACA_DIR/plugins/local/backend/$PLUGIN_ID"
FRONTEND_DIR="$PACA_DIR/plugins/local/frontend/$PLUGIN_ID"
MCP_DIR="$PACA_DIR/plugins/local/mcp/$PLUGIN_ID"
SKILLS_DIR="$PACA_DIR/plugins/local/skills/$PLUGIN_ID"

# Build backend
if [[ "$SKIP_BUILD" = false ]]; then
    print_step "Building backend WASM..."
    cd "$PLUGIN_DIR/backend"
    
    # Check if go.mod exists
    if [[ ! -f "go.mod" ]]; then
        print_error "go.mod not found in backend directory"
        exit 1
    fi

    if [[ "$GO_TOOLCHAIN" = "tinygo" ]]; then
        if ! command -v tinygo >/dev/null 2>&1; then
            print_error "tinygo is required but not installed (see https://tinygo.org/getting-started/install/), or pass --go-toolchain go"
            exit 1
        fi
        tinygo build -target=wasip1 -buildmode=c-shared -o backend.wasm .
    else
        if ! command -v go >/dev/null 2>&1; then
            print_error "go is required but not installed"
            exit 1
        fi
        GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o backend.wasm .
    fi
    
    if [[ ! -f "backend.wasm" ]]; then
        print_error "backend.wasm build failed"
        exit 1
    fi
    
    print_success "Backend WASM built successfully"
else
    print_step "Skipping backend build (--skip-build)"
fi

# Populate backend store
print_step "Populating backend store..."
mkdir -p "$BACKEND_DIR/migrations"

if [[ "$SKIP_BUILD" = false ]]; then
    cp backend.wasm "$BACKEND_DIR/backend.wasm"
    cp migrations/*.sql "$BACKEND_DIR/migrations/" 2>/dev/null || true
fi

cp "$PLUGIN_DIR/plugin.json" "$BACKEND_DIR/plugin.json"
print_success "Backend store populated"

# Build frontend
if [[ "$SKIP_BUILD" = false ]]; then
    print_step "Building frontend..."
    cd "$PLUGIN_DIR/frontend"
    
    # Check if package.json exists
    if [[ ! -f "package.json" ]]; then
        print_error "package.json not found in frontend directory"
        exit 1
    fi
    
    # Install dependencies if node_modules doesn't exist
    if [[ ! -d "node_modules" ]]; then
        print_step "Installing frontend dependencies..."
        bun install
    fi
    
    bun run build
    
    if [[ ! -d "dist" ]]; then
        print_error "Frontend build failed - dist directory not found"
        exit 1
    fi
    
    print_success "Frontend built successfully"
else
    print_step "Skipping frontend build (--skip-build)"
fi

# Populate frontend store
print_step "Populating frontend store..."
mkdir -p "$FRONTEND_DIR"

if [[ "$SKIP_BUILD" = false ]]; then
    cp -r dist/. "$FRONTEND_DIR/"
fi

print_success "Frontend store populated"

# Build and populate MCP bundle (optional — not every plugin has MCP tools)
if [[ -d "$PLUGIN_DIR/mcp" ]]; then
    if [[ "$SKIP_BUILD" = false ]]; then
        print_step "Building MCP bundle..."
        cd "$PLUGIN_DIR/mcp"

        if [[ ! -f "package.json" ]]; then
            print_error "package.json not found in mcp directory"
            exit 1
        fi

        if [[ ! -d "node_modules" ]]; then
            print_step "Installing MCP dependencies..."
            bun install
        fi

        bun run build

        if [[ ! -f "dist/mcp.js" ]]; then
            print_error "MCP build failed - dist/mcp.js not found"
            exit 1
        fi

        print_success "MCP bundle built successfully"
    else
        print_step "Skipping MCP build (--skip-build)"
    fi

    print_step "Populating MCP store..."
    mkdir -p "$MCP_DIR"

    if [[ "$SKIP_BUILD" = false ]]; then
        cp "$PLUGIN_DIR/mcp/dist/mcp.js" "$MCP_DIR/mcp.js"
    fi

    print_success "MCP store populated"
else
    print_info "No mcp directory found — skipping MCP bundle (plugin has no MCP tools)"
fi

# Populate skills store (optional — not every plugin has Agent Skills).
# Skills are plain SKILL.md files, not compiled artifacts, so there's no
# build step — just copy the directory tree as-is, unlike backend/frontend/mcp.
if [[ -d "$PLUGIN_DIR/skills" ]]; then
    print_step "Populating skills store..."
    mkdir -p "$SKILLS_DIR"
    cp -r "$PLUGIN_DIR/skills/." "$SKILLS_DIR/"
    print_success "Skills store populated"
else
    print_info "No skills directory found — skipping skills (plugin has no Agent Skills)"
fi

# Install plugin via API
if [[ "$SKIP_INSTALL" = false ]]; then
    # Validate API key is provided
    if [[ -z "$API_KEY" ]]; then
        print_error "API key is required for authentication"
        print_info "Set API key via --api-key option or API_KEY environment variable"
        print_info "See scripts/API_KEY_GUIDE.md for help creating an API key"
        exit 1
    fi

    print_step "Authenticating with API key..."
    AUTH_HEADER="X-API-Key: $API_KEY"
    print_success "Authenticated successfully"

    # Check if plugin already exists
    print_step "Checking if plugin already exists..."
    LIST_RESPONSE=$(curl -s -X GET "${API_ENDPOINT}/plugins" \
        -H "${AUTH_HEADER}" \
        -w "\n%{http_code}")

    HTTP_CODE=$(echo "$LIST_RESPONSE" | tail -n1)
    LIST_BODY=$(echo "$LIST_RESPONSE" | sed '$d')

    # Check if API call succeeded
    if [[ "$HTTP_CODE" != "200" ]]; then
        print_error "Failed to retrieve plugin list (HTTP $HTTP_CODE)"
        print_info "API Response: $LIST_BODY"

        # Provide helpful troubleshooting based on HTTP code
        case "$HTTP_CODE" in
            404)
                print_info "The API endpoint may not exist. Check that:"
                print_info "  1. Paca API services are running: docker compose -f deploy/docker-compose.dev.yml ps"
                print_info "  2. API URL is correct: $API_ENDPOINT"
                print_info "  3. Try accessing: curl $API_ENDPOINT/plugins"
                ;;
            401|403)
                print_info "Authentication failed. Check that:"
                print_info "  1. API key is valid and not revoked"
                print_info "  2. API key has proper permissions"
                ;;
            500|502|503)
                print_info "Server error. Check that:"
                print_info "  1. API services are healthy"
                print_info "  2. No recent deployments causing issues"
                ;;
        esac
        exit 1
    fi
    
    EXISTING_PLUGIN_ID=$(echo "$LIST_BODY" | grep -o "\"name\":\"${PLUGIN_ID}\"" | head -n1)
    
    if [[ -n "$EXISTING_PLUGIN_ID" ]]; then
        print_info "Plugin ${PLUGIN_ID} already exists in the database"
        
        # Ask if user wants to update
        read -p "Do you want to update the existing plugin? (y/N) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            print_step "Updating plugin..."
            
            # Get the plugin UUID from response
            print_info "Attempting to extract plugin UUID for: $PLUGIN_ID"
            print_info "Response structure: $(echo "$LIST_BODY" | jq '.data | keys' 2>/dev/null || echo "Could not parse structure")"
            
            # Try multiple jq approaches with better error handling
            if ! PLUGIN_UUID=$(echo "$LIST_BODY" | jq -r --arg name "$PLUGIN_ID" '.data.plugins[] | select(.name == $name) | .id' 2>/dev/null); then
                print_error "Failed to extract plugin UUID from response"
                print_info "API Response: $LIST_BODY"
                print_info "Available plugins:"
                echo "$LIST_BODY" | jq -r '.data.plugins[] | .name' 2>/dev/null || echo "Could not parse plugin list"
                exit 1
            else
                print_success "Extracted plugin UUID: $PLUGIN_UUID"
            fi

            if [[ -z "$PLUGIN_UUID" ]] || [[ "$PLUGIN_UUID" == "null" ]]; then
                print_error "Could not extract plugin UUID from response"
                print_error "API Response: $LIST_BODY"
                exit 1
            fi
            
            # Read the manifest
            MANIFEST=$(cat "$PLUGIN_DIR/plugin.json")
            
            # Update the plugin
            UPDATE_RESPONSE=$(curl -s -X PATCH "${API_ENDPOINT}/admin/plugins/${PLUGIN_UUID}" \
                -H "${AUTH_HEADER}" \
                -H "Content-Type: application/json" \
                -d "{\"version\":\"${PLUGIN_VERSION}\",\"manifest\":${MANIFEST},\"enabled\":true}" \
                -w "\n%{http_code}")
            
            HTTP_CODE=$(echo "$UPDATE_RESPONSE" | tail -n1)
            UPDATE_BODY=$(echo "$UPDATE_RESPONSE" | sed '$d')
            
            if [[ "$HTTP_CODE" != "200" ]]; then
                print_error "Plugin update failed (HTTP $HTTP_CODE)"
                print_error "Response: $UPDATE_BODY"
                exit 1
            fi
            
            print_success "Plugin updated successfully"
        else
            print_info "Skipping plugin update"
        fi
    else
        print_step "Installing plugin via API..."
        
        # Read the manifest
        MANIFEST=$(cat "$PLUGIN_DIR/plugin.json")
        
        # Install the plugin
        INSTALL_RESPONSE=$(curl -s -X POST "${API_ENDPOINT}/admin/plugins" \
            -H "${AUTH_HEADER}" \
            -H "Content-Type: application/json" \
            -d "{\"name\":\"${PLUGIN_ID}\",\"version\":\"${PLUGIN_VERSION}\",\"manifest\":${MANIFEST},\"enabled\":true}" \
            -w "\n%{http_code}")
        
        HTTP_CODE=$(echo "$INSTALL_RESPONSE" | tail -n1)
        INSTALL_BODY=$(echo "$INSTALL_RESPONSE" | sed '$d')
        
        if [[ "$HTTP_CODE" != "201" ]]; then
            print_error "Plugin installation failed (HTTP $HTTP_CODE)"
            print_error "Response: $INSTALL_BODY"
            exit 1
        fi
        
        print_success "Plugin installed successfully"
    fi
else
    print_step "Skipping API installation (--skip-install)"
fi


echo ""
print_success "Plugin $PLUGIN_ID v$PLUGIN_VERSION build and installation complete!"
print_info "Backend artifacts: $BACKEND_DIR"
print_info "Frontend artifacts: $FRONTEND_DIR"
if [[ -d "$PLUGIN_DIR/mcp" ]]; then
    print_info "MCP artifacts: $MCP_DIR"
fi
if [[ -d "$PLUGIN_DIR/skills" ]]; then
    print_info "Skills artifacts: $SKILLS_DIR"
fi
print_info "If the plugin is enabled, it will be available after restarting the Paca services"
