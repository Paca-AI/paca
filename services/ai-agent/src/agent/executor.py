"""Agent conversation executor — orchestrates LLM, skills, MCP, and repo tools."""

from __future__ import annotations

import asyncio
import logging
import threading
import time

import httpx
from openhands.sdk import Agent, AgentContext, Conversation
from openhands.sdk.conversation.state import ConversationExecutionStatus
from openhands.sdk.conversation.visualizer import ConversationVisualizerBase
from openhands.sdk.skills import merge_skills_by_name
from openhands.tools import get_default_tools

from ..config import settings
from ..core import streams as stream_store
from ..core.registry import (
    ChatSandboxState,
    active_conversations,
    chat_sandboxes,
    pause_events,
    stop_events,
)
from ..core.streams import TriggerMessage
from ..models.agent import AgentConfig
from ..models.conversation_status import ConversationStatus
from ..repositories import conversation_repository
from . import trigger_skills
from .builder import (
    build_llm,
    build_mcp_config,
    build_skills,
    load_default_skills,
    load_plugin_skills,
)
from .docker_workspace import start_sandbox, stop_sandbox
from .prompt import build_initial_prompt, build_project_context_suffix, build_trigger_suffix
from .repo_tools import make_repository_tool_specs

# The trigger_type Go publishes for direct-chat messages (agent_service.go's
# publishChatTrigger). Chat conversations keep their sandbox alive between
# turns instead of tearing it down — see `run_conversation` below.
CHAT_MESSAGE_TRIGGER_TYPE = "chat_message"

logger = logging.getLogger(__name__)

_ERROR_STATUSES = frozenset(
    {
        ConversationExecutionStatus.ERROR,
        ConversationExecutionStatus.STUCK,
    }
)

# events.reconcile() re-walks the *entire* remote event history over REST
# (openhands.sdk.conversation.impl.remote_conversation.RemoteEventsList has no
# "since" cursor) — the SDK's own blocking run() only pays that cost once, at
# completion, rather than on every status poll. We still need it more often
# than that to bound the WS-dead-thread persistence gap (see
# _wait_for_done_or_stop), but not on every 2s tick.
_RECONCILE_INTERVAL = 10.0

# Granularity for checking stop_event/pause_event in _wait_for_done_or_stop,
# decoupled from poll_interval (which paces the REST status check) so either
# event wakes the loop promptly instead of waiting up to a full poll_interval.
_EVENT_TICK_INTERVAL = 0.5


def _get_conversation_error_detail(conversation) -> str | None:
    """Extract the error detail from a conversation's ConversationErrorEvent, if any."""
    try:
        from openhands.sdk.event.conversation_error import ConversationErrorEvent

        events = conversation.state.events
        for event in reversed(list(events)):
            if isinstance(event, ConversationErrorEvent):
                code = (event.code or "").strip()
                detail = (event.detail or "").strip()
                if code and detail:
                    return f"{code}: {detail}"
                return detail or code or None
    except Exception:
        pass
    return None


