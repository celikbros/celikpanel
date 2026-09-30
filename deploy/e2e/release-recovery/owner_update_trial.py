#!/usr/bin/env python3
"""upd1: one owner-started update, a failed or good candidate, native recovery.

Roadmap item 3, first combined native run. One disposable registered QEMU
guest per cell. Everything the owner does goes through the Panel API with the
owner's own admin session, exactly as the web UI sends it; the root recovery
CLI is used only where the product text tells the owner to use it.

This driver reuses the existing fixture pieces instead of duplicating them:

* ``lab.py`` - registered QEMU identity, guarded SSH, private uploads;
* ``worker_fixture_origin.py`` - fixture signing key, guest-loopback
  ``celikpanel.net`` origin (never the production key or origin);
* ``current_worker_baseline.py`` - the real installer and the unchanged trust
  enrollment of the baseline (fixture key, provenance
  ``not-production-release-admission``);
* ``guest_bound_worker.py`` / ``guest_recovery_handoff.py`` /
  ``guest_recovery_fault.py`` - checkpoint proof and the second fault;
* ``recovery_fault_trial.py`` - QMP identity and the once-only reset;
* ``guest_probe.py`` - installed/running hashes, database digests, timers;
* the DNS pair driver's ``panel_api`` (session, Origin header, pinned TLS
  leaf, poll-mutation guard), ``redaction``, ``evidence`` and ``guidance``
  (product EN/TR catalogues) modules, loaded by path.

It records observations; it does not decide the P0 rows. ``result.json``
always carries ``native_evidence: false``. Nothing here updates, repairs or
administers an installed customer panel.
"""
from __future__ import annotations

import argparse
import base64
import contextlib
import dataclasses
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import socket
import subprocess
import sys
import threading
import time
from typing import Any, Callable, Iterable

