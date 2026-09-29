"""Test doubles for the offline sequencing tests (never used by a real run).

``World`` is the shared DNS truth both fake guests serve; ``FakePanel`` is a
small state machine speaking the Panel API subset the driver uses, including
its CSRF (Origin) and session checks; ``FakeGuests`` answers the probe
commands from the same world and records every guest command.
"""

from __future__ import annotations

import base64
import contextlib
import hashlib
import json
import subprocess
from dataclasses import dataclass, field
from typing import Any
from urllib.parse import parse_qs, urlsplit

from panel_api import PanelClient, Response

COOKIE = "fake-session-cookie-value-0123456789"
HOST_PUB = "ssh-ed25519 " + base64.b64encode(
    bytes.fromhex("0000000b7373682d6564323535313900000020") + bytes(range(32))).decode()


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
                 peer_node: str = "") -> None:
        self.node, self.role, self.engine, self.world = node, role, engine, world
        self.licensed = licensed
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
        if route == "/api/v1/panel/license" and method == "POST":
            self.licensed = True
            return json_response(200, {"state": "active"})
        if not self.licensed:
            return json_response(403, {"error": "Panel access requires an active server license. Existing services "
                                                "and scheduled tasks keep running. The server administrator must "
                                                "activate or renew the license.", "code": "license_required"})
        return self.route(method, route, query, payload)

    def state(self) -> dict[str, Any]:
        return {"version": 1, "revision": self.revision, "origin": "fresh", "status": self.status,
                "draft": self.draft, "required": True, "guidance": self.guidance, "checks": []}

    def execution(self) -> dict[str, Any]:
        script = self.execution_script or default_execution_script(self.role)
        index = min(self.polls, len(script) - 1)
        self.polls += 1
        value = json.loads(json.dumps(script[index]))
        value.update({"id": "exec-" + self.node, "request_id": self.started["request_id"], "plan_id": "plan-" + self.node})
        return value

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
        if route == "/api/v1/setup/plan" and method == "POST":
            return json_response(200, {"id": "plan-" + self.node, "can_start": self.plan_can_start, "blockers": [],
                                       "purpose": "dns", "steps": [{"id": "01-dns", "kind": "dns", "target": "local"}]})
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
            ready = self.started is not None
            return json_response(200, {"revision": 3, "active_engine": self.engine if ready else None,
                                       "state": "ready" if ready else "unconfigured",
                                       "topology": "paired" if ready else "unconfigured",
                                       "pair_role": self.role if ready else None, "pair_ready": ready,
                                       "zone_count": len(self.world.zones), "pending_zone_count": 0})
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
    def __init__(self, world: World, topology: Any) -> None:
        self.world = world
        self.topology = topology
        self.commands: list[tuple[str, str, bool]] = []
        self.plan = None
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
        elif "primary-activate" in command:
            self.world.enrolled = True
            stdout = json.dumps({"state": "configured", "enrollment_id": "e" * 32}).encode()
        return subprocess.CompletedProcess([command], 0, stdout, b"")

    def journal(self, node: str, unit: str, *, boots: str = "all") -> str:
        return f"{node} {unit} journal line\n"

    def disable_management(self, node: str) -> None:
        self.commands.append((node, "disable-management", True))
        self.world.management[node] = False

    def enable_management(self, node: str) -> None:
        self.commands.append((node, "enable-management", True))
        self.world.management[node] = True
        if self.world.mutate_ledger_on_return:
            self.world.bump(node)

    def reboot(self, node: str, timeout: int = 900) -> dict[str, Any]:
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
            return {"command": "native", "engine": options["engine"][0], "zone": zone,
                    "native_state": "present" if holds else "absent", "zone_files": []}
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