def _wait_for_done_or_stop(
    conversation,
    stop_event: threading.Event,
    pause_event: threading.Event,
    reconcile,
    poll_interval: float = 2.0,
    timeout: float = 3600.0,
) -> tuple[bool, bool, bool, str | None]:
    """Poll the remote conversation until it finishes or a stop/pause is signaled.

    Returns (stopped, errored, shutdown, error_detail):
      stopped      — True if the loop exited because stop_event or pause_event fired.
      errored      — True if the conversation ended with ERROR or STUCK status.
      shutdown     — True if it was stop_event (full shutdown) rather than
                     pause_event (interrupt-only) that fired.
      error_detail — The ConversationErrorEvent detail when errored is True,
                     else None.
    """
    start = time.monotonic()
    last_reconcile = start
    last_status_check = start - poll_interval  # force an immediate first check
    while True:
        # Tick on a short interval so either event wakes this loop promptly;
        # the REST status check below is separately paced by poll_interval so
        # this doesn't increase load on the sandbox.
        if stop_event.wait(timeout=_EVENT_TICK_INTERVAL) or pause_event.is_set():
            shutdown = stop_event.is_set()
            try:
                # interrupt() (not pause()) — pause() waits for the current
                # LLM/tool call to finish before taking effect, which can
                # stretch out for as long as that step takes (a long shell
                # command, a slow completion) and makes the stop/pause button
                # look like it did nothing. interrupt() cancels the in-flight
                # request immediately; the remote conversation still ends up
                # "paused" and resumable either way (confirmed against the
                # agent-server's own /interrupt route description).
                conversation.interrupt()
            except Exception as exc:
                logger.warning("Failed to interrupt conversation on stop request: %s", exc)
            # Catch up on anything the dead WS thread missed before we stop
            # watching this conversation entirely.
            reconcile(conversation)
            return True, False, shutdown, None

        now = time.monotonic()
        if now - start > timeout:
            logger.warning("Conversation polling timed out after %.0f seconds", timeout)
            reconcile(conversation)
            return False, False, False, None

        if now - last_status_check < poll_interval:
            continue
        last_status_check = now

        try:
            # RemoteState caches execution_status from WebSocket events and only
            # ever re-fetches over REST if the cache is still empty (see
            # RemoteState._get_conversation_info). openhands-sdk's WS client
            # thread permanently stops reconnecting after any ConnectionClosed
            # (upstream bug, still present as of openhands-sdk 1.30.0 — see
            # OpenHands/software-agent-sdk#1532), which would otherwise freeze
            # this loop on a stale "running" status until the timeout above.
            # Force a live REST read every poll so a dead socket can't wedge us.
            conversation.state.refresh_from_server()
            status = conversation.state.execution_status

            # The same dead socket also stops new agent events from ever
            # reaching our persistence callback (it only fires from inside the
            # WS client thread). reconcile() is a full-history REST walk (see
            # _RECONCILE_INTERVAL), so throttle it independently of the
            # cheap poll_interval status check above.
            if now - last_reconcile >= _RECONCILE_INTERVAL:
                reconcile(conversation)
                last_reconcile = now

            if status.is_terminal():
                errored = status in _ERROR_STATUSES
                detail = _get_conversation_error_detail(conversation) if errored else None
                if errored:
                    logger.error(
                        "Conversation ended with status %s — %s",
                        status.value,
                        detail or "no detail available",
                    )
                # Final catch-up so the run never ends with an un-persisted
                # tail from the last reconcile_interval stretch.
                reconcile(conversation)
                return False, errored, False, detail
        except Exception as exc:
            logger.debug("Failed to read conversation execution status: %s", exc)


def _keep_sandbox_alive(is_chat: bool, errored: bool, shutdown: bool) -> bool:
    """Whether a chat sandbox should stay registered in chat_sandboxes after a turn ends.

    True for a natural finish and for an interrupt-only pause; False for an
    error, a real (full-teardown) stop, or any non-chat trigger (those never
    pause between turns).
    """
    return is_chat and not errored and not shutdown


def _post_turn_status(
    is_chat: bool, stopped: bool, errored: bool, shutdown: bool
) -> tuple[ConversationStatus, str] | None:
    """Status + realtime event type to write after a turn ends.

    Returns None when the caller already owns the status write: a real
    (full-teardown) stop is initiated by Go's StopConversation, which
    synchronously writes "stopped" before publishing the trigger, so Python
    does not need to (and a race where the turn finishes before the stop
    signal lands is handled by the idle reaper, not here).
    """
    if shutdown:
        return None
    if errored:
        return ConversationStatus.FAILED, "agent.conversation.failed"
    if stopped:
        # Interrupt-only. Chat conversations pause (sandbox stays alive,
        # mirroring a natural per-turn finish); non-chat triggers have no
        # pause-between-turns concept and their sandbox was already torn
        # down in _run_sync.
        if is_chat:
            return ConversationStatus.PAUSED, "agent.conversation.paused"
        return ConversationStatus.STOPPED, "agent.conversation.stopped"
    if is_chat:
        return ConversationStatus.PAUSED, "agent.conversation.paused"
    return ConversationStatus.FINISHED, "agent.conversation.finished"


