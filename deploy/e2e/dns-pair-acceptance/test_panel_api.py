#!/usr/bin/env python3
"""Offline tests: Panel API client against recorded/fake responses."""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from panel_api import (  # noqa: E402
    PanelClient,
    PanelError,
    PinnedHTTPSTransport,
    PollMutationError,
    Response,
    UnknownOutcome,
)
from redaction import REDACTED, Redactor  # noqa: E402

PASSWORD = "Cp7!fixture-password-value-123"
COOKIE = "session-token-abcdef0123456789"


class Script:
    """A transport that answers from a list of (method, path) -> Response rules."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, str, dict[str, str], bytes | None]] = []
        self.rules: list[tuple[str, str, object]] = []

    def on(self, method: str, path: str, response: object) -> None:
        self.rules.append((method, path, response))

    def __call__(self, method, path, headers, body, timeout):  # noqa: ANN001
        self.calls.append((method, path, dict(headers), body))
        for index, (m, p, response) in enumerate(self.rules):
            if m == method and p == path:
                if isinstance(response, list):
                    value = response.pop(0) if len(response) > 1 else response[0]
                else:
                    value = response
                if isinstance(value, BaseException):
                    raise value
                return value
        return Response(404, [], b'{"error":"not found"}')


def ok(body: object, status: int = 200, headers: list | None = None) -> Response:
    return Response(status, headers or [("Content-Type", "application/json")], json.dumps(body).encode())


class PanelClientTest(unittest.TestCase):
    def setUp(self) -> None:
        self.script = Script()
        self.captured: list[dict] = []
        self.redactor = Redactor()
        self.client = PanelClient("debian13", "https://127.0.0.1:28443", self.script, self.redactor,
                                  self.captured.append, clock=self.fake_clock, sleep=self.fake_sleep)
        self.now = 0.0

    def fake_clock(self) -> float:
        return self.now

    def fake_sleep(self, seconds: float) -> None:
        self.now += seconds

    def login_ok(self) -> None:
        self.script.on("POST", "/api/v1/auth/login", ok(
            {"username": "owner", "role": "admin"},
            headers=[("Set-Cookie", f"celikpanel_session={COOKIE}; Path=/; HttpOnly; Secure; SameSite=Lax")]))
        self.script.on("GET", "/api/v1/auth/me", ok({"username": "owner", "role": "admin", "effective_role": "admin"}))

    def test_login_headers_and_redaction(self) -> None:
        self.login_ok()
        identity = self.client.login("owner", PASSWORD)
        self.assertEqual(identity["role"], "admin")
        post = self.script.calls[0]
        self.assertEqual(post[2]["Origin"], "https://127.0.0.1:28443")
        self.assertEqual(post[2]["Host"], "127.0.0.1:28443")
        self.assertNotIn("Cookie", post[2])
        get = self.script.calls[1]
        self.assertNotIn("Origin", get[2])
        self.assertEqual(get[2]["Cookie"], f"celikpanel_session={COOKIE}")
        blob = json.dumps(self.captured)
        self.assertNotIn(PASSWORD, blob)
        self.assertNotIn(COOKIE, blob)
        self.assertEqual(self.captured[0]["request"]["body"]["password"], REDACTED)
        self.assertIn(["Set-Cookie", REDACTED], self.captured[0]["response"]["headers"])
        self.assertEqual(self.client.mutations[0]["path"], "/api/v1/auth/login")

    def test_totp_and_non_admin_refused(self) -> None:
        self.script.on("POST", "/api/v1/auth/login", ok({"totp_required": True, "pending_token": "pending-token-xyz"}))
        with self.assertRaisesRegex(PanelError, "TOTP"):
            self.client.login("owner", PASSWORD)
        self.assertNotIn("pending-token-xyz", json.dumps(self.captured))

    def test_poll_cannot_mutate(self) -> None:
        self.login_ok()
        self.client.login("owner", PASSWORD)
        self.script.on("GET", "/api/v1/setup/operation?request_id=x", ok({"status": "running"}))

        def sneaky_view(view):  # noqa: ANN001
            view.post("/api/v1/setup/start", {})

        with self.assertRaises(PollMutationError):
            self.client.poll(sneaky_view, lambda value: False, timeout=10)

        def sneaky_client(view):  # noqa: ANN001
            view.get("/api/v1/setup/operation?request_id=x")
            self.client.post("/api/v1/setup/start", {"plan_id": "p"})

        before = len(self.script.calls)
        with self.assertRaises(PollMutationError):
            self.client.poll(sneaky_client, lambda value: False, timeout=10)
        methods = [call[0] for call in self.script.calls[before:]]
        self.assertEqual(methods, ["GET"])
        self.assertEqual(self.captured[-1]["outcome"], "refused-by-driver")
        # After the poll, mutations are allowed again.
        self.script.on("POST", "/api/v1/setup/plan", ok({"id": "p"}))
        self.assertEqual(self.client.post("/api/v1/setup/plan", {"revision": 1}).status, 200)

    def test_poll_reads_until_done_and_reports_changes(self) -> None:
        states = [ok({"status": "running", "phase": "01-dns"}), ok({"status": "running", "phase": "01-dns"}),
                  ok({"status": "waiting", "phase": "access_dns"})]
        self.script.on("GET", "/api/v1/setup/operation?request_id=r", states)
        changes = []
        done, last, reads = self.client.poll(
            lambda view: view.get("/api/v1/setup/operation?request_id=r").json(),
            lambda value: value["status"] == "waiting", timeout=60, interval=3, on_change=changes.append)
        self.assertTrue(done)
        self.assertEqual(reads, 3)
        self.assertEqual([c["status"] for c in changes], ["running", "waiting"])
        self.assertTrue(all(call[0] == "GET" for call in self.script.calls))

    def test_poll_timeout(self) -> None:
        self.script.on("GET", "/api/v1/dns/engine", ok({"pair_ready": False}))
        done, last, reads = self.client.poll(lambda view: view.get("/api/v1/dns/engine").json(),
                                             lambda value: value["pair_ready"], timeout=9, interval=3)
        self.assertFalse(done)
        self.assertEqual(reads, 4)

    def test_poll_stops_early_on_a_stable_failing_value(self) -> None:
        self.script.on("GET", "/api/v1/dns/engine", ok({"pair_ready": False}))
        done, last, reads = self.client.poll(lambda view: view.get("/api/v1/dns/engine").json(),
                                             lambda value: value["pair_ready"], timeout=2700, interval=5,
                                             stable_after=300, stable_polls=5)
        self.assertFalse(done)
        self.assertEqual(self.client.last_poll["stop"], "stable")
        self.assertEqual(self.client.last_poll["stable_seconds"], 300)
        self.assertEqual(reads, 61)
        self.assertEqual(self.now, 300)

    def test_poll_keeps_waiting_while_the_value_changes_or_is_in_progress(self) -> None:
        self.script.on("GET", "/api/v1/dns/engine", [ok({"pair_ready": False, "revision": i}) for i in range(100)]
                       + [ok({"pair_ready": True, "revision": 100})])
        done, last, reads = self.client.poll(lambda view: view.get("/api/v1/dns/engine").json(),
                                             lambda value: value["pair_ready"], timeout=2700, interval=5,
                                             stable_after=300)
        self.assertTrue(done)
        self.assertEqual(self.client.last_poll["stop"], "done")
        self.assertEqual(reads, 101)
        self.now = 0.0
        self.script.on("GET", "/api/v1/setup/operation?request_id=r", ok({"status": "running"}))
        done, last, reads = self.client.poll(lambda view: view.get("/api/v1/setup/operation?request_id=r").json(),
                                             lambda value: False, timeout=900, interval=5, stable_after=300,
                                             settled=lambda value: value.get("status") != "running")
        self.assertFalse(done)
        self.assertEqual(self.client.last_poll["stop"], "timeout", "a running step is progress, not a settled state")
        self.assertEqual(self.now, 900)

    def test_poll_bounds_a_listed_unknown_state_even_while_it_changes(self) -> None:
        # D-024 time bound: an unknown state is stopped at the limit although it is
        # "running" (never stable-stopped) and although other fields change.
        path = "/api/v1/setup/operation?request_id=r"
        unknown = [ok({"status": "running", "error": {"code": "server_setup_reconciling"}, "n": i}) for i in range(200)]
        self.script.on("GET", path, [ok({"status": "running", "n": -1})] + unknown)

        def bounded(value: dict) -> str | None:
            return (value.get("error") or {}).get("code")

        done, last, reads = self.client.poll(lambda view: view.get(path).json(), lambda value: False,
                                             timeout=2700, interval=5, stable_after=300,
                                             settled=lambda value: value.get("status") != "running",
                                             bounded=bounded, bound_seconds=300)
        self.assertFalse(done)
        poll = self.client.last_poll
        self.assertEqual((poll["stop"], poll["unknown_state"], poll["unknown_limit"]),
                         ("unknown-limit", "server_setup_reconciling", 300))
        self.assertEqual(poll["unknown_seconds"], 300)
        self.assertEqual(poll["unknown_reads"], 61)
        self.assertEqual(self.now, 305, "the span starts at the first unknown read (t=5)")

    def test_poll_unknown_span_restarts_when_the_state_leaves_it(self) -> None:
        path = "/api/v1/setup/operation?request_id=r"
        unknown = ok({"status": "running", "error": {"code": "server_setup_reconciling"}})
        clear = ok({"status": "running"})
        failed_read = Response(502, [("Content-Type", "application/json")], b'{"error":"bad gateway"}')
        # 40 unknown reads (200 s), one clear read, then unknown again with a failed read inside.
        self.script.on("GET", path, [unknown] * 40 + [clear] + [unknown] * 10 + [failed_read] + [unknown] * 200)

        def read(view):  # noqa: ANN001, ANN202
            response = view.get(path)
            return response.json() if response.status == 200 else {"poll_error": f"HTTP {response.status}"}

        done, last, reads = self.client.poll(read, lambda value: False, timeout=2700, interval=5,
                                             bounded=lambda value: (value.get("error") or {}).get("code"),
                                             bound_seconds=300)
        self.assertEqual(self.client.last_poll["stop"], "unknown-limit")
        # The clear read at t=200 ends the first span; the second starts at t=205 and a failed
        # read does not reset it, so the limit is reached at t=505.
        self.assertEqual(self.now, 505)
        self.assertEqual(self.client.last_poll["unknown_reads"], 60)

    def test_lost_mutation_response_is_unknown_not_retried(self) -> None:
        self.script.on("POST", "/api/v1/setup/start", TimeoutError("read timed out"))
        with self.assertRaises(UnknownOutcome):
            self.client.post("/api/v1/setup/start", {"plan_id": "p", "request_id": "0" * 32, "confirmed": True})
        self.assertEqual(sum(1 for call in self.script.calls if call[0] == "POST"), 1)
        self.assertEqual(self.captured[-1]["outcome"], "unknown")
        self.script.on("GET", "/api/v1/dns/engine", ConnectionResetError("reset"))
        with self.assertRaises(PanelError):
            self.client.get("/api/v1/dns/engine")

    def test_rejects_foreign_paths_and_urls(self) -> None:
        with self.assertRaises(PanelError):
            self.client.get("/metrics")
        with self.assertRaises(PanelError):
            PanelClient("x", "http://127.0.0.1:1", self.script, self.redactor, self.captured.append)
        with self.assertRaises(PanelError):
            PanelClient("x", "https://127.0.0.1:1/api", self.script, self.redactor, self.captured.append)
        with self.assertRaises(PanelError):
            PinnedHTTPSTransport("127.0.0.1", 1, "ABC")

    def test_gate_refusal_body_is_recorded_verbatim(self) -> None:
        body = {"preview_token": "", "blockers": [{"code": "pdns_primary_switch_paused"}], "action": "install"}
        self.script.on("POST", "/api/v1/dns/engine/switch/preview", ok(body))
        response = self.client.post("/api/v1/dns/engine/switch/preview",
                                    {"target_engine": "pdns", "expected_source": None, "expected_revision": 3})
        self.assertEqual(response.json()["blockers"][0]["code"], "pdns_primary_switch_paused")
        self.assertEqual(self.captured[-1]["response"]["json"]["blockers"], body["blockers"])
        # preview_token is metadata named like a secret; empty values stay visible.
        self.assertEqual(self.captured[-1]["response"]["json"]["preview_token"], "")


if __name__ == "__main__":
    unittest.main()
