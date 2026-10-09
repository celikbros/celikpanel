#!/usr/bin/env python3
"""set2 cell kind ``request-identity``: the eight guarded routes of D-029, measured with real services.

One cell on one fresh disposable QEMU guest (lab.py). The driver installs the candidate and runs the owner's server
setup exactly as the ``settings-writes`` cell does (``settings_writes_trial.SettingsTrial`` steps preflight, origin,
baseline-install, owner-login, license, setup, site) and then, as the logged-in owner, sends each guarded route

  (i)   the same request (same identity, same body) three times in a row,
  (ii)  three times at once,
  (iii) without the header ``X-CelikPanel-Request-Id``,
  (iv)  with the same identity and a different body,

and reads the native effect after each: backup archives and their manifests, the document root's checksum and the
site database's rows, the databases and accounts on MariaDB and PostgreSQL beside the Panel's rows, a login with the
password the Panel stores, WireGuard's peers, the imported site. For the long routes it also (v) drops the
connection while the request runs and (vi) restarts the Panel's service while a restore runs, then reads what the
Agent left. At the end it reads the ``request_identities`` rows (vii): their states and whether any stored answer
holds a secret this run was shown.

The driver calls only the Panel's HTTP API; every native fact is read over SSH by
``guest_request_identity_native.py``, and what the owner does on the server by hand goes through that helper's
``owner-*`` modes and is recorded as such. The isolated lab has no certificate authority: for the Let's Encrypt route
only the guard is measured. No update is started. Every result carries ``native_evidence: false``.

set3 (2026-10-12), the cells ``rid3-*``: the expectations follow the corrections of 2026-10-11. The import preview and
every stored answer hold no hash-shaped value and an imported mailbox authenticates with its original password (S1);
an archive with the directory member ``homedir/public_html/`` imports completely, a failed files step answers ``200``
``partial`` with truthful lists, hostile members are refused (P4); a PHP site is created, served by PHP-FPM and
deleted (P5); a certbot run that cannot reach the authority answers ``502 CERTIFICATE_ISSUE_FAILED`` (O9); a sent
database password is not echoed and its answer is replayed (O10); the MariaDB version shown is the server's (O14).

  request_identity_trial.py plan --cell rid-debian13 --artifacts A.json --work-root /var/tmp/cp-release-drill-X [--dry-run]
  request_identity_trial.py run  --cell rid-debian13 --artifacts A.json --work-root /var/tmp/cp-release-drill-X --execute
"""
from __future__ import annotations

import argparse
import base64
import dataclasses
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import socket
import ssl
import struct
import sys
import threading
import time
from typing import Any, Callable

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import settings_writes_trial as sw  # noqa: E402

base = sw.base
CELL_KIND = "request-identity"
HELPER = "guest_request_identity_native.py"
HEADER = "X-CelikPanel-Request-Id"
REPLAYED = "X-CelikPanel-Request-Replayed"
SITE_MEGABYTES = 192          # data that does not compress: a backup and a restore last several seconds
SITE_ROWS = 20000
IMPORT_ROWS = 300
IMPORT_DROP_SLEEP = 25        # seconds of ``DO SLEEP`` at the end of the dropped import's dump
IMPORT_SUFFIX = ""            # appended to the fixture names (a harness debugging session on a used guest sets it)
GUARD_CODES = ("REQUEST_ID_REQUIRED", "REQUEST_ID_REUSED", "REQUEST_IN_PROGRESS", "REQUEST_OUTCOME_UNKNOWN",
               "REQUEST_COMPLETED_RESULT_NOT_RETAINED")
