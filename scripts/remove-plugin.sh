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
Usage: $(basename "$0") <plugin_id> [options]

Remove a Paca plugin from the system.

Arguments:
  plugin_id          Plugin ID (e.g., com.paca.example)

Options:
  -h, --help          Show this help message
  --paca-dir DIR      Path to Paca project directory (default: auto-detected)
  --api-url URL       API base URL (default: http://localhost)
  --api-key KEY       API key for authentication (required)
  --unregister-only    Only remove plugin registration (keep local artifacts)
  --remove-artifacts-only Only remove local artifacts (keep plugin registration)

Environment Variables:
  PACA_DIR            Same as --paca-dir
  API_URL             Same as --api-url
  API_KEY             Same as --api-key (required)

Examples:
  # Basic usage
  $(basename "$0") com.paca.example

  # Using environment variable for API key
  export API_KEY=your-api-key
  $(basename "$0") com.paca.example

  # Custom API URL
  $(basename "$0") com.paca.example --api-url http://localhost:8080 --api-key your-api-key

  # Remove registration only (keep files for later)
  $(basename "$0") com.paca.example --unregister-only

  # Remove artifacts only (keep plugin enabled but remove files)
  $(basename "$0") com.paca.example --remove-artifacts-only
EOF
    exit 0
}

# Parse arguments
UNREGISTER_ONLY=false
REMOVE_ARTIFACTS_ONLY=false

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
        --unregister-only)
            UNREGISTER_ONLY=true
            shift
            ;;
        --remove-artifacts-only)
            REMOVE_ARTIFACTS_ONLY=true
            shift
            ;;
        -*)
            print_error "Unknown option: $1"
            show_usage
            ;;
        *)
            PLUGIN_ID="$1"
            shift
            ;;
    esac
done

# Validate required arguments
if [[ -z "$PLUGIN_ID" ]]; then
    print_error "Plugin ID is required"
    show_usage
fi

# Validate options are not mutually exclusive
if [[ "$UNREGISTER_ONLY" = true ]] && [[ "$REMOVE_ARTIFACTS_ONLY" = true ]]; then
    print_error "Cannot use both --unregister-only and --remove-artifacts-only together"
    exit 1
fi

print_info "Plugin: $PLUGIN_ID"
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

# Check if plugin exists
PLUGIN_EXISTS=false
if [[ -d "$BACKEND_DIR" ]] || [[ -d "$FRONTEND_DIR" ]]; then
    PLUGIN_EXISTS=true
fi

if [[ "$PLUGIN_EXISTS" = false ]]; then
    print_error "Plugin $PLUGIN_ID not found in local plugin store"
    print_info "Checked:"
    print_info "  Backend: $BACKEND_DIR"
    print_info "  Frontend: $FRONTEND_DIR"
    print_info "  MCP: $MCP_DIR"
    print_info "  Skills: $SKILLS_DIR"
    exit 1
fi

