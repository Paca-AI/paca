"""Tests for LLM / skill / MCP configuration builders."""

from unittest.mock import patch

import httpx
import pytest

import src.agent.builder as builder_module
from src.agent.builder import (
    build_llm,
    build_mcp_config,
    build_skills,
    load_default_skills,
    load_plugin_skills,
)
from src.models.agent import AgentConfig, AgentMCPServerRow, AgentSkillRow, PluginSkillRef

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


@pytest.fixture(autouse=True)
def _reset_default_skills_cache():
    # load_default_skills() caches its result in a module-level global (see
    # builder.py) so a real worker process only fetches once — tests must
    # reset it between runs or an earlier test's mocked response leaks into
    # a later one via the cache.
    builder_module._default_skills_cache = None
    yield
    builder_module._default_skills_cache = None


@pytest.fixture
def default_skills_api_url():
    with patch("src.agent.builder.settings.api_base_url", "http://api:8080"):
        yield "http://api:8080"


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


_LEGACY_PACA_CONTENT = (
    "You are Paca, an AI project management assistant embedded in this workspace.\n"
)


def _default_skills_payload(entries: list[dict]) -> dict:
    return {"success": True, "data": {"skills": entries}}


async def test_load_default_skills_is_non_empty(default_skills_api_url):
    entries = [{"name": "paca", "path": "paca.md", "content": _LEGACY_PACA_CONTENT}]
    responses = {
        f"{default_skills_api_url}/api/v1/skills": _FakeResponse(
            "", json_data=_default_skills_payload(entries)
        )
    }
    with _fake_httpx_client(responses):
        skills = await load_default_skills()
    assert len(skills) > 0


async def test_load_default_skills_paca_is_always_active(default_skills_api_url):
    entries = [{"name": "paca", "path": "paca.md", "content": _LEGACY_PACA_CONTENT}]
    responses = {
        f"{default_skills_api_url}/api/v1/skills": _FakeResponse(
            "", json_data=_default_skills_payload(entries)
        )
    }
    with _fake_httpx_client(responses):
        skills = await load_default_skills()
    paca = next(s for s in skills if s.name == "paca")
    assert paca.trigger is None
    assert paca.is_agentskills_format is False


async def test_load_default_skills_specialized_skills_are_model_selectable(
    default_skills_api_url,
):
    entries = [
        {
            "name": "paca-do",
            "path": "paca-do/SKILL.md",
            "content": _skill_md("paca-do", "Do things", triggers=["/paca-do"]),
        }
    ]
    responses = {
        f"{default_skills_api_url}/api/v1/skills": _FakeResponse(
            "", json_data=_default_skills_payload(entries)
        )
    }
    with _fake_httpx_client(responses):
        skills = await load_default_skills()
    paca_do = next(s for s in skills if s.name == "paca-do")
    assert paca_do.is_agentskills_format is True
    assert "/paca-do" in paca_do.trigger.keywords


async def test_load_default_skills_includes_workflow_skill(default_skills_api_url):
    entries = [
        {
            "name": "paca-workflow",
            "path": "paca-workflow/SKILL.md",
            "content": _skill_md("paca-workflow", "Runs the workflow", triggers=["/paca-workflow"]),
        }
    ]
    responses = {
        f"{default_skills_api_url}/api/v1/skills": _FakeResponse(
            "", json_data=_default_skills_payload(entries)
        )
    }
    with _fake_httpx_client(responses):
        skills = await load_default_skills()
    paca_workflow = next(s for s in skills if s.name == "paca-workflow")
    assert paca_workflow.is_agentskills_format is True
    assert "/paca-workflow" in paca_workflow.trigger.keywords


async def test_load_default_skills_is_cached(default_skills_api_url):
    entries = [{"name": "paca", "path": "paca.md", "content": _LEGACY_PACA_CONTENT}]
    responses = {
        f"{default_skills_api_url}/api/v1/skills": _FakeResponse(
            "", json_data=_default_skills_payload(entries)
        )
    }
    with _fake_httpx_client(responses):
        first = await load_default_skills()
        second = await load_default_skills()
    assert first is second


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


def test_paca_server_includes_actor_user_id_when_set(with_paca_key):
    cfg = build_mcp_config([], "agent-99", "proj-42", "user-7")
    assert cfg["paca"]["env"]["PACA_ACTOR_USER_ID"] == "user-7"


