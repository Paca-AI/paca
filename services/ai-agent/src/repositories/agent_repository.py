"""Database access layer for agent configuration."""

from __future__ import annotations

import asyncio
import base64
import ipaddress
import json
import logging
from urllib.parse import urljoin, urlparse

from ..config import settings
from ..core.db import get_pool
from ..models.agent import AgentConfig, AgentMCPServerRow, AgentSkillRow, PluginSkillRef

logger = logging.getLogger(__name__)


def _decrypt_secret(ciphertext: str, label: str = "LLM API key") -> str:
    """Decrypt an AES-256-GCM ciphertext produced by the Go API's secret.Encryptor.

    If ENCRYPTION_KEY is not configured the value is returned as-is (plaintext
    backward-compat mode).  Any decryption error returns an empty string so
    that a clear "missing API key" error surfaces at the LLM call rather than
    the misleading "token expired / incorrect" that results from forwarding the
    raw ciphertext to the provider. `label` is used only to identify the field
    being decrypted in log messages (e.g. "LLM API key", "environment variable").
    """
    if not ciphertext:
        return ciphertext
    if not settings.encryption_key:
        logger.warning(
            "ENCRYPTION_KEY is not set — %s will be used as-is from the "
            "database. If the api service encrypts secrets, set ENCRYPTION_KEY in "
            "the ai-agent environment to the same value.",
            label,
        )
        return ciphertext
    try:
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM

        key = bytes.fromhex(settings.encryption_key)
        raw = base64.b64decode(ciphertext)
        # Go's GCM uses a 12-byte nonce; nonce is prepended to ciphertext+tag.
        nonce_size = 12
        nonce, ct_with_tag = raw[:nonce_size], raw[nonce_size:]
        plaintext = AESGCM(key).decrypt(nonce, ct_with_tag, None)
        return plaintext.decode()
    except Exception as exc:
        logger.error(
            "Failed to decrypt %s — the value will be treated as empty. Verify "
            "that ENCRYPTION_KEY in the ai-agent service matches the api "
            "service. Error: %s",
            label,
            exc,
        )
        return ""


def _is_localhost_hostname(hostname: str) -> bool:
    if hostname.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(hostname).is_loopback
    except ValueError:
        return False


async def _assert_not_private_host(hostname: str) -> None:
    """Raise ValueError if hostname is, or resolves to, a private/internal IP.

    Mirrors apps/mcp/src/plugin-loader.ts's assertNotPrivateHost (SSRF guard
    for plugin-declared URLs) — see resolve_plugin_base_url for why this
    matters here too.
    """
    try:
        ip = ipaddress.ip_address(hostname)
    except ValueError:
        ip = None

    if ip is not None:
        if ip.is_private or ip.is_loopback or ip.is_link_local:
            raise ValueError(f'hostname "{hostname}" is a private/internal IP address')
        return

    loop = asyncio.get_running_loop()
    try:
        infos = await loop.getaddrinfo(hostname, None)
    except OSError as exc:
        raise ValueError(f'failed to resolve hostname "{hostname}": {exc}') from None

    for info in infos:
        resolved_ip = ipaddress.ip_address(info[4][0])
        if resolved_ip.is_private or resolved_ip.is_loopback or resolved_ip.is_link_local:
            raise ValueError(
                f'hostname "{hostname}" resolves to private/internal IP "{resolved_ip}"'
            )


async def resolve_plugin_base_url(base_url: str, gateway_base_url: str) -> str | None:
    """Resolve a plugin-declared skills baseUrl against the gateway, or
    return None (after logging a warning) if the URL is disallowed.

    A plugin manifest is admin-installed but still untrusted input, and
    content fetched from its skills baseUrl is injected into every
    conversation — so this mirrors the safety rules
    apps/mcp/src/plugin-loader.ts's resolveImportUrl already enforces for
    plugin-declared MCP entry URLs: relative URLs resolve against the
    gateway, https:// is allowed only for hosts that don't resolve to a
    private/internal IP (SSRF protection), and http:// is allowed only for
    localhost/loopback or the gateway's own host (e.g. the "gateway" Docker
    service name in local/dev deployments). Unlike the MCP loader, there's no
    file:// case here — this URL is fetched over plain HTTP, never imported
    as code.
    """
    resolved = urljoin(gateway_base_url, base_url)
    parsed = urlparse(resolved)
    hostname = parsed.hostname or ""

    if parsed.scheme == "https":
        try:
            await _assert_not_private_host(hostname)
        except ValueError as exc:
            logger.warning("Plugin skills baseUrl %r rejected: %s", base_url, exc)
            return None
        return resolved

    if parsed.scheme == "http":
        gateway_hostname = urlparse(gateway_base_url).hostname
        if not (
            _is_localhost_hostname(hostname)
            or (gateway_hostname is not None and hostname == gateway_hostname)
        ):
            logger.warning(
                "Plugin skills baseUrl %r uses http:// for host %r, which is neither "
                "localhost nor the configured gateway host %r — rejected",
                base_url,
                hostname,
                gateway_hostname,
            )
            return None
        return resolved

    logger.warning(
        "Plugin skills baseUrl %r has disallowed scheme %r (only http/https are allowed)",
        base_url,
        parsed.scheme,
    )
    return None


