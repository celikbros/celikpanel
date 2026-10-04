#!/usr/bin/env python3
"""DNS pair product-flow acceptance driver (roadmap item 2).

Drives two disposable kill-matrix QEMU guests through the *normal product
flow*: the customer installer on both, first administrator, license state,
the server-setup wizard API with DNS identity/topology, the domain screens'
zone lifecycle (add, record add/edit, delete, re-add), owner enrollment where
the product asks for it, then a management-disabled reboot (D-022) and a
read-only return of management.

Dry-run by default: ``plan`` (and ``run`` without ``--execute``) contacts no
guest and prints the full step plan. ``run --execute`` refuses anything but
the fixture plan's own disposable guests (SMBIOS UUID + cloud-init marker,
exactly like ``fixture.py reboot``). Nothing here is native evidence until a
run's retained ``result.json`` says so.
"""

from __future__ import annotations

import argparse
import base64
import contextlib
import datetime as dt
import hashlib
import json
import secrets
import shutil
import subprocess
import sys
import tarfile
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Iterator

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))

import dns_checks as dc  # noqa: E402
import evidence as ev  # noqa: E402
import guidance as gd  # noqa: E402
import install_steps as inst  # noqa: E402
import pair_contract as pc  # noqa: E402
import topology as topo  # noqa: E402
from panel_api import (  # noqa: E402
    PanelClient,
    PanelError,
    PinnedHTTPSTransport,
    PollMutationError,
    UnknownOutcome,
)
from redaction import Redactor  # noqa: E402

DRIVER_SCHEMA = "celikpanel/dns-pair-acceptance-driver/v1"
LICENSE_MODES = ("none", "owner-key", "acceptance-fixture")
# acceptance-fixture: both panels come from scripts/build-dist.sh
# --acceptance-license (panel built with the acceptance_license test tag,
# internal/licensing/acceptance_fixture.go). That build never contacts the
# license service and accepts only this public fixture key, only on a guest
# carrying the fixture's cloud-init marker. Release packaging refuses it.
ACCEPTANCE_FIXTURE_KEY = "CPK-acce57f1c7" + "0" * 54
ACCEPTANCE_FIXTURE_KIND = "acceptance_fixture"
ACCEPTANCE_FIXTURE_LABEL = "ACCEPTANCE FIXTURE \u2014 NOT FOR PRODUCTION"
ACCEPTANCE_BUILD_MARKER = "ACCEPTANCE-LICENSE-BUILD.txt"
NATIVE_EVIDENCE_SCOPE = {
    "none": "Customer panel build. The license lock is observed as the product shows it; nothing after it ran.",
    "owner-key": "Customer panel build with owner-supplied licenses verified by the license service.",
    "acceptance-fixture": (
        "License behaviour is NOT evidenced by this run. Both panels were built with the acceptance_license "
        "test tag and ran on the acceptance fixture license, which exists only in that build: no customer "
        "license, no license service contact, and no activation, renewal, expiry or rejection path of the "
        "product. Only the DNS pair product flow after licensing is in scope for review."
    ),
}
EDIT_METHODS = ("ui-replace", "api-put")
ADMIN_USERNAME = "owner"
RECORD_LABEL = "accept"
RECORD_ADDRESSES = ("198.51.100.10", "198.51.100.20")
RECORD_TTL = 300
# DNS engine card fields that would show a staged DNS identity or pairing.
DNS_IDENTITY_KEYS = ("revision", "active_engine", "engine_epoch", "state", "topology", "pair_role", "pair_ready",
                     "secondary_ready", "local_nameserver", "peer_nameserver", "local_ip", "peer_ip", "identity")
ENGINE_SUMMARY_KEYS = ("active_engine", "state", "topology", "pair_role", "pair_ready", "secondary_ready",
                       "zone_count", "pending_zone_count")
NON_DNS_SETUP_PHASES = frozenset({"access_dns", "panel_certificate", "verification", "verify"})
JOURNAL_UNITS = (
    "celikpanel-panel.service",
    "celikpanel-agent.service",
    "named.service",
    "bind9.service",
    "pdns.service",
)
DNS_UNIT = {("bind", "debian13"): "named.service", ("bind", "arch"): "named.service",
            ("pdns", "debian13"): "pdns.service"}
# Item 6 (pair1): the acceptance seam counts refused license-service attempts
# only inside the Panel process (licensing.AcceptanceLicenseServiceDialAttempts)
# and exposes the counter nowhere. The driver cannot read it; it reads the
# Panel journal instead (read-only) for the license service host and for the
# refusing transport's exact error text (internal/licensing/acceptance_fixture.go).
LICENSE_SERVICE_HOST = "celikpanel.net"
LICENSE_REFUSAL_TEXT = "the acceptance test build never contacts the license service"
ACCEPTANCE_BANNER_TEXT = "this panel was built with the acceptance_license test tag"
PANEL_UNIT = "celikpanel-panel.service"
STATIC_FINDINGS = (
    {
        "id": "ui-record-edit-is-delete-add",
        "text": "The Domains DNS screen has no edit form: web/src/components/DomainDNSManager.tsx "
                "calls only POST and DELETE on /dns/records; PUT /dns/records "
                "(cmd/panel/domain_dns_handlers.go handleUpdateDNSRecord) is API-only. "
                "--edit-method selects which one this run exercised.",
    },
)


class StepFailed(Exception):
    cause = ""


class ProductFailure(StepFailed):
    """A verified product defect stopped the step (the run fails with cause ``product``)."""

    cause = "product"


class GateRefused(Exception):
    pass


class ProductBlocked(Exception):
    pass


class StepSkipped(Exception):
    pass


@dataclass
class StepRecord:
    index: int
    id: str
    title: str
    verdict: str = "not-run"
    reason: str = ""
    # Why a failed step failed, when the driver knows: "product" (a product
    # defect the driver verified, e.g. an open-ended unknown state); "" otherwise.
    cause: str = ""
    started_at: str | None = None
    finished_at: str | None = None
    guidance: list[dict[str, Any]] = field(default_factory=list)
    checks: dict[str, Any] = field(default_factory=dict)

    @property
    def directory(self) -> str:
        return f"steps/{self.index:02d}-{self.id}"

    def as_dict(self) -> dict[str, Any]:
        return {
            "index": self.index,
            "id": self.id,
            "title": self.title,
            "verdict": self.verdict,
            "reason": self.reason,
            "cause": self.cause,
            "started_at": self.started_at,
            "finished_at": self.finished_at,
            "evidence_directory": self.directory,
            "guidance": self.guidance,
            "checks": self.checks,
        }


@dataclass
class Config:
    run_id: str
    run_label: str
    license_mode: str = "none"
    license_keys: dict[str, str] = field(default_factory=dict)  # role -> key (never written)
    edit_method: str = "ui-replace"
    infrastructure_dns: bool = False
    skip_security_updates: bool = False
    dist_archive: Path | None = None
    dist_root: str = ""
    dist_sha256: str = ""
    dist_commit: str = ""
    dist_tree: str = ""
    setup_timeout: float = 2700
    dns_timeout: float = 240
    deletion_timeout: float = 300
    reboot_timeout: int = 900
    poll_interval: float = 3.0
    dns_interval: float = 2.0
    # Item 5 (pair1): stop waiting once the polled state is identical for
    # stable_stop_polls reads spanning stable_stop_seconds and it is not a
    # product-declared in-progress state. None disables it.
    stable_stop_seconds: float | None = 300.0
    stable_stop_polls: int = 5
    # D-024 (pair2 t3): the longest a listed unknown/reconciling setup state
    # (guidance.UNKNOWN_STATE_CODES) may last before it is a product finding.
    unknown_state_limit: float = 300.0
    # The product's web/src the texts come from ("" = the driver's own checkout).
    web_src: str = ""
    web_src_provenance: dict[str, Any] = field(default_factory=dict)

    def public(self) -> dict[str, Any]:
        return {
            "run_id": self.run_id,
            "run_label": self.run_label,
            "license_mode": self.license_mode,
            "license_keys_supplied_for": sorted(self.license_keys),
            "edit_method": self.edit_method,
            "infrastructure_dns": self.infrastructure_dns,
            "skip_security_updates": self.skip_security_updates,
            "dist_archive": str(self.dist_archive) if self.dist_archive else None,
            "dist_root": self.dist_root,
            "dist_sha256": self.dist_sha256,
            "dist_commit": self.dist_commit,
            "dist_tree": self.dist_tree,
            "timeouts": {
                "setup": self.setup_timeout, "dns": self.dns_timeout,
                "deletion": self.deletion_timeout, "reboot": self.reboot_timeout,
                "stable_stop_seconds": self.stable_stop_seconds, "stable_stop_polls": self.stable_stop_polls,
                "unknown_state_limit_seconds": self.unknown_state_limit,
            },
            "unknown_state_codes": list(gd.UNKNOWN_STATE_CODES),
            "web_src": self.web_src or str(gd.WEB_SRC),
            "web_src_provenance": self.web_src_provenance or {"source": "driver checkout"},
        }


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")


def admin_password() -> str:
    # Mixed classes so any reasonable password policy admits it; never printed.
    return "Cp7!" + secrets.token_urlsafe(24)


def known_host_digest(known_hosts: Path, port: int) -> str | None:
    """sha256(wire key blob) of the fixture's pinned Ed25519 host key."""

    try:
        lines = known_hosts.read_text(encoding="utf-8").splitlines()
    except OSError:
        return None
    for line in lines:
        parts = line.split()
        if len(parts) >= 3 and parts[0] == f"[127.0.0.1]:{port}" and parts[1] == "ssh-ed25519":
            try:
                return hashlib.sha256(base64.b64decode(parts[2], validate=True)).hexdigest()
            except ValueError:
                return None
    return None


def pubkey_digest(text: str) -> str | None:
    parts = text.split()
    if len(parts) >= 2 and parts[0] == "ssh-ed25519":
        try:
            return hashlib.sha256(base64.b64decode(parts[1], validate=True)).hexdigest()
        except ValueError:
            return None
    return None


def dist_identity(archive: Path) -> dict[str, str]:
    """Root directory, release.commit and release.tree inside a make-dist archive."""

    digest = hashlib.sha256()
    with archive.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    roots: set[str] = set()
    values: dict[str, str] = {}
    with tarfile.open(archive, "r:gz") as bundle:
        for member in bundle:
            name = member.name.lstrip("./")
            if not name:
                continue
            roots.add(name.split("/", 1)[0])
            if name.count("/") == 1 and name.split("/", 1)[1] == ACCEPTANCE_BUILD_MARKER and member.isfile():
                values["acceptance_license_build"] = "yes"
            if name.count("/") == 1 and name.split("/", 1)[1] in {"release.commit", "release.tree"} and member.isfile():
                extracted = bundle.extractfile(member)
                if extracted is not None:
                    values[name.split("/", 1)[1]] = extracted.read(200).decode("ascii", "replace").strip()
    if len(roots) != 1:
        raise ValueError(f"dist archive must contain exactly one root directory, found {sorted(roots)}")
    return {"sha256": digest.hexdigest(), "root": roots.pop(),
            "commit": values.get("release.commit", ""), "tree": values.get("release.tree", ""),
            "acceptance_license_build": values.get("acceptance_license_build") == "yes"}