# Remove plugin registration via API
if [[ "$REMOVE_ARTIFACTS_ONLY" = false ]]; then
    # Validate API key is provided
    if [[ -z "$API_KEY" ]]; then
        print_error "API key is required for authentication"
        print_info "Set API key via --api-key option or API_KEY environment variable"
        print_info "See scripts/API_KEY_GUIDE.md for help creating an API key"
        exit 1
    fi

    print_step "Authenticating with API..."
    AUTH_HEADER="X-API-Key: $API_KEY"
    print_success "Authenticated successfully"

    # Get list of plugins to find plugin UUID
    print_step "Looking up plugin $PLUGIN_ID..."
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

    # Extract plugin UUID from response
    print_info "Attempting to extract plugin UUID for: $PLUGIN_ID"
    
    # Check if response has expected structure
    if ! echo "$LIST_BODY" | jq -e '.data' > /dev/null 2>/dev/null; then
        print_error "API response does not contain .data field"
        print_info "Response structure: $(echo "$LIST_BODY" | jq -r 'keys' 2>/dev/null || echo "Could not parse structure")"
        exit 1
    fi
    
    # Try multiple jq approaches with better error handling
    if ! PLUGIN_UUID=$(echo "$LIST_BODY" | jq -r --arg name "$PLUGIN_ID" '.data.plugins[] | select(.name == $name) | .id' 2>/dev/null); then
        print_error "Failed to extract plugin UUID from API response"
        print_info "API Response may not be valid JSON: $LIST_BODY"
        print_info "Available plugins:"
        echo "$LIST_BODY" | jq -r '.data.plugins[] | .name' 2>/dev/null || echo "Could not parse plugin list"
        exit 1
    fi

    if [[ -z "$PLUGIN_UUID" ]] || [[ "$PLUGIN_UUID" == "null" ]]; then
        print_error "Plugin $PLUGIN_ID not found in API database"
        print_info "Available plugins:"
        echo "$LIST_BODY" | jq -r '.data.plugins[] | "  - \(.name)"' 2>/dev/null || echo "  (Could not parse plugin list)"
        print_info "The plugin may have already been unregistered, or you may not have permission to access it"
        exit 1
    fi

    print_success "Plugin found: $PLUGIN_UUID"

    # Remove plugin registration
    print_step "Removing plugin registration..."
    DELETE_RESPONSE=$(curl -s -X DELETE "${API_ENDPOINT}/admin/plugins/${PLUGIN_UUID}" \
        -H "${AUTH_HEADER}" \
        -w "\n%{http_code}")

    HTTP_CODE=$(echo "$DELETE_RESPONSE" | tail -n1)
    DELETE_BODY=$(echo "$DELETE_RESPONSE" | sed '$d')

    if [[ "$HTTP_CODE" != "204" ]] && [[ "$HTTP_CODE" != "200" ]]; then
        print_error "Failed to remove plugin registration (HTTP $HTTP_CODE)"
        print_error "Response: $DELETE_BODY"
        exit 1
    fi

    print_success "Plugin registration removed successfully"
else
    print_step "Skipping plugin removal (--remove-artifacts-only)"
fi

# Remove local artifacts
if [[ "$UNREGISTER_ONLY" = false ]]; then
    print_step "Removing local plugin artifacts..."

    # Remove backend artifacts
    if [[ -d "$BACKEND_DIR" ]]; then
        print_info "Removing backend artifacts: $BACKEND_DIR"
        rm -rf "$BACKEND_DIR"
        print_success "Backend artifacts removed"
    else
        print_info "No backend artifacts found"
    fi

    # Remove frontend artifacts
    if [[ -d "$FRONTEND_DIR" ]]; then
        print_info "Removing frontend artifacts: $FRONTEND_DIR"
        rm -rf "$FRONTEND_DIR"
        print_success "Frontend artifacts removed"
    else
        print_info "No frontend artifacts found"
    fi

    # Remove MCP artifacts (if exists)
    if [[ -d "$MCP_DIR" ]]; then
        print_info "Removing MCP artifacts: $MCP_DIR"
        rm -rf "$MCP_DIR"
        print_success "MCP artifacts removed"
    else
        print_info "No MCP artifacts found"
    fi

    # Remove skills artifacts (if exists)
    if [[ -d "$SKILLS_DIR" ]]; then
        print_info "Removing skills artifacts: $SKILLS_DIR"
        rm -rf "$SKILLS_DIR"
        print_success "Skills artifacts removed"
    else
        print_info "No skills artifacts found"
    fi
else
    print_step "Skipping artifact removal (--unregister-only)"
fi

echo ""
print_success "Plugin $PLUGIN_ID removal complete!"
if [[ "$REMOVE_ARTIFACTS_ONLY" = false ]]; then
    print_info "Plugin registration removed from API"
fi
if [[ "$UNREGISTER_ONLY" = false ]]; then
    print_info "Local artifacts removed from plugin store"
fi
print_info "Note: Any database tables/migrations created by the plugin are NOT automatically removed"
print_info "You may need to manually clean up database if needed"
