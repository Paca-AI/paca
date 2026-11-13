# Caddy gateway for the Render blueprint (render.yaml at the repo root).
# Render can't mount a config file into a prebuilt image, so bake the shared
# Caddyfile in. Build context is the repo root.
FROM caddy:2-alpine
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
