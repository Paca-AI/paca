#!/usr/bin/env python3
"""
Generate the LLM provider/model list and write it to data/llm_models.json.

Run this script whenever you want to refresh the model list:

    cd services/ai-agent
    .venv/bin/python scripts/generate_llm_models.py

The output file is committed to the repository and served by the FastAPI
endpoint at runtime — no litellm calls happen during normal API serving.

os._exit(0) is used at the end to force-exit past threads that were abandoned
by the per-provider timeout (some providers launch interactive auth flows that
block stdin indefinitely).
"""

from __future__ import annotations

import asyncio
import contextlib
import io
import json
import os
from collections import defaultdict
from pathlib import Path

import litellm
from litellm.utils import get_api_base

_OUTPUT = Path(__file__).parent.parent / "data" / "llm_models.json"

# litellm.utils.get_api_base only resolves base URLs for a handful of providers
# (openai, gemini, and those that expose dynamic_api_base via get_llm_provider).
# Providers like anthropic use a native non-OpenAI API and are missed, so we
# fall back to a static map for the well-known ones.
_KNOWN_BASE_URLS: dict[str, str] = {
    "anthropic": "https://api.anthropic.com",
    "cohere": "https://api.cohere.com",
    "cohere_chat": "https://api.cohere.com",
    "openrouter": "https://openrouter.ai/api/v1",
    "replicate": "https://api.replicate.com/v1",
    "minimax": "https://api.minimax.io/v1",
}

# Keep newly released models available while the pinned LiteLLM catalog catches up.
_REQUIRED_MODELS: dict[str, tuple[str, ...]] = {
    "minimax": ("MiniMax-M2.7",),
}

# These providers launch interactive device-auth flows (GitHub OAuth, ChatGPT
# OAuth) when get_api_base is called, blocking forever on stdin.  Skip them.
_SKIP_PROVIDERS: frozenset[str] = frozenset({"chatgpt", "github_copilot"})


def _get_api_base_silent(model_str: str) -> str | None:
    """Call get_api_base while suppressing any stdout/stderr it produces."""
    sink = io.StringIO()
    with contextlib.redirect_stdout(sink), contextlib.redirect_stderr(sink):
        return get_api_base(model_str, {}) or None


async def _base_url(provider: str, model: str) -> str | None:
    """Return the default base URL for *provider*, or None if unknown."""
    if provider in _SKIP_PROVIDERS:
        return _KNOWN_BASE_URLS.get(provider)
    try:
        url = await asyncio.wait_for(
            asyncio.to_thread(_get_api_base_silent, f"{provider}/{model}"),
            timeout=5.0,
        )
        if url:
            return url
    except Exception:
        # Non-fatal: provider resolution can fail/time out; fall back to static map.
        pass
    return _KNOWN_BASE_URLS.get(provider)


async def _build() -> dict[str, dict]:
    provider_to_models: defaultdict[str, list[str]] = defaultdict(list)

    for model_name, metadata in litellm.model_cost.items():
        provider = metadata.get("litellm_provider")
        if not provider:
            continue
        provider = provider.lower()
        # Strip repeated provider prefixes so stored names are always bare.
        # Some litellm entries are double-prefixed (e.g. "openrouter/openrouter/auto"),
        # so strip in a loop until the name no longer starts with "{provider}/".
        bare = model_name
        prefix = f"{provider}/"
        while bare.lower().startswith(prefix):
            bare = bare[len(prefix):]
        provider_to_models[provider].append(bare)

    for provider, models in _REQUIRED_MODELS.items():
        for model in models:
            if model not in provider_to_models[provider]:
                provider_to_models[provider].append(model)

    providers = sorted(provider_to_models.items())
    print(f"Resolving base URLs for {len(providers)} providers…", flush=True)

    base_urls = await asyncio.gather(
        *[_base_url(p, models[0]) for p, models in providers]
    )

    return {
        provider: {
            "models": sorted(models),
            "base_url": base_url,
        }
        for (provider, models), base_url in zip(providers, base_urls)
    }


if __name__ == "__main__":
    # Use loop.run_until_complete + loop.close() instead of asyncio.run() so that
    # shutdown_default_executor() is never called — it would block indefinitely waiting
    # for threads that are stuck in interactive auth flows (e.g. chatgpt, github_copilot).
    loop = asyncio.new_event_loop()
    data = loop.run_until_complete(_build())
    loop.close()

    _OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    tmp = _OUTPUT.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(data, indent=2) + "\n")
    tmp.replace(_OUTPUT)

    with_url = sum(1 for v in data.values() if v["base_url"])
    print(f"Written {len(data)} providers ({with_url} with base URL) → {_OUTPUT}")

    os._exit(0)
