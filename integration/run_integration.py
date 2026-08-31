#!/usr/bin/env python3
"""End-to-end CPA/Kiro plugin tests using only isolated fake credentials."""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request
import time
from typing import Any


CPA = os.environ.get("CPA_BASE_URL", "http://cpa-provider-nexus-test:8317").rstrip("/")
MOCK = os.environ.get("KIRO_MOCK_BASE_URL", "http://kiro-mock:18080").rstrip("/")
API_KEY = os.environ.get("CPA_API_KEY", "cpa-provider-nexus-integration-key")
MANAGEMENT_KEY = os.environ.get(
    "CPA_MANAGEMENT_KEY", "cpa-provider-nexus-integration-management-key"
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


def public_request(path: str, expected_status: int = 200) -> bytes:
    req = urllib.request.Request(CPA + path, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            assert response.status == expected_status, response.status
            return response.read()
    except urllib.error.HTTPError as error:
        body = error.read()
        if error.code == expected_status:
            return body
        raise AssertionError(
            f"{path} returned HTTP {error.code}, expected {expected_status}: "
            f"{body.decode('utf-8', 'replace')}"
        ) from error


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
    plugins = management_json("/v0/management/plugins")
    kiro = next(item for item in plugins["plugins"] if item["id"] == "cpa-provider-nexus")
    assert kiro["effective_enabled"] is True, kiro
    assert kiro["supports_oauth"] is True, kiro
    assert kiro["oauth_provider"] == "nexus", kiro

    models = request_json("/v1/models")
    model_ids = {item["id"] for item in models["data"]}
    required = {
        "nexus/fixture-model",
        "nexus/failover-402",
        "nexus/failover-403",
        "nexus/failover-429",
        "nexus/failover-500",
        "nexus/gpt-5.6-sol",
        "nexus/gpt-5.6-terra",
        "nexus/gpt-5.6-luna",
    }
    assert required <= model_ids, model_ids

    quota = management_json("/v0/management/plugins/cpa-provider-nexus/quota")
    assert quota["provider"] == "nexus", quota
    assert len(quota["accounts"]) >= 2, quota
    assert {"Kiro Fake Account A", "Kiro Fake Account B"} <= {
        account["label"] for account in quota["accounts"]
    }, quota
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
    completion = json.loads(chat("nexus/fixture-model"))
    assert completion["choices"][0]["message"]["content"] == "mock success", completion

    stream = chat("nexus/fixture-model", stream=True).decode()
    assert "data: data:" not in stream, stream
    assert stream.count("data: [DONE]") == 1, stream
    assert '"content":"mock "' in stream and '"content":"success"' in stream, stream

    tool_completion = json.loads(chat("nexus/fixture-model", tools=True))
    tool_call = tool_completion["choices"][0]["message"]["tool_calls"][0]
    assert tool_call["function"]["name"] == "fixture_tool", tool_completion
    assert json.loads(tool_call["function"]["arguments"]) == {"value": "ok"}, tool_completion

    tool_stream = chat("nexus/fixture-model", stream=True, tools=True).decode()
    assert '"finish_reason":"tool_calls"' in tool_stream, tool_stream
    assert '"arguments":"{\\"value\\":\\"ok\\"}"' in tool_stream, tool_stream

    control([500, 200])
    assert json.loads(chat("nexus/failover-500"))["choices"][0]["message"]["content"] == "mock success"
    assert_two_account_failover(stats())

    control([402, 200])
    assert json.loads(chat("nexus/failover-402"))["choices"][0]["message"]["content"] == "mock success"
    assert_two_account_failover(stats())

    control([403, 403, 200])
    assert json.loads(chat("nexus/failover-403"))["choices"][0]["message"]["content"] == "mock success"
    snapshot = stats()
    assert_two_account_failover(snapshot, attempts=3)
    assert snapshot["accounts"][0] == snapshot["accounts"][1], snapshot
    assert snapshot["refresh_count"] == 1, snapshot
    assert snapshot["refresh_accounts"] == [snapshot["accounts"][0]], snapshot

    control([429, 200])
    failover_stream = chat("nexus/failover-429", stream=True).decode()
    assert "mock " in failover_stream and "success" in failover_stream, failover_stream
    assert_two_account_failover(stats())

    login = management_json("/v0/management/kiro-auth-url")
    assert login["status"] == "ok" and login["state"], login
    login_url = urllib.parse.urlparse(login["url"])
    login_query = urllib.parse.parse_qs(login_url.query)
    assert login_url.netloc == "app.kiro.dev" and login_url.path == "/signin", login
    assert login_query["redirect_uri"] == [
        "http://localhost:8317/v0/resource/plugins/cpa-provider-nexus/oauth"
    ], login_query

    callback_path = "/v0/resource/plugins/cpa-provider-nexus/oauth/signin/callback?" + urllib.parse.urlencode(
        {"state": login["state"], "code": "fixture-browser-code"}
    )
    callback_page = public_request(callback_path).decode()
    assert "Kiro authorization received" in callback_page, callback_page

    invalid_path = "/v0/resource/plugins/cpa-provider-nexus/oauth?" + urllib.parse.urlencode(
        {"state": "00000000-0000-4000-8000-000000000000", "code": "fixture-browser-code"}
    )
    public_request(invalid_path, expected_status=400)

    status: dict[str, Any] = {"status": "wait"}
    for _ in range(10):
        status = management_json(
            "/v0/management/get-auth-status?" + urllib.parse.urlencode({"state": login["state"]})
        )
        if status["status"] != "wait":
            break
        time.sleep(0.2)
    assert status["status"] == "ok", status

    print("PASS: Kiro provider OpenAI and public OAuth Resource Route integration suite")


if __name__ == "__main__":
    main()