async def list_enabled_plugin_skills() -> list[PluginSkillRef]:
    """List every skill declared by an enabled plugin, resolved to a fetchable URL.

    Plugins are instance-global (the plugins table has no project scoping —
    same as MCP tools, see apps/mcp/src/plugin-loader.ts), so this is not
    scoped to a project or agent; every enabled plugin's skills are available
    to every agent's conversations, layered in as an extra set of defaults
    (see executor.run_conversation). Queried fresh on every call — no
    caching, matching the fact the MCP side has none either (a fresh
    apps/mcp subprocess re-fetches the plugin list on every spawn).
    """
    pool = await get_pool()
    rows = await pool.fetch("SELECT name, manifest FROM plugins WHERE enabled = true")

    refs: list[PluginSkillRef] = []
    for row in rows:
        manifest = json.loads(row["manifest"]) if row["manifest"] else {}
        skills = manifest.get("skills")
        if not skills:
            continue
        base_url = skills.get("baseUrl")
        names = skills.get("names") or []
        if not base_url or not names:
            continue
        resolved_base = await resolve_plugin_base_url(base_url, settings.gateway_base_url)
        if resolved_base is None:
            continue
        for name in names:
            url = f"{resolved_base.rstrip('/')}/{name}/SKILL.md"
            refs.append(PluginSkillRef(plugin_name=row["name"], skill_name=name, url=url))
    return refs


async def load_agent_config(agent_id: str) -> AgentConfig | None:
    """Load full agent configuration (agent, MCP servers, skills) from the database."""
    pool = await get_pool()
    row = await pool.fetchrow(
        """
        SELECT
            a.id,
            a.project_id,
            a.system_prompt,
            a.agent_type,
            a.llm_provider,
            a.llm_model,
            a.llm_api_key_secret AS llm_api_key_secret_ref,
            a.llm_base_url,
            a.acp_provider,
            a.acp_command,
            a.max_iterations,
            a.timeout_minutes,
            a.git_committer_name,
            a.git_committer_email
        FROM agents a
        WHERE a.id = $1
        """,
        agent_id,
    )
    if row is None:
        return None

    mcp_rows = await pool.fetch(
        """
        SELECT server_name, transport, url, command, args, env, is_enabled
        FROM agent_mcp_servers
        WHERE agent_id = $1
        """,
        agent_id,
    )
    skill_rows = await pool.fetch(
        """
        SELECT skill_name, skill_content, triggers, is_enabled
        FROM agent_skills
        WHERE agent_id = $1
        """,
        agent_id,
    )
    env_var_rows = await pool.fetch(
        """
        SELECT key, encrypted_value
        FROM agent_environment_variables
        WHERE agent_id = $1
        """,
        agent_id,
    )

    mcp_servers = [
        AgentMCPServerRow(
            server_name=r["server_name"],
            transport=r["transport"],
            url=r["url"],
            command=r["command"],
            args=json.loads(r["args"]) if r["args"] else [],
            env=json.loads(r["env"]) if r["env"] else {},
            is_enabled=r["is_enabled"],
        )
        for r in mcp_rows
    ]
    skills = [
        AgentSkillRow(
            skill_name=r["skill_name"],
            skill_content=r["skill_content"],
            triggers=json.loads(r["triggers"]) if r["triggers"] else [],
            is_enabled=r["is_enabled"],
        )
        for r in skill_rows
    ]
    # A key is omitted rather than injected with an empty value on decrypt
    # failure — a missing env var is unambiguous, while an empty one can be
    # mistaken for a deliberately blank secret by downstream tooling (e.g.
    # git treating an empty GITHUB_TOKEN as "no auth" vs "broken auth").
    env_vars: dict[str, str] = {}
    for r in env_var_rows:
        key = r["key"]
        value = _decrypt_secret(r["encrypted_value"], label=f"environment variable {key}")
        if value:
            env_vars[key] = value

    return AgentConfig(
        agent_id=str(row["id"]),
        # NULL for a global-scope agent — str(None) would wrongly produce the
        # literal string "None", so this must stay conditional.
        project_id=str(row["project_id"]) if row["project_id"] is not None else None,
        system_prompt=row["system_prompt"],
        llm_provider=row["llm_provider"],
        llm_model=row["llm_model"],
        llm_api_key_secret_ref=_decrypt_secret(row["llm_api_key_secret_ref"] or ""),
        llm_base_url=row["llm_base_url"] or "",
        max_iterations=row["max_iterations"],
        timeout_minutes=row["timeout_minutes"] or 30,
        git_committer_name=row["git_committer_name"] or "paca-agent",
        git_committer_email=row["git_committer_email"]
        or "280579135+paca-agent@users.noreply.github.com",
        agent_type=row["agent_type"] or "llm",
        acp_provider=row["acp_provider"],
        acp_command=json.loads(row["acp_command"]) if row["acp_command"] else [],
        mcp_servers=mcp_servers,
        skills=skills,
        plugin_skills=await list_enabled_plugin_skills(),
        env_vars=env_vars,
    )


async def find_agent_by_bridge_token_hash(token_hash: str) -> tuple[str, str | None] | None:
    """Look up the ACP agent whose current local-bridge token hashes to
    token_hash. Used to authenticate an incoming bridge WebSocket connection
    (see src/routes/bridge.py) — a hash miss (wrong/revoked token) or a match
    against a non-ACP or soft-deleted agent both return None.

    Returns (agent_id, project_id) — project_id is None for a global-scope
    ACP agent (it has no single owning project; per-conversation project
    context, when there is one, is resolved separately — see
    conversation_repository.get_conversation_realtime_context).
    """
    pool = await get_pool()
    row = await pool.fetchrow(
        """
        SELECT id, project_id FROM agents
        WHERE acp_bridge_token_hash = $1
          AND agent_type = 'acp'
          AND deleted_at IS NULL
        """,
        token_hash,
    )
    if row is None:
        return None
    return (
        str(row["id"]),
        str(row["project_id"]) if row["project_id"] is not None else None,
    )
