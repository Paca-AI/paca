# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
# Runs on the build platform (not the target platform): the output is
# architecture-independent static assets, so cross-arch runs would otherwise
# execute bun under QEMU emulation for no benefit, which has been known to
# corrupt native-binary tarball extraction (e.g. @biomejs/cli-linux-arm64).
FROM --platform=$BUILDPLATFORM oven/bun:1-alpine AS builder

WORKDIR /build

# Install dependencies first for layer caching
COPY package.json bun.lock* ./
RUN bun install --frozen-lockfile

COPY . .

RUN bun run build

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM caddy:2-alpine

# Replace the default Caddy config with our SPA config
COPY Caddyfile /etc/caddy/Caddyfile

# Copy built assets from the build stage
COPY --from=builder /build/dist /srv

EXPOSE 3000

CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
