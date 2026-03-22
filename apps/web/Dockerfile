# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM oven/bun:1-alpine

WORKDIR /app

COPY package.json bun.lock* ./
RUN bun install --frozen-lockfile

COPY . .
RUN bun run build

CMD ["bun", "run", "preview", "--host", "0.0.0.0", "--port", "3000"]
