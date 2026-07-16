"""Tests for BridgeClient's URL conversion and message dispatch."""

from unittest.mock import AsyncMock, MagicMock

import pytest

from paca_acp_bridge.bridge_client import BridgeClient, to_bridge_ws_url


@pytest.mark.parametrize(
    "server,expected",
    [
        ("https://paca.example.com", "wss://paca.example.com/agent-bridge/ws"),
        ("http://localhost:8080", "ws://localhost:8080/agent-bridge/ws"),
        ("https://paca.example.com/", "wss://paca.example.com/agent-bridge/ws"),
    ],
)
def test_to_bridge_ws_url(server, expected):
    assert to_bridge_ws_url(server) == expected


@pytest.fixture
def client():
    return BridgeClient(
        server="https://paca.example.com", agent_id="agent-1", token="tok", workspace="/tmp"
    )


async def test_handle_message_routes_start_turn_to_runner(client):
    client._runner.start_turn = AsyncMock()

    message = {"type": "start_turn", "conversation_id": "c1"}
    await client._handle_message(message)

    client._runner.start_turn.assert_awaited_once_with(message)


async def test_handle_message_routes_stop_turn_to_interrupt(client):
    client._runner.interrupt = MagicMock()

    await client._handle_message({"type": "stop_turn", "conversation_id": "c1"})

    client._runner.interrupt.assert_called_once_with("c1")


async def test_handle_message_routes_pause_turn_to_interrupt(client):
    client._runner.interrupt = MagicMock()

    await client._handle_message({"type": "pause_turn", "conversation_id": "c1"})

    client._runner.interrupt.assert_called_once_with("c1")


async def test_handle_message_ignores_unknown_type(client):
    client._runner.start_turn = AsyncMock()
    client._runner.interrupt = MagicMock()

    await client._handle_message({"type": "something_else"})

    client._runner.start_turn.assert_not_called()
    client._runner.interrupt.assert_not_called()


async def test_send_drops_message_when_not_connected(client):
    # No connection established — should log and return rather than raise.
    await client._send({"type": "ping"})