class Driver:
    def __init__(
        self,
        config: Config,
        topology: topo.Topology,
        guests: Any,
        evidence: ev.EvidenceWriter,
        redactor: Redactor,
        translator: gd.Translator,
        panel_factory: Callable[["Driver", str], Any],
        *,
        clock: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], None] = time.sleep,
        archive_reader: Callable[[Path], bytes] | None = None,
        pid_alive: Callable[[str], bool] | None = None,
        dist_identity_reader: Callable[[Path], dict[str, str]] | None = None,
    ) -> None:
        self.config = config
        self.topology = topology
        self.guests = guests
        self.evidence = evidence
        self.redactor = redactor
        self.translator = translator
        self.panel_factory = panel_factory
        self.clock = clock
        self.sleep = sleep
        self.archive_reader = archive_reader or (lambda path: path.read_bytes())
        self.pid_alive = pid_alive or (lambda node: True)
        self.dist_identity_reader = dist_identity_reader or dist_identity
        self.steps: list[StepRecord] = []
        self.current: StepRecord | None = None
        self.findings: list[dict[str, Any]] = [dict(item) for item in STATIC_FINDINGS]
        self.blockers: list[dict[str, Any]] = []
        self.passwords: dict[str, str] = {}
        self.pins: dict[str, str] = {}
        self.cookies: dict[str, str | None] = {}
        self.request_ids: dict[str, str] = {}
        self.executions: dict[str, dict[str, Any]] = {}
        self.domain_id: int | None = None
        self.serial: int | None = None
        self.snapshots: dict[str, Any] = {}
        self.started_at = now_iso()
        self.identity_ok: set[str] = set()
        self.license_status: dict[str, dict[str, Any]] = {}
        # Steps the server owner performs in the product flow (install.sh's
        # restart request, dns-peer-enroll), recorded as owner actions.
        self.owner_steps: list[dict[str, Any]] = []
        self.license_journal: dict[str, dict[str, Any]] = {}
        self.web_src = Path(config.web_src) if config.web_src else gd.WEB_SRC
        # Every license_required setup read, with the license status read right after it.
        self.license_required_observations: list[dict[str, Any]] = []
        self._peer_start: dict[str, Any] | None = None

    # -- step machinery ---------------------------------------------------------

    def step(self, step_id: str, title: str, action: Callable[[StepRecord], None], *,
             needs: tuple[str, ...] = (), always: bool = False) -> StepRecord:
        record = StepRecord(len(self.steps), step_id, title)
        self.steps.append(record)
        unmet = [s for s in self.steps[:-1] if s.id in needs and s.verdict != "passed"]
        if unmet and not always:
            gate = any(s.verdict in {"refused-by-product-gate", "skipped"} for s in unmet)
            record.verdict = "skipped" if gate else "not-run"
            record.reason = "prerequisite step(s) " + ", ".join(f"{s.id}={s.verdict}" for s in unmet)
            return record
        record.started_at = now_iso()
        self.current = record
        try:
            action(record)
            record.verdict = "passed"
        except GateRefused as exc:
            record.verdict, record.reason = "refused-by-product-gate", str(exc)
        except ProductBlocked as exc:
            record.verdict, record.reason = "blocked-product", str(exc)
        except StepSkipped as exc:
            record.verdict, record.reason = "skipped", str(exc)
        except StepFailed as exc:
            record.verdict, record.reason, record.cause = "failed", str(exc), exc.cause
        except PollMutationError as exc:
            record.verdict, record.reason = "failed", f"driver safety: {exc}"
        except (PanelError, OSError, ValueError, KeyError, TypeError) as exc:
            record.verdict, record.reason = "failed", f"{type(exc).__name__}: {exc}"
        except Exception as exc:  # noqa: BLE001 - any harness exception is a failed step, recorded
            record.verdict, record.reason = "failed", f"unexpected {type(exc).__name__}: {exc}"
        finally:
            record.finished_at = now_iso()
            self.current = None
        return record

    def record(self, name: str, value: Any) -> str:
        assert self.current is not None
        return self.evidence.write_json(f"{self.current.directory}/{name}", value)

    def record_text(self, name: str, value: str) -> str:
        assert self.current is not None
        return self.evidence.write_text(f"{self.current.directory}/{name}", value)

    def recorder(self, exchange: dict[str, Any]) -> None:
        directory = self.current.directory if self.current else "steps/unscoped"
        self.evidence.api_exchange(directory, exchange)

    def add_guidance(self, record: StepRecord, item: dict[str, Any], *, context: str, payload: Any = None) -> None:
        entry = dict(item)
        entry["context"] = context
        entry["observed_at"] = now_iso()
        record.guidance.append(entry)
        if entry["state"] not in {"progress", "succeeded"} and not entry["actionable"]:
            record.checks.setdefault("d024_failures", []).append(
                {"context": context, "code": entry.get("code"), "why": entry["actionable_reason"]}
            )
            # A generic or missing explanation is a product finding (D-024), with
            # what the owner would see and the payload that produced it.
            self.findings.append({
                "id": f"d024-no-actionable-guidance-{record.id}",
                "kind": "product",
                "step": record.id,
                "context": context,
                "state": entry["state"],
                "code": entry.get("code"),
                "http_status": entry.get("http_status"),
                "why": entry["actionable_reason"],
                "shown": entry.get("shown"),
                "api_error_text": entry.get("api_error_text"),
                "payload": payload,
            })

    def require_actionable(self, record: StepRecord) -> None:
        failures = record.checks.get("d024_failures")
        if failures:
            raise StepFailed(f"D-024: a blocked/pending state carried no actionable guidance: {failures}")

    @contextlib.contextmanager
    def panel(self, role: topo.Role) -> Iterator[PanelClient]:
        with self.panel_factory(self, role.node) as client:
            client.cookie = self.cookies.get(role.node)
            try:
                yield client
            finally:
                self.cookies[role.node] = client.cookie

    def role(self, name: str) -> topo.Role:
        return self.topology.primary if name == "primary" else self.topology.secondary

    # -- orchestration --------------------------------------------------------

    def execute(self) -> dict[str, Any]:
        P, S = self.topology.primary, self.topology.secondary
        self.step("preflight", "Disposable guest identity, pristine layout, artifact identity", self.preflight)
        for role in (P, S):
            self.step(f"install-{role.role}", f"Customer installer on {role.node} ({role.engine} {role.role})",
                      lambda r, role=role: self.install(r, role), needs=("preflight",))
        for role in (P, S):
            self.step(f"login-{role.role}", f"First administrator signs in on {role.node}",
                      lambda r, role=role: self.login(r, role), needs=(f"install-{role.role}",))
        for role in (P, S):
            self.step(f"license-{role.role}", f"Panel license state on {role.node}",
                      lambda r, role=role: self.license(r, role), needs=(f"login-{role.role}",))
        licensed = ("license-primary", "license-secondary")
        review = self.step("setup-review-primary", "Wizard draft and reviewed plan on the primary",
                           lambda r: self.setup_review(r, P), needs=licensed)
        if review.verdict == "refused-by-product-gate":
            self.step("setup-secondary-before-primary", "Secondary setup while the primary is refused",
                      lambda r: self.secondary_without_primary(r), needs=licensed)
            for step_id in ("pair-ready", "zone-add", "record-add", "record-edit", "zone-delete",
                            "zone-readd", "independence-reboot", "management-return"):
                self.step(step_id, step_id, lambda r: None, needs=("setup-review-primary",))
        else:
            self.step("setup-start-primary", "Start the reviewed primary plan; wait for its DNS step",
                      lambda r: self.setup_start(r, P), needs=("setup-review-primary",))
            self.step("setup-review-secondary", "Wizard draft and reviewed plan on the secondary",
                      lambda r: self.setup_review(r, S), needs=("setup-start-primary",))
            self.step("setup-start-secondary", "Start the reviewed secondary plan; wait for its DNS step",
                      lambda r: self.setup_start(r, S), needs=("setup-review-secondary",))
            self.step("pair-ready", "Both Panels show the paired engines ready; setup settles",
                      self.pair_ready, needs=("setup-start-primary", "setup-start-secondary"))
            self.step("zone-add", "Add the domain on the primary", self.zone_add, needs=("pair-ready",))
            self.step("record-add", "Add an A record", self.record_add, needs=("zone-add",))
            self.step("record-edit", f"Edit the A record ({self.config.edit_method})", self.record_edit,
                      needs=("record-add",))
            self.step("zone-delete", "Delete the domain; prove secondary absence", self.zone_delete,
                      needs=("record-edit",))
            self.step("zone-readd", "Re-add the domain", self.zone_readd, needs=("zone-delete",))
            self.step("independence-reboot", "Management disabled on both; reboot; DNS serves alone",
                      self.independence, needs=("zone-readd",))
            self.step("management-return", "Management re-enabled; Panel shows the truth without mutating",
                      self.management_return, needs=("independence-reboot",))
        self.step("collect", "Journals, versions and native state from both guests", self.collect, always=True)
        return self.finalize()

    def finalize(self) -> dict[str, Any]:
        verdicts = [step.verdict for step in self.steps]
        overall = ev.overall_status(verdicts)
        causes = sorted({step.cause or "unclassified" for step in self.steps if step.verdict == "failed"})
        result = {
            "schema": ev.RESULT_SCHEMA,
            "driver": DRIVER_SCHEMA,
            "native_evidence": False,
            "native_evidence_note": "result of one disposable run; a reviewer decides what it proves",
            "license_mode": self.config.license_mode,
            "native_evidence_scope": NATIVE_EVIDENCE_SCOPE[self.config.license_mode],
            "started_at": self.started_at,
            "finished_at": now_iso(),
            "topology": self.topology.as_dict(),
            "config": self.config.public(),
            "steps": [step.as_dict() for step in self.steps],
            "overall": overall,
            # "product" only when every failed step failed on a verified product defect.
            "overall_cause": (None if overall != "failed" else causes[0] if len(causes) == 1 else "mixed"),
            "failure_causes": causes,
            "findings": self.findings,
            "license_required_observations": self.license_required_observations,
            "product_blockers": self.blockers,
            "owner_steps": self.owner_steps,
            "license_service_journal_check": self.license_journal,
        }
        if self.config.license_mode == "acceptance-fixture":
            result["license_status"] = self.license_status
        self.evidence.finalize(result)
        return result

    # -- guest helpers ----------------------------------------------------------

    def probe(self, node: str, *args: str) -> dict[str, Any]:
        return self.guests.probe(node, *args)

    def observe_dns(self, queries: list[tuple[str, str]]) -> list[dict[str, Any]]:
        """Ask both servers from both guests over the isolated peer link (UDP and TCP)."""

        observations = []
        for observer in (self.topology.primary.node, self.topology.secondary.node):
            for server in (self.topology.primary.address, self.topology.secondary.address):
                args = ["dns", "--server", server]
                for name, qtype in queries:
                    args += ["--query", f"{name}/{qtype}"]
                result = self.probe(observer, *args)
                result["observer"] = observer
                observations.append(result)
        return observations

    def wait_present(self, record: StepRecord, label: str, expected: dict[tuple[str, str], set[str]],
                     min_serial: int | None) -> dict[str, Any]:
        zone = self.topology.zone
        queries = [(zone, "SOA"), (zone, "NS")] + sorted(expected)
        servers = [self.topology.primary.address, self.topology.secondary.address]
        nameservers = [self.topology.primary.nameserver, self.topology.secondary.nameserver]
        deadline = self.clock() + self.config.dns_timeout
        attempts = 0
        first = None
        while True:
            attempts += 1
            observations = self.observe_dns(queries)
            verdict = dc.evaluate_present(observations, servers=servers, zone=zone, nameservers=nameservers,
                                          expected=expected, min_serial=min_serial)
            if first is None:
                first = observations
                self.record(f"dns-{label}-first.json", observations)
            if verdict["passed"] or self.clock() >= deadline:
                break
            self.sleep(self.config.dns_interval)
        self.record(f"dns-{label}-final.json", observations)
        verdict["attempts"] = attempts
        record.checks[f"dns_{label}"] = verdict
        if not verdict["passed"]:
            raise StepFailed(f"authoritative answers did not converge ({label}): {verdict['reasons'][:5]}")
        return verdict

    def native(self, record: StepRecord, label: str, *, expect_present: bool) -> None:
        catalog = dc.catalog_name(self.topology.primary.address)
        failures = []
        for role in (self.topology.primary, self.topology.secondary):
            observation = self.probe(role.node, "native", "--engine", role.engine, "--zone", self.topology.zone,
                                     "--catalog", catalog)
            self.record(f"native-{label}-{role.role}.json", observation)
            verdict = dc.evaluate_native(observation, expect_present=expect_present)
            record.checks[f"native_{label}_{role.role}"] = verdict
            if not verdict["passed"]:
                failures.append(f"{role.role}: {verdict['reasons']}")
        # The secondary transfers the catalog from the primary (allow-transfer to the peer).
        catalog_obs = self.probe(self.topology.secondary.node, "catalog", "--server",
                                 self.topology.primary.address, "--catalog", catalog)
        self.record(f"catalog-{label}.json", catalog_obs)
        verdict = dc.evaluate_catalog(catalog_obs, zone=self.topology.zone, expect_member=expect_present)
        record.checks[f"catalog_{label}"] = verdict
        if not verdict["passed"]:
            failures.append(f"catalog: {verdict['reasons']}")
        if failures:
            raise StepFailed(f"native state ({label}) differs: {failures}")

    def engine_snapshot(self, record: StepRecord, label: str) -> dict[str, Any]:
        snapshot = {}
        for role in (self.topology.primary, self.topology.secondary):
            with self.panel(role) as client:
                response = client.get("/api/v1/dns/engine", timeout=40, purpose="DNSEngineCard snapshot")
                snapshot[role.role] = {"status": response.status, "body": response.json()}
        record.checks[f"engine_{label}"] = {
            role: {key: (value.get("body") or {}).get(key, pc.ABSENT) for key in ENGINE_SUMMARY_KEYS}
            for role, value in snapshot.items()
        }
        return snapshot

    def stable_stop(self, settled: Callable[[Any], bool]) -> dict[str, Any]:
        if not self.config.stable_stop_seconds:
            return {}
        return {"stable_after": self.config.stable_stop_seconds, "stable_polls": self.config.stable_stop_polls,
                "settled": settled}

    def wait_pair_readiness(self, record: StepRecord, role: topo.Role, client: Any, *, timeout: float,
                            label: str) -> dict[str, Any]:
        """Poll GET /api/v1/dns/engine until D1 holds for this role (pair_contract.pair_readiness).

        Stops at once when the verdict cannot change (``pc.readiness_final``:
        an active paired secondary of a build that does not report
        ``secondary_ready``), early once the snapshot is stable and settled;
        the payload of the last read is always recorded.
        """

        def read(view: Any) -> Any:
            response = view.get("/api/v1/dns/engine", timeout=40)
            if response.status != 200:
                return {"poll_error": f"HTTP {response.status}", "body": response.json()}
            return response.json()

        done, snapshot, _ = client.poll(
            read, lambda body: pc.readiness_final(pc.pair_readiness(body, role=role.role, engine=role.engine)),
            timeout=timeout, interval=max(self.config.poll_interval, 5.0), **self.stable_stop(pc.engine_settled))
        verdict = pc.pair_readiness(snapshot, role=role.role, engine=role.engine)
        verdict["poll"] = dict(getattr(client, "last_poll", {}) or {}, timeout=timeout)
        verdict["payload_file"] = self.record(f"engine-{label}-{role.role}.json", snapshot)
        record.checks[f"pair_readiness_{label}_{role.role}"] = verdict
        if done != pc.readiness_final(verdict):
            raise StepFailed(f"{role.node}: pair readiness verdict changed between poll and record")
        return verdict

    def readiness_failure(self, role: topo.Role, verdict: dict[str, Any]) -> str:
        poll = verdict.get("poll") or {}
        if verdict.get("blocked") == pc.BLOCKED_NOT_REPORTED:
            return (f"{role.node}: this build does not report secondary readiness (D1: {verdict['rule']}); "
                    f"{'; '.join(verdict['reasons'])}; payload {verdict['payload_file']}: {verdict['observed']}")
        if poll.get("stop") == "stable":
            why = (f"stopped early: the snapshot was identical for {poll.get('identical_reads')} reads over "
                   f"{poll.get('stable_seconds')}s and does not satisfy the rule")
        else:
            why = f"not satisfied within {poll.get('timeout')}s"
        return (f"{role.node} Panel does not show a ready paired {role.engine} {role.role} (D1: {verdict['rule']}); "
                f"{why}: {'; '.join(verdict['reasons'])}; payload {verdict['payload_file']}: {verdict['observed']}")

    # -- steps ------------------------------------------------------------------

    def preflight(self, record: StepRecord) -> None:
        identities = {}
        for role in self.topology.roles:
            if not self.pid_alive(role.node):
                raise StepFailed(f"{role.node}: this cell's QEMU process is not alive")
            identities[role.node] = self.guests.verify(role.node)
            self.identity_ok.add(role.node)
            self.guests.run(role.node, inst.preinstall_check(), mutating=False)
            self.record(f"versions-before-{role.node}.json", self.probe(role.node, "versions"))
        self.record("guest-identity.json", identities)
        record.checks["guest_identity"] = {node: {k: v for k, v in value.items() if k != "boot_id"}
                                           for node, value in identities.items()}
        if self.config.dist_archive is not None:
            identity = self.dist_identity_reader(self.config.dist_archive)
            record.checks["dist"] = identity
            if identity["sha256"] != self.config.dist_sha256:
                raise StepFailed("dist archive digest differs from --dist-sha256")
            if identity["root"] != self.config.dist_root or identity["commit"] != self.config.dist_commit \
                    or identity["tree"] != self.config.dist_tree:
                raise StepFailed(f"dist archive identity differs: {identity}")
            acceptance_build = bool(identity.get("acceptance_license_build"))
            if self.config.license_mode == "acceptance-fixture" and not acceptance_build:
                raise StepFailed("--license-mode acceptance-fixture needs the archive from scripts/build-dist.sh "
                                 f"--acceptance-license ({ACCEPTANCE_BUILD_MARKER} is missing)")
            if self.config.license_mode != "acceptance-fixture" and acceptance_build:
                raise StepFailed("this archive's panel was built with the acceptance_license test tag; its license "
                                 "state is not the product's. Use --license-mode acceptance-fixture or a customer archive")

    def install(self, record: StepRecord, role: topo.Role) -> None:
        if self.config.dist_archive is None:
            raise StepFailed("no dist archive configured")
        data = self.archive_reader(self.config.dist_archive)
        staged = self.guests.run(role.node, inst.stage_archive(self.config.dist_root, self.config.dist_sha256,
                                                               self.config.dist_commit, self.config.dist_tree),
                                 mutating=True, stdin=data, timeout=900, check=False)
        self.record_text("stage.txt", _decode(staged.stdout) + _decode(staged.stderr))
        if staged.returncode != 0:
            raise StepFailed(f"staging the dist archive exited {staged.returncode}")
        password = admin_password()
        self.redactor.register(password)
        self.passwords[role.node] = password
        email = f"{ADMIN_USERNAME}@{self.topology.infra_zone}"
        uploaded = self.guests.run(role.node, inst.upload_credentials(), mutating=True,
                                   stdin=inst.credentials_document(ADMIN_USERNAME, email, password), check=False)
        self.record_text("credentials-upload.txt", _decode(uploaded.stdout) + _decode(uploaded.stderr))
        if uploaded.returncode != 0:
            raise StepFailed(f"credentials upload exited {uploaded.returncode}")
        installed = self.guests.run(role.node, inst.run_installer(
            self.config.dist_root, skip_security_updates=self.config.skip_security_updates),
            mutating=True, timeout=5400, check=False)
        self.record_text("install-sh.txt", _decode(installed.stdout) + "\n--- stderr ---\n" + _decode(installed.stderr))
        record.checks["installer_exit"] = installed.returncode
        if installed.returncode != 0:
            raise StepFailed(f"install.sh exited {installed.returncode}")
        check = self.guests.run(role.node, inst.postinstall_check(), mutating=False, check=False)
        self.record_text("postinstall.txt", _decode(check.stdout) + _decode(check.stderr))
        if check.returncode != 0:
            raise StepFailed("installed services or install.complete marker are missing")
        notice = inst.installer_restart_notice(_decode(installed.stdout))
        record.checks["installer_restart_notice"] = notice["state"]
        if notice["state"] == "recommended":
            # Nothing is broken; the owner may restart later. Recorded, not acted on.
            self.record_text("installer-restart-notice.txt", notice["text"])
        elif notice["state"] == "required":
            self.owner_restart_after_install(record, role, notice)
        tls = self.probe(role.node, "tls")
        self.record("tls-pin.json", tls)
        if "leaf_sha256" not in tls:
            raise StepFailed(f"panel TLS leaf could not be read on the guest: {tls}")
        self.pins[role.node] = tls["leaf_sha256"]

    def owner_restart_after_install(self, record: StepRecord, role: topo.Role, notice: dict[str, str]) -> None:
        """install.sh said the server must be restarted; the owner does that before using the panel.

        pair1 (H3): Arch replaced the running kernel's modules, so nftables
        could not load and setup's firewall check failed with a generic 500.
        The restart goes through the fixture's identity-checked orderly reboot
        (fixture.reboot_guest: SMBIOS UUID and marker re-checked, new boot ID).
        """

        self.record_text("installer-restart-notice.txt", notice["text"])
        reboot = self.guests.reboot(role.node, self.config.reboot_timeout)
        self.record("owner-restart-after-install.json", reboot)
        boot_ids = [reboot.get("boot_id_before"), reboot.get("boot_id_after")]
        owner_step = {
            "step": record.id,
            "node": role.node,
            "actor": "server owner",
            "action": "restart the server as install.sh asked (fixture orderly reboot, guest identity re-checked)",
            "trigger": notice["banner"],
            "installer_text": notice["text"],
            "boot_ids": boot_ids,
        }
        self.owner_steps.append(owner_step)
        record.checks["owner_restart_after_install"] = owner_step
        if not boot_ids[0] or boot_ids[0] == boot_ids[1]:
            raise StepFailed(f"{role.node}: the owner restart did not produce a new boot ({boot_ids})")
        deadline = self.clock() + 180
        while True:
            check = self.guests.run(role.node, inst.postinstall_check(), mutating=False, check=False)
            if check.returncode == 0 or self.clock() >= deadline:
                break
            self.sleep(3)
        self.record_text("postinstall-after-restart.txt", _decode(check.stdout) + _decode(check.stderr))
        if check.returncode != 0:
            raise StepFailed(f"{role.node}: installed services are not active after the owner restart")
        deadline = self.clock() + 180
        while "leaf_sha256" not in self.probe(role.node, "tls") and self.clock() < deadline:
            self.sleep(3)

    def login(self, record: StepRecord, role: topo.Role) -> None:
        with self.panel(role) as client:
            identity = client.login(ADMIN_USERNAME, self.passwords[role.node])
            record.checks["identity"] = {k: identity.get(k) for k in ("username", "role", "effective_role")}
            availability = client.get("/api/v1/panel/availability", purpose="usePanelSession availability")
            record.checks["availability"] = availability.json()

    def license(self, record: StepRecord, role: topo.Role) -> None:
        with self.panel(role) as client:
            access = client.get("/api/v1/license/access", purpose="LicenseOnboarding access").json() or {}
            record.checks["license_access_before"] = access
            if self.config.license_mode == "acceptance-fixture":
                self.acceptance_license(record, role, client, access)
                return
            if access.get("can_use_panel") is True:
                return
            if self.config.license_mode == "none":
                probe = client.get("/api/v1/setup", purpose="wizard entry under the license lock")
                item = gd.api_error_guidance(self.translator, probe.status, probe.json())
                self.add_guidance(record, item, context="license lock on the setup API")
                self.require_actionable(record)
                blocker = {
                    "id": "L1-license-activation-offline",
                    "text": "Every authenticated Panel API except the license/identity exemptions returns "
                            "403 license_required until a license from https://celikpanel.net/account/ is "
                            "active, and an active license must be re-verified online about every minute. "
                            "A disposable offline host cannot complete activation.",
                    "sources": [
                        "internal/licensing/license.go:24 (API), :28 (CheckInterval = time.Minute), "
                        ":33 (PublicKeyHex), :168-193 (Status), :334-337 (AccessStatus refreshes)",
                        "cmd/panel/license.go:25-39 (newServerLicense uses the compiled key), "
                        ":183-203 (exempt routes), :226-248 (allowLicensedPanel)",
                        "cmd/panel/middleware.go:172 (gate on every authenticated request)",
                        "cmd/panel/server_setup_operations.go:534 (setup start requires CanProvision)",
                    ],
                    "test_only_workaround": (
                        "--license-mode acceptance-fixture with scripts/build-dist.sh --acceptance-license: "
                        "the panel is built with the acceptance_license test tag "
                        "(internal/licensing/acceptance_fixture.go), accepts only the fixture license on a "
                        "disposable guest and never contacts the license service; release packaging refuses "
                        "that build (deploy/release-acceptance-license-guard.sh). Such a run does not evidence "
                        "license behaviour. Customer license policy is unchanged."
                    ),
                }
                if blocker not in self.blockers:
                    self.blockers.append(blocker)
                raise ProductBlocked(
                    "license activation cannot complete on a disposable offline host (blocker "
                    "L1-license-activation-offline); rerun with --license-mode owner-key if the owner "
                    "supplies keys and permits the license service, or with --license-mode "
                    "acceptance-fixture for a run that does not evidence licensing")
            key = self.config.license_keys.get(role.role)
            if not key:
                raise StepFailed(f"--license-mode owner-key needs a key for the {role.role}")
            self.redactor.register(key)
            response = client.post("/api/v1/panel/license", {"action": "activate", "key": key},
                                   purpose="LicensePanel activate")
            if response.status != 200:
                item = gd.api_error_guidance(self.translator, response.status, response.json())
                self.add_guidance(record, item, context="license activation refused")
                raise StepFailed(f"license activation returned HTTP {response.status}")
            access = client.get("/api/v1/license/access", purpose="LicenseOnboarding access").json() or {}
            record.checks["license_access_after"] = access
            if access.get("can_use_panel") is not True:
                raise StepFailed(f"license is not usable after activation: {access.get('state')}")

    def require_fixture_label(self, status: dict[str, Any], role: topo.Role, stage: str) -> None:
        """The panel must say it runs on the acceptance fixture license, for this cell and node."""

        cell = self.topology.cell_id(self.config.run_label)
        expected = {"license_kind": ACCEPTANCE_FIXTURE_KIND, "license_label": ACCEPTANCE_FIXTURE_LABEL,
                    "acceptance_guest": "verified", "acceptance_cell": cell, "acceptance_node": role.node}
        wrong = {key: status.get(key) for key, value in expected.items() if status.get(key) != value}
        if wrong or not str(status.get("license_service", "")).startswith("not contacted"):
            raise StepFailed(f"{role.node} does not label its license as the acceptance fixture for this guest "
                             f"({stage}): {wrong or status.get('license_service')}; an unlabelled license is "
                             "never treated as fixture evidence")

    def acceptance_license(self, record: StepRecord, role: topo.Role, client: Any, access: dict[str, Any]) -> None:
        """Activate the fixture through the License screen's endpoint and record what it reports."""

        self.redactor.register(ACCEPTANCE_FIXTURE_KEY)
        before = client.get("/api/v1/panel/license", purpose="LicensePanel status")
        status = before.json() or {}
        self.record(f"license-status-before-{role.role}.json", {"http_status": before.status, "body": status})
        record.checks["license_status_before"] = _license_summary(status)
        self.require_fixture_label(status, role, "before activation")
        record.checks["activation_posts"] = 0
        if access.get("can_use_panel") is not True:
            response = client.post("/api/v1/panel/license", {"action": "activate", "key": ACCEPTANCE_FIXTURE_KEY},
                                   purpose="LicensePanel activate (acceptance fixture key)")
            record.checks["activation_posts"] = 1
            if response.status != 200:
                item = gd.api_error_guidance(self.translator, response.status, response.json())
                self.add_guidance(record, item, context="acceptance fixture activation refused")
                raise StepFailed(f"acceptance fixture activation returned HTTP {response.status}")
        access = client.get("/api/v1/license/access", purpose="LicenseOnboarding access").json() or {}
        record.checks["license_access_after"] = access
        after = client.get("/api/v1/panel/license", purpose="LicensePanel status")
        status = after.json() or {}
        self.record(f"license-status-{role.role}.json", {"http_status": after.status, "body": status})
        record.checks["license_status"] = _license_summary(status)
        self.license_status[role.role] = record.checks["license_status"]
        self.require_fixture_label(status, role, "after activation")
        if access.get("can_use_panel") is not True or status.get("state") != "active" \
                or status.get("can_provision") is not True:
            raise StepFailed(f"acceptance fixture license is not usable: access={access} state={status.get('state')}")

    def draft(self, role: topo.Role) -> dict[str, Any]:
        t = self.topology
        draft: dict[str, Any] = {
            "purpose": "dns",
            "dns_hosting_management": "",
            "dns_publisher_endpoint": "",
            "remote_dns_connection_id": "",
            "panel_domain": f"panel-{role.node}.{t.infra_zone}",
            "mail_hostname": "",
            "dns_mode": "local",
            "dns_engine": role.engine,
            "dns_role": role.role,
            "ns1": t.primary.nameserver,
            "ns2": t.secondary.nameserver,
            "local_ip": role.address,
            "peer_ip": role.peer_address,
            "peer_ns": role.peer_nameserver,
            "node_version": "",
            "database": "",
        }
        if self.config.infrastructure_dns and role.role == "primary":
            draft["infrastructure_dns"] = {"zone": t.infra_zone}
        return draft

    def setup_review(self, record: StepRecord, role: topo.Role) -> None:
        with self.panel(role) as client:
            state = client.get("/api/v1/setup", purpose="ServerSetupGate").json() or {}
            record.checks["setup_before"] = {k: state.get(k) for k in ("status", "revision", "guidance", "required", "origin")}
            if state.get("status") not in {"new", "draft", "legacy"}:
                raise StepFailed(f"setup is not fresh on {role.node}: {state.get('status')}")
            if state.get("guidance") not in {"guided", "manual"}:
                choice = client.put("/api/v1/setup/guidance", {"revision": state.get("revision"), "guidance": "guided"},
                                    purpose="ServerSetupChoice guided")
                if choice.status != 200:
                    raise StepFailed(f"guidance choice returned HTTP {choice.status}")
                state = choice.json() or client.get("/api/v1/setup").json() or {}
            engine_before = client.get("/api/v1/dns/engine", timeout=40, purpose="DNSEngineCard before review").json() or {}
            draft = self.draft(role)
            saved = client.put("/api/v1/setup", {"revision": state.get("revision"), "draft": draft},
                               purpose="ServerSetup draft save")
            if saved.status != 200:
                item = gd.api_error_guidance(self.translator, saved.status, saved.json())
                self.add_guidance(record, item, context="draft save refused")
                raise StepFailed(f"draft save returned HTTP {saved.status}")
            state = saved.json() or {}
            code_keys = gd.wizard_code_keys(self.web_src / gd.WEB_SRC_FILES["wizard"])
            record.checks["wizard_code_key_pdns_primary"] = code_keys.get(gd.PDNS_PRIMARY_GATE_CODE)
            selection = gd.setup_selection_error(draft)
            if selection:
                raise StepFailed(f"the wizard's selection rule refuses this draft: {selection}")
            plan_response = client.post("/api/v1/setup/plan", {"revision": state.get("revision")},
                                        purpose="ServerSetup review")
            plan = plan_response.json() or {}
            record.checks["plan"] = {k: plan.get(k) for k in ("id", "can_start", "blockers", "purpose")}
            record.checks["plan_steps"] = [(s.get("id"), s.get("kind"), s.get("target")) for s in plan.get("steps") or []]
            self.record(f"plan-{role.role}.json", plan)
            blockers = [code for code in plan.get("blockers") or [] if isinstance(code, str)]
            if any(code.split(":")[0] == gd.PDNS_PRIMARY_GATE_CODE for code in blockers):
                self.refused_plan_left_nothing(record, role, client, state, engine_before)
                item = gd.plan_blocker_guidance(self.translator, blockers, code_keys)
                self.add_guidance(record, item, context="server plan blocker (ServerSetup review)")
                engine_item = gd.preview_blocker_guidance(self.translator, [{"code": gd.PDNS_PRIMARY_GATE_CODE}])
                record.checks["dns_engine_card_text"] = engine_item["shown"]
                self.require_actionable(record)
                if plan.get("can_start"):
                    raise StepFailed("the plan carries pdns_primary_switch_paused but says can_start=true")
                raise GateRefused("PowerDNS primary refused by the server plan (pdns_primary_switch_paused); shown: "
                                  + " | ".join(item["shown"]["en"]))
            if plan_response.status != 200:
                # What the owner sees: ServerSetup.tsx shows setup.planFailed for any non-OK
                # plan response (pair1 P1: 500 {"code":"INTERNAL"} after an unrestarted kernel).
                item = gd.setup_plan_failure_guidance(self.translator, plan_response.status, plan_response.json())
                self.add_guidance(record, item, context="server plan review failed (ServerSetup review)",
                                  payload={"request": "POST /api/v1/setup/plan", "http_status": plan_response.status,
                                           "body": plan_response.json()})
                record.checks["plan_failure_shown"] = item["shown"]
                verdict = "actionable" if item["actionable"] else f"D-024: not actionable ({item['actionable_reason']})"
                raise StepFailed(f"reviewed plan returned HTTP {plan_response.status} "
                                 f"{plan.get('code') if isinstance(plan, dict) else ''}; the owner sees "
                                 f"{item['shown']['en']} / {item['shown']['tr']}; {verdict}")
            if not plan.get("can_start"):
                item = gd.plan_blocker_guidance(self.translator, blockers, code_keys)
                self.add_guidance(record, item, context="server plan blockers (ServerSetup review)",
                                  payload={"blockers": blockers})
                raise StepFailed(f"reviewed plan cannot start: blockers={blockers}; shown {item['shown']['en']}"
                                 + ("" if item["actionable"] else f"; D-024: {item['actionable_reason']}"))
            self.executions[f"plan-{role.role}"] = plan

    def refused_plan_left_nothing(self, record: StepRecord, role: topo.Role, client: Any,
                                  saved: dict[str, Any], engine_before: dict[str, Any]) -> None:
        """A refused plan must leave the saved draft and the DNS identity exactly as they were (read-only)."""

        after = client.get("/api/v1/setup", purpose="ServerSetupGate after refused plan").json() or {}
        engine_after = client.get("/api/v1/dns/engine", timeout=40, purpose="DNSEngineCard after refused plan").json() or {}
        self.record(f"refused-plan-state-{role.role}.json", {"setup": after, "dns_engine_before": engine_before,
                                                             "dns_engine_after": engine_after})
        changes = [f"setup.{key}: {saved.get(key)!r} -> {after.get(key)!r}"
                   for key in ("revision", "status", "draft") if saved.get(key) != after.get(key)]
        changes += [f"dns_engine.{key}: {engine_before.get(key)!r} -> {engine_after.get(key)!r}"
                    for key in DNS_IDENTITY_KEYS if engine_before.get(key) != engine_after.get(key)]
        record.checks["refused_plan_left_nothing"] = not changes
        if changes:
            raise StepFailed(f"the refused plan left changes behind: {changes}")

    def _setup_reader(self, request_id: str, record: StepRecord | None = None,
                      role: topo.Role | None = None) -> Callable[[Any], Any]:
        def read(view: Any) -> Any:
            response = view.get(f"/api/v1/setup/operation?request_id={request_id}")
            if response.status != 200:
                body = response.json()
                if record is not None and role is not None and isinstance(body, dict) \
                        and body.get("code") == gd.LICENSE_REQUIRED_CODE:
                    self.license_required_seen(view, record, role, f"setup operation HTTP {response.status}", body)
                return {"poll_error": f"HTTP {response.status}"}
            value = response.json()
            if record is not None and role is not None and gd.license_required_state(value):
                self.license_required_seen(view, record, role, "setup execution", value)
            return value
        return read

    def license_required_seen(self, view: Any, record: StepRecord, role: topo.Role, source: str,
                              payload: Any) -> None:
        """pair2 P-C: record a license_required read and the license status read right after it.

        Read-only (the poll's view). When the license status at that moment is
        active, the product told the owner a license is required while it was
        active: a D-024 product finding that does not fail the run.
        """

        observed_at = now_iso()
        status = view.get("/api/v1/panel/license", purpose="LicensePanel status after license_required (read-only)")
        body = status.json()
        entry = {
            "observed_at": observed_at,
            "license_status_read_at": now_iso(),
            "step": record.id,
            "node": role.node,
            "role": role.role,
            "source": source,
            "setup_state": ({k: payload.get(k) for k in ("status", "phase", "error")}
                            if isinstance(payload, dict) else payload),
            "license_status_http": status.status,
            "license_status": _license_summary(body),
        }
        self.license_required_observations.append(entry)
        record.checks.setdefault("license_required_observations", []).append(entry)
        if status.status != 200 or not isinstance(body, dict) or body.get("state") != "active":
            return
        finding_id = f"d024-license-required-while-active-{record.id}-{role.node}"
        finding = next((f for f in self.findings if f.get("id") == finding_id), None)
        if finding is None:
            item = gd.setup_execution_guidance(self.translator, payload) if isinstance(payload, dict) \
                and "status" in payload else gd.api_error_guidance(self.translator, 403, payload)
            finding = {
                "id": finding_id,
                "kind": "product",
                "principle": "D-024",
                "step": record.id,
                "node": role.node,
                "text": "setup reported license required while the license was active",
                "shown": item.get("shown"),
                "title": item.get("title"),
                "observations": [],
                "effect": "recorded only; the run continues",
            }
            self.findings.append(finding)
        finding["observations"].append(entry)

    def unknown_bound(self) -> dict[str, Any]:
        return {"bounded": gd.unknown_state_code, "bound_seconds": self.config.unknown_state_limit}

    def open_ended_unknown(self, record: StepRecord, role: topo.Role, execution: Any, poll: dict[str, Any]) -> str:
        """D-024 (pair2 P-B): a listed unknown state outlasted the limit; record it and stop waiting."""

        code = poll.get("unknown_state")
        item = gd.setup_execution_guidance(self.translator, execution) if isinstance(execution, dict) else {}
        first = next((g.get("observed_at") for g in record.guidance
                      if isinstance(g.get("code"), str) and g["code"].split(":")[0] == code), None)
        payload_file = self.record(f"open-ended-unknown-{role.role}.json", execution)
        finding = {
            "id": f"d024-open-ended-unknown-{record.id}-{role.node}",
            "kind": "product",
            "principle": "D-024",
            "step": record.id,
            "node": role.node,
            "code": code,
            "text": (f"open-ended unknown: setup stayed at {code} (result not confirmed) for "
                     f"{poll.get('unknown_seconds')}s over {poll.get('unknown_reads')} reads; the text shown names "
                     "no reason, no actor and no next action other than not starting it again"),
            "lasted_seconds": poll.get("unknown_seconds"),
            "reads": poll.get("unknown_reads"),
            "limit_seconds": poll.get("unknown_limit"),
            "first_observed_at": first,
            "stopped_at": now_iso(),
            "title": item.get("title"),
            "message_keys": item.get("message_keys"),
            "shown": item.get("shown"),
            "details": item.get("details"),
            "payload_file": payload_file,
            "payload": execution,
        }
        self.findings.append(finding)
        record.checks.setdefault("open_ended_unknown", []).append(
            {k: finding[k] for k in ("node", "code", "lasted_seconds", "reads", "limit_seconds", "payload_file")})
        return (f"{role.node}: setup stayed in the unknown state {code} for {poll.get('unknown_seconds')}s "
                f"(limit {poll.get('unknown_limit')}s); open-ended unknown (D-024), stopped waiting; payload "
                f"{payload_file}; shown: {' | '.join((item.get('shown') or {}).get('en') or [])}")

    def peer_start(self) -> dict[str, Any]:
        if self._peer_start is None:
            self._peer_start = gd.peer_start_keys(self.web_src / gd.WEB_SRC_FILES["setup_guidance"], "primary")
        return self._peer_start

    def check_peer_start_guidance(self, record: StepRecord, role: topo.Role, execution: dict[str, Any],
                                  item: dict[str, Any]) -> None:
        """pair2 t3: the primary told the owner to start the secondary while its own DNS step could not serve.

        The role text deliberately allows starting the secondary while the
        primary's setup is still progressing, so an ordinary running/pending
        DNS step is only recorded (``peer_start_guidance_observations``). The
        finding is raised only for a blocking DNS state
        (``guidance.dns_blocking_state``: failed, a listed unknown state, a
        waiting state at the DNS step, a rolled-back DNS result).
        """

        resolved = self.peer_start()
        record.checks.setdefault("peer_start_guidance_keys", {k: resolved[k] for k in ("state", "keys", "role_keys")})
        shown_keys = [key for key in item.get("message_keys") or [] if key in resolved["keys"]]
        if not shown_keys:
            return
        blocking = gd.dns_blocking_state(execution)
        entry = {"observed_at": item.get("observed_at") or now_iso(), "status": execution.get("status"),
                 "phase": execution.get("phase"), "dns_step_status": gd.dns_step_status(execution),
                 "error_code": (execution.get("error") or {}).get("code") if isinstance(execution.get("error"), dict)
                 else None, "blocking": blocking, "keys": shown_keys}
        record.checks.setdefault("peer_start_guidance_observations", []).append(entry)
        if blocking is None:
            return
        finding_id = f"d024-contradictory-peer-start-{record.id}-{role.node}"
        finding = next((f for f in self.findings if f.get("id") == finding_id), None)
        if finding is None:
            finding = {
                "id": finding_id,
                "kind": "product",
                "principle": "D-024",
                "step": record.id,
                "node": role.node,
                "text": ("the primary's guidance told the owner to start the secondary against this server while "
                         "the primary's own DNS step was in a state where the secondary cannot succeed (failed, "
                         "result unknown, waiting at the DNS step, or rolled back)"),
                "keys": shown_keys,
                "shown": {language: [text for key, text in zip(item.get("message_keys") or [], texts)
                                     if key in shown_keys] for language, texts in (item.get("shown") or {}).items()},
                "observations": [],
                "effect": "recorded only; the run continues",
            }
            self.findings.append(finding)
        finding["observations"].append(entry)

    def _observe_execution(self, record: StepRecord, role: topo.Role) -> Callable[[Any], None]:
        def changed(execution: Any) -> None:
            if not isinstance(execution, dict) or "poll_error" in execution:
                return
            self.record(f"execution-{role.role}-{len(record.guidance):03d}.json", execution)
            item = gd.setup_execution_guidance(self.translator, execution)
            item["phase"] = execution.get("phase")
            item["status"] = execution.get("status")
            self.add_guidance(record, item, context=f"setup execution on {role.node}")
            if role.role == "primary":
                self.check_peer_start_guidance(record, role, execution, record.guidance[-1])
        return changed

    def setup_start(self, record: StepRecord, role: topo.Role) -> None:
        plan = self.executions.get(f"plan-{role.role}")
        if not plan:
            raise StepFailed("no reviewed plan")
        request_id = hashlib.sha256(f"pair-accept:{self.config.run_id}:{role.node}".encode()).hexdigest()[:32]
        self.request_ids[role.role] = request_id
        with self.panel(role) as client:
            try:
                response = client.post("/api/v1/setup/start",
                                       {"plan_id": plan["id"], "request_id": request_id, "confirmed": True},
                                       purpose="ServerSetup start (once)", timeout=120)
                if response.status not in (200, 202):
                    self.start_refused(record, role, client, response, request_id, "setup start refused")
            except UnknownOutcome as exc:
                # Reconcile the exact request by reads; never re-POST blindly.
                record.checks["start_outcome"] = f"unknown, reconciling: {exc}"
            done, execution, reads = client.poll(
                self._setup_reader(request_id, record, role),
                lambda ex: isinstance(ex, dict) and (ex.get("status") in {"failed", "succeeded"} or _dns_step_done(ex)),
                timeout=self.config.setup_timeout, interval=self.config.poll_interval,
                on_change=self._observe_execution(record, role), **self.stable_stop(pc.setup_settled),
                **self.unknown_bound(),
            )
            poll = dict(client.last_poll)
        record.checks["polls"] = reads
        record.checks["poll_stop"] = poll
        self.executions[role.role] = execution if isinstance(execution, dict) else {}
        if not isinstance(execution, dict) or execution.get("request_id") not in (None, request_id):
            raise StepFailed("setup operation could not be reconciled to the exact request")
        if poll.get("stop") == "unknown-limit":
            raise ProductFailure(self.open_ended_unknown(record, role, execution, poll))
        if not done:
            self.require_actionable(record)
            if poll.get("stop") == "stable":
                raise StepFailed(f"setup DNS step did not finish: the operation was unchanged for "
                                 f"{poll.get('stable_seconds')}s at status {execution.get('status')} phase "
                                 f"{execution.get('phase')} ({(execution.get('error') or {}).get('code')})")
            raise StepFailed(f"setup DNS step did not finish within {self.config.setup_timeout}s")
        self.require_actionable(record)
        if execution.get("status") == "failed" and not _dns_step_done(execution):
            raise StepFailed(f"setup failed before its DNS step: {execution.get('error')}")

    def start_refused(self, record: StepRecord, role: topo.Role, client: Any, response: Any, request_id: str,
                      context: str) -> None:
        """A non-2xx start: record what the wizard shows, reconcile the exact request once by a read, stop."""

        body = response.json()
        item = gd.setup_start_failure_guidance(self.translator, response.status, body)
        self.add_guidance(record, item, context=context,
                          payload={"request": "POST /api/v1/setup/start", "http_status": response.status, "body": body})
        with client.polling() as view:
            reconciled = view.get(f"/api/v1/setup/operation?request_id={request_id}")
        record.checks["start_refused"] = {"http_status": response.status, "shown": item["shown"],
                                          "reconcile_http_status": reconciled.status,
                                          "execution_exists": isinstance(reconciled.json(), dict)}
        verdict = "actionable" if item["actionable"] else f"D-024: not actionable ({item['actionable_reason']})"
        raise StepFailed(f"setup start on {role.node} returned HTTP {response.status}; the owner sees "
                         f"{item['shown']['en']}; {verdict}")

    def secondary_without_primary(self, record: StepRecord) -> None:
        """Gate path: the secondary's own setup must explain the missing primary (D-024)."""

        role = self.topology.secondary
        self.setup_review(record, role)
        plan = self.executions.get(f"plan-{role.role}")
        request_id = hashlib.sha256(f"pair-accept:{self.config.run_id}:{role.node}".encode()).hexdigest()[:32]
        with self.panel(role) as client:
            response = client.post("/api/v1/setup/start",
                                   {"plan_id": plan["id"], "request_id": request_id, "confirmed": True},
                                   purpose="ServerSetup start (once)", timeout=120)
            if response.status not in (200, 202):
                self.start_refused(record, role, client, response, request_id, "secondary setup start refused")
            done, execution, reads = client.poll(
                self._setup_reader(request_id, record, role),
                lambda ex: isinstance(ex, dict) and (ex.get("status") in {"failed", "succeeded", "waiting"}),
                timeout=min(self.config.setup_timeout, 1800), interval=self.config.poll_interval,
                on_change=self._observe_execution(record, role), **self.unknown_bound(),
            )
            poll = dict(client.last_poll)
        record.checks["secondary_final"] = {k: (execution or {}).get(k) for k in ("status", "phase", "error")}
        if poll.get("stop") == "unknown-limit":
            raise ProductFailure(self.open_ended_unknown(record, role, execution, poll))
        if not done:
            raise StepFailed("secondary setup did not reach a waiting or terminal state")
        self.require_actionable(record)

    def pair_ready(self, record: StepRecord) -> None:
        """D1 on both Panels (pair_contract), then each setup settles.

        Both Panels are always read, so a failure records both payloads. A
        stable snapshot that does not satisfy D1 stops the wait early
        (pair1: t1 r2 waited 45 minutes on an unchanging secondary snapshot).
        A secondary of a build that does not report ``secondary_ready`` is
        ``blocked-product`` when nothing else failed.
        """

        P, S = self.topology.primary, self.topology.secondary
        deadline_total = self.config.setup_timeout
        results: dict[str, Any] = {}
        failures: list[str] = []
        product_failures: list[str] = []
        blocked: list[str] = []
        for role in (P, S):
            with self.panel(role) as client:
                verdict = self.wait_pair_readiness(record, role, client, timeout=deadline_total, label="ready")
                results[role.role] = verdict["observed"]
                if verdict.get("blocked"):
                    blocked.append(self.readiness_failure(role, verdict))
                    continue
                if not verdict["passed"]:
                    failures.append(self.readiness_failure(role, verdict))
                    continue
                settled, execution, _ = client.poll(
                    self._setup_reader(self.request_ids[role.role], record, role),
                    lambda ex: isinstance(ex, dict) and (ex.get("status") in {"failed", "succeeded"}
                                                          or (ex.get("status") == "waiting" and ex.get("phase") in NON_DNS_SETUP_PHASES)),
                    timeout=deadline_total, interval=self.config.poll_interval,
                    on_change=self._observe_execution(record, role), **self.stable_stop(pc.setup_settled),
                    **self.unknown_bound(),
                )
                self.executions[role.role] = execution
                record.checks[f"setup_{role.role}_final"] = {k: (execution or {}).get(k) for k in ("status", "phase", "error")}
                record.checks[f"setup_{role.role}_poll"] = dict(client.last_poll)
                if client.last_poll.get("stop") == "unknown-limit":
                    product_failures.append(self.open_ended_unknown(record, role, execution, client.last_poll))
                    continue
                if not settled:
                    failures.append(f"{role.node} setup did not settle ({client.last_poll.get('stop')}): "
                                    f"{record.checks[f'setup_{role.role}_final']}")
                    continue
                failed_steps = [s for s in (execution.get("steps") or []) if s.get("status") == "failed"]
                if execution.get("status") == "failed" and any(s.get("kind") in {"dns", "infrastructure_dns", "firewall", "service"} for s in failed_steps):
                    failures.append(f"{role.node} setup failed at {[s.get('id') for s in failed_steps]}: {execution.get('error')}")
                    continue
                if execution.get("status") != "succeeded":
                    self.findings.append({
                        "id": f"setup-not-complete-offline-{role.role}",
                        "text": f"{role.node} setup ended {execution.get('status')} at phase {execution.get('phase')} "
                                f"({(execution.get('error') or {}).get('code')}); public hostname DNS and the panel "
                                "certificate cannot complete on an isolated host. The DNS pair steps completed.",
                    })
        self.record("engine-ready.json", results)
        self.snapshots["engine_ready"] = results
        self.require_actionable(record)
        if failures:
            raise StepFailed(" | ".join(failures + product_failures + blocked))
        if product_failures:
            raise ProductFailure(" | ".join(product_failures + blocked))
        if blocked:
            raise ProductBlocked(" | ".join(blocked))

    def zone_add(self, record: StepRecord, *, readd: bool = False) -> None:
        zone = self.topology.zone
        with self.panel(self.topology.primary) as client:
            response = client.post("/api/v1/domains/create",
                                   {"domain": zone, "project_type": "dnsonly", "ssl_type": "none"},
                                   purpose="AddDomainModal create (dnsonly)", timeout=300)
            body = response.json() or {}
            if response.status != 200:
                item = gd.api_error_guidance(self.translator, response.status, body,
                                             pending=body.get("partial_success") is True)
                self.add_guidance(record, item, context="domain create")
                self.require_actionable(record)
                raise StepFailed(f"domain create returned HTTP {response.status} ({body.get('code')})")
            domain_id = body.get("DomainID") or body.get("domain_id")
            if not isinstance(domain_id, int) or domain_id <= 0:
                raise StepFailed(f"domain create returned no domain identity: {sorted(body)}")
            if readd and domain_id == self.domain_id:
                raise StepFailed("re-added domain reused the deleted domain's identity")
            self.domain_id = domain_id
            listing = client.get("/api/v1/domains", purpose="Domains list").json() or []
            record.checks["domain_listed"] = any(row.get("id") == domain_id for row in listing if isinstance(row, dict))
            zone_info = client.get(f"/api/v1/domains/{domain_id}/dns/zone", purpose="DomainDNSManager zone")
            records = client.get(f"/api/v1/domains/{domain_id}/dns/records", purpose="DomainDNSManager records").json() or {}
        if not record.checks["domain_listed"] or zone_info.status != 200:
            raise StepFailed("the Panel does not show the new domain and its zone")
        expected = _expected_from_panel(records.get("records") or [], zone)
        if readd:
            expected[(f"{RECORD_LABEL}.{zone}", "A")] = set()
        self.record("panel-records.json", records)
        verdict = self.wait_present(record, "readd" if readd else "add", expected, None)
        self.serial = verdict["serial"]
        self.native(record, "readd" if readd else "add", expect_present=True)
        self.engine_snapshot(record, "after-readd" if readd else "after-add")

    def record_add(self, record: StepRecord) -> None:
        zone = self.topology.zone
        with self.panel(self.topology.primary) as client:
            response = client.post(f"/api/v1/domains/{self.domain_id}/dns/records",
                                   {"name": RECORD_LABEL, "type": "A", "content": RECORD_ADDRESSES[0],
                                    "ttl": RECORD_TTL, "prio": 0},
                                   purpose="DomainDNSManager addRecord", timeout=300)
            self._require_publication(record, response, "record add")
        verdict = self.wait_present(record, "record-add", {(f"{RECORD_LABEL}.{zone}", "A"): {RECORD_ADDRESSES[0]}},
                                    self.serial)
        self.serial = verdict["serial"]

    def record_edit(self, record: StepRecord) -> None:
        zone = self.topology.zone
        owner = f"{RECORD_LABEL}.{zone}"
        with self.panel(self.topology.primary) as client:
            listing = client.get(f"/api/v1/domains/{self.domain_id}/dns/records", purpose="DomainDNSManager records").json() or {}
            matches = [r for r in listing.get("records") or []
                       if r.get("type") == "A" and str(r.get("name", "")).rstrip(".").lower() == owner
                       and r.get("content") == RECORD_ADDRESSES[0]]
            if len(matches) != 1:
                raise StepFailed(f"expected exactly one {owner} A {RECORD_ADDRESSES[0]} in the Panel, found {len(matches)}")
            record_id = matches[0]["id"]
            if self.config.edit_method == "api-put":
                response = client.put(f"/api/v1/domains/{self.domain_id}/dns/records",
                                      {"id": record_id, "content": RECORD_ADDRESSES[1], "ttl": RECORD_TTL,
                                       "prio": 0, "disabled": False},
                                      purpose="record edit (API-only PUT)", timeout=300)
                self._require_publication(record, response, "record edit")
            else:
                removed = client.delete(f"/api/v1/domains/{self.domain_id}/dns/records?id={record_id}",
                                        purpose="DomainDNSManager deleteRecord", timeout=300)
                self._require_publication(record, removed, "record delete (edit)")
                added = client.post(f"/api/v1/domains/{self.domain_id}/dns/records",
                                    {"name": RECORD_LABEL, "type": "A", "content": RECORD_ADDRESSES[1],
                                     "ttl": RECORD_TTL, "prio": 0},
                                    purpose="DomainDNSManager addRecord (edit)", timeout=300)
                self._require_publication(record, added, "record add (edit)")
        verdict = self.wait_present(record, "record-edit", {(owner, "A"): {RECORD_ADDRESSES[1]}}, self.serial)
        self.serial = verdict["serial"]

    def _require_publication(self, record: StepRecord, response: Any, context: str) -> None:
        if response.status == 200:
            return
        body = response.json()
        item = gd.api_error_guidance(self.translator, response.status, body,
                                     pending=isinstance(body, dict) and body.get("code") == "DNS_PUBLICATION_FAILED")
        self.add_guidance(record, item, context=context)
        self.require_actionable(record)
        raise StepFailed(f"{context} returned HTTP {response.status} ({(body or {}).get('code') if isinstance(body, dict) else ''}"
                         f"/{(body or {}).get('reason') if isinstance(body, dict) else ''})")

    def zone_delete(self, record: StepRecord) -> None:
        domain_id = self.domain_id
        with self.panel(self.topology.primary) as client:
            response = client.delete(f"/api/v1/domains/{domain_id}", purpose="Domains handleDelete", timeout=600)
            outcome = self._deletion_outcome(record, client, response, "domain delete")
        record.checks["delete_first_outcome"] = outcome
        if outcome["state"] == "pending":
            reason = outcome.get("reason")
            if reason == "dns_peer_enrollment_required":
                self.pending_deletion_engine_observation(record)
                self._owner_enrollment(record)
                with self.panel(self.topology.primary) as client:
                    retry = client.delete(f"/api/v1/domains/{domain_id}",
                                          purpose="Domains handleDelete (owner retries the same deletion)", timeout=600)
                    outcome = self._deletion_outcome(record, client, retry, "domain delete after enrollment")
                    if outcome["state"] == "pending":
                        done, last, _ = client.poll(
                            lambda view: {"status": view.get(f"/api/v1/domains/{domain_id}/deletion-status").status},
                            lambda value: value.get("status") in (204, 404),
                            timeout=self.config.deletion_timeout, interval=10,
                        )
                        record.checks["deletion_status_poll"] = last
                        if not done:
                            raise StepFailed("deletion stayed pending after owner enrollment and one retry")
                        outcome = {"state": "succeeded", "via": "deletion marker cleared"}
                record.checks["delete_final_outcome"] = outcome
            else:
                self.require_actionable(record)
                raise StepFailed(f"deletion pending with reason {reason!r}; no documented owner action applies")
        if outcome["state"] == "error":
            raise StepFailed(f"domain delete failed: {outcome}")
        with self.panel(self.topology.primary) as client:
            listing = client.get("/api/v1/domains", purpose="Domains list").json() or []
        if any(isinstance(row, dict) and row.get("id") == domain_id for row in listing):
            raise StepFailed("the Panel still lists the deleted domain")
        servers = [self.topology.primary.address, self.topology.secondary.address]
        deadline = self.clock() + self.config.dns_timeout
        attempts = 0
        while True:
            attempts += 1
            observations = self.observe_dns([(self.topology.zone, "SOA")])
            absent = dc.evaluate_absent_dns(observations, servers=servers, zone=self.topology.zone)
            if absent["passed"] or self.clock() >= deadline:
                break
            self.sleep(self.config.dns_interval)
        self.record("dns-delete.json", observations)
        absent["attempts"] = attempts
        record.checks["dns_absent"] = absent
        self.native(record, "delete", expect_present=False)
        if not absent["passed"]:
            raise StepFailed(f"a server still answers authoritatively: {absent['reasons']}")
        self.engine_snapshot(record, "after-delete")
        self.require_actionable(record)

    def _deletion_outcome(self, record: StepRecord, client: PanelClient, response: Any, context: str) -> dict[str, Any]:
        if response.status in (200, 204):
            return {"state": "succeeded", "http_status": response.status, "body": response.json()}
        if response.status == 202:
            item = gd.deletion_pending_guidance(self.translator, 202, response.json())
            self.add_guidance(record, item, context=context)
            saved = client.get(f"/api/v1/domains/{self.domain_id}/deletion-status",
                               purpose="Domains restorePendingDeletions (read-only)")
            saved_item = gd.deletion_pending_guidance(self.translator, saved.status, saved.json(), saved=True)
            self.add_guidance(record, saved_item, context=f"{context}: saved deletion status")
            return {"state": "pending", "reason": item.get("reason"), "stage": item.get("stage")}
        item = gd.api_error_guidance(self.translator, response.status, response.json())
        self.add_guidance(record, item, context=context)
        return {"state": "error", "http_status": response.status}

    def pending_deletion_engine_observation(self, record: StepRecord) -> None:
        """Item 7 (pair1 P3): record, never fail, when the pending-deletion text for a
        PowerDNS secondary does not name the engine selector the owner tool needs."""

        if self.topology.secondary.engine != "pdns":
            return
        texts = [item for item in record.guidance if item.get("reason") == "dns_peer_enrollment_required"]
        if not texts:
            return
        shown = texts[0].get("shown") or {}
        english = " ".join(shown.get("en") or [])
        # The Panel text names the tool, its steps and the engine selector; the exact
        # per-command flags (such as --catalog-account) belong to the tool's README.
        missing = [flag for flag in ("--engine pdns",) if flag not in english]
        if not missing:
            return
        observation = {
            "id": "d024-observation-pending-deletion-pdns-engine-selector",
            "kind": "observation",
            "step": record.id,
            "text": "The pending-deletion guidance shown for a PowerDNS secondary does not name "
                    f"{' / '.join(missing)}, which cmd/dns-peer-enroll/README.md requires on every command for a "
                    "PowerDNS secondary. Recorded for the owner's wording decision; the run continues.",
            "missing": missing,
            "message_keys": texts[0].get("message_keys"),
            "shown": shown,
        }
        record.checks.setdefault("d024_observations", []).append(observation)
        if observation not in self.findings:
            self.findings.append(observation)

    def _owner_enrollment(self, record: StepRecord) -> None:
        """What the product asks both owners to do: dns-peer-enroll with the secondary's engine.

        A PowerDNS secondary uses --engine pdns on every command, the packaged
        pdns-peer-inspect, and --catalog-account read read-only from the
        secondary's own PowerDNS database (the catalog CONSUMER row).
        """

        P, S = self.topology.primary, self.topology.secondary
        catalog = dc.catalog_name(P.address)
        # Native state first, so the evidence shows what the secondary held while pending.
        pending_native: dict[str, dict[str, Any]] = {}
        for role in (P, S):
            pending_native[role.role] = self.probe(role.node, "native", "--engine", role.engine, "--zone",
                                                   self.topology.zone, "--catalog", catalog)
            self.record(f"native-pending-{role.role}.json", pending_native[role.role])
        engine = S.engine
        catalog_account = ""
        if engine == "pdns":
            rows = ((pending_native["secondary"].get("database") or {}).get("domains") or [])
            accounts = [row.get("account", "") for row in rows if isinstance(row, dict)
                        and str(row.get("name", "")).rstrip(".").lower() == catalog.rstrip(".").lower()
                        and str(row.get("type", "")).upper() == "CONSUMER"]
            record.checks["pdns_catalog_consumer_account"] = accounts
            if len(accounts) != 1 or not accounts[0]:
                raise StepFailed(f"the PowerDNS secondary has no single catalog CONSUMER zone account for {catalog}: "
                                 f"{accounts}; the owner cannot fill --catalog-account")
            catalog_account = accounts[0]
        root = self.config.dist_root
        transcript: list[dict[str, Any]] = []

        def owner(node: str, command: str, *, stdin: bytes | None = None) -> dict[str, Any]:
            completed = self.guests.run(node, command, mutating=True, stdin=stdin, timeout=300, check=False)
            entry = {"node": node, "command": command, "returncode": completed.returncode,
                     "stdout": _decode(completed.stdout)[-4000:], "stderr": _decode(completed.stderr)[-4000:]}
            transcript.append(entry)
            if completed.returncode != 0:
                self.record("owner-enrollment-transcript.json", transcript)
                raise StepFailed(f"owner enrollment command failed on {node}: {command.split()[2:4]}")
            try:
                return json.loads(entry["stdout"].strip().splitlines()[-1])
            except (ValueError, IndexError):
                return {}

        prepared = owner(P.node, inst.enroll_primary_prepare(root, engine))
        public_key = prepared.get("public_key", "")
        if not public_key.startswith("ssh-ed25519 "):
            raise StepFailed("primary-prepare returned no Ed25519 public key")
        owner(S.node, inst.write_primary_public_key(), stdin=(public_key.strip() + "\n").encode())
        owner(S.node, inst.enroll_secondary_install(root, primary_ip=P.address, peer_ip=S.address, catalog=catalog,
                                                    engine=engine, catalog_account=catalog_account))
        host_key = owner(S.node, inst.enroll_secondary_host_key(root, engine)).get("host_key_sha256", "")
        # Independent review through the fixture's pinned SSH channel.
        pub = self.guests.run(S.node, "cat /etc/ssh/ssh_host_ed25519_key.pub", mutating=False, check=False)
        reviewed = pubkey_digest(_decode(pub.stdout))
        known = None
        plan = getattr(self.guests, "plan", None)
        if plan:
            known = known_host_digest(Path(plan["nodes"][S.node]["paths"]["directory"]).parent / "ssh-known-hosts",
                                      int(plan["nodes"][S.node]["management"]["ssh_port"]))
        record.checks["host_key_review"] = {"cli": host_key, "pub_file": reviewed, "fixture_known_hosts": known}
        if not host_key or host_key != reviewed or (known is not None and known != host_key):
            self.record("owner-enrollment-transcript.json", transcript)
            raise StepFailed("secondary host-key digest failed independent review; not activating")
        owner(P.node, inst.enroll_primary_activate(root, credential_id=prepared.get("credential_id", ""),
                                                   primary_ip=P.address, peer_ip=S.address, catalog=catalog,
                                                   host_key_sha256=host_key, engine=engine))
        owner(P.node, inst.enroll_status(root, "primary", engine))
        owner(S.node, inst.enroll_status(root, "secondary", engine))
        self.record("owner-enrollment-transcript.json", transcript)
        label = "PowerDNS" if engine == "pdns" else "BIND"
        record.checks["owner_enrollment"] = f"completed with dns-peer-enroll ({label} secondary)"
        self.owner_steps.append({
            "step": record.id,
            "node": f"{P.node}+{S.node}",
            "actor": "primary administrator together with the secondary owner",
            "action": f"dns-peer-enroll{' --engine pdns' if engine == 'pdns' else ''}: primary-prepare, "
                      "secondary-install, secondary-host-key (independently reviewed), primary-activate, status",
            "trigger": "deletion pending: dns_peer_enrollment_required",
            "transcript": "owner-enrollment-transcript.json",
        })

    def zone_readd(self, record: StepRecord) -> None:
        self.zone_add(record, readd=True)

    def independence(self, record: StepRecord) -> None:
        P, S = self.topology.primary, self.topology.secondary
        before = {role.node: self.probe(role.node, "ledger") for role in (P, S)}
        self.record("ledger-before-disable.json", before)
        self.snapshots["before_disable"] = self._panel_truth(record, "before-disable")
        for role in (P, S):
            for unit in ("celikpanel-panel.service", "celikpanel-agent.service"):
                self.record_text(f"journal-{role.node}-{unit}-pre-reboot.txt", self.guests.journal(role.node, unit, boots="current"))
        for role in (P, S):
            self.guests.disable_management(role.node)
        reboots = {}
        for role in (P, S):
            reboots[role.node] = self.guests.reboot(role.node, self.config.reboot_timeout)
        self.record("reboots.json", reboots)
        problems = []
        for role in (P, S):
            state = self.probe(role.node, "management")
            self.record(f"management-after-reboot-{role.node}.json", state)
            units = state.get("units") or {}
            for unit in ("celikpanel-panel.service", "celikpanel-agent.service"):
                if (units.get(unit) or {}).get("active") == "active":
                    problems.append(f"{role.node}: {unit} is active with management disabled")
            dns_unit = DNS_UNIT[(role.engine, role.node)]
            if (units.get(dns_unit) or {}).get("active") != "active":
                problems.append(f"{role.node}: {dns_unit} is not active after reboot")
        after = {role.node: self.probe(role.node, "ledger") for role in (P, S)}
        self.record("ledger-after-reboot.json", after)
        for node in before:
            if before[node].get("sha256") != after[node].get("sha256"):
                problems.append(f"{node}: mutation ledger changed while management was disabled")
        if problems:
            raise StepFailed("; ".join(problems))
        self.wait_present(record, "management-absent", {(f"{RECORD_LABEL}.{self.topology.zone}", "A"): set()}, None)
        self.native(record, "management-absent", expect_present=True)
        record.checks["boot_ids"] = {node: [value.get("boot_id_before"), value.get("boot_id_after")]
                                     for node, value in reboots.items()}

    def _panel_truth(self, record: StepRecord, label: str) -> dict[str, Any]:
        truth: dict[str, Any] = {}
        for role in self.topology.roles:
            with self.panel(role) as client:
                with client.polling() as view:
                    engine = view.get("/api/v1/dns/engine", timeout=40)
                    domains = view.get("/api/v1/domains")
                    setup = view.get(f"/api/v1/setup/operation?request_id={self.request_ids.get(role.role, '')}")
            engine_body = engine.json() or {}
            truth[role.role] = {
                "engine": {k: engine_body.get(k, pc.ABSENT) for k in ENGINE_SUMMARY_KEYS},
                "domains": sorted(row.get("domain_name") for row in (domains.json() or []) if isinstance(row, dict)),
                "setup": {k: (setup.json() or {}).get(k) for k in ("id", "status", "phase")} if setup.status == 200 else setup.status,
            }
        self.record(f"panel-truth-{label}.json", truth)
        return truth

    def management_return(self, record: StepRecord) -> None:
        P, S = self.topology.primary, self.topology.secondary
        before = {role.node: self.probe(role.node, "ledger") for role in (P, S)}
        self.record("ledger-before-enable.json", before)
        for role in (P, S):
            self.guests.enable_management(role.node)
        for role in (P, S):
            deadline = self.clock() + 180
            while True:
                tls = self.probe(role.node, "tls")
                if "leaf_sha256" in tls or self.clock() >= deadline:
                    break
                self.sleep(3)
            if tls.get("leaf_sha256") != self.pins.get(role.node):
                raise StepFailed(f"{role.node}: panel TLS leaf changed or panel did not return: {tls}")
        failures = []
        blocked = []
        for role in (P, S):
            with self.panel(role) as client:
                client.login(ADMIN_USERNAME, self.passwords[role.node])
                verdict = self.wait_pair_readiness(record, role, client, timeout=240, label="return")
                if verdict.get("blocked"):
                    blocked.append(self.readiness_failure(role, verdict))
                elif not verdict["passed"]:
                    failures.append(self.readiness_failure(role, verdict))
        if failures:
            raise StepFailed("after management returned: " + " | ".join(failures + blocked))
        if blocked:
            raise ProductBlocked("after management returned: " + " | ".join(blocked))
        truth = self._panel_truth(record, "after-return")
        prior = self.snapshots.get("before_disable") or {}
        differences = []
        for role_name, value in truth.items():
            expected = prior.get(role_name) or {}
            for key in ("active_engine", "topology", "pair_role", "pair_ready", "secondary_ready"):
                if value["engine"].get(key) != (expected.get("engine") or {}).get(key):
                    differences.append(f"{role_name}.{key}: {expected.get('engine', {}).get(key)} -> {value['engine'].get(key)}")
            if value["domains"] != expected.get("domains"):
                differences.append(f"{role_name}.domains: {expected.get('domains')} -> {value['domains']}")
            if value["setup"] != expected.get("setup"):
                record.checks.setdefault("setup_state_changes", []).append(
                    {"role": role_name, "before": expected.get("setup"), "after": value["setup"]})
        after = {role.node: self.probe(role.node, "ledger") for role in (P, S)}
        self.record("ledger-after-return.json", after)
        for node in before:
            if before[node].get("sha256") != after[node].get("sha256"):
                differences.append(f"{node}: Agent mutation ledger changed after management returned "
                                   f"(start-up or a read admitted a mutation)")
        if differences:
            raise StepFailed("; ".join(differences))

    def collect(self, record: StepRecord) -> None:
        for role in self.topology.roles:
            node = role.node
            if node not in self.identity_ok:
                # Never touch a guest, even read-only, that did not prove it is this cell's own.
                self.evidence.write_text(f"guests/{node}/not-collected.txt",
                                         "guest identity was not verified in preflight; nothing collected")
                record.checks[f"collected_{node}"] = False
                continue
            for unit in JOURNAL_UNITS:
                read = True
                try:
                    text = self.guests.journal(node, unit)
                except Exception as exc:  # noqa: BLE001 - collection is best effort and says so
                    read = False
                    text = f"journal collection failed: {type(exc).__name__}: {exc}"
                self.evidence.write_text(f"guests/{node}/journal-{unit}.txt", text or "(empty)")
                if unit == PANEL_UNIT:
                    check = license_service_journal_check(text if read else None)
                    self.license_journal[node] = check
                    record.checks[f"license_service_journal_{node}"] = {
                        k: v for k, v in check.items() if not k.endswith("_lines")}
                    self.evidence.write_json(f"guests/{node}/license-service-journal-check.json", check)
            for command in ("versions", "management", "ledger"):
                try:
                    value = self.probe(node, command)
                except Exception as exc:  # noqa: BLE001
                    value = {"error": f"{type(exc).__name__}: {exc}"}
                self.evidence.write_json(f"guests/{node}/{command}.json", value)
        if self.config.license_mode == "acceptance-fixture":
            contacted = {node: {"host_lines": check["host_line_count"], "refusal_lines": check["refusal_line_count"]}
                         for node, check in self.license_journal.items() if check["state"] == "contact-or-attempt-seen"}
            if contacted:
                raise StepFailed(f"acceptance fixture build: the Panel journal names the license service or its "
                                 f"refusing transport: {contacted}")


