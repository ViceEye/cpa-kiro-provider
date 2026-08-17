#!/usr/bin/env python3
"""End-to-end CPA/Kiro plugin tests using only isolated fake credentials."""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from typing import Any


CPA = os.environ.get("CPA_BASE_URL", "http://cpa-kiro-provider-test:8317").rstrip("/")
MOCK = os.environ.get("KIRO_MOCK_BASE_URL", "http://kiro-mock:18080").rstrip("/")
API_KEY = os.environ.get("CPA_API_KEY", "kiro-provider-integration-key")
MANAGEMENT_KEY = os.environ.get(
    "CPA_MANAGEMENT_KEY", "kiro-provider-integration-management-key"
)


def request(path: str, payload: dict[str, Any] | None = None, *, mock: bool = False) -> bytes:
    base = MOCK if mock else CPA
    headers = {"Content-Type": "application/json"}
    if not mock:
        headers["Authorization"] = f"Bearer {API_KEY}"
    raw = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    req = urllib.request.Request(base + path, data=raw, headers=headers, method="POST" if raw is not None else "GET")
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", "replace")
        raise AssertionError(f"{path} returned HTTP {error.code}: {detail}") from error


def request_json(path: str, payload: dict[str, Any] | None = None, *, mock: bool = False) -> dict[str, Any]:
    return json.loads(request(path, payload, mock=mock))


def management_json(path: str) -> dict[str, Any]:
    req = urllib.request.Request(
        CPA + path,
        headers={"Authorization": f"Bearer {MANAGEMENT_KEY}"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            return json.loads(response.read())
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", "replace")
        raise AssertionError(f"{path} returned HTTP {error.code}: {detail}") from error


def control(statuses: list[int]) -> None:
    result = request_json("/control", {"statuses": statuses}, mock=True)
    assert result == {"ok": True}, result


def stats() -> dict[str, Any]:
    return request_json("/stats", mock=True)


def chat(model: str, *, stream: bool = False, tools: bool = False) -> bytes:
    payload: dict[str, Any] = {
        "model": model,
        "stream": stream,
        "messages": [{"role": "user", "content": "isolated integration test"}],
    }
    if tools:
        payload["tools"] = [
            {
                "type": "function",
                "function": {
                    "name": "fixture_tool",
                    "description": "Fixture tool",
                    "parameters": {"type": "object", "properties": {}},
                },
            }
        ]
    return request("/v1/chat/completions", payload)


def assert_two_account_failover(snapshot: dict[str, Any], attempts: int = 2) -> None:
    accounts = snapshot["accounts"]
    assert snapshot["generate_count"] == attempts, snapshot
    assert len(accounts) == attempts, snapshot
    assert accounts[0] != accounts[-1], snapshot
    assert "unknown" not in accounts, snapshot


def main() -> None:
    models = request_json("/v1/models")
    model_ids = {item["id"] for item in models["data"]}
    required = {
        "kiro/fixture-model",
        "kiro/failover-402",
        "kiro/failover-403",
        "kiro/failover-429",
        "kiro/failover-500",
        "kiro/gpt-5.6-sol",
        "kiro/gpt-5.6-terra",
        "kiro/gpt-5.6-luna",
    }
    assert required <= model_ids, model_ids

    quota = management_json("/v0/management/plugins/kiro-provider/quota")
    assert quota["provider"] == "kiro", quota
    assert len(quota["accounts"]) == 2, quota
    for account in quota["accounts"]:
        assert account["status"] == "ok", account
        assert account["subscription"] == "KIRO FIXTURE", account
        assert account["usage"], account
        credits = account["usage"][0]
        assert credits["usage_limit"] == 100, credits
        assert credits["current_usage"] == 25.5, credits
        assert credits["remaining"] == 74.5, credits
        assert credits["usage_percent"] == 25.5, credits

    control([])
    completion = json.loads(chat("kiro/fixture-model"))
    assert completion["choices"][0]["message"]["content"] == "mock success", completion

    stream = chat("kiro/fixture-model", stream=True).decode()
    assert "data: data:" not in stream, stream
    assert stream.count("data: [DONE]") == 1, stream
    assert '"content":"mock "' in stream and '"content":"success"' in stream, stream

    tool_completion = json.loads(chat("kiro/fixture-model", tools=True))
    tool_call = tool_completion["choices"][0]["message"]["tool_calls"][0]
    assert tool_call["function"]["name"] == "fixture_tool", tool_completion
    assert json.loads(tool_call["function"]["arguments"]) == {"value": "ok"}, tool_completion

    tool_stream = chat("kiro/fixture-model", stream=True, tools=True).decode()
    assert '"finish_reason":"tool_calls"' in tool_stream, tool_stream
    assert '"arguments":"{\\"value\\":\\"ok\\"}"' in tool_stream, tool_stream

    control([500, 200])
    assert json.loads(chat("kiro/failover-500"))["choices"][0]["message"]["content"] == "mock success"
    assert_two_account_failover(stats())

    control([402, 200])
    assert json.loads(chat("kiro/failover-402"))["choices"][0]["message"]["content"] == "mock success"
    assert_two_account_failover(stats())

    control([403, 403, 200])
    assert json.loads(chat("kiro/failover-403"))["choices"][0]["message"]["content"] == "mock success"
    snapshot = stats()
    assert_two_account_failover(snapshot, attempts=3)
    assert snapshot["accounts"][0] == snapshot["accounts"][1], snapshot
    assert snapshot["refresh_count"] == 1, snapshot
    assert snapshot["refresh_accounts"] == [snapshot["accounts"][0]], snapshot

    control([429, 200])
    failover_stream = chat("kiro/failover-429", stream=True).decode()
    assert "mock " in failover_stream and "success" in failover_stream, failover_stream
    assert_two_account_failover(stats())

    print("PASS: Kiro provider OpenAI Docker integration suite")


if __name__ == "__main__":
    main()
