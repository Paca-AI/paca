"""Domain model dataclasses for the AI agent service."""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class AgentMCPServerRow:
    server_name: str
    transport: str
    url: str | None
    command: str | None
    args: list[str]
    env: dict[str, str]
    is_enabled: bool


@dataclass
class AgentSkillRow:
    skill_name: str
    skill_content: str | None
    triggers: list[str]
    is_enabled: bool


@dataclass
class PluginSkillRef:
    """A plugin-declared skill's SKILL.md, not yet fetched.

    Resolved from an enabled plugin's manifest.skills block (see
    repositories.agent_repository.list_enabled_plugin_skills) into an
    absolute, safety-checked URL — builder.load_plugin_skills fetches `url`
    and parses it into an SDK Skill.
    """

    plugin_name: str
    skill_name: str
    url: str


@dataclass
class AgentConfig:
    agent_id: str
    project_id: str
    system_prompt: str | None
    llm_provider: str
    llm_model: str
    llm_api_key_secret_ref: str
    llm_base_url: str
    max_iterations: int
    # Minutes an ACP turn may run before the watchdog in acp_dispatch.py fails
    # it — see that module for why this exists (Pub/Sub dispatch has no
    # delivery guarantee, so a dropped start_turn must not leave a
    # conversation stuck at RUNNING forever). Matches the DB default (30);
    # LLM agents enforce their own timeout via executor.py instead.
    timeout_minutes: int = 30
    git_committer_name: str = "paca-agent"
    git_committer_email: str = "280579135+paca-agent@users.noreply.github.com"
    # "llm" (default) or "acp". ACP agents delegate to a coding CLI the user
    # runs themselves via the local bridge (src/agent/acp_bridge.py) instead
    # of calling llm_provider/llm_model directly — see acp_dispatch.py.
    agent_type: str = "llm"
    acp_provider: str | None = None
    acp_command: list[str] = field(default_factory=list)
    mcp_servers: list[AgentMCPServerRow] = field(default_factory=list)
    skills: list[AgentSkillRow] = field(default_factory=list)
    plugin_skills: list[PluginSkillRef] = field(default_factory=list)
    env_vars: dict[str, str] = field(default_factory=dict)