def license_service_journal_check(text: str | None) -> dict[str, Any]:
    """Read-only proxy for the in-process refused-connection counter (item 6).

    ``licensing.AcceptanceLicenseServiceDialAttempts`` is not exposed by any
    API or log, so the driver cannot read it. Instead: no Panel journal line
    may contain the license service host or the refusing transport's exact
    error text. The limit: an attempt whose error was never logged leaves no
    line, so a clean journal is necessary, not sufficient.
    """

    if text is None:
        return {"state": "unknown", "why": "the Panel journal could not be collected", "host": LICENSE_SERVICE_HOST,
                "refusal_text": LICENSE_REFUSAL_TEXT, "host_line_count": None, "refusal_line_count": None,
                "acceptance_banner_count": None, "host_lines": [], "refusal_lines": []}
    lines = text.splitlines()
    host = [line for line in lines if LICENSE_SERVICE_HOST in line.lower()]
    refusal = [line for line in lines if LICENSE_REFUSAL_TEXT in line]
    return {
        "state": "contact-or-attempt-seen" if host or refusal else "no-line-found",
        "why": "journal lines naming the license service host or the refusing transport's error",
        "host": LICENSE_SERVICE_HOST,
        "refusal_text": LICENSE_REFUSAL_TEXT,
        "journal_lines": len(lines),
        "host_line_count": len(host),
        "refusal_line_count": len(refusal),
        "acceptance_banner_count": sum(ACCEPTANCE_BANNER_TEXT in line for line in lines),
        "host_lines": host[:5],
        "refusal_lines": refusal[:5],
        "limit": "the in-process counter is not exposed; an unlogged attempt leaves no journal line",
    }


