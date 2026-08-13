#!/usr/bin/env python3
"""Gera deploy/docker-compose.easypanel.yml a partir do prod.yml + Caddyfile.
Ajustes p/ EasyPanel: Caddyfile embutido como config, gateway sem portas de
host (Traefik roteia), sem ai-agent/db-backup, sem 'name'.
Depois: docker compose --env-file .env.producao -f deploy/docker-compose.easypanel.yml config
        > paca-evup-deploy/docker-compose.easypanel.final.yml  (+ remover 'name')
"""
import yaml

BASE = "/Users/vitorhervatinmac/Documents/Build to Learn/PACA/paca"
with open(f"{BASE}/deploy/docker-compose.prod.yml") as f:
    c = yaml.safe_load(f)
with open(f"{BASE}/deploy/caddy/Caddyfile") as f:
    caddyfile = f.read()

# EasyPanel/Traefik termina o HTTPS; o gateway serve HTTP puro na porta 80.
caddyfile = caddyfile.replace("{$SITE_ADDRESS::80}", ":80")

c.pop("name", None)
svcs = c["services"]

for s in ("ai-agent", "db-backup"):
    svcs.pop(s, None)

gw = svcs["gateway"]
gw.pop("ports", None)
gw["volumes"] = [v for v in gw.get("volumes", []) if "Caddyfile" not in v]
gw["configs"] = [{"source": "caddyfile", "target": "/etc/caddy/Caddyfile"}]
gw["expose"] = ["80"]
if isinstance(gw.get("environment"), dict):
    gw["environment"].pop("SITE_ADDRESS", None)

for svc in svcs.values():
    dep = svc.get("depends_on")
    if isinstance(dep, dict):
        for d in dep.values():
            if isinstance(d, dict):
                d.pop("required", None)

c["configs"] = {"caddyfile": {"content": caddyfile}}

out = f"{BASE}/deploy/docker-compose.easypanel.yml"
with open(out, "w") as f:
    f.write("# Gerado para EasyPanel (Compose): Caddyfile embutido, sem portas de host,\n")
    f.write("# sem ai-agent/db-backup. NÃO editar à mão — regerar com este script.\n\n")
    yaml.safe_dump(c, f, sort_keys=False, default_flow_style=False, width=120)
print("escrito:", out)
print("servicos:", list(svcs.keys()))
