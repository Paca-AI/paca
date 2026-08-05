"""Builders for LLM, skill, and MCP configuration objects."""

from __future__ import annotations

import asyncio
import logging
import tempfile
from pathlib import Path

import httpx
from openhands.sdk import LLM
from openhands.sdk.context import KeywordTrigger, Skill
from openhands.sdk.skills import load_skills_from_dir
from pydantic import SecretStr

from .. import llm_catalog
from ..config import settings
from ..models.agent import AgentConfig, AgentMCPServerRow, AgentSkillRow, PluginSkillRef

logger = logging.getLogger(__name__)

_default_skills_cache: tuple[Skill, ...] | None = None
_default_skills_lock = asyncio.Lock()


async def load_default_skills() -> tuple[Skill, ...]:
    """Fetch and parse Paca's default ("agent" flavor) skill set from the
    Paca API (GET /api/v1/skills?target=agent).

    ai-agent has no skills directory of its own — this content is hardcoded
    directly in services/api's bundledskills package
    (internal/platform/bundledskills/bundledskills.go), the same place the
    CLI installer's "cli" flavor is served from, so there's a single place
    skill content is authored and versioned instead of independently
    maintained copies that could drift.

    Cached after the first successful call for the lifetime of this worker
    process — the content is static per deployed API version, so there's no
    reason to re-fetch on every conversation. A failed fetch is *not*
    cached, so a transient failure (e.g. racing an API restart) is retried
    on the next conversation rather than permanently breaking every
    subsequent one in this process.
    """
    global _default_skills_cache
    if _default_skills_cache is not None:
        return _default_skills_cache

    async with _default_skills_lock:
        if _default_skills_cache is not None:
            return _default_skills_cache

        async with httpx.AsyncClient(timeout=10.0) as client:
            response = await client.get(
                f"{settings.api_base_url}/api/v1/skills", params={"target": "agent"}
            )
            response.raise_for_status()
        entries = response.json()["data"]["skills"]

        with tempfile.TemporaryDirectory(prefix="paca-default-skills-") as tmp_dir:
            tmp_path = Path(tmp_dir)
            for entry in entries:
                # Written to the exact relative path the API reports (e.g.
                # "paca-do/SKILL.md", or the one legacy exception "paca.md")
                # rather than a fixed "<name>.md"/"<name>/SKILL.md" guess —
                # load_skills_from_dir categorizes a skill as legacy
                # (trigger=None, always active) vs AgentSkills-format purely
                # from whether the file is literally named SKILL.md, so the
                # on-disk layout has to match the source exactly.
                dest = tmp_path / entry["path"]
                dest.parent.mkdir(parents=True, exist_ok=True)
                dest.write_text(entry["content"], encoding="utf-8")

            repo_skills, knowledge_skills, agent_skills = load_skills_from_dir(tmp_path)
            skills = (
                tuple(repo_skills.values())
                + tuple(knowledge_skills.values())
                + tuple(agent_skills.values())
            )

        _default_skills_cache = skills
        return skills


async def load_plugin_skills(refs: list[PluginSkillRef]) -> list[Skill]:
    """Fetch and parse plugin-contributed skills for one conversation.

    Each ref points at an enabled plugin's <skill-name>/SKILL.md, extracted
    from the plugin's install artifact and served statically by the gateway
    (see docs/plugins/skills-plugin-system.md). Fetched fresh on every call —
    no caching, matching the fact the analogous MCP path has none either (a
    fresh apps/mcp subprocess re-fetches the plugin list on every spawn). A
    failure fetching or parsing any single skill is logged and skipped
    rather than raised, so one broken plugin skill can't break every
    conversation for every agent.
    """
    if not refs:
        return []

    result: list[Skill] = []
    async with httpx.AsyncClient(timeout=10.0) as client:
        for ref in refs:
            try:
                response = await client.get(ref.url)
                response.raise_for_status()
            except httpx.HTTPError as exc:
                logger.warning(
                    "Failed to fetch plugin skill %s/%s from %s: %s",
                    ref.plugin_name,
                    ref.skill_name,
                    ref.url,
                    exc,
                )
                continue

            try:
                with tempfile.TemporaryDirectory(prefix="paca-plugin-skill-") as tmp_dir:
                    # Skill.load derives the skill name from the parent
                    # directory of SKILL.md, so the on-disk layout has to
                    # match the AgentSkills convention even though the
                    # content only lives in this temp dir for the duration
                    # of the parse.
                    skill_dir = Path(tmp_dir) / ref.skill_name
                    skill_dir.mkdir(parents=True, exist_ok=True)
                    skill_md_path = skill_dir / "SKILL.md"
                    skill_md_path.write_text(response.text, encoding="utf-8")
                    result.append(Skill.load(skill_md_path, strict=True))
            except Exception as exc:
                logger.warning(
                    "Failed to parse plugin skill %s/%s: %s",
                    ref.plugin_name,
                    ref.skill_name,
                    exc,
                )
    return result


