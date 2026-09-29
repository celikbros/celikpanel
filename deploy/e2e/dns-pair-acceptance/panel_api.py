"""Panel HTTP API client that behaves like the web UI.

* Same endpoints and bodies as ``web/src`` (see README "API sequence").
* Session: the ``celikpanel_session`` cookie from ``POST /api/v1/auth/login``.
* CSRF: the Panel has no token; unsafe methods must carry an ``Origin`` equal
  to the request host (``cmd/panel/security.go`` ``csrfProtect``), exactly
  what a browser sends.
* TLS: the fresh panel serves its self-signed certificate. The client pins
  the leaf SHA-256 that the guest itself reported over the fixture's SSH
  channel instead of disabling verification blindly.
* Capture: every exchange is redacted *before* it reaches the recorder.
* Polling: :meth:`PanelClient.poll` runs its reader against a
  :class:`ReadOnlyView`; while a poll is active any unsafe method raises
  :class:`PollMutationError`. A lost response to a mutation is recorded as an
  unknown outcome and reconciled by reads; the client never retries it.
"""

from __future__ import annotations

import contextlib
import hashlib
import http.client
import json
import socket
import ssl
import time
from dataclasses import dataclass, field
from http.cookies import SimpleCookie
from typing import Any, Callable, Iterator

from redaction import Redactor

SESSION_COOKIE = "celikpanel_session"
SAFE_METHODS = frozenset({"GET", "HEAD", "OPTIONS"})
UNSAFE_METHODS = frozenset({"POST", "PUT", "PATCH", "DELETE"})


class PanelError(RuntimeError):
    pass


class PollMutationError(PanelError):
    """An unsafe request was attempted from inside a status poll."""


class UnknownOutcome(PanelError):
    """A mutation's response was lost; its effect must be reconciled by reads."""


class PinMismatch(PanelError):
    pass


@dataclass
class Response:
    status: int
    headers: list[tuple[str, str]]
    body: bytes
    elapsed: float = 0.0

    @property
    def text(self) -> str:
        return self.body.decode("utf-8", "replace")

    def json(self) -> Any:
        if not self.body.strip():
            return None
        try:
            return json.loads(self.body)
        except ValueError:
            return None

    def header(self, name: str) -> list[str]:
        return [value for key, value in self.headers if key.lower() == name.lower()]


Transport = Callable[[str, str, dict[str, str], bytes | None, float], Response]


class PinnedHTTPSTransport:
    """``http.client`` transport that pins the served leaf certificate."""

    def __init__(self, host: str, port: int, leaf_sha256: str) -> None:
        if len(leaf_sha256) != 64 or any(ch not in "0123456789abcdef" for ch in leaf_sha256):
            raise PanelError("pinned leaf digest must be 64 lowercase hex characters")
        self.host = host
        self.port = port
        self.leaf_sha256 = leaf_sha256

    def __call__(self, method: str, path: str, headers: dict[str, str], body: bytes | None,
                 timeout: float) -> Response:
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE  # replaced by the exact leaf pin below
        connection = http.client.HTTPSConnection(self.host, self.port, timeout=timeout, context=context)
        started = time.monotonic()
        try:
            connection.connect()
            der = connection.sock.getpeercert(binary_form=True)  # type: ignore[union-attr]
            if hashlib.sha256(der or b"").hexdigest() != self.leaf_sha256:
                raise PinMismatch("panel TLS leaf differs from the guest-reported certificate")
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            payload = response.read(8 << 20)
            return Response(response.status, list(response.getheaders()), payload, time.monotonic() - started)
        finally:
            connection.close()


@dataclass
class Exchange:
    request: dict[str, Any]
    response: dict[str, Any] | None
    outcome: str  # "response" | "unknown" | "refused-by-driver"
    note: str | None = None
    extra: dict[str, Any] = field(default_factory=dict)


class ReadOnlyView:
    """The only handle a poll reader receives: GET and nothing else."""

    def __init__(self, client: "PanelClient") -> None:
        self._client = client

    def get(self, path: str, *, timeout: float = 40) -> Response:
        return self._client.request("GET", path, timeout=timeout)

    def __getattr__(self, name: str) -> Any:
        raise PollMutationError(f"poll readers may only GET (attempted {name!r})")


