# ── Production Dockerfile for services/meeting-agent ─────────────────────────
#
# Multi-stage build:
#   1. build — install all dependencies and compile TypeScript to dist/.
#   2. final — production dependencies and the compiled output only.
#
# glibc base image (not alpine): @livekit/rtc-node and @livekit/agents ship
# prebuilt glibc binaries.

FROM node:22-bookworm-slim AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY tsconfig.json ./
COPY src/ src/
RUN npx tsc

FROM node:22-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production
COPY package.json package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY --from=build /app/dist dist
# The agents library runs a small health/info server here.
EXPOSE 8081
USER node
CMD ["node", "dist/index.js", "start"]