def build_llm(agent_config: AgentConfig) -> LLM:
    """Construct an OpenHands SDK LLM instance from agent configuration."""
    provider = agent_config.llm_provider
    llm_base_url = agent_config.llm_base_url or None

    # For providers outside our own catalog (freeform "Custom…" entries), route
    # through the OpenAI-compatible client by prefixing with "openai/". Checking
    # against our catalog rather than litellm.provider_list matters: litellm
    # reserves names like "custom"/"vllm"/"huggingface" for its own non-chat,
    # non-OpenAI-compatible handlers, and a user typing one of those by
    # coincidence would otherwise get silently misrouted into them.
    if llm_base_url and provider not in llm_catalog.load():
        model_str = (
            agent_config.llm_model
            if agent_config.llm_model.startswith("openai/")
            else f"openai/{agent_config.llm_model}"
        )
    else:
        model_str = f"{provider}/{agent_config.llm_model}"

    key_val = agent_config.llm_api_key_secret_ref or ""
    logger.info(
        "LLM config — model=%s base_url=%s api_key_set=%s",
        model_str,
        llm_base_url or "(none)",
        bool(key_val),
    )
    return LLM(
        model=model_str,
        api_key=SecretStr(key_val),
        base_url=llm_base_url,
        stream=True,
    )


def build_skills(db_skills: list[AgentSkillRow]) -> list[Skill]:
    """Convert DB skill rows into OpenHands SDK Skill objects.

    A skill with no configured triggers keeps `trigger=None` (always-active,
    matching existing behavior) rather than gaining a slash keyword — slash
    invocation only makes sense for skills that were already opted into
    trigger-based (on-demand) activation, since an always-active skill's
    content is already in context on every turn.
    """
    result = []
    for row in db_skills:
        if not row.is_enabled:
            continue
        content = row.skill_content or ""
        if row.triggers:
            keywords = list(row.triggers)
            slash = f"/{row.skill_name}"
            if slash not in keywords:
                keywords.append(slash)
            trigger = KeywordTrigger(keywords=keywords)
        else:
            trigger = None
        result.append(Skill(name=row.skill_name, content=content, trigger=trigger))
    return result


def build_mcp_config(
    db_servers: list[AgentMCPServerRow],
    agent_id: str,
    project_id: str | None,
    actor_user_id: str | None = None,
) -> dict:
    """Build the MCP server configuration dict for the OpenHands SDK.

    Returns a flat ``{server_name: server_config}`` map — the shape
    ``Agent(mcp_config=...)`` expects directly (no ``mcpServers`` wrapper).

    User-configured servers come first; the built-in Paca MCP server is always
    appended last so it cannot be overridden by user entries.

    project_id is None for a global-chat conversation (no project context) —
    PACA_PROJECT_ID is omitted entirely rather than sent empty, which puts
    the spawned apps/mcp process into its "unpinned" mode: each tool call may
    target any project the agent has permission in, rather than being locked
    to one (see apps/mcp/src/index.ts's PACA_AGENT_ID/PACA_PROJECT_ID
    handling).

    actor_user_id is the human the conversation's trigger names (e.g. a
    global-chat message) — forwarded as PACA_ACTOR_USER_ID so tool calls like
    "create a project for me" attribute the result to that human rather than
    the shared agent-bot identity. Safe to trust here because this whole
    process tree is server-side and this value came from DB-verified trigger
    data, never from LLM-controllable tool-call arguments (see
    services/api's parseActorUserIDHeader for the matching server-side check).
    """
    servers: dict = {}

    for row in db_servers:
        if not row.is_enabled:
            continue
        if row.transport == "stdio":
            servers[row.server_name] = {
                "command": row.command,
                "args": row.args,
                "env": row.env,
            }
        else:
            servers[row.server_name] = {
                "url": row.url,
                **({"auth": {"strategy": "oauth2"}} if row.transport == "oauth" else {}),
            }

    if settings.paca_api_key:
        if settings.dev_mcp_path:
            command, args = "node", [settings.dev_mcp_path]
        else:
            command, args = "npx", ["-y", "@paca-ai/paca-mcp"]
        env = {
            "PACA_API_KEY": settings.paca_api_key,
            "PACA_API_URL": settings.api_base_url,
            "PACA_GATEWAY_URL": settings.gateway_base_url,
            "PACA_AGENT_ID": agent_id,
        }
        if project_id is not None:
            env["PACA_PROJECT_ID"] = project_id
        if actor_user_id is not None:
            env["PACA_ACTOR_USER_ID"] = actor_user_id
        servers["paca"] = {
            "command": command,
            "args": args,
            "env": env,
        }

    return servers