HERE = Path(__file__).resolve().parent
PAIR = HERE.parent / "dns-pair-acceptance"
PRIVATE = "/root/celikpanel-release-recovery-lab"
RESULT_SCHEMA = "celikpanel/upd1-owner-update-result/v1"
INTENT_SCHEMA = "celikpanel/upd1-owner-update-intent/v1"
ARTIFACTS_SCHEMA = "celikpanel/upd1-artifacts/v1"
OBSERVER_INTENT_SCHEMA = "celikpanel/owner-update-observer-intent/v1"
OBSERVER_EVENT_SCHEMA = "celikpanel/owner-update-observer/v1"
BASELINE_VERSION, BASELINE_SEQUENCE = "v0.1.0-alpha.81", 81
CANDIDATE_VERSION, CANDIDATE_SEQUENCE = "v0.1.0-alpha.82", 82
# The real Alpha80 release commit named as the baseline policy's predecessor
# (the same value the unpublished Alpha81 fixture 45dfc265 used).
ALPHA80_COMMIT = "bd14d97efc5cfd19acd70ddf0edb9c6343317e2b"
ACCEPTANCE_NOTICE = "ACCEPTANCE-LICENSE-BUILD.txt"
ACCEPTANCE_NOTICE_PREFIX = b"This archive is NOT a CelikPanel release."
# Same value as dns-pair-acceptance/pair_acceptance.py ACCEPTANCE_FIXTURE_KEY
# (a test pins the equality); accepted only by the acceptance-license build on
# a marked disposable guest, never by a release.
ACCEPTANCE_FIXTURE_KEY = "CPK-acce57f1c7" + "0" * 54
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
HEX40 = re.compile(r"[0-9a-f]{40}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
# Web UI polling (web/src/components/SystemUpdateOperation.tsx).
POLL_MIN_MS, POLL_MAX_MS, POLL_FACTOR = 1500, 15000, 1.6
START_REQUEST_TIMEOUT_S = 15
SAMPLE_INTERVAL_S = 5.0
# Tables that authentication and background writers change (BOUND-WORKER AJ).
VOLATILE_TABLES = frozenset({"audit_logs", "metrics_samples", "sessions", "sqlite_sequence"})
# L2 (upd1 2026-09-30): a setup that waits at a prerequisite (access_dns on an
# isolated host) is a background writer too. Excluded only when this run
# recorded such a wait; server_setup_state is still compared (it changes only
# when setup completes or is abandoned, which would be a real change).
SETUP_WAIT_VOLATILE = {
    "server_setup_executions": "the waiting setup runner rewrites its execution row on every retry (about every "
                               "25 s: cmd/panel/server_setup_dns_retry.go claimServerSetupDNSRetry and "
                               "server_setup_operations.go execution update)",
}
# H5 (upd1 2026-09-30): upd1 is one isolated node. A local-DNS primary publishes
# only after its peer secondary serves the catalogue (cmd/panel/dns_engine.go),
# so a single node cannot create a domain (DNS_SERVER_REQUIRED). The owner
# therefore chooses DNS hosted elsewhere; DNS continuity is covered by the DNS
# pair runs of roadmap item 2. Local mode stays available for a two-node variant.
DNS_MODES = ("external", "local")
DEFAULT_DNS_MODE = "external"
DNS_NOT_PROVIDED = "not-provided-external-dns"
# H4: the phases at which an isolated host's setup waits for public DNS / a
# certificate (same set as dns-pair-acceptance NON_DNS_SETUP_PHASES).
SETUP_SETTLE_PHASES = frozenset({"access_dns", "panel_certificate", "verification", "verify"})
SETUP_STABLE_SECONDS = 120.0
SETUP_STABLE_POLLS = 3
CRON_NOT_AVAILABLE = "not available on this baseline"
ORIGIN_NAME = "celikpanel.net"
GUEST_HELPERS = ("guest_probe.py", "guest_port_fault.py", "guest_update_kill.py", "guest_bound_worker.py",
                 "guest_recovery_fault.py", "guest_recovery_handoff.py", "guest_owner_update_observer.py",
                 "guest_upd1_workload.py")
WORKLOADS = ("web", "dns", "smtp", "cron")
TERMINAL = {("recovered", "rollback_verified"), ("succeeded", "update_verified")}
PROVENANCE = {
    "baseline": "unpublished-local-fixture-commit-over-HEAD-labelled-v0.1.0-alpha.81; acceptance-license panel "
                "build (D-027 fixture); installed by the real installer; fixture trust root enrolled; "
                "not-production-release-admission",
    "candidate": "unpublished-local-fixture-commit-labelled-v0.1.0-alpha.82; signed with the disposable fixture "
                 "key; served by the guest-loopback celikpanel.net fixture origin; not a release",
    "defect": "fixture source patch: cmd/panel --migrate-only exits 1 after migrating the isolated copy "
              "(genuine candidate failure after candidate-installed, simulated by a committed fixture change)",
}


@dataclasses.dataclass(frozen=True)
class Cell:
    name: str
    node: str
    variant: str               # "defective" | "good"
    recovery_fault: dict | None
    mail_required: bool


CELLS = {
    "upd1-debian13-defective": Cell("upd1-debian13-defective", "debian13", "defective",
                                    {"action": "reboot", "checkpoint": "payload_restored"}, True),
    "upd1-debian13-good": Cell("upd1-debian13-good", "debian13", "good", None, True),
    "upd1-arch-defective": Cell("upd1-arch-defective", "arch", "defective",
                                {"action": "kill", "checkpoint": "runtime_verified"}, False),
    "upd1-arch-good": Cell("upd1-arch-good", "arch", "good", None, False),
}


class StepFailed(RuntimeError):
    """A verified failure of this step (the evidence says so)."""


class StepInconclusive(RuntimeError):
    """The required evidence could not be obtained."""


# ---------------------------------------------------------------------------
# Fixture source (applied only inside the disposable clone by build-upd1-artifacts.sh)
# ---------------------------------------------------------------------------

DEFECT_ORIGINAL = (
    "\tif *migrateOnlyFlag {\n"
    "\t\tlog.Println(\"Canonical panel database migrations completed\")\n"
    "\t\treturn\n"
    "\t}\n")
DEFECT_REPLACEMENT = (
    "\tif *migrateOnlyFlag {\n"
    "\t\t// upd1 disposable fixture defect: never a release. The isolated copy is\n"
    "\t\t// migrated, then this candidate reports failure so the signed update fails\n"
    "\t\t// after candidate-installed, while the transaction is still active.\n"
    "\t\tlog.Fatalf(\"upd1 fixture defect: this candidate cannot complete its offline database migration\")\n"
    "\t}\n")


def policy_text(version: str, current: int, previous: int, previous_version: str, previous_commit: str) -> str:
    if not HEX40.fullmatch(previous_commit) or current <= previous:
        raise ValueError("invalid fixture release policy")
    return ("format=celikpanel-release-sequence-policy-v1\n" f"version={version}\n" f"current={current}\n"
            f"previous={previous}\n" f"previous_version={previous_version}\n" f"previous_commit={previous_commit}\n")


def apply_defect(text: str) -> str:
    if text.count(DEFECT_ORIGINAL) != 1 or DEFECT_REPLACEMENT in text:
        raise ValueError("cmd/panel/main.go migrate-only block differs; the fixture defect cannot be applied exactly")
    return text.replace(DEFECT_ORIGINAL, DEFECT_REPLACEMENT)


def fixture_source(repo: Path, kind: str, previous_commit: str | None) -> dict:
    """Edit one disposable clone; the build script commits the result."""
    repo = Path(repo)
    if not (repo / ".git").exists() or "cp-upd1-build" not in str(repo):
        raise ValueError("fixture edits are allowed only in a disposable /var/tmp/cp-upd1-build clone")
    policy = repo / "deploy" / "release-sequence-policy"
    if kind == "baseline":
        policy.write_text(policy_text(BASELINE_VERSION, BASELINE_SEQUENCE, 80, "v0.1.0-alpha.80", ALPHA80_COMMIT))
        return {"kind": kind, "changed": [str(policy.relative_to(repo))]}
    if kind == "good":
        if not previous_commit or not HEX40.fullmatch(previous_commit):
            raise ValueError("the candidate policy names the exact baseline commit")
        policy.write_text(policy_text(CANDIDATE_VERSION, CANDIDATE_SEQUENCE, BASELINE_SEQUENCE,
                                      BASELINE_VERSION, previous_commit))
        return {"kind": kind, "changed": [str(policy.relative_to(repo))]}
    if kind == "defective":
        main = repo / "cmd" / "panel" / "main.go"
        main.write_text(apply_defect(main.read_text()))
        return {"kind": kind, "changed": [str(main.relative_to(repo))]}
    raise ValueError("unknown fixture kind")


# ---------------------------------------------------------------------------
# Pure rules (covered offline by test_owner_update_trial.py)
# ---------------------------------------------------------------------------

def validate_cell(name: str) -> Cell:
    if name not in CELLS:
        raise ValueError(f"unknown cell {name!r}; choose one of {sorted(CELLS)}")
    return CELLS[name]


def validate_artifacts(document: dict, *, check_files: bool = True) -> dict:
    if not isinstance(document, dict) or document.get("schema") != ARTIFACTS_SCHEMA:
        raise ValueError("artifacts document schema differs")
    roles = {"baseline": (BASELINE_VERSION, BASELINE_SEQUENCE), "good": (CANDIDATE_VERSION, CANDIDATE_SEQUENCE),
             "defective": (CANDIDATE_VERSION, CANDIDATE_SEQUENCE)}
    commits = set()
    for role, (version, sequence) in roles.items():
        item = document.get(role)
        if not isinstance(item, dict):
            raise ValueError(f"artifact {role} is missing")
        if item.get("version") != version or item.get("sequence") != sequence:
            raise ValueError(f"artifact {role} must be labelled {version} / {sequence}")
        for key, pattern in (("commit", HEX40), ("tree", HEX40), ("sha256", HEX64)):
            if not pattern.fullmatch(str(item.get(key, ""))):
                raise ValueError(f"artifact {role} {key} is not exact")
        if item.get("license_mode") != "acceptance-fixture":
            raise ValueError(f"artifact {role} must be the labelled acceptance-license build (D-027)")
        if check_files and not (Path(str(item.get("product_web_src", ""))) / "i18n").is_dir():
            raise ValueError(f"artifact {role} product web source (EN/TR catalogues) is missing")
        commits.add(item["commit"])
        if check_files:
            archive = Path(str(item.get("archive", "")))
            if not archive.is_file():
                raise ValueError(f"artifact {role} archive is missing: {archive}")
            if sha256_file(archive) != item["sha256"]:
                raise ValueError(f"artifact {role} archive digest differs")
    if len(commits) != 3:
        raise ValueError("baseline, good and defective candidates must be three distinct commits")
    if document.get("good", {}).get("parent") != document["baseline"]["commit"] \
            or document.get("defective", {}).get("parent") != document["good"]["commit"]:
        raise ValueError("fixture lineage must be baseline <- good <- defective")
    if not HEX40.fullmatch(str(document.get("source_head", ""))):
        raise ValueError("source HEAD is not exact")
    if check_files and not (Path(str(document.get("clone", ""))) / ".git").exists():
        raise ValueError("the disposable fixture clone is missing")
    return document


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def next_delay_ms(delay_ms: int, changed: bool) -> int:
    """SystemUpdateOperation.tsx: reset to the minimum on change, else x1.6 up to 15 s."""
    if changed:
        return POLL_MIN_MS
    return min(POLL_MAX_MS, int(round(delay_ms * POLL_FACTOR)))


def shell_status_command(request_id: str, language: str) -> str:
    """The offline shell's owner command (web/src/offline/page.ts)."""
    if not HEX32.fullmatch(request_id) or language not in ("en", "tr"):
        raise ValueError("invalid shell command input")
    return f"sudo /usr/libexec/celikpanel/recovery status --request-id {request_id} --lang {language}"


def status_agreement(request_id: str, api: dict | None, cli: dict | None, shell: dict | None) -> dict:
    """Do the three owner views name the same operation and the same phase?

    ``api``: /api/v1/recovery/status JSON (None while the Panel is unreachable).
    ``cli``: root CLI ``status --json`` JSON (None when SSH was unavailable).
    ``shell``: {"reference": id held by the browser marker, "status_command":
    command the offline page shows}. The offline shell carries no phase by
    design; it must name the same operation and the exact read-only command.
    """
    reasons: list[str] = []
    named = {}
    if api is not None:
        named["api"] = api.get("request_id")
    if cli is not None:
        named["cli"] = cli.get("request_id")
    if shell is not None:
        named["shell"] = shell.get("reference")
        if shell.get("status_command") not in (shell_status_command(request_id, "en"),
                                               shell_status_command(request_id, "tr")):
            reasons.append("shell status command differs from the exact CLI invocation")
    for source, value in sorted(named.items()):
        if value != request_id:
            reasons.append(f"{source} names operation {value!r}")
    known = {source: value for source, value in (("api", api), ("cli", cli))
             if isinstance(value, dict) and value.get("observation") == "known"}
    if len(known) == 2:
        for field in ("phase", "terminal_proof", "automatic_recovery", "previous_failure"):
            if (api or {}).get(field) != (cli or {}).get(field):
                reasons.append(f"{field} differs: api={api.get(field)!r} cli={cli.get(field)!r}")
    if reasons:
        verdict = "disagree"
    elif len(known) == 2:
        verdict = "agree"
    elif len(known) == 1:
        verdict = "single-source"
    else:
        verdict = "no-known-source"
    return {"verdict": verdict, "reasons": reasons, "sources": sorted(named),
            "known_sources": sorted(known), "phase": {k: v.get("phase") for k, v in known.items()}}


def agreement_verdict(samples: list[dict]) -> dict:
    """Failed only when a disagreement persists over two consecutive samples or at the end."""
    verdicts = [sample.get("verdict") for sample in samples]
    persistent = any(a == b == "disagree" for a, b in zip(verdicts, verdicts[1:]))
    final = verdicts[-1] if verdicts else None
    agreed = sum(1 for value in verdicts if value == "agree")
    if persistent or final == "disagree":
        return {"verdict": "failed", "agreed_samples": agreed, "samples": len(verdicts)}
    if agreed == 0:
        return {"verdict": "inconclusive", "agreed_samples": 0, "samples": len(verdicts)}
    return {"verdict": "passed", "agreed_samples": agreed, "samples": len(verdicts)}


def recovery_guidance(translator: Any, status: dict | None) -> dict:
    """What RecoveryStatus (web/src/components/RecoveryAccess.tsx) shows for one status."""
    if not isinstance(status, dict) or status.get("observation") != "known" or not status.get("phase"):
        keys = ["recovery.observationUnavailable"]
    else:
        phase, waiting, automatic = status.get("phase"), status.get("waiting_for"), status.get("automatic_recovery")
        keys = [("recovery.automatic.pausedTitle" if automatic else f"recovery.wait.{waiting}" if waiting
                 else f"recovery.phase.{phase}"),
                ("recovery.automatic.pausedHelp" if automatic else "recovery.wait.next" if waiting
                 else f"recovery.next.{phase}")]
        if automatic:
            keys += ["recovery.automatic.inspect", "recovery.automatic.resume"]
        if status.get("previous_failure"):
            keys.append(f"recovery.reason.{status['previous_failure']}")
    missing = [key for key in keys if not translator.has(key)]
    texts = {language: [translator.text(key, language=language) for key in keys] for language in ("en", "tr")}
    if status and status.get("automatic_recovery"):
        for language in ("en", "tr"):
            texts[language].insert(3, "sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50")
    return {"keys": keys, "texts": texts, "missing_keys": missing,
            "actionable": not missing and keys != ["recovery.observationUnavailable"],
            "no_actor_or_action": bool(missing)}


def update_card_guidance(translator: Any, status: dict | None) -> dict:
    """What SystemUpdateOperation.tsx shows for one /api/v1/panel/update/status body.

    A ``summary`` is a server string shown as-is in both languages; it is
    recorded as untranslated rather than judged here.
    """
    if not isinstance(status, dict) or not status.get("found"):
        return {"keys": [], "texts": {"en": [], "tr": []}, "summary": None, "untranslated_summary": False}
    state = status.get("status")
    key = {"failed": "panelUpdate.failed", "succeeded": "panelUpdate.succeeded",
           "running": "panelUpdate.running", "queued": "panelUpdate.queued"}.get(state)
    summary = status.get("summary") if state == "failed" else None
    texts = {language: [summary] if summary else ([translator.text(key, language=language)] if key else [])
             for language in ("en", "tr")}
    return {"keys": [key] if key and not summary else [], "texts": texts, "summary": summary,
            "untranslated_summary": bool(summary), "missing_keys": [key] if key and not translator.has(key) else []}


def classify_status(status: dict | None) -> str:
    if not isinstance(status, dict) or status.get("observation") != "known":
        return "unknown"
    pair = (status.get("phase"), status.get("terminal_proof"))
    if pair in TERMINAL:
        return "terminal"
    if status.get("phase") == "recovery_required" and status.get("automatic_recovery") == "paused_retry_limit":
        return "paused"
    return "in-progress"


def classify_outcome(variant: str, final: dict | None, owner_continued: bool) -> str:
    state = classify_status(final)
    if state == "paused":
        return "paused-owner-action-required"
    if state != "terminal":
        return "not-terminal"
    phase = final["phase"]
    if variant == "defective":
        if phase == "recovered":
            return "recovered-after-owner-continuation" if owner_continued else "recovered-automatically"
        return "defective-candidate-reported-success"
    if phase == "succeeded":
        return "update-verified-after-owner-continuation" if owner_continued else "update-verified"
    return "good-candidate-rolled-back"


def outage_windows(samples: Iterable[dict], key: str, interval: float = SAMPLE_INTERVAL_S,
                   gap_factor: float = 2.5) -> list[dict]:
    """Maximal windows in which ``key`` was not proven up.

    A window runs from the last good sample before it (``from``) to the first
    good sample after it (``to``, None while still open). ``failed`` marks
    failing samples inside; ``unobserved`` marks a sampling gap longer than
    ``gap_factor * interval`` (for example a reboot). ``lower_bound_s`` is the
    span of failing samples, ``upper_bound_s`` the span between good samples.
    """
    known = sorted((float(s["t"]), bool(s[key]["ok"])) for s in samples
                   if isinstance(s.get(key), dict) and s[key].get("ok") is not None and "t" in s)
    threshold = interval * gap_factor
    windows: list[dict] = []
    current: dict | None = None
    for index, (moment, ok) in enumerate(known):
        gap = index > 0 and moment - known[index - 1][0] > threshold
        previous = known[index - 1][0] if index > 0 else None
        if current is None:
            if ok and not gap:
                continue
            current = {"from": previous, "first_bad": None, "last_bad": None, "failed": False, "unobserved": gap}
        elif gap:
            current["unobserved"] = True
        if not ok:
            current["first_bad"] = moment if current["first_bad"] is None else current["first_bad"]
            current["last_bad"] = moment
            current["failed"] = True
            continue
        current["to"] = moment
        windows.append(_close(current))
        current = None
    if current is not None:
        current["to"] = None
        windows.append(_close(current))
    return windows


def _close(window: dict) -> dict:
    window["lower_bound_s"] = (round(window["last_bad"] - window["first_bad"], 3) if window["failed"] else 0.0)
    window["upper_bound_s"] = (round(window["to"] - window["from"], 3)
                               if window.get("to") is not None and window.get("from") is not None else None)
    window["kind"] = "+".join(k for k in ("failed", "unobserved") if window[k]) or "none"
    return window


def cron_windows(samples: Iterable[dict], period_limit: float = 130.0) -> list[dict]:
    """Stretches in which the cron stamp did not advance within ``period_limit`` seconds."""
    changes: list[float] = []
    last_mtime = None
    ordered = sorted((float(s["t"]), float(s["cron"]["mtime"])) for s in samples
                     if isinstance(s.get("cron"), dict) and "t" in s and s["cron"].get("ok")
                     and s["cron"].get("mtime") is not None)
    for moment, mtime in ordered:
        if last_mtime is None or mtime != last_mtime:
            changes.append(mtime)
            last_mtime = mtime
    windows = []
    for before, after in zip(changes, changes[1:]):
        if after - before > period_limit:
            windows.append({"from": before, "to": after, "upper_bound_s": round(after - before, 3),
                            "lower_bound_s": round(after - before - 60.0, 3), "kind": "not-advancing",
                            "failed": True, "unobserved": False})
    if ordered and changes and ordered[-1][0] - changes[-1] > period_limit:
        windows.append({"from": changes[-1], "to": None, "upper_bound_s": None,
                        "lower_bound_s": round(ordered[-1][0] - changes[-1] - 60.0, 3), "kind": "not-advancing",
                        "failed": True, "unobserved": False})
    return windows


def classify_windows(windows: list[dict], resets: list[float], slack: float = 10.0) -> list[dict]:
    """Label each window caused by a recorded host reset (the Debian second fault)."""
    for window in windows:
        start = window.get("from") if window.get("from") is not None else window.get("first_bad")
        end = window.get("to")
        caused = any((start is None or start - slack <= reset) and (end is None or reset <= end + slack)
                     for reset in resets)
        window["cause"] = "host-reset" if caused else "unexplained"
    return windows


def workload_verdict(windows: list[dict]) -> str:
    if not windows:
        return "never-interrupted"
    if all(window.get("cause") == "host-reset" for window in windows):
        return "interrupted-only-by-host-reset"
    return "interrupted"


def panel_verdict(windows: list[dict], started_at: float | None, terminal_at: float | None,
                  slack: float = 120.0) -> dict:
    """Panel may be unavailable only between the owner start and the terminal observation."""
    outside = []
    for window in windows:
        begin = window.get("from") if window.get("from") is not None else window.get("first_bad")
        end = window.get("to")
        inside = (started_at is not None and begin is not None and begin >= started_at - SAMPLE_INTERVAL_S * 2
                  and terminal_at is not None and end is not None and end <= terminal_at + slack)
        if not inside:
            outside.append(window)
    return {"verdict": "down-only-during-transaction" if not outside else "down-outside-transaction",
            "outside": outside}


def volatile_tables(setup_waiting: bool) -> dict:
    """Every table excluded from the preservation comparison, with its reason (listed in the verdict)."""
    tables = {name: "authentication or background writer (BOUND-WORKER AJ)" for name in VOLATILE_TABLES}
    if setup_waiting:
        tables.update(SETUP_WAIT_VOLATILE)
    return tables


def compare_databases(before: dict, after: dict, volatile: frozenset | dict = VOLATILE_TABLES) -> dict:
    try:
        pre, post = before["semantic"], after["semantic"]
    except (KeyError, TypeError):
        return {"verdict": "inconclusive", "reason": "database semantic observation unavailable"}
    pre_tables = {t["name"]: t for t in pre.get("tables", [])}
    post_tables = {t["name"]: t for t in post.get("tables", [])}
    names = sorted(set(pre_tables) | set(post_tables))
    differing = [n for n in names if pre_tables.get(n, {}).get("sha256") != post_tables.get(n, {}).get("sha256")]
    unexpected = [n for n in differing if n not in volatile]
    schema_equal = pre.get("schema_sha256") == post.get("schema_sha256")
    verdict = "equal-except-volatile" if schema_equal and not unexpected else "different"
    if schema_equal and not differing:
        verdict = "equal"
    result = {"verdict": verdict, "schema_equal": schema_equal, "tables_compared": len(names),
              "differing": differing, "unexpected": unexpected, "volatile_excluded": sorted(volatile),
              "pre_sha256": pre.get("sha256"), "post_sha256": post.get("sha256")}
    if isinstance(volatile, dict):
        result["volatile_reasons"] = {name: volatile[name] for name in sorted(volatile)}
    return result


# -- setup, DNS scope, cron and origin rules (upd1 2026-09-30 corrections) ----------

def setup_draft_choice(dns_mode: str, override: dict | None) -> dict:
    """The owner's draft field choices for ``dns_mode`` (H3/H5).

    ``external`` (default): DNS hosted elsewhere, no peer identity.
    ``local``: the product requires the paired identity
    (``server_setup_dns_identity_required``), so ``peer_ip`` and ``peer_ns``
    must be given through ``--setup-draft-json``.
    """
    if dns_mode not in DNS_MODES:
        raise ValueError(f"--dns-mode must be one of {list(DNS_MODES)}")
    override = dict(override or {})
    if "purpose" in override:
        raise ValueError("--setup-draft-json overrides draft fields other than purpose")
    if override.get("dns_mode", dns_mode) != dns_mode:
        raise ValueError(f"--setup-draft-json dns_mode {override['dns_mode']!r} conflicts with --dns-mode {dns_mode}")
    if dns_mode == "external":
        if override.get("peer_ip") or override.get("peer_ns"):
            raise ValueError("external DNS takes no peer identity; use --dns-mode local for a paired node")
        return dict(override, dns_mode="external", peer_ip="", peer_ns="")
    if not override.get("peer_ip") or not override.get("peer_ns"):
        raise ValueError("--dns-mode local needs peer_ip and peer_ns in --setup-draft-json (the product refuses a "
                         "local DNS setup without its paired identity: server_setup_dns_identity_required)")
    return dict(override, dns_mode="local")


def dns_scope(dns_mode: str) -> dict:
    if dns_mode == "external":
        return {"mode": "external", "verdict": DNS_NOT_PROVIDED,
                "note": "DNS is not provided by this run (the owner chose DNS hosted elsewhere on one isolated "
                        "node); DNS continuity is covered by the DNS pair runs of roadmap item 2"}
    return {"mode": "local", "verdict": "measured",
            "note": "local authoritative DNS; a single node cannot publish (DNS_SERVER_REQUIRED) until a "
                    "two-node upd1 variant exists"}


class SetupWait:
    """H4: when does the setup poll stop?

    ``terminal`` on succeeded/failed. ``settled`` once the execution has stayed
    at one of SETUP_SETTLE_PHASES for ``stable_seconds`` over at least
    ``polls`` reads and the latest read says ``waiting`` (the product flips the
    row to ``running`` for each retry, so ``running`` at the same phase does not
    restart the clock). Any other phase or status restarts it.
    """

    def __init__(self, stable_seconds: float = SETUP_STABLE_SECONDS, polls: int = SETUP_STABLE_POLLS) -> None:
        self.stable_seconds, self.polls = stable_seconds, polls
        self.phase: str | None = None
        self.since: float | None = None
        self.count = 0

    def observe(self, execution: Any, now: float) -> str:
        status = execution.get("status") if isinstance(execution, dict) else None
        phase = execution.get("phase") if isinstance(execution, dict) else None
        if status in ("succeeded", "failed"):
            return "terminal"
        if status not in ("waiting", "running") or phase not in SETUP_SETTLE_PHASES:
            self.phase, self.since, self.count = None, None, 0
            return "continue"
        if phase != self.phase:
            self.phase, self.since, self.count = phase, now, 0
        self.count += 1
        if status == "waiting" and self.count >= self.polls and now - self.since >= self.stable_seconds:
            return "settled"
        return "continue"

    def seconds(self, now: float) -> float:
        return round(now - self.since, 1) if self.since is not None else 0.0


def setup_steps(execution: Any) -> list[dict]:
    return [s for s in ((execution or {}).get("steps") or []) if isinstance(s, dict)] if isinstance(execution, dict) else []


def mail_steps_reached(execution: Any) -> bool:
    """False while any mail_profile step of the setup is still pending (the wizard never got there)."""
    return not any(s.get("kind") == "mail_profile" and s.get("status") == "pending" for s in setup_steps(execution))


def cron_availability(observed: dict) -> dict:
    """Read-only precondition before the owner's cron job (product finding P1).

    The Agent refuses a cron job when ``crontab`` is absent
    (cmd/agent/cron_rpc.go); the Panel currently masks that as 500 INTERNAL.
    Absent ``crontab`` is recorded as ``not available on this baseline``
    instead of being seeded; once setup installs cron the check passes.
    """
    units = {name: value for name, value in (observed.get("units") or {}).items()
             if isinstance(value, dict) and value.get("LoadState") not in (None, "", "not-found")}
    crontab = observed.get("crontab") or None
    available = bool(crontab)
    return {"available": available, "verdict": "available" if available else CRON_NOT_AVAILABLE,
            "crontab": crontab, "daemon_units": {name: units[name] for name in sorted(units)},
            "daemon_active": any(value.get("ActiveState") == "active" for value in units.values())}


def hosts_mappings(hosts_text: str, name: str = ORIGIN_NAME) -> list[str]:
    """Lines of /etc/hosts that map ``name`` (read from the file; no resolver query)."""
    found = []
    for line in hosts_text.splitlines():
        fields = line.split("#", 1)[0].split()
        if len(fields) >= 2 and name in fields[1:]:
            found.append(line.strip())
    return found


def origin_verdict(check: dict) -> dict:
    """Is the guest-loopback fixture origin the only answer for celikpanel.net?

    ``check``: guest_upd1_workload ``origin-check`` output. Every resolved
    address must be 127.0.0.1 and the fixture must answer HTTP 200.
    """
    addresses = []
    for line in str((check.get("getent") or {}).get("stdout") or "").splitlines():
        fields = line.split()
        if len(fields) >= 2 and ORIGIN_NAME in fields[1:]:
            addresses.append(fields[0])
    http = str((check.get("https") or {}).get("stdout") or "").strip()
    unit = check.get("unit") or {}
    loopback = bool(addresses) and all(address == "127.0.0.1" for address in addresses)
    return {"addresses": addresses, "http": http, "loopback_only": loopback,
            "unit": {k: unit.get(k) for k in ("ActiveState", "UnitFileState", "NRestarts")},
            "boot_id": check.get("boot_id"), "ok": loopback and http == "200"}


ORIGIN_UNIT = "cp-lab-upd1-origin.service"
SAMPLER_UNIT = "cp-lab-upd1-sampler.service"
BASELINE_INSTALL_UNIT = "celikpanel-lab-current-worker-baseline.service"
# Services the setup wizard installs or reconfigures (P2: the Arch webmail
# mail_profile failure needs these journals; globs are journalctl -u patterns).
SETUP_SERVICE_UNITS = ("nginx.service", "php*-fpm.service", "mariadb.service", "mysql.service",
                       "named.service", "bind9.service", "pdns.service", "postfix.service", "dovecot.service",
                       "rspamd.service", "cron.service", "cronie.service", "nftables.service")


def journal_groups(request_id: str | None) -> dict:
    """L3: journals kept by collect, also when the cell stopped at seed or earlier."""
    product = ["celikpanel-panel.service", "celikpanel-agent.service", "celikpanel-release-recovery.service"]
    if request_id:
        product += [f"celikpanel-self-update-{request_id}.service",
                    f"celikpanel-lab-owner-update-observer-{request_id}.service",
                    f"celikpanel-lab-recovery-fault-{request_id}.service"]
    return {"product": product, "setup-services": list(SETUP_SERVICE_UNITS),
            "lab": [ORIGIN_UNIT, SAMPLER_UNIT, BASELINE_INSTALL_UNIT]}


def observation_records_script(request_id: str | None) -> str:
    """Read-only listing of producer observation records (secret-looking contents withheld)."""
    if request_id is not None and not HEX32.fullmatch(request_id):
        raise ValueError("invalid request id")
    pattern = (request_id or "") + "*"
    return "\n".join([
        "python3 -I - <<'CP_UPD1_OBS'",
        "import json,re",
        "from pathlib import Path",
        "out={}",
        "root=Path('/var/lib/celikpanel-recovery-observations')",
        "paths=sorted(root.glob('" + pattern + "'))[:16] if root.is_dir() else []",
        "for p in paths:",
        "    if not p.is_file() or p.is_symlink(): continue",
        "    raw=p.read_bytes()[:4096].decode('ascii','replace')",
        "    out[p.name]=raw if not re.search(r'token|secret|password',raw,re.I) else 'withheld'",
        "print(json.dumps({'directory_present':root.is_dir(),'records':out}))",
        "CP_UPD1_OBS", ""])


def result_scope(dns_mode: str, cron: dict | None, setup_wait: dict | None, origin_checks: dict | None) -> dict:
    """What this run did and did not provide, for result.json (never a pass by omission)."""
    return {"dns": dns_scope(dns_mode),
            "cron": ({"verdict": "not checked (the cell stopped before seeding)"} if not cron
                     else {"verdict": cron["verdict"], "crontab": cron.get("crontab")}),
            "setup": ({"verdict": "not waiting"} if not setup_wait
                      else {"verdict": "observed isolated-host wait", "phase": setup_wait.get("phase"),
                            "code": setup_wait.get("code"), "not_run": setup_wait.get("not_run"),
                            "volatile_tables_added": sorted(SETUP_WAIT_VOLATILE)}),
            "origin": {label: {k: value.get(k) for k in ("ok", "addresses", "http")}
                       for label, value in (origin_checks or {}).items()}}


def workload_verdicts(samples: list[dict], resets: list[float], *, mail_listed: bool, cron: str,
                      dns_mode: str) -> dict:
    """Per-workload verdicts. ``cron``: ``measured`` | CRON_NOT_AVAILABLE | ``not-running-before-update``.

    In external DNS mode DNS is never measured and never counted as passed.
    """
    per: dict[str, dict] = {}
    for key in WORKLOADS:
        if key == "smtp" and not mail_listed:
            per[key] = {"verdict": "not-seeded"}
            continue
        if key == "dns" and dns_mode == "external":
            per[key] = dict(dns_scope("external"))
            continue
        if key == "cron" and cron == CRON_NOT_AVAILABLE:
            per[key] = {"verdict": "not-available-on-baseline", "note": "cron: " + CRON_NOT_AVAILABLE}
            continue
        if key == "cron" and cron != "measured":
            per[key] = {"verdict": "not-running-before-update"}
            continue
        windows = cron_windows(samples) if key == "cron" else outage_windows(samples, key)
        classify_windows(windows, resets)
        per[key] = {"verdict": workload_verdict(windows), "windows": windows}
    return per


def compare_states(before: dict, after: dict, fields: tuple = ("UnitFileState", "ActiveState")) -> dict:
    changed = {}
    for name in sorted(set(before) | set(after)):
        a = {k: (before.get(name) or {}).get(k) for k in fields}
        b = {k: (after.get(name) or {}).get(k) for k in fields}
        if a != b:
            changed[name] = {"before": a, "after": b}
    return {"equal": not changed, "changed": changed, "compared": len(set(before) | set(after))}


STEP_VERDICTS = ("passed", "failed", "observed", "inconclusive", "skipped", "not-run")


def overall(verdicts: list[str]) -> str:
    for verdict in verdicts:
        if verdict not in STEP_VERDICTS:
            raise ValueError(f"unknown step verdict {verdict!r}")
    if "failed" in verdicts:
        return "failed"
    if "not-run" in verdicts:
        return "incomplete"
    if "inconclusive" in verdicts:
        return "inconclusive"
    return "complete-for-review"


def parse_offline_copy(source: str) -> dict:
    """EN/TR texts of web/src/offline/copy.ts (the saved recovery page)."""
    result = {}
    for language in ("en", "tr"):
        match = re.search(language + r":\s*\{(.*?)\n\s*\}", source, re.S)
        if not match:
            raise ValueError(f"offline copy has no {language} block")
        result[language] = {key: value.replace("\\'", "'") for key, value in
                            re.findall(r"(\w+):\s*'((?:[^'\\]|\\.)*)'", match.group(1))}
    if set(result["en"]) != set(result["tr"]) or not result["en"]:
        raise ValueError("offline copy languages differ")
    return result


def parse_dispatch_journal(text: str) -> list[dict]:
    """``Recovery dispatch admitted: attempt=N`` lines with their journal timestamps."""
    found = []
    for line in text.splitlines():
        match = re.match(r"(\S+)\s+\S+\s+[^:]+:\s+Recovery dispatch admitted: attempt=(\w+) snapshot=(\S+)", line)
        if match:
            found.append({"at": match.group(1), "attempt": match.group(2), "snapshot": match.group(3)})
    return found


def attempts_from_receipts(receipts: list[dict], snapshot: str | None) -> dict:
    automatic = sorted((r for r in receipts if r.get("name") in ("1", "2", "3")
                        and (snapshot is None or r.get("snapshot") == snapshot)), key=lambda r: r["name"])
    owner = [r for r in receipts if str(r.get("name", "")).startswith("owner.")
             and (snapshot is None or r.get("snapshot") == snapshot)]
    return {"automatic_count": len(automatic),
            "automatic": [{"attempt": r["name"], "at": r.get("mtime_utc")} for r in automatic],
            "owner_count": len(owner), "owner": [{"name": r["name"], "at": r.get("mtime_utc")} for r in owner]}


# ---------------------------------------------------------------------------
# Module loading (lazy; offline tests need only the pure rules above)
# ---------------------------------------------------------------------------

_MODULES: dict[str, Any] = {}


def _load(name: str, path: Path) -> Any:
    if name in _MODULES:
        return _MODULES[name]
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    _MODULES[name] = value
    return value


def pair_modules() -> dict:
    """The pair driver's modules, by path. ``panel_api``/``evidence`` import ``redaction``."""
    redaction = _load("redaction", PAIR / "redaction.py")
    return {"redaction": redaction, "panel_api": _load("upd1_pair_panel_api", PAIR / "panel_api.py"),
            "evidence": _load("upd1_pair_evidence", PAIR / "evidence.py"),
            "guidance": _load("upd1_pair_guidance", PAIR / "guidance.py"),
            "install_steps": _load("upd1_pair_install_steps", PAIR / "install_steps.py")}


def lab_modules() -> dict:
    baseline = _load("upd1_current_worker_baseline", HERE / "current_worker_baseline.py")
    origin = _load("upd1_worker_fixture_origin", HERE / "worker_fixture_origin.py")
    native = _load("upd1_recovery_fault_trial", HERE / "recovery_fault_trial.py")
    return {"lab": baseline.lab, "baseline": baseline, "origin": origin, "native": native,
            "trial": native.trial, "archive": baseline.archive_tools}


def evidence_writer_class():
    pair_evidence = pair_modules()["evidence"]

    class Upd1Evidence(pair_evidence.EvidenceWriter):
        """The pair writer (create-new, secret refusal); upd1's own result schema."""

        def finalize_upd1(self, result: dict) -> dict:
            if result.get("schema") != RESULT_SCHEMA or result.get("native_evidence") is not False:
                raise ValueError("upd1 result must carry its schema and native_evidence=false")
            expected = overall([step["verdict"] for step in result.get("steps", [])])
            if result.get("overall") != expected:
                raise ValueError(f"overall {result.get('overall')!r} disagrees with step verdicts ({expected!r})")
            self.write_json("result.json", result)
            lines = [f"{sha256_file(self.directory / rel)}  {rel}\n" for rel in sorted(self.files)]
            self._write_bytes("SHA256SUMS", "".join(lines).encode("ascii"))
            self._finalized = True
            return result

    return Upd1Evidence


# ---------------------------------------------------------------------------
# Acceptance-license source proof (the one labelled extra file)
# ---------------------------------------------------------------------------

def acceptance_notice(archive: Path) -> bytes:
    import tarfile
    with tarfile.open(archive, "r:gz") as bundle:
        for member in bundle:
            if member.isfile() and member.name.split("/", 1)[-1] == ACCEPTANCE_NOTICE:
                return bundle.extractfile(member).read(8192)
    raise ValueError("archive is not the labelled acceptance-license build")


def acceptance_source_proof(verify: Callable, archive: Path, repository: Path) -> Callable:
    """Wrap candidate_archive.verify_committed_source for the labelled acceptance build.

    Exactly one file is exempted from Git-blob proof: the build's own notice,
    whose bytes are checked here. bin/ is already outside source proof; the
    rebuilt acceptance panel is covered by the archive inventory as before.
    """
    notice = acceptance_notice(archive)
    if not notice.startswith(ACCEPTANCE_NOTICE_PREFIX):
        raise ValueError("acceptance notice text differs")

    def wrapped(candidate: dict, _repository: Any) -> dict:
        files = dict(candidate["files"])
        if files.pop(ACCEPTANCE_NOTICE, None) != hashlib.sha256(notice).hexdigest():
            raise ValueError("acceptance notice digest differs from the archive inventory")
        proof = verify(dict(candidate, files=files), repository)
        return dict(proof, acceptance_notice_sha256=hashlib.sha256(notice).hexdigest(),
                    exempted_from_git_proof=[ACCEPTANCE_NOTICE])
    return wrapped


def prove_artifacts(artifacts: dict, roles: Iterable[str], modules: dict | None = None) -> dict:
    """Host-side and read-only: archive inventory, release policy and committed-source proof.

    The same proof preflight runs per cell; ``owner_update_trial.py prove``
    runs it for all three archives without any guest (H2: the exact
    dns-owner-tools/ inventory of make dist is part of it).
    """
    m = modules or lab_modules()
    archive = m["archive"]
    clone = Path(artifacts["clone"])
    proofs = {}
    for role in roles:
        item = artifacts[role]
        policy = m["baseline"].RELEASE_POLICY if role == "baseline" else m["origin"].RELEASE_POLICY
        candidate = archive.inspect_archive(Path(item["archive"]), item["sha256"], release_policy=policy)
        if candidate["commit"] != item["commit"] or candidate["tree"] != item["tree"]:
            raise ValueError(f"artifact {role} archive names commit/tree {candidate['commit']}/{candidate['tree']}, "
                             f"not {item['commit']}/{item['tree']}")
        proof = acceptance_source_proof(archive.verify_committed_source, Path(item["archive"]), clone)(candidate, clone)
        proofs[role] = {"commit": candidate["commit"], "tree": candidate["tree"], "files": len(candidate["files"]),
                        "release_policy": candidate["release_policy"], "source_proof": proof,
                        "dns_owner_tools": sorted(n for n in candidate["files"] if n.startswith("dns-owner-tools/")),
                        "agent_sha256": candidate["files"]["bin/agent"], "panel_sha256": candidate["files"]["bin/panel"]}
    return proofs


@contextlib.contextmanager
def patched(obj: Any, name: str, value: Any):
    old = getattr(obj, name)
    setattr(obj, name, value)
    try:
        yield
    finally:
        setattr(obj, name, old)


# ---------------------------------------------------------------------------
# Plan / dry run
# ---------------------------------------------------------------------------

def build_plan(cell: Cell, artifacts: dict, work_root: str, local_port: int,
               dns_mode: str = DEFAULT_DNS_MODE) -> dict:
    candidate = artifacts["defective" if cell.variant == "defective" else "good"]
    fault = cell.recovery_fault
    steps = [
        ("preflight", "registered lab identity, fresh guest (read-only; /etc/hosts is read, the resolver is never "
                      "asked for celikpanel.net), host-side artifact and source proofs"),
        ("origin", "fixture signing key; seal the candidate; guest-loopback celikpanel.net origin (provision + the "
                   "enabled lab unit cp-lab-upd1-origin.service, which survives a restart) BEFORE the baseline; then "
                   "celikpanel.net must resolve only to 127.0.0.1 and answer 200, so nothing reaches the real origin"),
        ("baseline-install", "current_worker_baseline: real installer + unchanged trust enrollment "
                             f"({BASELINE_VERSION}, commit {artifacts['baseline']['commit'][:12]}); the owner restart "
                             "the installer may demand, then the origin is proved again"),
        ("owner-login", "owner credentials (guest root only, host memory only), SSH loopback tunnel, pinned TLS leaf, "
                        "POST /api/v1/auth/login, GET /api/v1/auth/me, GET /api/v1/panel/availability"),
        ("license", "GET /api/v1/panel/license, POST {action:activate, acceptance fixture key} once, GET /api/v1/license/access"),
        ("setup", "GET /api/v1/setup, PUT /api/v1/setup/guidance, PUT /api/v1/setup (draft, dns_mode "
                  f"{dns_mode}), POST /api/v1/setup/plan, POST /api/v1/setup/start (once), poll GET "
                  "/api/v1/setup/operation?request_id= until succeeded/failed or a stable "
                  f"{int(SETUP_STABLE_SECONDS)} s wait at {sorted(SETUP_SETTLE_PHASES)} (recorded as observed)"
                  + (" [purpose web_mail]" if cell.mail_required else " [purpose web_mail attempted, web fallback recorded]")),
        ("seed", "POST /api/v1/domains/create {static}; POST /api/v1/domains/{id}/files?path=/index.html {action:write}; "
                 "GET .../dns/zone, .../dns/records; GET .../mail/setup; POST .../mail/accounts; read-only cron "
                 "availability, then POST .../cron only when crontab exists; GET /api/v1/firewall"),
        ("pre-state", "guest_probe observation (hashes, DB digests, timers), workload snapshot, offline shell fetch, "
                      "sampler unit (5 s, survives reboot) + host loop (SSH, Panel via tunnel)"),
        ("arm", "origin proved again (127.0.0.1, HTTP 200); owner-update observer for the chosen request id: " + (
            f"checkpoint mode, second fault {fault['action']} at {fault['checkpoint']}" if fault else "watch mode, no fault")),
        ("owner-start", "GET /api/v1/panel/update/check, GET /api/v1/host-mutation-readiness, "
                        "POST /api/v1/panel/update/start {request_id, confirmed:true, current_version, current_commit, ...target} once"),
        ("track", "GET /api/v1/panel/update/status?request_id= with UI backoff 1.5 s x1.6 <= 15 s; while the Panel is down: "
                  "GET /api/v1/recovery/status?request_id= (same session), root CLI status --json/--lang en/--lang tr, "
                  "offline shell reference; three-source agreement per sample"
                  + ("; QMP system_reset once at reboot_ready" if fault and fault["action"] == "reboot" else "")),
        ("owner-continuation (required)", "only if the product reports paused_retry_limit: verify all views, run the exact "
                                          "one-time retry command printed in the recovery journal, once"),
        ("terminal", "installed/running identity, floor/foundation, DB digests vs pre-update, seeded rows, site marker, "
                     "mailbox, cron, timers, firewall, Panel login and update card"),
        ("collect", "always once the guest was prepared, also after an early stop: sampler and host samples, "
                    "journals (Panel/Agent/recovery, setup services, lab units incl. the fixture origin), "
                    "observation records, observer/recovery-fault events, budget receipts"),
        ("verdicts", "per-workload outage windows (DNS: " + dns_scope(dns_mode)["verdict"] + "), Panel window, "
                     "agreement, outcome classification"),
    ]
    return {"schema": "celikpanel/upd1-plan/v1", "cell": dataclasses.asdict(cell), "work_root": work_root,
            "dns": dns_scope(dns_mode),
            "local_port": local_port, "baseline": {k: artifacts["baseline"][k] for k in ("version", "commit", "sha256")},
            "candidate": {k: candidate[k] for k in ("version", "commit", "sha256")},
            "expected_outcome": ("recovered (rollback_verified, previous_failure=update_failed), automatically or "
                                 "after the owner's one-time retry" if cell.variant == "defective"
                                 else "succeeded (update_verified)"),
            "provenance": PROVENANCE, "native_evidence": False,
            "steps": [{"name": n, "does": d} for n, d in steps]}


def validate_work_root(value: str) -> None:
    root = Path(value)
    if root.parent != Path("/var/tmp") or not re.fullmatch(r"cp-release-drill-[a-z0-9-]{1,50}", root.name):
        raise ValueError("work root must be a new /var/tmp/cp-release-drill-NAME lab")


# ---------------------------------------------------------------------------
# Native execution
# ---------------------------------------------------------------------------

class Tunnel:
    """Loopback-only SSH forward to the guest Panel; restarted after a reboot."""

    def __init__(self, lab: Any, root: Path, record: dict, node: dict, port: int) -> None:
        self.lab, self.root, self.record, self.node, self.port = lab, root, record, node, port
        self.process: subprocess.Popen | None = None
        self.lock = threading.Lock()

    def ensure(self, timeout: float = 20.0) -> bool:
        with self.lock:
            if self.process is not None and self.process.poll() is None and self._open():
                return True
            self.close_locked()
            base = self.lab.ssh(self.root, self.record, self.node)
            argv = base[:1] + ["-N", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=5",
                               "-L", f"127.0.0.1:{self.port}:127.0.0.1:2083"] + base[1:]
            self.process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                            stderr=subprocess.DEVNULL)
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                if self.process.poll() is not None:
                    return False
                if self._open():
                    return True
                time.sleep(0.25)
            return False

    def _open(self) -> bool:
        try:
            with socket.create_connection(("127.0.0.1", self.port), timeout=1):
                return True
        except OSError:
            return False

    def close_locked(self) -> None:
        if self.process is not None and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(5)
            except subprocess.TimeoutExpired:
                self.process.kill()
        self.process = None

    def close(self) -> None:
        with self.lock:
            self.close_locked()


