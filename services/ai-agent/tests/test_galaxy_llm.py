"""Tests for platform AI-role routing: act_as resolution + token mint (ADR-038 T3)."""

from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest

from src.agent import galaxy_llm
from src.agent.galaxy_llm import (
    _mint_base_url,
    galaxy_llm_api_key,
    mint_llm_token,
    resolve_act_as_principal,
)

# ─── Helpers ──────────────────────────────────────────────────────────────────


MEMBER_ID = "11111111-1111-1111-1111-111111111111"


def _pool_returning(row):
    pool = MagicMock()
    pool.fetchrow = AsyncMock(return_value=row)
    return pool


def _galaxy_settings(monkeypatch, **overrides):
    values = {
        "galaxy_ai_role": "paca-ai",
        "galaxy_ai_proxy_url": "http://nexus-identity:8086/ai/v1",
        "galaxy_identity_url": "",
        "galaxy_internal_service_secret": "platform-secret",
        "galaxy_service_subject": "paca-service@galaxy.internal.nexus",
    }
    values.update(overrides)
    for name, value in values.items():
        monkeypatch.setattr(galaxy_llm.settings, name, value)


class _FakeResponse:
    def __init__(self, status_code=200, body=None):
        self.status_code = status_code
        self._body = body or {}

    def json(self):
        return self._body


class _FakeAsyncClient:
    """Stands in for httpx.AsyncClient; records the request it was given."""

    def __init__(self, response=None, error=None):
        self.response = response
        self.error = error
        self.calls: list[dict] = []

    def __call__(self, *args, **kwargs):  # constructor stand-in
        return self

    async def __aenter__(self):
        return self

    async def __aexit__(self, *exc):
        return False

    async def post(self, url, **kwargs):
        self.calls.append({"url": url, **kwargs})
        if self.error is not None:
            raise self.error
        return self.response


# ─── resolve_act_as_principal ────────────────────────────────────────────────


async def test_no_actor_member_id_resolves_to_none():
    assert await resolve_act_as_principal(None) is None
    assert await resolve_act_as_principal("") is None


async def test_agent_actor_resolves_to_none():
    row = {"is_agent": True, "email": None, "oidc_sub": None}
    with patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))):
        assert await resolve_act_as_principal(MEMBER_ID) is None


async def test_human_actor_prefers_email_over_oidc_sub():
    row = {"is_agent": False, "email": "alice@example.com", "oidc_sub": "sub-uuid"}
    with patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))):
        assert await resolve_act_as_principal(MEMBER_ID) == "alice@example.com"


async def test_human_actor_without_email_falls_back_to_oidc_sub():
    row = {"is_agent": False, "email": None, "oidc_sub": "sub-uuid"}
    with patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))):
        assert await resolve_act_as_principal(MEMBER_ID) == "sub-uuid"


async def test_missing_member_row_resolves_to_none():
    with patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(None))):
        assert await resolve_act_as_principal(MEMBER_ID) is None


async def test_lookup_error_resolves_to_none_not_raise():
    # Attribution must never take a conversation down — the mint is the
    # fail-closed step, not the principal lookup.
    with patch.object(galaxy_llm, "get_pool", AsyncMock(side_effect=RuntimeError("db down"))):
        assert await resolve_act_as_principal(MEMBER_ID) is None


# ─── _mint_base_url ──────────────────────────────────────────────────────────


def test_mint_base_url_derived_from_proxy_url(monkeypatch):
    _galaxy_settings(monkeypatch)
    assert _mint_base_url() == "http://nexus-identity:8086"


def test_mint_base_url_prefers_explicit_identity_url(monkeypatch):
    _galaxy_settings(monkeypatch, galaxy_identity_url="http://other-identity:9999/")
    assert _mint_base_url() == "http://other-identity:9999"


# ─── mint_llm_token ──────────────────────────────────────────────────────────