# ─── Custom visualizer ────────────────────────────────────────────────────────


class _QuietVisualizer(ConversationVisualizerBase):
    """No-op visualizer that silently accepts all event types.

    The SDK's DefaultConversationVisualizer emits a WARNING for every event
    type absent from its EVENT_VISUALIZATION_CONFIG (e.g. StreamingDeltaEvent).
    Since this service handles all events via the `callbacks` list, we
    replace the default visualizer with this no-op to eliminate the noise.
    """

    def on_event(self, event) -> None:  # noqa: ANN001
        pass  # all event processing is done in the conversation callbacks


# ─── Repository info helper ───────────────────────────────────────────────────


class RepoInfoSource:
    """Fetches linked repository list from the repository plugin."""

    def __init__(self, plugin_id: str, project_id: str) -> None:
        self.plugin_id = plugin_id
        self.project_id = project_id
        self.repositories: list[dict] = []
        self.clone_url: str | None = None

    def _refresh(self) -> None:
        url = (
            f"{settings.api_base_url}/api/v1/plugins/{self.plugin_id}"
            f"/projects/{self.project_id}/repositories"
        )
        response = httpx.get(url, headers={"X-API-Key": settings.paca_api_key}, timeout=10)
        response.raise_for_status()
        items = response.json().get("data", [])
        if not isinstance(items, list):
            items = []
        self.repositories = items
        self.clone_url = items[0].get("clone_url") if items else None

    def get_repositories(self) -> list[dict]:
        self._refresh()
        return self.repositories


def _gather_repo_sources(trigger: TriggerMessage) -> list[RepoInfoSource]:
    sources = []
    for plugin_id in trigger.repo_plugin_ids:
        source = RepoInfoSource(plugin_id, trigger.project_id)
        try:
            source._refresh()
            if source.repositories:
                sources.append(source)
        except Exception as exc:
            logger.warning("Failed to get repository info from plugin %s: %s", plugin_id, exc)
    return sources


# ─── Shared event index ───────────────────────────────────────────────────────


class _AtomicCounter:
    """Thread-safe monotonic counter for ordering persisted events.

    Must be seeded with the conversation's next unused event_index (see
    conversation_repository.get_next_event_index) rather than always
    starting at 0 — event_index is unique per conversation for its entire
    lifetime, not just the current turn.
    """

    def __init__(self, start: int = 0) -> None:
        self._lock = threading.Lock()
        self._value = start

    def next(self) -> int:
        with self._lock:
            v = self._value
            self._value += 1
            return v


class _SeenEvents:
    """Thread-safe set of SDK event ids already routed to persistence.

    Shared between the live WebSocket callback and the reconciliation
    fallback in `_wait_for_done_or_stop` so that an event is persisted
    exactly once no matter which path first observes it.

    Must be seeded with the SDK ids already persisted in earlier turns (see
    conversation_repository.get_seen_event_ids) on a resumed chat turn — a
    fresh, empty set would not recognize those ids, and reconcile() re-walks
    the *entire* remote SDK event history (not just this turn's), so every
    old event would be persisted again under a new event_index.
    """

    def __init__(self, initial_ids: set[str] | None = None) -> None:
        self._lock = threading.Lock()
        self._ids: set[str] = set(initial_ids) if initial_ids else set()

    def mark_new(self, event_id: str) -> bool:
        """Return True and record the id if it hasn't been seen before."""
        with self._lock:
            if event_id in self._ids:
                return False
            self._ids.add(event_id)
            return True


# ─── Event callback ───────────────────────────────────────────────────────────