class Trial:
    def __init__(self, cell: Cell, artifacts: dict, work_root: str, local_port: int,
                 setup_draft: dict | None = None, dns_mode: str = DEFAULT_DNS_MODE) -> None:
        self.cell, self.artifacts = cell, artifacts
        self.m = lab_modules()
        self.p = pair_modules()
        self.lab, self.trial = self.m["lab"], self.m["trial"]
        self.root = self.lab.checked_root(work_root)
        self.record, self.plan = self.lab.load(self.root)
        self.node_name = cell.node
        self.node = self.plan["nodes"][cell.node]
        self.identity = self.trial.identity(self.record, self.plan, cell.node)
        self.local_port = local_port
        self.dns_mode = dns_mode
        self.setup_draft_override = setup_draft_choice(dns_mode, setup_draft)
        self.candidate = artifacts["defective" if cell.variant == "defective" else "good"]
        self.redactor = self.p["redaction"].Redactor()
        evidence_root = self.root / "evidence" / cell.node / "upd1"
        evidence_root.mkdir(parents=True, mode=0o700, exist_ok=True)
        run_id = self.p["evidence"].make_run_id(cell.name, dt.datetime.now(dt.timezone.utc))
        self.ev = evidence_writer_class()(evidence_root, run_id, self.redactor)
        self.translator = self.p["guidance"].Translator(self.p["guidance"].load_catalog(
            Path(artifacts["baseline"]["product_web_src"]) / "i18n"))
        self.steps: list[dict] = []
        self.step_dir = "steps/00-run"
        self.state: dict[str, Any] = {"findings": [], "resets": []}
        self.tunnel = Tunnel(self.lab, self.root, self.record, self.node, local_port)
        self.transport = None
        self.client = None
        self.host_samples: list[dict] = []
        self.stop_host_loop = threading.Event()

    # -- evidence ------------------------------------------------------------

    def record_json(self, name: str, value: Any) -> str:
        return self.ev.write_json(f"{self.step_dir}/{name}", value)

    def finding(self, text: str) -> None:
        if text not in self.state["findings"]:
            self.state["findings"].append(text)

    def step(self, name: str, function: Callable[[dict], str | None], *, needs: tuple = ()) -> str:
        index = len(self.steps) + 1
        self.step_dir = f"steps/{index:02d}-{re.sub(r'[^a-z0-9]+', '-', name.lower()).strip('-')}"
        entry = {"name": name, "verdict": "not-run", "checks": {}, "started_at": utc_now()}
        self.steps.append(entry)
        if any(self.verdict_of(dep) not in ("passed", "observed", "skipped") for dep in needs):
            entry["verdict"] = "not-run"
            entry["reason"] = "a required earlier step did not pass: " + ", ".join(needs)
            return entry["verdict"]
        try:
            verdict = function(entry["checks"]) or "passed"
            entry["verdict"] = verdict
        except StepFailed as exc:
            entry.update(verdict="failed", reason=self.redactor.text(str(exc)))
        except StepInconclusive as exc:
            entry.update(verdict="inconclusive", reason=self.redactor.text(str(exc)))
        except Exception as exc:  # noqa: BLE001 - recorded, never retried as a mutation
            detail = f"{type(exc).__name__}: {exc}"
            if isinstance(exc, subprocess.CalledProcessError) and exc.stderr:
                stderr = exc.stderr if isinstance(exc.stderr, str) else exc.stderr.decode("utf-8", "replace")
                detail += " | guest stderr: " + stderr[-2000:]
            entry.update(verdict="inconclusive", reason=self.redactor.text(detail))
        entry["finished_at"] = utc_now()
        self.record_json("step.json", entry)
        print(json.dumps({"step": name, "verdict": entry["verdict"], "reason": entry.get("reason")}), flush=True)
        return entry["verdict"]

    def verdict_of(self, name: str) -> str:
        for entry in self.steps:
            if entry["name"] == name:
                return entry["verdict"]
        return "not-run"

    # -- guest access ----------------------------------------------------------

    def guest(self, body: str, timeout: float = 120) -> subprocess.CompletedProcess:
        return self.lab.guarded_script(self.root, self.record, self.plan, self.node_name, body, timeout=timeout)

    def helper(self, script: str, mode: str | None, *args: str, timeout: float = 120) -> dict:
        argv = ["python3", "-I", f"{PRIVATE}/{script}"] + ([mode] if mode else [])
        argv += ["--lab-nonce", self.identity["nonce"], "--vm-uuid", self.identity["vm_uuid"],
                 "--cell-id", self.identity["cell_id"], "--node", self.node_name, *args]
        result = self.guest(shlex.join(argv), timeout=timeout)
        return json.loads(result.stdout)

    def workload(self, mode: str, *args: str, timeout: float = 120) -> dict:
        return self.helper("guest_upd1_workload.py", mode, *args, timeout=timeout)

    def ssh_reachable(self, timeout: float = 8) -> bool:
        try:
            return subprocess.run(self.lab.ssh(self.root, self.record, self.node) + ["true"],
                                  capture_output=True, timeout=timeout).returncode == 0
        except (subprocess.SubprocessError, OSError, ValueError):
            return False

    def wait_for_ssh(self, timeout: float = 900) -> float:
        started = time.monotonic()
        while time.monotonic() - started < timeout:
            if self.ssh_reachable():
                try:
                    self.guest("true", timeout=30)
                    return time.monotonic() - started
                except subprocess.SubprocessError:
                    pass
            time.sleep(3)
        raise StepInconclusive("guest SSH did not return")

    def upload_helpers(self) -> dict:
        return {name: self.lab.put_file(self.root, self.record, self.plan, self.node_name, HERE / name, name)[1]
                for name in GUEST_HELPERS}

    # -- Panel access ------------------------------------------------------------

    def refresh_pin(self) -> str:
        leaf = self.workload("tls-leaf")["leaf_sha256"]
        if self.transport is None:
            self.transport = self.p["panel_api"].PinnedHTTPSTransport("127.0.0.1", self.local_port, leaf)
        elif self.transport.leaf_sha256 != leaf:
            self.state.setdefault("pin_changes", []).append({"at": utc_now(), "from": self.transport.leaf_sha256,
                                                             "to": leaf})
            self.transport.leaf_sha256 = leaf
        return leaf

    def panel_client(self) -> Any:
        api = self.p["panel_api"]
        if self.client is None:
            self.refresh_pin()
            self.client = api.PanelClient("owner", f"https://127.0.0.1:{self.local_port}", self.transport,
                                          self.redactor, lambda exchange: self.ev.api_exchange(self.step_dir, exchange))
        self.tunnel.ensure()
        return self.client

    def api(self, method: str, path: str, body: Any = None, *, timeout: float = 40, purpose: str | None = None,
            view: Any = None) -> Any:
        client = self.panel_client()
        target = view if view is not None else client
        try:
            if method == "GET":
                return target.get(path, timeout=timeout, purpose=purpose)
            return client.request(method, path, body, timeout=timeout, purpose=purpose)
        except self.p["panel_api"].PinMismatch:
            self.refresh_pin()
            if method != "GET":
                raise StepInconclusive(f"{method} {path}: TLS leaf changed before the request was sent; not retried")
            return target.get(path, timeout=timeout, purpose=purpose)

    # -- steps ---------------------------------------------------------------------

    def preflight(self, checks: dict) -> str:
        self.lab.process_guard(self.node)
        intent_path = self.root / "evidence" / self.node_name / "upd1-intent.json"
        if intent_path.exists() or intent_path.is_symlink():
            raise StepFailed("an upd1 intent already exists for this guest; a cell never reruns on the same guest")
        validate_artifacts(self.artifacts)
        proofs = prove_artifacts(self.artifacts, ("baseline", "good" if self.cell.variant == "good" else "defective"),
                                 self.m)
        self.state["proofs"] = proofs
        fresh = self.guest("for p in /opt/celikpanel /etc/celikpanel /var/lib/celikpanel " + PRIVATE +
                           "/current-worker-baseline-intent.json; do test ! -e \"$p\" || echo \"present $p\"; done",
                           timeout=60).stdout
        # Read /etc/hosts and nsswitch only: asking the resolver for celikpanel.net before
        # the fixture origin exists would resolve the real name (upd1 2026-09-30).
        hosts = self.guest("cat /etc/hosts", timeout=30).stdout
        nsswitch = self.guest("grep -E '^hosts:' /etc/nsswitch.conf || true", timeout=30).stdout.strip()
        mapped = hosts_mappings(hosts)
        checks.update(proofs=proofs, guest_fresh="present" not in fresh and not mapped,
                      origin_name_before={"hosts_mappings": mapped, "nsswitch_hosts": nsswitch,
                                          "resolver_queried": False})
        if "present" in fresh or mapped:
            raise StepFailed("guest is not fresh: " + (fresh.strip() + " " + "; ".join(mapped)).strip())
        intent = {"schema": INTENT_SCHEMA, "cell": dataclasses.asdict(self.cell), "identity": self.identity,
                  "artifacts": {role: {k: self.artifacts[role][k] for k in ("version", "commit", "sha256")}
                                for role in ("baseline", "good", "defective")},
                  "created_at": utc_now(), "provenance": PROVENANCE}
        self.trial.save(self.root, self.node_name, "upd1-intent.json", encoded(intent))
        checks["helpers"] = self.upload_helpers()
        self.state["helpers_uploaded"] = True
        self.record_json("preflight.json", checks)
        return "passed"

    def origin_check(self, label: str) -> dict:
        """celikpanel.net must resolve only to 127.0.0.1 and the fixture must answer 200."""
        verdict = origin_verdict(self.workload("origin-check", timeout=60))
        self.state.setdefault("origin_checks", {})[label] = verdict
        return verdict

    def origin(self, checks: dict) -> str:
        origin = self.m["origin"]
        keys = origin.prepare_keys(str(self.root), self.node_name)
        self.state["public_key_sha256"] = keys["public_key_sha256"]
        loader = origin.module
        archive_path = Path(self.candidate["archive"])
        clone = Path(self.artifacts["clone"])

        def module(name):
            value = loader(name)
            if name == "candidate_archive":
                value.verify_committed_source = acceptance_source_proof(value.verify_committed_source, archive_path, clone)
            return value
        with patched(origin, "module", module):
            intent = origin.prepare(str(self.root), self.node_name, str(archive_path), CANDIDATE_VERSION,
                                    self.candidate["commit"], CANDIDATE_SEQUENCE, str(clone))
        staged = origin.stage(str(self.root), self.node_name)
        provision = self.guest(shlex.join(["python3", "-I", f"{PRIVATE}/worker-fixture-origin.py", "guest-provision",
                                           "--nonce", self.identity["nonce"]]), timeout=120)
        # L1: an enabled lab unit (never a transient unit), so the origin
        # returns after the restart the Arch installer demands and after a reset.
        # Named cp-lab-*: a celikpanel-* unit file would make the real installer
        # treat the guest as an already-started install (get.sh first_install_has_not_started).
        unit = self.workload("install-origin", timeout=60)
        checks.update(target=intent["target"], provenance=intent["provenance"], staged=staged,
                      provision=json.loads(provision.stdout), origin_unit=unit)
        if any(result.get("returncode") != 0 for result in unit.get("results", [])):
            raise StepFailed(f"fixture origin unit was not enabled and started: {unit.get('results')}")
        verdict = None
        for _ in range(10):
            time.sleep(2)
            verdict = self.origin_check("after-provision")
            if verdict["ok"]:
                break
        checks["origin_check"] = verdict
        if not verdict["ok"]:
            raise StepFailed(f"guest-loopback celikpanel.net fixture origin is not serving: {verdict}")
        self.state["origin_target"] = intent["target"]
        return "passed"

    def baseline_install(self, checks: dict) -> str:
        baseline = self.m["baseline"]
        item = self.artifacts["baseline"]
        wrapped = acceptance_source_proof(baseline.archive_tools.verify_committed_source, Path(item["archive"]),
                                          Path(self.artifacts["clone"]))
        with patched(baseline, "COMMIT", item["commit"]), \
                patched(baseline.archive_tools, "verify_committed_source", wrapped):
            started = baseline.start(self.root, self.record, self.plan, self.node_name, True,
                                     archive_path=item["archive"], archive_sha256=item["sha256"],
                                     public_key_sha256=self.state["public_key_sha256"])
        checks["start"] = started
        deadline = time.monotonic() + 3000
        status = None
        while time.monotonic() < deadline:
            status = baseline.status(self.root, self.record, self.plan, self.node_name)
            if status.get("installation") is not None:
                break
            time.sleep(15)
        checks["status"] = status
        if not status or status.get("installation") is None:
            raise StepInconclusive("baseline installation has no terminal result")
        collected = baseline.collect(self.root, self.record, self.plan, self.node_name)
        checks["collected"] = collected
        installation = status["installation"]
        if installation.get("verified") is not True:
            raise StepFailed(f"baseline installation not verified: {installation.get('error_type')}")
        log = Path(collected["artifacts"][baseline.LOG]["path"]).read_text(errors="replace")
        notice = self.p["install_steps"].installer_restart_notice(log)
        checks["installer_restart_notice"] = notice
        if notice.get("state") == "required":
            # The installer told the owner to restart; the owner does, before any seeding.
            subprocess.run(self.lab.ssh(self.root, self.record, self.node) + ["sudo systemctl reboot"],
                           capture_output=True, timeout=30)
            time.sleep(10)
            checks["owner_restart_seconds"] = self.wait_for_ssh()
            # L1: the fixture origin must come back by itself after that restart.
            verdict = None
            for _ in range(15):
                verdict = self.origin_check("after-owner-restart")
                if verdict["ok"]:
                    break
                time.sleep(4)
            checks["origin_after_restart"] = verdict
            if not verdict["ok"]:
                raise StepFailed(f"the fixture origin did not return after the owner restart: {verdict}")
        return "passed"

    def owner_login(self, checks: dict) -> str:
        credentials = self.workload("credentials")
        self.redactor.register(credentials["password"])
        self.state["username"] = credentials["username"]
        self._password = credentials["password"]
        if not self.tunnel.ensure():
            raise StepInconclusive("SSH tunnel to the guest Panel did not open")
        identity = self.panel_client().login(credentials["username"], self._password)
        availability = self.api("GET", "/api/v1/panel/availability", purpose="usePanelSession availability")
        checks.update(identity={k: identity.get(k) for k in ("username", "role", "effective_role")},
                      availability=availability.json(), tls_leaf=self.transport.leaf_sha256)
        return "passed"

    def license(self, checks: dict) -> str:
        self.redactor.register(ACCEPTANCE_FIXTURE_KEY)
        before = self.api("GET", "/api/v1/panel/license", purpose="LicensePanel status").json() or {}
        access = self.api("GET", "/api/v1/license/access", purpose="LicenseOnboarding access").json() or {}
        checks.update(before={k: before.get(k) for k in ("state", "can_provision", "license_service", "fixture")},
                      access_before=access)
        if access.get("can_use_panel") is not True:
            response = self.api("POST", "/api/v1/panel/license", {"action": "activate", "key": ACCEPTANCE_FIXTURE_KEY},
                                purpose="LicensePanel activate (acceptance fixture key)")
            checks["activation_http"] = response.status
        access = self.api("GET", "/api/v1/license/access", purpose="LicenseOnboarding access").json() or {}
        after = self.api("GET", "/api/v1/panel/license", purpose="LicensePanel status").json() or {}
        checks.update(access_after=access, after={k: after.get(k) for k in ("state", "can_provision", "license_service")})
        if access.get("can_use_panel") is not True:
            raise StepFailed(f"acceptance fixture license not usable: {access}")
        if not str(after.get("license_service", "")).startswith("not contacted"):
            self.finding("license status does not label the acceptance fixture as 'not contacted': "
                         + str(after.get("license_service")))
        return "passed"

    def local_ip(self) -> str:
        output = self.guest("ip -4 -o addr show scope global | awk '{print $4}'", timeout=30).stdout.split()
        addresses = [value.split("/")[0] for value in output]
        preferred = [a for a in addresses if a.startswith("192.0.2.")]
        if not addresses:
            raise StepInconclusive("guest has no global IPv4 address")
        return (preferred or addresses)[0]

    def draft(self, purpose: str, local_ip: str) -> dict:
        base = "upd1-infra.test"
        draft = {"purpose": purpose, "dns_hosting_management": "", "dns_publisher_endpoint": "",
                 "remote_dns_connection_id": "", "panel_domain": f"panel-{self.node_name}.{base}",
                 "mail_hostname": f"mail-{self.node_name}.{base}" if purpose == "web_mail" else "",
                 "dns_mode": "local", "dns_engine": "bind", "dns_role": "primary",
                 "ns1": f"ns1.{base}", "ns2": f"ns2.{base}", "local_ip": local_ip, "peer_ip": "", "peer_ns": "",
                 "node_version": "", "database": ""}
        draft.update(self.setup_draft_override or {})
        draft["purpose"] = purpose
        return draft

    def setup(self, checks: dict) -> str:
        state = self.api("GET", "/api/v1/setup", purpose="ServerSetupGate").json() or {}
        checks["before"] = {k: state.get(k) for k in ("status", "revision", "guidance", "required")}
        checks["dns"] = dns_scope(self.dns_mode)
        checks["draft_choices"] = self.setup_draft_override
        if state.get("guidance") not in ("guided", "manual"):
            state = self.api("PUT", "/api/v1/setup/guidance", {"revision": state.get("revision"), "guidance": "guided"},
                             purpose="ServerSetupChoice guided").json() or {}
        local_ip = self.local_ip()
        plan = None
        for purpose in ("web_mail", "web"):
            saved = self.api("PUT", "/api/v1/setup", {"revision": state.get("revision"), "draft": self.draft(purpose, local_ip)},
                             purpose=f"ServerSetup draft save ({purpose})")
            if saved.status != 200:
                raise StepFailed(f"draft save ({purpose}) returned HTTP {saved.status}: {saved.json()}")
            state = saved.json() or {}
            response = self.api("POST", "/api/v1/setup/plan", {"revision": state.get("revision")},
                                purpose=f"ServerSetup review ({purpose})")
            plan = response.json() or {}
            checks[f"plan_{purpose}"] = {"http": response.status, **{k: plan.get(k) for k in ("id", "can_start", "blockers")}}
            if response.status == 200 and plan.get("can_start"):
                break
            if purpose == "web_mail" and self.cell.mail_required:
                raise StepFailed(f"web_mail setup plan refused on {self.node_name}: {plan.get('blockers')}")
            self.finding(f"{self.node_name}: web_mail setup plan was refused ({plan.get('blockers')}); "
                         "mail is recorded as not provided on this platform")
            state = self.api("GET", "/api/v1/setup", purpose="ServerSetupGate").json() or {}
        if not plan or not plan.get("can_start"):
            raise StepFailed(f"setup plan cannot start: {plan}")
        self.state["purpose"] = purpose
        request_id = secrets.token_hex(16)
        try:
            response = self.api("POST", "/api/v1/setup/start", {"plan_id": plan["id"], "request_id": request_id,
                                                                 "confirmed": True},
                                purpose="ServerSetup start (once)", timeout=120)
            checks["start_http"] = response.status
        except self.p["panel_api"].UnknownOutcome as exc:
            checks["start_outcome"] = f"unknown, reconciling by reads: {exc}"
        deadline = time.monotonic() + 3600
        execution = None
        wait = SetupWait()
        decision = "continue"
        with self.panel_client().polling() as view:
            while time.monotonic() < deadline:
                try:
                    execution = self.api("GET", f"/api/v1/setup/operation?request_id={request_id}", view=view).json()
                except self.p["panel_api"].PanelError as exc:
                    execution = {"poll_error": str(exc)}
                decision = wait.observe(execution, time.monotonic())
                if decision in ("terminal", "settled"):
                    break
                time.sleep(5)
        checks["execution"] = {k: (execution or {}).get(k) for k in ("status", "phase", "error")}
        self.record_json("setup-execution.json", execution)
        self.state["setup_execution"] = execution
        if decision == "settled":
            # H4: the product's fixed public-resolver check cannot pass on an isolated
            # host; like the DNS pair driver, a stable wait there settles this step.
            steps = {s.get("id"): s.get("status") for s in setup_steps(execution)}
            pending = sorted(k for k, v in steps.items() if v == "pending")
            record = {"phase": execution.get("phase"), "code": (execution.get("error") or {}).get("code"),
                      "stable_wait_seconds": wait.seconds(time.monotonic()), "polls": wait.count,
                      "steps": steps, "not_run": pending, "mail_steps_reached": mail_steps_reached(execution)}
            checks["isolated_host_wait"] = record
            self.state["setup_waiting"] = record
            self.finding(f"{self.node_name}: server setup waits at {record['phase']} ({record['code']}) on an "
                         f"isolated host; later wizard steps were not run: {pending}")
            return "observed"
        if not isinstance(execution, dict) or execution.get("status") != "succeeded":
            raise StepFailed(f"server setup did not succeed: {checks['execution']}")
        return "passed"

    def seed(self, checks: dict) -> str:
        # Cron precondition, read-only and before any seeding (product finding P1: the
        # product does not install cron on Debian and masks the Agent's reason as 500).
        # Absence is recorded as "cron: not available on this baseline", not seeded.
        availability = cron_availability(self.workload("cron-availability", timeout=60))
        self.state["cron_availability"] = availability
        checks["cron_availability"] = availability
        domain = "upd1-owner.test"
        marker = "upd1-marker-" + secrets.token_hex(16)
        response = self.api("POST", "/api/v1/domains/create", {"domain": domain, "project_type": "static",
                                                               "ssl_type": "none"},
                            purpose="AddDomainModal create (website)", timeout=600)
        body = response.json() or {}
        domain_id = body.get("DomainID") or body.get("domain_id")
        if response.status != 200 or not isinstance(domain_id, int):
            raise StepFailed(f"domain create returned HTTP {response.status}: {body}")
        content = ("<!doctype html><title>upd1 owner site</title><p>" + marker + "</p>\n")
        written = self.api("POST", f"/api/v1/domains/{domain_id}/files?path=/index.html",
                           {"action": "write", "content": content}, purpose="DomainFileManager save")
        if written.status != 200:
            raise StepFailed(f"index page write returned HTTP {written.status}")
        zone = self.api("GET", f"/api/v1/domains/{domain_id}/dns/zone", purpose="DomainDNSManager zone")
        records = self.api("GET", f"/api/v1/domains/{domain_id}/dns/records", purpose="DomainDNSManager records")
        seeded = {"domain": domain, "domain_id": domain_id, "marker": marker,
                  "index_sha256": hashlib.sha256(content.encode()).hexdigest(),
                  "zone_http": zone.status, "zone": zone.json(), "records_http": records.status,
                  "records_sha256": hashlib.sha256(records.body).hexdigest()}
        mail = {"attempted": self.state.get("purpose") == "web_mail"}
        if mail["attempted"]:
            setup = self.api("GET", f"/api/v1/domains/{domain_id}/mail/setup", purpose="MailSettingsPanel setup")
            password = secrets.token_urlsafe(24)
            self.redactor.register(password)
            address = f"owner@{domain}"
            created = self.api("POST", f"/api/v1/domains/{domain_id}/mail/accounts",
                               {"address": address, "password": password, "quota_mb": 100},
                               purpose="DomainMailManager create account")
            accounts = self.api("GET", f"/api/v1/domains/{domain_id}/mail/accounts", purpose="DomainMailManager list")
            present = address in accounts.text
            mail.update(setup_http=setup.status, create_http=created.status, address=address, listed=present)
            if not present:
                # The wizard never reached its mail steps when it waits earlier (H4).
                reached = mail_steps_reached(self.state.get("setup_execution"))
                mail["setup_mail_steps_reached"] = reached
                if self.cell.mail_required and reached:
                    raise StepFailed(f"mailbox was not created: HTTP {created.status} {created.json()}")
                self.finding(f"{self.node_name}: mailbox creation did not succeed: HTTP {created.status}"
                             + ("" if reached else " (setup waited before its mail steps)"))
        seeded["mail"] = mail
        availability = self.state["cron_availability"]
        command = '/bin/date -u -Iseconds > "$HOME/upd1-cron-stamp.txt"'
        if not availability["available"]:
            seeded["cron"] = dict(availability, seeded=False, listed=False)
            self.finding(f"{self.node_name}: cron: {CRON_NOT_AVAILABLE} (crontab absent before seeding); the owner "
                         "cron job was not created and cron continuity is not measured")
        else:
            cron = self.api("POST", f"/api/v1/domains/{domain_id}/cron",
                            {"schedule": "* * * * *", "command": command, "comment": "upd1 owner cron"},
                            purpose="DomainCronManager create")
            listed = self.api("GET", f"/api/v1/domains/{domain_id}/cron", purpose="DomainCronManager list")
            seeded["cron"] = dict(availability, seeded=True, create_http=cron.status, command=command,
                                  listed="upd1-cron-stamp.txt" in listed.text,
                                  list_sha256=hashlib.sha256(listed.body).hexdigest())
            if not seeded["cron"]["listed"]:
                raise StepFailed(f"cron job was not created although crontab is present: HTTP {cron.status}")
        firewall = self.api("GET", "/api/v1/firewall", purpose="Firewall screen")
        seeded["firewall"] = {"http": firewall.status, "sha256": hashlib.sha256(firewall.body).hexdigest()}
        self.state["seed"] = seeded
        checks["seeded"] = seeded
        self.record_json("seeded.json", seeded)
        return "passed"

    def guest_observation(self, label: str) -> dict:
        since = (dt.datetime.now(dt.timezone.utc) - dt.timedelta(hours=6)).strftime("%Y-%m-%dT%H:%M:%SZ")
        args = ["--zone", self.state["seed"]["domain"], "--name", self.state["seed"]["domain"], "--since", since]
        if self.state.get("request_id"):
            args += ["--operation-id", self.state["request_id"]]
        value = self.helper("guest_probe.py", None, *args, timeout=180)
        self.record_json(f"guest-observation-{label}.json", value)
        return value

    def workload_snapshot(self, label: str, dns_server: str | None = None) -> dict:
        seed = self.state["seed"]
        args = ["--domain", seed["domain"], "--marker", seed["marker"],
                "--dns-server", dns_server or self.state.get("dns_server", "127.0.0.1")]
        if seed["mail"].get("listed"):
            args += ["--smtp", "--mailbox", seed["mail"]["address"]]
        value = self.workload("snapshot", *args, timeout=180)
        self.record_json(f"workload-{label}.json", value)
        return value

    def fetch_shell(self, label: str) -> dict:
        """The saved recovery page and its module, as the browser would have cached them."""
        result = {}
        try:
            self.tunnel.ensure()
            page = self.transport("GET", "/recovery-offline.html", {"Host": f"127.0.0.1:{self.local_port}"}, None, 15)
            result["page"] = {"status": page.status, "sha256": hashlib.sha256(page.body).hexdigest(), "bytes": len(page.body)}
            scripts = re.findall(rb'<script[^>]+src="(/[^"]+\.js)"', page.body)
            for src in scripts[:4]:
                asset = self.transport("GET", src.decode(), {"Host": f"127.0.0.1:{self.local_port}"}, None, 15)
                template = b"recovery status --request-id" in asset.body
                result.setdefault("scripts", []).append({"path": src.decode(), "status": asset.status,
                                                         "sha256": hashlib.sha256(asset.body).hexdigest(),
                                                         "carries_status_command": template})
        except Exception as exc:  # noqa: BLE001 - an unreachable Panel is itself the observation
            result["error"] = type(exc).__name__
        copy_source = Path(self.artifacts["baseline"]["product_web_src"]) / "offline" / "copy.ts"
        try:
            result["texts"] = parse_offline_copy(copy_source.read_text())
        except (OSError, ValueError) as exc:
            result["texts_error"] = str(exc)
        if self.state.get("request_id"):
            result["reference"] = self.state["request_id"]
            result["status_command"] = {lang: shell_status_command(self.state["request_id"], lang) for lang in ("en", "tr")}
        self.record_json(f"offline-shell-{label}.json", result)
        return result

    def host_loop(self) -> None:
        panel = self.p["panel_api"]
        while not self.stop_host_loop.is_set():
            started = time.monotonic()
            sample = {"t": time.time(), "utc": utc_now(), "ssh": {"ok": self.ssh_reachable(6)}}
            try:
                self.tunnel.ensure(timeout=5)
                response = self.transport("GET", "/api/v1/panel/availability",
                                          {"Host": f"127.0.0.1:{self.local_port}", "Accept": "application/json"}, None, 5)
                sample["panel"] = {"ok": response.status in (200, 401), "status": response.status}
            except panel.PinMismatch:
                sample["panel"] = {"ok": None, "error": "PinMismatch"}
            except Exception as exc:  # noqa: BLE001
                sample["panel"] = {"ok": False, "error": type(exc).__name__}
            self.host_samples.append(sample)
            self.stop_host_loop.wait(max(0.5, SAMPLE_INTERVAL_S - (time.monotonic() - started)))

    def pre_state(self, checks: dict) -> str:
        self.state["pre_observation"] = self.guest_observation("pre-update")
        self.state["pre_workload"] = self.workload_snapshot("pre-update")
        if self.dns_mode == "local" and not (self.state["pre_workload"]["dns_udp"].get("ok")
                                             and self.state["pre_workload"]["dns_tcp"].get("ok")):
            # The zone may be served only on the setup address; the owner's resolver would use that.
            address = self.local_ip()
            retry = self.workload_snapshot("pre-update-setup-address", address)
            if retry["dns_udp"].get("ok") and retry["dns_tcp"].get("ok"):
                self.state["dns_server"] = address
                self.state["pre_workload"] = retry
        self.state["pre_shell"] = self.fetch_shell("pre-update")
        seed = self.state["seed"]
        args = ["--domain", seed["domain"], "--marker", seed["marker"], "--interval", str(SAMPLE_INTERVAL_S),
                "--dns-server", self.state.get("dns_server", "127.0.0.1")]
        if seed["mail"].get("listed"):
            args.append("--smtp")
        checks["sampler"] = self.workload("install-sampler", *args)
        self.state["clock_skew"] = self.clock_skew()
        checks["clock_skew_seconds"] = self.state["clock_skew"]
        threading.Thread(target=self.host_loop, name="upd1-host-loop", daemon=True).start()
        deadline = time.monotonic() + 180
        samples = []
        while time.monotonic() < deadline:
            samples = self.read_samples()
            stamps = {s["cron"].get("mtime") for s in samples if s.get("cron", {}).get("ok")}
            if len(samples) >= 3 and len(stamps) >= 2:
                break
            time.sleep(10)
        pre = self.state["pre_workload"]
        checks.update(samples_before_start=len(samples),
                      web_ok=pre["web"].get("ok"), dns_ok=pre["dns_udp"].get("ok") and pre["dns_tcp"].get("ok"),
                      smtp_ok=pre["smtp"].get("ok"), cron_advancing=len({s["cron"].get("mtime") for s in samples
                                                                         if s.get("cron", {}).get("ok")}) >= 2,
                      firewall_tables=pre["firewall"].get("tables"), timers=sorted(pre["timers"]),
                      database=self.state["pre_observation"].get("database", {}).get("status"))
        checks["dns"] = dns_scope(self.dns_mode)
        # External DNS (H5): DNS is not provided by this run and never counted.
        failing = [k for k in ("web_ok", "dns_ok") if not checks[k] and not (k == "dns_ok" and self.dns_mode == "external")]
        if seed["mail"].get("listed") and not checks["smtp_ok"]:
            failing.append("smtp_ok")
        cron_seeded = bool(seed["cron"].get("seeded"))
        self.state["cron_precondition"] = cron_seeded and checks["cron_advancing"]
        if cron_seeded and not checks["cron_advancing"]:
            self.finding(f"{self.node_name}: the owner's cron job did not run within 180 s before the update "
                         "(cron daemon or tenant crontab not active); cron continuity is not measurable")
        if failing:
            raise StepFailed("workloads are not healthy before the update: " + ", ".join(failing))
        return "passed"

    def clock_skew(self) -> float:
        """Guest minus host wall clock, so host instants can be placed on guest sample times."""
        before = time.time()
        guest = float(self.guest("date +%s.%N", timeout=30).stdout.strip())
        after = time.time()
        return round(guest - (before + after) / 2.0, 3)

    def read_samples(self) -> list[dict]:
        offset = self.state.get("sample_offset", 0)
        value = self.workload("samples", "--offset", str(offset), timeout=60)
        raw = base64.b64decode(value["jsonl_base64"])
        self.state["sample_offset"] = value["next_offset"]
        self.state.setdefault("samples", []).extend(json.loads(line) for line in raw.splitlines() if line)
        return self.state["samples"]

    def arm(self, checks: dict) -> str:
        # L1: the candidate is offered only by the guest-loopback fixture origin; prove it
        # is still the only answer for celikpanel.net before the update check.
        checks["origin_check"] = self.origin_check("before-arm")
        if not checks["origin_check"]["ok"]:
            raise StepFailed(f"the fixture origin is not serving before arm: {checks['origin_check']}")
        check = self.api("GET", "/api/v1/panel/update/check", purpose="PanelUpdateCard check", timeout=60)
        body = check.json() or {}
        target = body.get("target") or {}
        checks["check"] = {"http": check.status, "body": body}
        expected = self.state["origin_target"]
        if (check.status != 200 or body.get("available") is not True or target.get("version") != CANDIDATE_VERSION
                or target.get("commit") != self.candidate["commit"] or target.get("sequence") != str(CANDIDATE_SEQUENCE)
                or target.get("archive_sha256") != expected["archive_sha256"]
                or body.get("current_version") != BASELINE_VERSION):
            raise StepFailed(f"update check does not offer the sealed fixture candidate: {body}")
        self.state["check"] = body
        request_id = secrets.token_hex(16)
        self.state["request_id"] = request_id
        proofs = self.state["proofs"]
        role = "defective" if self.cell.variant == "defective" else "good"
        intent = {"schema": OBSERVER_INTENT_SCHEMA, "identity": self.identity, "operation_id": request_id,
                  "mode": "checkpoint" if self.cell.recovery_fault else "watch",
                  "baseline": {"version": BASELINE_VERSION, "commit": self.artifacts["baseline"]["commit"],
                               "agent_sha256": proofs["baseline"]["agent_sha256"],
                               "panel_sha256": proofs["baseline"]["panel_sha256"]},
                  "target": {"version": CANDIDATE_VERSION, "commit": self.candidate["commit"],
                             "agent_sha256": proofs[role]["agent_sha256"], "panel_sha256": proofs[role]["panel_sha256"]},
                  "recovery_fault": self.cell.recovery_fault}
        name = f"owner-update-observer-{request_id}.json"
        saved = self.trial.save(self.root, self.node_name, name, encoded(intent))
        self.lab.put_file(self.root, self.record, self.plan, self.node_name, Path(saved["path"]), name)
        unit = f"celikpanel-lab-owner-update-observer-{request_id}.service"
        argv = ["systemd-run", f"--unit={unit}", "--no-block", "--property=RuntimeMaxSec=3700",
                "--property=TimeoutStopSec=10", "--property=UMask=0077",
                f"--property=ExecStopPost=-/usr/bin/systemctl thaw celikpanel-self-update-{request_id}.service",
                "python3", "-I", f"{PRIVATE}/guest_owner_update_observer.py", "--lab-nonce", self.identity["nonce"],
                "--vm-uuid", self.identity["vm_uuid"], "--cell-id", self.identity["cell_id"], "--node", self.node_name,
                "--operation-id", request_id, "--execute"]
        self.guest(shlex.join(argv), timeout=60)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            events = self.observer_events()
            if events and events[0].get("event") == "armed":
                checks.update(request_id=request_id, observer_intent=intent, observer_unit=unit)
                return "passed"
            time.sleep(0.5)
        raise StepInconclusive("observer did not report armed; the update is not started")

    def observer_events(self) -> list[dict]:
        rid = self.state["request_id"]
        body = ("python3 -I - <<'CP_UPD1_EVENTS'\nimport base64\nfrom pathlib import Path\n"
                f"p=Path('{PRIVATE}/owner-update-observer-{rid}.jsonl')\n"
                "print(base64.b64encode(p.read_bytes() if p.exists() else b'').decode())\nCP_UPD1_EVENTS\n")
        raw = base64.b64decode(self.guest(body, timeout=30).stdout.strip() or b"")
        events = [json.loads(line) for line in raw.splitlines() if line]
        for event in events:
            if (event.get("schema") != OBSERVER_EVENT_SCHEMA or event.get("operation_id") != rid
                    or event.get("identity") != {k: self.identity[k] for k in ("nonce", "vm_uuid", "cell_id", "node")}):
                raise StepInconclusive("observer event identity differs")
        return events

    def owner_start(self, checks: dict) -> str:
        readiness = None
        deadline = time.monotonic() + 600
        while time.monotonic() < deadline:
            readiness = self.api("GET", "/api/v1/host-mutation-readiness", purpose="PanelUpdateCard readiness").json() or {}
            if readiness.get("ready") is True:
                break
            checks.setdefault("readiness_waits", []).append(readiness)
            time.sleep(15)
        checks["readiness"] = readiness
        if not readiness or readiness.get("ready") is not True:
            raise StepFailed(f"host mutation readiness never allowed the owner to start: {readiness}")
        check = self.state["check"]
        body = {"request_id": self.state["request_id"], "confirmed": True, "current_version": check["current_version"],
                "current_commit": check["current_commit"], **check["target"]}
        attempt = {"schema": "celikpanel/upd1-owner-start-attempt/v1", "request_id": self.state["request_id"],
                   "body": body, "at": utc_now()}
        self.trial.save(self.root, self.node_name, f"upd1-owner-start-{self.state['request_id']}.json", encoded(attempt))
        self.state["started_at"] = time.time()
        try:
            response = self.api("POST", "/api/v1/panel/update/start", body, purpose="SystemUpdateOperation start (once)",
                                timeout=START_REQUEST_TIMEOUT_S)
            checks["start"] = {"http": response.status, "body": response.json()}
            if response.status != 200:
                raise StepFailed(f"update start refused: HTTP {response.status} {response.json()}")
        except self.p["panel_api"].UnknownOutcome as exc:
            checks["start"] = {"outcome": "unknown; reconciled only by status reads", "note": str(exc)}
        return "passed"

    def status_sample(self, view: Any, index: int) -> dict:
        rid = self.state["request_id"]
        sample: dict[str, Any] = {"index": index, "t": time.time(), "utc": utc_now()}
        api_update = api_recovery = None
        try:
            self.tunnel.ensure(timeout=5)
            update = self.api("GET", f"/api/v1/panel/update/status?request_id={rid}", view=view, timeout=10,
                              purpose="SystemUpdateOperation status (read-only poll)")
            sample["update_status"] = {"http": update.status, "body": update.json()}
            api_update = update.json() if update.status == 200 else None
            recovery = self.api("GET", f"/api/v1/recovery/status?request_id={rid}", view=view, timeout=15,
                                purpose="RecoveryStatus (read-only)")
            sample["recovery_api"] = {"http": recovery.status, "body": recovery.json()}
            api_recovery = recovery.json() if recovery.status == 200 else None
        except Exception as exc:  # noqa: BLE001 - Panel unreachable is the expected observation
            sample["panel_error"] = type(exc).__name__
            try:
                page = self.transport("GET", "/recovery-offline.html", {"Host": f"127.0.0.1:{self.local_port}"}, None, 5)
                sample["shell_fetch"] = {"http": page.status}
            except Exception as shell_exc:  # noqa: BLE001
                sample["shell_fetch"] = {"error": type(shell_exc).__name__,
                                         "note": "only a browser-saved copy of the shell can show while the Panel is down"}
        cli = None
        try:
            cli_raw = self.workload("cli-status", "--request-id", rid, timeout=45)
            sample["cli"] = cli_raw
            cli = json.loads(cli_raw["json"]["stdout"] or "null")
        except Exception as exc:  # noqa: BLE001
            sample["cli_error"] = type(exc).__name__
        shell = {"reference": rid, "status_command": shell_status_command(rid, "en")}
        sample["agreement"] = status_agreement(rid, api_recovery, cli, shell)
        sample["guidance_api"] = recovery_guidance(self.translator, api_recovery) if api_recovery else None
        sample["update_card"] = update_card_guidance(self.translator, api_update)
        sample["guidance_cli"] = recovery_guidance(self.translator, cli) if cli else None
        sample["observed"] = cli if isinstance(cli, dict) and cli.get("observation") == "known" else api_recovery
        if sample["observed"] and recovery_guidance(self.translator, sample["observed"])["no_actor_or_action"]:
            self.finding(f"status without actor/action text: {json.dumps(sample['observed'], sort_keys=True)}")
        return sample

    def track(self, checks: dict, *, label: str = "track") -> str:
        rid = self.state["request_id"]
        reboot_thread = None
        if self.cell.recovery_fault and self.cell.recovery_fault["action"] == "reboot" and not self.state.get("reboot"):
            reboot_thread = threading.Thread(target=self.reboot_when_ready, name="upd1-reboot", daemon=True)
            reboot_thread.start()
        delay = POLL_MIN_MS
        previous = None
        index = len(self.state.get("status_samples", []))
        deadline = time.monotonic() + 5400
        final = None
        while time.monotonic() < deadline:
            with self.panel_client().polling() as view:
                sample = self.status_sample(view, index)
            index += 1
            self.state.setdefault("status_samples", []).append(sample)
            self.ev.write_json(f"{self.step_dir}/samples/{index:04d}.json", sample)
            observed = sample.get("observed")
            state = classify_status(observed)
            if state in ("terminal", "paused"):
                final = observed
                break
            key = json.dumps([sample.get("update_status"), observed], sort_keys=True, default=str)
            delay = next_delay_ms(delay, key != previous)
            previous = key
            time.sleep(delay / 1000.0)
        if reboot_thread is not None:
            reboot_thread.join(timeout=5)
        self.state["final_status"] = final
        self.state["terminal_at"] = time.time() if final else None
        checks.update(samples=index, final=final, agreement=agreement_verdict(
            [s["agreement"] for s in self.state["status_samples"]]), reboot=self.state.get("reboot"))
        if final is None:
            raise StepInconclusive("no terminal or paused state was observed within 90 minutes")
        if classify_status(final) == "paused":
            self.state["paused"] = final
            return "observed"
        return "passed" if checks["agreement"]["verdict"] != "failed" else "failed"

    def reboot_when_ready(self) -> None:
        """Debian second fault: one QMP reset at the recovery helper's reboot_ready proof."""
        native, rid = self.m["native"], self.state["request_id"]
        intent = {"identity": self.identity, "operation_id": rid, "recovery_fault": self.cell.recovery_fault}
        result: dict[str, Any] = {"status": "waiting"}
        self.state["reboot"] = result
        deadline = time.monotonic() + 3600
        try:
            while time.monotonic() < deadline:
                events = self.observer_events()
                kinds = [e["event"] for e in events]
                if "released" in kinds and "recovery_fault_armed" not in kinds:
                    result.update(status="fault-not-armed", observer=kinds)
                    return
                if "recovery_fault_armed" not in kinds:
                    time.sleep(0.5)
                    continue
                try:
                    recovery, raw, fault_events = native.read_guest(self.root, self.record, self.plan, self.node_name, intent)
                except subprocess.CalledProcessError:
                    time.sleep(0.3)
                    continue
                if fault_events and fault_events[-1].get("event") == "released":
                    result.update(status="checkpoint-missed", events=[e["event"] for e in fault_events])
                    return
                if fault_events and fault_events[-1].get("event") == "reboot_ready":
                    break
                time.sleep(0.2)
            else:
                result["status"] = "timeout"
                return
            expected = native.qemu_identity(self.node)
            connection = native.QMP(self.node, expected)
            try:
                recovery, raw, fault_events = native.read_guest(self.root, self.record, self.plan, self.node_name,
                                                                intent, reboot_proof=True)
                proof_at = time.monotonic()
                proof = recovery.get("reboot_proof")
                if not proof or proof["worker"]["boot_id"] != fault_events[-1]["worker"]["boot_id"]:
                    result["status"] = "reboot-proof-unavailable"
                    return
                refs = native.save_collection(self.root, self.node_name, recovery, raw)
                attempt = {"schema": "celikpanel/upd1-reboot-attempt/v1", "identity": self.identity,
                           "operation_id": rid, "created_at": utc_now(), "qemu": expected,
                           "checkpoint_sha256": proof["checkpoint_sha256"], "before_boot_id": proof["worker"]["boot_id"],
                           "evidence": refs, "command": "system_reset", "scope": "registered-disposable-QEMU-only"}
                self.trial.save(self.root, self.node_name, f"upd1-reboot-attempt-{rid}.json", encoded(attempt))
                if time.monotonic() - proof_at > 3:
                    result["status"] = "proof-expired-no-reset"
                    return
                connection.reset()
                reset_at = time.time()
                self.state["resets"].append(reset_at)
                result.update(status="reset-submitted-once", reset_at=reset_at, before_boot_id=proof["worker"]["boot_id"])
            finally:
                connection.close()
            self.tunnel.close()
            time.sleep(5)
            result["ssh_return_seconds"] = self.wait_for_ssh()
            result["after_boot_id"] = self.guest("cat /proc/sys/kernel/random/boot_id", timeout=30).stdout.strip()
            result["new_boot"] = result["after_boot_id"] != result["before_boot_id"]
        except Exception as exc:  # noqa: BLE001 - recorded; a reset is never retried
            result.update(status="error", error=f"{type(exc).__name__}: {exc}")

    def owner_continuation(self, checks: dict) -> str:
        paused = self.state.get("paused")
        if not paused:
            checks["required"] = False
            return "skipped"
        rid = self.state["request_id"]
        last = self.state["status_samples"][-1]
        cli_texts = {lang: (last.get("cli", {}).get(lang, {}) or {}).get("stdout", "") for lang in ("en", "tr")}
        api_body = (last.get("recovery_api") or {}).get("body")
        shell = self.fetch_shell("paused")
        views = {
            "cli": {"automatic_recovery": paused.get("automatic_recovery"),
                    "names_journal_command": all("journalctl -u celikpanel-release-recovery.service" in t
                                                 for t in cli_texts.values()),
                    "texts": cli_texts},
            "api": ({"automatic_recovery": api_body.get("automatic_recovery"),
                     "guidance": recovery_guidance(self.translator, api_body)}
                    if isinstance(api_body, dict) else {"unavailable": last.get("panel_error") or "no response"}),
            "offline_shell": {"note": "the saved page carries no exhaustion state by design; it names the read-only "
                                      "status command whose output does", "status_command": shell.get("status_command"),
                              "texts": shell.get("texts")},
        }
        checks["views"] = views
        if not views["cli"]["names_journal_command"]:
            self.finding("paused CLI text does not name the recovery journal command in both languages")
        if isinstance(api_body, dict) and api_body.get("automatic_recovery") != "paused_retry_limit":
            raise StepFailed("the Panel recovery status does not report the exhausted budget")
        if not isinstance(api_body, dict):
            self.finding("while the recovery budget was exhausted the Panel recovery-status API was unavailable; "
                         "only the root CLI showed the exhausted state")
        snapshot = self.pending_snapshot()
        attempt = {"schema": "celikpanel/upd1-owner-continuation-attempt/v1", "request_id": rid, "snapshot": snapshot,
                   "at": utc_now(), "reason": "product reported paused_retry_limit; owner follows its printed retry"}
        self.trial.save(self.root, self.node_name, f"upd1-owner-continuation-{rid}.json", encoded(attempt))
        try:
            result = self.workload("owner-retry", "--request-id", rid, "--snapshot-name", snapshot, "--execute",
                                   timeout=3700)
        except subprocess.SubprocessError as exc:
            checks["result"] = f"unknown ({type(exc).__name__}); never repeated - status reads only"
            result = None
        checks["retry"] = result
        self.state["owner_continued"] = True
        self.state.pop("paused", None)
        return "observed"

    def pending_snapshot(self) -> str:
        for event in reversed(self.observer_events()):
            if event.get("snapshot"):
                return event["snapshot"]
        raise StepInconclusive("the operation snapshot is unknown; owner continuation is not attempted")

    def terminal(self, checks: dict) -> str:
        final = self.state.get("final_status")
        post_obs = self.guest_observation("post-recovery")
        post_work = self.workload_snapshot("post-recovery")
        pre_work = self.state["pre_workload"]
        expected = self.artifacts["baseline"] if self.cell.variant == "defective" else self.candidate
        builds = post_work.get("build", {})
        identity_ok = all(f"version={expected['version']}\ncommit={expected['commit']}\n" == (builds.get(n) or {}).get("identity")
                          for n in ("agent", "panel"))
        services = post_obs.get("services", {})
        running = {unit: ((value or {}).get("running_executable") or {}).get("sha256") is not None
                   and ((value or {}).get("running_executable") or {}).get("sha256")
                   == ((value or {}).get("installed_executable") or {}).get("sha256")
                   for unit, value in services.items()}
        # L2: tables the waiting setup rewrites are excluded only when this run recorded that wait.
        database = compare_databases(self.state["pre_observation"].get("database", {}), post_obs.get("database", {}),
                                     volatile_tables(bool(self.state.get("setup_waiting"))))
        timers = compare_states(pre_work.get("timers", {}), post_work.get("timers", {}))
        firewall_equal = pre_work.get("firewall", {}).get("sha256") == post_work.get("firewall", {}).get("sha256")
        # Owner view: a fresh login, then the update card and recovery reader.
        self.client = None
        self.tunnel.ensure()
        login_ok = True
        try:
            self.panel_client().login(self.state["username"], self._password)
        except Exception as exc:  # noqa: BLE001
            login_ok = False
            checks["login_error"] = type(exc).__name__
        card = {}
        if login_ok:
            rid = self.state["request_id"]
            for name, path in (("availability", "/api/v1/panel/availability"), ("version", "/api/v1/panel/version"),
                               ("check", "/api/v1/panel/update/check"),
                               ("status", f"/api/v1/panel/update/status?request_id={rid}"),
                               ("recovery", f"/api/v1/recovery/status?request_id={rid}"),
                               ("license", "/api/v1/license/access"),
                               ("domains", "/api/v1/domains"),
                               ("cron", f"/api/v1/domains/{self.state['seed']['domain_id']}/cron")):
                response = self.api("GET", path, purpose=f"terminal {name}")
                card[name] = {"http": response.status, "body": response.json()}
            card["recovery_guidance"] = recovery_guidance(self.translator, card["recovery"]["body"])
            card["update_card"] = update_card_guidance(self.translator, card["status"]["body"])
            if card["update_card"]["untranslated_summary"]:
                self.finding("the update card shows the server summary verbatim in both languages: "
                             + card["update_card"]["summary"])
            if self.state["seed"]["mail"].get("listed"):
                accounts = self.api("GET", f"/api/v1/domains/{self.state['seed']['domain_id']}/mail/accounts",
                                    purpose="terminal mail accounts")
                card["mail_listed"] = self.state["seed"]["mail"]["address"] in accounts.text
        seeded_rows = {"domain": any(isinstance(d, dict) and d.get("id") == self.state["seed"]["domain_id"]
                                     for d in (card.get("domains", {}).get("body") or [])),
                       "cron": "upd1-cron-stamp.txt" in json.dumps(card.get("cron", {}).get("body")),
                       "mail": card.get("mail_listed")}
        checks.update(final=final, outcome=classify_outcome(self.cell.variant, final, bool(self.state.get("owner_continued"))),
                      build_identity_ok=identity_ok, builds=builds, running_matches_installed=running,
                      floor=post_work.get("floor"), foundation=post_work.get("foundation"),
                      transaction=post_work.get("transaction"), database=database, timers=timers,
                      firewall_equal=firewall_equal, site_marker=post_work.get("web", {}).get("marker"),
                      dns=post_work.get("dns_udp", {}).get("ok"), mailbox=post_work.get("mailbox"),
                      smtp=post_work.get("smtp", {}).get("ok"), login_ok=login_ok, update_card=card,
                      seeded_rows=seeded_rows)
        failures = []
        if not identity_ok:
            failures.append("installed build identity is not the expected release")
        if not all(running.values()):
            failures.append("running executables differ from installed")
        if self.cell.variant == "defective" and database["verdict"] not in ("equal", "equal-except-volatile"):
            failures.append(f"database differs from the pre-update digest: {database.get('unexpected')}")
        if not timers["equal"]:
            failures.append(f"timers changed: {sorted(timers['changed'])}")
        if not firewall_equal:
            failures.append("firewall ruleset changed")
        checks["dns_scope"] = dns_scope(self.dns_mode)
        if not checks["site_marker"]:
            failures.append("site marker not served")
        if self.dns_mode == "local" and not checks["dns"]:
            failures.append("DNS SOA not served")
        if self.state["seed"]["mail"].get("listed") and not (post_work.get("mailbox", {}).get("present") and checks["smtp"]):
            failures.append("mailbox or submission service missing")
        cron_seeded = bool(self.state["seed"]["cron"].get("seeded"))
        if not login_ok or not seeded_rows["domain"] or (cron_seeded and not seeded_rows["cron"]):
            failures.append("owner login or seeded rows missing")
        if failures:
            raise StepFailed("; ".join(failures))
        return "passed"

    def collect(self, checks: dict) -> str:
        """L3: runs whatever step stopped the cell, as long as the guest was prepared.

        Each part is attempted on its own; a part that cannot be read is listed
        under ``unavailable`` and the rest is still kept.
        """
        self.stop_host_loop.set()
        if not self.state.get("helpers_uploaded"):
            checks["reason"] = "the guest was never prepared (preflight stopped before the lab helpers were uploaded)"
            return "skipped"
        rid = self.state.get("request_id")
        unavailable: dict[str, str] = {}

        def attempt(label: str, function: Callable[[], Any]) -> Any:
            try:
                return function()
            except Exception as exc:  # noqa: BLE001 - one missing part never loses the others
                unavailable[label] = self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]
                return None

        checks["stopped_after"] = [s["name"] for s in self.steps if s["verdict"] not in ("passed", "observed", "skipped")
                                   and s["name"] != "collect"][:1]
        samples = attempt("guest-samples", self.read_samples) or []
        self.ev.write_text(f"{self.step_dir}/guest-samples.jsonl", "\n".join(json.dumps(s, sort_keys=True) for s in samples))
        self.ev.write_text(f"{self.step_dir}/host-samples.jsonl",
                           "\n".join(json.dumps(s, sort_keys=True) for s in self.host_samples))
        journals = {}
        for label, units in journal_groups(rid).items():
            value = attempt(f"journal-{label}", lambda units=units: self.workload(
                "journal", "--since=-12h", "--lines", "20000", *sum((["--unit", u] for u in units), []), timeout=120))
            if value is not None:
                journals[label] = value.get("stdout", "")
                attempt(f"journal-{label}-write",
                        lambda label=label: self.ev.write_text(f"{self.step_dir}/journal-{label}.txt", journals[label]))
        observations = attempt("observation-records", lambda: json.loads(
            self.guest(observation_records_script(rid), timeout=30).stdout))
        if observations is not None:
            attempt("observation-records-write", lambda: self.record_json("observation-records.json", observations))
        observer = (attempt("observer-events", self.observer_events) or []) if rid else []
        self.record_json("observer-events.json", observer)
        snapshot = next((e.get("snapshot") for e in reversed(observer) if e.get("snapshot")), None)
        budget = attempt("budget", lambda: self.workload("budget", *(["--snapshot-name", snapshot] if snapshot else [])))
        if budget is not None:
            self.record_json("budget.json", budget)
        attempts = attempts_from_receipts((budget or {}).get("receipts", []), snapshot)
        attempts["journal_admissions"] = parse_dispatch_journal(journals.get("product", ""))
        self.state["attempts"] = attempts
        if rid and self.cell.recovery_fault:
            try:
                native = self.m["native"]
                value, raw, events = native.read_guest(self.root, self.record, self.plan, self.node_name,
                                                       {"identity": self.identity, "operation_id": rid,
                                                        "recovery_fault": self.cell.recovery_fault})
                checks["recovery_fault_events"] = [e.get("event") for e in events]
                self.ev.write_text(f"{self.step_dir}/recovery-fault-events.jsonl", raw.decode())
            except Exception as exc:  # noqa: BLE001
                checks["recovery_fault_events"] = f"unavailable: {type(exc).__name__}"
        checks.update(guest_samples=len(samples), host_samples=len(self.host_samples), journals=sorted(journals),
                      observer=[e.get("event") for e in observer], attempts=attempts, unavailable=unavailable)
        self.state["collect_unavailable"] = unavailable
        return "passed" if not unavailable else "inconclusive"

    def cron_scope(self) -> str:
        availability = self.state.get("cron_availability") or {}
        if availability and not availability.get("available"):
            return CRON_NOT_AVAILABLE
        return "measured" if self.state.get("cron_precondition") else "not-running-before-update"

    def verdicts(self, checks: dict) -> str:
        if "guest-samples" in (self.state.get("collect_unavailable") or {}):
            raise StepInconclusive("the guest sample series could not be read; no outage window is judged")
        samples = self.state.get("samples", [])
        skew = self.state.get("clock_skew") or 0.0
        # Host instants on the guest clock (the reset may also move the guest clock; recorded, not corrected).
        resets = [reset + skew for reset in self.state["resets"]]
        started = self.state["started_at"] + skew if self.state.get("started_at") else None
        terminal_at = self.state["terminal_at"] + skew if self.state.get("terminal_at") else None
        checks["clock_skew_seconds"] = skew
        per = workload_verdicts(samples, resets, mail_listed=bool(self.state.get("seed", {}).get("mail", {}).get("listed")),
                                cron=self.cron_scope(), dns_mode=self.dns_mode)
        panel_windows = classify_windows(outage_windows(samples, "panel"), resets)
        per["panel"] = dict(panel_verdict(panel_windows, started, terminal_at), windows=panel_windows)
        host_panel = outage_windows(self.host_samples, "panel")
        checks.update(workloads=per, host_panel_windows=host_panel, host_ssh_windows=outage_windows(self.host_samples, "ssh"),
                      agreement=agreement_verdict([s["agreement"] for s in self.state.get("status_samples", [])]),
                      outcome=classify_outcome(self.cell.variant, self.state.get("final_status"),
                                               bool(self.state.get("owner_continued"))),
                      attempts=self.state.get("attempts"))
        interrupted = [k for k in WORKLOADS if per[k]["verdict"] == "interrupted"]
        if interrupted or per["panel"]["verdict"] != "down-only-during-transaction":
            raise StepFailed("workload interruption outside the expected windows: "
                             + ", ".join(interrupted + ([] if per["panel"]["verdict"] == "down-only-during-transaction"
                                                        else ["panel"])))
        return "passed"

    # -- orchestration ---------------------------------------------------------------

    def execute(self) -> dict:
        self.step("preflight", self.preflight)
        self.step("origin", self.origin, needs=("preflight",))
        self.step("baseline-install", self.baseline_install, needs=("origin",))
        self.step("owner-login", self.owner_login, needs=("baseline-install",))
        self.step("license", self.license, needs=("owner-login",))
        self.step("setup", self.setup, needs=("license",))
        self.step("seed", self.seed, needs=("setup",))
        self.step("pre-state", self.pre_state, needs=("seed",))
        self.step("arm", self.arm, needs=("pre-state",))
        self.step("owner-start", self.owner_start, needs=("arm",))
        self.step("track", self.track, needs=("owner-start",))
        self.step("owner-continuation (required)", self.owner_continuation, needs=("track",))
        if self.state.get("owner_continued"):
            self.step("track-after-owner-continuation", lambda checks: self.track(checks, label="after-owner"),
                      needs=("owner-continuation (required)",))
        self.step("terminal", self.terminal, needs=("owner-start",))
        # L3: collect is attempted whatever stopped the cell (it skips itself only when
        # the guest was never prepared), so a cell stopped at seed or earlier keeps its journals.
        self.step("collect", self.collect)
        self.step("verdicts", self.verdicts, needs=("owner-start",))
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        result = {"schema": RESULT_SCHEMA, "native_evidence": False, "cell": dataclasses.asdict(self.cell),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": self.state.get("request_id"), "provenance": PROVENANCE,
                  "artifacts": {role: {k: self.artifacts[role][k] for k in ("version", "commit", "sha256")}
                                for role in ("baseline", "good", "defective")},
                  "outcome": {"classification": classify_outcome(self.cell.variant, self.state.get("final_status"),
                                                                 bool(self.state.get("owner_continued"))),
                              "final_status": self.state.get("final_status"),
                              "owner_continuation": bool(self.state.get("owner_continued")),
                              "attempts": self.state.get("attempts"), "reboot": self.state.get("reboot"),
                              "pin_changes": self.state.get("pin_changes", [])},
                  "scope": result_scope(self.dns_mode, self.state.get("cron_availability"),
                                        self.state.get("setup_waiting"), self.state.get("origin_checks")),
                  "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")}
                            for s in self.steps],
                  "overall": overall(verdicts),
                  "note": "Observations for the owner's review; the P0 rows are judged separately."}
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)


