"""Docker container sandbox for isolated OpenHands agent execution."""

from __future__ import annotations

import dataclasses
import io
import logging
import os
import platform as platform_module
import secrets
import socket
import tarfile
import threading
import time
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

import docker
import httpx
from docker.models.containers import Container
from openhands.sdk import RemoteWorkspace

from ..config import settings

logger = logging.getLogger(__name__)

# ─── Port pool ────────────────────────────────────────────────────────────────

_port_lock = threading.Lock()
_ports_in_use: set[int] = set()


def _acquire_port() -> int:
    with _port_lock:
        for p in range(
            settings.port_pool_start, settings.port_pool_start + settings.port_pool_size
        ):
            if p not in _ports_in_use:
                _ports_in_use.add(p)
                return p
    raise RuntimeError(
        f"No ports available in the agent port pool "
        f"({settings.port_pool_start}–{settings.port_pool_start + settings.port_pool_size - 1}). "
        "Consider increasing PORT_POOL_SIZE."
    )


def _release_port(port: int) -> None:
    with _port_lock:
        _ports_in_use.discard(port)


# ─── Helpers ──────────────────────────────────────────────────────────────────


def _detect_platform() -> str:
    machine = platform_module.machine().lower()
    if "arm" in machine or "aarch64" in machine:
        return "linux/arm64"
    return "linux/amd64"


def _is_inside_docker() -> bool:
    return os.path.exists("/.dockerenv")


def docker_daemon_url() -> str:
    """Resolve the Docker daemon base_url from settings.

    DOCKER_HOST takes precedence and is passed through verbatim, so a
    tcp:// socket proxy (ADR-038: no raw /var/run/docker.sock mount in prod)
    works out of the box.  DOCKER_SOCKET keeps its legacy bare-path form but
    values that already carry a scheme are also passed through.
    """
    if settings.docker_host:
        return settings.docker_host
    sock = settings.docker_socket
    if "://" in sock:
        return sock
    return f"unix://{sock}"


def _get_current_networks(client: docker.DockerClient) -> list[str]:
    """Return the Docker networks the current container is attached to."""
    try:
        hostname = socket.gethostname()
        info = client.containers.get(hostname)
        return list(info.attrs["NetworkSettings"]["Networks"].keys())
    except Exception as exc:
        logger.debug("Could not detect current container networks: %s", exc)
        return []


def _get_app_host_path(client: docker.DockerClient) -> str | None:
    """Return the host-side absolute path of the ai-agent application directory.

    Inside Docker: read it from the container's bind-mount table (the compose
    file mounts the source tree at /app).

    Outside Docker (local dev): derive it from the location of the ``src``
    package on disk.
    """
    if _is_inside_docker():
        return _find_host_path_for(client, "/app")
    # Local dev: walk up from this file's location to the project root
    # (…/services/ai-agent/src/agent/docker_workspace.py → …/services/ai-agent)
    try:
        return str(Path(__file__).resolve().parent.parent.parent)
    except Exception:
        return None


def _find_host_path_for(client: docker.DockerClient, container_path: str) -> str | None:
    """Return the host-side source of a bind mount in the current container.

    Used to re-share a directory that is already bind-mounted into this
    container into the sibling sandbox containers we spawn.  The Docker socket
    gives access to the *host* daemon, so volumes must be expressed as host
    paths — not paths that only exist inside this container.
    """
    try:
        hostname = socket.gethostname()
        info = client.containers.get(hostname)
        for mount in info.attrs.get("Mounts", []):
            if mount.get("Type") == "bind" and mount.get("Destination") == container_path:
                return mount["Source"]
    except Exception as exc:
        logger.debug("Could not inspect mounts for %s: %s", container_path, exc)
    return None


def _wait_for_ready(host: str, timeout: float = 120.0) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            resp = httpx.get(f"{host}/health", timeout=2.0)
            if resp.status_code < 500:
                return
        except Exception:
            pass
        time.sleep(1.0)
    raise TimeoutError(f"Agent server at {host} not ready after {timeout}s")


# ─── Repo tools injection ─────────────────────────────────────────────────────

_REPO_TOOLS_DEST = "/tmp/paca_tools"
_REPO_TOOLS_FILES = [
    ("src/__init__.py", "paca_tools/src/__init__.py"),
    ("src/agent/__init__.py", "paca_tools/src/agent/__init__.py"),
    ("src/agent/repo_tools.py", "paca_tools/src/agent/repo_tools.py"),
]