async def persist_conversation_event(
    conversation_id: str,
    project_id: str | None,
    event_type: str,
    event_source: str,
    event_index: int,
    payload: str,
    actor_user_id: str | None = None,
) -> None:
    """Write a single event to Postgres + Valkey and fan it out over realtime.

    Shared by the in-process SDK event callback below (via `_persist_event`,
    which additionally filters and indexes SDK event objects) and
    routes/bridge.py, which persists events reported by a local ACP bridge
    daemon — both paths must write to the same destinations so the frontend
    renders either source identically. project_id is None and actor_user_id
    is set for a global-chat conversation (see TriggerMessage).
    """
    await conversation_repository.insert_conversation_event(
        conversation_id=conversation_id,
        event_type=event_type,
        event_source=event_source,
        event_index=event_index,
        payload=payload,
    )
    await stream_store.publish_event(
        {
            "conversation_id": conversation_id,
            "project_id": project_id or "",
            "event_type": event_type,
            "event_source": event_source,
            "event_index": str(event_index),
            "payload": payload,
            "status": "running",
        }
    )
    await stream_store.publish_realtime(
        project_id=project_id,
        conversation_id=conversation_id,
        event_type=f"agent.{event_type.lower()}",
        actor_user_id=actor_user_id,
    )


def _persist_event(
    trigger: TriggerMessage,
    loop: asyncio.AbstractEventLoop,
    counter: _AtomicCounter,
    event,
) -> None:
    """Filter, index, and persist a single SDK event to Postgres + Valkey.

    Shared by the live WebSocket callback and the reconciliation fallback (see
    `_make_reconciler`) so both paths apply identical filtering and write to
    the same destinations, regardless of which one first observes an event.
    """
    event_type = type(event).__name__
    event_source = str(getattr(event, "source", "agent"))

    # Events the frontend never renders — skip to avoid wasting event_index
    # slots and polluting the paginated events list.
    if event_type in {
        "StreamingDeltaEvent",  # raw streaming chunks — only the final message matters
        "ConversationStateUpdateEvent",  # internal iteration bookkeeping
        "SystemPromptEvent",  # system prompt echo — not shown to user
        "ConversationErrorEvent",  # SDK error signal — surfaced via conversation status
    }:
        return

    event_index = counter.next()
    payload = event.model_dump_json() if hasattr(event, "model_dump_json") else "{}"

    future = asyncio.run_coroutine_threadsafe(
        persist_conversation_event(
            conversation_id=trigger.conversation_id,
            project_id=trigger.project_id,
            event_type=event_type,
            event_source=event_source,
            event_index=event_index,
            payload=payload,
            actor_user_id=trigger.actor_user_id,
        ),
        loop,
    )
    try:
        future.result(timeout=10)
    except Exception as exc:
        logger.warning(
            "Event persist failed for conversation %s: %s",
            trigger.conversation_id,
            exc,
        )


def _make_event_callback(
    trigger: TriggerMessage,
    loop: asyncio.AbstractEventLoop,
    counter: _AtomicCounter,
    seen: _SeenEvents,
):
    """Return a synchronous callback invoked by the OpenHands SDK on each complete event."""

    def callback(event) -> None:
        event_id = getattr(event, "id", None)
        if event_id is not None and not seen.mark_new(event_id):
            return
        _persist_event(trigger, loop, counter, event)

    return callback


def _make_reconciler(
    trigger: TriggerMessage,
    loop: asyncio.AbstractEventLoop,
    counter: _AtomicCounter,
    seen: _SeenEvents,
):
    """Return a function that catches up on events the dead WS thread missed.

    openhands-sdk's WS client thread permanently stops reconnecting after any
    ConnectionClosed (see the comment in `_wait_for_done_or_stop`), so the
    composed event callback silently stops firing entirely — even though the
    agent keeps running in its sandbox and producing events. `events.reconcile()`
    re-fetches the full event history over REST and merges it straight into
    the SDK's local cache, bypassing callbacks, so anything new it pulls in
    has to be routed through `_persist_event` by hand here.
    """

    def reconcile(conversation) -> None:
        try:
            conversation.state.events.reconcile()
            for event in list(conversation.state.events):
                event_id = getattr(event, "id", None)
                if event_id is None or not seen.mark_new(event_id):
                    continue
                _persist_event(trigger, loop, counter, event)
        except Exception as exc:
            logger.debug(
                "Event reconciliation failed for conversation %s: %s",
                trigger.conversation_id,
                exc,
            )

    return reconcile