class PanelClient:
    def __init__(
        self,
        name: str,
        base_url: str,
        transport: Transport,
        redactor: Redactor,
        recorder: Callable[[dict[str, Any]], None],
        *,
        clock: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], None] = time.sleep,
    ) -> None:
        if not base_url.startswith("https://") or base_url.count("/") != 2:
            raise PanelError("base URL must be https://host:port without a path")
        self.name = name
        self.origin = base_url
        self.transport = transport
        self.redactor = redactor
        self.recorder = recorder
        self.clock = clock
        self.sleep = sleep
        self.cookie: str | None = None
        self._polling = 0
        self.mutations: list[dict[str, Any]] = []

    # -- core -----------------------------------------------------------------

    def _headers(self, method: str, body: bytes | None) -> dict[str, str]:
        headers = {"Accept": "application/json", "Host": self.origin.split("//", 1)[1]}
        if body is not None:
            headers["Content-Type"] = "application/json"
        if method in UNSAFE_METHODS:
            headers["Origin"] = self.origin
        if self.cookie:
            headers["Cookie"] = f"{SESSION_COOKIE}={self.cookie}"
        return headers

    def _capture_cookie(self, response: Response) -> None:
        for raw in response.header("Set-Cookie"):
            cookie = SimpleCookie()
            try:
                cookie.load(raw)
            except Exception:  # noqa: BLE001 - malformed cookies are simply not adopted
                continue
            if SESSION_COOKIE in cookie:
                value = cookie[SESSION_COOKIE].value
                if value:
                    self.redactor.register(value)
                    self.cookie = value
                else:
                    self.cookie = None

    def request(
        self,
        method: str,
        path: str,
        body: Any = None,
        *,
        timeout: float = 40,
        purpose: str | None = None,
    ) -> Response:
        method = method.upper()
        if method not in SAFE_METHODS | UNSAFE_METHODS:
            raise PanelError(f"unsupported method {method}")
        if not path.startswith("/api/v1/"):
            raise PanelError("the driver only calls /api/v1 endpoints")
        if method in UNSAFE_METHODS and self._polling:
            self._record(method, path, body, None, "refused-by-driver",
                         note="unsafe request attempted inside a status poll")
            raise PollMutationError(f"{method} {path} attempted inside a status poll")
        payload = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        headers = self._headers(method, payload)
        try:
            response = self.transport(method, path, headers, payload, timeout)
        except (OSError, socket.timeout, http.client.HTTPException) as exc:
            self._record(method, path, body, None, "unknown", note=f"{type(exc).__name__}: {exc}",
                         purpose=purpose, headers=headers)
            if method in UNSAFE_METHODS:
                raise UnknownOutcome(f"{method} {path}: response lost ({type(exc).__name__}); reconcile by reads") from exc
            raise PanelError(f"GET {path} failed: {exc}") from exc
        self._capture_cookie(response)
        self._record(method, path, body, response, "response", purpose=purpose, headers=headers)
        if method in UNSAFE_METHODS:
            self.mutations.append({"method": method, "path": path, "status": response.status, "purpose": purpose})
        return response

    def _record(self, method: str, path: str, body: Any, response: Response | None, outcome: str, *,
                note: str | None = None, purpose: str | None = None,
                headers: dict[str, str] | None = None) -> None:
        exchange: dict[str, Any] = {
            "panel": self.name,
            "outcome": outcome,
            "purpose": purpose,
            "request": {
                "method": method,
                "path": self.redactor.text(path),
                "headers": self.redactor.headers(headers or {}),
                "body": self.redactor.value(body),
            },
            "response": None,
        }
        if note:
            exchange["note"] = self.redactor.text(note)
        if response is not None:
            parsed = response.json()
            exchange["response"] = {
                "status": response.status,
                "elapsed_seconds": round(response.elapsed, 3),
                "headers": self.redactor.headers(response.headers),
                "json": self.redactor.value(parsed) if parsed is not None else None,
                "text": None if parsed is not None else self.redactor.text(response.text[:4096]),
            }
        self.recorder(exchange)

    # -- convenience ----------------------------------------------------------

    def get(self, path: str, **kwargs: Any) -> Response:
        return self.request("GET", path, **kwargs)

    def post(self, path: str, body: Any = None, **kwargs: Any) -> Response:
        return self.request("POST", path, body, **kwargs)

    def put(self, path: str, body: Any = None, **kwargs: Any) -> Response:
        return self.request("PUT", path, body, **kwargs)

    def delete(self, path: str, **kwargs: Any) -> Response:
        return self.request("DELETE", path, **kwargs)

    @contextlib.contextmanager
    def polling(self) -> Iterator[ReadOnlyView]:
        self._polling += 1
        try:
            yield ReadOnlyView(self)
        finally:
            self._polling -= 1

    def poll(
        self,
        reader: Callable[[ReadOnlyView], Any],
        done: Callable[[Any], bool],
        *,
        timeout: float,
        interval: float = 3.0,
        on_change: Callable[[Any], None] | None = None,
        key: Callable[[Any], Any] = lambda value: json.dumps(value, sort_keys=True, default=str),
    ) -> tuple[bool, Any, int]:
        """Read until ``done``; returns (done, last value, number of reads).

        The reader only ever sees a :class:`ReadOnlyView`, and the client
        refuses unsafe methods while this runs, so a poll cannot start a
        mutation even indirectly.
        """

        deadline = self.clock() + timeout
        last: Any = None
        previous: Any = object()
        reads = 0
        with self.polling() as view:
            while True:
                try:
                    last = reader(view)
                except PanelError as exc:
                    if isinstance(exc, PollMutationError):
                        raise
                    last = {"poll_error": str(exc)}
                reads += 1
                marker = key(last)
                if on_change is not None and marker != previous:
                    on_change(last)
                previous = marker
                if not (isinstance(last, dict) and "poll_error" in last) and done(last):
                    return True, last, reads
                if self.clock() >= deadline:
                    return False, last, reads
                self.sleep(interval)

    # -- product operations (UI-equivalent) ----------------------------------

    def login(self, username: str, password: str) -> dict[str, Any]:
        self.redactor.register(password)
        response = self.post("/api/v1/auth/login", {"username": username, "password": password},
                             purpose="login (Login.tsx)")
        body = response.json()
        if response.status != 200 or not isinstance(body, dict):
            raise PanelError(f"login failed with HTTP {response.status}")
        if body.get("totp_required"):
            raise PanelError("fresh administrator unexpectedly requires TOTP")
        if not self.cookie:
            raise PanelError("login did not set the session cookie")
        me = self.get("/api/v1/auth/me", purpose="session identity (usePanelSession.ts)")
        identity = me.json()
        if me.status != 200 or not isinstance(identity, dict) or identity.get("role") != "admin":
            raise PanelError("authenticated identity is not an administrator")
        return identity
