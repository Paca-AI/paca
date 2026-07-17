"""Tests for LLM / skill / MCP configuration builders."""

from unittest.mock import patch

import pytest

from src.agent.builder import build_llm, build_mcp_config, build_skills, load_default_skills
from src.models.agent import AgentConfig, AgentMCPServerRow, AgentSkillRow

# ─── Fixtures / helpers ───────────────────────────────────────────────────────


def _agent_config(
    provider: str = "anthropic",
    model: str = "claude-sonnet-4-6",
    base_url: str = "",
) -> AgentConfig:
    return AgentConfig(
        agent_id="agent-1",
        project_id="proj-1",
        system_prompt=None,
        llm_provider=provider,
        llm_model=model,
        llm_api_key_secret_ref="secret-ref",
        llm_base_url=base_url,
        max_iterations=10,
    )


@pytest.fixture
def catalog():
    """Stub Paca's known-provider catalog so tests don't depend on data/llm_models.json."""
    with patch("src.agent.builder.llm_catalog.load") as m:
        m.return_value = {"anthropic": {}, "openai": {}}
        yield m


def _skill(
    name: str = "my-skill",
    content: str = "do it",
    triggers: list[str] | None = None,
    enabled: bool = True,
) -> AgentSkillRow:
    return AgentSkillRow(
        skill_name=name,
        skill_content=content,
        triggers=triggers or [],
        is_enabled=enabled,
    )


def _server(
    name: str = "my-server",
    transport: str = "stdio",
    enabled: bool = True,
    url: str | None = None,
    command: str = "node",
    args: list[str] | None = None,
    env: dict | None = None,
) -> AgentMCPServerRow:
    return AgentMCPServerRow(
        server_name=name,
        transport=transport,
        url=url,
        command=command,
        args=args or [],
        env=env or {},
        is_enabled=enabled,
    )


@pytest.fixture
def no_paca_key():
    with patch("src.agent.builder.settings") as m:
        m.paca_api_key = ""
        yield m


@pytest.fixture
def with_paca_key():
    with patch("src.agent.builder.settings") as m:
        m.paca_api_key = "test-api-key"
        m.api_base_url = "http://api:8080"
        m.gateway_base_url = "http://gateway"
        m.dev_mcp_path = ""
        yield m


# ─── build_llm ────────────────────────────────────────────────────────────────


def test_known_provider_with_base_url_uses_native_routing(catalog):
    config = _agent_config(
        provider="anthropic", model="claude-sonnet-4-6", base_url="https://api.anthropic.com"
    )
    llm = build_llm(config)
    assert llm.model == "anthropic/claude-sonnet-4-6"


def test_custom_literal_provider_does_not_collide_with_litellm_builtin(catalog):
    # Regression test for #221: litellm has its own built-in "custom" provider
    # with an incompatible non-chat wire format. A user typing "custom" into the
    # freeform provider field (the obvious choice for "a custom provider") must
    # still get routed through the OpenAI-compatible passthrough, not litellm's
    # native "custom" handler.
    config = _agent_config(
        provider="custom", model="llama-3.1-70b", base_url="http://self-hosted:8000/v1"
    )
    llm = build_llm(config)
    assert llm.model == "openai/llama-3.1-70b"


def test_unknown_freeform_provider_routes_through_openai_passthrough(catalog):
    config = _agent_config(
        provider="my-vllm-box", model="mixtral", base_url="http://10.0.0.5:8000/v1"
    )
    llm = build_llm(config)
    assert llm.model == "openai/mixtral"


def test_model_already_openai_prefixed_is_not_double_prefixed(catalog):
    config = _agent_config(
        provider="custom",
        model="openai/already-prefixed",
        base_url="http://self-hosted:8000/v1",
    )
    llm = build_llm(config)
    assert llm.model == "openai/already-prefixed"


def test_no_base_url_uses_raw_provider_string(catalog):
    config = _agent_config(provider="custom", model="some-model", base_url="")
    llm = build_llm(config)
    assert llm.model == "custom/some-model"


def test_llm_uses_configured_base_url_and_stream(catalog):
    config = _agent_config(
        provider="custom", model="some-model", base_url="http://self-hosted:8000/v1"
    )
    llm = build_llm(config)
    assert llm.base_url == "http://self-hosted:8000/v1"
    assert llm.stream is True


# ─── build_skills ─────────────────────────────────────────────────────────────


def test_enabled_skill_is_included():
    skills = build_skills([_skill("alpha")])
    assert len(skills) == 1
    assert skills[0].name == "alpha"


def test_disabled_skill_is_excluded():
    skills = build_skills([_skill("alpha", enabled=False)])
    assert skills == []