# ─── Main entry point ─────────────────────────────────────────────────────────


async def run_conversation(trigger: TriggerMessage, agent_config: AgentConfig) -> None:
    """Execute a single agent conversation end-to-end.

    Chat-message conversations are special: instead of always cold-starting a
    sandbox and destroying it once this turn finishes, they check
    `chat_sandboxes` for a live sandbox from a previous turn in the same
    conversation and reattach to it. On a natural (non-stopped, non-errored)
    finish, the sandbox is kept alive and registered for the *next* turn
    instead of being torn down — see the end of `_run_sync` below.
    """
    is_chat = trigger.trigger_type == CHAT_MESSAGE_TRIGGER_TYPE
    resume_state = chat_sandboxes.get(trigger.conversation_id) if is_chat else None
    loop = asyncio.get_event_loop()
    # Seed from the conversation's existing history, not 0 — on a resumed
    # chat turn, starting back at 0 would collide with event_index values
    # already used by earlier turns and get silently dropped on insert (see
    # _AtomicCounter's docstring).
    next_index = await conversation_repository.get_next_event_index(trigger.conversation_id)
    counter = _AtomicCounter(next_index)
    # Seed from events already persisted in earlier turns — see _SeenEvents'
    # docstring for why an empty set here would re-persist the whole prior
    # history as duplicates on every resumed chat turn.
    seen_ids = await conversation_repository.get_seen_event_ids(trigger.conversation_id)
    seen_events = _SeenEvents(seen_ids)
    stop_event = threading.Event()
    pause_event = threading.Event()
    logger.info("Starting conversation %s (agent=%s)", trigger.conversation_id, trigger.agent_id)
    await conversation_repository.update_conversation_status(
        trigger.conversation_id, ConversationStatus.RUNNING
    )

    # Register the stop/pause events so the worker can signal us via stream
    # control messages: stop_event -> shutdown, pause_event -> interrupt-only.
    stop_events[trigger.conversation_id] = stop_event
    pause_events[trigger.conversation_id] = pause_event

    try:
        llm = build_llm(agent_config)
        # Precedence on a name collision: agent-configured skills win over
        # plugin-contributed skills, which win over bundled defaults. The two
        # fetches are independent (different sources, no shared state), so
        # run them concurrently rather than paying both round trips serially
        # on every conversation start.
        plugin_skills, default_skills = await asyncio.gather(
            load_plugin_skills(agent_config.plugin_skills), load_default_skills()
        )
        skills = merge_skills_by_name(
            build_skills(agent_config.skills),
            merge_skills_by_name(plugin_skills, default_skills),
        )
        trigger_skills.append_trigger_skill(
            skills,
            trigger.trigger_type,
            trigger.task_id,
            trigger.conversation_id,
            is_automation=trigger.actor_member_id is None,
        )
        mcp_config = build_mcp_config(
            agent_config.mcp_servers, agent_config.agent_id, trigger.project_id
        )

        system_suffix = agent_config.system_prompt or ""

        # Inject current project context so the AI never needs to ask for the
        # project ID or call list_projects to discover it.
        system_suffix += build_project_context_suffix(trigger.project_id)

        # Documentation lives in Paca — never create local files, and always
        # read it before acting (it holds architecture/conventions/prior
        # decisions no skill-specific instruction should let you skip).
        # Detailed write-back workflow is covered by the always-active
        # `paca` skill and the specialized `paca-doc` skill.
        system_suffix += (
            "\n\n## Documentation\n"
            "This project's documentation is managed in Paca — use "
            "`list_docs` / `read_doc` / `write_doc`. Never create local "
            "markdown files. **Read the relevant documentation before doing "
            "anything else** — it defines the project's architecture, "
            "conventions, and prior decisions.\n"
        )

        has_repos = len(trigger.repo_plugin_ids) > 0
        logger.info(
            "Conversation %s — repo_plugin_ids=%s has_repos=%s",
            trigger.conversation_id,
            trigger.repo_plugin_ids,
            has_repos,
        )
        if has_repos:
            system_suffix += (
                "\n\n## Repository Access\n"
                "This project has linked repositories: `list_repositories` → "
                "`clone_repository` (clones to /workspace/repo). Create a "
                "feature branch before changing anything, commit your work, "
                "then call `push_branch` — do NOT skip this step. PR "
                "creation, review, and comments are handled by the "
                "repository plugin's own tools.\n"
            )

        # Pre-gather repository info for the trigger context suffix. Only
        # needed on a cold start: RemoteConversation never re-sends the local
        # `agent` (and therefore this AgentContext) to the server when
        # reattaching to an existing sandbox conversation — it only GETs to
        # validate the agent kind — so building this for a resumed chat turn
        # would be pure waste (including the repo-plugin API calls below).
        all_repos_info = None
        if resume_state is None and has_repos:
            try:
                repo_sources = _gather_repo_sources(trigger)
                all_repos: list[dict] = []
                for source in repo_sources:
                    for repo in source.repositories:
                        all_repos.append(
                            {
                                "plugin_id": source.plugin_id,
                                "repo_id": repo["id"],
                                "full_name": repo["full_name"],
                                "owner": repo["owner"],
                                "repo_name": repo["repo_name"],
                                "clone_url": repo["clone_url"],
                            }
                        )
                if all_repos:
                    all_repos_info = all_repos
            except Exception as exc:
                logger.warning("Failed to gather repository info: %s", exc)
        if resume_state is None:
            # Inject trigger-specific metadata (action type, IDs, repo setup)
            # into the system suffix rather than muddying the initial user
            # message. Resumed turns skip this too — see above.
            system_suffix += build_trigger_suffix(trigger, all_repos_info)
        agent_context = AgentContext(skills=skills, system_message_suffix=system_suffix)

        def _run_sync() -> tuple[bool, bool, bool, str | None]:
            """Returns (stopped, errored, shutdown, error_detail) — see `_wait_for_done_or_stop`."""
            if resume_state is not None:
                handle = resume_state.handle
            else:
                handle = start_sandbox(
                    trigger.conversation_id,
                    git_committer_name=agent_config.git_committer_name,
                    git_committer_email=agent_config.git_committer_email,
                    env=agent_config.env_vars,
                )
            workspace = handle.workspace

            try:
                agent_kwargs: dict = {"llm": llm, "agent_context": agent_context}
                if mcp_config:
                    agent_kwargs["mcp_config"] = mcp_config

                if has_repos:
                    agent_kwargs["tools"] = get_default_tools() + make_repository_tool_specs(
                        trigger.project_id,
                        trigger.repo_plugin_ids,
                        api_base_url=settings.api_base_url,
                        api_key=settings.paca_api_key,
                    )

                agent = Agent(**agent_kwargs)
                # RemoteWorkspace → RemoteConversation; persistence_dir is not
                # supported for remote conversations (state lives in the
                # sandbox). On a cold start, do not pass conversation_id: the
                # sandbox server assigns its own UUID internally (passing our
                # application ID causes the server to 400 on subsequent GETs
                # — it only handles IDs it generated itself). Event callbacks
                # persist to our DB using trigger.conversation_id directly, so
                # the mapping stays correct regardless. On a chat resume, we
                # *do* pass the previously-remembered sandbox-assigned id so
                # the SDK reattaches to the same server-side conversation
                # (and its history) instead of creating a new one.
                #
                # delete_on_close=False for chat: conversation.close() below
                # runs after every turn (to stop the WS thread), including
                # turns that end in a pause rather than a real stop — it must
                # not delete the sandbox-side conversation the next turn
                # needs to reattach to.
                conversation = Conversation(
                    agent=agent,
                    workspace=workspace,
                    conversation_id=(resume_state.sdk_conversation_id if resume_state else None),
                    callbacks=[_make_event_callback(trigger, loop, counter, seen_events)],
                    max_iteration_per_run=agent_config.max_iterations,
                    visualizer=_QuietVisualizer(),
                    delete_on_close=(not is_chat),
                )

                # Register so the worker's stop handler can find this
                # conversation while this turn is in-flight.
                active_conversations[trigger.conversation_id] = conversation
                try:
                    if resume_state is not None:
                        # Reply text only — the agent already has full context.
                        conversation.send_message(trigger.message)
                    else:
                        # Cold start — build_initial_prompt handles the
                        # empty-message fallback (e.g. human task assignment).
                        conversation.send_message(build_initial_prompt(trigger))

                    # Use non-blocking run so our polling loop can be
                    # interrupted by a stop signal without waiting for the
                    # SDK timeout.
                    conversation.run(blocking=False)
                    reconcile = _make_reconciler(trigger, loop, counter, seen_events)
                    stopped, errored, shutdown, error_detail = _wait_for_done_or_stop(
                        conversation, stop_event, pause_event, reconcile
                    )
                finally:
                    active_conversations.pop(trigger.conversation_id, None)
                    # Stop the SDK's WebSocket client thread so it does not
                    # linger once we let go of `conversation`. For chat turns
                    # this does NOT delete the sandbox-side conversation
                    # (delete_on_close is False above) or touch the container
                    # — only a real stop/error tears those down, below.
                    try:
                        conversation.close()
                    except Exception as exc:
                        logger.debug("Failed to close conversation: %s", exc)
            except Exception:
                # Something went wrong before we could decide pause-vs-
                # teardown (e.g. Agent/Conversation construction itself
                # failed) — don't leak the sandbox, then re-raise so the
                # caller's except-block marks this conversation "failed".
                chat_sandboxes.pop(trigger.conversation_id, None)
                stop_sandbox(handle)
                raise

            if _keep_sandbox_alive(is_chat, errored, shutdown):
                # Natural finish or interrupt-only pause — keep the sandbox
                # alive, waiting for the next reply.
                chat_sandboxes[trigger.conversation_id] = ChatSandboxState(
                    handle=handle,
                    sdk_conversation_id=conversation.id,
                    project_id=trigger.project_id,
                    actor_user_id=trigger.actor_user_id,
                    last_active_at=time.monotonic(),
                )
            else:
                # Real teardown: explicit stop, an error, or (for non-chat
                # triggers) always — those never pause between turns.
                chat_sandboxes.pop(trigger.conversation_id, None)
                stop_sandbox(handle)

            return stopped, errored, shutdown, error_detail

        stopped, errored, shutdown, error_detail = await asyncio.get_event_loop().run_in_executor(
            None, _run_sync
        )
        result = _post_turn_status(is_chat, stopped, errored, shutdown)
        if result is not None:
            status, event_type = result
            await conversation_repository.update_conversation_status(
                trigger.conversation_id,
                status,
                error_message=error_detail if errored else None,
            )
            await stream_store.publish_realtime(
                project_id=trigger.project_id,
                conversation_id=trigger.conversation_id,
                event_type=event_type,
                actor_user_id=trigger.actor_user_id,
            )

    except Exception as exc:
        if not stop_event.is_set() and not pause_event.is_set():
            logger.exception("Conversation %s failed: %s", trigger.conversation_id, exc)
            await conversation_repository.update_conversation_status(
                trigger.conversation_id, ConversationStatus.FAILED, error_message=str(exc)
            )
            await stream_store.publish_realtime(
                project_id=trigger.project_id,
                conversation_id=trigger.conversation_id,
                event_type="agent.conversation.failed",
                actor_user_id=trigger.actor_user_id,
            )

    finally:
        stop_events.pop(trigger.conversation_id, None)
        pause_events.pop(trigger.conversation_id, None)


