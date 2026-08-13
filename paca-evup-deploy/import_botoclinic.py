#!/usr/bin/env python3
"""Importador reutilizável do backlog Botoclinic para o Paca.

Uso (prod local):   python3 import_botoclinic.py
Uso (VPS):          PACA_BASE=https://paca.SEU-DOMINIO.com.br \\
                    PACA_USER=admin PACA_PASS=<senha-admin-prod> \\
                    python3 import_botoclinic.py

Faz login (cookie), garante o projeto "Botoclinic", resolve os status do
board por categoria e cria as tarefas (título = coluna D, status = coluna E).
"""
import json, os, urllib.request, urllib.error, http.cookiejar

BASE = os.environ.get("PACA_BASE", "http://localhost").rstrip("/") + "/api/v1"
USER = os.environ.get("PACA_USER", "admin")
PASS = os.environ.get("PACA_PASS", "adminpassword")
PROJECT_NAME = "Botoclinic"

# coluna E (status_raw) -> categoria do status no board do Paca
CAT_MAP = {
    "Ag. Analise": "backlog",
    "Planejado":   "todo",
    "Concluido":   "done",
    "Cancelado":   "done",
}

jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

def call(method, path, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    with opener.open(req, timeout=20) as r:
        body = r.read().decode()
        return json.loads(body)["data"] if body else None

# 1) login (guarda cookie access_token)
call("POST", "/auth/login", {"username": USER, "password": PASS, "remember_me": True})
print(f"login OK em {BASE}")

# 2) garante o projeto Botoclinic
projects = call("GET", "/projects").get("items", [])
proj = next((p for p in projects if p["name"] == PROJECT_NAME), None)
if proj is None:
    proj = call("POST", "/projects", {"name": PROJECT_NAME})
    print(f"projeto criado: {PROJECT_NAME} ({proj['id']})")
else:
    print(f"projeto existente: {PROJECT_NAME} ({proj['id']})")
pid = proj["id"]

# 3) mapa categoria -> status_id
statuses = call("GET", f"/projects/{pid}/task-statuses").get("items", [])
by_cat = {s["category"]: s["id"] for s in statuses}
print("status do board:", {s["name"]: s["category"] for s in statuses})

# 4) tarefas ja existentes (idempotencia — permite re-rodar sem duplicar)
existing = call("GET", f"/projects/{pid}/tasks?page_size=200").get("items", [])
existing_titles = {t["title"] for t in existing}
print(f"tarefas ja no projeto: {len(existing_titles)}")

# 5) importa
with open("botoclinic_tasks.json", encoding="utf-8") as f:
    tasks = json.load(f)

ok, fail, skipped, errors = 0, 0, 0, []
for t in tasks:
    if t["title"] in existing_titles:
        skipped += 1; continue
    cat = CAT_MAP.get(t["status_raw"])
    sid = by_cat.get(cat)
    if not sid:
        fail += 1; errors.append((t["title"], f"sem status p/ {t['status_raw']!r}")); continue
    try:
        d = call("POST", f"/projects/{pid}/tasks", {"title": t["title"], "status_id": sid})
        ok += 1
        print(f"OK #{d['task_number']:>2} [{t['status_raw']:<11}] {t['title'][:60]}")
    except urllib.error.HTTPError as e:
        fail += 1; errors.append((t["title"], f"HTTP {e.code}: {e.read().decode()[:150]}"))

print(f"\n=== {ok} criadas, {skipped} ja existiam, {fail} falhas de {len(tasks)} ===")
for title, err in errors:
    print(f"  FALHA: {title[:50]} :: {err}")
