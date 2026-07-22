#!/bin/sh
set -e

# Create plugin directories with correct ownership
echo "Initializing plugin directories..."
mkdir -p /plugins /plugins-frontend /plugins-mcp /plugins-skills
chown -R app:app /plugins /plugins-frontend /plugins-mcp /plugins-skills
chmod -R 755 /plugins /plugins-frontend /plugins-mcp /plugins-skills

echo "Starting API as app user..."
exec su-exec app:app /app/api "$@"