def _decode(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace")
    return str(value)


LICENSE_SUMMARY_KEYS = ("state", "observation", "can_provision", "product", "license_id", "expires_at",
                        "offline_until", "license_kind", "license_label", "license_service", "acceptance_guest",
                        "acceptance_cell", "acceptance_node", "acceptance_smbios_uuid")


def _license_summary(status: Any) -> dict[str, Any]:
    status = status if isinstance(status, dict) else {}
    return {key: status.get(key) for key in LICENSE_SUMMARY_KEYS if key in status}


def _dns_step_done(execution: dict[str, Any]) -> bool:
    return any(step.get("kind") == "dns" and step.get("status") == "succeeded"
               for step in execution.get("steps") or [] if isinstance(step, dict))


def _expected_from_panel(records: list[dict[str, Any]], zone: str) -> dict[tuple[str, str], set[str]]:
    expected: dict[tuple[str, str], set[str]] = {}
    for item in records:
        if not isinstance(item, dict) or item.get("disabled") or item.get("type") not in {"A", "AAAA"}:
            continue
        name = str(item.get("name", "")).rstrip(".").lower() or zone
        expected.setdefault((name, item["type"]), set()).add(str(item.get("content")))
    return expected


# ---------------------------------------------------------------------------
# Command line
# ---------------------------------------------------------------------------

API_SEQUENCE = [
    ("login", "POST /api/v1/auth/login, GET /api/v1/auth/me, GET /api/v1/panel/availability"),
    ("license", "GET /api/v1/license/access; locked: GET /api/v1/setup -> 403 license_required; "
                "owner-key: POST /api/v1/panel/license {action:activate}; acceptance-fixture: GET "
                "/api/v1/panel/license (label), POST {action:activate, fixture key} once, GET both again"),
    ("setup-review", "GET /api/v1/setup, PUT /api/v1/setup/guidance, PUT /api/v1/setup, POST /api/v1/setup/plan"),
    ("setup-start", "POST /api/v1/setup/start (once, fixed request_id), poll GET /api/v1/setup/operation?request_id="),
    ("pair-ready", "poll GET /api/v1/dns/engine on both Panels until D1 holds (primary: pair_ready true, "
                   "secondary_ready absent; secondary: secondary_ready true, pair_ready false); a stable snapshot "
                   "that does not satisfy it stops early; a secondary without secondary_ready is blocked-product "
                   "(this build does not report secondary readiness); poll setup operation until settled"),
    ("zone-add", "POST /api/v1/domains/create {project_type:dnsonly}, GET /api/v1/domains, GET .../dns/zone, GET .../dns/records"),
    ("record-add", "POST /api/v1/domains/{id}/dns/records"),
    ("record-edit", "ui-replace: DELETE .../dns/records?id= then POST; api-put: PUT .../dns/records"),
    ("zone-delete", "DELETE /api/v1/domains/{id}; 202 -> GET .../deletion-status (read-only); owner enrollment "
                    "(dns-peer-enroll, --engine pdns for a PowerDNS secondary) then the same DELETE once"),
    ("zone-readd", "POST /api/v1/domains/create"),
    ("independence-reboot", "systemctl disable --now panel+agent; fixture reboot of both; native serving checks"),
    ("management-return", "systemctl enable --now agent+panel; login; read-only GETs (D1 again); ledger digest "
                          "unchanged"),
]


def build_plan(args: argparse.Namespace, topology: topo.Topology) -> dict[str, Any]:
    cell = topology.cell_id(args.run_label)
    fixture = "deploy/e2e/dns-kill-matrix/fixture.py"
    return {
        "schema": DRIVER_SCHEMA,
        "dry_run": True,
        "topology": topology.as_dict(),
        "cell_id": cell,
        "fixture_commands": [
            ["python3", fixture, "prepare", "--work-root", str(args.work_root), "--cell-id", cell,
             "--ssh-public-key", "<identity>.pub", "--execute"],
            ["python3", fixture, "start", "--work-root", str(args.work_root), "--cell-id", cell, "--execute"],
            ["python3", fixture, "wait-ssh", "--work-root", str(args.work_root), "--cell-id", cell,
             "--identity-file", "<identity>", "--execute"],
        ],
        "license_mode": args.license_mode,
        "native_evidence_scope": NATIVE_EVIDENCE_SCOPE[args.license_mode],
        "edit_method": args.edit_method,
        "steps": [{"step": name, "api": api} for name, api in API_SEQUENCE],
        "expected_outcome_note": (
            "pdns-primary/bind-secondary is expected to end refused-by-product-gate "
            "(server plan blocker pdns_primary_switch_paused) while its gate is closed; --license-mode none is "
            "expected to end blocked-product at license; acceptance-fixture runs do not evidence license behaviour; "
            "D1 needs secondary_ready on the secondary's GET /api/v1/dns/engine and none on the primary's; a build "
            "that does not report it (pair1, aa6b9380) ends pair-ready blocked-product; a listed unknown setup "
            "state (" + ", ".join(gd.UNKNOWN_STATE_CODES) + ") lasting longer than "
            f"{args.unknown_state_limit_seconds:g}s fails the step with cause product (D-024 open-ended unknown)"
        ),
        "unknown_state_limit_seconds": args.unknown_state_limit_seconds,
        "unknown_state_codes": list(gd.UNKNOWN_STATE_CODES),
        "safety": [
            "guests are reached only through the fixture plan's loopback SSH ports",
            "every mutating guest command re-checks the SMBIOS UUID and fixture marker",
            "the Panel is reached through a loopback SSH tunnel with a pinned TLS leaf",
            "status polls cannot issue unsafe HTTP methods",
            "secrets are redacted before capture and a surviving registered secret aborts the write",
        ],
    }


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="action", required=True)
    for name in ("plan", "run"):
        current = sub.add_parser(name)
        current.add_argument("--topology", required=True, choices=sorted(topo.TOPOLOGIES))
        current.add_argument("--primary-node", choices=sorted(topo.NODE_ADDRESSES))
        current.add_argument("--zone", default="pair-accept.test")
        current.add_argument("--infra-zone", default="ns-accept.test")
        current.add_argument("--run-label", required=True)
        current.add_argument("--work-root", type=Path, required=True)
        current.add_argument("--license-mode", choices=LICENSE_MODES, default="none")
        current.add_argument("--edit-method", choices=EDIT_METHODS, default="ui-replace")
        current.add_argument("--infrastructure-dns", action="store_true")
        current.add_argument("--unknown-state-limit-seconds", type=float, default=300.0,
                             help="D-024: how long a listed unknown/reconciling setup state may last before it is "
                                  "recorded as an open-ended unknown and the step fails with cause product")
        if name == "run":
            current.add_argument("--identity-file", type=Path, required=True)
            current.add_argument("--dist-archive", type=Path, required=True)
            current.add_argument("--dist-sha256", required=True)
            current.add_argument("--dist-commit", required=True)
            current.add_argument("--dist-tree", required=True)
            current.add_argument("--evidence-root", type=Path, required=True)
            current.add_argument("--license-key-file-primary", type=Path)
            current.add_argument("--license-key-file-secondary", type=Path)
            current.add_argument("--allow-license-service", action="store_true",
                                 help="owner-key mode contacts celikpanel.net from the guests")
            current.add_argument("--skip-security-updates", action="store_true")
            current.add_argument("--local-port-debian13", type=int, default=28443)
            current.add_argument("--local-port-arch", type=int, default=28444)
            current.add_argument("--setup-timeout", type=float, default=2700)
            current.add_argument("--dns-timeout", type=float, default=240)
            current.add_argument("--reboot-timeout", type=int, default=900)
            current.add_argument("--stable-stop-seconds", type=float, default=300.0,
                                 help="stop a wait once the polled state is unchanged this long and still fails "
                                      "its rule (0 disables)")
            current.add_argument("--product-web-src", type=Path,
                                 help="the PRODUCT commit's web/src (build-dist.sh exports it next to the dist, "
                                      "with PRODUCT-COMMIT); required when the product commit differs from the "
                                      "driver's checkout")
            current.add_argument("--execute", action="store_true")
    return parser.parse_args(argv)


