# Upgrade para a v0.10.0 (pronto pra revisão)

O merge do upstream `v0.10.0` no fork EVUP já foi feito e **validado** no branch
**`evup-v0.10`** (frontend compila, backend compila, o app sobe no dev).
As customizações EVUP foram **preservadas**: tema dark EVUP, logo, pt-BR como
padrão, aviso de versão na home e a nova página **Novidades**.

> Bônus: o PR de tradução pt-BR (#266) foi **aceito e mergeado no upstream** —
> o pt-BR agora é oficial no Paca. 🎉

## Estado atual
- **Produção (EasyPanel)**: ainda na `v0.9.4-evup.1` (intacta, funcionando).
- **`evup`** (branch): produção atual.
- **`evup-v0.10`** (branch): a atualização, pronta pra testar/deploy. O ambiente
  de dev (localhost:3000) está rodando este branch.

## Como testar antes de subir
No dev já está no `evup-v0.10`. Faça login e confira: board, sprints, a página
Novidades, o tema e o pt-BR. Se algo estranho, me chame.

## Como subir a v0.10.0 em produção (quando aprovar)

1. **Incorporar no branch de produção:**
   ```bash
   git checkout evup && git merge evup-v0.10
   ```
2. **⚠️ NOVO segredo obrigatório na v0.10.0:** o backend agora **exige**
   `AI_AGENT_INTERNAL_KEY` (vem de `INTERNAL_API_KEY`). Adicione ao
   `.env.producao` (e no Environment do EasyPanel):
   ```bash
   echo "INTERNAL_API_KEY=$(openssl rand -hex 32)" >> paca-evup-deploy/.env.producao
   ```
   Sem isso, o `api` não sobe (sai com "AI_AGENT_INTERNAL_KEY must be set").
3. **Nova tag → o Actions builda as imagens:**
   ```bash
   git tag v0.10.0-evup.1 && git push personal v0.10.0-evup.1
   ```
   (as imagens `ghcr.io/vhervatin/paca-*:0.10.0-evup.1` já nascem públicas,
   pois o package já é público)
4. **No EasyPanel** (serviço `paca`, aba Ambiente): troque as tags
   `PACA_*_IMAGE` para `:0.10.0-evup.1`, ajuste `PACA_VERSION=v0.10.0-evup.1`,
   e adicione a linha `INTERNAL_API_KEY=...`. Depois **Implantar**.

   > Lembrete: no EasyPanel usamos o compose com valores embutidos
   > (`docker-compose.easypanel.final.yml`). Regenere-o do novo `.env.producao`:
   > `docker compose --env-file paca-evup-deploy/.env.producao -f deploy/docker-compose.easypanel.yml config > paca-evup-deploy/docker-compose.easypanel.final.yml`
   > (removendo a linha `name:` do topo), e cole no campo Conteúdo.

5. Após subir, o banco de produção **não muda** (migrações rodam sozinhas). As
   54 tarefas do Botoclinic continuam lá.

## Reverter (se algo der errado)
No EasyPanel, volte as tags `PACA_*_IMAGE` para `:0.9.4-evup.1` e Implantar.
Os dados ficam intactos (volumes).