# ─── Paused chat sandbox teardown ─────────────────────────────────────────────


async def teardown_paused_chat_sandbox(conversation_id: str) -> bool:
    """Tear down a paused (idle, no in-flight run) chat sandbox, if one exists.

    Used for two cases where a chat sandbox needs to go away *between* turns
    (i.e. there is no in-flight `run_conversation` task to signal via
    `stop_events` — that path is `worker._handle_control`'s "agent.stop"
    branch): the user explicitly stopped the chat, or the idle reaper
    reclaimed it.

    Returns True if a sandbox was found and torn down.
    """
    if conversation_id in stop_events:
        # A turn is actively in flight for this conversation (registered for
        # the turn's full duration — see run_conversation) — chat_sandboxes
        # still holds its entry (looked up via .get(), not popped, in
        # _run_sync) but tearing it down here would yank the sandbox out
        # from under the running turn. Only the turn itself may retire this
        # entry once it finishes. This guards both callers: the idle reaper
        # (which only checks last_active_at, not liveness) and the
        # "agent.stop" fallback in worker._handle_control.
        return False
    entry = chat_sandboxes.pop(conversation_id, None)
    if entry is None:
        return False
    # stop_sandbox() blocks on container.stop(timeout=30) — a synchronous
    # Docker Engine API call that can take several seconds. Unlike
    # run_conversation's teardown (inside _run_sync, already offloaded via
    # run_in_executor), this coroutine runs directly on the worker's shared
    # event loop, so calling stop_sandbox() here inline would stall every
    # other concurrent coroutine (including the trigger-stream Valkey read in
    # worker.run_worker) for the container's full shutdown time.
    await asyncio.get_event_loop().run_in_executor(None, stop_sandbox, entry.handle)
    await conversation_repository.update_conversation_status(
        conversation_id, ConversationStatus.STOPPED
    )
    await stream_store.publish_realtime(
        project_id=entry.project_id,
        conversation_id=conversation_id,
        event_type="agent.conversation.stopped",
        actor_user_id=entry.actor_user_id,
    )
    return True