async def test_mint_sends_attribution_contract(monkeypatch):
    _galaxy_settings(monkeypatch)
    fake = _FakeAsyncClient(response=_FakeResponse(200, {"access_token": "rs256-token"}))
    with patch.object(galaxy_llm.httpx, "AsyncClient", fake):
        token = await mint_llm_token("alice@example.com")

    assert token == "rs256-token"
    call = fake.calls[0]
    assert call["url"] == "http://nexus-identity:8086/internal/mint-service-token"
    assert call["headers"]["X-Service-Secret"] == "platform-secret"
    body = call["json"]
    assert body["sub"] == "paca-service@galaxy.internal.nexus"
    assert body["aud"] == "galaxy-ai"
    assert body["roles"] == []
    assert body["ttl_seconds"] == 900
    assert body["extra"]["act_as"] == "alice@example.com"
    assert body["extra"]["act_as_agent"] == "paca-ai"


async def test_mint_omits_act_as_when_unknown(monkeypatch):
    # An absent claim honestly says "we do not know" (PM attribution pattern).
    _galaxy_settings(monkeypatch)
    fake = _FakeAsyncClient(response=_FakeResponse(200, {"access_token": "t"}))
    with patch.object(galaxy_llm.httpx, "AsyncClient", fake):
        await mint_llm_token(None)
    assert "act_as" not in fake.calls[0]["json"]["extra"]
    assert fake.calls[0]["json"]["extra"]["act_as_agent"] == "paca-ai"


async def test_mint_refused_raises_without_leaking_secret(monkeypatch):
    _galaxy_settings(monkeypatch)
    fake = _FakeAsyncClient(response=_FakeResponse(401, {}))
    with patch.object(galaxy_llm.httpx, "AsyncClient", fake):
        with pytest.raises(RuntimeError) as err:
            await mint_llm_token("alice@example.com")
    assert "401" in str(err.value)
    assert "platform-secret" not in str(err.value)


async def test_mint_unreachable_raises_runtime_error(monkeypatch):
    _galaxy_settings(monkeypatch)
    fake = _FakeAsyncClient(error=httpx.ConnectError("no route"))
    with patch.object(galaxy_llm.httpx, "AsyncClient", fake):
        with pytest.raises(RuntimeError) as err:
            await mint_llm_token(None)
    assert "galaxy_network" in str(err.value)


async def test_mint_without_secret_fails_closed(monkeypatch):
    _galaxy_settings(monkeypatch, galaxy_internal_service_secret="")
    with pytest.raises(RuntimeError):
        await mint_llm_token("alice@example.com")


async def test_mint_empty_token_body_raises(monkeypatch):
    _galaxy_settings(monkeypatch)
    fake = _FakeAsyncClient(response=_FakeResponse(200, {}))
    with patch.object(galaxy_llm.httpx, "AsyncClient", fake):
        with pytest.raises(RuntimeError):
            await mint_llm_token(None)


# ─── galaxy_llm_api_key ──────────────────────────────────────────────────────


async def test_galaxy_mode_off_returns_none(monkeypatch):
    _galaxy_settings(monkeypatch, galaxy_ai_role="")
    assert await galaxy_llm_api_key(MEMBER_ID) is None


async def test_galaxy_mode_on_resolves_then_mints(monkeypatch):
    _galaxy_settings(monkeypatch)
    row = {"is_agent": False, "email": "bob@example.com", "oidc_sub": None}
    fake = _FakeAsyncClient(response=_FakeResponse(200, {"access_token": "tok"}))
    with (
        patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))),
        patch.object(galaxy_llm.httpx, "AsyncClient", fake),
    ):
        assert await galaxy_llm_api_key(MEMBER_ID) == "tok"
    assert fake.calls[0]["json"]["extra"]["act_as"] == "bob@example.com"


# ─── resolve_mcp_act_as_sub / mint_paca_mcp_token / galaxy_mcp_bearer_token ──


from src.agent.galaxy_llm import (  # noqa: E402
    _mcp_token_ttl_seconds,
    galaxy_mcp_bearer_token,
    resolve_mcp_act_as_sub,
)


