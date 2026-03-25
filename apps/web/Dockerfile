# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM oven/bun:1-alpine AS builder

WORKDIR /build

# Install dependencies first for layer caching
COPY package.json bun.lock* ./
RUN bun install --frozen-lockfile

COPY . .

# VITE_API_BASE_URL is injected at build time so the bundled client always
# points at the correct gateway origin (set via docker-compose build args).
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
RUN bun run build

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM nginx:1.29-alpine

# Replace the default nginx config with our SPA config
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy built assets from the build stage
COPY --from=builder /build/dist /usr/share/nginx/html

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]
