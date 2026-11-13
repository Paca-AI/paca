# Caddy gateway for the Render blueprint (render.yaml at the repo root).
# Render can't mount a config file into a prebuilt image, so bake the shared
# Caddyfile in. Build context is the repo root.
FROM caddy:2-alpine
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
# The web app is a Render static site (an https:// WEB_UPSTREAM on another
# host), which routes by Host header, so forward the upstream's own host.
# Fails the build if the Caddyfile line this patches ever changes.
RUN sed -i 's|reverse_proxy {\$WEB_UPSTREAM:web:3000}|reverse_proxy {\$WEB_UPSTREAM:web:3000} {\n\t\theader_up Host {upstream_hostport}\n\t}|' /etc/caddy/Caddyfile \
 && grep -q 'header_up Host {upstream_hostport}' /etc/caddy/Caddyfile
