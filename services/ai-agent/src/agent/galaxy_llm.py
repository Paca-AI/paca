"""Per-conversation act_as tokens for the Vortex platform AI proxy (ADR-038 T3).

When ``GALAXY_AI_ROLE`` is set, every conversation's LLM traffic goes through
identity's ``/ai/v1`` under that AI role, authenticated with a short-lived
RS256 service token that names the TRIGGERING USER in a signed ``act_as``
claim. The proxy's ``ai_usage_logs`` then attributes cost per user instead of
lumping everything under one service subject — the same contract PM uses
(``pm_service/utils/attribution.py`` + ``routers/ai_service.py``):

* Token: ``POST {identity}/internal/mint-service-token`` with header
  ``X-Service-Secret: <platform INTERNAL_SERVICE_SECRET>``. Identity signs
  RS256 with its OIDC key; the fleet verifies via JWKS. There is deliberately
  NO HS256 self-mint fallback here — Paca does not hold the fleet JWT_SECRET
  and must never grow a copy (ADR-038 T3: no raw per-agent keys, no shared
  signing secrets in the fork). If the mint fails, the conversation fails.
* Claims: ``sub``/``email``/``name`` identify the service;
  ``act_as`` = triggering user (email preferred over UUID — a wall of hex
  identifies nobody on a dashboard, per the PM pattern);
  ``act_as_agent`` = the AI role (surfaces as ``agent_id`` in usage logs).
  An absent ``act_as`` honestly says "we do not know" (agent-triggered or
  unmapped members) and the call is billed to the service subject.
* TTL: identity clamps ``ttl_seconds`` to 30..900. We ask for the max (900s)
  because the token is serialized into the sandbox once per conversation
  start (the OpenHands SDK sends the agent spec — LLM config included — to
  the agent-server, which makes the LLM calls itself). Conversations that
  run past 15 minutes will start getting 401s from the proxy; the runtime
  surfaces that as a failed conversation. Known trade-off, documented in
  deploy/galaxy/README.md.

The principal is resolved from ``TriggerMessage.actor_member_id`` — the
project member who assigned the task / wrote the @mention / sent the chat
message — via ``project_members`` → ``users`` (same read-only join the
galaxy notify-bridge uses).
"""

from __future__ import annotations

import logging

import httpx

from ..config import settings
from ..core.db import get_pool

logger = logging.getLogger(__name__)

# Read-only principal lookup (mirrors deploy/galaxy/notify-bridge SQL_MEMBER).
# LEFT JOIN: an agent member has no users row worth naming; a deleted user
# resolves to NULLs and the claim is honestly omitted.
_SQL_ACTOR = """
SELECT (pm.member_type = 'agent') AS is_agent,
       u.email,
       u.oidc_sub
FROM project_members pm
LEFT JOIN users u ON u.id = pm.user_id AND u.deleted_at IS NULL
WHERE pm.id = $1::uuid
"""

# Identity clamps to 30..900 — ask for the ceiling (see module docstring).
_TOKEN_TTL_SECONDS = 900


async def resolve_act_as_principal(actor_member_id: str | None) -> str | None:
    """The signed ``act_as`` value for this conversation, or None.

    None (claim omitted, billed to the service subject) when the trigger has
    no actor, the actor is another agent, the member row is gone, or the user
    has neither email nor a Vortex ``oidc_sub``. Lookup errors also return
    None — attribution must never take a conversation down; the token mint
    itself is the fail-closed step.
    """
    if not actor_member_id:
        return None
    try:
        pool = await get_pool()
        row = await pool.fetchrow(_SQL_ACTOR, actor_member_id)
    except Exception as exc:
        logger.warning(
            "act_as lookup failed for member %s — proceeding unattributed: %s",
            actor_member_id,
            exc,
        )
        return None
    if row is None or row["is_agent"]:
        return None
    # Email over UUID, matching PM (`bind_user(actor=email or user_id)`).
    return row["email"] or row["oidc_sub"] or None


def _mint_base_url() -> str:
    if settings.galaxy_identity_url.strip():
        return settings.galaxy_identity_url.strip().rstrip("/")
    base = settings.galaxy_ai_proxy_url.strip().rstrip("/")
    return base.removesuffix("/ai/v1")


async def mint_llm_token(act_as: str | None) -> str:
    """Mint the RS256 bearer token this conversation's LLM calls will carry.

    Raises RuntimeError (→ conversation FAILED with a clear message) when the
    mint endpoint is unreachable or refuses — fail closed, never fall back to
    an unattributed or self-signed credential.
    """
    secret = settings.galaxy_internal_service_secret.strip()
    if not secret:
        # config.py fail-fasts on this at startup; guard again for direct calls.
        raise RuntimeError(
            "GALAXY_INTERNAL_SERVICE_SECRET is empty — cannot mint a platform "
            "AI proxy token (GALAXY_AI_ROLE is set)."
        )
    extra: dict[str, str] = {
        "email": settings.galaxy_service_subject,
        "name": "Paca AI Agent",
        # Billed as this agent in ai_usage_logs.agent_id.
        "act_as_agent": settings.galaxy_ai_role,
    }
    if act_as:
        extra["act_as"] = act_as
    url = f"{_mint_base_url()}/internal/mint-service-token"
    try:
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.post(
                url,
                headers={"X-Service-Secret": secret, "Content-Type": "application/json"},
                json={
                    "sub": settings.galaxy_service_subject,
                    "aud": "galaxy-ai",
                    "roles": [],
                    "ttl_seconds": _TOKEN_TTL_SECONDS,
                    "extra": extra,
                },
            )
    except httpx.HTTPError as exc:
        raise RuntimeError(
            f"Platform AI token mint unreachable at {url} — is ai-agent on "
            f"galaxy_network and identity up? ({exc})"
        ) from exc
    if resp.status_code != 200:
        raise RuntimeError(
            f"Platform AI token mint refused (HTTP {resp.status_code}) — check "
            "GALAXY_INTERNAL_SERVICE_SECRET matches the platform "
            "INTERNAL_SERVICE_SECRET."
        )
    token = resp.json().get("access_token")
    if not token:
        raise RuntimeError("Platform AI token mint returned no access_token.")
    logger.info(
        "Minted platform AI token (role=%s, act_as=%s, ttl=%ss)",
        settings.galaxy_ai_role,
        act_as or "(service)",
        _TOKEN_TTL_SECONDS,
    )
    return token


async def galaxy_llm_api_key(actor_member_id: str | None) -> str | None:
    """The per-conversation LLM api_key for galaxy mode, or None when off."""
    if not settings.galaxy_ai_role.strip():
        return None
    act_as = await resolve_act_as_principal(actor_member_id)
    return await mint_llm_token(act_as)