async def test_mcp_act_as_uses_oidc_sub_never_email():
    # Paca resolves principals via users.oidc_sub — an email would never match.
    row = {"is_agent": False, "email": "alice@example.com", "oidc_sub": "sub-uuid"}
    with patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))):
        assert await resolve_mcp_act_as_sub(MEMBER_ID) == "sub-uuid"


async def test_mcp_act_as_none_when_user_has_no_oidc_sub():
    # Non-SSO user: NO email fallback — fail closed at the API instead.
    row = {"is_agent": False, "email": "alice@example.com", "oidc_sub": None}
    with patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))):
        assert await resolve_mcp_act_as_sub(MEMBER_ID) is None


async def test_mcp_act_as_none_for_agent_actor():
    row = {"is_agent": True, "email": None, "oidc_sub": "agent-sub"}
    with patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))):
        assert await resolve_mcp_act_as_sub(MEMBER_ID) is None


def test_mcp_token_ttl_is_conversation_timeout_plus_buffer_capped(monkeypatch):
    # Ideal = conversation timeout + 10 min, but identity hard-clamps service
    # tokens to 900s (C2 bound) — we cap explicitly rather than pretending.
    monkeypatch.setattr(galaxy_llm.settings, "conversation_timeout_seconds", 120)
    assert _mcp_token_ttl_seconds() == 120 + 600
    monkeypatch.setattr(galaxy_llm.settings, "conversation_timeout_seconds", 3600)
    assert _mcp_token_ttl_seconds() == 900


async def test_mcp_mint_sends_paca_aud_and_sub_act_as(monkeypatch):
    _galaxy_settings(monkeypatch)
    monkeypatch.setattr(galaxy_llm.settings, "conversation_timeout_seconds", 3600)
    row = {"is_agent": False, "email": "cao.phan@galaxytechnology.vn", "oidc_sub": "debe7c24-sub"}
    fake = _FakeAsyncClient(response=_FakeResponse(200, {"access_token": "mcp-tok"}))
    with (
        patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))),
        patch.object(galaxy_llm.httpx, "AsyncClient", fake),
    ):
        assert await galaxy_mcp_bearer_token(MEMBER_ID) == "mcp-tok"
    body = fake.calls[0]["json"]
    assert body["aud"] == "paca-api"
    assert body["ttl_seconds"] == 900
    # act_as is the Vortex OIDC sub, NOT the email (users.oidc_sub mapping).
    assert body["extra"]["act_as"] == "debe7c24-sub"
    assert body["extra"]["act_as_agent"] == "paca-ai"


async def test_mcp_token_none_when_galaxy_mode_off(monkeypatch):
    _galaxy_settings(monkeypatch, galaxy_ai_role="")
    assert await galaxy_mcp_bearer_token(MEMBER_ID) is None


async def test_mcp_token_minted_without_act_as_for_non_sso_actor(monkeypatch):
    # Fail-closed delivery: the token still ships, Paca rejects it (401), the
    # MCP server surfaces a clear zero-tools error — never impersonation.
    _galaxy_settings(monkeypatch)
    row = {"is_agent": False, "email": "nobody@example.com", "oidc_sub": None}
    fake = _FakeAsyncClient(response=_FakeResponse(200, {"access_token": "t"}))
    with (
        patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))),
        patch.object(galaxy_llm.httpx, "AsyncClient", fake),
    ):
        assert await galaxy_mcp_bearer_token(MEMBER_ID) == "t"
    assert "act_as" not in fake.calls[0]["json"]["extra"]


async def test_mcp_mint_refused_raises(monkeypatch):
    _galaxy_settings(monkeypatch)
    row = {"is_agent": False, "email": None, "oidc_sub": "s"}
    fake = _FakeAsyncClient(response=_FakeResponse(403, {}))
    with (
        patch.object(galaxy_llm, "get_pool", AsyncMock(return_value=_pool_returning(row))),
        patch.object(galaxy_llm.httpx, "AsyncClient", fake),
    ):
        with pytest.raises(RuntimeError) as err:
            await galaxy_mcp_bearer_token(MEMBER_ID)
    assert "403" in str(err.value)
