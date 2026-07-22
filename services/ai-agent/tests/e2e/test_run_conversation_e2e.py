"""End-to-end regression tests for the AI agent's conversation pipeline.

Drives the real executor.run_conversation() against a REAL OpenHands
agent-server sandbox container (the same image used in production) and a
fake, scriptable OpenAI-compatible LLM server — only the Postgres/Valkey
persistence calls and the default-skills fetch (which hits a real Paca API
this suite doesn't spin up) are mocked. This exercises what a pure unit test can't:
that openhands-sdk's RemoteConversation really does deliver events through
the `callbacks` channel, since `token_callbacks` is never invoked for remote
workspaces (confirmed by reading
openhands.sdk.conversation.impl.remote_conversation — RemoteConversation
accepts the kwarg but never wires it to anything).

The first test is regression coverage for
https://github.com/Paca-AI/paca/issues/211 and #214: before the fix, every
agent reply was silently dropped — for every LLM provider, not just
custom/self-hosted ones — because the event callback unconditionally skipped
the agent's MessageEvent, trusting a token-streaming path that
openhands-sdk >=1.29 never actually invokes for RemoteConversation. The
other tests cover scenarios that were never exercised end-to-end before:
a multi-step tool-call flow, and the LLM-failure path.

Requires Docker and pulls ghcr.io/paca-ai/paca-agent-server on first run.
Run with: PACA_E2E=1 pytest tests/e2e -v
"""

from __future__ import annotations

import os
import uuid
from dataclasses import dataclass
from unittest.mock import AsyncMock

import docker
import pytest

from src.agent import executor
from src.core import streams as stream_store
from src.core.streams import TriggerMessage
from src.models.agent import AgentConfig
from src.repositories import conversation_repository

from .fake_llm_server import (
    FakeOpenAIServer,
    ScriptedReply,
    error_reply,
    text_reply,
    tool_call_reply,
)

pytestmark = pytest.mark.skipif(
    os.environ.get("PACA_E2E") != "1",
    reason="set PACA_E2E=1 to run e2e tests (requires Docker; pulls the agent-server image)",
)


@pytest.fixture(scope="module")
def docker_client():
    from src.config import settings

    try:
        client = docker.DockerClient(base_url=f"unix://{settings.docker_socket}")
        client.ping()
    except Exception as exc:
        pytest.skip(f"Docker daemon not reachable: {exc}")
    yield client
    client.close()


def _bridge_gateway_ip(client: docker.DockerClient) -> str:
    """IP the sandbox container can use to reach a server bound to the host's
    0.0.0.0 — the container can't reach the host via "localhost"."""
    network = client.networks.get("bridge")
    return network.attrs["IPAM"]["Config"][0]["Gateway"]


@pytest.fixture
def make_fake_llm(docker_client):
    """Factory fixture: make_fake_llm(script=[...]) -> base_url string.

    Starts a FakeOpenAIServer scripted with the given replies and tears it
    down at the end of the test. Call multiple times for multiple servers.
    """
    servers: list[FakeOpenAIServer] = []

    def _make(script: list[ScriptedReply] | None = None) -> str:
        server = FakeOpenAIServer(script=script)
        server.start()
        servers.append(server)
        return f"http://{_bridge_gateway_ip(docker_client)}:{server.port}/v1"

    yield _make
    for server in servers:
        server.stop()


@dataclass
class MockedPersistence:
    insert_event: AsyncMock
    update_status: AsyncMock
    publish_realtime: AsyncMock


@pytest.fixture
def persistence(monkeypatch) -> MockedPersistence:
    mocks = MockedPersistence(
        insert_event=AsyncMock(), update_status=AsyncMock(), publish_realtime=AsyncMock()
    )
    monkeypatch.setattr(conversation_repository, "insert_conversation_event", mocks.insert_event)
    monkeypatch.setattr(conversation_repository, "update_conversation_status", mocks.update_status)
    # No prior events for these single-turn e2e scenarios — the counter
    # starts at 0, same as before get_next_event_index existed.
    monkeypatch.setattr(
        conversation_repository, "get_next_event_index", AsyncMock(return_value=0)
    )
    monkeypatch.setattr(
        conversation_repository, "get_seen_event_ids", AsyncMock(return_value=set())
    )
    monkeypatch.setattr(stream_store, "publish_event", AsyncMock())
    monkeypatch.setattr(stream_store, "publish_realtime", mocks.publish_realtime)
    return mocks


@pytest.fixture(autouse=True)
def stub_default_skills(monkeypatch):
    """load_default_skills() fetches Paca's bundled skill set from the API
    over HTTP (GET /api/v1/skills) — this suite runs only the sandbox and a
    fake LLM, no real Paca API, so the real call would fail with a DNS/
    connection error on every conversation. Skill content itself is covered
    by tests/test_builder.py; this suite's subject is the conversation/
    persistence pipeline, not skill fetching."""
    monkeypatch.setattr(executor, "load_default_skills", AsyncMock(return_value=()))