def validate_run_args(args: argparse.Namespace) -> None:
    if args.license_mode == "owner-key":
        if not args.allow_license_service:
            raise SystemExit("owner-key license mode contacts celikpanel.net; add --allow-license-service "
                             "only when the owner has decided that")
        for flag in ("license_key_file_primary", "license_key_file_secondary"):
            if getattr(args, flag) is None:
                raise SystemExit(f"owner-key license mode needs --{flag.replace('_', '-')}")
    elif args.license_key_file_primary or args.license_key_file_secondary:
        raise SystemExit("license key files are only accepted with --license-mode owner-key")
    if args.license_mode != "owner-key" and args.allow_license_service:
        raise SystemExit("--allow-license-service is only meaningful with --license-mode owner-key; "
                         "the acceptance fixture build never contacts the license service")
    if len({args.local_port_debian13, args.local_port_arch}) != 2:
        raise SystemExit("local tunnel ports must differ")
    if args.stable_stop_seconds < 0:
        raise SystemExit("--stable-stop-seconds must be 0 (disabled) or positive")
    if args.unknown_state_limit_seconds <= 0:
        raise SystemExit("--unknown-state-limit-seconds must be positive (D-024 needs a bound)")
    args.web_src_provenance = resolve_product_web_src(args)


PRODUCT_COMMIT_MARKER = "PRODUCT-COMMIT"