# ---------------------------------------------------------------------------
# Small helpers and CLI
# ---------------------------------------------------------------------------

def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def encoded(value: Any) -> bytes:
    return (json.dumps(value, sort_keys=True) + "\n").encode()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    fix = sub.add_parser("fixture-source", help="edit a disposable clone (used by build-upd1-artifacts.sh)")
    fix.add_argument("--repo", required=True, type=Path)
    fix.add_argument("--kind", required=True, choices=("baseline", "good", "defective"))
    fix.add_argument("--previous-commit")
    prove = sub.add_parser("prove", help="read-only host proof of all three archives (no guest)")
    prove.add_argument("--artifacts", required=True, type=Path)
    for name in ("plan", "run"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--cell", required=True, choices=sorted(CELLS))
        cmd.add_argument("--artifacts", required=True, type=Path)
        cmd.add_argument("--work-root", required=True)
        cmd.add_argument("--local-port", type=int, default=18443)
        cmd.add_argument("--setup-draft-json", type=Path, help="optional draft field overrides (owner choices); "
                         "--dns-mode local needs peer_ip and peer_ns here")
        cmd.add_argument("--dns-mode", choices=DNS_MODES, default=DEFAULT_DNS_MODE,
                         help="external (default): DNS hosted elsewhere on the single node, recorded as not provided "
                              "by this run (covered by the item 2 pair runs); local: reserved for a two-node variant")
        if name == "plan":
            cmd.add_argument("--dry-run", action="store_true", help="validate the plan without any guest")
        else:
            cmd.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    if args.command == "fixture-source":
        print(json.dumps(fixture_source(args.repo, args.kind, args.previous_commit)))
        return 0
    if args.command == "prove":
        document = validate_artifacts(json.loads(args.artifacts.read_text()))
        print(json.dumps({"schema": "celikpanel/upd1-artifact-proof/v1", "native_evidence": False,
                          "proofs": prove_artifacts(document, ("baseline", "good", "defective"))},
                         indent=2, sort_keys=True))
        return 0
    cell = validate_cell(args.cell)
    validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    document = json.loads(args.artifacts.read_text())
    draft = json.loads(args.setup_draft_json.read_text()) if args.setup_draft_json else None
    if draft is not None and not isinstance(draft, dict):
        parser.error("--setup-draft-json must be a JSON object of draft fields")
    try:
        choices = setup_draft_choice(args.dns_mode, draft)
    except ValueError as exc:
        parser.error(str(exc))
    if args.command == "plan":
        validate_artifacts(document, check_files=not args.dry_run)
        plan = build_plan(cell, document, args.work_root, args.local_port, args.dns_mode)
        plan["draft_choices"] = choices
        print(json.dumps(plan, indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    validate_artifacts(document)
    result = Trial(cell, document, args.work_root, args.local_port, draft, args.dns_mode).execute()
    print(json.dumps({"overall": result["overall"], "outcome": result["outcome"]["classification"],
                      "request_id": result["request_id"]}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
