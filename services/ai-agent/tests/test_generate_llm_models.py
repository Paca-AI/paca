from unittest.mock import AsyncMock

from scripts import generate_llm_models


async def test_build_includes_required_minimax_model(monkeypatch):
    monkeypatch.setattr(generate_llm_models.litellm, "model_cost", {})
    monkeypatch.setattr(
        generate_llm_models,
        "_base_url",
        AsyncMock(return_value="https://api.minimax.io/v1"),
    )

    catalog = await generate_llm_models._build()

    assert catalog["minimax"]["models"] == ["MiniMax-M2.7"]