def driver_checkout(root: Path = gd.REPO) -> dict[str, Any]:
    """The commit of the driver's own checkout and whether its web/src is unmodified (None when unknown).

    A driver extracted from ``git archive`` has no repository: its commit is
    unknown and a run must name the product's texts with --product-web-src.
    """

    git = shutil.which("git")
    if git is None or not (root / ".git").exists():
        return {"commit": None, "web_src_clean": None}
    base = [git, "-c", "safe.directory=*", "-C", str(root)]
    try:
        head = subprocess.run(base + ["rev-parse", "HEAD"], capture_output=True, text=True, timeout=30, check=True)
        dirty = subprocess.run(base + ["status", "--porcelain", "--", "web/src"], capture_output=True, text=True,
                               timeout=60, check=True)
    except (OSError, subprocess.SubprocessError):
        return {"commit": None, "web_src_clean": None}
    return {"commit": head.stdout.strip() or None, "web_src_clean": not dirty.stdout.strip()}


def resolve_product_web_src(args: argparse.Namespace, checkout: Callable[[], dict[str, Any]] | None = None
                            ) -> dict[str, Any]:
    """Where the guidance texts come from: always the product commit's web/src.

    Without --product-web-src the driver's own web/src is used only when its
    checkout is exactly the product commit with web/src unmodified. With it,
    the directory must hold the files the driver reads and a PRODUCT-COMMIT
    marker (written by build-dist.sh) naming --dist-commit.
    """

    commit = args.dist_commit
    if args.product_web_src is None:
        own = (checkout or driver_checkout)()
        if own["commit"] == commit and own["web_src_clean"]:
            return {"source": "driver checkout", "web_src": str(gd.WEB_SRC), "commit": commit}
        raise SystemExit(
            f"the product build is commit {commit} but the driver's checkout is {own['commit'] or 'unknown'}"
            f"{'' if own['web_src_clean'] in (True, None) else ' with a modified web/src'}; the guidance texts must "
            "come from the product: pass --product-web-src <dir> (scripts/build-dist.sh exports it as "
            "<dist>/product-web-src and records it in dist.json; run-topology.sh passes it)")
    web_src = args.product_web_src.resolve()
    missing = gd.missing_web_src_files(web_src)
    if missing:
        raise SystemExit(f"--product-web-src {web_src} lacks {missing}")
    marker = web_src / PRODUCT_COMMIT_MARKER
    try:
        recorded = marker.read_text(encoding="ascii").split()
    except (OSError, UnicodeDecodeError):
        raise SystemExit(f"--product-web-src {web_src} has no readable {PRODUCT_COMMIT_MARKER} marker "
                         "(scripts/build-dist.sh writes it); the texts' commit cannot be established") from None
    if not recorded or recorded[0] != commit:
        raise SystemExit(f"--product-web-src {web_src} is from commit {recorded[0] if recorded else 'unknown'}, "
                         f"the product build is {commit}")
    args.product_web_src = web_src
    return {"source": "--product-web-src", "web_src": str(web_src), "commit": commit,
            "marker": PRODUCT_COMMIT_MARKER}


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        topology = topo.resolve(args.topology, primary_node=args.primary_node, zone=args.zone,
                                infra_zone=args.infra_zone)
        topology.cell_id(args.run_label)
    except topo.TopologyError as exc:
        print(f"pair-acceptance: {exc}", file=sys.stderr)
        return 2
    plan = build_plan(args, topology)
    if args.action == "plan" or not args.execute:
        if args.action == "run":
            validate_run_args(args)
            plan["web_src"] = args.web_src_provenance
        print(json.dumps(plan, indent=2, sort_keys=True))
        return 0
    validate_run_args(args)
    plan["web_src"] = args.web_src_provenance
    return execute_run(args, topology, plan)