def _find_idle_chat_sandboxes(now: float, timeout_seconds: float) -> list[str]:
    """Return conversation_ids whose chat sandbox has been idle too long.

    Pure function over the current `chat_sandboxes` registry — split out from
    `reap_idle_chat_sandboxes` so the idle-selection logic is testable without
    driving the sleep loop. Excludes conversations with an in-flight turn
    (present in `stop_events`) — their sandbox entry may look stale by
    `last_active_at` alone (that field isn't updated while a turn is
    running, only at turn-end and by heartbeats), but they are not idle.
    """
    return [
        cid
        for cid, entry in list(chat_sandboxes.items())
        if now - entry.last_active_at > timeout_seconds and cid not in stop_events
    ]


async def reap_idle_chat_sandboxes() -> None:
    """Background loop: periodically tear down chat sandboxes idle too long.

    This is the disconnect-detection mechanism: the frontend refreshes
    last_active_at via "agent.heartbeat" control messages roughly every 30s
    while a conversation is loaded in a tab, so a sandbox only goes idle here
    once heartbeats actually stop (tab closed, crash, network loss). Runs
    until cancelled — started alongside the worker loop in `main.py`'s
    lifespan.
    """
    timeout_seconds = settings.chat_sandbox_idle_timeout_minutes * 60
    while True:
        await asyncio.sleep(20)
        for cid in _find_idle_chat_sandboxes(time.monotonic(), timeout_seconds):
            logger.info("Reaping idle chat sandbox for conversation %s", cid)
            try:
                await teardown_paused_chat_sandbox(cid)
            except Exception:
                logger.exception("Failed to reap idle chat sandbox for conversation %s", cid)