def _agent_config(
    base_url: str, *, max_iterations: int = 3
) -> AgentConfig:
    return AgentConfig(
        agent_id="e2e-test-agent",
        project_id="e2e-test-project",
        system_prompt="You are a helpful assistant.",
        llm_provider="openai",
        llm_model="fake-model",
        llm_api_key_secret_ref="sk-fake",
        llm_base_url=base_url,
        max_iterations=max_iterations,
    )


def _trigger(*, repo_plugin_ids: list[str] | None = None) -> TriggerMessage:
    # Unique per call: chat_sandboxes is a module-level registry keyed by
    # conversation_id, and a natural finish now correctly keeps a chat
    # sandbox alive (is_chat=True) — a shared, hardcoded id would make one
    # test's leftover sandbox get reattached to by the next test.
    return TriggerMessage(
        stream_id="1-1",
        trigger_type="chat_message",
        conversation_id=f"e2e-test-conversation-{uuid.uuid4().hex[:8]}",
        agent_id="e2e-test-agent",
        project_id="e2e-test-project",
        task_id=None,
        comment_id=None,
        chat_session_id=None,
        message="Say hello",
        actor_member_id="tester",
        repo_plugin_ids=repo_plugin_ids or [],
    )


def _persisted_events(insert_event: AsyncMock) -> list[dict]:
    return [call.kwargs for call in insert_event.call_args_list]


async def test_agent_reply_is_persisted_through_real_sandbox(make_fake_llm, persistence):
    base_url = make_fake_llm()
    trigger = _trigger()

    try:
        await executor.run_conversation(trigger, _agent_config(base_url))

        persisted = _persisted_events(persistence.insert_event)
        agent_messages = [
            p
            for p in persisted
            if p["event_type"] == "MessageEvent" and p["event_source"] == "agent"
        ]
        assert agent_messages, "the agent's reply was never persisted (see issues #211, #214)"
        assert "Hello! This is a canned reply" in agent_messages[0]["payload"]

        # Chat conversations pause (not finish) after a natural per-turn
        # completion — the sandbox stays alive for the next reply.
        persistence.update_status.assert_any_call(trigger.conversation_id, "paused")
    finally:
        await executor.teardown_paused_chat_sandbox(trigger.conversation_id)


async def test_agent_tool_call_then_reply_persists_full_flow(make_fake_llm, persistence):
    """A tool call followed by a final text reply spans two agent.step()
    iterations. Confirms the action, its observation, AND the final reply
    all persist through the real pipeline, not just the isolated unit test."""
    base_url = make_fake_llm(
        script=[
            tool_call_reply("terminal", {"command": "echo hello-from-e2e"}),
            text_reply("Done! I ran the command."),
        ]
    )
    agent_config = _agent_config(base_url, max_iterations=5)
    trigger = _trigger(repo_plugin_ids=["fake-plugin-id"])

    try:
        await executor.run_conversation(trigger, agent_config)

        persisted = _persisted_events(persistence.insert_event)
        event_kinds = [(p["event_type"], p["event_source"]) for p in persisted]

        assert ("ActionEvent", "agent") in event_kinds, f"tool call never persisted: {event_kinds}"
        assert ("ObservationEvent", "environment") in event_kinds, (
            f"tool result never persisted: {event_kinds}"
        )

        agent_messages = [
            p
            for p in persisted
            if p["event_type"] == "MessageEvent" and p["event_source"] == "agent"
        ]
        assert agent_messages, "the agent's final reply was never persisted"
        assert "Done! I ran the command." in agent_messages[-1]["payload"]

        # Chat conversations pause (not finish) after a natural per-turn
        # completion — the sandbox stays alive for the next reply.
        persistence.update_status.assert_any_call(trigger.conversation_id, "paused")
    finally:
        await executor.teardown_paused_chat_sandbox(trigger.conversation_id)


async def test_llm_failure_marks_conversation_failed(make_fake_llm, persistence):
    """Previously-untested path: when the LLM call itself fails (not just
    the reply-persistence bug), the conversation must end up "failed" and
    the frontend must be notified, instead of silently hanging."""
    base_url = make_fake_llm(script=[error_reply(500)])
    trigger = _trigger()

    await executor.run_conversation(trigger, _agent_config(base_url))

    persistence.update_status.assert_any_call(trigger.conversation_id, "failed")
    failed_events = [
        call
        for call in persistence.publish_realtime.call_args_list
        if call.kwargs.get("event_type") == "agent.conversation.failed"
    ]
    assert failed_events, "agent.conversation.failed was never published"

    agent_messages = [
        p
        for p in _persisted_events(persistence.insert_event)
        if p["event_type"] == "MessageEvent" and p["event_source"] == "agent"
    ]
    assert not agent_messages, "no reply should have been persisted when the LLM call failed"
