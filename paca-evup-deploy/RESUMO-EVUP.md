# Paca EVUP — Resumo geral do projeto

> Estado em que paramos, tudo que foi feito e como operar. Guarde este arquivo.
> Última versão em produção: **v0.10.7-evup.1**.

---

## 1. O que é

Fork do **Paca** (https://github.com/Paca-AI/paca — alternativa open-source a Jira/Trello, self-hosted, com agentes de IA) **customizado para a EVUP**: marca EVUP, português como padrão, tema escuro estilo SharePass, aviso de nova versão e página de novidades.

Repo clonado em: `/Users/vitorhervatinmac/Documents/Build to Learn/PACA/paca`

---

## 2. Ambientes & URLs

| Ambiente | URL | Login | O que é |
|---|---|---|---|
| **Dev (local)** | http://localhost:3000 | `admin` / `adminpassword` | build ao vivo (hot-reload). Subir/parar no seu Mac |
| **Produção (VPS)** | https://paca.evup.com.br | `admin` / senha no `.env.producao` (`ADMIN_PASSWORD`) | EasyPanel na VPS 38.242.198.112 |

Subir o dev (de `.../paca`):
```bash
docker compose -f deploy/docker-compose.dev.yml up -d --build postgres valkey minio api web realtime gateway
```
Parar: `docker compose -f deploy/docker-compose.dev.yml down` (`-v` apaga dados). O dev **não** reinicia sozinho se o Docker cair — é só rodar o comando acima de novo.

---

## 3. Repositórios, remotes e branches

**Remotes** (no repo `paca`):
- `upstream` = https://github.com/Paca-AI/paca (projeto original — fonte das atualizações)
- `origin` = https://github.com/vhervatin/paca (fork **público** — usado para os PRs)
- `personal` = https://github.com/vhervatin/paca-evup (repo **privado** — deploy/CI das imagens)

**Branches:**
- `master` — espelho limpo do upstream (NÃO customizar aqui).
- `evup` — **todas as customizações EVUP**; é o que roda na VPS.
- `feat/add-pt-br-locale` — PR do pt-BR (**#266, já MERGED** no upstream).
- `feat/release-changelog` — PR da página Novidades (**#294, aberto**, aguardando o mantenedor).

---

## 4. Customizações EVUP (branch `evup`)

- **Tema "EVUP Dark"** (paleta do SharePass) em `apps/web/src/index.css` bloco `.dark`; dark forçado como padrão (`index.html` + `use-theme-mode.ts`).
- **Logo EVUP** (`apps/web/public/evup-logo.png`) na sidebar após "paca", no login e no favicon.
- **pt-BR** completo (9 namespaces em `apps/web/src/i18n/locales/pt-BR/`) e definido como padrão.
- **Aviso de nova versão** na home: endpoint `GET /api/v1/version` + banner `apps/web/src/components/home/UpdateBanner.tsx`.
- **Página Novidades** (changelog): `GET /api/v1/releases` + `apps/web/src/routes/_authenticated/admin/changelog/index.tsx`, item na sidebar (Administração). Renderer de markdown com `safeHref` (links só http/https/mailto).
- Config por env no `api`: `PACA_VERSION`, `RELEASE_UPSTREAM_REPO`, `RELEASE_FORK_REPO`.

---

## 5. Deploy — EasyPanel (VPS)

Roda como serviço **Compose** chamado `paca` **dentro do projeto `evup_geral`** (plano free não cria projeto novo). O domínio aponta pro serviço interno **`gateway`** (Caddy) na porta **80**; o Traefik do EasyPanel faz o HTTPS.

**CI:** `.github/workflows/evup-release.yml` builda e publica no GHCR ao dar push numa tag `v*`. Imagens `ghcr.io/vhervatin/paca-{api,web,realtime}` estão **públicas** → EasyPanel puxa sem senha.

**Importante:** o `deploy/docker-compose.prod.yml` cru **não** funciona no EasyPanel (bind-mount do Caddyfile + portas 80/443 conflitam com o Traefik). Usa-se um compose adaptado + resolvido:
- `deploy/docker-compose.easypanel.yml` — gerado por `paca-evup-deploy/gen_easypanel_compose.py` (Caddyfile embutido como `config`, sem portas de host, sem ai-agent/db-backup).
- `paca-evup-deploy/docker-compose.easypanel.final.yml` — versão **com valores/secrets embutidos** (0 variáveis) que se cola no campo **Conteúdo** do serviço. Gerada via `docker compose config`.

**Cloudflare:** registro **A** `paca` → 38.242.198.112, **DNS only (cinza)** durante a emissão do certificado (proxy laranja quebra o Let's Encrypt).

---

## 6. Como ATUALIZAR (subir versão nova do Paca sem perder o EVUP)

No branch `evup`, de `.../paca`:
```bash
# 1. buscar novidades do upstream
git fetch upstream --tags

# 2. mesclar a versão desejada (ex: v0.10.7)
git merge vX.Y.Z         # resolver conflitos (costuma ser 1-2 arquivos)

# 3. VALIDAR (obrigatório):
#    - go build:
docker run --rm -v "$PWD/services/api":/src -w /src paca-dev-api:latest sh -c "go build ./..."
#    - subir o dev e rodar o smoke test:
docker compose -f deploy/docker-compose.dev.yml up -d --build postgres valkey minio api web realtime gateway
PACA_BASE=http://localhost:3000 PACA_USER=admin PACA_PASS=adminpassword python3 deploy/smoke-test.py

# 4. commit + tag → dispara build das imagens
git commit ... ; git tag vX.Y.Z-evup.1 ; git push personal evup ; git push personal vX.Y.Z-evup.1

# 5. atualizar env + regenerar o compose final
sed -i '' -E -e 's#(paca-(api|web|realtime)):[^[:space:]]*#\1:X.Y.Z-evup.1#' \
             -e 's#^PACA_VERSION=.*#PACA_VERSION=vX.Y.Z-evup.1#' paca-evup-deploy/.env.producao
python3 paca-evup-deploy/gen_easypanel_compose.py
docker compose --env-file paca-evup-deploy/.env.producao -f deploy/docker-compose.easypanel.yml config \
  > /tmp/final.yml   # depois remover a linha 'name:' e salvar em docker-compose.easypanel.final.yml

# 6. EasyPanel: colar o .final.yml no Conteúdo → Implantar
# 7. rodar o smoke test contra produção (passo 8 abaixo)
```
**Por que não se perde nada:** customizações estão no CÓDIGO (vão nas imagens); dados ficam em VOLUMES na VPS (upgrade só troca imagens; migrações rodam sozinhas).

---

## 7. Importação de dados (Botoclinic — 54 tarefas)

Já importadas em dev e prod. Importador **idempotente** em `paca-evup-deploy/import_botoclinic.py` (+ `botoclinic_tasks.json`). Mapeamento: título = coluna D; status = coluna E (Ag. Análise→Backlog, Planejado→Todo, Concluído/Cancelado→Done).
```bash
cd paca-evup-deploy
PACA_BASE=https://paca.evup.com.br PACA_USER=admin \
  PACA_PASS="$(grep '^ADMIN_PASSWORD=' .env.producao | cut -d= -f2)" python3 import_botoclinic.py
```
Cada ambiente tem banco próprio — dados **não** migram entre dev/prod; re-rodar contra o alvo.

---

## 8. Smoke test — SEMPRE após deploy/merge

`deploy/smoke-test.py` valida as rotas-chave (health, version, releases, login, projects, **agents/llm-models**, global-roles, roles/agents). Exit ≠ 0 se algo falhar.
```bash
PACA_BASE=https://paca.evup.com.br PACA_USER=admin \
  PACA_PASS="$(grep '^ADMIN_PASSWORD=' paca-evup-deploy/.env.producao | cut -d= -f2)" \
  python3 deploy/smoke-test.py
```
Bug já pego por ele: `/agents/llm-models` dava 500 sem o serviço ai-agent → corrigido pra degradar a `{}` (200).

---

## 9. PRs pro upstream (Paca-AI/paca)

- **#266 — pt-BR:** ✅ **MERGED** (português é oficial no Paca).
- **#294 — release-awareness (banner + página changelog):** aberto e **MERGEABLE**, CI verde, pullfrog aprovou ("No new issues found"). Aguarda o mantenedor **pikann** (Hải Huỳnh, humano) dar o Approve/merge. Commits com a conta `vhervatin` → você entra como contribuidor.
- Atores no PR: **pikann** = humano (mantenedor) · **pullfrog[bot]** = IA revisora · **vhervatin** = você.
- CI de fork só roda após o mantenedor aprovar (política padrão do GitHub).

---

## 10. Credenciais / arquivos sensíveis

- **Dev:** `admin` / `adminpassword` (fixo no `docker-compose.dev.yml`).
- **Prod:** `admin` / valor de `ADMIN_PASSWORD` em `paca-evup-deploy/.env.producao` (secrets gerados: POSTGRES/JWT/ENCRYPTION/STORAGE). **NÃO versionar** — a pasta `paca-evup-deploy/` é gitignored.
- Se perder o `.env.producao`, os secrets se perdem (não dá pra recuperar) — mantenha backup seguro.

---

## 11. Pendências / próximos passos possíveis

- Aguardar o pikann mergear o **PR #294**.
- (Opcional) Integrar o smoke test no GitHub Actions p/ rodar automático após cada build (precisa de 1 secret: senha admin de prod).
- AI agent está desligado em prod (precisa de Docker socket + chave de LLM) — ligar quando quiser os agentes de IA de fato.

---

## 12. Cheat sheet

```bash
# subir dev
docker compose -f deploy/docker-compose.dev.yml up -d postgres valkey minio api web realtime gateway
# versão em produção
curl -s https://paca.evup.com.br/api/v1/version | python3 -m json.tool
# smoke test prod
PACA_BASE=https://paca.evup.com.br PACA_USER=admin PACA_PASS="$(grep '^ADMIN_PASSWORD=' paca-evup-deploy/.env.producao|cut -d= -f2)" python3 deploy/smoke-test.py
# ver PRs
gh pr view 294 --repo Paca-AI/paca --web
```