def _copy_repo_tools_to_container(container: Container) -> bool:
    """Copy src.agent.repo_tools into /tmp/paca_tools in the sandbox.

    Used in production where /app is baked into the image instead of
    bind-mounted from the host.  After this call, the agent server can
    run importlib.import_module("src.agent.repo_tools") when
    OH_EXTRA_PYTHON_PATH=/tmp/paca_tools is set.

    Returns True on success, False if any required source file is missing.
    """
    src_root = Path("/app")
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w") as tar:
        for dir_name in ["paca_tools", "paca_tools/src", "paca_tools/src/agent"]:
            info = tarfile.TarInfo(name=dir_name)
            info.type = tarfile.DIRTYPE
            info.mode = 0o755
            tar.addfile(info)
        for src_rel, dest_rel in _REPO_TOOLS_FILES:
            path = src_root / src_rel
            if not path.exists():
                logger.warning("Cannot copy repo_tools into sandbox: missing %s", path)
                return False
            tar.add(str(path), arcname=dest_rel)
    buf.seek(0)
    container.put_archive("/tmp", buf.getvalue())
    logger.debug("Copied repo_tools into sandbox container at %s", _REPO_TOOLS_DEST)
    return True


# ─── Sandbox handle ───────────────────────────────────────────────────────────


@dataclasses.dataclass
class SandboxHandle:
    """A live sandbox container, kept alive past a single start/stop pair.

    Returned by `start_sandbox()` and consumed by `stop_sandbox()`. Chat
    conversations hold onto this (via `core.registry.chat_sandboxes`) across
    multiple user turns instead of tearing the container down after each one.
    """

    client: docker.DockerClient
    container: Container
    workspace: RemoteWorkspace
    conversation_id: str
    host_port: int | None = None


# ─── Start / stop primitives ──────────────────────────────────────────────────


def start_sandbox(
    conversation_id: str,
    git_committer_name: str = "paca-agent",
    git_committer_email: str = "280579135+paca-agent@users.noreply.github.com",
    env: dict[str, str] | None = None,
) -> SandboxHandle:
    """Spin up an isolated agent-server container and return a SandboxHandle.

    Inside Docker (production/dev-compose)
    ───────────────────────────────────────
    The sandbox container joins the same Docker network as the ai-agent service.
    This is what makes the `api` and `gateway` hostnames resolvable inside the
    sandbox — MCP servers (including the built-in Paca MCP) call those services
    directly, so they must be reachable from within the container.

    Outside Docker (local dev)
    ──────────────────────────
    The container port is mapped to a host port from the pool and accessed via
    localhost.

    Unlike the old `docker_sandbox` contextmanager, this does **not** stop the
    container on its own — callers that want scoped auto-teardown should use
    `docker_sandbox()` below; callers that want the container to outlive this
    call (chat conversations kept alive between turns) must call
    `stop_sandbox()` explicitly when they're actually done with it.
    """
    client = docker.DockerClient(base_url=docker_daemon_url())
    container: Container | None = None
    host_port: int | None = None

    try:
        # ── Volumes ───────────────────────────────────────────────────────────
        volumes: dict = {}

        # Share the ai-agent source tree so the remote server can import
        # src.agent.repo_tools to register the custom repository tools.
        app_host_path = _get_app_host_path(client)
        if app_host_path:
            volumes[app_host_path] = {"bind": "/app", "mode": "ro"}
            logger.debug("Sharing app source into sandbox from host path: %s", app_host_path)
        else:
            # Production: /app is baked into the image (not bind-mounted).
            # We will copy the necessary files into the container after it starts.
            logger.debug(
                "No host bind-mount for /app detected — "
                "will inject repo_tools into the sandbox container directly."
            )

        # When DEV_MCP_PATH is set, the MCP server is a local file on a bind-
        # mounted volume (e.g. /mcp/build/index.js).  The OpenHands runtime
        # runs the MCP subprocess *inside* the sandbox container, so the same
        # volume must be forwarded there.  We resolve the host-side source of
        # the bind mount so Docker can re-attach it to the sibling container.
        if settings.dev_mcp_path and _is_inside_docker():
            path_parts = Path(settings.dev_mcp_path).parts
            if len(path_parts) < 2:
                logger.warning(
                    "DEV_MCP_PATH=%s has no parent directory component; "
                    "skipping sandbox volume injection.",
                    settings.dev_mcp_path,
                )
                path_parts = ()
            mcp_bind_dir = ("/" + path_parts[1]) if path_parts else None
            mcp_host_path = _find_host_path_for(client, mcp_bind_dir) if mcp_bind_dir else None
            if mcp_host_path:
                volumes[mcp_host_path] = {"bind": mcp_bind_dir, "mode": "ro"}
                logger.debug(
                    "Sharing MCP source into sandbox: host=%s → container=%s",
                    mcp_host_path,
                    mcp_bind_dir,
                )
            else:
                logger.warning(
                    "DEV_MCP_PATH=%s is set but %s is not a recognised bind-mount "
                    "on this container; the MCP server will likely fail to start "
                    "inside the sandbox.",
                    settings.dev_mcp_path,
                    mcp_bind_dir,
                )

        # ── Environment ───────────────────────────────────────────────────────
        # User-configured secret env vars are spread first so that the
        # hardcoded infra keys below always win on name collision — a
        # misconfigured agent variable can never shadow sandbox internals.
        environment: dict = {
            **(env or {}),
            # OH_SECRET_KEY is required by the agent server to encrypt persisted
            # secrets.  Each container is ephemeral so a fresh key per run is fine.
            "OH_SECRET_KEY": secrets.token_hex(32),
            # Suppress the SDK startup banner from container logs.
            "OPENHANDS_SUPPRESS_BANNER": "1",
            # Git committer identity — applied to every git command in the container.
            "GIT_AUTHOR_NAME": git_committer_name,
            "GIT_AUTHOR_EMAIL": git_committer_email,
            "GIT_COMMITTER_NAME": git_committer_name,
            "GIT_COMMITTER_EMAIL": git_committer_email,
            # Add the app source to Python's module search path so the agent
            # server can run importlib.import_module("src.agent.repo_tools").
            # Value depends on whether we share via bind-mount or file injection.
            "OH_EXTRA_PYTHON_PATH": "/app" if app_host_path else _REPO_TOOLS_DEST,
        }

        run_kwargs: dict = {
            "image": settings.agent_server_image,
            "detach": True,
            "platform": _detect_platform(),
            # Automatically deleted when stopped — no manual cleanup needed.
            "remove": True,
            "labels": {
                "paca.conversation_id": conversation_id,
                "paca.managed": "true",
            },
            "environment": environment,
        }
        if volumes:
            run_kwargs["volumes"] = volumes

        if _is_inside_docker():
            # Join the same network as this container so the sandbox can reach
            # `api` and `gateway` by their Docker Compose service hostnames.
            networks = _get_current_networks(client)
            network = networks[0] if networks else "bridge"
            run_kwargs["network"] = network

            logger.info(
                "Starting agent sandbox: conversation=%s network=%s image=%s",
                conversation_id,
                network,
                settings.agent_server_image,
            )
            container = client.containers.run(**run_kwargs)
            container.reload()
            container_ip = container.attrs["NetworkSettings"]["Networks"][network]["IPAddress"]
            host = f"http://{container_ip}:{settings.agent_server_container_port}"
        else:
            # Local dev: map to a pooled host port and access via localhost.
            host_port = _acquire_port()
            run_kwargs["ports"] = {f"{settings.agent_server_container_port}/tcp": host_port}

            logger.info(
                "Starting agent sandbox: conversation=%s host_port=%d image=%s",
                conversation_id,
                host_port,
                settings.agent_server_image,
            )
            container = client.containers.run(**run_kwargs)
            host = f"http://localhost:{host_port}"

        if not app_host_path:
            # Production path: inject repo_tools directly into the container
            # filesystem before the server finishes starting up.
            try:
                _copy_repo_tools_to_container(container)
            except Exception as exc:
                logger.warning(
                    "Failed to inject repo_tools into sandbox — "
                    "repository tools (list_repositories, clone_repository, …) "
                    "will not be available: %s",
                    exc,
                )

        _wait_for_ready(host)
        logger.info("Agent sandbox ready: conversation=%s host=%s", conversation_id, host)

        return SandboxHandle(
            client=client,
            container=container,
            workspace=RemoteWorkspace(host=host, working_dir="/workspace"),
            conversation_id=conversation_id,
            host_port=host_port,
        )

    except Exception:
        # Nothing has taken ownership of this container/port yet (start_sandbox
        # hasn't returned) — clean up whatever partially started before
        # re-raising, since there's no caller-side `stop_sandbox()` to do it.
        if container is not None:
            try:
                container.stop(timeout=30)
            except Exception as exc:
                logger.warning(
                    "Failed to stop partially-started sandbox for conversation=%s: %s",
                    conversation_id,
                    exc,
                )
        if host_port is not None:
            _release_port(host_port)
        client.close()
        raise


