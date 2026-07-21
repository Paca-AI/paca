"""Builders for LLM, skill, and MCP configuration objects."""

from __future__ import annotations

import logging
import tempfile
from functools import lru_cache
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

# services/ai-agent/src/skills — bundled into the Docker image by the
# Dockerfile's existing `COPY src/ ./src/` line.
_DEFAULT_SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


@lru_cache(maxsize=1)
def load_default_skills() -> tuple[Skill, ...]:
    """Load Paca's default skill set, bundled under src/skills/.

    The directory is static per deployment, so this is cached after the
    first call — safe to call on every conversation without re-parsing
    disk each time.
    """
    repo_skills, knowledge_skills, agent_skills = load_skills_from_dir(_DEFAULT_SKILLS_DIR)
    return (
        tuple(repo_skills.values())
        + tuple(knowledge_skills.values())
        + tuple(agent_skills.values())
    )


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
    project_id: str,
) -> dict:
    """Build the MCP server configuration dict for the OpenHands SDK.

    Returns a flat ``{server_name: server_config}`` map — the shape
    ``Agent(mcp_config=...)`` expects directly (no ``mcpServers`` wrapper).

    User-configured servers come first; the built-in Paca MCP server is always
    appended last so it cannot be overridden by user entries.
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
        servers["paca"] = {
            "command": command,
            "args": args,
            "env": {
                "PACA_API_KEY": settings.paca_api_key,
                "PACA_API_URL": settings.api_base_url,
                "PACA_GATEWAY_URL": settings.gateway_base_url,
                "PACA_AGENT_ID": agent_id,
                "PACA_PROJECT_ID": project_id,
            },
        }

    return servers