def execute_run(args: argparse.Namespace, topology: topo.Topology, plan: dict[str, Any]) -> int:
    import guests as guest_module  # noqa: PLC0415 - imports fixture.py (Linux QEMU host only)

    fixture = guest_module.fixture
    fixture.require_linux_qemu_host()
    root = fixture.validate_work_root(args.work_root)
    fixture_plan = fixture.load_cell_plan(root, plan["cell_id"])
    fixture.require_identity_file(args.identity_file)
    identity = dist_identity(args.dist_archive)
    if identity["sha256"] != args.dist_sha256 or identity["commit"] != args.dist_commit \
            or identity["tree"] != args.dist_tree:
        raise SystemExit(f"dist archive identity differs from the supplied values: {identity}")
    keys: dict[str, str] = {}
    if args.license_mode == "owner-key":
        for role, path in (("primary", args.license_key_file_primary), ("secondary", args.license_key_file_secondary)):
            keys[role] = path.read_text(encoding="ascii").strip()
    run_id = ev.make_run_id(args.run_label, dt.datetime.now(dt.timezone.utc))
    config = Config(
        run_id=run_id, run_label=args.run_label, license_mode=args.license_mode, license_keys=keys,
        edit_method=args.edit_method, infrastructure_dns=args.infrastructure_dns,
        skip_security_updates=args.skip_security_updates, dist_archive=args.dist_archive,
        dist_root=identity["root"], dist_sha256=identity["sha256"], dist_commit=identity["commit"],
        dist_tree=identity["tree"], setup_timeout=args.setup_timeout, dns_timeout=args.dns_timeout,
        reboot_timeout=args.reboot_timeout, stable_stop_seconds=args.stable_stop_seconds or None,
        unknown_state_limit=args.unknown_state_limit_seconds,
        web_src=args.web_src_provenance["web_src"], web_src_provenance=args.web_src_provenance,
    )
    redactor = Redactor(keys.values())
    writer = ev.EvidenceWriter(args.evidence_root.resolve(), run_id, redactor)
    writer.write_json("run.json", {"schema": ev.RUN_SCHEMA, "plan": plan, "config": config.public(),
                                   "cell_directory": fixture_plan["cell_directory"]})
    translator = gd.Translator(gd.load_catalog(Path(config.web_src) / gd.WEB_SRC_FILES["i18n"]))
    guests = guest_module.Guests(fixture_plan, args.identity_file)
    ports = {"debian13": args.local_port_debian13, "arch": args.local_port_arch}

    @contextlib.contextmanager
    def panel_factory(driver: Driver, node: str) -> Iterator[PanelClient]:
        pin = driver.pins.get(node)
        if not pin:
            raise PanelError(f"no pinned panel certificate for {node}")
        with guests.tunnel(node, ports[node]) as base:
            yield PanelClient(node, base, PinnedHTTPSTransport("127.0.0.1", ports[node], pin), redactor,
                              driver.recorder)

    driver = Driver(config, topology, guests, writer, redactor, translator, panel_factory,
                    pid_alive=lambda node: fixture._pid_alive(Path(fixture_plan["nodes"][node]["paths"]["pid"])))
    result = driver.execute()
    print(json.dumps({"run_id": run_id, "overall": result["overall"],
                      "evidence": str(writer.directory)}, indent=2))
    return 0 if result["overall"] in {"passed", "refused-by-product-gate"} else 1


if __name__ == "__main__":
    raise SystemExit(main())