def test_mixed_skills_only_enabled_returned():
    rows = [_skill("a"), _skill("b", enabled=False), _skill("c")]
    skills = build_skills(rows)
    assert [s.name for s in skills] == ["a", "c"]


def test_empty_input_returns_empty_list():
    assert build_skills([]) == []


def test_skill_content_preserved():
    skills = build_skills([_skill(content="be very helpful")])
    assert skills[0].content == "be very helpful"


def test_none_content_becomes_empty_string():
    row = AgentSkillRow(skill_name="s", skill_content=None, triggers=[], is_enabled=True)
    skills = build_skills([row])
    assert skills[0].content == ""


def test_skill_with_triggers_has_trigger_object():
    row = _skill(triggers=["review", "check"])
    skills = build_skills([row])
    assert skills[0].trigger is not None


def test_skill_without_triggers_has_no_trigger():
    row = _skill(triggers=[])
    skills = build_skills([row])
    assert skills[0].trigger is None


def test_skill_with_triggers_gains_slash_keyword():
    row = _skill(name="my-skill", triggers=["review"])
    skills = build_skills([row])
    assert "/my-skill" in skills[0].trigger.keywords
    assert "review" in skills[0].trigger.keywords


def test_skill_with_triggers_does_not_duplicate_slash_keyword():
    row = _skill(name="my-skill", triggers=["/my-skill", "review"])
    skills = build_skills([row])
    assert skills[0].trigger.keywords.count("/my-skill") == 1


# ─── load_default_skills ──────────────────────────────────────────────────────


def test_load_default_skills_is_non_empty():
    skills = load_default_skills()
    assert len(skills) > 0


def test_load_default_skills_paca_is_always_active():
    skills = load_default_skills()
    paca = next(s for s in skills if s.name == "paca")
    assert paca.trigger is None
    assert paca.is_agentskills_format is False


def test_load_default_skills_specialized_skills_are_model_selectable():
    skills = load_default_skills()
    paca_do = next(s for s in skills if s.name == "paca-do")
    assert paca_do.is_agentskills_format is True
    assert "/paca-do" in paca_do.trigger.keywords


def test_load_default_skills_includes_workflow_skill():
    skills = load_default_skills()
    paca_workflow = next(s for s in skills if s.name == "paca-workflow")
    assert paca_workflow.is_agentskills_format is True
    assert "/paca-workflow" in paca_workflow.trigger.keywords


def test_load_default_skills_is_cached():
    assert load_default_skills() is load_default_skills()


# ─── build_mcp_config ─────────────────────────────────────────────────────────


def test_stdio_server_fields(no_paca_key):
    server = _server(command="node", args=["index.js"], env={"KEY": "VAL"})
    cfg = build_mcp_config([server], "agent-1", "proj-1")
    entry = cfg["my-server"]
    assert entry["command"] == "node"
    assert entry["args"] == ["index.js"]
    assert entry["env"] == {"KEY": "VAL"}


def test_http_server_has_url_no_auth(no_paca_key):
    server = _server(transport="http", url="https://mcp.example.com")
    cfg = build_mcp_config([server], "a", "p")
    entry = cfg["my-server"]
    assert entry["url"] == "https://mcp.example.com"
    assert "auth" not in entry


def test_oauth_server_has_auth_field(no_paca_key):
    server = _server(transport="oauth", url="https://mcp.example.com")
    cfg = build_mcp_config([server], "a", "p")
    assert cfg["my-server"]["auth"] == {"strategy": "oauth2"}


def test_disabled_server_excluded(no_paca_key):
    server = _server(enabled=False)
    cfg = build_mcp_config([server], "a", "p")
    assert "my-server" not in cfg


def test_paca_server_injected_when_key_set(with_paca_key):
    cfg = build_mcp_config([], "agent-99", "proj-42")
    assert "paca" in cfg
    paca = cfg["paca"]
    assert paca["command"] == "npx"
    assert paca["args"] == ["-y", "@paca-ai/paca-mcp"]
    assert paca["env"]["PACA_AGENT_ID"] == "agent-99"
    assert paca["env"]["PACA_PROJECT_ID"] == "proj-42"
    assert paca["env"]["PACA_API_KEY"] == "test-api-key"


def test_paca_server_uses_local_build_when_dev_mcp_path_set(with_paca_key):
    with_paca_key.dev_mcp_path = "/mcp/build/index.js"
    cfg = build_mcp_config([], "agent-99", "proj-42")
    paca = cfg["paca"]
    assert paca["command"] == "node"
    assert paca["args"] == ["/mcp/build/index.js"]


