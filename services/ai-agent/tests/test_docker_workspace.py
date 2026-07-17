"""Tests for docker_workspace utility functions (no Docker daemon required)."""

from unittest.mock import patch

import pytest

from src.agent.docker_workspace import (
    _acquire_port,
    _detect_platform,
    _ports_in_use,
    _release_port,
)
from src.config import settings


@pytest.fixture(autouse=True)
def clean_port_pool():
    """Restore the port pool to empty before and after every test."""
    _ports_in_use.clear()
    yield
    _ports_in_use.clear()


# ─── _detect_platform ─────────────────────────────────────────────────────────


def test_x86_64_returns_amd64():
    with patch("src.agent.docker_workspace.platform_module.machine", return_value="x86_64"):
        assert _detect_platform() == "linux/amd64"


def test_aarch64_returns_arm64():
    with patch("src.agent.docker_workspace.platform_module.machine", return_value="aarch64"):
        assert _detect_platform() == "linux/arm64"


def test_armv7l_returns_arm64():
    with patch("src.agent.docker_workspace.platform_module.machine", return_value="armv7l"):
        assert _detect_platform() == "linux/arm64"


def test_unknown_arch_defaults_to_amd64():
    with patch("src.agent.docker_workspace.platform_module.machine", return_value="riscv64"):
        assert _detect_platform() == "linux/amd64"


# ─── Port pool ────────────────────────────────────────────────────────────────


def test_acquire_returns_first_available_port():
    port = _acquire_port()
    assert port == settings.port_pool_start


def test_acquired_port_is_tracked():
    port = _acquire_port()
    assert port in _ports_in_use


def test_second_acquire_returns_next_port():
    first = _acquire_port()
    second = _acquire_port()
    assert second == first + 1


def test_release_removes_port_from_pool():
    port = _acquire_port()
    _release_port(port)
    assert port not in _ports_in_use


def test_released_port_can_be_reacquired():
    port = _acquire_port()
    _release_port(port)
    reacquired = _acquire_port()
    assert reacquired == port


def test_exhausted_pool_raises_runtime_error():
    _ports_in_use.update(
        range(settings.port_pool_start, settings.port_pool_start + settings.port_pool_size)
    )
    with pytest.raises(RuntimeError, match="No ports available"):
        _acquire_port()


# ─── docker_daemon_url ────────────────────────────────────────────────────────


def test_docker_daemon_url_prefers_docker_host(monkeypatch):
    from src.agent import docker_workspace

    monkeypatch.setattr(docker_workspace.settings, "docker_host", "tcp://socket-proxy:2375")
    assert docker_workspace.docker_daemon_url() == "tcp://socket-proxy:2375"


def test_docker_daemon_url_wraps_bare_socket_path(monkeypatch):
    from src.agent import docker_workspace

    monkeypatch.setattr(docker_workspace.settings, "docker_host", "")
    monkeypatch.setattr(docker_workspace.settings, "docker_socket", "/var/run/docker.sock")
    assert docker_workspace.docker_daemon_url() == "unix:///var/run/docker.sock"


def test_docker_daemon_url_passes_through_socket_with_scheme(monkeypatch):
    from src.agent import docker_workspace

    monkeypatch.setattr(docker_workspace.settings, "docker_host", "")
    monkeypatch.setattr(docker_workspace.settings, "docker_socket", "tcp://legacy-proxy:2375")
    assert docker_workspace.docker_daemon_url() == "tcp://legacy-proxy:2375"


# ─── Sandbox extra networks (ADR-038 T3) ─────────────────────────────────────


def test_sandbox_extra_networks_parses_comma_separated(monkeypatch):
    from src.agent import docker_workspace

    monkeypatch.setattr(
        docker_workspace.settings, "sandbox_extra_networks", "galaxy_network, other-net ,"
    )
    assert docker_workspace._sandbox_extra_networks() == ["galaxy_network", "other-net"]


def test_sandbox_extra_networks_empty_by_default(monkeypatch):
    from src.agent import docker_workspace

    monkeypatch.setattr(docker_workspace.settings, "sandbox_extra_networks", "")
    assert docker_workspace._sandbox_extra_networks() == []


def test_start_sandbox_primary_network_excludes_extras_and_connects_them(monkeypatch):
    """Drive start_sandbox with a mocked daemon: the primary network must be
    the stack network even when an extra network (galaxy_network) is listed
    first alphabetically, and the extra must be connected before readiness."""
    from unittest.mock import MagicMock

    from src.agent import docker_workspace

    monkeypatch.setattr(
        docker_workspace.settings, "sandbox_extra_networks", "galaxy_network"
    )

    container = MagicMock()
    container.attrs = {
        "NetworkSettings": {"Networks": {"galaxy-paca_default": {"IPAddress": "10.0.0.9"}}}
    }
    client = MagicMock()
    client.containers.run.return_value = container
    galaxy_net = MagicMock()
    client.networks.get.return_value = galaxy_net

    events: list[str] = []
    galaxy_net.connect.side_effect = lambda c: events.append("connect")

    with (
        patch("src.agent.docker_workspace.docker.DockerClient", return_value=client),
        patch("src.agent.docker_workspace._is_inside_docker", return_value=True),
        # galaxy_network deliberately FIRST: alphabetical order must not win.
        patch(
            "src.agent.docker_workspace._get_current_networks",
            return_value=["galaxy_network", "galaxy-paca_default"],
        ),
        patch("src.agent.docker_workspace._get_app_host_path", return_value="/host/app"),
        patch(
            "src.agent.docker_workspace._wait_for_ready",
            side_effect=lambda *a, **k: events.append("ready"),
        ),
    ):
        handle = docker_workspace.start_sandbox("conv-1")

    run_kwargs = client.containers.run.call_args.kwargs
    assert run_kwargs["network"] == "galaxy-paca_default"
    client.networks.get.assert_called_once_with("galaxy_network")
    galaxy_net.connect.assert_called_once_with(container)
    # Secondary attach happens before the readiness wait — LLM calls need the
    # route from the very first agent step.
    assert events == ["connect", "ready"]
    assert handle.workspace.host == "http://10.0.0.9:8000"
