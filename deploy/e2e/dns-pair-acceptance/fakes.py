"""Test doubles for the offline sequencing tests (never used by a real run).

``World`` is the shared DNS truth both fake guests serve; ``FakePanel`` is a
small state machine speaking the Panel API subset the driver uses, including
its CSRF (Origin) and session checks; ``FakeGuests`` answers the probe
commands from the same world and records every guest command.

The DNS engine card bodies are real captures (copied, minimal, redacted; the
evidence tree is never read at test time):

* ``api_secondary_ready=True`` (default in the sequence tests): the pair2
  build ``916e1577`` (``fixtures/pair2/``). The secondary carries
  ``secondary_ready: true`` and ``pair_ready: false``; the primary carries
  ``pair_ready: true`` and no ``secondary_ready`` key (product contract).
* ``api_secondary_ready=False``: the pair1 build ``aa6b9380``
  (``fixtures/pair1/``), which serializes no ``secondary_ready`` anywhere.

An execution-script entry may carry ``"_license_state"``: when that entry is
served, the Panel's license status reports that state from then on (the key
itself is not returned).
"""

from __future__ import annotations

import base64
import contextlib
import hashlib
import json
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlsplit

from panel_api import PanelClient, Response

COOKIE = "fake-session-cookie-value-0123456789"
FIXTURE_KEY = "CPK-acce57f1c7" + "0" * 54
FIXTURE_LABEL = "ACCEPTANCE FIXTURE \u2014 NOT FOR PRODUCTION"
FIXTURE_ROOT = Path(__file__).resolve().parent / "fixtures"
FIXTURES = FIXTURE_ROOT / "pair1"
FIXTURES_PAIR2 = FIXTURE_ROOT / "pair2"
CAPTURED_ENGINE = {
    ("primary", "bind"): "engine-bind-primary-paired.json",
    ("secondary", "bind"): "engine-bind-secondary-paired.json",
    ("secondary", "pdns"): "engine-pdns-secondary-paired.json",
}
PDNS_CATALOG_ACCOUNT = "celikpanel-peer-catalog-v1"
HOST_PUB = "ssh-ed25519 " + base64.b64encode(
    bytes.fromhex("0000000b7373682d6564323535313900000020") + bytes(range(32))).decode()


def captured(name: str, run: str = "pair1") -> dict[str, Any]:
    """One captured exchange (``fixtures/<run>/<name>``)."""

    return json.loads((FIXTURE_ROOT / run / name).read_text(encoding="utf-8"))


def captured_engine(role: str, engine: str, run: str = "pair2") -> dict[str, Any]:
    """The real ``GET /api/v1/dns/engine`` body of that run for a paired role and engine.

    Neither run showed a ready paired PowerDNS primary (pair1: refused by the
    server plan; pair2 t3: failed inside the product), so that one is the
    captured BIND primary with the engine entries and the operation target
    swapped; every other body is returned exactly as captured.
    """

    name = CAPTURED_ENGINE.get((role, engine))
    if name:
        return captured(name, run)["body"]
    if (role, engine) != ("primary", "pdns"):
        raise ValueError(f"no captured engine body for {role}/{engine}")
    body = captured(CAPTURED_ENGINE[("primary", "bind")], run)["body"]
    body["active_engine"] = "pdns"
    body["operation"]["target_engine"] = "pdns"
    for entry in body["engines"]:
        active = entry["id"] == "pdns"
        entry.update({"installed": active, "managed": active, "running": active,
                      "status": "active" if active else "available"})
    return body


def json_response(status: int, body: Any, headers: list | None = None) -> Response:
    return Response(status, (headers or []) + [("Content-Type", "application/json")],
                    b"" if body is None else json.dumps(body).encode())


@dataclass
class World:
    zones: dict[str, dict[str, Any]] = field(default_factory=dict)
    secondary_holds: set[str] = field(default_factory=set)
    ledger: dict[str, int] = field(default_factory=dict)
    management: dict[str, bool] = field(default_factory=dict)
    boot: dict[str, int] = field(default_factory=dict)
    enrolled: bool = False
    catalog_members: set[str] = field(default_factory=set)
    mutate_ledger_on_return: bool = False

    def bump(self, node: str) -> None:
        self.ledger[node] = self.ledger.get(node, 0) + 1