def stop_sandbox(handle: SandboxHandle) -> None:
    """Tear down a sandbox previously returned by `start_sandbox()`."""
    try:
        handle.container.stop(timeout=30)
        logger.info("Agent sandbox stopped: conversation=%s", handle.conversation_id)
    except Exception as exc:
        logger.warning(
            "Failed to stop agent container for conversation=%s: %s",
            handle.conversation_id,
            exc,
        )
    if handle.host_port is not None:
        _release_port(handle.host_port)
    handle.client.close()


# ─── Context manager (non-chat triggers: scoped to a single run) ─────────────


@contextmanager
def docker_sandbox(
    conversation_id: str,
    git_committer_name: str = "paca-agent",
    git_committer_email: str = "280579135+paca-agent@users.noreply.github.com",
    env: dict[str, str] | None = None,
) -> Iterator[RemoteWorkspace]:
    """Start a sandbox for the duration of the `with` block, then stop it.

    Thin wrapper around `start_sandbox()`/`stop_sandbox()` for trigger types
    that always run exactly one turn per sandbox (task assignment, comment
    mentions, description writes). Chat conversations use the two primitives
    directly so the sandbox can outlive a single turn.
    """
    handle = start_sandbox(
        conversation_id,
        git_committer_name=git_committer_name,
        git_committer_email=git_committer_email,
        env=env,
    )
    try:
        yield handle.workspace
    finally:
        stop_sandbox(handle)
