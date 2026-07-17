"""Tests for fail-fast configuration validation (ADR-038).

config.py exits the process at import time when required settings are
missing, so these tests exercise it in a subprocess rather than in-process.
"""

import os
import subprocess
import sys
from pathlib import Path

SERVICE_ROOT = Path(__file__).resolve().parent.parent


def _import_config(extra_env: dict[str, str]) -> subprocess.CompletedProcess:
    env = {
        **os.environ,
        "DATABASE_URL": "postgresql://test:test@localhost/test",
        **extra_env,
    }
    return subprocess.run(
        [sys.executable, "-c", "import src.config"],
        cwd=SERVICE_ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )


def test_empty_internal_api_key_fails_fast_with_clear_error():
    result = _import_config({"INTERNAL_API_KEY": ""})
    assert result.returncode == 1
    assert "FATAL" in result.stderr
    assert "INTERNAL_API_KEY" in result.stderr


def test_whitespace_internal_api_key_fails_fast():
    result = _import_config({"INTERNAL_API_KEY": "   "})
    assert result.returncode == 1
    assert "FATAL" in result.stderr


def test_valid_internal_api_key_imports_cleanly():
    result = _import_config({"INTERNAL_API_KEY": "some-strong-secret"})
    assert result.returncode == 0, result.stderr