class FakePanel:
    def __init__(self, node: str, role: str, engine: str, world: World, *, licensed: bool = True,
                 plan_can_start: bool = True, execution_script: list[dict] | None = None,
                 delete_script: list[tuple[int, dict]] | None = None, zone: str = "pair-accept.example",
                 peer_node: str = "", acceptance_cell: str | None = None, acceptance_label: bool = True,
                 pdns_primary_gate_open: bool = False, refused_plan_side_effect: bool = False,
                 api_secondary_ready: bool = False, engine_script: list[dict] | None = None,
                 plan_response: tuple[int, Any] | None = None, start_response: tuple[int, Any] | None = None) -> None:
        self.node, self.role, self.engine, self.world = node, role, engine, world
        # False: exactly the pair1 API (no secondary_ready). True: the pair2 API (on the secondary only).
        self.api_secondary_ready = api_secondary_ready
        self.license_state_override: str | None = None
        # Explicit engine bodies per poll (after setup start); the last one repeats.
        self.engine_script = engine_script
        self.engine_polls = 0
        self.plan_response = plan_response
        self.start_response = start_response
        self.licensed = licensed
        # acceptance_cell: the panel is the acceptance_license test build on a
        # guest whose marker names this cell (internal/licensing/acceptance_fixture.go).
        self.acceptance_cell = acceptance_cell
        self.acceptance_label = acceptance_label
        self.activations: list[str] = []
        # 6f2fb028: the server plan refuses a paired PowerDNS primary while the gate is closed.
        self.pdns_primary_gate_open = pdns_primary_gate_open
        self.refused_plan_side_effect = refused_plan_side_effect
        self.plan_can_start = plan_can_start
        self.execution_script = execution_script
        self.delete_script = list(delete_script or [])
        self.zone = zone
        self.peer_node = peer_node
        self.calls: list[tuple[str, str]] = []
        self.unsafe_calls: list[tuple[str, str]] = []
        self.revision = 1
        self.guidance = ""
        self.status = "new"
        self.draft: dict[str, Any] = {}
        self.started: dict[str, Any] | None = None
        self.start_count = 0
        self.polls = 0
        self.domains: dict[int, dict[str, Any]] = {}
        self.next_id = 10
        self.records: dict[int, dict[int, dict[str, Any]]] = {}
        self.next_record = 100
        self.pending_delete: int | None = None

    # -- transport ---------------------------------------------------------------

    def __call__(self, method: str, path: str, headers: dict[str, str], body: bytes | None, timeout: float) -> Response:
        self.calls.append((method, path))
        if method in {"POST", "PUT", "DELETE", "PATCH"}:
            self.unsafe_calls.append((method, path))
            if headers.get("Origin") != "https://" + headers.get("Host", ""):
                return json_response(403, {"error": "cross-origin request blocked"})
        payload = json.loads(body) if body else None
        parts = urlsplit(path)
        route, query = parts.path, parse_qs(parts.query)
        if route == "/api/v1/auth/login":
            return json_response(200, {"username": "owner", "role": "admin"},
                                 [("Set-Cookie", f"celikpanel_session={COOKIE}; Path=/; HttpOnly")])
        if headers.get("Cookie") != f"celikpanel_session={COOKIE}":
            return json_response(401, {"error": "authentication required", "code": "AUTH_REQUIRED"})
        if route == "/api/v1/auth/me":
            return json_response(200, {"username": "owner", "role": "admin", "effective_role": "admin"})
        if route == "/api/v1/panel/availability":
            return json_response(200, {"schema": "celikpanel-panel-availability/v1", "state": "available"})
        if route == "/api/v1/license/access":
            return json_response(200, {"can_use_panel": self.licensed, "state": "active" if self.licensed else "missing",
                                       "observation": "known", "valid_until": 0})
        if route == "/api/v1/panel/license" and self.acceptance_cell is not None:
            if method == "POST":
                self.activations.append(payload.get("key", ""))
                if payload.get("key") != FIXTURE_KEY:
                    return json_response(400, {"error": "this panel is an acceptance test build: it accepts only the "
                                                        "acceptance fixture key", "code": "license_action_failed"})
                self.licensed = True
            return json_response(200, self.license_status())
        if route == "/api/v1/panel/license" and method == "POST":
            self.licensed = True
            return json_response(200, {"state": "active"})
        if not self.licensed:
            return json_response(403, {"error": "Panel access requires an active server license. Existing services "
                                                "and scheduled tasks keep running. The server administrator must "
                                                "activate or renew the license.", "code": "license_required"})
        return self.route(method, route, query, payload)

    def license_status(self) -> dict[str, Any]:
        status: dict[str, Any] = {"state": "active" if self.licensed else "missing", "observation": "known",
                                  "can_provision": self.licensed}
        if self.licensed and self.license_state_override:
            status.update({"state": self.license_state_override, "can_provision": False,
                           "observation": "unknown"})
        if self.licensed:
            status.update({"product": "celikpanel-acceptance-fixture", "license_id": "acceptance-fixture",
                           "expires_at": 1790000000, "offline_until": 1789000060})
        if self.acceptance_label:
            status.update({"license_kind": "acceptance_fixture", "license_label": FIXTURE_LABEL,
                           "license_service": "not contacted: acceptance test build", "acceptance_guest": "verified",
                           "acceptance_cell": self.acceptance_cell, "acceptance_node": self.node,
                           "acceptance_smbios_uuid": "not readable by the panel service user"})
        return status

    def state(self) -> dict[str, Any]:
        return {"version": 1, "revision": self.revision, "origin": "fresh", "status": self.status,
                "draft": self.draft, "required": True, "guidance": self.guidance, "checks": []}

    def execution(self) -> dict[str, Any]:
        script = self.execution_script or default_execution_script(self.role)
        index = min(self.polls, len(script) - 1)
        self.polls += 1
        value = json.loads(json.dumps(script[index]))
        if "_license_state" in value:
            self.license_state_override = value.pop("_license_state")
        value.update({"id": "exec-" + self.node, "request_id": self.started["request_id"], "plan_id": "plan-" + self.node})
        return value

    def engine_body(self) -> dict[str, Any]:
        if self.started is None:
            return captured("engine-unconfigured.json")["body"]
        if self.engine_script is not None:
            body = json.loads(json.dumps(self.engine_script[min(self.engine_polls, len(self.engine_script) - 1)]))
            self.engine_polls += 1
            return body
        body = captured_engine(self.role, self.engine, "pair2" if self.api_secondary_ready else "pair1")
        body["zone_count"] = len(self.domains)
        return body

    def route(self, method: str, route: str, query: dict, payload: Any) -> Response:
        if route == "/api/v1/setup" and method == "GET":
            return json_response(200, self.state())
        if route == "/api/v1/setup/guidance" and method == "PUT":
            self.guidance = payload["guidance"]
            self.revision += 1
            return json_response(200, self.state())
        if route == "/api/v1/setup" and method == "PUT":
            if payload["revision"] != self.revision:
                return json_response(409, {"error": "setup changed", "code": "setup_conflict"})
            self.draft = payload["draft"]
            self.revision += 1
            self.status = "draft"
            return json_response(200, self.state())
        if route == "/api/v1/setup/plan" and method == "POST" and self.plan_response is not None:
            return json_response(*self.plan_response)
        if route == "/api/v1/setup/start" and method == "POST" and self.start_response is not None:
            self.start_count += 1
            return json_response(*self.start_response)
        if route == "/api/v1/setup/plan" and method == "POST":
            blockers = []
            if self.draft.get("dns_engine") == "pdns" and self.draft.get("dns_role") == "primary" \
                    and self.draft.get("dns_mode") == "local" and not self.pdns_primary_gate_open:
                blockers.append("pdns_primary_switch_paused")
                if self.refused_plan_side_effect:  # a product defect the driver must catch
                    self.revision += 1
                    self.world.bump(self.node)
            return json_response(200, {"id": "plan-" + self.node, "can_start": self.plan_can_start and not blockers,
                                       "blockers": blockers, "purpose": "dns",
                                       "steps": [{"id": "01-dns", "kind": "dns", "target": "local"}]})
        if route == "/api/v1/setup/start" and method == "POST":
            self.start_count += 1
            if self.started and self.started["request_id"] != payload["request_id"]:
                return json_response(409, {"error": "busy", "code": "server_setup_busy"})
            self.started = payload
            self.world.bump(self.node)
            self.status = "running"
            return json_response(202, {"id": "exec-" + self.node, "request_id": payload["request_id"], "status": "running"})
        if route == "/api/v1/setup/operation":
            if not self.started or query.get("request_id", [""])[0] != self.started["request_id"]:
                return json_response(200, None)
            return json_response(200, self.execution())
        if route == "/api/v1/dns/engine":
            return json_response(200, self.engine_body())
        if route == "/api/v1/domains/create" and method == "POST":
            domain_id = self.next_id
            self.next_id += 1
            self.domains[domain_id] = {"id": domain_id, "domain_name": payload["domain"], "status": "active"}
            self.records[domain_id] = {}
            self.world.zones[payload["domain"]] = {"serial": 2026092901, "records": {}}
            self.world.secondary_holds.add(payload["domain"])
            self.world.catalog_members.add(payload["domain"])
            self.world.bump(self.node)
            return json_response(200, {"DomainID": domain_id, "Domain": payload["domain"], "SiteID": 0})
        if route == "/api/v1/domains" and method == "GET":
            return json_response(200, list(self.domains.values()))
        parts = route.split("/")
        if len(parts) >= 5 and parts[3] == "domains" and parts[4].isdigit():
            return self.domain_route(method, int(parts[4]), "/".join(parts[5:]), query, payload)
        return json_response(404, {"error": "not found"})

    def domain_route(self, method: str, domain_id: int, rest: str, query: dict, payload: Any) -> Response:
        domain = self.domains.get(domain_id)
        if rest == "deletion-status":
            if domain is None:
                return json_response(404, {"error": "domain not found"})
            if self.pending_delete == domain_id:
                return json_response(200, {"status": "deletion_pending", "stage": "dns_cleanup",
                                           "reason": "dns_peer_enrollment_required", "message": "pending"})
            return Response(204, [], b"")
        if domain is None:
            return json_response(404, {"error": "domain not found"})
        zone = domain["domain_name"]
        if rest == "" and method == "DELETE":
            status, body = self.delete_script.pop(0) if self.delete_script else (200, {"status": "deleted", "domain": zone})
            if status == 202:
                self.pending_delete = domain_id
                self.world.zones.pop(zone, None)  # removed on the primary; the secondary still holds it
                self.world.catalog_members.discard(zone)
                self.world.bump(self.node)
                return json_response(202, body)
            if self.pending_delete == domain_id and not self.world.enrolled:
                return json_response(202, {"status": "deletion_pending", "domain": zone, "stage": "dns_cleanup",
                                           "reason": "dns_peer_enrollment_required", "message": "pending"})
            self.pending_delete = None
            self.domains.pop(domain_id)
            self.world.zones.pop(zone, None)
            self.world.secondary_holds.discard(zone)
            self.world.catalog_members.discard(zone)
            self.world.bump(self.node)
            return json_response(200, body)
        if rest == "dns/zone":
            return json_response(200, {"id": domain_id, "name": zone, "type": "NATIVE"})
        if rest == "dns/records":
            entries = self.records[domain_id]
            if method == "GET":
                return json_response(200, {"records": list(entries.values())})
            if method == "POST":
                record_id = self.next_record
                self.next_record += 1
                name = payload["name"] + "." + zone
                entries[record_id] = {"id": record_id, "domain_id": domain_id, "name": name, "type": payload["type"],
                                      "content": payload["content"], "ttl": payload["ttl"], "disabled": False}
            elif method == "DELETE":
                entries.pop(int(query["id"][0]))
            elif method == "PUT":
                entries[payload["id"]]["content"] = payload["content"]
            self.world.zones[zone]["serial"] += 1
            self.world.zones[zone]["records"] = {}
            for entry in entries.values():
                self.world.zones[zone]["records"].setdefault((entry["name"], entry["type"]), set()).add(entry["content"])
            self.world.bump(self.node)
            return json_response(200, {"success": True})
        return json_response(404, {"error": "not found"})


