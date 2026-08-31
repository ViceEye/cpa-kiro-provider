#!/usr/bin/env python3
"""Network-isolated Kiro protocol fixture server for CPA plugin tests."""

from __future__ import annotations

import json
import struct
import threading
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import parse_qs, urlparse


LOCK = threading.Lock()
STATE: dict[str, Any] = {
    "generate_count": 0,
    "accounts": [],
    "paths": [],
    "statuses": [],
    "refresh_count": 0,
    "refresh_accounts": [],
}


def _string_header(name: str, value: str) -> bytes:
    name_bytes = name.encode("utf-8")
    value_bytes = value.encode("utf-8")
    return bytes([len(name_bytes)]) + name_bytes + bytes([7]) + struct.pack(">H", len(value_bytes)) + value_bytes


def event_frame(payload: dict[str, Any]) -> bytes:
    headers = _string_header(":message-type", "event") + _string_header(
        ":event-type", "assistantResponseEvent"
    )
    body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    total_length = 16 + len(headers) + len(body)
    prelude = struct.pack(">II", total_length, len(headers))
    message = prelude + struct.pack(">I", zlib.crc32(prelude)) + headers + body
    return message + struct.pack(">I", zlib.crc32(message))


def account_name(authorization: str) -> str:
    if "fake-account-a-access" in authorization:
        return "account-a"
    if "fake-account-b-access" in authorization:
        return "account-b"
    if "fake-browser-access" in authorization:
        return "account-browser"
    return "unknown"


class Handler(BaseHTTPRequestHandler):
    server_version = "KiroFixture/1.0"

    def log_message(self, _format: str, *_args: Any) -> None:
        return

    def _json(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/stats":
            with LOCK:
                snapshot = dict(STATE)
            self._json(200, snapshot)
            return
        if parsed.path == "/getUsageLimits":
            query = parse_qs(parsed.query)
            if query.get("origin") != ["AI_EDITOR"] or query.get("resourceType") != ["AGENTIC_REQUEST"]:
                self._json(400, {"message": "improper usage query"})
                return
            self._json(
                200,
                {
                    "daysUntilReset": 7,
                    "nextDateReset": 1788220800,
                    "subscriptionInfo": {
                        "subscriptionTitle": "KIRO FIXTURE",
                        "type": "FIXTURE_PLAN",
                    },
                    "overageConfiguration": {"overageStatus": "DISABLED"},
                    "usageBreakdownList": [
                        {
                            "resourceType": "CREDIT",
                            "displayName": "Credit",
                            "unit": "INVOCATIONS",
                            "currency": "USD",
                            "currentUsageWithPrecision": 25.5,
                            "usageLimitWithPrecision": 100,
                            "currentOveragesWithPrecision": 0,
                            "overageCapWithPrecision": 0,
                            "overageRate": 0,
                            "overageCharges": 0,
                            "nextDateReset": 1788220800,
                        }
                    ],
                },
            )
            return
        self._json(404, {"message": "not found"})

    def do_POST(self) -> None:
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)
        try:
            request = json.loads(raw)
        except json.JSONDecodeError:
            self._json(400, {"message": "invalid JSON"})
            return

        if self.path == "/control":
            statuses = request.get("statuses", [])
            if not isinstance(statuses, list) or not all(isinstance(item, int) for item in statuses):
                self._json(400, {"message": "statuses must be an integer array"})
                return
            with LOCK:
                STATE["generate_count"] = 0
                STATE["accounts"] = []
                STATE["paths"] = []
                STATE["statuses"] = statuses
                STATE["refresh_count"] = 0
                STATE["refresh_accounts"] = []
            self._json(200, {"ok": True})
            return

        if self.path == "/refreshToken":
            refresh_token = str(request.get("refreshToken", ""))
            account = "account-a" if "account-a" in refresh_token else "account-b" if "account-b" in refresh_token else "account-browser" if "browser" in refresh_token else "unknown"
            access_token = "fake-browser-access" if account == "account-browser" else f"fake-{account}-access" if account != "unknown" else "fake-refreshed-access"
            with LOCK:
                STATE["refresh_count"] += 1
                STATE["refresh_accounts"].append(account)
            self._json(
                200,
                {"accessToken": access_token, "refreshToken": refresh_token, "expiresIn": 3600},
            )
            return

        if self.path == "/oauth/token":
            if (
                request.get("code") != "fixture-browser-code"
                or not request.get("code_verifier")
                or request.get("redirect_uri")
                not in {
                    "http://localhost:3128",
                    "http://localhost:8317/v0/resource/plugins/cpa-provider-nexus/oauth",
                }
            ):
                self._json(400, {"message": "invalid browser token exchange"})
                return
            self._json(
                200,
                {
                    "accessToken": "fake-browser-access",
                    "refreshToken": "fake-browser-refresh",
                    "profileArn": "arn:aws:codewhisperer:eu-west-1:000000000000:profile/browser-fixture",
                    "expiresIn": 3600,
                },
            )
            return

        target = self.headers.get("X-Amz-Target", "")
        if target == "AmazonCodeWhispererService.ListAvailableProfiles":
            self._json(
                200,
                {
                    "profiles": [
                        {
                            "arn": "arn:aws:codewhisperer:us-east-1:000000000000:profile/fixture"
                        }
                    ]
                },
            )
            return

        if target == "AmazonCodeWhispererService.ListAvailableModels":
            self._json(
                200,
                {
                    "models": [
                        {
                            "modelId": "fixture-model",
                            "modelName": "Fixture Model",
                            "description": "Dynamic fixture model",
                            "rateMultiplier": 1.0,
                            "rateUnit": "Credit",
                            "supportedInputTypes": ["TEXT", "IMAGE"],
                            "tokenLimits": {"maxInputTokens": 272000, "maxOutputTokens": 64000},
                        },
                        {"modelId": "failover-402"},
                        {"modelId": "failover-403"},
                        {"modelId": "failover-429"},
                        {"modelId": "failover-500"},
                        {"modelId": "gpt-5.6-sol"},
                        {"modelId": "gpt-5.6-terra"},
                        {"modelId": "gpt-5.6-luna"},
                    ]
                },
            )
            return

        authorization = self.headers.get("Authorization", "")
        account = account_name(authorization)
        with LOCK:
            STATE["generate_count"] += 1
            request_number = STATE["generate_count"]
            STATE["accounts"].append(account)
            STATE["paths"].append(self.path)
            statuses = list(STATE["statuses"])
        status = statuses[request_number - 1] if request_number <= len(statuses) else 200
        if status != 200:
            self._json(status, {"message": f"fixture status {status}"})
            return

        context = (
            request.get("conversationState", {})
            .get("currentMessage", {})
            .get("userInputMessage", {})
            .get("userInputMessageContext", {})
        )
        frames: list[bytes]
        if context.get("tools"):
            frames = [
                event_frame({"name": "fixture_tool", "toolUseId": "call_fixture", "input": {}}),
                event_frame({"input": "{\"value\":\"ok\"}"}),
                event_frame({"stop": True}),
                event_frame({"usage": 2, "contextUsagePercentage": 4.5}),
            ]
        else:
            frames = [
                event_frame({"content": "mock "}),
                event_frame({"content": "success"}),
                event_frame({"usage": 1, "contextUsagePercentage": 2.5}),
            ]
        body = b"".join(frames)
        self.send_response(200)
        self.send_header("Content-Type", "application/vnd.amazon.eventstream")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 18080), Handler).serve_forever()
