"""Routes triggers for ACP-type agents to their connected local bridge.

Mirrors executor.run_conversation's role for LLM agents (called from
worker._process_trigger), but never spins up a sandbox — the ACP CLI runs
entirely on the user's own machine via apps/acp-bridge, connected through
acp_bridge.py / routes/bridge.py. Auth, MCP servers, and skills are all the
user's own local responsibility — nothing is forwarded from Paca.
"""

from __future__ import annotations

import asyncio
import logging

from ..core import streams as stream_store
from ..core.streams import TriggerMessage
from ..models.agent import AgentConfig
from ..models.conversation_status import ConversationStatus
from ..repositories import conversation_repository
from . import acp_bridge
from .prompt import build_acp_message

logger = logging.getLogger(__name__)

_OFFLINE_MESSAGE = (
    "Local ACP bridge is not connected. Run `paca-acp-bridge run --agent-id "
    "<id> --token <token>` in your project folder, then try again."
)
_TIMEOUT_MESSAGE = (
    "ACP turn timed out waiting for a result from the local bridge. Check "
    "that `paca-acp-bridge run` is still running and reachable, then retry."
)

# Strong references to in-flight watchdog tasks — asyncio only holds a weak
# reference to a task via create_task(), so an unreferenced task can be
# garbage-collected mid-sleep (see the asyncio docs' "Important" note on
# create_task). Discarded once the task finishes.
_watchdog_tasks: set[asyncio.Task] = set()


async def _fail_offline(trigger: TriggerMessage) -> None:
    await conversation_repository.update_conversation_status(
        trigger.conversation_id, ConversationStatus.FAILED, error_message=_OFFLINE_MESSAGE
    )
    await stream_store.publish_realtime(
        project_id=trigger.project_id,
        conversation_id=trigger.conversation_id,
        event_type="agent.conversation.failed",
        actor_user_id=trigger.actor_user_id,
    )


async def _watchdog(
    conversation_id: str,
    project_id: str | None,
    timeout_minutes: int,
    actor_user_id: str | None = None,
) -> None:
    """Fails a dispatched ACP turn that never reported a terminal turn_status.

    Dispatch to the bridge goes over Valkey Pub/Sub (see acp_bridge.dispatch),
    which has no delivery guarantee — a start_turn published while the
    daemon is mid-reconnect is silently dropped, leaving the conversation
    stuck at RUNNING with nothing to ever move it forward. This is the
    backstop: after timeout_minutes with no terminal status, fail the
    conversation ourselves. Race-safe against a legitimate late turn_status
    via fail_if_not_terminal's conditional UPDATE.
    """
    await asyncio.sleep(max(timeout_minutes, 1) * 60)
    failed = await conversation_repository.fail_if_not_terminal(conversation_id, _TIMEOUT_MESSAGE)
    if not failed:
        return
    logger.warning(
        "ACP turn for conversation %s timed out after %d minute(s) with no "
        "turn_status from the bridge",
        conversation_id,
        timeout_minutes,
    )
    await stream_store.publish_realtime(
        project_id=project_id,
        conversation_id=conversation_id,
        event_type="agent.conversation.failed",
        actor_user_id=actor_user_id,
    )


def _schedule_watchdog(
    conversation_id: str,
    project_id: str | None,
    timeout_minutes: int,
    actor_user_id: str | None = None,
) -> None:
    task = asyncio.create_task(
        _watchdog(conversation_id, project_id, timeout_minutes, actor_user_id)
    )
    _watchdog_tasks.add(task)
    task.add_done_callback(_watchdog_tasks.discard)


async def dispatch_acp_trigger(trigger: TriggerMessage, agent_config: AgentConfig) -> None:
    """Hand a trigger off to the agent's connected local ACP bridge."""
    agent_id = agent_config.agent_id
    if not await acp_bridge.is_online(agent_id):
        logger.info(
            "ACP bridge offline for agent %s; failing conversation %s",
            agent_id,
            trigger.conversation_id,
        )
        await _fail_offline(trigger)
        return

    await conversation_repository.update_conversation_status(
        trigger.conversation_id, ConversationStatus.RUNNING
    )
    dispatched = await acp_bridge.dispatch(
        agent_id,
        {
            "type": "start_turn",
            "conversation_id": trigger.conversation_id,
            "project_id": trigger.project_id,
            "message": build_acp_message(trigger),
            "trigger_type": trigger.trigger_type,
            "acp_provider": agent_config.acp_provider,
            "acp_command": agent_config.acp_command,
        },
    )
    if not dispatched:
        # Bridge disconnected between the is_online check above and here
        # (e.g. the daemon crashed mid-dispatch) — fail the same way.
        logger.info(
            "ACP bridge for agent %s went offline before dispatch; failing conversation %s",
            agent_id,
            trigger.conversation_id,
        )
        await _fail_offline(trigger)
        return

    _schedule_watchdog(
        trigger.conversation_id,
        trigger.project_id,
        agent_config.timeout_minutes,
        trigger.actor_user_id,
    )
