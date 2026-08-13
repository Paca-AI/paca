# Deploy Paca EVUP no EasyPanel — checklist

Tudo que eu podia preparar já está nesta pasta:
- `.env.producao` — variáveis prontas (com os segredos já gerados)
- `docker-compose.prod.yml` — o stack pra colar
- `import_botoclinic.py` + `botoclinic_tasks.json` — importador das 54 tarefas

Faltam só os passos que exigem o SEU acesso. ~10 min.
Os passos 1–4 podem ser feitos em paralelo; só o Passo 5 exige o DNS já pronto.

---

## Passo 1 — DNS na Cloudflare (FAÇA PRIMEIRO)

O HTTPS do EasyPanel é emitido pelo Let's Encrypt, que valida o domínio na
hora. O DNS precisa existir antes do Passo 5, senão o certificado falha.

1. Pegue o **IP público da VPS** (onde o EasyPanel roda).
2. Na Cloudflare (DNS do `evup.com.br`), crie:
   - **Type:** A · **Name:** `paca` · **Content:** `<IP da VPS>`
   - **Proxy status:** **DNS only** (nuvem **CINZA**) — NÃO Proxied.
3. ⚠️ Deixe em "DNS only" até o certificado ser emitido. O proxy laranja da
   Cloudflare quebra o desafio do Let's Encrypt. Depois que o site abrir em
   https, se quiser o proxy da Cloudflare, ligue a nuvem laranja **e** mude o
   SSL/TLS da Cloudflare para **Full (strict)**.

## Passo 2 — Trocar o domínio no `.env.producao`
Abra `.env.producao` e troque as 3 linhas que têm `paca.SEU-DOMINIO.com.br`
pelo seu domínio real (ex.: `paca.evup.com.br`).

## Passo 3 — Tornar as imagens públicas no GHCR (mais simples)
Assim o EasyPanel baixa as imagens sem precisar de senha/credencial — e esta
versão do painel não tem uma seção "Registries" de qualquer forma.

No GitHub → seu perfil → aba **Packages** (github.com/vhervatin?tab=packages):
1. Clique em **paca-api** → **Package settings** (⚙️, à direita) → role até
   **Danger Zone** → **Change visibility** → **Public** (confirme digitando o nome).
2. Repita para **paca-web** e **paca-realtime**.

As imagens **não contêm segredos** (senhas/keys ficam no `.env` em runtime); o
**código-fonte do repo continua privado**. Só os builds ficam acessíveis.

> Alternativa — manter as imagens privadas: por SSH na VPS, rode
> `docker login ghcr.io -u vhervatin` usando um token `read:packages` (GitHub →
> Settings → Developer settings → Tokens classic → só `read:packages`). O Docker
> do host guarda a credencial e o Compose usa no pull.

## Passo 4 — Criar o serviço
No EasyPanel:
1. **Create Project** → nome `paca`.
2. Dentro do projeto → **+ Service → Compose**.
3. **Content**: cole todo o conteúdo de `docker-compose.prod.yml`.
4. **Environment**: cole todo o conteúdo de `.env.producao` (já com o domínio trocado).

## Passo 5 — Domínio + HTTPS (precisa do DNS do Passo 1 pronto)
No serviço, procure o serviço **gateway** e adicione o **Domain**:
- Host: seu domínio (`paca.SEU-DOMINIO.com.br`)
- Port: **80**
O EasyPanel emite o certificado HTTPS (Let's Encrypt) sozinho.

## Passo 6 — Deploy
Clique **Deploy**. Acompanhe os logs até o serviço `api` mostrar
`starting server` e o `gateway` ficar de pé.

## Passo 7 — Entrar
Abra `https://SEU-DOMINIO`. Login:
- Usuário: `admin`
- Senha: veja `ADMIN_PASSWORD` no `.env.producao`

As migrações do banco rodam sozinhas.

## Passo 8 — Importar as 54 tarefas do Botoclinic (banco de prod começa vazio)
Desta pasta, rodando na sua máquina:
```bash
PACA_BASE=https://paca.SEU-DOMINIO.com.br PACA_USER=admin \
  PACA_PASS='<ADMIN_PASSWORD do .env.producao>' python3 import_botoclinic.py
```
(ou me chame que eu rodo.)

---

### Se algo falhar
- **Certificado HTTPS não emite / erro no domínio** → o registro na Cloudflare
  provavelmente está **Proxied (laranja)**; troque para **DNS only (cinza)** e
  tente de novo no EasyPanel. Confirme também que o A aponta pro IP certo.
- **Imagem não baixa / unauthorized** → o token `read:packages` não foi aceito;
  confira a credencial `ghcr.io` no EasyPanel. (Ou torne os 3 pacotes públicos
  em GitHub → Packages → cada um → Package settings → Public, e aí nem precisa
  de token.)
- **Login não funciona / cookie** → confirme `COOKIE_SECURE=true` e que o acesso
  é por **https** (não http).
- **Página não carrega** → veja os logs do `api` e do `gateway` no EasyPanel.
