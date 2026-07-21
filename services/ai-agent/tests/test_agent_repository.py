"""Tests for the plugin-skills URL resolution safety helper.

resolve_plugin_base_url mirrors apps/mcp/src/plugin-loader.ts's
resolveImportUrl SSRF guard for plugin-declared URLs — see that module and
docs/plugins/skills-plugin-system.md for the full rationale. Test cases use
IP literals (rather than hostnames needing DNS resolution) wherever the
scheme allows it, so this suite runs fully offline and deterministically.
"""

from src.repositories.agent_repository import resolve_plugin_base_url

GATEWAY = "http://gateway"


async def test_relative_url_resolves_against_gateway():
    resolved = await resolve_plugin_base_url("/plugins-skills/com.paca.example", GATEWAY)
    assert resolved == "http://gateway/plugins-skills/com.paca.example"


async def test_https_public_ip_is_allowed():
    resolved = await resolve_plugin_base_url("https://1.1.1.1/skills", GATEWAY)
    assert resolved == "https://1.1.1.1/skills"


async def test_https_loopback_ip_is_rejected():
    assert await resolve_plugin_base_url("https://127.0.0.1/skills", GATEWAY) is None


async def test_https_private_rfc1918_ip_is_rejected():
    assert await resolve_plugin_base_url("https://10.0.0.5/skills", GATEWAY) is None


async def test_https_link_local_metadata_ip_is_rejected():
    # 169.254.169.254 is the cloud-provider instance-metadata address — a
    # classic SSRF target if plugin manifests could redirect fetches here.
    assert await resolve_plugin_base_url("https://169.254.169.254/skills", GATEWAY) is None


async def test_http_localhost_is_allowed():
    resolved = await resolve_plugin_base_url("http://localhost/skills", GATEWAY)
    assert resolved == "http://localhost/skills"


async def test_http_loopback_ip_is_allowed():
    resolved = await resolve_plugin_base_url("http://127.0.0.1/skills", GATEWAY)
    assert resolved == "http://127.0.0.1/skills"


async def test_http_matching_gateway_host_is_allowed():
    resolved = await resolve_plugin_base_url("http://gateway/skills", GATEWAY)
    assert resolved == "http://gateway/skills"


async def test_http_untrusted_host_is_rejected():
    assert await resolve_plugin_base_url("http://evil.example.com/skills", GATEWAY) is None


async def test_disallowed_scheme_is_rejected():
    assert await resolve_plugin_base_url("ftp://example.com/skills", GATEWAY) is None