def default_execution_script(role: str) -> list[dict]:
    ctx = {"dns_mode": "local", "dns_role": role, "local_nameserver": "ns1.x", "local_ip": "192.0.2.11",
           "peer_nameserver": "ns2.x", "peer_ip": "192.0.2.10", "panel_domain": "panel.x",
           "dns_hosting_management": ""}
    running = {"status": "running", "phase": "01-dns", "context": ctx,
               "steps": [{"id": "01-dns", "kind": "dns", "status": "running"}]}
    dns_done = {"status": "running", "phase": "02-firewall", "context": ctx,
                "steps": [{"id": "01-dns", "kind": "dns", "status": "succeeded"},
                          {"id": "02-firewall", "kind": "firewall", "status": "running"}]}
    waiting = {"status": "waiting", "phase": "access_dns", "context": ctx,
               "error": {"code": "server_setup_access_dns_required", "message": "public DNS"},
               "steps": [{"id": "01-dns", "kind": "dns", "status": "succeeded"},
                         {"id": "02-firewall", "kind": "firewall", "status": "succeeded"},
                         {"id": "03-access_dns", "kind": "access_dns", "status": "running",
                          "target": "panel.x", "qualifier": "192.0.2.11"}]}
    return [running, dns_done, waiting]


class FakeGuests:
    def __init__(self, world: World, topology: Any, *, installer_stdout: dict[str, str] | None = None,
                 journals: dict[tuple[str, str], str] | None = None) -> None:
        self.world = world
        self.topology = topology
        self.commands: list[tuple[str, str, bool]] = []
        self.plan = None
        self.installer_stdout = installer_stdout or {}
        self.journals = journals or {}
        for role in topology.roles:
            world.management.setdefault(role.node, True)
            world.boot.setdefault(role.node, 1)
            world.ledger.setdefault(role.node, 0)

    def verify(self, node: str) -> dict[str, str]:
        return {"product_uuid": "00000000-0000-5000-8000-00000000000" + ("1" if node == "arch" else "2"),
                "schema": "celikpanel/dns-kill-fixture-plan/v1", "cell_id": "pair-accept__x__y", "node": node,
                "boot_id": f"{self.world.boot[node]:08d}-0000-0000-0000-000000000000"}

    def run(self, node: str, command: str, *, mutating: bool, stdin: bytes | None = None,
            timeout: float = 120, check: bool = True) -> subprocess.CompletedProcess:
        self.commands.append((node, command, mutating))
        stdout = b"ok\n"
        if "primary-prepare" in command:
            stdout = json.dumps({"state": "prepared", "credential_id": "0123456789abcdef0123456789abcdef",
                                 "public_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPrimaryKeyForTests",
                                 "public_key_sha256": "0" * 64}).encode()
        elif "secondary-host-key" in command:
            digest = hashlib.sha256(base64.b64decode(HOST_PUB.split()[1])).hexdigest()
            stdout = json.dumps({"host_key_sha256": digest}).encode()
        elif command.startswith("cat /etc/ssh/ssh_host_ed25519_key.pub"):
            stdout = (HOST_PUB + " root@guest\n").encode()
        elif "exec /bin/bash ./install.sh" in command:
            stdout = self.installer_stdout.get(node, "CelikPanel installed\n").encode()
        elif "primary-activate" in command:
            self.world.enrolled = True
            stdout = json.dumps({"state": "configured", "enrollment_id": "e" * 32}).encode()
        return subprocess.CompletedProcess([command], 0, stdout, b"")

    def journal(self, node: str, unit: str, *, boots: str = "all") -> str:
        return self.journals.get((node, unit), f"{node} {unit} journal line\n")

    def disable_management(self, node: str) -> None:
        self.commands.append((node, "disable-management", True))
        self.world.management[node] = False

    def enable_management(self, node: str) -> None:
        self.commands.append((node, "enable-management", True))
        self.world.management[node] = True
        if self.world.mutate_ledger_on_return:
            self.world.bump(node)

    def reboot(self, node: str, timeout: int = 900) -> dict[str, Any]:
        self.commands.append((node, "reboot", True))
        before = self.world.boot[node]
        self.world.boot[node] += 1
        return {"node": node, "boot_id_before": str(before), "boot_id_after": str(self.world.boot[node])}

    def serves(self, server: str, zone: str) -> bool:
        if server == self.topology.primary.address:
            return zone in self.world.zones
        return zone in self.world.secondary_holds

    def probe(self, node: str, command: str, *args: str) -> dict[str, Any]:
        options: dict[str, list[str]] = {}
        key = None
        for arg in args:
            if arg.startswith("--"):
                key = arg[2:]
                options.setdefault(key, [])
            elif key:
                options[key].append(arg)
        if command == "tls":
            return {"leaf_sha256": "a" * 64}
        if command == "versions":
            return {"command": "versions", "packages": {}, "node": node}
        if command == "ledger":
            return {"command": "ledger", "present": True,
                    "sha256": hashlib.sha256(str(self.world.ledger[node]).encode()).hexdigest()}
        if command == "management":
            enabled = self.world.management[node]
            role = self.topology.role_for_node(node)
            dns_unit = "pdns.service" if role.engine == "pdns" else "named.service"
            return {"command": "management", "units": {
                "celikpanel-panel.service": {"active": "active" if enabled else "inactive"},
                "celikpanel-agent.service": {"active": "active" if enabled else "inactive"},
                dns_unit: {"active": "active"}}}
        if command == "native":
            zone = options["zone"][0]
            role = self.topology.role_for_node(node)
            holds = zone in self.world.zones if role.role == "primary" else zone in self.world.secondary_holds
            value = {"command": "native", "engine": options["engine"][0], "zone": zone,
                     "native_state": "present" if holds else "absent", "zone_files": []}
            if options["engine"][0] == "pdns" and options.get("catalog"):
                # guest_probe pdns_rows: the catalog row with its account column (read-only).
                kind = "CONSUMER" if role.role == "secondary" else "PRODUCER"
                value["database"] = {"read": True, "domains": [
                    {"name": options["catalog"][0], "type": kind, "master": "", "catalog": "",
                     "options_present": False, "account": PDNS_CATALOG_ACCOUNT}]}
            return value
        if command == "catalog":
            return {"command": "catalog", "transferred": True, "serial": 7,
                    "members": sorted(z + "." for z in self.world.catalog_members)}
        if command == "dns":
            server = options["server"][0]
            answers = []
            for item in options["query"]:
                name, _, qtype = item.rpartition("/")
                zone = next((z for z in list(self.world.zones) + sorted(self.world.secondary_holds)
                             if name == z or name.endswith("." + z)), None)
                for transport in ("udp", "tcp"):
                    base = {"server": server, "name": name + ".", "qtype": qtype, "transport": transport,
                            "truncated": False}
                    if zone is None or not self.serves(server, zone):
                        answers.append({**base, "rcode": "REFUSED", "authoritative": False, "answer": []})
                        continue
                    data = self.world.zones.get(zone) or {"serial": 1, "records": {}}
                    records = []
                    if qtype == "SOA" and name == zone:
                        records = [(zone, "SOA", f"ns1.ns-accept.example. h. {data['serial']} 1 2 3 4")]
                    elif qtype == "NS" and name == zone:
                        records = [(zone, "NS", self.topology.primary.nameserver + "."),
                                   (zone, "NS", self.topology.secondary.nameserver + ".")]
                    else:
                        records = [(name, qtype, value) for value in sorted(data["records"].get((name, qtype), set()))]
                    rcode = "NOERROR" if records or name == zone else "NXDOMAIN"
                    answers.append({**base, "rcode": rcode, "authoritative": True,
                                    "answer": [{"name": n + ".", "type": t, "ttl": 300, "data": d} for n, t, d in records]})
            return {"command": "dns", "server": server, "answers": answers}
        raise AssertionError(f"unexpected probe {command}")


def panel_factory(panels: dict[str, FakePanel], redactor: Any, clock: Any, sleep: Any):
    ports = {"debian13": 28443, "arch": 28444}

    @contextlib.contextmanager
    def factory(driver: Any, node: str):
        yield PanelClient(node, f"https://127.0.0.1:{ports[node]}", panels[node], redactor, driver.recorder,
                          clock=clock, sleep=sleep)

    return factory