ROUTES = (
    ("domain-database-mariadb", "POST /api/v1/domains/{id}/databases (MariaDB)"),
    ("domain-database-postgresql", "POST /api/v1/domains/{id}/databases (PostgreSQL)"),
    ("admin-account-mariadb", "POST /api/v1/database-servers/{id}/admin-account (MariaDB)"),
    ("admin-account-postgresql", "POST /api/v1/database-servers/{id}/admin-account (PostgreSQL)"),
    ("server-database-mariadb", "POST /api/v1/database-servers/{id}/databases (MariaDB)"),
    ("server-database-postgresql", "POST /api/v1/database-servers/{id}/databases (PostgreSQL)"),
    ("backup", "POST /api/v1/domains/{id}/backups"),
    ("letsencrypt", "POST /api/v1/domains/{id}/ssl/letsencrypt (reissue)"),
    ("vpn-peer", "POST /api/v1/vpn/peers"),
    ("import", "POST /api/v1/import/cpanel/apply"),
    ("restore", "POST /api/v1/domains/{id}/backups/restore"),
)
SECTIONS = (
    ("C0-prepare", "the owner's site, the engines and the lab's isolation"),
    ("C1-domain-databases", "a domain's database (MariaDB and PostgreSQL)"),
    ("C2-admin-account", "the Panel's own account on each database engine"),
    ("C3-server-databases", "a database on a database server (MariaDB and PostgreSQL)"),
    ("C4-backup", "manual backup"),
    ("C5-letsencrypt", "Let's Encrypt reissue: the guard only"),
    ("C6-vpn-peer", "VPN peer"),
    ("C6b-php-site", "a PHP site: created, served by PHP-FPM through the web server, deleted (set3, P5)"),
    ("C7-import", "cPanel import"),
    ("C7b-import-answers", "the imported mailbox's password, a failed files step, hostile archive members (set3, S1 and P4)"),
    ("C8-restore", "domain restore, with a dropped connection and a Panel restart"),
    ("C9-identities", "the request_identities rows"),
)
CELLS = {
    "rid-debian13": sw.SettingsCell("rid-debian13", "debian13", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "rid-ubuntu": sw.SettingsCell("rid-ubuntu", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "rid-arch": sw.SettingsCell("rid-arch", "arch", "web", sw.WEB_PRESET + ("postgresql",), False),
    # set3: the same guests and profiles, measured after the corrections of 2026-10-11.
    "rid3-debian13": sw.SettingsCell("rid3-debian13", "debian13", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "rid3-ubuntu": sw.SettingsCell("rid3-ubuntu", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "rid3-arch": sw.SettingsCell("rid3-arch", "arch", "web", sw.WEB_PRESET + ("postgresql",), False),
}
OWNER_SNIPPET_ENV = "SET3_OWNER_NGINX_PHP_SNIPPET"   # "1": a second reading with one recorded owner action (see C6b)
PHP_SITE_DOMAIN = "set3-php.test"
PHP_PROBE_PATH = "/set3-probe.php"
PHP_PROBE_MARK = "set3-php-executed:42:"
HOSTILE_KINDS = ("dotdot", "absolute", "symlink")
MAILBOX_KEYS = ["domain", "has_password", "quota_mb", "user"]     # cmd/panel/import_handlers.go importPreviewMailbox


def version_number(text: Any) -> str | None:
    """The dotted number a version string starts its first number with (10.11.14 of 10.11.14-MariaDB-0ubuntu0...)."""
    found = re.search(r"\d+\.\d+(?:\.\d+)?", str(text or ""))
    return found.group(0) if found else None


def partial_lists(steps: list) -> tuple:
    """The `imported` and `not_imported` lists an import answer must carry for its steps (finalize is not a part)."""
    parts = [s for s in steps or [] if isinstance(s, dict) and s.get("step") != "finalize"]
    return [s.get("step") for s in parts if s.get("ok")], [s.get("step") for s in parts if not s.get("ok")]


# ---------------------------------------------------------------------------
# Pure rules (covered offline by test_request_identity_trial.py)
# ---------------------------------------------------------------------------

def new_identity() -> str:
    """The product's operation-id format: 32 lowercase hexadecimal characters."""
    return secrets.token_hex(16)


def answer_code(entry: dict) -> str | None:
    parsed = entry.get("_parsed")
    return parsed.get("code") if isinstance(parsed, dict) and isinstance(parsed.get("code"), str) else None


def is_guard_refusal(entry: dict) -> bool:
    return answer_code(entry) in GUARD_CODES


def brief(entry: dict | None) -> dict | None:
    if entry is None:
        return None
    parsed = entry.get("_parsed") if isinstance(entry.get("_parsed"), dict) else {}
    return {"status": entry.get("status"), "code": parsed.get("code"), "reason": parsed.get("reason"),
            "replayed": entry.get("replayed"), "seconds": entry.get("seconds"), "outcome": entry.get("outcome"),
            "error": entry.get("error")}


def judge_sequential(entries: list, digests: list, retained: bool) -> tuple:
    """Three arrivals in a row of one identity. Retained: the three answers are the same bytes and the later two are
    marked as replays. Not retained (a one-time secret): the first is the handler's own answer and the later two are
    the status-only refusal."""
    first = entries[0]
    if first.get("status") is None or any(e.get("status") is None for e in entries):
        return None, "an arrival got no answer"
    if retained:
        same = all(e["status"] == first["status"] for e in entries) and len(set(digests)) == 1
        marked = all(e.get("replayed") == "1" for e in entries[1:]) and first.get("replayed") is None
        return same and marked, {"statuses": [e["status"] for e in entries], "same_bytes": len(set(digests)) == 1,
                                 "replays_marked": marked}
    reason = None if 200 <= first["status"] < 300 else "failed"
    later = all(e["status"] == 409 and answer_code(e) == "REQUEST_COMPLETED_RESULT_NOT_RETAINED"
                and ((e.get("_parsed") or {}).get("reason") or None) == reason for e in entries[1:])
    return (not is_guard_refusal(first)) and later, {"first": brief(first), "later": [brief(e) for e in entries[1:]]}


def judge_concurrent(entries: list, digests: list, retained: bool) -> tuple:
    """Three arrivals at once of one identity. ``REQUEST_IN_PROGRESS`` is always an allowed answer for a waiter.
    Retained: every other answer is the same bytes. Not retained: exactly one arrival gets the handler's own answer."""
    if any(e.get("status") is None for e in entries):
        return None, "an arrival got no answer"
    waiting = [e for e in entries if answer_code(e) == "REQUEST_IN_PROGRESS"]
    others = [(e, d) for e, d in zip(entries, digests) if answer_code(e) != "REQUEST_IN_PROGRESS"]
    detail = {"answers": [brief(e) for e in entries], "in_progress": len(waiting)}
    if not others:
        return None, detail
    if retained:
        same = len({d for _, d in others}) == 1 and len({e["status"] for e, _ in others}) == 1
        return same and not any(is_guard_refusal(e) for e, _ in others), detail
    own = [e for e, _ in others if not is_guard_refusal(e)]
    rest = [e for e, _ in others if is_guard_refusal(e)]
    return len(own) == 1 and all(answer_code(e) == "REQUEST_COMPLETED_RESULT_NOT_RETAINED" for e in rest), detail


def one_new(before: list, after: list) -> list:
    return sorted(set(after) - set(before))


def restore_classification(reference: dict, drifted: dict, now: dict) -> str:
    """What a restore left, from the document root's digest and the row count against the two known states."""
    files_back, rows_back = now["docroot"] == reference["docroot"], now["rows"] == reference["rows"]
    files_same, rows_same = now["docroot"] == drifted["docroot"], now["rows"] == drifted["rows"]
    if files_back and rows_back:
        return "finished: the document root and the rows are those of the backup"
    if files_same and rows_same:
        return "not started or rolled back: the document root and the rows are as they were before the restore"
    return ("half: files " + ("restored" if files_back else "as before" if files_same else "neither state") + ", rows "
            + ("restored" if rows_back else "as before" if rows_same else "neither state"))


def matrix_value(ok: bool | None) -> str:
    return "FAIL" if ok is False else "not established" if ok is None else "pass"


def worse(previous: str | None, value: str) -> str:
    rank = {"FAIL": 3, "not established": 2, "pass": 1}
    return value if previous is None or rank[value] > rank[previous] else previous


def build_plan(settings: sw.SettingsCell, artifacts: dict, work_root: str, local_port: int) -> dict:
    return {"schema": "celikpanel/set2-request-identity-plan/v1", "cell_kind": CELL_KIND, "native_evidence": False,
            "cell": dataclasses.asdict(settings), "work_root": work_root, "local_port": local_port,
            "candidate": {k: artifacts["baseline"][k] for k in ("version", "commit", "sha256")},
            "setup": {"purpose": settings.purpose, "dns_mode": "external",
                      "customization": {"components": sorted(settings.components)}},
            "site": {"domain": sw.SITE_DOMAIN, "megabytes": SITE_MEGABYTES, "rows": SITE_ROWS},
            "steps": ["preflight", "origin", "baseline-install", "owner-login", "license", "setup", "site"]
                     + [key for key, _ in SECTIONS] + ["collect"],
            "routes": [{"id": key, "route": route} for key, route in ROUTES],
            "arrivals": {"i": "the same request three times in a row", "ii": "three times at once",
                         "iii": "without the header", "iv": "the same identity with a different body",
                         "v": "the connection dropped while the request runs (restore, import)",
                         "vi": "the Panel goes away while a restore runs: the owner's systemctl restart, and the process killed",
                         "vii": "the request_identities rows afterwards"},
            "rule": "the driver calls only the Panel's HTTP API as the logged-in owner; every native fact is a read-only "
                    "SSH inspection; owner actions on the guest are recorded as such; no certificate authority is "
                    "contacted or imitated; no update is started"}


# ---------------------------------------------------------------------------
# Native execution
# ---------------------------------------------------------------------------

class RequestIdentityTrial(sw.SettingsTrial):
    def __init__(self, settings: sw.SettingsCell, artifacts: dict, work_root: str, local_port: int) -> None:
        super().__init__(settings, artifacts, work_root, local_port)
        self.matrix: dict[str, dict] = {}
        self.needles: set[str] = set()
        self.raw_digests: dict[int, str] = {}
        self.sequence = 0
        self.identities: dict[str, str] = {}      # identity -> what it was used for
        self.times: dict[str, dict] = {}
        self.answer_shapes: list[dict] = []       # set3: per guarded answer, hash-shaped values and echoed sent secrets
        self.import_password: str | None = None   # set3: the imported mailboxes' original password (never recorded)

    # -- helpers ------------------------------------------------------------------------------

    def upload_helpers(self) -> dict:
        helpers = super().upload_helpers()
        helpers[HELPER] = self.lab.put_file(self.root, self.record, self.plan, self.node_name, HERE / HELPER, HELPER)[1]
        return helpers

    def native2(self, mode: str, **arguments: Any) -> dict:
        payload = base64.b64encode(json.dumps(arguments).encode()).decode()
        return self.helper(HELPER, mode, "--args-b64", payload, timeout=600)

    def snap2(self, label: str, mode: str, **arguments: Any) -> dict:
        value = self.native2(mode, **arguments)
        self.native_sequence += 1
        name = f"native/{self.native_sequence:03d}-{label}.json"
        self.record_json(name, value)
        self.current.setdefault("natives", []).append({"label": label, "mode": mode, "file": name,
                                                       "owner_action": mode.startswith("owner-"),
                                                       "lab_action": mode.startswith("lab-")})
        return value

    def owner2(self, label: str, mode: str, **arguments: Any) -> dict:
        value = self.snap2("owner-" + label, mode, **arguments)
        self.current.setdefault("owner_actions", []).append({"label": label, "mode": mode, "at": value.get("at")})
        return value

    def secret(self, text: Any) -> None:
        if isinstance(text, str) and len(text) >= 6:
            self.redactor.register(text)
            self.needles.add(text)

    def scrub(self, value: Any) -> Any:
        """Evidence form of a request or an answer: every secret value is learned (so that it is redacted wherever
        it appears later and can be searched for in the stored rows) and removed."""
        secret_key = self.p["redaction"].secret_key

        def learn(item: Any) -> None:
            if isinstance(item, dict):
                for key, inner in item.items():
                    if key.lower() == "client_config" and isinstance(inner, str):
                        for match in re.finditer(r"(?m)^(PrivateKey|PresharedKey)\s*=\s*(\S+)", inner):
                            self.secret(match.group(2))
                    elif isinstance(inner, str) and secret_key(key):
                        self.secret(inner)
                    else:
                        learn(inner)
            elif isinstance(item, list):
                for inner in item:
                    learn(inner)

        def strip(item: Any) -> Any:
            if isinstance(item, dict):
                return {key: ("[REDACTED client configuration]" if key.lower() == "client_config" else strip(inner))
                        for key, inner in item.items()}
            if isinstance(item, list):
                return [strip(inner) for inner in item]
            return item
        learn(value)
        return self.redactor.value(strip(value))

    def carries_secret(self, value: Any) -> bool:
        secret_key = self.p["redaction"].secret_key
        if isinstance(value, dict):
            return any(key.lower() == "client_config" or (secret_key(key) and isinstance(inner, str) and inner)
                       or self.carries_secret(inner) for key, inner in value.items())
        if isinstance(value, list):
            return any(self.carries_secret(inner) for inner in value)
        return False

    # -- the owner's browser, with the identity under the driver's control -------------------------

    def _headers(self, payload: bytes | None, identity: str | None) -> dict:
        headers = self.panel_client()._headers("POST", payload)
        if identity:
            headers[HEADER] = identity
        return headers

    def _entry(self, label: str, path: str, body: Any, identity: str | None, result: tuple) -> dict:
        kind, value, started = result
        self.sequence += 1
        entry = {"n": self.sequence, "label": label, "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(started)),
                 "request": {"method": "POST", "path": path, "body": self.scrub(body), "identity": identity,
                             "header_sent": bool(identity)}}
        if kind != "answer":
            entry.update(outcome="no-answer", status=None, error=self.redactor.text(f"{type(value).__name__}: {value}")[:300],
                         seconds=round(time.time() - started, 3), _parsed=None)
            self.current.setdefault("calls", []).append(entry)
            return entry
        parsed = value.json()
        headers = {k: v for k, v in value.headers if k.lower() in (HEADER.lower(), REPLAYED.lower(), "content-type",
                                                                   "cache-control")}
        digest = hashlib.sha256(value.body).hexdigest()
        self.raw_digests[entry["n"]] = digest
        # set3: counted on the raw bytes before anything is redacted; only the counts are kept.
        secret_key = self.p["redaction"].secret_key
        sent = [inner for key, inner in body.items() if isinstance(inner, str) and inner and secret_key(key)] \
            if isinstance(body, dict) else []
        entry["hash_shaped_values_in_the_raw_answer"] = base.hash_shaped_count(value.body)
        entry["sent_secret_values_in_the_raw_answer"] = sum(1 for item in sent if item.encode() in value.body)
        self.answer_shapes.append({"n": entry["n"], "label": label, "path": path,
                                   "hash_shaped": entry["hash_shaped_values_in_the_raw_answer"],
                                   "sent_secret_echoed": entry["sent_secret_values_in_the_raw_answer"]})
        entry.update(outcome="answer", status=value.status, seconds=round(value.elapsed, 3), headers=headers,
                     replayed=next((v for k, v in value.headers if k.lower() == REPLAYED.lower()), None),
                     json=self.scrub(parsed), text=None if parsed is not None else self.redactor.text(value.text[:600]),
                     body_bytes=len(value.body),
                     body_sha256="(not recorded: the answer carries a one-time secret)" if self.carries_secret(parsed) else digest)
        if isinstance(parsed, dict) and ("error" in parsed or "code" in parsed):
            entry["answer"] = {k: parsed.get(k) for k in ("code", "reason", "error", "vars", "detail", "action") if parsed.get(k) is not None}
            keys = ([f"err.{parsed.get('code')}.{parsed.get('reason')}"] if parsed.get("reason") else []) + [f"err.{parsed.get('code')}"]
            english = self.translator.catalog["en"]
            texts = {key: {language: self.translator.text(key, dict(parsed.get("vars") or {}), language=language)
                           for language in ("en", "tr")} for key in keys if key in english}
            if texts:
                entry["catalogue_texts"] = texts
        entry["_parsed"] = parsed
        self.current.setdefault("calls", []).append(entry)
        return entry

    def post(self, label: str, path: str, body: Any, identity: str | None, timeout: float = 600) -> dict:
        payload = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        headers = self._headers(payload, identity)
        started = time.time()
        try:
            result = ("answer", self.transport("POST", path, headers, payload, timeout), started)
        except Exception as exc:  # noqa: BLE001 - a lost answer is an observation here
            result = ("error", exc, started)
        return self._entry(label, path, body, identity, result)

    def post_many(self, label: str, path: str, body: Any, identities: list, timeout: float = 600) -> list:
        """The same request on separate connections at the same moment."""
        payload = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        prepared = [self._headers(payload, identity) for identity in identities]
        results: list = [None] * len(identities)
        barrier = threading.Barrier(len(identities))

        def work(index: int) -> None:
            barrier.wait()
            started = time.time()
            try:
                results[index] = ("answer", self.transport("POST", path, prepared[index], payload, timeout), started)
            except Exception as exc:  # noqa: BLE001
                results[index] = ("error", exc, started)
        threads = [threading.Thread(target=work, args=(index,), daemon=True) for index in range(len(identities))]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join(timeout + 60)
        return [self._entry(f"{label} {index + 1} of {len(identities)}", path, body, identities[index],
                            results[index] or ("error", TimeoutError("the arrival did not return"), time.time()))
                for index in range(len(identities))]

    def post_and_drop(self, label: str, path: str, body: Any, identity: str, hold: Callable[[], dict]) -> dict:
        """Send the request, wait until ``hold`` says the server is at work, then drop the connection without
        reading an answer (linger 0: the socket is reset, not closed in order)."""
        payload = json.dumps(body, separators=(",", ":")).encode()
        headers = self._headers(payload, identity)
        context = ssl.create_default_context()
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE   # replaced by the exact leaf pin below
        plain = socket.create_connection(("127.0.0.1", self.local_port), timeout=20)
        tls = context.wrap_socket(plain)
        leaf = hashlib.sha256(tls.getpeercert(binary_form=True) or b"").hexdigest()
        if leaf != self.transport.leaf_sha256:
            tls.close()
            raise base.StepInconclusive("panel TLS leaf differs from the guest-reported certificate")
        lines = [f"POST {path} HTTP/1.1"] + [f"{k}: {v}" for k, v in headers.items()] + [f"Content-Length: {len(payload)}"]
        started = time.time()
        tls.sendall(("\r\n".join(lines) + "\r\n\r\n").encode() + payload)
        held = hold()
        early = b""
        tls.settimeout(0.3)
        try:
            early = tls.recv(4096)
        except (socket.timeout, TimeoutError, ssl.SSLWantReadError, ssl.SSLError):
            early = b""
        tls.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
        tls.close()
        self.sequence += 1
        entry = {"n": self.sequence, "label": label, "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(started)),
                 "request": {"method": "POST", "path": path, "body": self.scrub(body), "identity": identity, "header_sent": True},
                 "outcome": "connection-dropped-by-the-client", "status": None, "held": held,
                 "dropped_after_seconds": round(time.time() - started, 3),
                 "answer_bytes_received_before_the_drop": len(early), "_parsed": None}
        self.current.setdefault("calls", []).append(entry)
        return entry

    def judge(self, route: str, roman: str, text: str, ok: bool | None, detail: Any = None) -> bool | None:
        self.check(f"{route} ({roman}): {text}", ok, detail)
        cell = self.matrix.setdefault(route, {})
        cell[roman] = worse(cell.get(roman), matrix_value(ok))
        return ok

    def not_reached(self, route: str, romans: tuple, why: str, detail: Any = None) -> None:
        for roman in romans:
            self.judge(route, roman, "reached", None, {"not reached": why, "detail": detail})

    def _own_identity(self, used_for: str) -> str:
        identity = new_identity()
        self.identities[identity] = used_for
        return identity

    def digests(self, entries: list) -> list:
        return [self.raw_digests.get(entry["n"]) for entry in entries]

    def row(self, identity: str) -> dict | None:
        rows = self.native2("read-identities", ids=[identity]).get("rows") or []
        return rows[0] if rows else None

    def wait_row(self, identity: str, label: str, limit: float = 900.0) -> dict:
        """Read-only wait until the identity's row is no longer ``running``."""
        started, row = time.time(), None
        while time.time() - started < limit:
            row = self.row(identity)
            if row and row["status"] != "running":
                break
            time.sleep(2)
        record = {"identity": identity, "row": row, "waited_seconds": round(time.time() - started, 1)}
        self.record_json(f"native/row-{label}.json", record)
        return record

    # -- the generic arrivals (i)-(iv) --------------------------------------------------------------

    def suite(self, route: str, path: str, bodies: dict, effect: Callable[[str], dict],
              one_effect: Callable[[dict, dict, dict | None, str], tuple], *, retained: bool = True,
              timeout: float = 600, on_answer: Callable[[dict], None] | None = None) -> dict:
        started = base.utc_now()
        e0 = effect(route + "-before")
        a3 = self.post(f"{route} (iii) without the header", path, bodies["i"], None, timeout)
        e3 = effect(route + "-after-iii")
        self.judge(route, "iii", "without the header the answer is 428 REQUEST_ID_REQUIRED",
                   a3["status"] == 428 and answer_code(a3) == "REQUEST_ID_REQUIRED", brief(a3))
        self.judge(route, "iii", "and nothing changed on the server", e3["fp"] == e0["fp"], {"before": e0["fp"], "after": e3["fp"]})

        identity = new_identity()
        self.identities[identity] = route + " (i)"
        sequential = []
        for number in (1, 2, 3):
            answer = self.post(f"{route} (i) sequential arrival {number} of 3", path, bodies["i"], identity, timeout)
            sequential.append(answer)
            if number == 1 and on_answer is not None:
                on_answer(answer)
        e1 = effect(route + "-after-i")
        ok, detail = one_effect(e3, e1, sequential[0], identity)
        self.judge(route, "i", "three arrivals in a row of one identity left one native effect", ok, detail)
        ok, detail = judge_sequential(sequential, self.digests(sequential), retained)
        self.judge(route, "i", "the three answers are " + ("the same bytes, the later two marked as replays" if retained else
                   "the handler's own answer once, then the status-only refusal"), ok, detail)

        a4 = self.post(f"{route} (iv) the same identity with a different body", path, bodies["other"], identity, timeout)
        e4 = effect(route + "-after-iv")
        self.judge(route, "iv", "the same identity with a different body is refused 409 REQUEST_ID_REUSED",
                   a4["status"] == 409 and answer_code(a4) == "REQUEST_ID_REUSED", brief(a4))
        self.judge(route, "iv", "and there is no second effect", e4["fp"] == e1["fp"], {"before": e1["fp"], "after": e4["fp"]})

        second = new_identity()
        self.identities[second] = route + " (ii)"
        concurrent = self.post_many(f"{route} (ii) concurrent arrival", path, bodies["ii"], [second] * 3, timeout)
        own = next((a for a in concurrent if a.get("status") and not is_guard_refusal(a)), None)
        if own is not None and on_answer is not None:
            on_answer(own)
        e2 = effect(route + "-after-ii")
        ok, detail = one_effect(e4, e2, own, second)
        self.judge(route, "ii", "three arrivals at once of one identity left one native effect", ok, detail)
        ok, detail = judge_concurrent(concurrent, self.digests(concurrent), retained)
        self.judge(route, "ii", "the answers are " + ("the same bytes, or REQUEST_IN_PROGRESS for a waiter" if retained else
                   "the handler's own answer for exactly one arrival; the others the status-only refusal or REQUEST_IN_PROGRESS"),
                   ok, detail)
        self.times[route] = {"started_at": started, "finished_at": base.utc_now()}
        return {"identity_i": identity, "identity_ii": second, "sequential": sequential, "concurrent": concurrent,
                "own_concurrent": own, "effects": {"before": e0, "iii": e3, "i": e1, "iv": e4, "ii": e2}}

    # -- C0 ------------------------------------------------------------------------------------------

    def panel_state(self, label: str) -> dict:
        return self.snap2(label, "read-panel-state")["state"]

    def c0_prepare(self) -> None:
        domain_id = self.state["domain_id"]
        state = self.panel_state("panel-state-before")
        mine = next((d for d in state["domains"] if d.get("id") == domain_id), None)
        self.check("the Panel's database names the site's domain and its subscription", bool(mine), mine)
        self.state["subscription_id"] = (mine or {}).get("subscription_id")
        servers = self.call("C0 database servers (the Databases page)", "GET", "/api/v1/database-servers")
        listed = servers["_parsed"] if isinstance(servers["_parsed"], list) else []
        by_type = {}
        for server in listed:
            by_type.setdefault(str(server.get("type_name")).lower(), server.get("id"))   # H28: "MariaDB", "PostgreSQL"
        self.state["database_servers"] = by_type
        self.current["database_servers"] = [{k: s.get(k) for k in ("id", "type_name", "name", "version", "host", "port", "status")}
                                            for s in listed]
        self.check("the Databases page lists a MariaDB and a PostgreSQL server", {"mariadb", "postgresql"} <= set(by_type), by_type)
        self.engine_versions("C0, as the engines were registered", listed)
        isolated = self.snap2("lab-isolate-acme", "lab-isolate-acme", action="apply")
        self.current["acme_isolation"] = {"resolves": isolated.get("resolves"),
                                          "what": "lab isolation: the two Let's Encrypt directory names resolve to this "
                                                  "guest's own loopback, where nothing answers for them"}
        self.current["acme_isolation"].update(confirmed=isolated.get("confirmed"), after_seconds=isolated.get("confirmed_after_seconds"))
        self.check("lab isolation: the Let's Encrypt directory names resolve to this guest's loopback only",
                   isolated.get("confirmed") is True, isolated.get("resolves"))
        seeded = self.owner2("seed-site", "owner-seed-site", domain_id=domain_id, megabytes=SITE_MEGABYTES)
        self.check("the owner's site is in the document root", seeded["docroot"].get("files", 0) >= 6
                   and seeded["docroot"].get("bytes", 0) >= SITE_MEGABYTES * 1024 * 1024, seeded["docroot"])
        identities = self.snap2("identities-before", "read-identities")
        self.check("request_identities exists and is empty before the first guarded request",
                   identities.get("count") == 0 and "request_sha256" in (identities.get("columns") or []), identities.get("states"))
        self.current["units_before"] = self.snap2("units-before", "read-units")["units"]
        self.state["identity_log_since"] = int(self.native2("read-clock")["epoch"])

    def engine_versions(self, when: str, listed: list | None = None) -> None:
        """set3 (O14): the version the Databases page shows for each engine beside what the engine itself reports."""
        if listed is None:
            servers = self.call(f"database servers (the Databases page): {when}", "GET", "/api/v1/database-servers")
            listed = servers["_parsed"] if isinstance(servers["_parsed"], list) else []
        tag = re.sub(r"[^a-z0-9]+", "-", when.lower()).strip("-")[:40]
        native = self.snap2("engine-versions-" + tag, "read-engine-versions")
        shown = {str(s.get("type_name")).lower(): s.get("version") for s in listed}
        maria, postgres = native["mariadb"], native["postgresql"]
        record = {"when": when, "shown": shown, "mariadb_select_version": maria["select_version"],
                  "mariadbd_version_line": maria["server_program_version"], "mariadb_client_version_line": maria["client_program_version"],
                  "postgresql_show_server_version": postgres["show_server_version"], "os": native.get("os_release"),
                  "mariadb_shown_is_exactly_select_version": shown.get("mariadb") == maria["select_version"]}
        self.current.setdefault("engine_versions", []).append(record)
        self.check(f"O14 ({when}): the MariaDB version the Databases page shows is the server's own",
                   bool(version_number(maria["select_version"])) and "VERSION" not in str(shown.get("mariadb"))
                   and version_number(shown.get("mariadb")) == version_number(maria["select_version"]), record)
        self.check(f"O14 ({when}): the PostgreSQL version shown is the server's own",
                   bool(version_number(postgres["show_server_version"]))
                   and version_number(shown.get("postgresql")) == version_number(postgres["show_server_version"]), record)

    # -- C1, C3: databases ------------------------------------------------------------------------------

    def engines(self, label: str, tables: tuple = ()) -> dict:
        return self.snap2(label, "read-engines", mariadb_tables_in=list(tables))

    def database_effect(self, engine: str, where: Callable[[dict], bool]) -> Callable[[str], dict]:
        def effect(label: str) -> dict:
            native = self.engines(label)
            # H29: one row is a database on one server (a domain's MariaDB and PostgreSQL databases share a name)
            rows = [f"{r['server_id']}:{r['name']}" for r in self.panel_state(label + "-rows")["databases_v2"] if where(r)]
            names = native[engine]["databases"]
            return {"fp": {"on_engine": names, "panel_rows": sorted(rows)}, "accounts": native[engine].get("users") or native[engine].get("roles")}
        return effect

    @staticmethod
    def one_database(before: dict, after: dict, answer: dict | None, identity: str) -> tuple:
        new_engine = one_new(before["fp"]["on_engine"], after["fp"]["on_engine"])
        new_rows = [row.split(":", 1)[1] for row in one_new(before["fp"]["panel_rows"], after["fp"]["panel_rows"])]
        named = (answer or {}).get("_parsed", {}).get("name") if answer and isinstance(answer.get("_parsed"), dict) else None
        detail = {"new_on_engine": new_engine, "new_panel_rows": new_rows, "answer_names": named,
                  "answer": brief(answer)}
        return len(new_engine) == 1 and new_rows == new_engine and named == new_engine[0], detail

    def password(self) -> str:
        value = secrets.token_urlsafe(18)
        self.secret(value)
        return value

    def c1_domain_databases(self) -> None:
        domain_id = self.state["domain_id"]
        path = f"/api/v1/domains/{domain_id}/databases"
        for engine, api_type in (("mariadb", "mysql"), ("postgresql", "postgresql")):
            route = "domain-database-" + engine
            bodies = {key: {"name": name, "type": api_type, "password": self.password()}
                      for key, name in (("i", "seq"), ("ii", "con"), ("other", "oth"))}
            server_id = (self.state.get("database_servers") or {}).get(engine)
            done = self.suite(route, path, bodies,
                              self.database_effect(engine, lambda r, s=server_id: r.get("domain_id") == domain_id
                                                   and (s is None or r.get("server_id") == s)),
                              self.one_database, retained=True, timeout=900)
            first = done["sequential"][0].get("_parsed") if isinstance(done["sequential"][0].get("_parsed"), dict) else {}
            self.state.setdefault("site_databases", {})[engine] = {"name": first.get("name"), "id": first.get("id")}
        listed = self.call("C1 the domain's databases (DomainDatabaseManager)", "GET", path)
        names = [d.get("name") for d in ((listed["_parsed"] or {}).get("databases") or [])] if isinstance(listed["_parsed"], dict) else []
        self.current["domain_databases_listed"] = names
        self.check("the domain's page lists exactly the four databases that were created (two per engine)", len(names) == 4, names)
        site_db = (self.state.get("site_databases") or {}).get("mariadb", {}).get("name")
        if site_db:
            seeded = self.owner2("seed-rows", "owner-seed-rows", database=site_db, action="seed", rows=SITE_ROWS)
            self.check("the owner's application rows are in the site's MariaDB database",
                       seeded.get("returncode") == 0 and (seeded.get("rows_and_sum") or [""])[0] == str(SITE_ROWS), seeded)
        else:
            self.check("the site's MariaDB database exists for the owner's rows", None, self.state.get("site_databases"))

    def c3_server_databases(self) -> None:
        servers = self.state.get("database_servers") or {}
        # O14: C2 provisioned the Panel's account on each engine again; the engine is asked for its version then.
        self.engine_versions("C3, after the Panel's account was provisioned again")
        for engine in ("mariadb", "postgresql"):
            route = "server-database-" + engine
            server_id = servers.get(engine)
            if not server_id:
                self.not_reached(route, ("i", "ii", "iii", "iv"), "the Databases page lists no such server", servers)
                continue
            path = f"/api/v1/database-servers/{server_id}/databases"
            minted = engine == "mariadb"     # no password sent: the Panel mints one and shows it once
            existing = None
            if not minted:
                # H35: only a request for an existing database user has an answer without a password (a kept answer).
                users = [u for u in self.panel_state(route + "-users")["database_users"] if u.get("server_id") == server_id]
                existing = users[0]["id"] if users else None
            bodies = {}
            for key, name in (("i", "srvseq"), ("ii", "srvcon"), ("other", "srvoth")):
                bodies[key] = {"database_name": name, "user_id": existing} if existing else                     {"database_name": name, "new_username": name + "u"}
            self.current.setdefault("who_chose_the_database_users_pw", {})[engine] =                 "an existing database user is given access: the answer carries no password and is kept" if existing                 else "a new user without a password: the Panel mints one and shows it once (the answer is not kept)"
            self.suite(route, path, bodies, self.database_effect(engine, lambda r, s=server_id: r.get("server_id") == s),
                       self.one_database, retained=bool(existing), timeout=900)
            # set3 (O10): a new user with a password the owner sent. The answer does not send it back, so it is kept
            # and the replay is that answer byte for byte.
            sent = {"database_name": "srvsent", "new_username": "srvsentu", "new_password": self.password()}
            identity = self._own_identity(route + " (a password the owner sent)")
            before = self.database_effect(engine, lambda r, s=server_id: r.get("server_id") == s)(route + "-sent-before")
            first = self.post(f"{route}: a new user with a password the owner sent", path, sent, identity, 900)
            again = self.post(f"{route}: the same request again", path, sent, identity, 900)
            after = self.database_effect(engine, lambda r, s=server_id: r.get("server_id") == s)(route + "-sent-after")
            parsed = first.get("_parsed") if isinstance(first.get("_parsed"), dict) else {}
            record = {"first": brief(first), "answer_keys": sorted(parsed), "answer_carries_a_password_field": "password" in parsed,
                      "password_set": parsed.get("password_set"),
                      "sent_value_occurs_in_the_raw_answer": first.get("sent_secret_values_in_the_raw_answer"),
                      "replay": brief(again), "replay_sent_value_occurs": again.get("sent_secret_values_in_the_raw_answer"),
                      "same_bytes": self.raw_digests.get(first["n"]) is not None
                      and self.raw_digests.get(first["n"]) == self.raw_digests.get(again.get("n")),
                      "new_on_engine": one_new(before["fp"]["on_engine"], after["fp"]["on_engine"])}
            self.current.setdefault("sent_password", {})[engine] = record
            self.check(f"O10 ({engine}): a database created with a password the owner sent: the answer does not echo it "
                       "(no `password` field, `password_set: true`, the value is not in the answer's bytes)",
                       first.get("status") == 200 and not record["answer_carries_a_password_field"]
                       and parsed.get("password_set") is True and record["sent_value_occurs_in_the_raw_answer"] == 0, record)
            self.check(f"O10 ({engine}): its replay is the first answer byte for byte, marked as a replay, and one database was created",
                       again.get("status") == 200 and again.get("replayed") == "1" and record["same_bytes"]
                       and len(record["new_on_engine"]) == 1 and record["new_on_engine"][0] == parsed.get("name"), record)   # H40

    # -- C2: the Panel's own account ---------------------------------------------------------------------

    def c2_admin_account(self) -> None:
        servers = self.state.get("database_servers") or {}
        generations: dict[str, int] = {}

        for engine in ("mariadb", "postgresql"):
            route = "admin-account-" + engine
            server_id = servers.get(engine)
            if not server_id:
                self.not_reached(route, ("i", "ii", "iii", "iv"), "the Databases page lists no such server", servers)
                continue
            path = f"/api/v1/database-servers/{server_id}/admin-account"

            def effect(label: str, engine=engine, path=path) -> dict:
                shown = self.api("GET", path, purpose=f"DatabaseAccountStrip reveal ({label})")
                parsed = shown.json() if shown.status == 200 else None
                password = parsed.get("password") if isinstance(parsed, dict) else None
                username = parsed.get("username") if isinstance(parsed, dict) else None
                login = None
                if password:
                    self.secret(password)
                    generation = generations.setdefault(hashlib.sha256((engine + password).encode()).hexdigest(), len(generations) + 1)
                    login = self.snap2(label + "-login", "read-login", engine=engine, username=username,
                                       password_b64=base64.b64encode(password.encode()).decode())
                else:
                    generation = 0
                counts = self.panel_state(label + "-rows")["audit_counts"]
                provisions = sum(n for action, n in counts.items() if action.startswith("database.admin_account.provision:"))
                failures = sum(n for action, n in counts.items() if action.startswith("database.admin_account.provision.failed"))
                return {"fp": {"stored_value_generation": generation, "provision_audit_entries": provisions,
                               "failed_provision_audit_entries": failures, "username": username},
                        "reveal_status": shown.status, "login_with_what_the_panel_stores": (login or {}).get("logged_in"),
                        "login": {k: (login or {}).get(k) for k in ("returncode", "stdout", "stderr")}}

            def one_effect(before: dict, after: dict, answer: dict | None, identity: str) -> tuple:
                detail = {"before": before["fp"], "after": after["fp"], "login_with_what_the_panel_stores": after["login_with_what_the_panel_stores"],
                          "login": after["login"], "answer": brief(answer)}
                return (after["fp"]["provision_audit_entries"] == before["fp"]["provision_audit_entries"] + 1
                        and after["fp"]["stored_value_generation"] not in (0, before["fp"]["stored_value_generation"])
                        and after["login_with_what_the_panel_stores"] is True), detail

            done = self.suite(route, path, {"i": None, "ii": None, "other": {"again": True}}, effect, one_effect,
                              retained=False, timeout=900)
            final = done["effects"]["ii"]
            self.judge(route, "ii", "after the concurrent arrivals a login with the password the Panel stores succeeds on the engine",
                       final["login_with_what_the_panel_stores"] is True, final["login"])

    # -- C4: manual backup ---------------------------------------------------------------------------------

    def backups(self, label: str) -> dict:
        return self.snap2(label, "read-backups", domain_id=self.state["domain_id"])

    def backup_effect(self, label: str) -> dict:
        native = self.backups(label)
        names = sorted(e["name"] for e in native["entries"] if e["name"].endswith(".cpbak") and not e["name"].startswith("."))
        return {"fp": {"archives": names, "manifests_read": native["manifests_read"], "partial_files": native["partial_files"],
                       "origins": native["origins"]}, "entries": native["entries"]}

    def c4_backup(self) -> None:
        path = f"/api/v1/domains/{self.state['domain_id']}/backups"

        def one_effect(before: dict, after: dict, answer: dict | None, identity: str) -> tuple:
            new = one_new(before["fp"]["archives"], after["fp"]["archives"])
            entry = next((e for e in after["entries"] if new and e["name"] == new[0]), {})
            manifest = entry.get("manifest") or {}
            named = ((answer or {}).get("_parsed") or {}).get("backup", {}).get("name") if answer and isinstance(answer.get("_parsed"), dict) else None
            detail = {"new_archives": new, "manifest": manifest, "bytes": entry.get("bytes"), "answer_names": named,
                      "expected_job_key": "request:" + identity, "partial_files": after["fp"]["partial_files"]}
            return (len(new) == 1 and manifest.get("origin") == "manual" and manifest.get("type") == "full"
                    and manifest.get("job_key") == "request:" + identity and named == new[0]
                    and after["fp"]["manifests_read"] == len(after["fp"]["archives"]) and not after["fp"]["partial_files"]), detail

        done = self.suite("backup", path, {"i": {"type": "full"}, "ii": {"type": "full"}, "other": {"type": "files"}},
                          self.backup_effect, one_effect, retained=True, timeout=2400)
        names = []
        for answer in (done["sequential"][0], done["own_concurrent"]):
            parsed = (answer or {}).get("_parsed")
            names.append(parsed.get("backup", {}).get("name") if isinstance(parsed, dict) else None)
        self.state["backups"] = {"reference": names[0], "other": names[1]}
        self.current["backups_for_restore"] = self.state["backups"]
        listing = self.call("C4 the domain's backups (DomainBackupManager)", "GET", path)
        listed = [b.get("name") for b in ((listing["_parsed"] or {}).get("backups") or [])] if isinstance(listing["_parsed"], dict) else []
        self.check("the Backups panel lists exactly the two archives (one per identity)", sorted(listed) == sorted(n for n in names if n), listed)

    # -- C5: Let's Encrypt, the guard only ----------------------------------------------------------------

    def c5_letsencrypt(self) -> None:
        route = "letsencrypt"
        path = f"/api/v1/domains/{self.state['domain_id']}/ssl/letsencrypt"
        section_start = int(self.native2("read-clock")["epoch"])
        entered = re.compile(r"\]\[agent\]|SSL issue domain|certificate request|letsencrypt|certbot", re.I)

        def effect(label: str) -> dict:
            acme = self.snap2(label + "-acme", "read-acme")
            log = self.snap2(label + "-journal", "read-journal", units=["celikpanel-panel.service", "celikpanel-agent.service"],
                             since_epoch=section_start, lines=2000)["journal"].get("stdout", "")
            lines = [l for l in log.splitlines() if entered.search(l)]
            nginx = self.snap2(label + "-nginx", "read-journal", units=["nginx.service"], since_epoch=section_start,
                               lines=2000)["journal"].get("stdout", "")
            reloads = [l for l in nginx.splitlines() if "Reload" in l]
            certificates = [c for c in self.panel_state(label + "-rows")["ssl_certificates"] if c.get("domain_id") == self.state["domain_id"]]
            return {"fp": {"certbot_logs": acme["certbot_log_count"], "panel_and_agent_lines": len(lines),
                           "nginx_reload_lines": len(reloads), "certificates": len(certificates)},
                    "lines": lines[-12:], "acme_isolated": acme.get("acme_isolated")}

        # H38: no request is sent to this route unless the lab's ACME isolation is confirmed on this guest.
        isolation = self.snap2(route + "-isolation", "read-acme")
        if isolation.get("acme_isolated") is not True:
            self.not_reached(route, ("i", "ii", "iii", "iv"), "the lab's ACME isolation is not confirmed on this guest, so no "
                             "request was sent to the route (a certificate authority could have been contacted)",
                             isolation.get("resolves"))
            return
        # H39 (rid-arch run-b): certbot's log files are recorded but not compared: the first run on a guest creates more
        # than one file and each later run one, so their number does not grow by the same amount per entry.
        counters = ("panel_and_agent_lines", "nginx_reload_lines")
        bodies = {"i": {"email": "owner@set2-lab.test", "auto_renew": True, "reissue": True},
                  "ii": {"email": "owner@set2-lab.test", "auto_renew": True, "reissue": True},
                  "other": {"email": "other@set2-lab.test", "auto_renew": False, "reissue": True}}
        # What ONE entry into the handler leaves is measured first, with an identity of its own (a new identity always
        # enters once); the arrivals of one identity are then held to exactly that.
        before_one = effect(route + "-one-entry-before")
        single = new_identity()
        self.identities[single] = route + " (one entry, for comparison)"
        measured = self.post(f"{route}: one request with an identity of its own (what one entry leaves)", path, bodies["i"], single, 1800)
        after_one = effect(route + "-one-entry-after")
        one_entry = {key: after_one["fp"][key] - before_one["fp"][key] for key in counters}
        self.current["one_entry_leaves"] = {"delta": one_entry, "answer": brief(measured), "lines": after_one["lines"][-6:]}
        self.check("one entry into the handler leaves a trace that can be counted (journal lines, certbot logs or reloads)",
                   True if any(one_entry.values()) else None, self.current["one_entry_leaves"])

        def one_effect(before: dict, after: dict, answer: dict | None, identity: str) -> tuple:
            delta = {key: after["fp"][key] - before["fp"][key] for key in counters}
            detail = {"delta": delta, "one_entry_leaves": one_entry, "answer": brief(answer),
                      "certbot_log_files": [before["fp"]["certbot_logs"], after["fp"]["certbot_logs"]],
                      "what_counts": "journal lines of the Panel and the Agent about the issuance and nginx reloads for the "
                                     "validation virtual host, each compared with what one entry left"}
            if not any(one_entry.values()):
                return None, detail
            return delta == one_entry, detail

        done = self.suite(route, path, bodies, effect, one_effect, retained=True, timeout=1800)
        first = done["sequential"][0]
        self.current["answer_without_acme"] = {"status": first.get("status"), "json": first.get("json"), "text": first.get("text"),
                                               "seconds": first.get("seconds")}
        self.note("no certificate authority is reachable from this guest, so no issuance was attempted against one; the "
                  "route's answer without ACME is recorded and only the guard is judged", self.current["answer_without_acme"])
        self.check("no certificate was recorded for the domain", done["effects"]["ii"]["fp"]["certificates"] == 0,
                   done["effects"]["ii"]["fp"])
        # set3 (O9): the authority is unreachable from this guest (its names resolve to loopback): a typed answer.
        parsed = first.get("_parsed") if isinstance(first.get("_parsed"), dict) else {}
        self.current["certificate_failure"] = {"status": first.get("status"), "code": parsed.get("code"), "reason": parsed.get("reason"),
                                               "error": parsed.get("error"), "vars": parsed.get("vars"),
                                               "texts": first.get("catalogue_texts"), "one_entry_answer": brief(measured)}
        self.check("O9: with the certificate authority unreachable the answer is 502 CERTIFICATE_ISSUE_FAILED, reason "
                   "authority_unreachable", first.get("status") == 502 and parsed.get("code") == "CERTIFICATE_ISSUE_FAILED"
                   and parsed.get("reason") == "authority_unreachable", self.current["certificate_failure"])

    # -- C6: VPN peer ------------------------------------------------------------------------------------------

    def c6_vpn_peer(self) -> None:
        route = "vpn-peer"
        subscription_id = self.state.get("subscription_id")
        status = self.call("C6 VPN status (VPNPage)", "GET", "/api/v1/vpn/status")
        ready = isinstance(status["_parsed"], dict) and status["_parsed"].get("running")
        prerequisites = self.current.setdefault("prerequisites", [])
        if not ready:
            request_id = new_identity()
            install = self.call("C6 install the WireGuard component (Components page)", "POST", "/api/v1/service/install",
                                {"request_id": request_id, "service_id": "wireguard"}, timeout=300)
            prerequisites.append({"step": "install", "status": install["status"], "answer": install.get("answer")})
            operation = None
            deadline = time.monotonic() + 1200
            while install["status"] in (200, 202) and time.monotonic() < deadline:
                polled = self.api("GET", f"/api/v1/service/operation?request_id={request_id}", purpose="service operation poll")
                parsed = polled.json() if polled.status == 200 else None
                operation = parsed.get("operation", parsed) if isinstance(parsed, dict) else None
                if isinstance(operation, dict) and (operation.get("finished_at") or operation.get("status") in (
                        "succeeded", "failed", "interrupted", "cancelled")):
                    break
                time.sleep(5)
            self.current["wireguard_install"] = {k: (operation or {}).get(k) for k in ("status", "phase", "started_at", "finished_at", "error")}
            prerequisites.append({"step": "install-operation", "operation": self.current["wireguard_install"]})
            scan = self.call("C6 scan the components again", "POST", "/api/v1/managed-services/scan", None, timeout=300)
            prerequisites.append({"step": "scan", "status": scan["status"]})
            status = self.call("C6 VPN status after the install", "GET", "/api/v1/vpn/status")
            ready = isinstance(status["_parsed"], dict) and status["_parsed"].get("running")
        self.current["vpn_status"] = {k: (status["_parsed"] or {}).get(k) for k in ("installed", "configured", "running", "port", "endpoint", "peer_count")} \
            if isinstance(status["_parsed"], dict) else status.get("answer")
        grant = self.call("C6 grant the VPN add-on to the subscription (Add-ons page)", "POST",
                          f"/api/v1/subscriptions/{subscription_id}/entitlements", {"product_id": "vpn"})
        prerequisites.append({"step": "entitlement", "status": grant["status"], "answer": grant.get("answer") or grant["_parsed"]})
        if not ready or grant["status"] != 200:
            self.not_reached(route, ("i", "ii", "iii", "iv"), "the VPN server could not be made ready through the Panel on this guest",
                             {"vpn_status": self.current["vpn_status"], "prerequisites": prerequisites})
            return

        def effect(label: str) -> dict:
            native = self.snap2(label, "read-wireguard")
            rows = [p for p in self.panel_state(label + "-rows")["vpn_peers"]]
            active = sorted(p["public_key"] for p in rows if p.get("desired_state") == "active")
            live = sorted(key for keys in native["peers"].values() for key in keys)
            return {"fp": {"wg_peers": live, "config_peer_blocks": native["config_peer_blocks"], "panel_active_rows": active,
                           "panel_rows": len(rows)}, "rows": rows}

        def one_effect(before: dict, after: dict, answer: dict | None, identity: str) -> tuple:
            new_live = one_new(before["fp"]["wg_peers"], after["fp"]["wg_peers"])
            new_rows = one_new(before["fp"]["panel_active_rows"], after["fp"]["panel_active_rows"])
            key = ((answer or {}).get("_parsed") or {}).get("public_key") if answer and isinstance(answer.get("_parsed"), dict) else None
            detail = {"new_wg_peers": new_live, "new_active_rows": new_rows, "answer_public_key": key,
                      "rows_before_after": [before["fp"]["panel_rows"], after["fp"]["panel_rows"]],
                      "config_peer_blocks": [before["fp"]["config_peer_blocks"], after["fp"]["config_peer_blocks"]],
                      "answer": brief(answer)}
            return (len(new_live) == 1 and new_rows == new_live and key == new_live[0]
                    and after["fp"]["panel_rows"] == before["fp"]["panel_rows"] + 1), detail

        def acknowledge(answer: dict) -> None:
            parsed = answer.get("_parsed") if isinstance(answer.get("_parsed"), dict) else {}
            if answer.get("status") == 200 and parsed.get("id") and parsed.get("delivery_token"):
                done = self.call(f"C6 the page confirms it received the configuration of peer {parsed['id']}", "POST",
                                 f"/api/v1/vpn/peers/{parsed['id']}/ack", {"delivery_token": parsed["delivery_token"]})
                self.current.setdefault("acknowledged", []).append({"peer": parsed["id"], "status": done["status"]})

        bodies = {key: {"name": name, "subscription_id": subscription_id}
                  for key, name in (("i", "set2 laptop"), ("ii", "set2 phone"), ("other", "set2 other"))}
        self.suite(route, "/api/v1/vpn/peers", bodies, effect, one_effect, retained=False, timeout=900, on_answer=acknowledge)
        peers = self.call("C6 the devices list (VPNPage)", "GET", "/api/v1/vpn/peers")
        self.current["devices_listed"] = [{k: p.get(k) for k in ("id", "name", "ip", "desired_state", "sync_state")}
                                          for p in ((peers["_parsed"] or {}).get("peers") or [])] if isinstance(peers["_parsed"], dict) else None

    # -- C7: import ----------------------------------------------------------------------------------------------

    def imported(self, label: str, fixture: dict) -> dict:
        native = self.snap2(label, "read-imported", domain=fixture["domain"])
        engines = self.engines(label + "-engines", (fixture["database"],))
        table = engines["seed_tables"].get(fixture["database"], {})
        row = native["rows"][0] if native["rows"] and "id" in native["rows"][0] else {}
        docroot = native.get("docroot") or {}
        return {"fp": {"domain_row": bool(row), "status": row.get("status"), "files": docroot.get("files"),
                       "docroot_sha256": docroot.get("sha256"), "mailboxes": sorted(a.get("address") for a in native["email_accounts"] if a.get("address")),
                       "forwardings": len(native["email_forwardings"]),
                       "database_on_engine": fixture["database"] in engines["mariadb"]["databases"],
                       "database_row": [d.get("name") for d in native["databases_v2"]], "rows": table.get("rows")},
                "docroot": docroot, "site_account": native.get("site_account"), "escapes": native.get("escapes"),
                "site_home_entries": native.get("site_home_entries"), "file_list": docroot.get("file_list"),
                "non_regular": docroot.get("non_regular")}

    def import_complete(self, fixture: dict, state: dict, answer: dict | None) -> tuple:
        fp = state["fp"]
        parsed = (answer or {}).get("_parsed") if answer and isinstance(answer.get("_parsed"), dict) else {}
        steps = parsed.get("steps") or []
        expected_files = 2 + (1 if fixture["megabytes"] else 0)
        mail_ok = (not self.settings.mail) or fp["mailboxes"] == ["info@" + fixture["domain"]]
        # set3 (P4): the document root holds exactly the archive's site files (names, sizes, digests).
        same_files = state.get("file_list") == fixture.get("docroot_expected") and not state.get("non_regular")
        answered = answer is None or ((answer or {}).get("status") == 200 and parsed.get("status") == "active")
        detail = {"native": fp, "expected_files": expected_files, "expected_rows": str(IMPORT_ROWS),
                  "answer_status": (answer or {}).get("status"), "import_status": parsed.get("status"),
                  "docroot_files_equal_the_archives": same_files, "answered_200_active": answered,
                  "steps": [{k: s.get(k) for k in ("step", "ok", "code")} for s in steps]}
        return (same_files and answered and fp["domain_row"] and fp["status"] == "active" and fp["files"] == expected_files and mail_ok
                and fp["database_on_engine"] and fp["database_row"] == [fixture["database"]] and fp["rows"] == str(IMPORT_ROWS)), detail

    def c7_import(self) -> None:
        route = "import"
        subscription_id = self.state.get("subscription_id")
        fixtures = {}
        # set3 (S1): every archive's mailbox carries the crypt hash of one password the lab knows. The password is
        # registered with the redactor and never written; its hash is made on the guest and never leaves it.
        self.import_password = self.password()
        self.redactor.register(base64.b64encode(self.import_password.encode()).decode())
        for key, megabytes, sleep in (("i", 0, 0), ("ii", 0, 0), ("v", 16, IMPORT_DROP_SLEEP), ("tar", 0, 0)):
            tag = {"i": "seq", "ii": "con", "v": "drop", "tar": "tar"}[key] + IMPORT_SUFFIX
            fixtures[key] = self.import_fixture(tag, f"set2-import-{tag}.test", "s2imp" + tag, megabytes, sleep)
        self.current["fixtures"] = fixtures
        self.state["import_fixtures"] = fixtures
        self.note("no sample cPanel archive exists in the repository, so the lab built a minimal one from the rules of the "
                  "import parser (cmd/agent/cpmove_rpc.go): account file, public_html, one mailbox, one forwarder, one "
                  "database dump; the dropped import's dump ends with DO SLEEP so that the import lasts long enough to drop. "
                  "set3: every archive holds the directory member `homedir/public_html/` as tar writes it (the import "
                  "now takes it, P4), and the mailbox's shadow entry is the sha512-crypt hash of a password the lab knows")
        body = self.import_body

        for key in ("i", "ii", "v", "tar"):
            self.preview(key, fixtures[key])

        # H34 (set2 run-a): an archive that holds the directory member `homedir/public_html/` the way tar writes an archive
        # of a directory. One request with an identity of its own; what the import does with it is recorded and judged.
        whole = self.post("import: an archive that holds the public_html directory member as tar writes it",
                          "/api/v1/import/cpanel/apply", body(fixtures["tar"]), self._own_identity("import (an archive as tar writes it)"), 3600)
        state = self.imported("tar-archive-after", fixtures["tar"])
        parsed = whole.get("_parsed") if isinstance(whole.get("_parsed"), dict) else {}
        files_step = next((s for s in parsed.get("steps") or [] if s.get("step") == "files"), {})
        self.current["archive_as_tar_writes_it"] = {"status": whole.get("status"), "import_status": parsed.get("status"),
                                                    "steps": parsed.get("steps"), "native": state["fp"]}
        self.current["archive_as_tar_writes_it"].update(
            docroot_files=state.get("file_list"), archive_files=fixtures["tar"]["docroot_expected"],
            imported=parsed.get("imported"), not_imported=parsed.get("not_imported"))
        self.check("P4: an archive that holds the directory member `homedir/public_html/` as tar writes it imports completely "
                   "(200, active; the document root's files equal the archive's)",
                   whole.get("status") == 200 and parsed.get("status") == "active" and files_step.get("ok") is True
                   and state.get("file_list") == fixtures["tar"]["docroot_expected"] and not state.get("non_regular")
                   and not parsed.get("not_imported"), self.current["archive_as_tar_writes_it"])

        # The generic arrivals use two archives: the sequential identity imports one domain, the concurrent identity another.
        current = {"fixture": fixtures["i"]}

        def effect(label: str) -> dict:
            a, b = self.imported(label + "-seq", fixtures["i"]), self.imported(label + "-con", fixtures["ii"])
            return {"fp": {"seq": a["fp"], "con": b["fp"]}, "seq": a, "con": b}

        def one_effect(before: dict, after: dict, answer: dict | None, identity: str) -> tuple:
            which = "seq" if not before["fp"]["seq"]["domain_row"] else "con"
            other = "con" if which == "seq" else "seq"
            ok, detail = self.import_complete(fixtures["i" if which == "seq" else "ii"], after[which], answer)
            detail["the_other_domain_unchanged"] = after["fp"][other] == before["fp"][other]
            return ok and detail["the_other_domain_unchanged"], detail

        bodies = {"i": body(fixtures["i"]), "ii": body(fixtures["ii"]), "other": body(fixtures["i"], do_files=False)}
        done = self.suite(route, "/api/v1/import/cpanel/apply", bodies, effect, one_effect, retained=True, timeout=3600)
        current["done"] = True
        self.current["import_answer"] = done["sequential"][0].get("json")

        # (v) the connection is dropped while the import runs
        fixture = fixtures["v"]
        identity = new_identity()
        self.identities[identity] = route + " (v)"
        before = self.imported("v-before", fixture)

        def hold() -> dict:
            started = time.time()
            while time.time() - started < 120:
                rows = self.native2("read-imported", domain=fixture["domain"])["rows"]
                if rows and "id" in rows[0]:
                    time.sleep(2)
                    return {"seen": "the Panel's database names the domain: the import is under way",
                            "after_seconds": round(time.time() - started, 1)}
                time.sleep(0.5)
            return {"seen": None, "after_seconds": round(time.time() - started, 1)}

        dropped = self.post_and_drop(f"{route} (v) the connection is dropped while the import runs", "/api/v1/import/cpanel/apply",
                                     body(fixture), identity, hold)
        at_drop = self.row(identity)
        self.judge(route, "v", "the request was still running when the connection was dropped",
                   dropped["answer_bytes_received_before_the_drop"] == 0 and bool(at_drop) and at_drop["status"] == "running",
                   {"dropped": {k: dropped[k] for k in ("held", "dropped_after_seconds", "answer_bytes_received_before_the_drop")},
                    "row_at_drop": at_drop})
        waited = self.wait_row(identity, "import-v")
        row = waited["row"] or {}
        after = self.imported("v-after", fixture)
        ok, detail = self.import_complete(fixture, after, None)
        self.judge(route, "v", "the server finished the import after the client was gone (row done; the whole site is there)",
                   row.get("status") == "done" and row.get("response_status") in (200, 202) and ok,
                   {"row": row, "waited_seconds": waited["waited_seconds"], "import": detail, "before": before["fp"]})
        replay = self.post(f"{route} (v) the same identity again after the drop", "/api/v1/import/cpanel/apply", body(fixture), identity)
        again = self.imported("v-after-replay", fixture)
        parsed = replay.get("_parsed") if isinstance(replay.get("_parsed"), dict) else {}
        self.judge(route, "v", "the replay gets the stored answer (the first run's bytes, marked as a replay) and imports nothing again",
                   replay.get("status") == row.get("response_status") and replay.get("replayed") == "1"
                   and self.raw_digests.get(replay["n"]) == (row.get("body") or {}).get("sha256") and again["fp"] == after["fp"],
                   {"replay": brief(replay), "import_status": parsed.get("status"), "stored_sha256": (row.get("body") or {}).get("sha256"),
                    "replay_sha256": self.raw_digests.get(replay["n"]), "unchanged": again["fp"] == after["fp"]})

    # -- set3: import helpers, C6b and C7b ------------------------------------------------------------------------

    def import_fixture(self, tag: str, domain: str, user: str, megabytes: int = 0, sleep: int = 0, hostile: str | None = None) -> dict:
        made = self.owner2("cpmove-fixture-" + tag, "owner-cpmove-fixture", domain=domain, user=user, database=user + "_app",
                           megabytes=megabytes, sleep_seconds=sleep, rows=IMPORT_ROWS, public_html_directory_member=True,
                           hostile=hostile, password_b64=base64.b64encode((self.import_password or "").encode()).decode()
                           if self.import_password else None)
        fixture = {k: made.get(k) for k in ("path", "bytes", "sha256", "domain", "user", "database", "members",
                                            "public_html_directory_member", "hostile", "hostile_members", "docroot_expected",
                                            "mailbox_shadow")}
        fixture.update(megabytes=megabytes, sleep_seconds=sleep)
        return fixture

    def import_body(self, fixture: dict, **changes: Any) -> dict:
        value = {"path": fixture["path"], "subscription_id": self.state.get("subscription_id"), "domain": fixture["domain"],
                 "do_files": True, "do_mail": self.settings.mail, "do_dns": False, "do_databases": True}
        value.update(changes)
        return value

    def preview(self, key: str, fixture: dict) -> dict:
        """The import page's preview of one archive. set3 (S1): the answer's raw bytes are searched for hash-shaped
        values before anything is recorded, and only each mailbox's four named fields are kept."""
        label = f"inspect the archive of {fixture['domain']} (ImportPage preview)"
        response = self.api("POST", "/api/v1/import/cpanel/inspect", {"path": fixture["path"]}, purpose=label, timeout=300)
        parsed = response.json()
        seen = parsed if isinstance(parsed, dict) else {}
        boxes = [box for box in seen.get("mail_accounts") or [] if isinstance(box, dict)]
        record = {"label": label, "archive": key, "at": base.utc_now(), "status": response.status, "keys": sorted(seen),
                  "domains": seen.get("domains"), "public_html": seen.get("public_html"),
                  "databases": [d.get("name") if isinstance(d, dict) else d for d in seen.get("databases") or []],
                  "mail_accounts": [{k: box.get(k) for k in MAILBOX_KEYS} for box in boxes],
                  "mailbox_keys": sorted({k for box in boxes for k in box}),
                  "hash_shaped_values_in_the_raw_answer": base.hash_shaped_count(response.text),
                  "raw_bytes": len(response.text.encode())}
        if response.status != 200:
            record["answer"] = {k: seen.get(k) for k in ("code", "error", "reason")}
        self.current.setdefault("previews", []).append(record)
        self.check(f"the preview of the {key} archive names its domain, its mailbox and its database",
                   response.status == 200 and fixture["domain"] in (seen.get("domains") or []) and bool(record["databases"])
                   and bool(boxes), record)
        self.check(f"S1: the preview of the {key} archive holds no hash-shaped value; a mailbox is its address, its quota and "
                   "`has_password` only", response.status == 200 and record["hash_shaped_values_in_the_raw_answer"] == 0
                   and record["mailbox_keys"] == MAILBOX_KEYS and all(box.get("has_password") is True for box in boxes), record)
        return record

    def c6b_php_site(self) -> None:
        """set3 (P5): a PHP site from the Domains page, one PHP page asked for through the web server, the site
        deleted again. The native PHP-FPM facts of the platform are recorded beside it."""
        domain = PHP_SITE_DOMAIN
        self.current["php_before"] = self.snap2("php-before", "read-php")
        if os.environ.get(OWNER_SNIPPET_ENV) == "1":
            # A second reading only (never the cell's first run): the owner places the one file whose absence stopped
            # the first run, so that what lies behind that stop can be measured. Recorded as an owner action.
            placed = self.owner2("nginx-php-snippet", "owner-nginx-php-snippet")
            self.current["owner_placed_the_nginx_snippet"] = {k: placed.get(k) for k in ("path", "existed_before", "made_directory",
                                                                                         "file", "sha256", "fastcgi_conf")}
            self.note("OWNER ACTION for this reading: the server owner placed /etc/nginx/snippets/fastcgi-php.conf by hand "
                      "(the text Debian's nginx package ships). Every result of C6b, C7 and C7b in this run is a result WITH "
                      "that file; without it the PHP site cannot be created on this platform (the first run).",
                      self.current["owner_placed_the_nginx_snippet"])
        created = self.call("C6b create a PHP site (AddDomainModal)", "POST", "/api/v1/domains/create",
                            {"domain": domain, "project_type": "php", "ssl_type": "none"}, timeout=600)
        answer = created["_parsed"] if isinstance(created["_parsed"], dict) else {}
        domain_id = answer.get("DomainID") or answer.get("domain_id")
        self.check("P5: a PHP site is created from the Domains page (200 with the new domain's id)",
                   created["status"] == 200 and isinstance(domain_id, int),
                   created.get("answer") or {k: answer.get(k) for k in sorted(answer) if "id" in k.lower() or "root" in k.lower()})
        if not isinstance(domain_id, int):
            return
        probe = self.owner2("php-probe", "owner-php-probe", domain_id=domain_id)
        page = self.snap2("php-page", "read-http", domain=domain, path=PHP_PROBE_PATH)
        text = str(page.get("body") or "")
        parts = text.strip().split(":")
        executed = page.get("status") == 200 and text.startswith(PHP_PROBE_MARK) and "<?php" not in text and len(parts) >= 5
        served = {"status": page.get("status"), "headers": page.get("headers"), "body": text[:200], "error": page.get("error"),
                  "php_version": parts[2] if executed else None, "server_api": parts[3] if executed else None,
                  "runs_as": parts[4] if executed else None, "site_account": probe.get("site_account")}
        self.current["php_page"] = served
        self.check("P5: the PHP page asked for through the web server was executed by PHP-FPM (the marker and 6 * 7 = 42, "
                   "no source text, server API fpm-fcgi)", executed and served["server_api"] == "fpm-fcgi", served)
        self.check("P5: the page runs as the site's own account", executed and served["runs_as"] == probe.get("site_account"), served)
        facts = self.snap2("php-with-site", "read-php")
        pool = next((p for p in facts["pools"] if p["settings"].get("user") == probe.get("site_account")), None)
        socket_path = ((pool or {}).get("settings") or {}).get("listen")
        native = {"units": {name: {k: unit.get(k) for k in ("LoadState", "ActiveState", "SubState", "FragmentPath", "ExecStart",
                                                            "ProtectHome", "ProtectSystem", "PrivateTmp", "ReadWritePaths")}
                            for name, unit in facts["units"].items()},
                  "pool_directories": facts["pool_directories"], "site_pool": pool,
                  "socket": facts["sockets"].get(socket_path) if socket_path else None,
                  "run_directories": facts["run_directories"], "program": facts["program"],
                  "program_version": facts["program_version"], "web_server_accounts": facts["web_server_accounts"]}
        self.current["php_native"] = native
        self.check("P5: the site's pool file exists under the platform's pool directory and its socket is a socket owned by "
                   "an account the web server runs as", bool(pool) and bool(native["socket"])
                   and native["socket"].get("kind") == "socket"
                   and native["socket"].get("owner") in (facts["web_server_accounts"] + [probe.get("site_account")]), native)
        deleted = self.call("C6b delete the PHP site (Domains page)", "DELETE", f"/api/v1/domains/{domain_id}", timeout=600)
        gone, started, listed_now = False, time.time(), None
        while time.time() - started < 240:
            listing = self.api("GET", "/api/v1/domains", purpose="Domains list after the delete")
            rows = listing.json() if listing.status == 200 else None
            listed_now = [d.get("domain_name") for d in rows] if isinstance(rows, list) else None
            if isinstance(listed_now, list) and domain not in listed_now:
                gone = True
                break
            time.sleep(3)
        left = self.snap2("php-after-delete", "read-imported", domain=domain)
        facts_after = self.snap2("php-facts-after-delete", "read-php")
        page_after = self.snap2("php-page-after-delete", "read-http", domain=domain, path=PHP_PROBE_PATH)
        after = {"delete_status": deleted["status"], "delete_answer": deleted.get("answer") or deleted["_parsed"],
                 "listed_after_seconds": round(time.time() - started, 1), "no_longer_listed": gone,
                 "domain_rows": left.get("rows"), "site_account": left.get("site_account"),
                 "pool_file_left": any(p["settings"].get("user") == probe.get("site_account") for p in facts_after["pools"]),
                 "socket_left": bool(socket_path) and socket_path in facts_after["sockets"]
                 and facts_after["sockets"][socket_path].get("exists"),
                 "page_after": {"status": page_after.get("status"), "marker": PHP_PROBE_MARK in str(page_after.get("body") or "")},
                 "php_fpm_units_after": {name: unit.get("ActiveState") for name, unit in facts_after["units"].items()}}
        self.current["php_deleted"] = after
        self.check("P5: the PHP site is deleted: no longer listed, no domain row, no site account, its pool file gone, the "
                   "page no longer served, and PHP-FPM still active",
                   gone and not after["domain_rows"] and after["site_account"] is None and not after["pool_file_left"]
                   and not after["page_after"]["marker"]
                   and all(state == "active" for name, state in after["php_fpm_units_after"].items()
                           if facts["units"].get(name, {}).get("ActiveState") == "active"), after)

    def c7b_import_answers(self) -> None:
        """set3: (S1) the imported mailboxes authenticate with the original password; (P4) an import whose files
        step fails answers 200 `partial` with truthful lists, and hostile members are never extracted."""
        fixtures = self.state.get("import_fixtures") or {}
        if self.settings.mail and self.import_password:
            secret = base64.b64encode(self.import_password.encode()).decode()
            for key in ("tar", "i", "v"):
                fixture = fixtures.get(key)
                if not fixture:
                    continue
                address = "info@" + fixture["domain"]
                login = self.snap2("mail-login-" + key, "read-mail-login", address=address, password_b64=secret)
                good, wrong = login["readings"]["the_original_password"], login["readings"]["a_wrong_password"]
                record = {"address": address, "the_original_password": good, "a_wrong_password": wrong,
                          "dovecot": login.get("dovecot_version"), "doveadm_user": login.get("doveadm_user")}
                self.current.setdefault("mail_logins", []).append(record)
                self.check(f"S1: the imported mailbox {address} authenticates with its original password (an IMAP LOGIN on "
                           "loopback and `doveadm auth test`)", good["imap"].get("logged_in") is True
                           and good["doveadm_auth_test"].get("returncode") == 0, record)
                self.check(f"S1: the same mailbox refuses a wrong password (both readings)",
                           wrong["imap"].get("logged_in") is False and wrong["doveadm_auth_test"].get("returncode") not in (0, None), record)
        else:
            self.note("mail is not supported on this platform: no mailbox was imported, so S1's login is not measured here")

        for kind in HOSTILE_KINDS:
            short = {"dotdot": "dd", "absolute": "ab", "symlink": "sl"}[kind]
            fixture = self.import_fixture("hostile-" + kind, f"set3-hostile-{kind}.test", "s3h" + short, hostile=kind)
            self.preview("hostile-" + kind, fixture)
            whole = kind == "dotdot"     # the one that also imports mail and the database, for the two lists
            request = self.import_body(fixture) if whole else self.import_body(fixture, do_mail=False, do_databases=False)
            identity = self._own_identity(f"import (hostile member: {kind})")
            before = self.imported(f"hostile-{kind}-before", fixture)
            answer = self.post(f"import: an archive with a hostile member ({kind})", "/api/v1/import/cpanel/apply", request, identity, 3600)
            state = self.imported(f"hostile-{kind}-after", fixture)
            parsed = answer.get("_parsed") if isinstance(answer.get("_parsed"), dict) else {}
            steps = parsed.get("steps") or []
            files_step = next((s for s in steps if s.get("step") == "files"), {})
            should_in, should_out = partial_lists(steps)
            archive_files = fixture["docroot_expected"]
            site_files = state.get("file_list") or {}
            from_archive = sorted(name for name, value in archive_files.items() if site_files.get(name) == value)
            record = {"kind": kind, "hostile_members": fixture.get("hostile_members"), "request": {k: request[k] for k in (
                          "do_files", "do_mail", "do_dns", "do_databases")},
                      "status": answer.get("status"), "import_status": parsed.get("status"), "code": parsed.get("code"),
                      "domain_status": parsed.get("domain_status"), "message": parsed.get("message"),
                      "imported": parsed.get("imported"), "not_imported": parsed.get("not_imported"), "steps": steps,
                      "native": state["fp"], "site_home_entries": state.get("site_home_entries"),
                      "docroot_files": sorted(site_files), "archive_files_in_the_docroot": from_archive,
                      "non_regular_in_the_docroot": state.get("non_regular"), "entries_outside": state.get("escapes"),
                      "entries_outside_before": before.get("escapes")}
            self.current.setdefault("hostile", {})[kind] = record
            self.check(f"P4 ({kind}): nothing of the hostile member exists anywhere on the server, the document root holds no "
                       "link and no stage directory was left in the site's home",
                       state.get("escapes") == [] and not state.get("non_regular")
                       and not any(str(name).startswith(".cpmove") for name in state.get("site_home_entries") or []), record)
            refused = files_step.get("ok") is False
            if kind == "absolute" and not refused:
                self.note("the member with an absolute path is not site payload: the files step left it out and imported the "
                          "site's own files; nothing was written at that path", record)
                self.check("P4 (absolute): the member was left out and the rest imported completely (200, active, the "
                           "document root's files equal the archive's)", answer.get("status") == 200
                           and parsed.get("status") == "active" and site_files == archive_files, record)
                continue
            self.check(f"P4 ({kind}): the files step refuses the archive", refused, files_step)
            self.check(f"P4 ({kind}): the import answers 200 with status `partial`, code IMPORT_PARTIAL and the domain left `pending`",
                       answer.get("status") == 200 and parsed.get("status") == "partial" and parsed.get("code") == "IMPORT_PARTIAL"
                       and parsed.get("domain_status") == "pending" and bool(parsed.get("message")), record)
            mail_ok = (not request["do_mail"]) or state["fp"]["mailboxes"] == ["info@" + fixture["domain"]]
            data_ok = (not request["do_databases"]) or (state["fp"]["database_on_engine"] and state["fp"]["rows"] == str(IMPORT_ROWS)
                                                        and state["fp"]["database_row"] == [fixture["database"]])
            self.check(f"P4 ({kind}): `imported` and `not_imported` are the steps' own results and are true on the server "
                       "(the domain row is `pending`; what is listed as imported is there; none of the archive's site files is "
                       "in the document root)",
                       parsed.get("imported") == should_in and parsed.get("not_imported") == should_out
                       and "files" in (parsed.get("not_imported") or []) and "domain" in (parsed.get("imported") or [])
                       and state["fp"]["domain_row"] and state["fp"]["status"] == "pending" and not from_archive
                       and mail_ok and data_ok, record)
            if whole:
                again = self.post("import: the same partial import again (the same identity)", "/api/v1/import/cpanel/apply",
                                  request, identity, 3600)
                later = self.imported(f"hostile-{kind}-after-replay", fixture)
                self.check("P4: the replay of a partial import is the stored answer byte for byte and imports nothing again",
                           again.get("status") == 200 and again.get("replayed") == "1"
                           and self.raw_digests.get(again["n"]) == self.raw_digests.get(answer["n"]) and later["fp"] == state["fp"],
                           {"replay": brief(again), "unchanged": later["fp"] == state["fp"]})

    # -- C8: restore ----------------------------------------------------------------------------------------------

    def site_state(self, label: str) -> dict:
        database = (self.state.get("site_databases") or {}).get("mariadb", {}).get("name")
        docroot = self.snap2(label + "-docroot", "read-docroot", domain_id=self.state["domain_id"])
        engines = self.engines(label + "-rows", (database,) if database else ())
        table = engines["seed_tables"].get(database or "", {})
        backups = self.backup_effect(label + "-backups")
        return {"docroot": docroot["docroot"].get("sha256"), "files": docroot["docroot"].get("files"),
                "rows": [table.get("rows"), table.get("sum_id")], "archives": backups["fp"]["archives"],
                "origins": backups["fp"]["origins"], "partial_files": backups["fp"]["partial_files"],
                "site_home_entries": docroot.get("site_home_entries"), "entries": backups["entries"]}

    def drift(self, label: str) -> dict:
        """The owner changes the site and its rows after the backup, so that a restore has something to put back."""
        database = (self.state.get("site_databases") or {}).get("mariadb", {}).get("name")
        self.owner2("change-site-" + label, "owner-change-site", domain_id=self.state["domain_id"], tag=label)
        if database:
            self.owner2("change-rows-" + label, "owner-seed-rows", database=database, action="drift")
        return self.site_state("drifted-" + label)

    @staticmethod
    def same_site(a: dict, b: dict) -> bool:
        return a["docroot"] == b["docroot"] and a["rows"] == b["rows"]

    def restore_marks(self, route: str, roman: str, text: str, reference: dict, before: dict, after: dict,
                      extra_archives: int, detail: dict | None = None) -> None:
        new = one_new(before["archives"], after["archives"])
        made = [e.get("manifest", {}).get("origin") for e in after["entries"] if e["name"] in new]
        ok = self.same_site(after, reference) and not self.same_site(before, reference) and len(new) == extra_archives \
            and all(origin == "pre_restore" for origin in made) and not after["partial_files"]
        self.judge(route, roman, text, ok, dict(detail or {}, site_is_the_backups=self.same_site(after, reference),
                                                 site_was_changed_before=not self.same_site(before, reference),
                                                 new_archives=new, new_archive_origins=made, partial_files=after["partial_files"],
                                                 docroot=[reference["docroot"], before["docroot"], after["docroot"]],
                                                 rows=[reference["rows"], before["rows"], after["rows"]]))

    def hold_until_restore_started(self, tree_before: list) -> Callable[[], dict]:
        def hold() -> dict:
            started = time.time()
            while time.time() - started < 60:
                now = self.native2("read-backups", domain_id=self.state["domain_id"])
                new = [line for line in now["tree"] if line not in tree_before]
                if now["partial_files"] or new:
                    return {"seen": "the Agent is writing in the domain's backup directory", "after_seconds": round(time.time() - started, 1),
                            "new_entries": [line.rsplit("/", 1)[-1] for line in new][:6]}
                time.sleep(0.2)
            return {"seen": None, "after_seconds": round(time.time() - started, 1)}
        return hold

    def c8_restore(self) -> None:
        route = "restore"
        backups = self.state.get("backups") or {}
        name, other = backups.get("reference"), backups.get("other")
        if not name or not other:
            self.not_reached(route, ("i", "ii", "iii", "iv", "v", "vi"), "the manual backups of C4 do not exist", backups)
            return
        path = f"/api/v1/domains/{self.state['domain_id']}/backups/restore"
        body, other_body = {"backup_name": name}, {"backup_name": other}
        reference = self.site_state("reference")
        self.current["reference"] = {k: reference[k] for k in ("docroot", "files", "rows", "archives")}
        self.note("the site as it is now is the site both manual backups hold: nothing changed it since C4", self.current["reference"])

        # (iii) without the header
        drifted = self.drift("iii")
        self.judge(route, "iii", "before the arrivals the owner's change is in place (the site is not the backup's)",
                   not self.same_site(drifted, reference), {"docroot": [reference["docroot"], drifted["docroot"]], "rows": [reference["rows"], drifted["rows"]]})
        a3 = self.post(f"{route} (iii) without the header", path, body, None)
        s3 = self.site_state("after-iii")
        self.judge(route, "iii", "without the header the answer is 428 REQUEST_ID_REQUIRED",
                   a3["status"] == 428 and answer_code(a3) == "REQUEST_ID_REQUIRED", brief(a3))
        self.judge(route, "iii", "and nothing was restored and no archive was written",
                   self.same_site(s3, drifted) and s3["archives"] == drifted["archives"],
                   {"archives": [len(drifted["archives"]), len(s3["archives"])]})

        # (i) three in a row; the owner changes the site again after the first answer, so a replay that ran would show
        identity = new_identity()
        self.identities[identity] = route + " (i)"
        first = self.post(f"{route} (i) sequential arrival 1 of 3", path, body, identity, 2700)
        s_first = self.site_state("after-i-first")
        self.restore_marks(route, "i", "the first arrival restored the site and wrote one pre-restore archive", reference, s3, s_first, 1,
                           {"answer": brief(first), "seconds": first.get("seconds")})
        self.current["restore_seconds"] = first.get("seconds")
        again = self.drift("i-after-first")
        later = [self.post(f"{route} (i) sequential arrival {n} of 3", path, body, identity, 2700) for n in (2, 3)]
        s_i = self.site_state("after-i")
        self.judge(route, "i", "the second and third arrival restored nothing: the owner's later change is still in place and "
                   "no archive was added", self.same_site(s_i, again) and s_i["archives"] == again["archives"],
                   {"docroot": [again["docroot"], s_i["docroot"]], "rows": [again["rows"], s_i["rows"]],
                    "archives": [len(again["archives"]), len(s_i["archives"])]})
        ok, detail = judge_sequential([first] + later, self.digests([first] + later), True)
        self.judge(route, "i", "the three answers are the same bytes, the later two marked as replays", ok, detail)

        # (iv) the same identity, another backup
        a4 = self.post(f"{route} (iv) the same identity with a different body", path, other_body, identity)
        s4 = self.site_state("after-iv")
        self.judge(route, "iv", "the same identity with a different body is refused 409 REQUEST_ID_REUSED",
                   a4["status"] == 409 and answer_code(a4) == "REQUEST_ID_REUSED", brief(a4))
        self.judge(route, "iv", "and there is no second effect", self.same_site(s4, s_i) and s4["archives"] == s_i["archives"],
                   {"archives": [len(s_i["archives"]), len(s4["archives"])]})

        # (ii) three at once
        second = new_identity()
        self.identities[second] = route + " (ii)"
        concurrent = self.post_many(f"{route} (ii) concurrent arrival", path, body, [second] * 3, 2700)
        s_ii = self.site_state("after-ii")
        self.restore_marks(route, "ii", "three arrivals at once of one identity restored the site once (one pre-restore archive)",
                           reference, s4, s_ii, 1, {"answers": [brief(a) for a in concurrent]})
        ok, detail = judge_concurrent(concurrent, self.digests(concurrent), True)
        self.judge(route, "ii", "the answers are the same bytes, or REQUEST_IN_PROGRESS for a waiter", ok, detail)

        # the Agent's lock: two different identities at once
        before_lock = self.drift("lock")
        pair = [new_identity(), new_identity()]
        for item in pair:
            self.identities[item] = route + " (two identities at once)"
        raced = self.post_many(f"{route} (lock) two different identities at once, arrival", path, body, pair, 2700)
        s_lock = self.site_state("after-lock")
        refused = [a for a in raced if a.get("status") == 409 and answer_code(a) == "BACKUP_RESTORE_IN_PROGRESS"]
        served = [a for a in raced if a.get("status") == 200]
        self.judge(route, "ii", "of two different identities at once one restore ran and the other was refused by the Agent's "
                   "lock (409 BACKUP_RESTORE_IN_PROGRESS)", len(refused) == 1 and len(served) == 1, [brief(a) for a in raced])
        self.restore_marks(route, "ii", "and the site was restored once (one pre-restore archive)", reference, before_lock, s_lock, 1)
        self.current["lock"] = {"answers": [brief(a) for a in raced],
                                "refusal": next((a.get("answer") for a in refused), None),
                                "texts": next((a.get("catalogue_texts") for a in refused), None)}

        # (v) the connection is dropped while the restore runs
        before_drop = self.drift("v")
        dropped_identity = new_identity()
        self.identities[dropped_identity] = route + " (v)"
        tree = self.native2("read-backups", domain_id=self.state["domain_id"])["tree"]
        dropped = self.post_and_drop(f"{route} (v) the connection is dropped while the restore runs", path, body, dropped_identity,
                                     self.hold_until_restore_started(tree))
        at_drop = self.row(dropped_identity)
        self.judge(route, "v", "the request was still running when the connection was dropped",
                   dropped["answer_bytes_received_before_the_drop"] == 0 and bool(at_drop) and at_drop["status"] == "running",
                   {"dropped": {k: dropped[k] for k in ("held", "dropped_after_seconds", "answer_bytes_received_before_the_drop")},
                    "row_at_drop": at_drop})
        waited = self.wait_row(dropped_identity, "restore-v")
        row = waited["row"] or {}
        s_v = self.site_state("after-v")
        self.restore_marks(route, "v", "the server finished the restore after the client was gone (one pre-restore archive)",
                           reference, before_drop, s_v, 1, {"row": row, "waited_seconds": waited["waited_seconds"]})
        self.judge(route, "v", "the row is done with the handler's answer kept", row.get("status") == "done"
                   and row.get("response_status") == 200 and row.get("response_retained") == 1, row)
        replay = self.post(f"{route} (v) the same identity again after the drop", path, body, dropped_identity)
        s_v2 = self.site_state("after-v-replay")
        self.judge(route, "v", "the replay gets the stored answer (the first run's bytes, marked as a replay) and restores nothing again",
                   replay.get("status") == 200 and replay.get("replayed") == "1"
                   and self.raw_digests.get(replay["n"]) == (row.get("body") or {}).get("sha256") and s_v2["archives"] == s_v["archives"],
                   {"replay": brief(replay), "stored_sha256": (row.get("body") or {}).get("sha256"),
                    "replay_sha256": self.raw_digests.get(replay["n"]), "archives": [len(s_v["archives"]), len(s_v2["archives"])]})

        # (vi) the Panel goes away while a restore runs: the owner's restart, then the process killed outright
        for how in ("restart", "kill"):
            self.panel_goes_away(how, route, path, body, reference)
        me = self.call("C8 the owner's session after the Panel came back", "GET", "/api/v1/auth/me")
        self.check("the owner's session is still valid after the Panel came back", me["status"] == 200, me["status"])

    def wait_panel_ready(self, label: str) -> dict:
        """Read-only wait until the Panel serves management requests again (after a start it answers them
        `503 PANEL_STARTING` for a while). The Backups panel's list is asked, as the page does."""
        started, last, seen = time.time(), None, []
        path = f"/api/v1/domains/{self.state['domain_id']}/backups"
        while time.time() - started < 900:
            self.tunnel.ensure()
            try:
                response = self.api("GET", path, purpose=f"Backups panel list while the Panel comes back ({label})")
                parsed = response.json()
                last = {"status": response.status, "code": parsed.get("code") if isinstance(parsed, dict) else None}
            except Exception as exc:  # noqa: BLE001 - the Panel is down or starting; that is what is being waited for
                last = {"no_answer": type(exc).__name__}
            if not seen or seen[-1]["answer"] != last:
                seen.append({"after_seconds": round(time.time() - started, 1), "answer": last})
            if last.get("status") == 200:
                break
            time.sleep(1)
        return {"ready_after_seconds": round(time.time() - started, 1), "answers_in_order": seen}

    def settle(self, label: str, reference: dict, before: dict) -> tuple:
        """What the Agent left: read until two readings in a row agree and nothing partial remains (bounded)."""
        observations, final = [], None
        deadline = time.time() + 900
        while time.time() < deadline:
            now = self.site_state(f"{label}-agent-{len(observations) + 1:02d}")
            observations.append({"at": base.utc_now(), "docroot": now["docroot"], "rows": now["rows"], "archives": len(now["archives"]),
                                 "partial_files": now["partial_files"], "site_home_entries": now["site_home_entries"],
                                 "classification": restore_classification(reference, before, now)})
            if final is not None and self.same_site(final, now) and final["archives"] == now["archives"] and not now["partial_files"] \
                    and final["site_home_entries"] == now["site_home_entries"]:
                return now, observations
            final = now
            time.sleep(6)
        return final, observations

    def panel_goes_away(self, how: str, route: str, path: str, body: dict, reference: dict) -> None:
        what = {"restart": "the owner restarts the Panel's service (systemctl restart)",
                "kill": "the Panel's process is killed outright (SIGKILL, as a crash or the OOM killer)"}[how]
        before = self.drift("vi-" + how)
        identity = self._own_identity(f"{route} (vi, {how})")
        units_before = self.snap2(f"vi-{how}-units-before", "read-units")
        clock = int(units_before["epoch"])
        tree = self.native2("read-backups", domain_id=self.state["domain_id"])["tree"]
        holder: dict = {}

        def client() -> None:
            holder["answer"] = self.post(f"{route} (vi, {how}) the restore that is running when the Panel goes away", path, body,
                                         identity, 1800)
        thread = threading.Thread(target=client, daemon=True)
        thread.start()
        held = self.hold_until_restore_started(tree)()
        at_fault = self.row(identity)
        if how == "restart":
            fault = self.owner2("restart-panel", "owner-restart-panel")
        else:
            fault = self.snap2("lab-kill-panel", "lab-kill-panel")
            self.current.setdefault("lab_faults", []).append({"what": what, "at": fault.get("at")})
        thread.join(1900)
        seen = holder.get("answer") or {}
        ready = self.wait_panel_ready(how)
        row_after = self.row(identity)
        units_after = self.snap2(f"vi-{how}-units-after", "read-units")
        pids = {name.split(".")[0].split("-")[1]: [units_before["units"][name].get("MainPID"), units_after["units"][name].get("MainPID")]
                for name in ("celikpanel-panel.service", "celikpanel-agent.service")}
        record = {"what": what, "client_saw": brief(seen) or "nothing", "held": held, "row_when_the_panel_went_away": at_fault,
                  "panel_main_pid": pids["panel"], "agent_main_pid": pids["agent"], "agent_kept_its_process": pids["agent"][0] == pids["agent"][1],
                  "systemctl": {k: fault.get(k) for k in ("restart_took_seconds", "unit_policy")}, "returncode": fault["result"].get("returncode"),
                  "panel_ready_again": ready, "row_afterwards": row_after}
        self.current.setdefault("panel_goes_away", {})[how] = record
        self.judge(route, "vi", f"{how}: the restore was running when the Panel went away (row running; the Agent at work; a new Panel process afterwards)",
                   bool(held.get("seen")) and bool(at_fault) and at_fault["status"] == "running" and pids["panel"][0] != pids["panel"][1],
                   {k: record[k] for k in ("held", "row_when_the_panel_went_away", "panel_main_pid", "agent_main_pid", "client_saw")})
        final, observations = self.settle("vi-" + how, reference, before)
        new = one_new(before["archives"], final["archives"])
        record.update(what_the_agent_left=restore_classification(reference, before, final),
                      new_archives=[{"name": e["name"], "origin": e.get("manifest", {}).get("origin"), "bytes": e["bytes"]}
                                    for e in final["entries"] if e["name"] in new],
                      partial_files=final["partial_files"], site_home_entries=final["site_home_entries"],
                      backup_directory_entries_that_are_not_archives=[e["name"] for e in final["entries"]
                                                                      if not e["name"].endswith(".cpbak") or e["name"].startswith(".")],
                      observations=observations)
        journal = self.snap2(f"vi-{how}-journal", "read-journal", units=["celikpanel-agent.service", "celikpanel-panel.service"],
                             since_epoch=clock, lines=600)["journal"].get("stdout", "")
        self.keep_text(f"native-text/journal-panel-{how}.txt", journal or "(no lines)")
        replay = self.post(f"{route} (vi, {how}) the same identity again after the Panel came back", path, body, identity)
        after_replay = self.site_state(f"vi-{how}-after-replay")
        record.update(replay=brief(replay), replay_answer=replay.get("answer"), replay_texts=replay.get("catalogue_texts"))
        status = (row_after or {}).get("status")
        self.judge(route, "vi", f"{how}: what the Agent left was read (recorded; the state itself is the observation)", True,
                   {k: record[k] for k in ("what_the_agent_left", "new_archives", "partial_files", "agent_kept_its_process",
                                           "site_home_entries", "row_afterwards", "client_saw")})
        if status == "interrupted":
            self.judge(route, "vi", f"{how}: the row is `interrupted` and the replay answers 409 REQUEST_OUTCOME_UNKNOWN, starting nothing",
                       replay.get("status") == 409 and answer_code(replay) == "REQUEST_OUTCOME_UNKNOWN"
                       and self.same_site(after_replay, final) and after_replay["archives"] == final["archives"], brief(replay))
        elif status == "done":
            self.judge(route, "vi", f"{how}: the row is `done`: the request finished before the Panel stopped, the site is the "
                       "backup's, and the replay is the stored answer",
                       self.same_site(final, reference) and len(new) == 1 and replay.get("status") == row_after.get("response_status")
                       and replay.get("replayed") == "1" and after_replay["archives"] == final["archives"],
                       {"client_saw": brief(seen), "replay": brief(replay), "site_is_the_backups": self.same_site(final, reference),
                        "new_archives": record["new_archives"]})
        else:
            self.judge(route, "vi", f"{how}: the row is `done` or `interrupted` once the Panel is back", False, row_after)
        if how == "kill":
            self.judge(route, "vi", "kill: the row of a request whose Panel process was killed is `interrupted`", status == "interrupted", row_after)
        self.note(f"(vi, {how}) {what}: the client saw {record['client_saw']}; the row is `{status}`; the Agent left: "
                  f"{record['what_the_agent_left']}; the Panel served management requests again after "
                  f"{ready['ready_after_seconds']} s", record["panel_ready_again"])

    # -- C9: the rows -------------------------------------------------------------------------------------------------

    def c9_identities(self) -> None:
        needles = [base64.b64encode(n.encode()).decode() for n in sorted(self.needles)]
        table = self.snap2("identities-at-end", "read-identities", needles_b64=needles)
        rows = table.get("rows") or []
        self.current["rows"] = {"count": table.get("count"), "states": table.get("states"),
                                "sensitive_values_compared": table.get("needles_compared"),
                                "rows_holding_a_sensitive_value_of_this_run": table.get("rows_holding_a_sensitive_value_of_this_run"),
                                "by_route": {}}
        for row in rows:
            entry = self.current["rows"]["by_route"].setdefault(row["route"], {"rows": 0, "states": {}, "retained": 0, "status_only": 0})
            entry["rows"] += 1
            entry["states"][row["status"]] = entry["states"].get(row["status"], 0) + 1
            entry["retained" if row["response_retained"] else "status_only"] += 1
        known = set(self.identities)
        self.judge("rows", "vii", "every identity this driver used has exactly one row, and there is no other row",
                   {row["id"] for row in rows} == known and len(rows) == len(known),
                   {"rows": len(rows), "identities_used": len(known), "unknown_rows": sorted({r["id"] for r in rows} - known)[:5],
                    "missing_rows": sorted(known - {r["id"] for r in rows})[:5]})
        self.judge("rows", "vii", "no stored answer holds a secret this run was shown (VPN keys, minted and stored passwords)",
                   table.get("needles_compared", 0) > 0 and not table.get("rows_holding_a_sensitive_value_of_this_run"),
                   {"sensitive_values_compared": table.get("needles_compared"),
                    "rows_holding_one": table.get("rows_holding_a_sensitive_value_of_this_run")})
        secret_routes = ("/api/v1/database-servers/{id}/admin-account", "/api/v1/vpn/peers")
        kept = [r["id"] for r in rows if r["route"] in secret_routes and (r["response_retained"] or r["body"].get("stored") and r["body"].get("bytes"))]
        self.judge("rows", "vii", "the engine-account and VPN peer rows keep the status only (no stored body)", not kept, kept)
        words = {r["id"]: r["body"]["sensitive_words_found"] for r in rows if r["body"].get("sensitive_words_found")}
        self.judge("rows", "vii", "no stored answer contains a private key, a client configuration, a delivery token or a password field",
                   not words, words)
        # set3 (S1): no stored row and no answer of a guarded route holds a hash-shaped value; no answer echoed a
        # secret the request sent (O10).
        self.judge("rows", "vii", "no request_identities row holds a hash-shaped value (stored answer and every other column)",
                   table.get("rows_holding_a_hash_shaped_value") == [] and len(rows) > 0,
                   {"rows": len(rows), "rows_holding_one": table.get("rows_holding_a_hash_shaped_value")})
        shaped = [a for a in self.answer_shapes if a["hash_shaped"]]
        echoed = [a for a in self.answer_shapes if a["sent_secret_echoed"]]
        self.current["answers_searched"] = {"answers": len(self.answer_shapes), "holding_a_hash_shaped_value": shaped,
                                            "echoing_a_sent_secret": echoed}
        self.judge("rows", "vii", "no answer of a guarded route held a hash-shaped value, and none echoed a secret its request sent",
                   bool(self.answer_shapes) and not shaped and not echoed, self.current["answers_searched"])
        start = self.state.get("identity_log_since")
        if start:
            log = self.native2("read-journal", units=["celikpanel-panel.service", "celikpanel-agent.service"],
                               since_epoch=start, lines=3000)["journal"].get("stdout", "")
            self.current["journal_searched"] = {"lines": len(log.splitlines()), "hash_shaped_values": base.hash_shaped_count(log)}
            self.judge("rows", "vii", "the Panel's and the Agent's journal lines of this cell hold no hash-shaped value",
                       self.current["journal_searched"]["hash_shaped_values"] == 0 and bool(log), self.current["journal_searched"])
        self.judge("rows", "vii", "every row lives 24 hours and none is still `running`",
                   all(r["lifetime_seconds"] == 86400 for r in rows) and "running" not in (table.get("states") or {}),
                   {"states": table.get("states"), "lifetimes": sorted({r["lifetime_seconds"] for r in rows})})
        cut = sorted(str(self.identities.get(r["id"])) for r in rows if r["status"] == "interrupted")
        self.judge("rows", "vii", "the interrupted rows are restores of arrival (vi) only, the killed one among them",
                   "restore (vi, kill)" in cut and all(item.startswith("restore (vi") for item in cut),
                   [self.identities.get(r["id"]) for r in rows if r["status"] != "done"])
        self.current["rows"]["table"] = [{"used_for": self.identities.get(r["id"]), "route": r["route"], "status": r["status"],
                                          "response_status": r["response_status"], "retained": r["response_retained"],
                                          "stored_bytes": r["body"].get("bytes"), "stored_keys": r["body"].get("json_keys")} for r in rows]

    # -- collect and result -----------------------------------------------------------------------------------------

    def execute(self) -> dict:
        self.step("preflight", self.preflight)
        self.step("origin", self.origin, needs=("preflight",))
        self.step("baseline-install", self.baseline_install, needs=("origin",))
        self.step("owner-login", self.owner_login, needs=("baseline-install",))
        self.step("license", self.license, needs=("owner-login",))
        self.step("setup", self.setup, needs=("license",))
        self.step("site", self.site, needs=("setup",))
        functions = {"C0-prepare": self.c0_prepare, "C1-domain-databases": self.c1_domain_databases,
                     "C2-admin-account": self.c2_admin_account, "C3-server-databases": self.c3_server_databases,
                     "C4-backup": self.c4_backup, "C5-letsencrypt": self.c5_letsencrypt, "C6-vpn-peer": self.c6_vpn_peer,
                     "C7-import": self.c7_import, "C8-restore": self.c8_restore, "C9-identities": self.c9_identities,
                     "C6b-php-site": self.c6b_php_site, "C7b-import-answers": self.c7b_import_answers}
        for key, title in SECTIONS:
            self.section(key, title, functions[key], needs=("site",))
            self.sections.setdefault(key, {"title": title, "verdict": "not-run", "reason": "the site step did not pass"})
        self.step("collect", self.collect)
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        result = {"schema": base.RESULT_SCHEMA, "cell_kind": CELL_KIND, "native_evidence": False,
                  "cell": dataclasses.asdict(self.settings),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": None, "provenance": base.provenance_for("good"),
                  "artifacts": {"baseline": {k: self.artifacts["baseline"][k] for k in ("version", "commit", "sha256")}},
                  "outcome": {"classification": "request-identity-measured", "final_status": None},
                  "setup": {"purpose": self.settings.purpose, "components": sorted(self.settings.components),
                            "waiting": self.state.get("setup_waiting")},
                  "site": {k: self.state.get(k) for k in ("domain_id", "site_user", "mailbox", "subscription_id", "database_servers",
                                                           "site_databases", "backups")},
                  "matrix": {route: self.matrix.get(route, {}) for route in [key for key, _ in ROUTES] + ["rows"]},
                  "routes": dict(ROUTES), "times": self.times, "identities_used": len(self.identities),
                  "sections": self.sections, "findings": self.state["findings"],
                  "owner_placed_the_nginx_php_snippet": os.environ.get(OWNER_SNIPPET_ENV) == "1",
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")} for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "request-identity: observations for the owner's review; no update is started and no P0 row is judged."}
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("plan", "run"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--cell", required=True, choices=sorted(CELLS))
        cmd.add_argument("--artifacts", required=True, type=Path)
        cmd.add_argument("--work-root", required=True)
        cmd.add_argument("--local-port", type=int, default=18443)
        if name == "plan":
            cmd.add_argument("--dry-run", action="store_true", help="validate the plan without any guest")
        else:
            cmd.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    settings = CELLS[args.cell]
    base.validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    document = json.loads(args.artifacts.read_text())
    base.configure_labels(document)
    if args.command == "plan":
        base.validate_cell_artifacts(document, settings.cell, check_files=not args.dry_run)
        print(json.dumps(build_plan(settings, document, args.work_root, args.local_port), indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    base.validate_cell_artifacts(document, settings.cell)
    result = RequestIdentityTrial(settings, document, args.work_root, args.local_port).execute()
    print(json.dumps({"overall": result["overall"], "cell_kind": CELL_KIND, "matrix": result["matrix"],
                      "sections": {k: v.get("verdict") for k, v in result["sections"].items()}}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
