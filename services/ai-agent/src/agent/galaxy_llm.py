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

A SECOND token is minted per conversation for the built-in Paca MCP server
(``mint_paca_mcp_token``): same mint endpoint and signing key, but
``aud=paca-api`` and ``act_as`` = the triggering user's Vortex OIDC sub
(NOT email — Paca's galaxy_bearer resolves principals via ``users.oidc_sub``).
It is delivered into the sandbox as ``PACA_MCP_TOKEN`` with
``PACA_AUTH_MODE=bearer`` (see builder.build_mcp_config), replacing the
legacy X-API-Key/X-Agent-ID header pair the API rejects by design.
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

# The identity mint endpoint's hard TTL ceiling (C2 hardening: a leaked
# INTERNAL_SERVICE_SECRET can only mint tokens capped at 15 minutes —
# identity_service/api/internal.py: ``ttl = max(30, min(ttl, 900))``).
# Requests above it are silently clamped, so we cap explicitly instead of
# pretending a longer token was granted.
_IDENTITY_MAX_TTL_SECONDS = 900


async def _fetch_actor_row(actor_member_id: str | None):
    """The project_members→users row for a HUMAN actor, or None.

    None when the trigger has no actor, the actor is another agent, the
    member row is gone, or the lookup errors — attribution must never take a
    conversation down; the token mint itself is the fail-closed step.
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
    return row


async def resolve_act_as_principal(actor_member_id: str | None) -> str | None:
    """The signed ``act_as`` value for this conversation's LLM token, or None.

    None (claim omitted, billed to the service subject) when the trigger has
    no actor, the actor is another agent, the member row is gone, or the user
    has neither email nor a Vortex ``oidc_sub``.
    """
    row = await _fetch_actor_row(actor_member_id)
    if row is None:
        return None
    # Email over UUID, matching PM (`bind_user(actor=email or user_id)`).
    return row["email"] or row["oidc_sub"] or None


async def resolve_mcp_act_as_sub(actor_member_id: str | None) -> str | None:
    """The signed ``act_as`` value for this conversation's Paca MCP token.

    STRICTLY the user's Vortex OIDC sub: Paca's galaxy_bearer middleware
    resolves principals via ``users.oidc_sub`` (populated at SSO login), so
    an email would never match.  No fallback — a trigger user who has never
    signed in via SSO yields None, the token is minted without ``act_as``,
    its service subject resolves to no local user, and the API fails closed
    (401 → the MCP server exposes zero tools with a clear error).  Agents
    must be triggered by SSO users for tool write-backs — documented in
    deploy/galaxy/README.md.
    """
    row = await _fetch_actor_row(actor_member_id)
    if row is None:
        return None
    return row["oidc_sub"] or None


def _mint_base_url() -> str:
    if settings.galaxy_identity_url.strip():
        return settings.galaxy_identity_url.strip().rstrip("/")
    base = settings.galaxy_ai_proxy_url.strip().rstrip("/")
    return base.removesuffix("/ai/v1")


async def _mint_service_token(act_as: str | None, aud: str, ttl_seconds: int) -> str:
    """Mint an RS256 platform token via identity's /internal/mint-service-token.

    Shared by the LLM token (aud=galaxy-ai) and the Paca MCP bearer token
    (aud=paca-api).  Raises RuntimeError (→ conversation FAILED with a clear
    message) when the mint endpoint is unreachable or refuses — fail closed,
    never fall back to an unattributed or self-signed credential.
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
        # Billed as this agent in ai_usage_logs.agent_id; recorded as the
        # acting agent by Paca's galaxy_bearer (attribution only — grants
        # nothing beyond the act_as principal's permissions).
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
                    "aud": aud,
                    "roles": [],
                    "ttl_seconds": ttl_seconds,
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
        "Minted platform token (role=%s, aud=%s, act_as=%s, ttl=%ss)",
        settings.galaxy_ai_role,
        aud,
        act_as or "(service)",
        ttl_seconds,
    )
    return token


async def mint_llm_token(act_as: str | None) -> str:
    """Mint the RS256 bearer token this conversation's LLM calls will carry."""
    return await _mint_service_token(act_as, aud="galaxy-ai", ttl_seconds=_TOKEN_TTL_SECONDS)


def _mcp_token_ttl_seconds() -> int:
    """TTL to request for the Paca MCP bearer token.

    Ideal: the conversation timeout plus a 10-minute buffer, so the token
    outlives any single turn.  Reality: identity clamps every service-token
    TTL to _IDENTITY_MAX_TTL_SECONDS (a deliberate platform C2 bound), so we
    cap explicitly — asking for more would be silently shortened anyway.
    Consequence: MCP write-backs in conversations that run past ~15 minutes
    fail with a clear non-retrying 401 error (same trade-off as the LLM
    token, documented in deploy/galaxy/README.md).
    """
    ideal = settings.conversation_timeout_seconds + 600
    return min(ideal, _IDENTITY_MAX_TTL_SECONDS)


async def mint_paca_mcp_token(act_as_sub: str | None) -> str:
    """Mint the RS256 bearer the sandboxed Paca MCP server presents to the API.

    ``act_as_sub`` must be the triggering user's Vortex OIDC sub (see
    resolve_mcp_act_as_sub) — Paca resolves principals via users.oidc_sub.
    """
    return await _mint_service_token(
        act_as_sub, aud="paca-api", ttl_seconds=_mcp_token_ttl_seconds()
    )


async def galaxy_llm_api_key(actor_member_id: str | None) -> str | None:
    """The per-conversation LLM api_key for galaxy mode, or None when off."""
    if not settings.galaxy_ai_role.strip():
        return None
    act_as = await resolve_act_as_principal(actor_member_id)
    return await mint_llm_token(act_as)


async def galaxy_mcp_bearer_token(actor_member_id: str | None) -> str | None:
    """The per-conversation Paca MCP bearer token for galaxy mode, or None when off.

    In galaxy mode the built-in Paca MCP server runs with PACA_AUTH_MODE=bearer
    and this token instead of the legacy X-API-Key/X-Agent-ID pair (which the
    API rejects by design, AGENT_HEADER_IMPERSONATION=disabled).  The token is
    minted even when the actor has no Vortex sub — the API then fails closed
    and the MCP server surfaces a clear zero-tools error rather than silently
    impersonating anyone.
    """
    if not settings.galaxy_ai_role.strip():
        return None
    act_as_sub = await resolve_mcp_act_as_sub(actor_member_id)
    if not act_as_sub:
        logger.warning(
            "Paca MCP bearer: trigger member %s has no Vortex oidc_sub — the "
            "MCP token will carry no act_as and Paca will refuse tool calls "
            "(fail closed; agents must be triggered by SSO users).",
            actor_member_id or "(none)",
        )
    return await mint_paca_mcp_token(act_as_sub)
