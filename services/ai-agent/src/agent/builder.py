"""Builders for LLM, skill, and MCP configuration objects."""

from __future__ import annotations

import logging
from functools import lru_cache
from pathlib import Path

from openhands.sdk import LLM
from openhands.sdk.context import KeywordTrigger, Skill
from openhands.sdk.skills import load_skills_from_dir
from pydantic import SecretStr

from .. import llm_catalog
from ..config import settings
from ..models.agent import AgentConfig, AgentMCPServerRow, AgentSkillRow

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


def build_llm(agent_config: AgentConfig, api_key: str | None = None) -> LLM:
    """Construct an OpenHands SDK LLM instance from agent configuration.

    ``api_key`` is the per-conversation platform token minted by
    ``galaxy_llm.galaxy_llm_api_key`` — only used in GALAXY_AI_ROLE mode,
    where it wins over every stored/override key.
    """
    provider = agent_config.llm_provider
    llm_base_url = agent_config.llm_base_url or None

    # Galaxy platform role routing (ADR-038 T3): model = the AI ROLE name —
    # identity's /ai/v1 resolves it via ai_role_assignments to whatever model
    # the platform admin bound (rebind in /nexus/admin → AI Models, no Paca
    # redeploy). The "openai/" prefix only picks litellm's OpenAI-compatible
    # client; the proxy is OpenAI-compatible regardless of the bound model's
    # real provider. Wins over LLM_BASE_URL_OVERRIDE (a role implies the
    # platform proxy; configuring both would be contradictory).
    if settings.galaxy_ai_role.strip():
        role = settings.galaxy_ai_role.strip()
        key_val = api_key or settings.llm_api_key_override
        if not key_val:
            # Fail closed: without a minted act_as token the proxy would 401
            # anyway — surface the real cause instead of an upstream auth error.
            raise RuntimeError(
                "GALAXY_AI_ROLE is set but no platform token was provided to "
                "build_llm — the executor must mint one per conversation "
                "(see src/agent/galaxy_llm.py)."
            )
        logger.info(
            "GALAXY_AI_ROLE active for agent %s: model=openai/%s via %s "
            "(per-agent provider/model/key/base_url ignored)",
            agent_config.agent_id,
            role,
            settings.galaxy_ai_proxy_url,
        )
        return LLM(
            model=f"openai/{role}",
            api_key=SecretStr(key_val),
            base_url=settings.galaxy_ai_proxy_url,
            stream=True,
        )

    # Platform override (ADR-038): force LLM traffic through the platform
    # proxy regardless of the per-agent llm_base_url stored in the DB.
    if settings.llm_base_url_override:
        logger.info(
            "LLM_BASE_URL_OVERRIDE active for agent %s: routing via %s "
            "(per-agent base_url %r ignored)",
            agent_config.agent_id,
            settings.llm_base_url_override,
            agent_config.llm_base_url or "",
        )
        llm_base_url = settings.llm_base_url_override

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
    if settings.llm_api_key_override:
        logger.info(
            "LLM_API_KEY_OVERRIDE active for agent %s: per-agent API key ignored",
            agent_config.agent_id,
        )
        key_val = settings.llm_api_key_override
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