def test_paca_server_omits_actor_user_id_when_none(with_paca_key):
    cfg = build_mcp_config([], "agent-99", "proj-42")
    assert "PACA_ACTOR_USER_ID" not in cfg["paca"]["env"]


def test_paca_server_omits_project_id_when_none_for_global_agent(with_paca_key):
    """The security-relevant half of build_mcp_config's new behavior: a
    global-chat conversation (project_id=None) must put the spawned MCP
    process into "unpinned" mode by omitting PACA_PROJECT_ID entirely, not by
    sending an empty string — see the function's own doc comment. Previously
    untested; only the actor_user_id inclusion/omission was covered."""
    cfg = build_mcp_config([], "agent-99", None, "user-7")
    assert "PACA_PROJECT_ID" not in cfg["paca"]["env"]
    assert cfg["paca"]["env"]["PACA_AGENT_ID"] == "agent-99"
    assert cfg["paca"]["env"]["PACA_ACTOR_USER_ID"] == "user-7"


# ─── load_plugin_skills ────────────────────────────────────────────────────────


class _FakeResponse:
    def __init__(self, text: str, status_code: int = 200, json_data: object = None):
        self.text = text
        self.status_code = status_code
        self._json_data = json_data

    def raise_for_status(self) -> None:
        if self.status_code >= 400:
            raise httpx.HTTPError(f"status {self.status_code}")

    def json(self) -> object:
        return self._json_data


class _FakeAsyncClient:
    def __init__(self, responses: dict[str, _FakeResponse]):
        self._responses = responses

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        return False

    async def get(self, url: str, params: dict | None = None) -> _FakeResponse:
        if url not in self._responses:
            raise httpx.HTTPError(f"no mock response for {url}")
        return self._responses[url]


def _fake_httpx_client(responses: dict[str, _FakeResponse]):
    return patch(
        "src.agent.builder.httpx.AsyncClient",
        return_value=_FakeAsyncClient(responses),
    )


def _skill_md(
    name: str = "pr-review",
    description: str = "Reviews PRs",
    triggers: list[str] | None = None,
) -> str:
    trigger_lines = ""
    if triggers:
        trigger_lines = "triggers:\n" + "".join(f"  - {t}\n" for t in triggers)
    return f"---\nname: {name}\ndescription: {description}\n{trigger_lines}---\nDo the review.\n"


async def test_load_plugin_skills_empty_refs_returns_empty():
    assert await load_plugin_skills([]) == []


async def test_load_plugin_skills_parses_valid_skill():
    ref = PluginSkillRef(
        plugin_name="com.paca.example", skill_name="pr-review", url="http://gw/pr-review/SKILL.md"
    )
    responses = {ref.url: _FakeResponse(_skill_md("pr-review"))}
    with _fake_httpx_client(responses):
        skills = await load_plugin_skills([ref])
    assert len(skills) == 1
    assert skills[0].name == "pr-review"


async def test_load_plugin_skills_skips_on_fetch_failure():
    ref = PluginSkillRef(
        plugin_name="com.paca.example", skill_name="pr-review", url="http://gw/pr-review/SKILL.md"
    )
    responses = {ref.url: _FakeResponse("not found", status_code=404)}
    with _fake_httpx_client(responses):
        skills = await load_plugin_skills([ref])
    assert skills == []


async def test_load_plugin_skills_skips_on_name_mismatch():
    # Frontmatter name disagrees with the ref's skill_name (which drives the
    # on-disk directory name) — strict AgentSkills validation rejects this.
    ref = PluginSkillRef(
        plugin_name="com.paca.example", skill_name="pr-review", url="http://gw/pr-review/SKILL.md"
    )
    responses = {ref.url: _FakeResponse(_skill_md("some-other-name"))}
    with _fake_httpx_client(responses):
        skills = await load_plugin_skills([ref])
    assert skills == []


async def test_load_plugin_skills_one_bad_skill_does_not_block_others():
    good = PluginSkillRef(
        plugin_name="com.paca.example", skill_name="pr-review", url="http://gw/pr-review/SKILL.md"
    )
    bad = PluginSkillRef(
        plugin_name="com.paca.example", skill_name="broken", url="http://gw/broken/SKILL.md"
    )
    responses = {
        good.url: _FakeResponse(_skill_md("pr-review")),
        bad.url: _FakeResponse("not found", status_code=500),
    }
    with _fake_httpx_client(responses):
        skills = await load_plugin_skills([good, bad])
    assert [s.name for s in skills] == ["pr-review"]
