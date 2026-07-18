# sdd-proxy — tiny same-origin data proxy for the native SDD plugin (ADR-038).
# Zero npm dependencies: Node 20 built-ins only (http, crypto, global fetch).
FROM node:20-alpine

WORKDIR /app
COPY server.js ./

# Drop privileges — the image ships the `node` user.
USER node

ENV PORT=8791
EXPOSE 8791

# Lightweight liveness probe against the unauthenticated /healthz route.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:'+(process.env.PORT||8791)+'/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

CMD ["node", "server.js"]