def test_paca_server_omitted_when_no_key(no_paca_key):
    cfg = build_mcp_config([], "a", "p")
    assert "paca" not in cfg


def test_user_servers_appear_before_paca(with_paca_key):
    servers = [_server(name="custom")]
    cfg = build_mcp_config(servers, "a", "p")
    keys = list(cfg)
    assert keys.index("custom") < keys.index("paca")


def test_multiple_servers_all_included(no_paca_key):
    servers = [_server(name="srv1"), _server(name="srv2")]
    cfg = build_mcp_config(servers, "a", "p")
    assert "srv1" in cfg
    assert "srv2" in cfg


def test_empty_servers_returns_empty_dict(no_paca_key):
    cfg = build_mcp_config([], "a", "p")
    assert cfg == {}


def test_llm_base_url_override_takes_precedence_over_agent_config(catalog, monkeypatch):
    from src.agent import builder

    monkeypatch.setattr(builder.settings, "llm_base_url_override", "https://proxy.example/ai/v1")
    config = _agent_config(
        provider="anthropic", model="claude-sonnet-4-6", base_url="https://api.anthropic.com"
    )
    llm = build_llm(config)
    assert llm.base_url == "https://proxy.example/ai/v1"


def test_llm_base_url_override_applies_even_without_agent_base_url(catalog, monkeypatch):
    from src.agent import builder

    monkeypatch.setattr(builder.settings, "llm_base_url_override", "https://proxy.example/ai/v1")
    llm = build_llm(_agent_config(base_url=""))
    assert llm.base_url == "https://proxy.example/ai/v1"


def test_llm_api_key_override_takes_precedence_over_agent_secret(catalog, monkeypatch):
    from src.agent import builder

    monkeypatch.setattr(builder.settings, "llm_api_key_override", "platform-proxy-key")
    llm = build_llm(_agent_config())
    assert llm.api_key is not None
    assert llm.api_key.get_secret_value() == "platform-proxy-key"


def test_no_override_keeps_agent_configured_values(catalog):
    llm = build_llm(_agent_config(base_url="http://self-hosted:8000/v1"))
    assert llm.base_url == "http://self-hosted:8000/v1"
    assert llm.api_key is not None
    assert llm.api_key.get_secret_value() == "secret-ref"


# ─── GALAXY_AI_ROLE platform routing (ADR-038 T3) ────────────────────────────


def test_galaxy_role_routes_model_and_base_url_to_platform_proxy(catalog, monkeypatch):
    from src.agent import builder

    monkeypatch.setattr(builder.settings, "galaxy_ai_role", "paca-ai")
    monkeypatch.setattr(
        builder.settings, "galaxy_ai_proxy_url", "http://nexus-identity:8086/ai/v1"
    )
    config = _agent_config(
        provider="anthropic", model="claude-sonnet-4-6", base_url="https://api.anthropic.com"
    )
    llm = build_llm(config, api_key="minted-act-as-token")
    # model = the AI ROLE — identity resolves it via ai_role_assignments.
    assert llm.model == "openai/paca-ai"
    assert llm.base_url == "http://nexus-identity:8086/ai/v1"
    assert llm.api_key is not None
    assert llm.api_key.get_secret_value() == "minted-act-as-token"


def test_galaxy_role_wins_over_llm_base_url_override(catalog, monkeypatch):
    from src.agent import builder

    monkeypatch.setattr(builder.settings, "galaxy_ai_role", "paca-ai")
    monkeypatch.setattr(
        builder.settings, "galaxy_ai_proxy_url", "http://nexus-identity:8086/ai/v1"
    )
    monkeypatch.setattr(builder.settings, "llm_base_url_override", "https://other.example/v1")
    llm = build_llm(_agent_config(), api_key="tok")
    assert llm.base_url == "http://nexus-identity:8086/ai/v1"
    assert llm.model == "openai/paca-ai"


def test_galaxy_role_without_token_fails_closed(catalog, monkeypatch):
    from src.agent import builder

    monkeypatch.setattr(builder.settings, "galaxy_ai_role", "paca-ai")
    monkeypatch.setattr(builder.settings, "llm_api_key_override", "")
    with pytest.raises(RuntimeError):
        build_llm(_agent_config())


def test_galaxy_role_static_key_override_is_dev_fallback(catalog, monkeypatch):
    from src.agent import builder

    monkeypatch.setattr(builder.settings, "galaxy_ai_role", "paca-ai")
    monkeypatch.setattr(builder.settings, "llm_api_key_override", "static-dev-key")
    llm = build_llm(_agent_config())
    assert llm.api_key is not None
    assert llm.api_key.get_secret_value() == "static-dev-key"
