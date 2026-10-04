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

upd3 adds two candidate-panel start kinds (product 8ffc5e06): ``start-check``
(the read-only start check fails after the database publication -> automatic
rollback) and ``real-start`` (the check passes, the real start fails after
completion.pending -> forward completion to its limit, then the pause; the
owner retry is printed but never run). Each is judged by its own rules
(``judge_start_check`` / ``judge_real_start``) from the product's catalogues and
recovery CLI source of the built commit and the CLI output verbatim.

upd4 (after the upd3 native run) makes the run-copy corrections permanent -
H8 (``SettledFailure``: track stops after 600 s of an unchanged failed/none
status with no recovery activity), H9 (``dispatch_direction``: a rollback
dispatch is ``operation=rollback`` in any phase or ``operation=update
phase=active``), H10 (``load_card_rules``: the update card and the recovery
screen are rendered from the product build's own ``systemUpdateOutcome.ts``,
``recoveryObservation.ts`` and ``RecoveryAccess.tsx`` rules and catalogues) and
the observer sidecar v2 (``inspect`` points that never overlap the update's
preflight) - and adds two cell kinds: ``owner-continuation`` (the good
candidate cannot bind its port because ``guest_owner_port_hold.py`` holds it,
forward completion pauses, the owner releases the port and runs the printed
retry once) and ``mgmt-off-reboot`` (after a verified good update the owner
disables the Panel and Agent, reboots, and every workload is measured without
management before it is re-enabled).

After the upd4 native run (evidence/upd4-20261001) the harness corrections
H11-H15 are permanent: the recovery screen is rendered from the build's own
``RecoveryAccess.tsx`` JSX (H11, ``parse_screen_source``/``recovery_guidance``),
management-return waits for ``panel_state=ready`` (H12), the update-only verdict
window ends at the instant taken before the owner's stop command (H13) minus one
sample interval (H14), and the raw port-hold events are kept in the evidence
(H15). A start kind whose candidate never ran (upd4 F4/F5) is judged
``not-measured`` and the cell ends ``inconclusive-kind-not-reached``.

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
# upd7: the published baseline tag of the current artifacts document (configure_labels), or None.
LABEL_REF: str | None = None
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
# H19 (upd8 Ubuntu run a): on Ubuntu the cloud image's PackageKit daemon (packagekitd) is started by apt's
# DPkg::Post-Invoke hook after every package operation and exits about 300 s later; the agent counts it as a
# package-manager task (cmd/agent/service_mutation_lock_linux.go linuxPackageProcessBusyAt) and v0.1.0-alpha.80's
# setup step then fails with HOST_MUTATION_BUSY ("... wait and try again"). There the owner does exactly that.
HOST_MUTATION_BUSY_CODE = "HOST_MUTATION_BUSY"
# cmd/agent hostMutationBusyMessage (v0.1.0-alpha.80 and the source): the cause a component operation names.
HOST_MUTATION_BUSY_TEXT = "another server change or package-manager task is still running"
SETUP_OWNER_ATTEMPTS = 12
HOST_IDLE_TIMEOUT_S = 900.0
HOST_IDLE_NODES = ("ubuntu",)
# The agent's own process names (linuxPackageProcessBusyAt) and apt/dpkg fcntl locks, read only (F_GETLK).
HOST_IDLE_PROBE = r"""
import fcntl,json,os,struct
names={"apt","apt-get","dpkg","dpkg-deb","pacman","makepkg","dnf","dnf5","yum","microdnf","rpm","rpmdb","packagekitd",
       "packagekit","pkcon","dnfdaemon-server","dnfdaemon-serve"}
seen=set()
for pid in os.listdir("/proc"):
    if pid.isdigit():
        try:
            comm=open("/proc/"+pid+"/comm").read().strip()
        except OSError:
            continue
        if comm in names:
            seen.add(comm)
locks=[]
for path in ("/var/lib/dpkg/lock-frontend","/var/lib/dpkg/lock","/var/cache/apt/archives/lock"):
    try:
        fd=os.open(path,os.O_RDWR|os.O_NOFOLLOW)
    except OSError:
        continue
    try:
        got=fcntl.fcntl(fd,fcntl.F_GETLK,struct.pack("hhxxxxqqi4x",fcntl.F_WRLCK,0,0,0,0))
        if struct.unpack("hhxxxxqqi4x",got)[0]!=fcntl.F_UNLCK:
            locks.append(path)
    finally:
        os.close(fd)
print(json.dumps({"processes":sorted(seen),"locks":locks}))
"""


def host_package_manager_waits(node: str) -> bool:
    """H19: only the platform where the owner was observed to need the wait (upd8 Ubuntu run a)."""
    return node in HOST_IDLE_NODES


# upd9: what the Agent's rule (cmd/agent/service_mutation_lock_linux.go packageKitDaemonProvablyIdle) reads,
# read here from /proc only (no lock is taken, PackageKit is never contacted): per packagekitd its APT backend in
# maps, its children, and the /proc/locks lines on the four apt/dpkg lock files or owned by it; plus every other
# package-manager process name of the Agent's list. upd10: the backend is read as c855a757 reads it - every mapped
# pathname under a packagekit-backend directory is recorded in full (backend_pathnames) and apt_backend is
# PK_BACKEND_RULE's answer; the efcba145 reading (aptcc_backend, grep_c_aptcc) is kept beside it. "rule" is this
# probe's reading of the corrected rule; the Agent's own answer is the readiness API read next to it.
PK_LOCK_PATHS = ("/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock", "/var/cache/apt/archives/lock",
                 "/var/lib/apt/lists/lock")
# upd10: the backend part of the rule as c855a757 reads it (packageKitMapsShowOnlyAPTBackend, procMapsPathname): the
# pathname column of every maps line whose directory is named exactly packagekit-backend; the daemon can be idle
# only when at least one is mapped and every one is an absolute, clean libpk_backend_apt.so or
# libpk_backend_aptcc.so. Shared by PK_PROBE and the offline tests (same text, executed in both).
PK_BACKEND_RULE = r"""
import posixpath
PK_BACKEND_DIR="packagekit-backend"
PK_APT_MODULES=("libpk_backend_apt.so","libpk_backend_aptcc.so")
def maps_pathname(line):
    parts=line.split(None,5)
    return parts[5].strip() if len(parts)==6 else ""
def backend_pathnames(maps):
    out=[]
    for line in maps.splitlines():
        p=maps_pathname(line)
        if p and posixpath.basename(posixpath.dirname(p))==PK_BACKEND_DIR and p not in out:
            out.append(p)
    return out
def apt_backend_only(paths):
    if not paths:
        return False
    for p in paths:
        if not p.startswith("/") or posixpath.normpath(p)!=p or posixpath.basename(p) not in PK_APT_MODULES:
            return False
    return True
"""
# upd10: package-related processes outside the Agent's list, named so a refusal can be told apart (real activity
# such as unattended-upgrades, needrestart, apt's fetch methods vs only the idle daemon); comm and a short cmdline.
PK_CONTEXT_NAMES = ("unattended-upgr", "needrestart", "apt.systemd.dai", "apt-check", "update-notifier", "aptd",
                    "http", "https", "store", "gpgv", "apt-key", "debconf", "frontend", "dpkg-preconfigu",
                    "dpkg-trigger", "dpkg-divert", "dpkg-statoverri", "update-initramf", "ldconfig")
PK_PROBE = PK_BACKEND_RULE + r"""
import datetime,json,os,subprocess
CONTEXT=set(""" + repr(PK_CONTEXT_NAMES) + r""")
LOCKS=("/var/lib/dpkg/lock-frontend","/var/lib/dpkg/lock","/var/cache/apt/archives/lock","/var/lib/apt/lists/lock")
NAMES={"apt","apt-get","dpkg","dpkg-deb","pacman","makepkg","dnf","dnf5","yum","microdnf","rpm","rpmdb","packagekit",
       "pkcon","dnfdaemon-server","dnfdaemon-serve"}
def rd(p):
    try:
        with open(p,"rb") as f:
            return f.read().decode("utf-8","replace")
    except OSError:
        return None
at=datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
procs={}
for d in os.listdir("/proc"):
    if not d.isdigit():
        continue
    comm=rd("/proc/"+d+"/comm"); st=rd("/proc/"+d+"/status")
    if comm is None or st is None:
        continue
    ppid=next((l.split()[1] for l in st.splitlines() if l.startswith("PPid:")),None)
    procs[int(d)]=(comm.strip(),ppid)
pk=sorted(p for p,(c,_) in procs.items() if c=="packagekitd")
inodes={}
for path in LOCKS:
    try:
        inodes[os.stat(path).st_ino]=path
    except OSError:
        pass
lines=[];unparsed=[]
for line in (rd("/proc/locks") or "").splitlines():
    f=line.split()
    if not f:
        continue
    i=2 if len(f)>1 and f[1]=="->" else 1
    try:
        owner=int(f[i+3]); ident=f[i+4].split(":"); ino=int(ident[-1])
    except (IndexError,ValueError):
        unparsed.append(line.strip()); continue
    if ino in inodes or owner in pk:
        lines.append({"line":line.strip(),"path":inodes.get(ino),"owner":owner,
                      "owner_comm":procs.get(owner,(None,None))[0],"waiting":f[1]=="->"})
uptime=float((rd("/proc/uptime") or "0").split()[0]); tck=os.sysconf("SC_CLK_TCK")
daemons=[]
for pid in pk:
    maps=rd("/proc/%d/maps"%pid); stat=rd("/proc/%d/stat"%pid)
    etime=None
    if stat:
        try:
            etime=round(uptime-int(stat.rsplit(")",1)[1].split()[19])/tck,1)
        except (IndexError,ValueError):
            pass
    kids=[{"pid":p,"comm":c} for p,(c,pp) in sorted(procs.items()) if pp==str(pid)]
    held=[l for l in lines if l["owner"]==pid and l["path"]]
    unowned=[l for l in lines if l["path"] and l["owner"]<=0]
    # upd9 kept for comparison: the efcba145 reading (suffix /libpk_backend_aptcc.so) and grep -c aptcc.
    aptcc=None if maps is None else any(l.strip().endswith("/libpk_backend_aptcc.so") for l in maps.splitlines())
    paths=None if maps is None else backend_pathnames(maps)
    backend=bool(paths) and apt_backend_only(paths)
    daemons.append({"pid":pid,"etime_s":etime,"maps_read":maps is not None,
                    "backend_pathnames":paths,"apt_backend":backend,
                    "grep_c_aptcc":None if maps is None else sum(1 for l in maps.splitlines() if "aptcc" in l),
                    "aptcc_backend":aptcc,"children":kids,"holds_or_waits":held,
                    "rule_idle":backend and not kids and not held and not unowned and not unparsed})
others=[{"pid":p,"comm":c} for p,(c,_) in sorted(procs.items()) if c in NAMES]
context=[]
for p,(c,pp) in sorted(procs.items()):
    if c in CONTEXT or pp in [str(x) for x in pk]:
        cl=rd("/proc/%d/cmdline"%p)
        context.append({"pid":p,"ppid":pp,"comm":c,"cmdline":None if cl is None else cl.replace("\0"," ").strip()[:200]})
try:
    unit=subprocess.run(["systemctl","show","packagekit.service","-p","ActiveState","-p","SubState",
                         "-p","ExecMainStartTimestamp"],capture_output=True,text=True,timeout=10).stdout.strip().splitlines()
except Exception as exc:
    unit=["unavailable: "+type(exc).__name__]
# The Agent's general fcntl probe covers the first three paths for every process (e.g. unattended-upgrades).
general=[l for l in lines if l["path"] in LOCKS[:3]]
rule="busy" if others or general or any(not d["rule_idle"] for d in daemons) else "idle"
print(json.dumps({"at":at,"packagekitd":daemons,"packagekit_unit":unit,"lock_lines":lines,"unparsed_lock_lines":unparsed,
                  "other_package_processes":others,"general_lock_lines":general,"context_processes":context,
                  "rule":rule},sort_keys=True))
"""
# upd9 busystart: the owner's harmless long package task - a download-only install of one package that is not
# installed (nothing is unpacked or configured; no service changes), slowed by apt's own Dl-Limit so it holds the
# apt archives lock for about BUSY_OP_TARGET_S, then one apt-get update (Ubuntu's apt hook then pings PackageKit,
# which keeps or starts the idle daemon). Read-only selection first (dpkg-query, apt-cache: no lock).
def pk_summary(observations: list[dict]) -> dict:
    """upd9: count the PackageKit readings and set each readiness answer against them. "bracketed" means both /proc
    readings around the readiness answer agree (same packagekitd pids, same rule reading)."""
    out: dict[str, Any] = {"observations": len(observations), "with_packagekitd": 0, "rule": {"idle": 0, "busy": 0},
                           "readiness": {}, "grep_c_aptcc": [], "backend_pathnames_seen": [], "apt_backend": [],
                           "children_seen": [], "lock_lines_seen": [], "other_processes_seen": [],
                           "context_processes_seen": [],
                           "pk_alive_rule_idle_bracketed": {"agent_ready": 0, "agent_package_manager_active": 0,
                                                            "agent_other": 0, "panel_answered": 0}}
    for record in observations:
        readings = [r for r in (record.get("before"), record.get("after")) if isinstance(r, dict)]
        for reading in readings:
            out["rule"][reading.get("rule", "busy")] = out["rule"].get(reading.get("rule", "busy"), 0) + 1
            for daemon in reading.get("packagekitd") or []:
                if daemon.get("grep_c_aptcc") not in out["grep_c_aptcc"]:
                    out["grep_c_aptcc"].append(daemon.get("grep_c_aptcc"))
                for name in daemon.get("backend_pathnames") or []:
                    if name not in out["backend_pathnames_seen"]:
                        out["backend_pathnames_seen"].append(name)
                if daemon.get("apt_backend") not in out["apt_backend"]:
                    out["apt_backend"].append(daemon.get("apt_backend"))
                for child in daemon.get("children") or []:
                    if child.get("comm") not in out["children_seen"]:
                        out["children_seen"].append(child.get("comm"))
            for line in reading.get("lock_lines") or []:
                seen = f"{line.get('path')} owner={line.get('owner_comm')}"
                if seen not in out["lock_lines_seen"]:
                    out["lock_lines_seen"].append(seen)
            for proc in reading.get("other_package_processes") or []:
                if proc.get("comm") not in out["other_processes_seen"]:
                    out["other_processes_seen"].append(proc.get("comm"))
            for proc in reading.get("context_processes") or []:
                if proc.get("comm") not in out["context_processes_seen"]:
                    out["context_processes_seen"].append(proc.get("comm"))
        if any(r.get("packagekitd") for r in readings):
            out["with_packagekitd"] += 1
        body = (record.get("readiness") or {}).get("body")
        if not isinstance(body, dict):
            continue
        key = "ready" if body.get("ready") is True else f"{body.get('code')}/{body.get('reason')}"
        out["readiness"][key] = out["readiness"].get(key, 0) + 1
        if (len(readings) == 2 and all(r.get("packagekitd") and r.get("rule") == "idle" for r in readings)
                and [d.get("pid") for d in readings[0]["packagekitd"]] == [d.get("pid") for d in readings[1]["packagekitd"]]):
            bucket = out["pk_alive_rule_idle_bracketed"]
            if record["readiness"].get("answered_by") == "panel":
                bucket["panel_answered"] += 1
            elif body.get("ready") is True:
                bucket["agent_ready"] += 1
            elif body.get("reason") == "package_manager_active":
                bucket["agent_package_manager_active"] += 1
            else:
                bucket["agent_other"] += 1
    return out


BUSY_OP_UNIT = "cp-lab-upd9-owner-apt.service"
BUSY_OP_TARGET_S = 240
BUSY_OP_RUNTIME_MAX_S = 1500
BUSY_OP_CANDIDATES = ("golang-1.22-src", "golang-1.21-src", "golang-1.23-src", "libllvm18", "libllvm17t64",
                      "gcc-14", "libicu74")
BUSY_OP_CHOOSE = r"""
for p in %s; do
  st=$(dpkg-query -W -f='${Status}' "$p" 2>/dev/null || true)
  case "$st" in *"install ok installed"*) continue;; esac
  size=$(apt-cache show --no-all-versions "$p" 2>/dev/null | awk '/^Size:/{print $2; exit}')
  if [ -n "$size" ] && [ "$size" -ge 5000000 ]; then echo "$p $size"; exit 0; fi
done
echo "none 0"
"""


def busy_op_command(package: str, limit_kib: int) -> str:
    if not re.fullmatch(r"[a-z0-9][a-z0-9.+-]{1,60}", package) or not 4 <= limit_kib <= 100000:
        raise ValueError("invalid busy operation choice")
    stamp = "$(date -u +%FT%T.%3NZ)"
    return ("echo \"op-start " + stamp + "\"; "
            f"DEBIAN_FRONTEND=noninteractive apt-get install --download-only -y --no-install-recommends "
            f"-o Acquire::http::Dl-Limit={limit_kib} -o Acquire::https::Dl-Limit={limit_kib} {package}; "
            "echo \"download-only rc=$? " + stamp + "\"; apt-get update; echo \"update rc=$? " + stamp + "\"")


ORIGIN_NAME = "celikpanel.net"
GUEST_HELPERS = ("guest_probe.py", "guest_port_fault.py", "guest_update_kill.py", "guest_bound_worker.py",
                 "guest_recovery_fault.py", "guest_recovery_handoff.py", "guest_owner_update_observer.py",
                 "guest_upd1_workload.py", "guest_owner_port_hold.py")
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
# upd3: two candidate-panel start defects (product change 8ffc5e06; component tests only there).
KIND_PROVENANCE = {
    "start-check": "fixture source patch: cmd/panel configurePanelHTTPTLS (shared by the read-only "
                   "--check-startup-readiness and the real start) always fails; --migrate-only and "
                   "--inspect-build-identity are untouched, so the update reaches the start check after the "
                   "database publication (simulated by a committed fixture change)",
    "real-start": "fixture source patch: cmd/panel main() exits (log.Fatalf) just before its listener starts; the "
                  "read-only start check exits before flag parsing and never reaches that line, so the check "
                  "passes and the real start fails after completion.pending (simulated by a committed fixture change)",
    # upd4: no candidate defect; the good candidate G is installed unchanged.
    "owner-continuation": "no candidate defect (good candidate G); owner-fixable host cause: guest_owner_port_hold.py "
                          "holds 127.0.0.1:2083 from the moment the updater stops the old Panel, so the new Panel cannot "
                          "bind, its stability wait fails (panel_start_unverified) and forward completion pauses; the "
                          "harness then does what the product text tells the owner (release the port, run the printed "
                          "one-time retry once) (simulated host condition, never a product change)",
    "mgmt-off-reboot": "no candidate defect (good candidate G); after the verified update the owner disables and stops "
                       "the Panel and Agent units, reboots once (orderly) and measures every workload without management "
                       "(owner action on a disposable guest, never a product change)",
}
# Cell variant -> artifact role in upd1-artifacts.json.
VARIANT_ROLES = {"good": "good", "defective": "defective", "start-check": "startcheck", "real-start": "realstart",
                 "owner-continuation": "good", "mgmt-off-reboot": "good"}
OWNER_VARIANTS = ("owner-continuation", "mgmt-off-reboot")
FORWARD_VARIANTS = ("good",) + OWNER_VARIANTS
BASE_ROLES = ("baseline", "good", "defective")
EXTRA_ROLES = ("startcheck", "realstart")
ROLLBACK_VARIANTS = ("defective", "start-check")
START_CHECK_CODE = "candidate_panel_startup_check_failed"
REAL_START_CODE = "panel_start_unverified"
FAILURE_SIDECAR_SCHEMA = "celikpanel-recovery-failure/v1"
# upd5: the two typed stops before any change (internal/recoveryobs ValidFailureCode). The product writes the record's
# code only for state=unchanged (update.sh report_update_failure); the request is then final, nothing follows.
PREFLIGHT_REFUSED_CODE = "update_preflight_refused"
RUNTIME_PREFLIGHT_CODE = "recovery_runtime_preflight_failed"
PREFLIGHT_STOP_CODES = (PREFLIGHT_REFUSED_CODE, RUNTIME_PREFLIGHT_CODE)
FAILURE_CODES = (START_CHECK_CODE, REAL_START_CODE) + PREFLIGHT_STOP_CODES
# Phases in which cmd/recovery failureCodeGuidance prints each code's pending text (the preflight stops: failed only).
CLI_PENDING_PHASES = {START_CHECK_CODE: ("failed", "recovering"), REAL_START_CODE: ("failed", "recovering"),
                      PREFLIGHT_REFUSED_CODE: ("failed",), RUNTIME_PREFLIGHT_CODE: ("failed",)}
# upd5: automatic recovery values that are still recovery in progress: neither the pause nor an owner action.
# pause_pending: the last admitted attempt failed and the next timer run records the pause (keeps first_failure_code).
AUTOMATIC_IN_PROGRESS = ("retry_scheduled", "pause_pending")
# upd5: optional <request>.renewal sidecar (internal/recoveryobs DecodeRenewal), shown only at the pause.
RENEWAL_SIDECAR_SCHEMA = "celikpanel-recovery-renewal/v1"
# update.sh publish_update_renewal_observation: "on" when one of these timers was enabled or active before the pause.
RENEWAL_UNITS = ("certbot.timer", "certbot-renew.timer")
RENEWAL_ON_UNIT_FILE = ("enabled", "enabled-runtime")
RENEWAL_ON_ACTIVE = ("active", "activating", "reloading", "refreshing")
# The start-check fixture fails inside configurePanelHTTPTLS; the product maps that to this reason code
# (cmd/panel/startup_readiness.go). Any other reason means the check refused for a cause that is not the fixture's.
START_CHECK_FIXTURE_REASON = "tls_pair_invalid"
# O5 (upd2): at the start instant the Panel API may still say accepted while the root CLI says running.
START_LAG_PAIR = ("accepted", "running")
# upd12: the Panel's bounded retry of startup mail work refused as busy (product 6b6f8a0c,
# cmd/panel/startup_deferred_mail.go). Read-only: the Panel journal and native mail file facts only.
DEFERRED_MAIL_NOTE = ("the Panel retries this by itself every 30 seconds for up to 10 minutes once no other server "
                      "change is running; nothing needs to be done now")
DEFERRED_MAIL_TASKS = {"certificate startup reconcile: certificate dependents:": "mail certificate publication",
                       "milter wiring at startup:": "mail filter wiring"}
DEFERRED_MAIL_ATTEMPT = re.compile(r"startup mail work, attempt (\d+) of (\d+): (.*)$")
DEFERRED_MAIL_GIVE_UP = "still not done after"
# 20 attempts x 30 s, plus each attempt's probe (<= 15 s) and step timeouts; the watch is bounded independently.
DEFERRED_MAIL_WATCH_S = 900.0
DEFERRED_MAIL_POLL_S = 10.0
# After the loop resolved, one more read past more than one retry interval shows that nothing ran again.
DEFERRED_MAIL_SETTLE_S = 45.0
DEFERRED_MAIL_FILES = ("/etc/postfix/main.cf", "/etc/postfix/master.cf", "/etc/postfix/celikpanel_sni",
                       "/etc/postfix/celikpanel_sni.db", "/etc/postfix/celikpanel_sni.lmdb", "/etc/postfix/virtual",
                       "/etc/postfix/virtual.db", "/etc/postfix/vmailbox", "/etc/postfix/vmailbox.db",
                       "/etc/postfix/vmailbox_domains", "/etc/postfix/vmailbox_domains.db", "/etc/aliases.db",
                       "/etc/dovecot/conf.d/98-celikpanel-tls.conf")
PANEL_JOURNAL_LINE = re.compile(r"^(\S+) \S+ panel\[(\d+)\]: (?:\d{4}/\d\d/\d\d \d\d:\d\d:\d\d )?(.*)$")


# upd13 H22: failed baseline status reads tolerated while the installer runs (at the 15 s poll: about two minutes).
BASELINE_STATUS_READ_FAILURES_MAX = 8


def baseline_status_read_failure(exc: subprocess.CalledProcessError) -> dict:
    """upd13 H22: one failed read of the baseline installer's status, as recorded (exit code and the guest's last
    stderr line; the command line holds no secret but is not kept)."""
    stderr = exc.stderr if isinstance(exc.stderr, str) else (exc.stderr or b"").decode("utf-8", "replace")
    lines = [line for line in stderr.strip().splitlines() if line.strip()]
    return {"at": utc_now(), "returncode": exc.returncode, "stderr_tail": lines[-3:]}


def provenance_for(variant: str) -> dict:
    """The good and migrate-only cells keep their provenance unchanged; the start kinds name their own defect."""
    if LABEL_REF is None and variant not in KIND_PROVENANCE:
        return PROVENANCE
    base = dict(PROVENANCE)
    if LABEL_REF is not None:
        # upd7: the baseline is the published tag's tree with only the acceptance-license seam added.
        base["baseline"] = (f"published-tag-{LABEL_REF}-tree-plus-the-D-027-acceptance-license-seam-only (Panel license "
                            f"code: {', '.join(BASELINE_REF_PATCHED)}); its release policy, installer, update/rollback/"
                            "recovery/bootstrap scripts, get.sh and Agent are byte-identical to the tag; installed by "
                            "the real installer; fixture trust root enrolled; not-production-release-admission")
        base["candidate"] = (f"unpublished-local-fixture-commit-over-the-source-labelled-{CANDIDATE_VERSION}; signed "
                             "with the disposable fixture key; served by the guest-loopback celikpanel.net fixture "
                             "origin; not a release")
    if variant in KIND_PROVENANCE:
        return dict({k: v for k, v in base.items() if k != "defect"}, defect=KIND_PROVENANCE[variant])
    return base


@dataclasses.dataclass(frozen=True)
class Cell:
    name: str
    node: str
    variant: str               # "defective" | "good" | "start-check" | "real-start"
    recovery_fault: dict | None
    mail_required: bool
    # upd9: h19=False is an ordinary owner who starts the reviewed setup plan once (no H19 wait or retry);
    # pk_observe records the PackageKit facts the Agent's rule reads (/proc only) and the Agent's read-only
    # readiness answer; scenario None | "setup-once" (the cell ends after setup) | "busy-start" (an update start
    # during a real package task, then the update while packagekitd idles).
    h19: bool = True
    pk_observe: bool = False
    scenario: str | None = None


SCENARIOS = (None, "setup-once", "busy-start")

CELLS = {
    "upd1-debian13-defective": Cell("upd1-debian13-defective", "debian13", "defective",
                                    {"action": "reboot", "checkpoint": "payload_restored"}, True),
    "upd1-debian13-good": Cell("upd1-debian13-good", "debian13", "good", None, True),
    "upd1-arch-defective": Cell("upd1-arch-defective", "arch", "defective",
                                {"action": "kill", "checkpoint": "runtime_verified"}, False),
    "upd1-arch-good": Cell("upd1-arch-good", "arch", "good", None, False),
    # upd3: the start check fails after the database publication -> automatic rollback, with the same
    # second fault as the migrate-only cells.
    "upd1-debian13-startcheck": Cell("upd1-debian13-startcheck", "debian13", "start-check",
                                     {"action": "reboot", "checkpoint": "payload_restored"}, True),
    "upd1-arch-startcheck": Cell("upd1-arch-startcheck", "arch", "start-check",
                                 {"action": "kill", "checkpoint": "runtime_verified"}, False),
    # upd3: the check passes, the real start fails after completion.pending -> forward completion up to its
    # limit, then the pause. No second fault; the owner retry is not run (it would retry the same candidate).
    "upd1-debian13-realstart": Cell("upd1-debian13-realstart", "debian13", "real-start", None, True),
    "upd1-arch-realstart": Cell("upd1-arch-realstart", "arch", "real-start", None, False),
    # upd4: the owner path never exercised natively - forward completion exhausts its budget on a transient,
    # owner-fixable cause (the Panel port held), and the owner's one-time retry completes the same operation.
    "upd1-debian13-owner-continuation": Cell("upd1-debian13-owner-continuation", "debian13", "owner-continuation",
                                             None, True),
    "upd1-arch-owner-continuation": Cell("upd1-arch-owner-continuation", "arch", "owner-continuation", None, False),
    # upd4: after a verified good update, management disabled + one orderly reboot; workloads measured without it.
    "upd1-debian13-mgmt-off-reboot": Cell("upd1-debian13-mgmt-off-reboot", "debian13", "mgmt-off-reboot", None, True),
    "upd1-arch-mgmt-off-reboot": Cell("upd1-arch-mgmt-off-reboot", "arch", "mgmt-off-reboot", None, False),
    # upd8: the same three cells on a one-node Ubuntu 24.04 lab (lab.py --platform ubuntu); mail is required
    # as on Debian (same apt package family), the second fault is Debian's reset at payload_restored.
    # upd11: every Ubuntu cell keeps the PackageKit probe on (read-only /proc facts and the Agent's readiness
    # answer at setup, arm and the owner's start; PackageKit's journal at collect); the owner model (H19) is
    # unchanged. The start-check and management-off kinds mirror Debian's definitions.
    "upd1-ubuntu-good": Cell("upd1-ubuntu-good", "ubuntu", "good", None, True, pk_observe=True),
    "upd1-ubuntu-owner-continuation": Cell("upd1-ubuntu-owner-continuation", "ubuntu", "owner-continuation", None, True,
                                           pk_observe=True),
    "upd1-ubuntu-defective": Cell("upd1-ubuntu-defective", "ubuntu", "defective",
                                  {"action": "reboot", "checkpoint": "payload_restored"}, True, pk_observe=True),
    "upd1-ubuntu-startcheck": Cell("upd1-ubuntu-startcheck", "ubuntu", "start-check",
                                   {"action": "reboot", "checkpoint": "payload_restored"}, True, pk_observe=True),
    "upd1-ubuntu-mgmt-off-reboot": Cell("upd1-ubuntu-mgmt-off-reboot", "ubuntu", "mgmt-off-reboot", None, True,
                                        pk_observe=True),
    # upd9 (efcba145: an idle packagekitd is not package activity). setuponce: the candidate installed fresh, the
    # web_mail setup started once by an ordinary owner, PackageKit observed; the cell ends after setup.
    "upd1-ubuntu-setuponce": Cell("upd1-ubuntu-setuponce", "ubuntu", "good", None, True,
                                  h19=False, pk_observe=True, scenario="setup-once"),
    # busystart: after setup, the owner's own long package task; the update start is tried while it runs (refusal
    # expected), then the good update is started while packagekitd still idles after it (admission expected).
    "upd1-ubuntu-busystart": Cell("upd1-ubuntu-busystart", "ubuntu", "good", None, True,
                                  pk_observe=True, scenario="busy-start"),
}


def candidate_role(cell: Cell) -> str:
    return VARIANT_ROLES[cell.variant]


def cell_roles(cell: Cell) -> tuple:
    """Artifact roles a cell records: the three base roles, plus its own candidate for the start kinds."""
    role = candidate_role(cell)
    return BASE_ROLES if role in BASE_ROLES else BASE_ROLES + (role,)


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


# upd3 start-check: the one function the read-only start check and the real start share for TLS
# (cmd/panel/server_lifecycle.go, since 8ffc5e06). --migrate-only and --inspect-build-identity never call it.
START_CHECK_FILE = "cmd/panel/server_lifecycle.go"
START_CHECK_ORIGINAL = (
    "func configurePanelHTTPTLS(server *http.Server, certPath, keyPath string) (bool, error) {\n"
    "\tif server == nil {\n"
    "\t\treturn false, errors.New(\"panel HTTP server is nil\")\n"
    "\t}\n")
START_CHECK_REPLACEMENT = START_CHECK_ORIGINAL + (
    "\t// upd3 disposable fixture defect (start-check): never a release. The TLS\n"
    "\t// preparation shared by the read-only start check and the real start fails,\n"
    "\t// so the update stops at the start check, before completion.pending.\n"
    "\tif server != nil {\n"
    "\t\treturn false, errors.New(\"upd3 fixture defect: this candidate cannot prepare its panel TLS listener\")\n"
    "\t}\n")
# upd3 real-start: main() only, after every early-exit mode; the start check exits before flag parsing
# (runStartupReadinessEntry) and never executes this line.
REAL_START_FILE = "cmd/panel/main.go"
REAL_START_ORIGINAL = "\trunningServer, err := startPanelHTTP(server, certPath, keyPath)\n"
REAL_START_REPLACEMENT = (
    "\t// upd3 disposable fixture defect (real-start): never a release. The read-only\n"
    "\t// start check exits before flag parsing and never reaches this line; the real\n"
    "\t// start exits here, before its listener, so the unit never stays up.\n"
    "\tlog.Fatalf(\"upd3 fixture defect: this candidate exits before its panel listener starts\")\n"
    + REAL_START_ORIGINAL)
KIND_PATCHES = {"start-check": (START_CHECK_FILE, START_CHECK_ORIGINAL, START_CHECK_REPLACEMENT),
                "real-start": (REAL_START_FILE, REAL_START_ORIGINAL, REAL_START_REPLACEMENT)}


def apply_kind_patch(kind: str, text: str) -> str:
    """Apply one upd3 fixture patch exactly once; refuse when the product source differs."""
    path, original, replacement = KIND_PATCHES[kind]
    if text.count(original) != 1 or "upd3 fixture defect" in text:
        raise ValueError(f"{path} differs from the reviewed source; the {kind} fixture defect cannot be applied exactly")
    return text.replace(original, replacement)


# ---------------------------------------------------------------------------
# upd7: a genuine published baseline (build-upd1-artifacts.sh --baseline-ref)
# ---------------------------------------------------------------------------
# Every earlier run (upd1-upd6) installed a build of the current source labelled as the
# baseline. With --baseline-ref the baseline is the PUBLISHED tag's own tree. The only change
# to that tree is the D-027 acceptance-license seam (Panel license code only), because the
# tag predates it and its Panel refuses every owner API with license_required otherwise
# (README "Why not the 45dfc265 Alpha81 build"). Its release policy, installer, update,
# rollback, recovery, bootstrap and get.sh files and the whole Agent stay byte-identical to the
# tag (build-upd1-artifacts.sh proves this). The candidates are the source labelled as the next
# release after the tag.
DEFAULT_LABELS = {"baseline": (BASELINE_VERSION, BASELINE_SEQUENCE), "candidate": (CANDIDATE_VERSION, CANDIDATE_SEQUENCE)}
BASELINE_REFS = {
    "v0.1.0-alpha.80": {"commit": ALPHA80_COMMIT, "baseline": ("v0.1.0-alpha.80", 80),
                        "candidate": ("v0.1.0-alpha.81", 81),
                        "baseline_policy": {"version": "v0.1.0-alpha.80", "current": 80, "previous": 79,
                                            "previous_version": "v0.1.0-alpha.79"}},
}
SEAM_COPIED = ("internal/licensing/acceptance_off.go", "internal/licensing/acceptance_owner_linux.go",
               "internal/licensing/acceptance_owner_other.go")
SEAM_ADAPTED = "internal/licensing/acceptance_fixture.go"
SEAM_LICENSE = "internal/licensing/license.go"
SEAM_PANEL = "cmd/panel/license.go"
BASELINE_REF_PATCHED = tuple(sorted(SEAM_COPIED + (SEAM_ADAPTED, SEAM_LICENSE, SEAM_PANEL)))
# Paths that must stay byte-identical to the tag in the baseline fixture commit (prefixes end with "/").
BASELINE_REF_UNCHANGED = ("install.sh", "update.sh", "rollback.sh", "rebuild.sh", "bootstrap-update.sh",
                          "bootstrap-prebuilt-update.sh", "download-portal/", "deploy/", "cmd/agent/", "web/",
                          "Makefile", "go.mod", "go.sum")
# The seam commit 01a450e6's hooks, re-applied to the tag's license.go (each anchor occurs once there).
SEAM_LICENSE_HOOKS = (
    ("\tCanProvision bool   `json:\"can_provision\"`\n}\n",
     "\tCanProvision bool   `json:\"can_provision\"`\n"
     "\t// Empty in every ordinary build (acceptance_off.go), so the JSON is unchanged.\n"
     "\t// Only the acceptance_license test build labels its fixture license here.\n"
     "\tacceptanceStatus\n}\n"),
    ("\trejected   atomic.Bool\n}\n",
     "\trejected   atomic.Bool\n"
     "\t// seam is assigned only by NewServer in the acceptance_license test build\n"
     "\t// (acceptance_fixture.go). Ordinary builds have no implementation and no\n"
     "\t// assignment, so it is always nil there.\n"
     "\tseam acceptanceSeam\n}\n\n"
     "// acceptanceSeam replaces license verification only in the acceptance_license\n"
     "// test build. It is an interface with no implementation in ordinary builds.\n"
     "type acceptanceSeam interface {\n\tstatus() Status\n\tactivate(ctx context.Context, key, hostname string) error\n"
     "\trefresh(ctx context.Context, force bool) error\n}\n"),
    ("func (m *Manager) Status() Status {\n",
     "func (m *Manager) Status() Status {\n\tif m.seam != nil {\n\t\treturn m.seam.status()\n\t}\n"),
    ("func (m *Manager) request(ctx context.Context, action string, input map[string]string) error {\n",
     "func (m *Manager) request(ctx context.Context, action string, input map[string]string) error {\n"
     "\tif m.seam != nil {\n\t\treturn errors.New(\"license service is never contacted by this build\")\n\t}\n"),
    ("func (m *Manager) Activate(ctx context.Context, key, hostname string) error {\n",
     "func (m *Manager) Activate(ctx context.Context, key, hostname string) error {\n"
     "\tif m.seam != nil {\n\t\treturn m.seam.activate(ctx, key, hostname)\n\t}\n"),
    ("func (m *Manager) Refresh(ctx context.Context, force bool) error {\n",
     "func (m *Manager) Refresh(ctx context.Context, force bool) error {\n"
     "\tif m.seam != nil {\n\t\treturn m.seam.refresh(ctx, force)\n\t}\n"),
)
SEAM_PANEL_HOOK = ("\treturn licensing.New(file, key, id)\n",
                   "\t// NewServer is exactly licensing.New in every ordinary build. Only the\n"
                   "\t// acceptance_license test build, which release packaging refuses, installs\n"
                   "\t// the acceptance fixture seam there.\n"
                   "\treturn licensing.NewServer(file, key, id)\n")
# The tag's licensing package has no Status.Observation field and no errInvalidState sentinel
# (both arrived later, 0aae716f). The tag-only fixture file drops the field and defines the sentinel.
SEAM_ADAPT = (
    ("s.State, s.Observation, s.CanProvision = \"verification_unavailable\", ObservationUnavailable, false",
     "s.State, s.CanProvision = \"verification_unavailable\", false", 1),
    (", Observation: ObservationKnown", "", 4),
    (", Observation: ObservationUnavailable", "", 1),
)
SEAM_ADAPT_TRAILER = ("\n// upd7 fixture adaptation for the v0.1.0-alpha.80 tree: that tree's licensing package has no\n"
                      "// errInvalidState sentinel; this tag-only file defines it for its own receipt checks.\n"
                      "var errInvalidState = errors.New(\"invalid license state\")\n")


def baseline_ref_profile(ref: str) -> dict:
    if ref not in BASELINE_REFS:
        raise ValueError(f"unsupported baseline ref {ref!r}; choose one of {sorted(BASELINE_REFS)}")
    return BASELINE_REFS[ref]


def replace_once(text: str, original: str, replacement: str, path: str, count: int = 1) -> str:
    if text.count(original) != count:
        raise ValueError(f"{path} differs from the reviewed source; the fixture change cannot be applied exactly")
    return text.replace(original, replacement)


def adapt_seam_fixture(text: str) -> str:
    """HEAD's acceptance_fixture.go, adapted to the tag's licensing package (exact edits or refusal)."""
    for original, replacement, count in SEAM_ADAPT:
        text = replace_once(text, original, replacement, SEAM_ADAPTED, count)
    if "Observation" in text or "errInvalidState = " in text or "upd7 fixture adaptation" in text:
        raise ValueError(f"{SEAM_ADAPTED} still names a field the tag does not have")
    return text + SEAM_ADAPT_TRAILER


def seam_baseline_source(repo: Path, source_commit: str, ref: str) -> dict:
    """Apply the acceptance-license seam to a checkout of the published tag (Panel license code only)."""
    profile = baseline_ref_profile(ref)
    head = subprocess.run(["git", "-C", str(repo), "rev-parse", "HEAD"], capture_output=True, text=True,
                          check=True).stdout.strip()
    if head != profile["commit"]:
        raise ValueError(f"the clone is not at {ref} ({profile['commit']}); refusing to patch {head}")
    if not HEX40.fullmatch(source_commit or ""):
        raise ValueError("the seam is copied from the exact source commit")

    def source(path: str) -> str:
        return subprocess.run(["git", "-C", str(repo), "show", f"{source_commit}:{path}"], capture_output=True,
                              check=True).stdout.decode("utf-8")
    for path in SEAM_COPIED + (SEAM_ADAPTED,):
        if (repo / path).exists():
            raise ValueError(f"{path} already exists in {ref}; the seam is not applied twice")
    for path in SEAM_COPIED:
        (repo / path).write_text(source(path), encoding="utf-8")
    (repo / SEAM_ADAPTED).write_text(adapt_seam_fixture(source(SEAM_ADAPTED)), encoding="utf-8")
    license_go = (repo / SEAM_LICENSE).read_text(encoding="utf-8")
    for original, replacement in SEAM_LICENSE_HOOKS:
        license_go = replace_once(license_go, original, replacement, SEAM_LICENSE)
    (repo / SEAM_LICENSE).write_text(license_go, encoding="utf-8")
    panel = (repo / SEAM_PANEL).read_text(encoding="utf-8")
    (repo / SEAM_PANEL).write_text(replace_once(panel, *SEAM_PANEL_HOOK, SEAM_PANEL), encoding="utf-8")
    return {"kind": "baseline-ref", "ref": ref, "tag_commit": profile["commit"], "seam_source": source_commit,
            "changed": list(BASELINE_REF_PATCHED)}


def labels_of(document: dict | None) -> dict:
    """Release labels of an artifacts document: the upd1 defaults, or the published baseline ref's profile."""
    ref = (document or {}).get("baseline_ref")
    if ref is None:
        return {"ref": None, **DEFAULT_LABELS, "baseline_policy": {"version": DEFAULT_LABELS["baseline"][0],
                "current": DEFAULT_LABELS["baseline"][1], "previous": 80, "previous_version": "v0.1.0-alpha.80"}}
    if not isinstance(ref, dict):
        raise ValueError("baseline_ref must be an object")
    profile = baseline_ref_profile(ref.get("ref"))
    if ref.get("tag_commit") != profile["commit"]:
        raise ValueError("baseline_ref names a different tag commit")
    return {"ref": ref["ref"], "baseline": profile["baseline"], "candidate": profile["candidate"],
            "baseline_policy": dict(profile["baseline_policy"])}


def configure_labels(document: dict | None) -> dict:
    """Set this module's and the helper modules' release labels for one artifacts document (idempotent)."""
    global BASELINE_VERSION, BASELINE_SEQUENCE, CANDIDATE_VERSION, CANDIDATE_SEQUENCE, LABEL_REF
    labels = labels_of(document)
    (BASELINE_VERSION, BASELINE_SEQUENCE), (CANDIDATE_VERSION, CANDIDATE_SEQUENCE) = labels["baseline"], labels["candidate"]
    LABEL_REF = labels["ref"]
    if labels["ref"] is None and "upd1_current_worker_baseline" not in _MODULES:
        return labels   # the helper modules load later with exactly these default labels
    m = lab_modules()
    m["baseline"].VERSION, m["baseline"].SEQUENCE = labels["baseline"]
    m["baseline"].RELEASE_POLICY = dict(labels["baseline_policy"])
    m["origin"].RELEASE_POLICY = {"version": CANDIDATE_VERSION, "current": CANDIDATE_SEQUENCE,
                                  "previous": BASELINE_SEQUENCE, "previous_version": BASELINE_VERSION}
    if not m["origin"].allowed_policy(m["origin"].RELEASE_POLICY):
        raise ValueError("the candidate transition is not one the fixture origin serves")
    return labels


def fixture_source(repo: Path, kind: str, previous_commit: str | None, *, baseline_ref: str | None = None,
                   source_commit: str | None = None) -> dict:
    """Edit one disposable clone; the build script commits the result."""
    repo = Path(repo)
    if not (repo / ".git").exists() or "cp-upd1-build" not in str(repo):
        raise ValueError("fixture edits are allowed only in a disposable /var/tmp/cp-upd1-build clone")
    policy = repo / "deploy" / "release-sequence-policy"
    if kind == "baseline-ref":
        return seam_baseline_source(repo, source_commit or "", baseline_ref or "")
    if kind == "baseline":
        policy.write_text(policy_text(BASELINE_VERSION, BASELINE_SEQUENCE, 80, "v0.1.0-alpha.80", ALPHA80_COMMIT))
        return {"kind": kind, "changed": [str(policy.relative_to(repo))]}
    if kind == "good":
        if not previous_commit or not HEX40.fullmatch(previous_commit):
            raise ValueError("the candidate policy names the exact baseline commit")
        profile = baseline_ref_profile(baseline_ref) if baseline_ref else DEFAULT_LABELS
        (base_version, base_sequence), (version, sequence) = profile["baseline"], profile["candidate"]
        policy.write_text(policy_text(version, sequence, base_sequence, base_version, previous_commit))
        return {"kind": kind, "changed": [str(policy.relative_to(repo))]}
    if kind == "defective":
        main = repo / "cmd" / "panel" / "main.go"
        main.write_text(apply_defect(main.read_text()))
        return {"kind": kind, "changed": [str(main.relative_to(repo))]}
    if kind in KIND_PATCHES:
        # Applied over the good candidate (same v0.1.0-alpha.82 policy), never over the migrate-only defect.
        target = repo / KIND_PATCHES[kind][0]
        target.write_text(apply_kind_patch(kind, target.read_text(encoding="utf-8")), encoding="utf-8")
        return {"kind": kind, "changed": [KIND_PATCHES[kind][0]]}
    raise ValueError("unknown fixture kind")


# ---------------------------------------------------------------------------
# Pure rules (covered offline by test_owner_update_trial.py)
# ---------------------------------------------------------------------------

def validate_cell(name: str) -> Cell:
    if name not in CELLS:
        raise ValueError(f"unknown cell {name!r}; choose one of {sorted(CELLS)}")
    return CELLS[name]


def validate_cell_artifacts(document: dict, cell: Cell, *, check_files: bool = True) -> dict:
    return validate_artifacts(document, check_files=check_files, require=(candidate_role(cell),))


def validate_artifacts(document: dict, *, check_files: bool = True, require: Iterable[str] = ()) -> dict:
    """The three base roles always; the upd3 start kinds when present or required.

    ``startcheck`` and ``realstart`` are each one fixture commit over the good
    candidate (same v0.1.0-alpha.82 policy). A document built before upd3 has
    only the base roles and still serves the good and migrate-only cells.
    """
    if not isinstance(document, dict) or document.get("schema") != ARTIFACTS_SCHEMA:
        raise ValueError("artifacts document schema differs")
    for role in require:
        if role in EXTRA_ROLES and role not in document:
            raise ValueError(f"artifact {role} is missing; rebuild with build-upd1-artifacts.sh (upd3 adds the "
                             "start-check and real-start candidates)")
    roles = {"baseline": (BASELINE_VERSION, BASELINE_SEQUENCE), "good": (CANDIDATE_VERSION, CANDIDATE_SEQUENCE),
             "defective": (CANDIDATE_VERSION, CANDIDATE_SEQUENCE)}
    roles.update({role: (CANDIDATE_VERSION, CANDIDATE_SEQUENCE) for role in EXTRA_ROLES if role in document})
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
    if len(commits) != len(roles):
        raise ValueError("baseline, good and defective candidates must be three distinct commits"
                         if len(roles) == 3 else "every upd1/upd3 artifact must be a distinct commit")
    if document.get("good", {}).get("parent") != document["baseline"]["commit"] \
            or document.get("defective", {}).get("parent") != document["good"]["commit"]:
        raise ValueError("fixture lineage must be baseline <- good <- defective")
    for role in EXTRA_ROLES:
        if role in roles and document[role].get("parent") != document["good"]["commit"]:
            raise ValueError(f"fixture lineage must be good <- {role} (one fixture commit over the good candidate)")
    if not HEX40.fullmatch(str(document.get("source_head", ""))):
        raise ValueError("source HEAD is not exact")
    if document.get("baseline_ref") is not None:
        # upd7: the baseline is one fixture commit over the published tag, patching only the seam files.
        ref = document["baseline_ref"]
        profile = baseline_ref_profile(ref.get("ref"))
        if (ref.get("tag_commit") != profile["commit"] or document["baseline"].get("parent") != profile["commit"]
                or ref.get("patched_files") != list(BASELINE_REF_PATCHED)
                or (BASELINE_VERSION, BASELINE_SEQUENCE) != profile["baseline"]):
            raise ValueError("the published-baseline fixture is not the seam-only commit over the tag "
                             "(or configure_labels was not applied)")
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


def start_accepted(http_status: int, body: Any) -> bool:
    """H6 (upd2): the product answers 202 Accepted for a queued/running start and 200 for a replay of an
    existing one (cmd/panel/system_update_handlers.go); the web UI checks response.ok. Only 200/202 with
    ``accepted: true`` is an accepted start; anything else stays a refusal."""
    return http_status in (200, 202) and isinstance(body, dict) and body.get("accepted") is True


def status_agreement(request_id: str, api: dict | None, cli: dict | None, shell: dict | None, *,
                     start_instant: bool = False) -> dict:
    """Do the three owner views name the same operation and the same phase?

    ``api``: /api/v1/recovery/status JSON (None while the Panel is unreachable).
    ``cli``: root CLI ``status --json`` JSON (None when SSH was unavailable).
    ``shell``: {"reference": id held by the browser marker, "status_command":
    command the offline page shows}. The offline shell carries no phase by
    design; it must name the same operation and the exact read-only command.

    O5 (upd2): ``start_instant`` marks a sample within the first poll interval
    after the owner's start. There, and only there, the Panel one step behind
    the CLI (api ``accepted``, cli ``running``, every other field equal) is the
    natural ordering and is recorded as ``start-instant-lag``, not a disagreement.
    """
    reasons: list[str] = []
    lag = None
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
        differing = [field for field in ("phase", "terminal_proof", "automatic_recovery", "previous_failure")
                     if (api or {}).get(field) != (cli or {}).get(field)]
        if (start_instant and differing == ["phase"]
                and (api.get("phase"), cli.get("phase")) == START_LAG_PAIR):
            lag = "start-instant lag: the Panel API still said accepted while the root CLI already said running"
        else:
            for field in differing:
                reasons.append(f"{field} differs: api={api.get(field)!r} cli={cli.get(field)!r}")
    if reasons:
        verdict = "disagree"
    elif lag:
        verdict = "start-instant-lag"
    elif len(known) == 2:
        verdict = "agree"
    elif len(known) == 1:
        verdict = "single-source"
    else:
        verdict = "no-known-source"
    result = {"verdict": verdict, "reasons": reasons, "sources": sorted(named),
              "known_sources": sorted(known), "phase": {k: v.get("phase") for k, v in known.items()}}
    if lag:
        result["lag"] = lag
    return result


# H21 (upd11): the track stopped on the first terminal root-CLI read while the Panel API, read a few hundred ms
# earlier in the same sample, still showed the previous phase (upd11 ubuntu-startcheck run-a sample 28: api
# recovering, cli recovered). One confirming sample is taken after this delay instead of judging that race.
H21_CONFIRM_DELAY_S = 2.0


def final_needs_confirmation(sample: dict) -> bool:
    """The sample that ends the track disagrees between the known views: take one confirming sample (H21)."""
    return (sample.get("agreement") or {}).get("verdict") == "disagree"


def agreement_verdict(samples: list[dict]) -> dict:
    """Failed only when a disagreement persists over two consecutive samples or at the end.

    ``start-instant-lag`` samples (O5) are neither agreement nor disagreement; they are counted.
    """
    verdicts = [sample.get("verdict") for sample in samples]
    persistent = any(a == b == "disagree" for a, b in zip(verdicts, verdicts[1:]))
    final = verdicts[-1] if verdicts else None
    agreed = sum(1 for value in verdicts if value == "agree")
    lagged = sum(1 for value in verdicts if value == "start-instant-lag")
    if persistent or final == "disagree":
        return {"verdict": "failed", "agreed_samples": agreed, "samples": len(verdicts), "lag_samples": lagged}
    if agreed == 0:
        return {"verdict": "inconclusive", "agreed_samples": 0, "samples": len(verdicts), "lag_samples": lagged}
    return {"verdict": "passed", "agreed_samples": agreed, "samples": len(verdicts), "lag_samples": lagged}


RECOVERY_LOG_COMMAND = "sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50"
# H11 (upd4): the component and region of RecoveryAccess.tsx the screen model renders (read from the build's source).
SCREEN_COMPONENT = "RecoveryStatus"
SCREEN_REGION = ("role", "status")
RENDER_ERRORS = (TypeError, KeyError, IndexError, AttributeError)


def screen_unavailable(reason: str) -> dict:
    """The screen could not be rendered from the build's own source: unknown, never a mismatch or a finding."""
    return {"keys": [], "texts": {"en": [], "tr": []}, "missing_keys": [], "actionable": None,
            "no_actor_or_action": False, "unavailable": reason}


def recovery_guidance(translator: Any, status: dict | None, rules: dict | None = None,
                      request_id: str | None = None) -> dict:
    """What RecoveryStatus (web/src/components/RecoveryAccess.tsx) of the served build shows for one status.

    H11 (upd4): rendered from the build's own source, never a frozen layout.
    The body goes through the build's ``parseRecoveryObservation`` and
    ``reconcileRecoveryObservation`` exactly as the component's ``check`` does on a
    freshly opened screen (no earlier record; a refused body sets ``unavailable``
    and keeps no record). Then the component's status region (the element with
    ``role="status"``, read by ``parse_screen_source``) is evaluated with the
    build's functions (``retryingCauseKey``, ``recoveryFailureGuidanceKey`` ...) and
    the served catalogue. ``texts`` are the visible lines in source order (the
    journal command is the region's own ``<code>`` line); ``keys`` every catalogue
    key the region looked up, in order. Two inputs are fixed and recorded: the
    read has settled (``busy`` false) and the observed-at line is left out (its
    text is the browser's locale time). A root-CLI status carries no
    ``panel_state``; the Panel adds ``ready`` before the screen sees it, so the
    model adds it too.

    Rules that were not loaded, a changed region or a construct outside the
    evaluator's subset give ``unavailable``: unknown, never a finding.
    """
    screen = (rules or {}).get("screen") or {}
    module = (rules or {}).get("module")
    if module is None or not screen.get("region"):
        return screen_unavailable(screen.get("unavailable") or "the build's screen rules were not loaded")
    evaluator = web_eval()
    rid = request_id or (status.get("request_id") if isinstance(status, dict) else None) or ""
    last, unavailable = None, False
    try:
        if not isinstance(status, dict):
            raise evaluator.JSThrow("recovery unavailable")         # the component's failed read
        observed = module.call("parseRecoveryObservation", dict({"panel_state": "ready"}, **status), rid)
        merged = module.call("reconcileRecoveryObservation", None, observed)
        last, unavailable = merged.get("record"), bool(merged.get("unavailable"))
    except evaluator.JSThrow:
        unavailable = True
    except (evaluator.Unsupported,) + RENDER_ERRORS as exc:
        return screen_unavailable(f"{type(exc).__name__}: {exc}")
    shown = {k: v for k, v in last.items() if k != "observed_at"} if isinstance(last, dict) else None
    texts: dict[str, list[str]] = {}
    keys: dict[str, list[str]] = {}
    for language in ("en", "tr"):
        called: list[str] = []

        def t(key, values=None, language=language, called=called):
            called.append(key)
            return translator.text(key, values, language=language)
        try:
            texts[language] = evaluator.render_region(screen["region"], {
                "t": t, "last": shown, "unavailable": unavailable, "busy": False, "requestId": rid,
                "locale": language}, module)
        except (evaluator.Unsupported, evaluator.JSThrow) + RENDER_ERRORS as exc:
            return screen_unavailable(f"{type(exc).__name__}: {exc}")
        keys[language] = called
    missing = [key for key in keys["en"] if not translator.has(key)]
    result = {"keys": keys["en"], "texts": texts, "missing_keys": missing,
              "actionable": not missing and keys["en"] != ["recovery.observationUnavailable"],
              "no_actor_or_action": bool(missing), "observation": last, "observation_unavailable": unavailable}
    if keys["tr"] != keys["en"]:
        result["tr_keys"] = keys["tr"]
    return result


# -- H10: the update card, rendered by the product build's own functions -------------------------

CARD_SOURCES = {"outcome": "lib/systemUpdateOutcome.ts", "observation": "lib/recoveryObservation.ts",
                "typed": "lib/systemUpdateFailure.ts", "screen": "components/RecoveryAccess.tsx"}
CARD_FUNCTIONS = ("failedUpdateGuidance", "parseRecoveryObservation", "recoveryFailureGuidanceKey",
                  "systemUpdateFailureMessage")
PLACEHOLDER_RE = re.compile(r"\{(target|previous|message|version|current|time)\}")


class CardModelUnavailable(ValueError):
    """The build's card source is outside what the evaluator reads; the card is recorded as not modelled."""


def web_eval() -> Any:
    return _load("upd1_web_source_eval", HERE / "web_source_eval.py")


def parse_screen_source(screen_tsx: str) -> dict:
    """H11: the build's RecoveryStatus status region (``role="status"``), read from its own RecoveryAccess.tsx.

    Nothing of the layout is copied here. A source whose component or region
    cannot be found exactly once, or that the evaluator cannot read, gives
    ``unavailable`` (the screen is then unknown, never a finding).
    """
    evaluator = web_eval()
    try:
        tree = evaluator.component_return_jsx(screen_tsx, SCREEN_COMPONENT)
        regions = evaluator.find_elements(tree, *SCREEN_REGION)
        if len(regions) != 1:
            raise evaluator.Unsupported(f"{len(regions)} elements with {SCREEN_REGION[0]}=\"{SCREEN_REGION[1]}\"")
    except (evaluator.Unsupported, IndexError, KeyError) as exc:
        return {"component": SCREEN_COMPONENT,
                "unavailable": f"the build's RecoveryAccess.tsx {SCREEN_COMPONENT} cannot be read: {exc}"}
    return {"component": SCREEN_COMPONENT, "region": regions[0]}


def parse_card_rules(sources: dict) -> dict:
    """The build's own card functions, ready to evaluate (``web_source_eval``), never a frozen copy.

    ``sources``: {"outcome", "observation", "typed"[, "screen"]} source texts.
    Every function the card calls is parsed up front; a construct outside the
    evaluator's subset raises ``CardModelUnavailable`` (the judge says unknown).
    """
    evaluator = web_eval()
    try:
        module = evaluator.Module({name: sources[name] for name in ("outcome", "observation", "typed")})
        for name in CARD_FUNCTIONS:
            module.function(name)
        codes = module.constant("recoveryFailureCodes")
        command = module.constant("RECOVERY_LOG_COMMAND")
    except (evaluator.Unsupported, KeyError) as exc:
        raise CardModelUnavailable(f"the build's card source cannot be read: {type(exc).__name__}: {exc}") from exc
    return {"module": module, "failure_codes": list(codes), "command": command,
            "screen": parse_screen_source(sources.get("screen") or "")}


def load_card_rules(web_src: Path) -> dict:
    """Read the rules from one product build's ``web/src`` (``product_web_src`` of an artifact role)."""
    root = Path(web_src)
    texts = {}
    for name, relative in CARD_SOURCES.items():
        try:
            texts[name] = (root / relative).read_text(encoding="utf-8")
        except OSError as exc:
            raise CardModelUnavailable(f"{relative}: {type(exc).__name__}") from exc
    rules = parse_card_rules(texts)
    rules["source"] = {name: {"path": relative, "sha256": hashlib.sha256(texts[name].encode()).hexdigest()}
                       for name, relative in CARD_SOURCES.items()}
    return rules


def card_model(rules: dict | None, status_body: dict | None, recovery_body: dict | None, request_id: str | None,
               versions: dict, translator: Any) -> dict:
    """What SystemUpdateOperation.tsx shows for one update-status body and the exact request's recovery body.

    The outer selection (found / succeeded / running / failed / identity) is the
    component's (JSX, mirrored here); for a failed update the card is exactly
    what the build's ``failedUpdateGuidance`` returns for the observation its
    ``parseRecoveryObservation`` accepts, with the component's inputs (message
    = the server summary, product messages, typed message through the build's
    ``systemUpdateFailureMessage``, the settled read). Items per language:
    title, lines, the fixed command, the server-reported line.
    """
    if rules is None:
        return {"state": None, "items": {"en": [], "tr": []}, "unavailable": "card rules not loaded"}
    if not isinstance(status_body, dict) or not status_body.get("found"):
        return {"state": "none", "items": {"en": [], "tr": []}}
    status = status_body.get("status")
    same = status_body.get("request_id") == request_id
    if same and status == "succeeded":
        items = [{"key": "panelUpdate.succeeded"}, {"key": "panelUpdate.succeeded"}]
        return {"state": "succeeded", "items": {"en": items, "tr": items}}
    if same and status in ("running", "queued"):
        items = [{"key": "panelUpdate.title"}, {"key": f"panelUpdate.{status}"}]
        return {"state": "in-progress", "items": {"en": items, "tr": items}}
    evaluator, module = web_eval(), rules["module"]
    observation, parse_error = None, None
    if isinstance(recovery_body, dict) and request_id:
        try:
            observation = module.call("parseRecoveryObservation", recovery_body, request_id)
        except evaluator.JSThrow as exc:
            parse_error = str(exc)
    items: dict[str, list] = {}
    state = None
    for language in ("en", "tr"):
        def t(key, values=None, language=language):
            return translator.text(key, values, language=language)
        shown = t("panelUpdate.identityMismatchCleared") if not same else (status_body.get("summary")
                                                                          or t("panelUpdate.failed"))
        typed = module.call("systemUpdateFailureMessage", shown, t)
        guidance = module.call("failedUpdateGuidance", observation, {
            "targetVersion": versions["target"], "previousVersion": versions["previous"],
            "message": "" if shown == t("panelUpdate.failed") else shown,
            "productMessages": [t("panelUpdate.notAccepted"), t("panelUpdate.identityMismatchCleared")],
            "typedMessage": typed if typed != shown else None, "reading": False})
        if not isinstance(guidance, dict) or not isinstance(guidance.get("lines"), list):
            raise CardModelUnavailable("failedUpdateGuidance returned no card")
        entries = [guidance.get("title")] + list(guidance["lines"])
        rendered = [dict(entry) for entry in entries if isinstance(entry, dict)]
        if guidance.get("command"):
            rendered.append({"text": guidance["command"]})
        if guidance.get("serverMessage"):
            rendered.append({"key": "panelUpdate.outcome.serverMessage",
                             "vars": {"message": guidance["serverMessage"]}, "server_message": True})
        items[language] = rendered
        state = state or guidance.get("state")
    return {"state": state, "items": items, "observation": observation, "observation_error": parse_error}


def update_card_guidance(translator: Any, status: dict | None, recovery: dict | None = None,
                         rules: dict | None = None, request_id: str | None = None,
                         versions: dict | None = None) -> dict:
    """The rendered update card (keys, EN/TR texts) from the build's own functions; see ``card_model``."""
    versions = versions or {"target": CANDIDATE_VERSION, "previous": BASELINE_VERSION}
    request_id = request_id or (status or {}).get("request_id")
    evaluator = web_eval()
    try:
        model = card_model(rules, status, recovery, request_id, versions, translator)
    except (CardModelUnavailable, evaluator.Unsupported, evaluator.JSThrow) as exc:
        model = {"state": None, "items": {"en": [], "tr": []}, "unavailable": f"{type(exc).__name__}: {exc}"}
    texts = {language: [item["text"] if "text" in item else translator.text(item["key"], item.get("vars"),
                                                                              language=language)
                        for item in model["items"][language]] for language in ("en", "tr")}
    keys = [item["key"] for item in model["items"]["en"] if "key" in item]
    tr_keys = [item["key"] for item in model["items"]["tr"] if "key" in item]
    # upd5: OutcomeText.fallback - SystemUpdateOperation shows the boot-catalogue fallback until the screen catalogue
    # holding ``key`` has arrived. ``texts`` are the settled card (screens ready); the fallback lines are kept beside.
    fallbacks = {language: [{"key": item["key"], "fallback": item["fallback"],
                             "text": translator.text(item["fallback"], item.get("vars"), language=language)}
                            for item in model["items"][language] if isinstance(item.get("fallback"), str)]
                 for language in ("en", "tr")}
    fallback_keys = [entry["fallback"] for entry in fallbacks["en"]]
    server = next((item["vars"]["message"] for item in model["items"]["en"] if item.get("server_message")), None)
    card = {"state": model["state"], "keys": keys, "texts": texts,
            "missing_keys": sorted({k for k in keys + tr_keys + fallback_keys if not translator.has(k)}),
            "server_message": server, "unavailable": model.get("unavailable"),
            "source": (rules or {}).get("source"), "observation_error": model.get("observation_error")}
    if fallback_keys:
        card["fallbacks"] = fallbacks
    if tr_keys != keys:
        card["tr_keys"] = tr_keys
    return card


def judge_update_card(card: dict | None, expected_states: Iterable[str]) -> dict:
    """A finding only for a real mismatch: a key the build's catalogue lacks, an unfilled placeholder, or a card
    state that contradicts the server's recovery record for this cell. A card that cannot be modelled is unknown;
    the server-reported secondary line is recorded, never judged."""
    expected_states = tuple(expected_states)
    if not isinstance(card, dict) or card.get("unavailable") or card.get("state") is None:
        return {"verdict": "unknown", "reason": (card or {}).get("unavailable") or "card not read", "findings": []}
    findings = []
    if card.get("missing_keys"):
        findings.append(f"card keys missing from the build's catalogue: {card['missing_keys']}")
    for language in ("en", "tr"):
        unfilled = sorted({m.group(0) for text in card["texts"][language] for m in PLACEHOLDER_RE.finditer(text)})
        if unfilled:
            findings.append(f"card {language} text has unfilled placeholders {unfilled}")
    if card.get("state") not in expected_states:
        findings.append(f"the card shows state {card.get('state')!r} while this cell's server record expects "
                        f"{list(expected_states)}")
    return {"verdict": "finding" if findings else "as-expected", "findings": findings,
            "server_message_line": card.get("server_message")}


def stopped_before_change(status: dict | None) -> bool:
    """upd5: the product's typed stop before any change: ``failed``/``none`` with ``previous_failure=update_failed``
    and a preflight stop code. The record carries that code only when the updater reported ``state=unchanged``, and
    nothing follows for the request (cmd/recovery: "nothing more happens for this request")."""
    return (isinstance(status, dict) and status.get("observation") == "known" and status.get("phase") == "failed"
            and status.get("terminal_proof") == "none" and status.get("previous_failure") == "update_failed"
            and status.get("failure_code") in PREFLIGHT_STOP_CODES
            and not status.get("automatic_recovery") and not status.get("waiting_for"))


def classify_status(status: dict | None) -> str:
    """``terminal``, ``stopped`` (a typed stop before any change, final for the request), ``paused`` (the owner must
    act), ``in-progress`` (including ``retry_scheduled`` and ``pause_pending``: recovery still runs) or ``unknown``."""
    if not isinstance(status, dict) or status.get("observation") != "known":
        return "unknown"
    pair = (status.get("phase"), status.get("terminal_proof"))
    if pair in TERMINAL:
        return "terminal"
    if stopped_before_change(status):
        return "stopped"
    if status.get("phase") == "recovery_required" and status.get("automatic_recovery") == "paused_retry_limit":
        return "paused"
    # pause_pending is not the pause: the owner is not asked to act before the next timer run records it.
    return "in-progress"


def stop_line_confirms(final: dict | None, lines: list[dict] | None) -> bool | None:
    """Does the updater's own failure line confirm the record's typed stop (same code, ``state=unchanged``)?
    None while the line was not read."""
    if lines is None:
        return None
    code = (final or {}).get("failure_code")
    return any(line.get("code") == code and line.get("state") == "unchanged" for line in lines)


RUNNING_AFTER_SUCCESS_RULE = "record-running-after-panel-success"
RUNNING_AFTER_SUCCESS_SECONDS = 900.0


class RunningAfterSuccess:
    """H17 (upd7): after an update started by a historical Agent (v0.1.0-alpha.80) the updater writes the initial
    running record itself, and the record may stay ``running`` after success until the new Agent reconciles the
    request (48d21d58, open point). When the Panel's own update status says ``succeeded`` while the recovery record
    stays known ``running``/``none`` for 900 s without change, ``track`` stops with this named rule (verdict
    observed, never passed) and records when each side was first seen; it does not wait 90 minutes."""

    def __init__(self, seconds: float = RUNNING_AFTER_SUCCESS_SECONDS) -> None:
        self.limit = seconds
        self.since: float | None = None
        self.first: dict | None = None
        self.count = 0

    def observe(self, sample: dict, observed: dict | None, now: float) -> dict | None:
        body = ((sample.get("update_status") or {}).get("body")) if isinstance(sample.get("update_status"), dict) else None
        panel_ok = (sample.get("update_status") or {}).get("http") == 200 and isinstance(body, dict) \
            and body.get("status") == "succeeded"
        running = isinstance(observed, dict) and observed.get("observation") == "known" \
            and observed.get("phase") == "running" and observed.get("terminal_proof") == "none"
        if not (panel_ok and running):
            self.since, self.first, self.count = None, None, 0
            return None
        if self.since is None:
            self.since, self.first = now, {"utc": sample.get("utc"), "index": sample.get("index")}
        self.count += 1
        if now - self.since < self.limit:
            return None
        return {"rule": RUNNING_AFTER_SUCCESS_RULE, "seconds": round(now - self.since, 1), "samples": self.count,
                "first": self.first, "last": {"utc": sample.get("utc"), "index": sample.get("index")},
                "panel_status": {k: body.get(k) for k in ("status", "updated_at", "summary")},
                "record": {k: observed.get(k) for k in ("phase", "terminal_proof", "reason", "observed_at",
                                                        "previous_failure", "automatic_recovery")}}


SETTLED_FAILED_RULE = "settled-failed-before-change"
SETTLED_FAILED_SECONDS = 600.0
SETTLED_FAILED_SAMPLES = 3


class SettledFailure:
    """H8 (upd3): when does ``track`` stop on a failure the product keeps unchanged?

    A failure before any change (phase ``failed``, proof ``none``) with no
    automatic recovery and no wait has no terminal or paused state; the 90-min
    deadline would only delay the same inconclusive verdict (upd3 cell 1). The
    rule: the known status has stayed exactly the same failed/none status (every
    field in ``FIELDS``) for ``seconds`` over at least ``samples`` reads, with no
    recovery activity. Any change, or any other status, restarts the clock.
    """

    FIELDS = ("phase", "terminal_proof", "reason", "previous_failure", "failure_code", "first_failure_code",
              "automatic_recovery", "waiting_for")

    def __init__(self, seconds: float = SETTLED_FAILED_SECONDS, samples: int = SETTLED_FAILED_SAMPLES) -> None:
        self.seconds, self.samples = seconds, samples
        self.signature: tuple | None = None
        self.since: float | None = None
        self.count = 0

    @staticmethod
    def quiet(observed: Any) -> bool:
        return (isinstance(observed, dict) and observed.get("observation") == "known"
                and observed.get("phase") == "failed" and observed.get("terminal_proof") == "none"
                and not observed.get("automatic_recovery") and not observed.get("waiting_for"))

    def observe(self, observed: Any, now: float) -> bool:
        if not self.quiet(observed):
            self.signature, self.since, self.count = None, None, 0
            return False
        signature = tuple(observed.get(field) for field in self.FIELDS)
        if signature != self.signature:
            self.signature, self.since, self.count = signature, now, 0
        self.count += 1
        return self.count >= self.samples and now - self.since >= self.seconds

    def record(self, now: float) -> dict:
        since = self.since if self.since is not None else now
        return {"rule": SETTLED_FAILED_RULE, "seconds": round(now - since, 1), "samples": self.count,
                "status": dict(zip(self.FIELDS, self.signature or ())), "limit_seconds": self.seconds}


STOPPED_BEFORE_CHANGE = "stopped-before-change"


def classify_outcome(variant: str, final: dict | None, owner_continued: bool,
                     failure_lines: list[dict] | None = None) -> str:
    """The cell's outcome class. A typed stop before any change is ``stopped-before-change`` once the updater's
    failure line confirms it (same code, ``state=unchanged``); ``-unconfirmed`` while that line was not read and
    ``-contradicted`` when the line read says otherwise."""
    state = classify_status(final)
    if state == "stopped":
        confirmed = stop_line_confirms(final, failure_lines)
        return (STOPPED_BEFORE_CHANGE if confirmed else STOPPED_BEFORE_CHANGE + "-unconfirmed" if confirmed is None
                else STOPPED_BEFORE_CHANGE + "-contradicted")
    if state == "paused":
        return "paused-owner-action-required"
    if state != "terminal":
        return "not-terminal"
    phase = final["phase"]
    if variant == "owner-continuation":
        # The one expected class; every other end is a finding of the owner-continuation judge.
        if phase == "succeeded":
            return ("recovered-after-owner-continuation" if owner_continued
                    else "update-verified-without-owner-continuation")
        return "rolled-back-after-owner-continuation" if owner_continued else "rolled-back-instead-of-forward"
    if variant in ROLLBACK_VARIANTS:
        if phase == "recovered":
            return "recovered-after-owner-continuation" if owner_continued else "recovered-automatically"
        return "defective-candidate-reported-success"
    if variant == "real-start":
        # The expected end of this cell is the pause above; a terminal state is itself an observation.
        return "real-start-candidate-rolled-back" if phase == "recovered" else "real-start-candidate-reported-success"
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


PANEL_UNTIL_END = "down-from-update-until-end"


def panel_verdict_until_end(windows: list[dict], started_at: float | None) -> dict:
    """real-start: the Panel may be down from the owner start to the end of the record, and never before.

    ``down-from-update-until-end`` is the expected observation for this fixture
    (its panel never listens). ``never-down`` or ``came-back`` mean the fixture
    did not do what it claims; ``down-outside-transaction`` is a Panel outage
    before the owner started.
    """
    outside = []
    for window in windows:
        begin = window.get("from") if window.get("from") is not None else window.get("first_bad")
        if started_at is None or begin is None or begin < started_at - SAMPLE_INTERVAL_S * 2:
            outside.append(window)
    if outside:
        verdict = "down-outside-transaction"
    elif not windows:
        verdict = "never-down"
    elif windows[-1].get("to") is None:
        verdict = PANEL_UNTIL_END
    else:
        verdict = "came-back"
    return {"verdict": verdict, "outside": outside}


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


def getent_outcome(result: dict | None) -> str:
    """getent(1) exit codes: 0 found, 2 key not found (a valid answer), anything else a probe failure.

    Sidecar v2 (upd3): v1 treated rc 2 as a failed probe and lost every light probe of cell 1.
    Mirrors guest_upd1_workload.getent_outcome (a test pins the equality).
    """
    if not isinstance(result, dict) or result.get("status") != "ok":
        return "probe-failed"
    return {0: "found", 2: "not-found"}.get(result.get("returncode"), "probe-failed")


def origin_verdict(check: dict) -> dict:
    """Is the guest-loopback fixture origin the only answer for celikpanel.net?

    ``check``: guest_upd1_workload ``origin-check`` output. Every resolved
    address must be 127.0.0.1 and the fixture must answer HTTP 200.

    H7 (upd2): ``getent hosts celikpanel.net`` prints answers for that one name
    only, but with nss-resolve (Arch) a hosts-file name that shares 127.0.0.1
    with ``localhost`` is printed under its canonical name (``127.0.0.1
    localhost``). Every answer line counts; the raw output is recorded.
    """
    addresses = []
    raw = str((check.get("getent") or {}).get("stdout") or "")
    for line in raw.splitlines():
        fields = line.split()
        if fields:
            addresses.append(fields[0])
    http = str((check.get("https") or {}).get("stdout") or "").strip()
    unit = check.get("unit") or {}
    loopback = bool(addresses) and all(address == "127.0.0.1" for address in addresses)
    return {"addresses": addresses, "getent_stdout": raw[:512],
            "getent_status": (check.get("getent") or {}).get("status"),
            "getent_returncode": (check.get("getent") or {}).get("returncode"),
            # Sidecar v2: rc 2 is "not found" (the fixture name does not resolve), not a failed probe.
            "getent_outcome": getent_outcome(check.get("getent")),
            "http": http, "loopback_only": loopback,
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


def deferred_mail_watched(cell: Cell) -> bool:
    """upd12: cells whose update ends with a running Panel on a mail stack (the real-start Panel never listens;
    management-off restarts the Panel by its own reboot)."""
    return cell.mail_required and cell.scenario is None and cell.variant not in ("real-start", "mgmt-off-reboot")


def deferred_mail_view(text: str) -> dict:
    """upd12: per Panel process (journal PID, from its "Starting CelikPanel Backend..." line): which startup step was
    refused as busy with the retry note, every "startup mail work" line of the bounded retry, and whether the retry
    resolved (each deferred step completed or failed once, or the give-up line). Lines are kept verbatim."""
    processes: list[dict] = []
    by_pid: dict[str, dict] = {}
    for line in text.splitlines():
        match = PANEL_JOURNAL_LINE.match(line)
        if not match:
            continue
        at, pid, message = match.groups()
        if message.startswith("Starting CelikPanel Backend..."):
            process = {"pid": pid, "started_at": at, "ready_at": None, "startup_lines": [], "deferred": [],
                       "attempts": [], "resolved": {}, "gave_up": False, "other_mail_lines": []}
            processes.append(process)
            by_pid[pid] = process
            continue
        process = by_pid.get(pid)
        if process is None:
            continue
        prefix = next((p for p in DEFERRED_MAIL_TASKS if message.startswith(p)), None)
        attempt = DEFERRED_MAIL_ATTEMPT.match(message)
        if prefix:
            process["startup_lines"].append({"at": at, "text": message})
            if DEFERRED_MAIL_NOTE in message and DEFERRED_MAIL_TASKS[prefix] not in process["deferred"]:
                process["deferred"].append(DEFERRED_MAIL_TASKS[prefix])
        elif message.startswith("Panel ready on") and process["ready_at"] is None:
            process["ready_at"] = at
        elif attempt:
            entry = {"at": at, "attempt": int(attempt[1]), "of": int(attempt[2]), "text": message}
            process["attempts"].append(entry)
            for task in process["deferred"]:
                for outcome in ("completed", "failed"):
                    if f"{task} {outcome}" in message:
                        process["resolved"].setdefault(task, []).append(
                            {"outcome": outcome, "attempt": entry["attempt"], "at": at})
            if DEFERRED_MAIL_GIVE_UP in message:
                process["gave_up"] = True
        elif "mail SNI reconciled" in message or message.startswith("milter chain:"):
            process["other_mail_lines"].append({"at": at, "text": message})
    for process in processes:
        process["finished"] = (not process["deferred"] or process["gave_up"]
                               or all(task in process["resolved"] for task in process["deferred"]))
        process["repeated"] = sorted(task for task, seen in process["resolved"].items() if len(seen) > 1)
    return {"processes": processes, "last": processes[-1] if processes else None}


def mail_file_facts_script() -> str:
    """upd12: read-only size, mtime and SHA-256 of the native mail files the deferred steps may write."""
    lines = ["for f in " + " ".join(shlex.quote(p) for p in DEFERRED_MAIL_FILES) + "; do",
             '  if [ -f "$f" ]; then printf \'%s|%s|%s\\n\' "$f" "$(stat -c \'%s|%y\' "$f")" '
             '"$(sha256sum < "$f" | cut -c1-64)"; else printf \'%s|absent\\n\' "$f"; fi',
             "done",
             "for k in smtpd_milters non_smtpd_milters tls_server_sni_maps; do "
             "printf 'postconf|%s|%s\\n' \"$k\" \"$(postconf -h \"$k\" 2>/dev/null || echo unavailable)\"; done"]
    return "\n".join(lines) + "\n"


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


def overall(verdicts: list[str], kind_not_reached: bool = False) -> str:
    """The cell's verdict from its steps. ``kind_not_reached`` (the kind judge said ``not-measured``): steps that
    could not run or stayed inconclusive because the candidate never ran are part of that one answer,
    ``inconclusive-kind-not-reached``; a failed step still makes the cell ``failed``."""
    for verdict in verdicts:
        if verdict not in STEP_VERDICTS:
            raise ValueError(f"unknown step verdict {verdict!r}")
    if "failed" in verdicts:
        return "failed"
    if kind_not_reached:
        return OVERALL_KIND_NOT_REACHED
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
            "automatic": [{"attempt": r["name"], "at": r.get("mtime_utc"), "phase": receipt_phase(r),
                           "operation": receipt_operation(r),
                           "direction": dispatch_direction(receipt_operation(r), receipt_phase(r))}
                          for r in automatic],
            "owner_count": len(owner),
            "owner": [{"name": r["name"], "at": r.get("mtime_utc"), "phase": receipt_phase(r),
                       "operation": receipt_operation(r),
                       "direction": dispatch_direction(receipt_operation(r), receipt_phase(r))} for r in owner]}


def receipt_phase(receipt: dict) -> str | None:
    """``phase=`` of a dispatch receipt the guest judged safe (read with ``operation=``: ``dispatch_direction``)."""
    match = re.search(r"^phase=([a-z-]+)$", str(receipt.get("text") or ""), re.M)
    return match.group(1) if match else None


def receipt_operation(receipt: dict) -> str | None:
    match = re.search(r"^operation=([a-z]+)$", str(receipt.get("text") or ""), re.M)
    return match.group(1) if match else None


FORWARD_PHASES = ("completion", "completion-scheduler")


def dispatch_direction(operation: str | None, phase: str | None) -> str:
    """H9 (planner's rule, upd3): which way one automatic dispatch went.

    ``rollback``: ``operation=rollback`` in ANY phase (a resumed rollback's own
    completion is ``rollback/completion``, arch-startcheck upd3), or
    ``operation=update phase=active`` (the update failed while still active).
    ``forward``: ``operation=update`` with ``phase=completion`` (or its
    scheduler step). Anything else is ``unknown`` (never guessed).
    """
    if operation == "rollback":
        return "rollback"
    if operation == "update" and phase == "active":
        return "rollback"
    if operation == "update" and phase in FORWARD_PHASES:
        return "forward"
    return "unknown"


def directions_rule(dispatches: list[dict] | None, expected: str) -> bool | None:
    """True when every dispatch went ``expected``; False when one went the other known way; None otherwise."""
    if not dispatches:
        return None
    directions = [d.get("direction") or dispatch_direction(d.get("operation"), d.get("phase")) for d in dispatches]
    other = "forward" if expected == "rollback" else "rollback"
    if other in directions:
        return False
    return True if all(d == expected for d in directions) else None


# ---------------------------------------------------------------------------
# upd3: candidate-panel start kinds (pure rules; covered offline)
# ---------------------------------------------------------------------------

def parse_failure_sidecar(raw: str | None, request_id: str, target_commit: str) -> dict:
    """<request>.failure, celikpanel-recovery-failure/v1 (internal/recoveryobs DecodeFailure, 8ffc5e06).

    Exactly four fixed lines bound to this request and the candidate commit;
    anything else is recorded as invalid (the product then shows generic text).
    """
    if raw is None:
        return {"present": False, "valid": False, "code": None}
    if raw == "withheld":
        return {"present": True, "valid": False, "code": None, "reason": "withheld by the secret-looking filter"}
    lines = raw.split("\n")
    expected = [f"schema={FAILURE_SIDECAR_SCHEMA}", f"request_id={request_id}", f"target_commit={target_commit}"]
    if len(lines) != 5 or lines[:3] != expected or not lines[3].startswith("failure_code=") or lines[4] != "":
        return {"present": True, "valid": False, "code": None, "reason": "fields, request or target commit differ",
                "text": raw[:512]}
    code = lines[3][len("failure_code="):]
    if code not in FAILURE_CODES:
        return {"present": True, "valid": False, "code": None, "reason": f"unknown failure code {code!r}"}
    return {"present": True, "valid": True, "code": code}


def sidecar_from_records(records: dict | None, request_id: str, target_commit: str) -> dict:
    """The sidecar among the collected observation records (observation_records_script output)."""
    if not isinstance(records, dict):
        return {"present": None, "valid": False, "code": None, "reason": "observation records not collected"}
    return parse_failure_sidecar((records.get("records") or {}).get(request_id + ".failure"), request_id,
                                 target_commit)


def parse_renewal_sidecar(raw: str | None, request_id: str, target_commit: str) -> dict:
    """<request>.renewal, celikpanel-recovery-renewal/v1 (internal/recoveryobs DecodeRenewal), read only.

    Exactly four fixed lines bound to this request and the candidate commit, the last
    ``renewal_before_update=on|off``; anything else is invalid (the product then says "not recorded")."""
    if raw is None:
        return {"present": False, "valid": False, "value": None}
    if raw == "withheld":
        return {"present": True, "valid": False, "value": None, "reason": "withheld by the secret-looking filter"}
    lines = raw.split("\n")
    expected = [f"schema={RENEWAL_SIDECAR_SCHEMA}", f"request_id={request_id}", f"target_commit={target_commit}"]
    if (len(raw.encode()) > 2048 or len(lines) != 5 or lines[:3] != expected or lines[4] != ""
            or lines[3] not in ("renewal_before_update=on", "renewal_before_update=off")):
        return {"present": True, "valid": False, "value": None, "reason": "fields, request, commit or value differ",
                "text": raw[:512]}
    return {"present": True, "valid": True, "value": lines[3].split("=", 1)[1]}


def renewal_sidecar_from_records(records: dict | None, request_id: str, target_commit: str) -> dict:
    if not isinstance(records, dict):
        return {"present": None, "valid": False, "value": None, "reason": "observation records not collected"}
    return parse_renewal_sidecar((records.get("records") or {}).get(request_id + ".renewal"), request_id,
                                 target_commit)


def renewal_expected(timers: dict | None) -> str | None:
    """What the updater should record from the pre-update timer snapshot (update.sh rule): ``on`` when a Certbot
    timer was enabled or active, ``off`` when neither (or none is installed); None without a snapshot."""
    if not isinstance(timers, dict):
        return None
    for unit in RENEWAL_UNITS:
        state = timers.get(unit) or {}
        if state.get("UnitFileState") in RENEWAL_ON_UNIT_FILE or state.get("ActiveState") in RENEWAL_ON_ACTIVE:
            return "on"
    return "off"


def renewal_at_pause(status: dict | None, pre_timers: dict | None, sidecar: dict | None = None) -> dict:
    """``renewal_before_update`` at the pause against the pre-update timer snapshot (and, once collected, the
    sidecar). ``mismatch`` is a finding; ``not-recorded`` (an older product or no sidecar) and ``unknown``
    (no snapshot) are not."""
    recorded = (status or {}).get("renewal_before_update")
    recorded = recorded if recorded in ("on", "off") else None
    expected = renewal_expected(pre_timers)
    snapshot = {unit: {k: ((pre_timers or {}).get(unit) or {}).get(k) for k in ("UnitFileState", "ActiveState")}
                for unit in RENEWAL_UNITS if unit in (pre_timers or {})}
    result = {"recorded": recorded, "expected_from_pre_update_timers": expected, "pre_update_timers": snapshot,
              "findings": []}
    if sidecar and sidecar.get("present") is not None:
        result["sidecar"] = sidecar
        if sidecar.get("valid") and sidecar.get("value") != recorded:
            result["findings"].append(f"the <request>.renewal sidecar says {sidecar.get('value')!r} but the status at "
                                      f"the pause says renewal_before_update={recorded!r}")
    if recorded is None:
        result["verdict"] = "not-recorded"
    elif expected is None:
        result["verdict"] = "unknown"
    elif recorded == expected:
        result["verdict"] = "as-before"
    else:
        result["verdict"] = "mismatch"
        result["findings"].append(f"the pause says renewal_before_update={recorded} but the pre-update timer "
                                  f"snapshot says {expected}: {snapshot or 'no Certbot timer installed'}")
    if result["findings"] and result["verdict"] != "mismatch":
        result["verdict"] = "mismatch"
    return result


UPDATE_FAILURE_RE = re.compile(r"CELIKPANEL_UPDATE_FAILURE code=(\S*) state=(\S*) reason=(.*?) detail=(.*)$")
START_CHECK_REASON_RE = re.compile(r"panel startup check failed: ([a-z_]+): ([^\n]+)")
# update.sh fail_update_preflight / cmd/panel boundedPanelUpdateFailure: closed lowercase step and class tokens.
PREFLIGHT_REASON_RE = re.compile(r"^update preflight step=([a-z_]{1,40}) class=([a-z_]{1,40})(?:[: ]|$)")
RUNTIME_REASON_RE = re.compile(r"^recovery runtime preflight step=([a-z_]{1,40})(?:[: ]|$)")


def parse_update_failure_lines(text: str) -> list[dict]:
    """The update's final ``!! CELIKPANEL_UPDATE_FAILURE code=.. state=.. reason=.. detail=..`` lines (update.sh).

    upd5: a refused preflight also yields its ``step`` and ``class``, a runtime preflight stop its ``step``;
    ``bounded`` marks the Panel's form rebuilt from closed tokens only (reason = the tokens, empty detail)."""
    found = []
    for line in str(text or "").splitlines():
        match = UPDATE_FAILURE_RE.search(line)
        if match:
            code, reason, detail = match.group(1), match.group(3), match.group(4)
            entry = {"code": code, "state": match.group(2), "reason": reason[:300]}
            typed = (PREFLIGHT_REASON_RE.match(reason) if code == PREFLIGHT_REFUSED_CODE
                     else RUNTIME_REASON_RE.match(reason) if code == RUNTIME_PREFLIGHT_CODE else None)
            if typed:
                entry["step"] = typed.group(1)
                if code == PREFLIGHT_REFUSED_CODE:
                    entry["class"] = typed.group(2)
                entry["bounded"] = detail == "" and reason == typed.group(0).rstrip(": ")
            found.append(entry)
    return found


def parse_start_check_reasons(text: str) -> list[dict]:
    """The start check's one product-authored reason line, as the updater's die message carries it."""
    return [{"code": code, "text": detail.strip()[:240]}
            for code, detail in START_CHECK_REASON_RE.findall(str(text or ""))]


def _go_string(literal: str) -> str:
    return json.loads(literal)


GO_TERM_RE = re.compile(r'\s*(?:("(?:[^"\\]|\\.)*")|([A-Za-z_]\w*))')


def _go_concat(source: str, pos: int, env: dict) -> tuple[str, int]:
    """One Go string concatenation (literals and names bound in ``env``) from ``pos``; returns (value, end)."""
    parts = []
    while True:
        match = GO_TERM_RE.match(source, pos)
        if not match:
            raise ValueError(f"unsupported Go expression at {source[pos:pos + 40]!r}")
        if match.group(1):
            parts.append(_go_string(match.group(1)))
        elif match.group(2) in env:
            parts.append(env[match.group(2)])
        else:
            raise ValueError(f"unbound Go name {match.group(2)!r}")
        pos = match.end()
        plus = re.match(r"\s*\+", source[pos:])
        if not plus:
            return "".join(parts), pos
        pos += plus.end()


def _go_request_text(go_source: str, name: str) -> dict:
    """EN/TR of a ``func <name>(requestID string) (string, string, bool)`` guidance (cmd/recovery
    preflightStoppedGuidance / preflightRefusedGuidance) with ``{request_id}`` where the request id goes."""
    body = re.search(r"\nfunc " + re.escape(name) + r"\(requestID string\) \(string, string, bool\) \{\n(.*?)\n}\n",
                     go_source, re.S)
    if not body:
        raise ValueError(f"cmd/recovery/main.go has no {name}")
    env = {"requestID": "{request_id}"}
    text = body.group(1)
    for assign in re.finditer(r"\n?\t(\w+) := ", text):
        env[assign.group(1)] = _go_concat(text, assign.end(), env)[0]
    ret = re.search(r"\treturn ", text)
    if not ret:
        raise ValueError(f"{name} returns no text")
    en, pos = _go_concat(text, ret.end(), env)
    comma = re.match(r"\s*,", text[pos:])
    if not comma:
        raise ValueError(f"{name} returns no Turkish text")
    tr, pos = _go_concat(text, pos + comma.end(), env)
    if not re.match(r"\s*,\s*true\s*$", text[pos:]):
        raise ValueError(f"{name} does not end with true")
    return {"en": en, "tr": tr}


def cli_text(texts: dict, key: str, language: str, status: dict | None) -> str:
    """The reviewed CLI text for one key and language, with this status's request id where the product puts it."""
    return texts[key][language].replace("{request_id}", str((status or {}).get("request_id") or ""))


def parse_cli_guidance(go_source: str) -> dict:
    """EN/TR owner texts of the PRODUCT's root recovery CLI (cmd/recovery/main.go), never copies.

    Keys: ``<code>.recovered`` / ``<code>.pending`` from failureCodeGuidance,
    ``paused`` (the paused_retry_limit text, which takes precedence), and
    ``cause_markers``: how the output names the typed cause, as this source
    prints it (the 8ffc5e06 ``<label>: <code>`` line and/or a later support
    line carrying ``failure_code=<code>``).
    """
    string = r'("(?:[^"\\]|\\.)*")'
    body = re.search(r"\nfunc failureCodeGuidance\(.*?\n}\n", go_source, re.S)
    if not body:
        raise ValueError("cmd/recovery/main.go has no failureCodeGuidance (product before 8ffc5e06?)")
    texts: dict[str, dict] = {}
    for segment in re.split(r'\n\tcase "', body.group(0))[1:]:
        code = segment.split('"', 1)[0]
        for condition, en, tr in re.findall(r"if ([^{]+)\{\s*return " + string + r",\s*" + string + r", true",
                                            segment):
            state = "recovered" if '"recovered"' in condition else "pending"
            texts[f"{code}.{state}"] = {"en": _go_string(en), "tr": _go_string(tr)}
        # upd5: the preflight stops return a request-bound text from a helper (``{request_id}`` in the template).
        for condition, helper in re.findall(r"if ([^{]+)\{\s*return (\w+)\(status\.RequestID\)", segment):
            state = "recovered" if '"recovered"' in condition else "pending"
            texts[f"{code}.{state}"] = _go_request_text(go_source, helper)
    paused = re.search(r'AutomaticRecovery == "paused_retry_limit" \{\s*en, tr = ' + string + r",\s*" + string,
                       go_source)
    if not paused:
        raise ValueError("cmd/recovery/main.go paused_retry_limit text not found")
    texts["paused"] = {"en": _go_string(paused.group(1)), "tr": _go_string(paused.group(2))}
    # upd5: recovery still finishing its last attempt (optional: older products have no pause_pending).
    pending = re.search(r'AutomaticRecovery == "pause_pending" \{\s*en, tr = ' + string + r",\s*" + string, go_source)
    if pending:
        texts["pause_pending"] = {"en": _go_string(pending.group(1)), "tr": _go_string(pending.group(2))}
    # upd5: the pause's renewal sentence, "on" (renewal was stopped) or "off" (already off before the update).
    renewal = {name: _go_string(value) for name, value in
               re.findall(r"\n\t(pausedRenewal(?:Off)?(?:EN|TR)) += " + string, go_source)}
    if {"pausedRenewalEN", "pausedRenewalTR"} <= set(renewal):
        texts["paused_renewal"] = {"on": {"en": renewal["pausedRenewalEN"], "tr": renewal["pausedRenewalTR"]}}
        if {"pausedRenewalOffEN", "pausedRenewalOffTR"} <= set(renewal):
            texts["paused_renewal"]["off"] = {"en": renewal["pausedRenewalOffEN"], "tr": renewal["pausedRenewalOffTR"]}
    markers: dict[str, list] = {"en": [], "tr": []}
    label = re.search(r'translated\(lang, ("[^"]+"), ("[^"]+")\), status\.FailureCode', go_source)
    if label:
        markers["en"].append(_go_string(label.group(1)) + ": {code}")
        markers["tr"].append(_go_string(label.group(2)) + ": {code}")
    if '" failure_code=" + status.FailureCode' in go_source:
        for lang in ("en", "tr"):
            markers[lang].append("failure_code={code}")
    if not markers["en"]:
        raise ValueError("cmd/recovery/main.go prints the typed cause in no known form")
    texts["cause_markers"] = markers
    for key in (f"{START_CHECK_CODE}.recovered", f"{START_CHECK_CODE}.pending", f"{REAL_START_CODE}.pending"):
        if key not in texts:
            raise ValueError(f"cmd/recovery/main.go has no {key} text")
    return texts


def cli_guidance_key(status: dict | None) -> str | None:
    """Which reviewed CLI text writeStatus prints first for one status (pause > wait > typed cause)."""
    if not isinstance(status, dict) or status.get("observation") != "known":
        return None
    phase, proof = status.get("phase"), status.get("terminal_proof")
    if phase == "recovery_required" and proof == "none" and status.get("automatic_recovery") == "paused_retry_limit":
        return "paused"
    if phase == "recovery_required" and proof == "none" and status.get("automatic_recovery") == "pause_pending":
        return "pause_pending"
    if phase == "recovering" and proof == "none" and status.get("waiting_for") in ("initializing", "starting",
                                                                                  "stopping"):
        return None
    code = status.get("failure_code") if status.get("previous_failure") == "update_failed" else None
    if code == START_CHECK_CODE and phase == "recovered" and proof == "rollback_verified":
        return f"{START_CHECK_CODE}.recovered"
    if code in CLI_PENDING_PHASES and proof == "none" and phase in CLI_PENDING_PHASES[code]:
        return f"{code}.pending"
    return None


def panel_log_command(texts: dict) -> str:
    """The panel log command as the product's own real-start text names it."""
    match = re.search(r"sudo journalctl -u celikpanel-panel\b[^.;]*?-n \d+", texts[f"{REAL_START_CODE}.pending"]["en"])
    if not match:
        raise ValueError("the product's panel_start_unverified text names no panel log command")
    return match.group(0)


def cli_text_observations(samples: list[dict], texts: dict) -> dict:
    """Per CLI sample: which reviewed text applied and whether EN and TR printed it verbatim.

    ``samples``: track status samples (``cli`` = guest cli-status output).
    ``texts``: parse_cli_guidance of the product source.
    """
    by_key: dict[str, dict] = {}
    mismatches = []
    command = panel_log_command(texts)
    command_seen = {"en": False, "tr": False}
    cause_lines = {"en": 0, "tr": 0}
    for sample in samples:
        cli = sample.get("cli") or {}
        try:
            status = json.loads(((cli.get("json") or {}).get("stdout")) or "null")
        except ValueError:
            status = None
        out = {lang: str((cli.get(lang) or {}).get("stdout") or "") for lang in ("en", "tr")}
        for lang in ("en", "tr"):
            command_seen[lang] = command_seen[lang] or command in out[lang]
            code = (status or {}).get("failure_code")
            if code and any(marker.format(code=code) in out[lang] for marker in texts["cause_markers"][lang]):
                cause_lines[lang] += 1
        key = cli_guidance_key(status)
        if key is None or key not in texts:
            continue                                    # no reviewed text applies (or this product has none)
        checked = [key]
        if key == "paused" and "paused_renewal" in texts:
            # upd5: the pause's renewal sentence follows the recorded renewal_before_update (off, else on).
            renewal = "off" if (status or {}).get("renewal_before_update") == "off" else "on"
            if renewal in texts["paused_renewal"]:
                checked.append(f"paused.renewal.{renewal}")
        for name in checked:
            entry = by_key.setdefault(name, {"samples": 0, "en": 0, "tr": 0, "first_utc": sample.get("utc")})
            entry["samples"] += 1
            entry["last_utc"] = sample.get("utc")
            for lang in ("en", "tr"):
                expected = (texts["paused_renewal"][name.rsplit(".", 1)[1]][lang] if name.startswith("paused.renewal.")
                            else cli_text(texts, name, lang, status))
                if expected in out[lang]:
                    entry[lang] += 1
                else:
                    mismatches.append({"utc": sample.get("utc"), "key": name, "language": lang,
                                       "printed_first_line": out[lang].split("\n", 1)[0][:400]})
    return {"by_key": by_key, "mismatches": mismatches, "panel_log_command": command,
            "panel_log_command_seen": command_seen, "recorded_cause_lines": cause_lines}


def completion_marker_seen(events: list[dict] | None) -> bool | None:
    """Did the owner-update observer ever see completion.pending? None when its timeline is unavailable."""
    timeline = [e for e in (events or []) if e.get("event") == "timeline"]
    if not timeline:
        return None
    return any(e.get("transaction_phase") == "completion.pending" for e in timeline)


def view_reachability(samples: list[dict]) -> dict:
    """Which owner views answered, over the whole track and in its last sample."""
    def views(sample):
        return {"panel_update_status": (sample.get("update_status") or {}).get("http") == 200,
                "panel_recovery_status": (sample.get("recovery_api") or {}).get("http") == 200,
                "root_cli": bool(sample.get("cli")) and "cli_error" not in sample,
                "offline_page_served": (sample.get("shell_fetch") or {}).get("http") == 200
                if "shell_fetch" in sample else "panel-reachable"}
    per = [views(s) for s in samples]
    summary = {name: {"reachable": sum(1 for v in per if v[name] is True), "samples": len(per)}
               for name in ("panel_update_status", "panel_recovery_status", "root_cli")}
    return {"summary": summary, "last": per[-1] if per else None,
            "ssh_owner_view": "not attempted (recovery view needs an interactive SSH terminal and a one-time code)"}


def _rule(findings: list, unknown: list, expected: list, name: str, value: bool | None, finding: str) -> None:
    if value is None:
        unknown.append(name)
    elif value:
        expected.append(name)
    else:
        findings.append(finding)


# upd4 F4/F5: a start kind whose candidate never ran was not measured; that is not a finding about the kind.
KIND_NOT_MEASURED = "not-measured"
KIND_NOT_REACHED_RULE = "kind-not-reached"
OVERALL_KIND_NOT_REACHED = "inconclusive-kind-not-reached"
KIND_CODES = {"start-check": START_CHECK_CODE, "real-start": REAL_START_CODE}
KIND_MOMENT = {"start-check": "the candidate Panel's read-only start check",
               "real-start": "the candidate Panel's real start"}


def kind_not_reached(kind: str, obs: dict) -> dict | None:
    """Did the update stop before the cell's candidate kind could act (upd4 F4, F5)? Positive evidence only.

    The kind was not reached when every one of these holds:

    * the update's failure line was read and names another code (never the kind's or the start check's), the
      journal holds no start-check reason, and no failure sidecar names a start kind - so the candidate Panel
      never ran its start check (start-check) or its real start (real-start);
    * the observer saw no ``completion.pending`` (``False``, not unknown);
    * the previous release is installed and every automatic dispatch (if any) is a rollback;
    * and the request ended in one of the two shapes the product gives such a stop:
      ``stopped-before-change`` (no terminal state; the H8 rule stopped the track on an unchanged
      ``failed``/``none`` status without a start-kind code, upd4 F4) or ``rolled-back-before-candidate``
      (``recovered``/``rollback_verified`` without a start-kind code, upd4 F5).

    Anything missing or contradicting leaves the kind judged normally (findings or unknown), so this can only
    turn an unmeasured cell into "not measured", never hide a measured misbehaviour.
    """
    code = KIND_CODES.get(kind)
    if code is None:
        return None
    start_codes = (START_CHECK_CODE, REAL_START_CODE)
    codes = obs.get("update_failure_codes")
    sidecar = obs.get("sidecar") or {}
    dispatches = obs.get("receipt_dispatches")
    if (not codes or any(c in start_codes for c in codes) or obs.get("check_reasons") is None
            or obs.get("check_reasons") or sidecar.get("code") in start_codes
            or obs.get("completion_marker_seen") is not False or obs.get("installed") != "baseline"
            or (dispatches and directions_rule(dispatches, "rollback") is not True)):
        return None
    final, stop = obs.get("final"), obs.get("track_stop") or {}
    status = stop.get("status") or {}
    if (final is None and stop.get("rule") == SETTLED_FAILED_RULE and status.get("phase") == "failed"
            and status.get("terminal_proof") == "none" and status.get("failure_code") not in start_codes):
        shape = STOPPED_BEFORE_CHANGE
        how = (f"the update failed before changing the installed version and stayed {status.get('phase')}/"
               f"{status.get('terminal_proof')} ({SETTLED_FAILED_RULE} after {stop.get('seconds')} s)")
    elif (classify_status(final) == "stopped"
          and stop_line_confirms(final, obs.get("update_failure_lines")) is True
          and sidecar.get("code") in (None, final.get("failure_code"))):
        # upd5: the product's own typed final stop (failed/none, update_failed, a preflight stop code) confirmed by
        # the updater's failure line (same code, state=unchanged); a sidecar, when read, names the same code.
        shape = STOPPED_BEFORE_CHANGE
        line = next(line for line in obs["update_failure_lines"] if line.get("code") == final.get("failure_code"))
        typed = "".join(f" {k}={line[k]}" for k in ("step", "class") if line.get(k))
        how = (f"the update stopped before changing the installed version: typed final stop "
               f"failure_code={final.get('failure_code')}{typed}, state=unchanged")
    elif (isinstance(final, dict) and (final.get("phase"), final.get("terminal_proof"))
          == ("recovered", "rollback_verified") and final.get("failure_code") not in start_codes):
        shape = "rolled-back-before-candidate"
        how = (f"the update failed and was rolled back ({len(dispatches or [])} rollback dispatch(es)) before the "
               "candidate ran")
    else:
        return None
    lines = obs.get("update_failure_lines") or [{"code": c} for c in codes]
    causes = "; ".join(f"code={line.get('code')} state={line.get('state')} reason={line.get('reason')}"
                       for line in lines)
    return {"shape": shape, "code_expected": code,
            "reason": f"{kind} not measured: {how}; {KIND_MOMENT[kind]} never ran (no {code}, no start-check "
                      f"reason, no completion.pending, previous release installed). Update failure line: {causes}",
            "evidence": {"update_failure_codes": codes, "completion_marker_seen": False, "installed": "baseline",
                         "receipt_dispatches": dispatches, "final": final, "track_stop": stop or None}}


def not_measured(kind: str, reached: dict) -> dict:
    """The kind judge's answer for a cell whose kind was not reached: no rule is judged, nothing is a finding."""
    return {"kind": kind, "verdict": KIND_NOT_MEASURED, "reason": reached["reason"], "not_reached": reached,
            "expected": [], "findings": [], "unknown": [], "native_evidence": False}


def judge_start_check(obs: dict) -> dict:
    """start-check: rollback expected; completion.pending must never exist.

    Expected observation: the update fails in ``active`` after the database
    publication with ``candidate_panel_startup_check_failed`` (failure line and
    sidecar), the check's reason is the fixture's (``tls_pair_invalid``), no
    completion.pending, every automatic dispatch is ``phase=active`` (rollback),
    the final state is recovered/rollback_verified with that code, the old
    release runs, the database equals the pre-update digests (listed
    exclusions) and the CLI prints the product's "returned" text in EN and TR.
    An update that stopped before the start check ran is ``not-measured`` (``kind_not_reached``).
    """
    stopped_before = kind_not_reached("start-check", obs)
    if stopped_before:
        return not_measured("start-check", stopped_before)
    findings: list[str] = []
    unknown: list[str] = []
    expected: list[str] = []
    final = obs.get("final") or {}
    _rule(findings, unknown, expected, "rolled-back",
          None if not final else (final.get("phase"), final.get("terminal_proof")) == ("recovered", "rollback_verified"),
          f"not returned to the previous version: final {final.get('phase')}/{final.get('terminal_proof')}")
    _rule(findings, unknown, expected, "status-failure-code",
          None if not final else final.get("failure_code") == START_CHECK_CODE
          and final.get("previous_failure") == "update_failed",
          f"final status names failure_code={final.get('failure_code')!r} previous_failure="
          f"{final.get('previous_failure')!r}, not update_failed/{START_CHECK_CODE}")
    codes = obs.get("update_failure_codes")
    _rule(findings, unknown, expected, "update-failure-line", None if codes is None else START_CHECK_CODE in codes,
          f"the update's failure line does not carry {START_CHECK_CODE}: {codes}")
    reasons = obs.get("check_reasons")
    _rule(findings, unknown, expected, "fixture-reason",
          None if not reasons else all(r == START_CHECK_FIXTURE_REASON for r in reasons),
          f"the start check refused for {reasons}, not only the fixture's {START_CHECK_FIXTURE_REASON} "
          "(a good candidate could fail the same way)")
    sidecar = obs.get("sidecar") or {}
    _rule(findings, unknown, expected, "sidecar", None if sidecar.get("present") is None
          else sidecar.get("valid") and sidecar.get("code") == START_CHECK_CODE,
          f"failure sidecar: {sidecar}")
    marker = obs.get("completion_marker_seen")
    _rule(findings, unknown, expected, "no-completion-marker", None if marker is None else not marker,
          "completion.pending was created although the start check failed")
    dispatches = obs.get("receipt_dispatches")
    _rule(findings, unknown, expected, "rollback-dispatch", directions_rule(dispatches, "rollback"),
          f"automatic dispatches {_dispatch_text(dispatches)}: a forward attempt (operation=update phase=completion) "
          "in a start-check cell; expected only rollback dispatches (operation=rollback in any phase, or "
          "operation=update phase=active)")
    _rule(findings, unknown, expected, "update-card", _card_rule(obs.get("update_card")),
          f"update card: {(obs.get('update_card') or {}).get('findings')}")
    _rule(findings, unknown, expected, "old-release-running", None if obs.get("installed") is None
          else obs["installed"] == "baseline", f"installed release after recovery: {obs.get('installed')}")
    _rule(findings, unknown, expected, "database-equal", None if obs.get("database") is None
          else obs["database"] in ("equal", "equal-except-volatile"),
          f"database differs from the pre-update digests: {obs.get('database')}")
    cli = (obs.get("cli") or {}).get("by_key", {}).get(f"{START_CHECK_CODE}.recovered")
    _rule(findings, unknown, expected, "cli-returned-text", None if obs.get("cli") is None
          else bool(cli) and cli["en"] > 0 and cli["tr"] > 0,
          "the root CLI never printed the product's 'returned to the previous version' text in EN and TR")
    web = obs.get("web_keys_missing")
    _rule(findings, unknown, expected, "web-catalogue", None if web is None else not web,
          f"recovery screen keys missing from the product catalogue: {web}")
    return _judged("start-check", expected, findings, unknown)


def judge_real_start(obs: dict) -> dict:
    """real-start: the check passes, completion.pending exists, the stability wait fails, forward completion is
    retried to its limit and pauses. Rollback must NOT happen; the owner retry is not run by the harness.
    An update that stopped (or was rolled back) before the candidate's real start is ``not-measured``
    (``kind_not_reached``, upd4 F4/F5), not a finding about the kind.
    """
    stopped_before = kind_not_reached("real-start", obs)
    if stopped_before:
        return not_measured("real-start", stopped_before)
    findings: list[str] = []
    unknown: list[str] = []
    expected: list[str] = []
    final = obs.get("final") or {}
    codes = obs.get("update_failure_codes")
    _rule(findings, unknown, expected, "check-passed", None if codes is None else START_CHECK_CODE not in codes
          and not obs.get("check_reasons"),
          f"the start check refused the real-start candidate ({obs.get('check_reasons')}); the cell measured the "
          "check, not the real start")
    _rule(findings, unknown, expected, "update-failure-line", None if codes is None else REAL_START_CODE in codes,
          f"the update's failure line does not carry {REAL_START_CODE}: {codes}")
    sidecar = obs.get("sidecar") or {}
    _rule(findings, unknown, expected, "sidecar", None if sidecar.get("present") is None
          else sidecar.get("valid") and sidecar.get("code") == REAL_START_CODE, f"failure sidecar: {sidecar}")
    marker = obs.get("completion_marker_seen")
    _rule(findings, unknown, expected, "completion-marker", marker, "completion.pending was never observed")
    dispatches = obs.get("receipt_dispatches")
    _rule(findings, unknown, expected, "forward-dispatch", directions_rule(dispatches, "forward"),
          f"automatic dispatches {_dispatch_text(dispatches)}, expected only completion (forward)")
    _rule(findings, unknown, expected, "paused", None if not final else final.get("phase") == "recovery_required"
          and final.get("automatic_recovery") == "paused_retry_limit",
          f"forward completion did not pause at its limit: final {final.get('phase')}/"
          f"{final.get('automatic_recovery')}")
    _rule(findings, unknown, expected, "no-rollback", None if not final else final.get("phase") != "recovered"
          and obs.get("installed") != "baseline", "the server was returned to the previous version")
    _rule(findings, unknown, expected, "candidate-installed", None if obs.get("installed") is None
          else obs["installed"] == "candidate", f"installed release: {obs.get('installed')}")
    _rule(findings, unknown, expected, "owner-retry-not-run", not obs.get("owner_retry_run"),
          "the owner retry was run in a real-start cell")
    _rule(findings, unknown, expected, "retry-command-printed", None if obs.get("printed_retry_command") is None
          else bool(obs["printed_retry_command"]), "the recovery journal printed no one-time retry command")
    cli = obs.get("cli")
    pending = (cli or {}).get("by_key", {}).get(f"{REAL_START_CODE}.pending")
    _rule(findings, unknown, expected, "cli-real-start-text", None if cli is None
          else bool(pending) and pending["en"] > 0 and pending["tr"] > 0,
          "the root CLI never printed the product's panel_start_unverified text (panel log command, no supported "
          "return) in EN and TR; it may be shown only while previous_failure=update_failed and the phase is "
          "failed/recovering")
    _rule(findings, unknown, expected, "cli-panel-log-command", None if cli is None
          else all(cli["panel_log_command_seen"].values()),
          f"no CLI sample named the panel log command in both languages: {(cli or {}).get('panel_log_command_seen')}")
    paused = (cli or {}).get("by_key", {}).get("paused")
    _rule(findings, unknown, expected, "cli-paused-text", None if cli is None
          else bool(paused) and paused["en"] > 0 and paused["tr"] > 0,
          "the root CLI did not print the product's paused text in EN and TR")
    web = obs.get("web_keys_missing")
    _rule(findings, unknown, expected, "web-catalogue", None if web is None else not web,
          f"recovery screen keys missing from the product catalogue: {web}")
    workloads = obs.get("workloads")
    if workloads is None:
        unknown.append("workloads-kept-running")
    else:
        broken = sorted(k for k, v in workloads.items() if v == "interrupted")
        _rule(findings, unknown, expected, "workloads-kept-running", not broken,
              f"workloads interrupted while the Panel could not come up: {broken}")
    panel = obs.get("panel_verdict")
    _rule(findings, unknown, expected, "panel-down-until-end", None if panel is None else panel == PANEL_UNTIL_END,
          f"Panel window {panel!r}, expected {PANEL_UNTIL_END} for this fixture")
    return _judged("real-start", expected, findings, unknown)


# ---------------------------------------------------------------------------
# upd4: owner-continuation (port hold) and management-off reboot (pure rules; covered offline)
# ---------------------------------------------------------------------------

PORT_HOLD_UNIT_PREFIX = "celikpanel-lab-owner-port-hold-"
PORT_HOLD_EVENT_SCHEMA = "celikpanel/upd1-owner-port-hold/v1"
PORT_HOLD_SOURCES = {"update": "update.sh", "timer": "deploy/systemd/celikpanel-release-recovery.timer",
                     "runner": "deploy/release-recovery-runner.sh"}
# Allowances around the product's own constants (each at least twice what upd3 measured natively:
# 30 s of update work before the real start, 8 s of work per forward attempt, a track poll of <= 15 s
# plus one CLI sample, and the owner-continuation reads before the release).
PORT_HOLD_ALLOWANCES = {"start_work_s": 120, "attempt_work_s": 60, "owner_detection_s": 120, "owner_step_s": 300}
# upd3 native reference (real-start cells, same stability wait, timer and budget): from the old Panel's stop
# (when the hold binds) to paused_retry_limit.
PORT_HOLD_MEASURED = {"debian13": {"panel_down": "19:57:31", "pause": "20:04:02", "seconds": 391},
                      "arch": {"panel_down": "20:44:27", "pause": "20:50:50", "seconds": 383}}
PORT_CONFLICT_RE = re.compile(r"address already in use|bind: |listen tcp", re.I)


def systemd_seconds(value: str) -> float:
    """systemd.time spans used by the recovery timer (``30s``, ``1min 30s``, ``1s``, ``500ms``, bare seconds)."""
    text = str(value or "").strip()
    if re.fullmatch(r"\d+", text):
        return float(text)
    total, matched = 0.0, ""
    units = {"ms": 0.001, "s": 1, "sec": 1, "m": 60, "min": 60, "h": 3600, "hr": 3600}
    for number, unit in re.findall(r"(\d+)\s*(ms|sec|min|hr|s|m|h)\b", text):
        total += int(number) * units[unit]
        matched += number + unit
    if not matched or re.sub(r"\s+", "", text) != matched:
        raise ValueError(f"unsupported systemd time span {value!r}")
    return total


def parse_hold_sources(update_sh: str, timer: str, runner: str) -> dict:
    """The product constants the port hold must outlast, read from the build's own files."""
    wait = re.search(r"^PANEL_START_WAIT_SECONDS=(\d+)$", update_sh, re.M)
    inactive = re.search(r"^OnUnitInactiveSec=(.+)$", timer, re.M)
    accuracy = re.search(r"^AccuracySec=(.+)$", timer, re.M)
    budget = re.search(r"OWNER_RETRY_SNAPSHOT && \$count == (\d+) \]\]", runner)
    if not wait or not inactive or not budget:
        raise ValueError("the stability wait, recovery timer or retry budget is not where the reviewed product has it")
    return {"panel_start_wait_s": int(wait.group(1)), "timer_inactive_s": systemd_seconds(inactive.group(1)),
            "timer_accuracy_s": systemd_seconds(accuracy.group(1)) if accuracy else 60.0,
            "retry_budget": int(budget.group(1))}


def port_hold_bound(constants: dict, allowances: dict | None = None) -> dict:
    """How long the owner-continuation hold may last (hold_seconds) and the unit's RuntimeMaxSec.

    Forward completion reaches the pause after the update's own real start
    (start work + one stability wait) and ``retry_budget`` forward attempts,
    each one stability wait + attempt work + the recovery timer's
    OnUnitInactiveSec + AccuracySec; the runner pauses on the next tick after
    the last attempt. The hold must then survive the driver's detection of the
    pause and the owner-continuation reads before the owner releases it.
    """
    a = dict(PORT_HOLD_ALLOWANCES, **(allowances or {}))
    per_attempt = (constants["panel_start_wait_s"] + a["attempt_work_s"] + constants["timer_inactive_s"]
                   + constants["timer_accuracy_s"])
    pause_worst = a["start_work_s"] + constants["panel_start_wait_s"] + constants["retry_budget"] * per_attempt
    hold = pause_worst + a["owner_detection_s"] + a["owner_step_s"]
    hold_seconds = int(-(-hold // 60) * 60)
    measured = max(v["seconds"] for v in PORT_HOLD_MEASURED.values())
    return {"constants": constants, "allowances": a, "per_attempt_s": per_attempt, "pause_worst_s": pause_worst,
            "hold_seconds": hold_seconds, "runtime_max_seconds": hold_seconds + 60,
            "measured_pause_s": {k: v["seconds"] for k, v in PORT_HOLD_MEASURED.items()},
            "margin_over_measured_s": hold_seconds - measured,
            "formula": "hold = start_work + wait + budget*(wait + attempt_work + OnUnitInactiveSec + AccuracySec) "
                       "+ owner_detection + owner_step, rounded up to a minute; RuntimeMaxSec = hold + 60"}


def plan_port_hold(root: Path | None = None) -> dict:
    """The bound as the plan shows it, from the driver's own checkout (a run reads the candidate commit's files)."""
    root = root or HERE.parents[2]
    try:
        texts = {name: (root / path).read_text(encoding="utf-8") for name, path in PORT_HOLD_SOURCES.items()}
        bound = port_hold_bound(parse_hold_sources(texts["update"], texts["timer"], texts["runner"]))
    except (OSError, ValueError) as exc:
        return {"unavailable": f"{type(exc).__name__}: {exc}"}
    return dict(bound, source="driver checkout (plan only; a run reads update.sh, the recovery timer and the runner "
                              "of the candidate commit)")


def port_hold_state(events: list[dict] | None) -> dict:
    """What the hold's own event stream says (held, phases seen, released and why)."""
    events = events or []
    kinds = [e.get("event") for e in events]
    held = next((e for e in events if e.get("event") == "port_held"), None)
    released = next((e for e in events if e.get("event") == "released"), None)
    return {"armed": "armed" in kinds, "held": held is not None, "held_at": (held or {}).get("at"),
            "phases_seen": [e.get("phase") for e in events if e.get("event") in ("port_held", "phase_seen")],
            "released": released is not None, "release_reason": (released or {}).get("reason"),
            "released_at": (released or {}).get("at"), "held_seconds": (released or {}).get("held_seconds")}


INSPECTION_POINTS = ("before-site", "after-setup", "after-seed", "before-check", "after-track", "at-pause",
                     "after-continuation", "after-terminal", "after-reboot", "after-management-return")


def inspection_allowed(state: dict, label: str) -> tuple[bool, str]:
    """Sidecar v2 rule, now the driver's: a read-only inspection never overlaps the update's preflight.

    Allowed before the update check (``check_at`` unset), or while no update or
    owner retry awaits its terminal state (``awaiting_terminal`` false: the track
    ended at a terminal, paused or settled state). Never at the old ``pre-update``
    point, which fell inside the preflight window in upd3 cell 1.
    """
    if label not in INSPECTION_POINTS:
        return False, f"{label!r} is not a scheduled inspection point"
    if not state.get("check_at"):
        return True, "before the update check"
    if state.get("awaiting_terminal"):
        return False, "between the update check (or the owner's retry) and a terminal, paused or settled state"
    return True, "after a terminal, paused or settled state"


MANAGEMENT_UNITS = ("celikpanel-panel.service", "celikpanel-agent.service")
MANAGEMENT_OFF_MEASURE_SECONDS = 180.0
# H12: how long management-return waits (read-only) for panel_state=ready before comparing the owner state.
MANAGEMENT_RETURN_READY_SECONDS = 180.0
MANAGEMENT_RETURN_READY_POLL_S = 3.0
MANAGEMENT_OFF_WORKLOADS = ("web", "smtp", "db")
RENEWAL_TIMER_PATTERNS = ("certbot", "renew", "acme")


def management_state(services: dict | None) -> dict:
    units = {unit: {k: ((services or {}).get(unit) or {}).get(k) for k in ("ActiveState", "UnitFileState")}
             for unit in MANAGEMENT_UNITS}
    off = all(v["ActiveState"] in ("inactive", "failed") and v["UnitFileState"] == "disabled" for v in units.values())
    on = all(v["ActiveState"] == "active" and v["UnitFileState"] == "enabled" for v in units.values())
    return {"units": units, "disabled_and_stopped": off, "enabled_and_active": on}


def boot_window(samples: Iterable[dict], boot_id: str | None) -> list[dict]:
    return sorted((s for s in samples if boot_id and s.get("boot_id") == boot_id and "t" in s), key=lambda s: s["t"])


def window_seconds(window: list[dict]) -> float:
    if len(window) < 2:
        return 0.0
    return round(float(window[-1].get("monotonic", window[-1]["t"])) - float(window[0].get("monotonic", window[0]["t"])), 1)


def served_after_boot(window: list[dict], key: str) -> dict:
    """``served``: the workload answered after the boot and never failed again in the window."""
    measured = [s for s in window if isinstance(s.get(key), dict) and s[key].get("ok") is not None]
    if not measured:
        return {"verdict": "not-measured", "samples": 0}
    oks = [index for index, s in enumerate(measured) if s[key]["ok"]]
    if not oks:
        return {"verdict": "never-served", "samples": len(measured)}
    first = oks[0]
    failing_after = sum(1 for s in measured[first:] if not s[key]["ok"])
    return {"verdict": "served" if failing_after == 0 else "interrupted", "samples": len(measured),
            "first_ok_seconds_after_boot": measured[first].get("monotonic"), "failing_before_first_ok": first,
            "failing_after_first_ok": failing_after}


# H20 (upd11): the 180 s window starts with the sampler, which runs about 13 s after the boot; on a slow guest boot the
# owner's cron daemon starts much later (Arch upd11: crond 85 s after the boot, first run 144 s after it), so the fixed
# window can end between its first and second in-boot run. The measurement then continues, read-only, for at most one
# cron period limit more; a cron that does not advance still fails, and the extension is recorded.
CRON_LATE_START_EXTENSION_S = 130.0


def management_off_window_complete(window: list[dict], cron_seeded: bool) -> bool:
    """The management-off window may be judged: 180 s of new-boot samples, and either cron is not seeded, its
    in-boot stamps already pass, or the bounded H20 extension is used up."""
    seconds = window_seconds(window)
    if seconds < MANAGEMENT_OFF_MEASURE_SECONDS:
        return False
    if not cron_seeded or cron_after_boot(window)["verdict"] == "served":
        return True
    return seconds >= MANAGEMENT_OFF_MEASURE_SECONDS + CRON_LATE_START_EXTENSION_S


def cron_after_boot(window: list[dict], period_limit: float = 130.0) -> dict:
    """The owner's cron job ran in this boot: at least two new stamps, none more than ``period_limit`` apart."""
    if not window:
        return {"verdict": "not-measured"}
    started = float(window[0]["t"]) - 5.0
    stamps = sorted({float(s["cron"]["mtime"]) for s in window if isinstance(s.get("cron"), dict)
                     and s["cron"].get("ok") and s["cron"].get("mtime") is not None
                     and float(s["cron"]["mtime"]) >= started})
    stalls = [w for w in cron_windows(window, period_limit) if (w.get("from") or 0) >= started]
    verdict = "served" if len(stamps) >= 2 and not stalls else "not-advancing"
    return {"verdict": verdict, "stamps_in_boot": len(stamps), "stalls": stalls}


def renewal_timer_verdict(before: dict | None, after: dict | None) -> dict:
    """The certificate renewal timer keeps its state across management-off and the reboot."""
    names = sorted(n for n in set(before or {}) | set(after or {}) if any(p in n for p in RENEWAL_TIMER_PATTERNS))
    if not names:
        return {"verdict": "not-present", "timers": {}}
    per, ok = {}, True
    for name in names:
        a = {k: ((before or {}).get(name) or {}).get(k) for k in ("UnitFileState", "ActiveState")}
        b = {k: ((after or {}).get(name) or {}).get(k) for k in ("UnitFileState", "ActiveState")}
        good = a["UnitFileState"] == b["UnitFileState"] and (a["ActiveState"] != "active" or b["ActiveState"] == "active")
        ok = ok and good
        per[name] = {"before": a, "after": b, "kept": good}
    enabled = any(v["after"]["UnitFileState"] == "enabled" and v["after"]["ActiveState"] == "active"
                  for v in per.values())
    return {"verdict": "as-before" if ok else "changed", "enabled_and_active": enabled, "timers": per}


def truth_differences(before: dict | None, after: dict | None) -> list[str]:
    """Owner-visible Panel state that differs after management returned (volatile fields are not kept)."""
    before, after = before or {}, after or {}
    return [f"{key}: {before.get(key)!r} -> {after.get(key)!r}" for key in sorted(set(before) | set(after))
            if before.get(key) != after.get(key)]


def judge_owner_continuation(obs: dict) -> dict:
    """owner-continuation: forward completion exhausts its budget on the held port, the owner releases the port
    and runs the printed retry once, and the same request completes forward (recovered-after-owner-continuation).
    """
    findings: list[str] = []
    unknown: list[str] = []
    expected: list[str] = []
    final, paused, hold = obs.get("final") or {}, obs.get("paused") or {}, obs.get("hold") or {}
    codes = obs.get("update_failure_codes")
    _rule(findings, unknown, expected, "update-failure-line", None if codes is None else REAL_START_CODE in codes,
          f"the update's failure line does not carry {REAL_START_CODE}: {codes}")
    _rule(findings, unknown, expected, "hold-through-pause", None if not hold else bool(hold.get("held_at_pause")),
          f"the port hold did not last until the pause: {hold.get('release_reason')!r} (the cause was gone before "
          "the owner could act; the cell did not measure the owner path)")
    _rule(findings, unknown, expected, "paused-on-cause", bool(paused) and paused.get("automatic_recovery")
          == "paused_retry_limit" and paused.get("first_failure_code") == REAL_START_CODE,
          f"forward completion did not pause on {REAL_START_CODE}: {paused or 'never paused'}")
    dispatches = obs.get("receipt_dispatches")
    _rule(findings, unknown, expected, "forward-dispatch-budget", None if not dispatches
          else directions_rule(dispatches, "forward") is True and len(dispatches) == 3,
          f"automatic dispatches {_dispatch_text(dispatches)}, expected three forward attempts")
    log = obs.get("panel_log")
    _rule(findings, unknown, expected, "panel-log-names-cause", None if log is None else bool(log.get("names_cause")),
          "the panel log the product tells the owner to read does not show the port conflict")
    _rule(findings, unknown, expected, "retry-command-printed", None if obs.get("printed_retry_command") is None
          else bool(obs["printed_retry_command"]), "the recovery journal printed no one-time retry command")
    _rule(findings, unknown, expected, "port-released-by-owner", None if not hold
          else hold.get("release_reason") == "owner-released" and bool(hold.get("released_before_retry")),
          f"the port was not released by the owner before the retry: {hold.get('release_reason')!r}")
    retry = obs.get("owner_retry") or {}
    owner_count = obs.get("owner_receipts")
    _rule(findings, unknown, expected, "owner-retry-once", None if not retry
          else retry.get("action") == "executed-once" and owner_count in (None, 1),
          f"the printed retry was not run exactly once: action={retry.get('action')!r} owner receipts={owner_count}")
    _rule(findings, unknown, expected, "completed-forward", None if not final
          else (final.get("phase"), final.get("terminal_proof")) == ("succeeded", "update_verified"),
          f"the same request did not complete forward: final {final.get('phase')}/{final.get('terminal_proof')}")
    _rule(findings, unknown, expected, "outcome-class", obs.get("outcome") == "recovered-after-owner-continuation",
          f"outcome {obs.get('outcome')!r}, expected recovered-after-owner-continuation")
    _rule(findings, unknown, expected, "candidate-installed", None if obs.get("installed") is None
          else obs["installed"] == "candidate", f"installed release: {obs.get('installed')}")
    timers = obs.get("timers")
    _rule(findings, unknown, expected, "timers-restored", None if timers is None else bool(timers.get("equal")),
          f"timers did not return to their pre-update state: {sorted((timers or {}).get('changed') or {})}")
    workloads = obs.get("workloads")
    if workloads is None:
        unknown.append("workloads-kept-running")
    else:
        broken = sorted(k for k, v in workloads.items() if v == "interrupted")
        _rule(findings, unknown, expected, "workloads-kept-running", not broken,
              f"workloads interrupted while the owner path ran: {broken}")
    panel = obs.get("panel_verdict")
    _rule(findings, unknown, expected, "panel-back", None if panel is None and obs.get("login_ok") is None
          else panel == "down-only-during-transaction" and bool(obs.get("login_ok")),
          f"the Panel did not come back only after the operation: window {panel!r}, login {obs.get('login_ok')}")
    cli = obs.get("cli")
    paused_text = (cli or {}).get("by_key", {}).get("paused")
    _rule(findings, unknown, expected, "cli-paused-text", None if cli is None
          else bool(paused_text) and paused_text["en"] > 0 and paused_text["tr"] > 0,
          "the root CLI did not print the product's paused text in EN and TR")
    web = obs.get("web_keys_missing")
    _rule(findings, unknown, expected, "web-catalogue", None if web is None else not web,
          f"recovery screen keys missing from the product catalogue: {web}")
    _rule(findings, unknown, expected, "update-card", _card_rule(obs.get("update_card")),
          f"update card: {(obs.get('update_card') or {}).get('findings')}")
    return _judged("owner-continuation", expected, findings, unknown)


def judge_management_off(obs: dict) -> dict:
    """mgmt-off-reboot: with the Panel and Agent disabled and stopped, after one orderly reboot, every workload is
    served natively for the measured window; management then returns to the same owner-visible state."""
    findings: list[str] = []
    unknown: list[str] = []
    expected: list[str] = []
    final = obs.get("final") or {}
    _rule(findings, unknown, expected, "update-verified", None if not final
          else (final.get("phase"), final.get("terminal_proof")) == ("succeeded", "update_verified"),
          f"the good update was not verified first: {final.get('phase')}/{final.get('terminal_proof')}")
    management = obs.get("management_after_reboot")
    _rule(findings, unknown, expected, "management-off", None if management is None
          else bool(management.get("disabled_and_stopped")),
          f"the Panel/Agent were not disabled and stopped after the reboot: {(management or {}).get('units')}")
    _rule(findings, unknown, expected, "new-boot", obs.get("new_boot"), "the guest did not boot again")
    seconds = obs.get("window_seconds")
    _rule(findings, unknown, expected, "measured-window", None if seconds is None
          else seconds >= MANAGEMENT_OFF_MEASURE_SECONDS,
          f"the management-off window lasted {seconds} s, fewer than {MANAGEMENT_OFF_MEASURE_SECONDS:.0f} s")
    served = obs.get("served") or {}
    for key, value in sorted(served.items()):
        verdict = (value or {}).get("verdict")
        if verdict in ("not-seeded", "not-measured-by-design"):
            continue
        _rule(findings, unknown, expected, f"{key}-served-without-management",
              None if verdict in (None, "not-measured") else verdict == "served",
              f"{key} with management off: {verdict} ({value})")
    renewal = obs.get("renewal_timer")
    _rule(findings, unknown, expected, "renewal-timer-kept", None if renewal is None
          else renewal.get("verdict") in ("as-before",), f"certificate renewal timer: {renewal}")
    firewall = obs.get("firewall")
    _rule(findings, unknown, expected, "firewall-present", None if firewall is None
          else bool(firewall.get("present")) and bool(firewall.get("equal_to_before")),
          f"firewall ruleset with management off: {firewall}")
    back = obs.get("management_return")
    # H12: an owner state that was never compared (the Panel did not report ready in time) is unknown.
    _rule(findings, unknown, expected, "management-returned", None if back is None
          or (back.get("login_ok") and "owner_state" in back)
          else bool(back.get("login_ok")) and not back.get("differences"),
          f"after management returned: {back}")
    return _judged("mgmt-off-reboot", expected, findings, unknown)


def _dispatch_text(dispatches: list[dict] | None) -> list[str]:
    return [f"{d.get('operation')}/{d.get('phase')}" for d in dispatches or []]


def _card_rule(judged: dict | None) -> bool | None:
    if not isinstance(judged, dict) or judged.get("verdict") in (None, "unknown"):
        return None
    return judged["verdict"] == "as-expected"


def _judged(kind: str, expected: list, findings: list, unknown: list) -> dict:
    verdict = "finding" if findings else "inconclusive" if unknown else "as-expected"
    return {"kind": kind, "verdict": verdict, "expected": expected, "findings": findings, "unknown": unknown,
            "native_evidence": False}


JUDGES = {"start-check": judge_start_check, "real-start": judge_real_start,
          "owner-continuation": judge_owner_continuation, "mgmt-off-reboot": judge_management_off}


def expectation_step_verdict(judged: dict) -> str:
    return {"as-expected": "passed", "finding": "failed", "inconclusive": "inconclusive",
            KIND_NOT_MEASURED: "inconclusive"}[judged["verdict"]]


def installed_role(builds: dict, artifacts: dict, role: str) -> str | None:
    """Which release the installed agent and panel name (``baseline``, ``candidate`` or ``other``)."""
    identities = {(builds.get(n) or {}).get("identity") for n in ("agent", "panel")}
    if not identities or None in identities:
        return None
    for label, item in (("baseline", artifacts["baseline"]), ("candidate", artifacts[role])):
        if identities == {f"version={item['version']}\ncommit={item['commit']}\n"}:
            return label
    return "other"


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
            expected = overall([step["verdict"] for step in result.get("steps", [])],
                               kind_not_reached=((result.get("kind") or {}).get("judged") or {}).get("verdict")
                               == KIND_NOT_MEASURED)
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
    candidate = artifacts[candidate_role(cell)]
    fault = cell.recovery_fault
    real_start = cell.variant == "real-start"
    owner_path = cell.variant == "owner-continuation"
    management_off = cell.variant == "mgmt-off-reboot"
    hold = plan_port_hold() if owner_path else None
    arm_extra = ("; owner port hold guest_owner_port_hold.py armed for the same request (binds 127.0.0.1:2083 once "
                 "the updater has stopped the old Panel; kept through the update's exit and every forward attempt; "
                 f"bound {hold.get('hold_seconds')} s, RuntimeMaxSec {hold.get('runtime_max_seconds')} s)"
                 if owner_path else "")
    seed_extra = ("; mgmt-off: POST /api/v1/domains/{id}/databases {name, type:mysql, password} (owner database), then "
                  "one table with one row (the site marker) through the native client as the server owner"
                  if management_off else "")
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
                 "availability, then POST .../cron only when crontab exists; GET /api/v1/firewall" + seed_extra),
        ("pre-state", "guest_probe observation (hashes, DB digests, timers), workload snapshot, offline shell fetch, "
                      "sampler unit (5 s, survives reboot) + host loop (SSH, Panel via tunnel)"),
        ("arm", "read-only inspection before-check; origin proved again (127.0.0.1, HTTP 200); owner-update observer "
                "for the chosen request id: " + (
                    f"checkpoint mode, second fault {fault['action']} at {fault['checkpoint']}" if fault
                    else "watch mode, no fault") + arm_extra),
        ("owner-start", "GET /api/v1/panel/update/check, GET /api/v1/host-mutation-readiness, "
                        "POST /api/v1/panel/update/start {request_id, confirmed:true, current_version, current_commit, ...target} once"),
        ("track", "GET /api/v1/panel/update/status?request_id= with UI backoff 1.5 s x1.6 <= 15 s; while the Panel is down: "
                  "GET /api/v1/recovery/status?request_id= (same session), root CLI status --json/--lang en/--lang tr, "
                  "offline shell reference; three-source agreement per sample; stops inconclusive after "
                  f"{int(SETTLED_FAILED_SECONDS)} s of an unchanged failed/none status with no recovery activity "
                  f"({SETTLED_FAILED_RULE}, H8)"
                  + ("; QMP system_reset once at reboot_ready" if fault and fault["action"] == "reboot" else "")),
        ("owner-continuation (required)", (
            "the pause is this cell's expected end: verify all views and record the exact one-time retry command "
            "printed in the recovery journal (owner-retry without --execute); it is NOT run, because it would retry "
            "the same broken candidate" if real_start else
            "the pause is required: record every owner text (CLI EN/TR, card and recovery screen when reachable, the "
            "panel log the product names, timers, the hold's events), read the printed one-time retry, then do what "
            "the text tells the owner: release the port (systemctl stop of the hold unit, verified released) and run "
            "exactly the printed retry command once; then track the same request to its terminal state"
            if owner_path else
            "only if the product reports paused_retry_limit: verify all views, run the exact "
            "one-time retry command printed in the recovery journal, once")),
        ("terminal", (
            "candidate installed and not running (Panel down), completion marker present, seeded workloads (site "
            "marker, mailbox, cron), timers, firewall; Panel login attempted once and recorded as unreachable"
            if real_start else
            "installed/running identity, floor/foundation, DB digests vs pre-update, seeded rows, site marker, "
            "mailbox, cron, timers, firewall, Panel login and update card (rendered from the served build's "
            "systemUpdateOutcome.ts rules and catalogues, judged only on a real mismatch)")),
    ]
    if deferred_mail_watched(cell):
        steps.append(("deferred-mail-watch", (
            "upd12, read-only: the Panel journal every "
            f"{int(DEFERRED_MAIL_POLL_S)} s until the last Panel process's bounded startup mail retry resolved "
            f"(each deferred step completed or failed, or the give-up line) or {int(DEFERRED_MAIL_WATCH_S)} s; then "
            f"once more after {int(DEFERRED_MAIL_SETTLE_S)} s (nothing runs again); native mail file facts (size, "
            "mtime, SHA-256, postconf milters/SNI map) at the start and the end; the sampler keeps running")))
    if management_off:
        steps += [
            ("management-off", "owner Panel state read once; sudo systemctl disable --now celikpanel-panel.service "
                               "celikpanel-agent.service; both inactive and disabled"),
            ("owner-reboot", "sudo systemctl reboot (orderly, once); SSH back; a new boot id"),
            ("management-off-measure", f"at least {int(MANAGEMENT_OFF_MEASURE_SECONDS)} s of 5 s samples in the new "
                                       "boot with management off: site HTTP + marker, SMTP (Debian), cron stamps "
                                       "advancing, the owner's database row through the native client, certificate "
                                       "renewal timer state, firewall ruleset present and unchanged; anything that "
                                       "needed the Panel is recorded"),
            ("management-return", "sudo systemctl enable --now celikpanel-agent.service celikpanel-panel.service; the "
                                  "Panel TLS answers; fresh login; read-only wait (at most "
                                  f"{int(MANAGEMENT_RETURN_READY_SECONDS)} s) for panel_state=ready (H12); the same "
                                  "owner state (domains, cron, mailbox, database, version, update and recovery "
                                  "status) as before management-off"),
        ]
    steps += [
        ("collect", "always once the guest was prepared, also after an early stop: sampler and host samples, "
                    "journals (Panel/Agent/recovery, setup services, lab units incl. the fixture origin), "
                    "observation records, observer/recovery-fault events, budget receipts"
                    + (", the raw port-hold events file (H15)" if owner_path else "")),
        ("verdicts", "per-workload outage windows (DNS: " + dns_scope(dns_mode)["verdict"] + "), Panel window"
                     + (" (expected down from the update until the end; an update that stopped before the "
                        "candidate ran is judged as any update)" if real_start else "")
                     + (" (the update part only: samples up to management-off, requested-at instant minus one "
                        "sample interval, H13/H14)" if management_off else "")
                     + ", agreement, outcome classification"),
    ]
    pk_text = ("; upd9: every ~10 s PackageKit read from /proc (PK_PROBE: packagekitd, the mapped pathnames under "
               "packagekit-backend (upd10), its children, /proc/locks lines of the apt/dpkg locks) bracketing "
               "GET /api/v1/host-mutation-readiness")
    if cell.scenario == "setup-once":
        names = [n for n, _ in steps]
        steps = steps[:names.index("setup") + 1]
        steps[-1] = ("setup", steps[-1][1] + "; started ONCE by an ordinary owner (no H19 wait or retry)" + pk_text)
        steps += [("packagekit-after-setup", "read-only: PackageKit and the readiness answer every 20 s until "
                                             "packagekitd exits (<= 420 s), then once more"),
                  ("collect", "journals, observation records (no sampler in this cell)")]
    elif cell.scenario == "busy-start":
        names = [n for n, _ in steps]
        steps.insert(names.index("pre-state") + 1, (
            "busy-start", f"owner's package task {BUSY_OP_UNIT}: apt-get install --download-only of one package not "
                          f"installed, Dl-Limit for ~{BUSY_OP_TARGET_S} s, then apt-get update; while it holds the apt "
                          "lock: readiness, GET update/check, POST update/start once (refusal expected), status, panel "
                          "log, version and unit PIDs before/after; then wait for the task to end and confirm "
                          "packagekitd still runs; arm follows without the H19 wait, and owner-start first POSTs the "
                          "armed request at once while packagekitd idles (no readiness wait); if that is refused, the "
                          "owner waits for readiness, arms a new request and starts it once"))
    if cell.variant in KIND_EXPECTED:
        steps.append(("kind-expectation", "judge the " + cell.variant + " observations: " + KIND_EXPECTED[cell.variant]
                      + " (texts from the product build's web/src catalogues and cmd/recovery/main.go, CLI output "
                        "verbatim)"
                      + (f"; an update that stops before {KIND_MOMENT[cell.variant]} runs is {KIND_NOT_MEASURED} "
                         f"and the cell ends {OVERALL_KIND_NOT_REACHED}" if cell.variant in KIND_CODES else "")))
    plan = {"schema": "celikpanel/upd1-plan/v1", "cell": dataclasses.asdict(cell), "work_root": work_root,
            "dns": dns_scope(dns_mode),
            "local_port": local_port, "baseline": {k: artifacts["baseline"][k] for k in ("version", "commit", "sha256")},
            "candidate": {k: candidate[k] for k in ("version", "commit", "sha256")},
            "expected_outcome": KIND_EXPECTED.get(cell.variant) or (
                "recovered (rollback_verified, previous_failure=update_failed), automatically or "
                "after the owner's one-time retry" if cell.variant == "defective"
                else "succeeded (update_verified)"),
            "provenance": provenance_for(cell.variant), "native_evidence": False,
            "inspections": {"points": list(INSPECTION_POINTS),
                            "rule": "read-only; before the update check, or after a terminal, paused or settled "
                                    "state; never inside the update preflight window (sidecar v2)"},
            "steps": [{"name": n, "does": d} for n, d in steps]}
    if cell.scenario == "setup-once":
        plan["expected_outcome"] = ("the web_mail setup reaches the isolated host's access_dns wait in ONE owner attempt "
                                    "(no update is started)")
    elif cell.scenario == "busy-start":
        plan["expected_outcome"] = ("during the owner's package task: readiness HOST_MUTATION_BUSY/package_manager_active, "
                                    "update start refused, installed release unchanged; after it, with packagekitd "
                                    "idle-alive: start admitted and succeeded (update_verified)")
    if owner_path:
        plan["port_hold"] = dict(hold, unit=PORT_HOLD_UNIT_PREFIX + "<request_id>.service",
                                 helper="guest_owner_port_hold.py", address="127.0.0.1:2083",
                                 release="owner: systemctl stop of the hold unit at the pause, before the retry")
    if management_off:
        plan["management_off"] = {"units": list(MANAGEMENT_UNITS),
                                  "measure_seconds": MANAGEMENT_OFF_MEASURE_SECONDS,
                                  "workloads": ["web", "smtp (Debian)", "cron", "db", "renewal timer", "firewall"]}
    if cell.variant in KIND_PATCHES:
        path, _, _ = KIND_PATCHES[cell.variant]
        plan["defect"] = {"kind": cell.variant, "file": path, "candidate_role": candidate_role(cell),
                          "owner_retry": "not run" if real_start else "only if the product pauses",
                          "second_fault": fault}
    return plan


KIND_EXPECTED = {
    "start-check": (f"the update fails in phase active after the database publication with {START_CHECK_CODE} "
                    f"(failure line and <request>.failure sidecar; check reason {START_CHECK_FIXTURE_REASON}); "
                    "completion.pending is never created; automatic rollback (dispatch phase=active, inverse "
                    "database exchange) to recovered/rollback_verified; old release running; database equal to the "
                    "pre-update digests (listed exclusions); CLI EN/TR 'returned to the previous version'"),
    "real-start": (f"the start check passes; completion.pending exists; the stability wait fails with "
                   f"{REAL_START_CODE}; forward completion is retried to its limit (N and timestamps recorded) and "
                   "pauses (paused_retry_limit); no rollback; the owner retry is printed but not run; texts name the "
                   "panel log command and say there is no supported return; site, mail and cron keep running while "
                   "the Panel stays down"),
    "owner-continuation": (f"the good candidate passes the start check; the held port makes its real start fail "
                           f"({REAL_START_CODE}); forward completion is retried three times (dispatch update/completion) "
                           "and pauses (paused_retry_limit, first_failure_code panel_start_unverified) while the port "
                           "is still held; the panel log the product names shows the port conflict; the owner "
                           "releases the port and runs the printed one-time retry exactly once; the same request "
                           "completes forward to update_verified (recovered-after-owner-continuation); the candidate "
                           "runs; certbot.timer and every other timer end in their pre-update state; site, mail and "
                           "cron never interrupted; the Panel is back; every owner text at the pause and after the "
                           "retry recorded in EN and TR"),
    "mgmt-off-reboot": ("the good update is verified; the owner disables and stops the Panel and Agent, reboots once "
                        "(orderly); for at least 180 s after the boot, with management off, the site answers with "
                        "its marker, SMTP answers (Debian), the cron stamp advances, the owner's database row is read "
                        "through the native client, the certificate renewal timer keeps its pre-update state and the "
                        "firewall ruleset is present and unchanged; management is re-enabled and the Panel shows the "
                        "same owner state"),
}


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
        self.role = candidate_role(cell)
        self.candidate = artifacts[self.role]
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

    def inspect(self, label: str, light: bool = False) -> dict | None:
        """Read-only side inspection at a scheduled point (sidecar v2 folded in). Never raises; never inside the
        update's preflight window (``inspection_allowed``)."""
        allowed, why = inspection_allowed(self.state, label)
        record = {"label": label, "light": light, "at": utc_now(), "allowed": allowed, "why": why}
        if allowed:
            try:
                record["result"] = self.workload("inspect", *(["--light"] if light else []), timeout=120)
            except Exception as exc:  # noqa: BLE001 - one lost inspection never stops the cell
                record["unavailable"] = self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]
        count = self.state.setdefault("inspections", {}).get(label, 0) + 1
        self.state["inspections"][label] = count
        try:
            self.record_json(f"inspect-{label}-{count:02d}.json", record)
        except Exception:  # noqa: BLE001 - evidence writer refusal is recorded by the writer itself
            pass
        return record

    def lab_event_bytes(self, name: str) -> bytes:
        """A private JSONL event file of this lab under the guest's private root, verbatim (read-only)."""
        if not re.fullmatch(r"[a-z0-9-]+-[0-9a-f]{32}\.jsonl", name):
            raise ValueError("invalid lab event file name")
        body = ("python3 -I - <<'CP_UPD1_EVENTS'\nimport base64\nfrom pathlib import Path\n"
                f"p=Path('{PRIVATE}/{name}')\n"
                "print(base64.b64encode(p.read_bytes() if p.exists() else b'').decode())\nCP_UPD1_EVENTS\n")
        return base64.b64decode(self.guest(body, timeout=30).stdout.strip() or b"")

    def lab_events(self, name: str) -> list[dict]:
        return [json.loads(line) for line in self.lab_event_bytes(name).splitlines() if line]

    def port_hold_event_name(self) -> str:
        return f"owner-port-hold-{self.state['request_id']}.jsonl"

    def keep_port_hold_events(self) -> dict:
        """H15 (upd4): the hold's raw event stream, copied from the guest's private root into this step's evidence
        (listed in SHA256SUMS like every evidence file); the summary alone was kept before."""
        name = self.port_hold_event_name()
        raw = self.lab_event_bytes(name)
        if not raw:
            return {"file": None, "events": 0, "note": f"{PRIVATE}/{name} is absent or empty on the guest"}
        kept = self.ev.write_text(f"{self.step_dir}/{name}", raw.decode("utf-8", "replace"))
        return {"file": kept, "events": len([line for line in raw.splitlines() if line]),
                "guest_sha256": hashlib.sha256(raw).hexdigest(), "guest_path": f"{PRIVATE}/{name}"}

    def port_hold_events(self) -> list[dict]:
        events = self.lab_events(self.port_hold_event_name())
        for event in events:
            if (event.get("schema") != PORT_HOLD_EVENT_SCHEMA or event.get("operation_id") != self.state["request_id"]
                    or event.get("identity") != {k: self.identity[k] for k in ("nonce", "vm_uuid", "cell_id", "node")}):
                raise StepInconclusive("port hold event identity differs")
        return events

    # -- the build that serves the owner (texts and card rules come from it) ---------------------

    def served_role(self) -> str:
        return "baseline" if self.cell.variant in ROLLBACK_VARIANTS else self.role

    def translator_for(self, role: str) -> Any:
        return self.translator if role == "baseline" else self.candidate_translator()

    def card_rules(self, role: str | None = None) -> dict | None:
        role = role or self.served_role()
        cache = self.__dict__.setdefault("_card_rules_cache", {})
        if role not in cache:
            try:
                cache[role] = load_card_rules(Path(self.artifacts[role]["product_web_src"]))
            except CardModelUnavailable as exc:
                cache[role] = None
                self.state.setdefault("card_model_unavailable", {})[role] = str(exc)
        return cache[role]

    def card(self, status_body: Any, recovery_body: Any, role: str | None = None) -> dict:
        role = role or self.served_role()
        rules = self.card_rules(role)
        card = update_card_guidance(self.translator_for(role), status_body if isinstance(status_body, dict) else None,
                                    recovery_body if isinstance(recovery_body, dict) else None, rules,
                                    self.state.get("request_id"))
        if rules is None:
            card["unavailable"] = (self.state.get("card_model_unavailable") or {}).get(role) or "card rules not loaded"
        card["role"] = role
        return card

    def screen(self, status: Any, role: str | None = None) -> dict:
        role = role or self.served_role()
        rules = self.card_rules(role)
        screen = recovery_guidance(self.translator_for(role), status, rules, self.state.get("request_id"))
        if rules is None:
            screen["unavailable"] = (self.state.get("card_model_unavailable") or {}).get(role) or screen["unavailable"]
        return screen

    def build_file(self, role: str, path: str) -> str:
        """One file of a built commit, read from the fixture clone (``git show``), never the working tree."""
        commit = self.artifacts[role]["commit"]
        return subprocess.run(["git", "-c", "safe.directory=*", "-C", self.artifacts["clone"], "show",
                               f"{commit}:{path}"], capture_output=True, check=True, timeout=60).stdout.decode("utf-8")

    def port_hold_bound(self) -> dict:
        texts = {name: self.build_file(self.role, path) for name, path in PORT_HOLD_SOURCES.items()}
        bound = port_hold_bound(parse_hold_sources(texts["update"], texts["timer"], texts["runner"]))
        return dict(bound, source={"role": self.role, "commit": self.candidate["commit"],
                                   "files": dict(PORT_HOLD_SOURCES)})

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
        validate_cell_artifacts(self.artifacts, self.cell)
        proofs = prove_artifacts(self.artifacts, ("baseline", self.role), self.m)
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
                                for role in cell_roles(self.cell)},
                  "created_at": utc_now(), "provenance": provenance_for(self.cell.variant)}
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
        failed_reads: list[dict] = []
        while time.monotonic() < deadline:
            try:
                status = baseline.status(self.root, self.record, self.plan, self.node_name)
            except subprocess.CalledProcessError as exc:
                # upd13 H22: the Arch installer's own full upgrade (pacman -Syu) replaces PAM, and for a moment the
                # guest's sudo cannot load it; that read is recorded and repeated at the next poll. A failure that
                # persists still stops the step.
                failed_reads.append(baseline_status_read_failure(exc))
                checks["h22_failed_status_reads"] = failed_reads
                if len(failed_reads) >= BASELINE_STATUS_READ_FAILURES_MAX:
                    raise
                time.sleep(15)
                continue
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

    def owner_waits(self) -> bool:
        """H19 applies on Ubuntu unless the cell is an ordinary owner who starts once (upd9 setuponce)."""
        return host_package_manager_waits(self.node_name) and getattr(getattr(self, "cell", None), "h19", True)

    def pk_on(self) -> bool:
        """upd9: PackageKit observations for this cell (offline tests build a Trial without a cell)."""
        return bool(getattr(getattr(self, "cell", None), "pk_observe", False))

    def scenario(self) -> str | None:
        return getattr(getattr(self, "cell", None), "scenario", None)

    def pk_probe(self) -> dict:
        """upd9: the PackageKit facts the Agent's rule reads, from /proc only (PK_PROBE)."""
        return json.loads(self.guest("python3 -I - <<'CP_PK'\n" + PK_PROBE + "\nCP_PK\n", timeout=60).stdout)

    def pk_observation(self, label: str, view: Any = None, readiness: bool = True) -> dict:
        """upd9: /proc facts, then the Agent's read-only readiness answer through the Panel, then /proc again, so the
        answer is bracketed by two readings. Never raises; recorded in state for the step's evidence file."""
        record: dict[str, Any] = {"label": label, "host_utc": utc_now()}
        for part in ("before", "readiness", "after"):
            if part == "readiness" and not readiness:
                continue
            try:
                if part == "readiness":
                    response = self.api("GET", "/api/v1/host-mutation-readiness", view=view, timeout=20,
                                        purpose="Agent package activity (readiness, read-only)")
                    body = response.json() or {}
                    record["readiness"] = {"http": response.status, "body": body, "utc": utc_now(),
                                           "answered_by": "panel" if body.get("reason") == "panel_operation_active"
                                           else "agent"}
                else:
                    record[part] = self.pk_probe()
            except Exception as exc:  # noqa: BLE001 - one lost reading never stops the cell
                record[part + "_error"] = self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]
            if not readiness:
                break
        self.state.setdefault("pk_observations", []).append(record)
        return record

    def pk_running(self, record: dict) -> bool | None:
        reading = record.get("after") or record.get("before")
        return None if not isinstance(reading, dict) else bool(reading.get("packagekitd"))

    def wait_host_package_manager_idle(self, timeout: float = HOST_IDLE_TIMEOUT_S) -> dict:
        """H19: read-only; returns when no package-manager process or apt/dpkg lock is seen, or at the limit."""
        started, seen, polls = time.monotonic(), [], 0
        while True:
            polls += 1
            value = json.loads(self.guest("python3 -I - <<'CP_IDLE'\n" + HOST_IDLE_PROBE + "\nCP_IDLE\n",
                                          timeout=60).stdout)
            if value["processes"] or value["locks"]:
                if value not in seen:
                    seen.append(value)
            else:
                return {"idle": True, "waited_s": round(time.monotonic() - started, 1), "polls": polls,
                        "busy_seen": seen, "at": utc_now()}
            if time.monotonic() - started >= timeout:
                return {"idle": False, "waited_s": round(time.monotonic() - started, 1), "polls": polls,
                        "busy_seen": seen, "at": utc_now()}
            time.sleep(10)

    def failed_setup_component(self, execution: dict) -> dict:
        """H19: read the failed setup step's component operation (GET /api/v1/service/operation?id=...)."""
        failed = [s for s in setup_steps(execution) if s.get("status") == "failed"]
        if not failed:
            return {"read": False, "reason": "no failed setup step"}
        if not failed[0].get("operation_id"):
            # A firewall/DNS step calls the Agent itself and has no component operation; its cause is in the
            # panel log line "server setup step <id> failed: ..." (the log the product texts name).
            step_id = str(failed[0].get("id") or "")
            if not re.fullmatch(r"[0-9]{2}-[a-z_]{1,40}", step_id):
                return {"read": False, "reason": "failed step id is not canonical"}
            out = self.guest("journalctl -u celikpanel-panel.service --no-pager -o cat -n 2000 | grep -F "
                             + shlex.quote(f"server setup step {step_id} failed:") + " | tail -n 1 || true",
                             timeout=60).stdout.strip()
            return {"read": True, "source": "panel journal", "step": step_id, "line": self.redactor.text(out[-600:]),
                    "names_host_busy": HOST_MUTATION_BUSY_TEXT in out}
        operation_id = str(failed[0]["operation_id"])
        if not HEX32.fullmatch(operation_id):
            return {"read": False, "reason": "component operation id is not canonical"}
        response = self.api("GET", f"/api/v1/service/operation?id={operation_id}",
                            purpose="ServerSetup failed step component operation")
        body = response.json() or {}
        operation = body.get("operation") if isinstance(body, dict) else None
        text = json.dumps(operation or {})
        found = {"read": True, "http": response.status, "step": failed[0].get("id"), "operation_id": operation_id,
                 "status": (operation or {}).get("status"), "phase": (operation or {}).get("phase"),
                 "error": (operation or {}).get("error"), "names_host_busy": HOST_MUTATION_BUSY_TEXT in text}
        if not found["names_host_busy"]:
            # H19 (upd8 Ubuntu run c): the mail profile operation shows only its generic code and the phase
            # profile/webmail/mail-tls; the cause is in the panel log line naming this operation, which the owner
            # reads (the product's texts name that log).
            out = self.guest("journalctl -u celikpanel-panel.service --no-pager -o cat -n 3000 | grep -F "
                             + shlex.quote(f"service operation {operation_id}") + " | tail -n 2 || true",
                             timeout=60).stdout.strip()
            found.update(log_line=self.redactor.text(out[-600:]), names_host_busy=HOST_MUTATION_BUSY_TEXT in out)
        return found

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
        purposes = ("web_mail", "web")
        if LABEL_REF is not None and not self.cell.mail_required:
            # H18 (upd7 run a, Arch): the published v0.1.0-alpha.80 accepts the web_mail plan on Arch (the source
            # refuses it: server_setup_service_unsupported:dovecot) and then fails at 05-mail_profile
            # (service_install_failed: "vmail user: open mail root parent"). A failed setup cannot be retried on the
            # same guest, so where mail is not required the owner chooses web only; recorded, never counted as mail.
            purposes = ("web",)
            checks["web_mail_not_attempted"] = (f"published baseline {LABEL_REF} on {self.node_name}: web_mail plan is "
                                                "accepted but its 05-mail_profile step fails (upd7 run a); purpose web")
            self.finding(f"{self.node_name}: web_mail was not attempted on the published {LABEL_REF} baseline (it accepts "
                         "the plan but fails at 05-mail_profile); mail is recorded as not provided on this platform")
        attempts: list[dict] = []
        idle_waits: list[dict] = []
        if not self.owner_waits():
            checks["owner_model"] = ("ordinary owner: the reviewed setup plan is started once; no wait for the "
                                     "package manager and no newly reviewed plan after a stop (H19 off)")
        pk_first = len(self.state.get("pk_observations", []))
        for attempt in range(1, SETUP_OWNER_ATTEMPTS + 1):
            if self.owner_waits():
                # H19 (upd8 Ubuntu run a): the owner follows the product text "wait and try again" -
                # before each start the owner waits until no package-manager task runs (read-only check).
                idle_waits.append(dict(self.wait_host_package_manager_idle(), before_attempt=attempt))
            if self.pk_on():
                self.pk_observation(f"setup-a{attempt:02d}-before-start")
            if attempt > 1:
                state = self.api("GET", "/api/v1/setup", purpose="ServerSetupGate").json() or {}
            for purpose in purposes:
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
            last_light = 0.0
            last_pk = 0.0
            with self.panel_client().polling() as view:
                while time.monotonic() < deadline:
                    if time.monotonic() - last_light >= 30:
                        # Sidecar v2: the before-first-site hosting-root series, every 30 s until setup settles.
                        self.inspect("before-site", light=True)
                        last_light = time.monotonic()
                    if self.pk_on() and time.monotonic() - last_pk >= 10:
                        # upd9: PackageKit facts and the readiness answer every ~10 s while setup runs.
                        self.pk_observation(f"setup-a{attempt:02d}-poll", view=view)
                        last_pk = time.monotonic()
                    try:
                        execution = self.api("GET", f"/api/v1/setup/operation?request_id={request_id}", view=view).json()
                    except self.p["panel_api"].PanelError as exc:
                        execution = {"poll_error": str(exc)}
                    if self.pk_on() and isinstance(execution, dict):
                        # upd9: the setup's own step transitions on the same host clock (changes only).
                        phases = self.state.setdefault("pk_setup_phases", [])
                        entry = {"attempt": attempt, "status": execution.get("status"),
                                 "phase": execution.get("phase"), "error": execution.get("error"),
                                 "steps": {x.get("id"): x.get("status") for x in setup_steps(execution)}}
                        if not phases or {k: v for k, v in phases[-1].items() if k != "utc"} != entry:
                            phases.append(dict(entry, utc=utc_now()))
                    decision = wait.observe(execution, time.monotonic())
                    if decision in ("terminal", "settled"):
                        break
                    time.sleep(5)
            if self.pk_on():
                self.pk_observation(f"setup-a{attempt:02d}-end")
            busy = (isinstance(execution, dict) and execution.get("status") == "failed"
                    and (execution.get("error") or {}).get("code") == HOST_MUTATION_BUSY_CODE)
            summary = {"attempt": attempt, "request_id": request_id, "purpose": purpose,
                       "status": (execution or {}).get("status"), "phase": (execution or {}).get("phase"),
                       "error": (execution or {}).get("error"),
                       "steps": {x.get("id"): x.get("status") for x in setup_steps(execution)}}
            if summary["error"]:
                # upd9: what the owner's screen can say for this code (the installed build's catalogues).
                summary["owner_texts"] = self.owner_error_texts(summary["error"])
            if (not busy and (self.owner_waits() or self.pk_on()) and isinstance(execution, dict)
                    and execution.get("status") == "failed"):
                # H19 (upd8 Ubuntu run b): a mail profile step installs its packages (PackageKit starts) and its own
                # next sub-step (mail TLS synchronization) is then refused as busy; the setup shows the generic
                # mail_profile_install_failed. The owner opens the failed step's component operation (the setup
                # screen links it) and reads its cause; only that named cause makes the owner wait and retry.
                # upd9: read for the record also where the owner does not retry (setuponce).
                component = self.failed_setup_component(execution)
                summary["component"] = component
                busy = bool(component.get("names_host_busy"))
            attempts.append(summary)
            if not busy or not self.owner_waits() or attempt == SETUP_OWNER_ATTEMPTS:
                break
            self.record_json(f"setup-execution-attempt-{attempt:02d}.json", execution)
            self.finding(f"{self.node_name}: server setup attempt {attempt} stopped at {summary['phase']} with "
                         f"{(summary['error'] or {}).get('code')} (cause: {HOST_MUTATION_BUSY_TEXT}); the owner waits "
                         "and starts a newly reviewed plan, as the product text says")
        checks["owner_attempts"] = attempts
        if idle_waits:
            checks["owner_idle_waits"] = idle_waits
        if self.pk_on():
            observations = self.state.get("pk_observations", [])[pk_first:]
            self.record_json("packagekit-observations.json", observations)
            self.record_json("setup-phases.json", self.state.get("pk_setup_phases", []))
            checks["packagekit"] = pk_summary(observations)
        self.inspect("after-setup")
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

    def owner_error_texts(self, error: Any) -> dict:
        """upd9: the API's code/message and the catalogue sentences an owner screen can show for it (EN/TR)."""
        if not isinstance(error, dict):
            return {}
        code, reason = str(error.get("code") or ""), str(error.get("reason") or "")
        texts: dict[str, Any] = {"code": code, "api_message": error.get("message")}
        for key in ([f"err.{code}.{reason}"] if reason else []) + [f"err.{code}"]:
            if code and self.translator.has(key):
                texts[key] = {language: self.translator.text(key, language=language) for language in ("en", "tr")}
        return texts

    def seed(self, checks: dict) -> str:
        if self.owner_waits():
            # H19: no agent mutation (seed) and no update (arm/start) while a package-manager task runs.
            checks["owner_waited_for_idle"] = self.wait_host_package_manager_idle()
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
        seeded["database"] = (self.seed_owner_database(domain_id, marker) if self.cell.variant == "mgmt-off-reboot"
                              else {"seeded": False, "reason": "only the management-off cell seeds an owner database"})
        self.state["seed"] = seeded
        checks["seeded"] = seeded
        self.record_json("seeded.json", seeded)
        self.inspect("after-seed")
        return "passed"

    def seed_owner_database(self, domain_id: int, marker: str) -> dict:
        """mgmt-off: the seeded site has no database, so the owner adds one through the Panel API
        (DomainDatabaseManager) and their application writes one table with one row through the native client.
        A refusal is recorded as a finding and the database workload as not seeded (never passed by omission)."""
        password = secrets.token_urlsafe(24)
        self.redactor.register(password)
        created = self.api("POST", f"/api/v1/domains/{domain_id}/databases",
                           {"name": "upd1db", "type": "mysql", "password": password},
                           purpose="DomainDatabaseManager create database", timeout=180)
        body = created.json() if created.status == 200 else None
        listed = self.api("GET", f"/api/v1/domains/{domain_id}/databases", purpose="DomainDatabaseManager list")
        record = {"create_http": created.status, "name": (body or {}).get("name"), "type": (body or {}).get("type"),
                  "list_http": listed.status}
        name = record["name"]
        if created.status != 200 or not isinstance(name, str) or not re.fullmatch(r"[A-Za-z][A-Za-z0-9_]{0,63}", name):
            self.finding(f"{self.node_name}: the owner database could not be created through the Panel API: HTTP "
                         f"{created.status}; the database workload is not measured")
            return dict(record, seeded=False, reason=f"Panel API answered HTTP {created.status}")
        record["listed"] = name in listed.text
        try:
            record["table"] = self.workload("db-seed", "--db-name", name, "--marker", marker, timeout=60)
        except Exception as exc:  # noqa: BLE001 - recorded; never repeated
            record["table"] = {"error": self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]}
        record["seeded"] = bool(((record["table"] or {}).get("query") or {}).get("ok"))
        if not record["seeded"]:
            self.finding(f"{self.node_name}: the owner's table could not be written or read through the native client: "
                         f"{record['table']}")
        return record

    def owner_db_name(self) -> str | None:
        database = (self.state.get("seed") or {}).get("database") or {}
        return database.get("name") if database.get("seeded") else None

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
        if self.owner_db_name():
            args += ["--db-name", self.owner_db_name()]
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
        if self.owner_db_name():
            args += ["--db-name", self.owner_db_name()]
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
        if self.owner_waits() and self.scenario() != "busy-start":
            # H19: no agent mutation (seed) and no update (arm/start) while a package-manager task runs.
            # upd9 busystart: the owner has just watched the package task end and starts while packagekitd idles.
            checks["owner_waited_for_idle"] = self.wait_host_package_manager_idle()
        if self.pk_on():
            checks["packagekit_at_arm"] = self.pk_observation("arm-before-check")
        # Sidecar v2: the last read-only inspection before the update check; none follows until a terminal,
        # paused or settled state (upd3 cell 1's v1 inspection fell inside the update preflight).
        self.inspect("before-check")
        # L1: the candidate is offered only by the guest-loopback fixture origin; prove it
        # is still the only answer for celikpanel.net before the update check.
        checks["origin_check"] = self.origin_check("before-arm")
        if not checks["origin_check"]["ok"]:
            raise StepFailed(f"the fixture origin is not serving before arm: {checks['origin_check']}")
        if self.cell.variant == "owner-continuation":
            # Read before the check: the bound comes from the candidate commit's own files.
            try:
                self.state["port_hold_bound"] = self.port_hold_bound()
            except (subprocess.SubprocessError, OSError, ValueError, UnicodeError) as exc:
                raise StepInconclusive(f"the port hold bound cannot be derived from the candidate build: {exc}")
            checks["port_hold_bound"] = self.state["port_hold_bound"]
        self.state["check_at"] = time.time()
        self.state["awaiting_terminal"] = True
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
        role = self.role
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
                if self.cell.variant == "owner-continuation":
                    checks["port_hold"] = self.arm_port_hold(request_id)
                return "passed"
            time.sleep(0.5)
        raise StepInconclusive("observer did not report armed; the update is not started")

    def arm_port_hold(self, request_id: str) -> dict:
        """owner-continuation: the owner-fixable cause, armed before the start; it binds only after the updater
        has stopped the old Panel and is released only by the owner at the pause (or by its own bound)."""
        bound = self.state["port_hold_bound"]
        unit = PORT_HOLD_UNIT_PREFIX + request_id + ".service"
        argv = ["systemd-run", f"--unit={unit}", "--no-block", f"--property=RuntimeMaxSec={bound['runtime_max_seconds']}",
                "--property=TimeoutStopSec=10", "--property=UMask=0077",
                "python3", "-I", f"{PRIVATE}/guest_owner_port_hold.py", "--lab-nonce", self.identity["nonce"],
                "--vm-uuid", self.identity["vm_uuid"], "--cell-id", self.identity["cell_id"], "--node", self.node_name,
                "--operation-id", request_id, "--hold-seconds", str(bound["hold_seconds"])]
        self.guest(shlex.join(argv), timeout=60)
        self.state["port_hold_unit"] = unit
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            events = self.port_hold_events()
            if events and events[0].get("event") == "armed":
                if any(e.get("event") == "released" for e in events):
                    raise StepInconclusive("the port hold released before the update started; not started")
                return {"unit": unit, "bound": bound, "armed_at": events[0].get("at")}
            time.sleep(0.5)
        raise StepInconclusive("the port hold did not report armed; the update is not started")

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

    def idle_alive_start(self, checks: dict) -> bool:
        """upd9 busystart: right after the owner's package task ended, packagekitd still idling, the owner presses
        start for the armed request at once (the API, without waiting for readiness). True when admitted."""
        obs = self.pk_observation("idle-alive-before-post")
        rid, check = self.state["request_id"], self.state["check"]
        body = {"request_id": rid, "confirmed": True, "current_version": check["current_version"],
                "current_commit": check["current_commit"], **check["target"]}
        self.trial.save(self.root, self.node_name, f"upd1-owner-start-{rid}.json", encoded(
            {"schema": "celikpanel/upd1-owner-start-attempt/v1", "request_id": rid, "body": body, "at": utc_now(),
             "note": "upd9 busystart: started while packagekitd idles after the owner's package task"}))
        self.state["started_at"] = time.time()
        try:
            response = self.api("POST", "/api/v1/panel/update/start", body, timeout=START_REQUEST_TIMEOUT_S,
                                purpose="SystemUpdateOperation start while packagekitd idles (once)")
            result = {"http": response.status, "body": response.json(), "utc": utc_now()}
            admitted = start_accepted(response.status, response.json())
        except self.p["panel_api"].UnknownOutcome as exc:
            result = {"outcome": "unknown; reconciled by the status read", "note": str(exc), "utc": utc_now()}
            admitted = None
        after = self.pk_observation("idle-alive-after-answer", readiness=False)
        status = self.api("GET", f"/api/v1/panel/update/status?request_id={rid}",
                          purpose="SystemUpdateOperation status (read-only)")
        status_body = status.json() or {}
        if admitted is None:
            admitted = status_body.get("status") in ("queued", "running", "succeeded")
        record = {"request_id": rid, "packagekitd_alive_before": self.pk_running(obs),
                  "packagekitd_alive_after": self.pk_running(after), "readiness": obs.get("readiness"),
                  "start": result, "admitted": admitted, "status": {"http": status.status, "body": status_body}}
        if not admitted:
            start_body = result.get("body") if isinstance(result.get("body"), dict) else {}
            record["owner_texts"] = self.owner_error_texts({"code": start_body.get("code"),
                                                            "message": start_body.get("error")})
            record["panel_log"] = self.redactor.text(self.guest(
                "journalctl -u celikpanel-panel.service --no-pager -o short-iso-precise --since=-5min "
                "| grep -F '[panel-update]' | tail -n 4 || true", timeout=60).stdout.strip())
            record["version"] = self.panel_version()
            self.finding("busystart: the update start while packagekitd idled after the owner's package task was "
                         f"refused (HTTP {result.get('http')} {(start_body or {}).get('code')}); readiness said "
                         f"{((obs.get('readiness') or {}).get('body'))}")
        else:
            self.state["start_answered_at"] = time.time()
        checks["idle_alive_attempt"] = record
        self.state["idle_alive_attempt"] = record
        self.record_json("idle-alive-start.json", record)
        return bool(admitted)

    def owner_start(self, checks: dict) -> str:
        if self.scenario() == "busy-start" and not self.state.get("idle_alive_attempt"):
            if self.idle_alive_start(checks):
                checks["start"] = self.state["idle_alive_attempt"]["start"]
                return "passed"
            # Refused: the owner waits as the card says (readiness below), then checks and arms anew for a new
            # request (the refused request id is final) and starts once.
            self.state["rearm_pending"] = True
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
        if self.state.pop("rearm_pending", False):
            rearm: dict = {}
            self.arm(rearm)
            checks["rearm"] = {k: rearm.get(k) for k in ("request_id", "observer_unit", "check", "packagekit_at_arm")}
        if self.pk_on():
            # upd9: /proc only (no readiness call, no lock) right before the one start request.
            checks["packagekit_before_post"] = self.pk_observation("owner-start-before-post", readiness=False)
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
            if not start_accepted(response.status, response.json()):
                raise StepFailed(f"update start refused: HTTP {response.status} {response.json()}")
        except self.p["panel_api"].UnknownOutcome as exc:
            checks["start"] = {"outcome": "unknown; reconciled only by status reads", "note": str(exc)}
        self.state["start_answered_at"] = time.time()
        if self.pk_on():
            checks["packagekit_after_answer"] = self.pk_observation("owner-start-after-answer", readiness=False)
        return "passed"

    # -- upd9 scenarios ------------------------------------------------------------------

    def unit_facts(self) -> list[str]:
        """Read-only: the Panel and Agent units' main PIDs and start times (a restart would change them)."""
        return self.guest("systemctl show celikpanel-agent.service celikpanel-panel.service -p Id -p MainPID "
                          "-p ActiveState -p ExecMainStartTimestamp --no-pager", timeout=30).stdout.strip().splitlines()

    def panel_version(self) -> dict:
        response = self.api("GET", "/api/v1/panel/version", purpose="PanelVersion (read-only)")
        body = response.json() or {}
        return {"http": response.status, **{k: body.get(k) for k in ("version", "commit", "agent_commit",
                                                                      "agent_matches")}}

    def readiness_texts(self) -> dict:
        """The sentences the update card shows for a refused readiness read (PanelUpdateCard.tsx), EN/TR."""
        keys = ("services.mutationReadiness.title", "services.mutationReadiness.package_manager_active",
                "err.HOST_MUTATION_BUSY.package_manager_active", "panelUpdate.packageManagerBusy",
                "err.PANEL_UPDATE_START_REFUSED")
        return {key: ({language: self.translator.text(key, language=language) for language in ("en", "tr")}
                      if self.translator.has(key) else "no catalogue entry") for key in keys}

    def busy_unit_state(self) -> dict:
        out = self.guest(f"systemctl show {BUSY_OP_UNIT} -p ActiveState -p SubState -p Result -p ExecMainStatus "
                         "-p ExecMainStartTimestamp -p ExecMainExitTimestamp --no-pager", timeout=30).stdout
        return dict(line.split("=", 1) for line in out.strip().splitlines() if "=" in line)

    def busy_start(self, checks: dict) -> str:
        """upd9 busystart: the owner's own long package task; the update start tried once while it runs; then the
        task ends and the owner confirms packagekitd still idles before arm/start (no H19 wait)."""
        pk_first = len(self.state.get("pk_observations", []))
        checks["before_op"] = self.pk_observation("busy-before-op")
        checks["guest_archives_free_bytes"] = self.guest("df -B1 --output=avail /var/cache/apt/archives | tail -n 1",
                                                         timeout=30).stdout.strip()
        chosen = self.guest(BUSY_OP_CHOOSE % " ".join(BUSY_OP_CANDIDATES), timeout=60).stdout.split()
        if len(chosen) != 2 or chosen[0] == "none":
            raise StepInconclusive(f"no candidate package for the owner's download-only task: {chosen}")
        package, size = chosen[0], int(chosen[1])
        limit = max(4, size // 1024 // BUSY_OP_TARGET_S)
        command = busy_op_command(package, limit)
        operation: dict[str, Any] = {"unit": BUSY_OP_UNIT, "package": package, "size_bytes": size,
                                     "dl_limit_kib_s": limit, "command": command}
        checks["operation"] = operation
        argv = ["systemd-run", f"--unit={BUSY_OP_UNIT}", "--no-block", f"--property=RuntimeMaxSec={BUSY_OP_RUNTIME_MAX_S}",
                "--property=TimeoutStopSec=30", "/bin/bash", "-c", command]
        started = time.monotonic()
        operation["started_utc"] = utc_now()
        self.guest(shlex.join(argv), timeout=60)
        held = None
        while time.monotonic() - started < 120:
            probe = self.pk_probe()
            if any(p.get("comm") == "apt-get" for p in probe["other_package_processes"]) and probe["general_lock_lines"]:
                held = probe
                break
            time.sleep(1)
        operation["holding"] = held
        if held is None:
            raise StepInconclusive("the owner's package task did not hold an apt/dpkg lock within 120 s")
        operation["holding_after_s"] = round(time.monotonic() - started, 1)
        rid = secrets.token_hex(16)
        busy: dict[str, Any] = {"request_id": rid, "version_before": self.panel_version(),
                                "units_before": self.unit_facts()}
        busy["observation"] = self.pk_observation("busy-during-op")
        check = self.api("GET", "/api/v1/panel/update/check", purpose="PanelUpdateCard check", timeout=60)
        cbody = check.json() or {}
        busy["check"] = {"http": check.status, "body": cbody}
        if check.status != 200 or cbody.get("available") is not True or not isinstance(cbody.get("target"), dict):
            raise StepInconclusive(f"the update check offered no candidate during the package task: {cbody}")
        body = {"request_id": rid, "confirmed": True, "current_version": cbody["current_version"],
                "current_commit": cbody["current_commit"], **cbody["target"]}
        attempt = {"schema": "celikpanel/upd1-owner-start-attempt/v1", "request_id": rid, "body": body,
                   "at": utc_now(), "note": "upd9 busystart: started while the owner's package task runs"}
        self.trial.save(self.root, self.node_name, f"upd1-owner-start-{rid}.json", encoded(attempt))
        try:
            response = self.api("POST", "/api/v1/panel/update/start", body, timeout=START_REQUEST_TIMEOUT_S,
                                purpose="SystemUpdateOperation start during the owner's package task (once)")
            busy["start"] = {"http": response.status, "body": response.json(), "utc": utc_now()}
        except self.p["panel_api"].UnknownOutcome as exc:
            busy["start"] = {"outcome": "unknown; reconciled only by status reads", "note": str(exc), "utc": utc_now()}
        busy["after_start"] = self.pk_observation("busy-after-start-answer", readiness=False)
        status = self.api("GET", f"/api/v1/panel/update/status?request_id={rid}",
                          purpose="SystemUpdateOperation status (read-only)")
        busy["status"] = {"http": status.status, "body": status.json()}
        start_body = (busy["start"].get("body") or {}) if isinstance(busy["start"].get("body"), dict) else {}
        busy["owner_texts"] = {
            "start_refusal": self.owner_error_texts({"code": start_body.get("code"), "message": start_body.get("error"),
                                                     "reason": start_body.get("reason")}),
            "catalogue": self.readiness_texts()}
        busy["panel_log"] = self.redactor.text(self.guest(
            "journalctl -u celikpanel-panel.service --no-pager -o short-iso-precise --since=-15min "
            "| grep -F '[panel-update]' | tail -n 6 || true", timeout=60).stdout.strip())
        busy["agent_log"] = self.redactor.text(self.guest(
            "journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise --since=-15min "
            "| grep -i -E 'update|package manager' | tail -n 8 || true", timeout=60).stdout.strip())
        busy["version_after"] = self.panel_version()
        busy["units_after"] = self.unit_facts()
        readiness = ((busy["observation"].get("readiness") or {}).get("body")) or {}
        admitted = start_accepted(busy["start"].get("http", 0), start_body) if "http" in busy["start"] else None
        expectations = {
            "readiness_busy_package_manager_active": readiness.get("ready") is False
            and readiness.get("code") == HOST_MUTATION_BUSY_CODE and readiness.get("reason") == "package_manager_active",
            "start_refused": admitted is False,
            "status_not_running": (busy["status"]["body"] or {}).get("status") not in ("queued", "running"),
            "release_unchanged": busy["version_before"] == busy["version_after"]
            and busy["units_before"] == busy["units_after"],
            "task_still_running_at_answer": bool((busy["after_start"].get("before") or {}).get("other_package_processes")),
        }
        busy["expectations"] = expectations
        self.state["busy"] = busy
        self.record_json("busy-start-refusal.json", busy)
        if admitted is not False:
            self.state["request_id_busy_admitted"] = rid
            raise StepFailed(f"the update start during the owner's package task was not refused: {busy['start']}")
        last = 0.0
        unit: dict = {}
        while time.monotonic() - started < BUSY_OP_RUNTIME_MAX_S + 120:
            unit = self.busy_unit_state()
            if unit.get("ActiveState") in ("inactive", "failed"):
                break
            if time.monotonic() - last >= 15:
                self.pk_observation("busy-op-poll")
                last = time.monotonic()
            time.sleep(3)
        operation.update(ended_utc=utc_now(), unit=unit, duration_s=round(time.monotonic() - started, 1))
        operation["journal"] = self.redactor.text(self.guest(
            f"journalctl -u {BUSY_OP_UNIT} --no-pager -o short-iso-precise | tail -n 80 || true", timeout=60).stdout)
        after = self.pk_observation("busy-after-op")
        checks["after_op"] = after
        after_ready = ((after.get("readiness") or {}).get("body") or {}).get("ready") is True
        expectations.update(task_finished=unit.get("ActiveState") in ("inactive", "failed"),
                            packagekitd_alive_after_task=self.pk_running(after) is True,
                            readiness_ready_after_task=after_ready)
        observations = self.state.get("pk_observations", [])[pk_first:]
        self.record_json("packagekit-observations.json", observations)
        checks.update(busy={k: busy[k] for k in ("request_id", "start", "status", "expectations", "owner_texts")},
                      packagekit=pk_summary(observations))
        for name, value in expectations.items():
            if not value:
                self.finding(f"busystart: expectation {name} not met")
        self.state["busy_after_op_at"] = time.time()
        return "passed" if all(expectations.values()) else "observed"

    def pk_after_setup(self, checks: dict) -> str:
        """upd9 setuponce: after setup stopped or settled, read PackageKit and the readiness answer every 20 s until
        packagekitd is gone (or 420 s), then once more: the idle daemon against the Agent's own answer."""
        pk_first = len(self.state.get("pk_observations", []))
        started = time.monotonic()
        with self.panel_client().polling() as view:
            while True:
                record = self.pk_observation("after-setup-poll", view=view)
                if self.pk_running(record) is False or time.monotonic() - started >= 420:
                    break
                time.sleep(20)
            self.pk_observation("after-setup-final", view=view)
        observations = self.state.get("pk_observations", [])[pk_first:]
        self.record_json("packagekit-observations.json", observations)
        checks["packagekit"] = pk_summary(observations)
        checks["seconds"] = round(time.monotonic() - started, 1)
        return "observed"

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
        answered = self.state.get("start_answered_at")
        start_instant = index == 0 or (answered is not None and sample["t"] - answered <= POLL_MIN_MS / 1000.0)
        sample["agreement"] = status_agreement(rid, api_recovery, cli, shell, start_instant=start_instant)
        # H10: the screen and the card as the served build renders them (its RecoveryAccess.tsx and
        # systemUpdateOutcome.ts rules and its catalogues), never a frozen model.
        sample["guidance_api"] = self.screen(api_recovery) if api_recovery else None
        sample["update_card"] = self.card(api_update, api_recovery) if api_update else None
        sample["guidance_cli"] = self.screen(cli) if cli else None
        sample["observed"] = cli if isinstance(cli, dict) and cli.get("observation") == "known" else api_recovery
        if sample["observed"] and self.screen(sample["observed"])["no_actor_or_action"]:
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
        settled = SettledFailure()
        running_after_success = RunningAfterSuccess()
        stop = None
        while time.monotonic() < deadline:
            with self.panel_client().polling() as view:
                sample = self.status_sample(view, index)
            index += 1
            self.state.setdefault("status_samples", []).append(sample)
            self.ev.write_json(f"{self.step_dir}/samples/{index:04d}.json", sample)
            observed = sample.get("observed")
            state = classify_status(observed)
            if state in ("terminal", "paused", "stopped"):
                # upd5: a typed stop before any change is final for the request; no H8 wait is needed.
                final = observed
                if final_needs_confirmation(sample):
                    # H21 (upd11): the Panel API was read just before the record turned final and the root CLI just
                    # after; one confirming sample decides (a persisting disagreement still fails the rule).
                    time.sleep(H21_CONFIRM_DELAY_S)
                    with self.panel_client().polling() as view:
                        confirm = self.status_sample(view, index)
                    index += 1
                    confirm["h21_confirming_sample"] = True
                    self.state["status_samples"].append(confirm)
                    self.ev.write_json(f"{self.step_dir}/samples/{index:04d}.json", confirm)
                    self.state.setdefault("h21_confirmations", []).append(
                        {"sample": index, "agreement": (confirm.get("agreement") or {}).get("verdict"),
                         "class": classify_status(confirm.get("observed"))})
                    if classify_status(confirm.get("observed")) == state:
                        final = confirm.get("observed")
                break
            if settled.observe(observed, time.monotonic()):
                # H8: named rule; the verdict stays inconclusive and says why.
                stop = dict(settled.record(time.monotonic()),
                            update_status=(sample.get("update_status") or {}).get("body"))
                break
            stale = running_after_success.observe(sample, observed, time.monotonic())
            if stale is not None:
                # H17 (upd7): named rule; the known open point is measured, not waited on for 90 minutes.
                self.state["record_running_after_success"] = stale
                final = None
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
        if final is not None or stop is not None:
            self.state["awaiting_terminal"] = False
            self.inspect("at-pause" if classify_status(final) == "paused" else "after-track")
        if stop is not None:
            checks["settled_failure"] = stop
            self.state["track_stop"] = stop
            raise StepInconclusive(
                f"{SETTLED_FAILED_RULE}: the status stayed {stop['status'].get('phase')}/"
                f"{stop['status'].get('terminal_proof')} (previous_failure={stop['status'].get('previous_failure')}) "
                f"without automatic recovery or a wait for {stop['seconds']} s over {stop['samples']} reads; "
                "no terminal or paused state follows a failure the product keeps unchanged")
        if final is None and self.state.get("record_running_after_success"):
            checks["record_running_after_success"] = self.state["record_running_after_success"]
            self.state["awaiting_terminal"] = False
            self.inspect("after-track")
            self.finding(f"{RUNNING_AFTER_SUCCESS_RULE}: the Panel reported the update succeeded while the recovery "
                         f"record stayed running for {self.state['record_running_after_success']['seconds']} s")
            return "observed"
        if final is None:
            raise StepInconclusive("no terminal or paused state was observed within 90 minutes")
        if classify_status(final) == "paused":
            self.state["paused"] = final
            self.state["paused_status"] = final
            return "observed"
        if classify_status(final) == "stopped":
            # The product's own final answer for this request; whether it is the cell's expected end is judged later.
            checks["stopped_before_change"] = {k: final.get(k) for k in ("phase", "terminal_proof", "previous_failure",
                                                                         "failure_code")}
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
        update_body = (last.get("update_status") or {}).get("body")
        views = {
            "cli": {"automatic_recovery": paused.get("automatic_recovery"),
                    "names_journal_command": all("journalctl -u celikpanel-release-recovery.service" in t
                                                 for t in cli_texts.values()),
                    "texts": cli_texts},
            "api": ({"automatic_recovery": api_body.get("automatic_recovery"),
                     "guidance": self.screen(api_body)}
                    if isinstance(api_body, dict) else {"unavailable": last.get("panel_error") or "no response"}),
            "update_card": (self.card(update_body, api_body) if isinstance(update_body, dict)
                            else {"unavailable": "the Panel did not answer; no browser can show the card"}),
            # What the build's recovery screen renders for this exact status (with the paused cause line), shown
            # only where a Panel answers; recorded here as the model of that screen, not as a reachable view.
            "recovery_screen_model": dict(self.screen(paused), reachable=isinstance(api_body, dict)),
            "offline_shell": {"note": "the saved page carries no exhaustion state by design; it names the read-only "
                                      "status command whose output does", "status_command": shell.get("status_command"),
                              "texts": shell.get("texts")},
        }
        checks["views"] = views
        # upd5: renewal_before_update recorded at the pause, against the pre-update timer snapshot.
        renewal = renewal_at_pause(paused if paused.get("renewal_before_update") or not isinstance(api_body, dict)
                                   else api_body, (self.state.get("pre_workload") or {}).get("timers"))
        checks["renewal_before_update"] = renewal
        self.state["renewal_at_pause"] = renewal
        for finding in renewal["findings"]:
            self.finding(f"renewal at the pause: {finding}")
        if not views["cli"]["names_journal_command"]:
            self.finding("paused CLI text does not name the recovery journal command in both languages")
        if isinstance(api_body, dict) and api_body.get("automatic_recovery") != "paused_retry_limit":
            raise StepFailed("the Panel recovery status does not report the exhausted budget")
        if not isinstance(api_body, dict):
            self.finding("while the recovery budget was exhausted the Panel recovery-status API was unavailable; "
                         "only the root CLI showed the exhausted state")
        snapshot = self.pending_snapshot()
        if self.cell.variant == "real-start":
            # The pause is this cell's expected end. The printed retry would retry the same broken
            # candidate, so it is read (owner-retry without --execute validates the exact state and
            # returns the printed command) and never run.
            try:
                printed = self.workload("owner-retry", "--request-id", rid, "--snapshot-name", snapshot, timeout=120)
            except Exception as exc:  # noqa: BLE001 - recorded; the retry is never run here
                printed = {"error": self.redactor.text(f"{type(exc).__name__}: {exc}")[:400]}
            checks["printed_retry"] = {k: printed.get(k) for k in ("action", "argv", "snapshot", "error",
                                                                    "journal_sha256", "attempted_at")}
            checks["owner_retry"] = "not run by design (real-start): it would retry the same candidate"
            self.state["printed_retry_command"] = (" ".join(printed["argv"]) if printed.get("action")
                                                   == "validated-not-executed" and printed.get("argv") else "")
            self.state["views_at_pause"] = views
            return "observed"
        self.state["views_at_pause"] = views
        if self.cell.variant == "owner-continuation":
            return self.owner_continuation_port(checks, rid, snapshot, paused)
        attempt = {"schema": "celikpanel/upd1-owner-continuation-attempt/v1", "request_id": rid, "snapshot": snapshot,
                   "at": utc_now(), "reason": "product reported paused_retry_limit; owner follows its printed retry"}
        self.trial.save(self.root, self.node_name, f"upd1-owner-continuation-{rid}.json", encoded(attempt))
        self.state["awaiting_terminal"] = True
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

    def owner_continuation_port(self, checks: dict, rid: str, snapshot: str, paused: dict) -> str:
        """owner-continuation: exactly what the product's paused text tells the owner, in order.

        1. read the panel log it names (``sudo journalctl -u celikpanel-panel -n 50``) - it must show the cause;
        2. read the printed one-time retry (owner-retry without --execute; nothing runs);
        3. fix the cause: release the port (``systemctl stop`` of the hold unit), verified released;
        4. run exactly the printed retry command, once, with a durable attempt record.
        Every text the owner sees at the pause is recorded before step 3.
        """
        events = self.port_hold_events()
        hold = port_hold_state(events)
        checks["hold_at_pause"] = hold
        self.state["hold_at_pause"] = dict(hold, held_at_pause=hold["held"] and not hold["released"])
        if not self.state["hold_at_pause"]["held_at_pause"]:
            self.finding(f"owner-continuation: the port hold was not held at the pause "
                         f"(released: {hold.get('release_reason')!r}); the cause was already gone")
        if paused.get("first_failure_code") != REAL_START_CODE:
            self.finding(f"owner-continuation: the pause names first_failure_code={paused.get('first_failure_code')!r}, "
                         f"not {REAL_START_CODE}")
        try:
            log = self.workload("journal", "--since=-3h", "--lines", "50", "--unit", "celikpanel-panel.service",
                                timeout=60).get("stdout", "")
        except Exception as exc:  # noqa: BLE001 - recorded
            log = None
            checks["panel_log_error"] = self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]
        if log is not None:
            self.ev.write_text(f"{self.step_dir}/panel-log-at-pause.txt", log)
        self.state["panel_log"] = None if log is None else {
            "command": "sudo journalctl -u celikpanel-panel -n 50", "lines": len(log.splitlines()),
            "names_cause": bool(PORT_CONFLICT_RE.search(log)),
            "matching_lines": [line[-300:] for line in log.splitlines() if PORT_CONFLICT_RE.search(line)][-5:]}
        checks["panel_log"] = self.state["panel_log"]
        checks["at_pause_snapshot"] = {k: v for k, v in self.workload_snapshot("at-pause").items()
                                       if k in ("timers", "services", "transaction", "web", "smtp", "cron")}
        try:
            printed = self.workload("owner-retry", "--request-id", rid, "--snapshot-name", snapshot, timeout=120)
        except Exception as exc:  # noqa: BLE001 - the retry is then not run
            raise StepInconclusive(f"the printed one-time retry could not be read: {type(exc).__name__}") from exc
        self.state["printed_retry_command"] = (" ".join(printed["argv"]) if printed.get("action")
                                               == "validated-not-executed" and printed.get("argv") else "")
        checks["printed_retry"] = {k: printed.get(k) for k in ("action", "argv", "snapshot", "journal_sha256",
                                                                "attempted_at")}
        self.ev.write_text(f"{self.step_dir}/recovery-journal-at-pause.txt", printed.get("journal_text") or "")
        if not self.state["printed_retry_command"]:
            raise StepInconclusive("the recovery journal printed no one-time retry command; nothing is run")
        # The owner fixes the cause: release the port (a hold that already ended has nothing to release).
        released_at = time.time()
        hold = port_hold_state(self.port_hold_events())
        stop_code = None
        if not hold["released"]:
            try:
                stop_code = self.guest(shlex.join(["systemctl", "stop", self.state["port_hold_unit"]]),
                                       timeout=60).returncode
            except subprocess.CalledProcessError as exc:
                stop_code = exc.returncode
        deadline = time.monotonic() + 20
        while not hold["released"] and time.monotonic() < deadline:
            time.sleep(0.5)
            hold = port_hold_state(self.port_hold_events())
        hold["released_before_retry"] = hold["released"]
        hold["held_at_pause"] = self.state["hold_at_pause"]["held_at_pause"]
        self.state["hold"] = hold
        checks["release"] = {"command": f"systemctl stop {self.state['port_hold_unit']}", "returncode": stop_code,
                             "requested_at": utc_now(), "hold": hold,
                             "note": None if stop_code is not None else "the hold had already ended; nothing to stop"}
        # H15: the hold's own event stream as it stands at the release (collect keeps the final one too).
        try:
            checks["port_hold_events"] = self.keep_port_hold_events()
        except Exception as exc:  # noqa: BLE001 - recorded; the owner path does not depend on the copy
            checks["port_hold_events"] = {"unavailable": self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]}
        if not hold["released"]:
            raise StepInconclusive("the port hold did not report released; the retry is not run")
        # Read-only, still paused: whether the Panel unit came back by itself once the port was free.
        self.inspect("after-continuation")
        attempt ={"schema": "celikpanel/upd1-owner-continuation-attempt/v1", "request_id": rid, "snapshot": snapshot,
                   "at": utc_now(), "released_at": released_at,
                   "reason": "product reported paused_retry_limit; the owner fixed the cause the panel log names "
                             "(released the held port) and follows the printed one-time retry"}
        self.trial.save(self.root, self.node_name, f"upd1-owner-continuation-{rid}.json", encoded(attempt))
        self.state["awaiting_terminal"] = True
        try:
            result = self.workload("owner-retry", "--request-id", rid, "--snapshot-name", snapshot, "--execute",
                                   timeout=3700)
        except subprocess.SubprocessError as exc:
            checks["result"] = f"unknown ({type(exc).__name__}); never repeated - status reads only"
            result = None
        checks["retry"] = None if result is None else {
            k: result.get(k) for k in ("action", "argv", "started_at", "finished_at", "result")}
        self.state["owner_retry"] = {"action": (result or {}).get("action"),
                                     "returncode": ((result or {}).get("result") or {}).get("returncode")}
        self.state["owner_continued"] = True
        self.state.pop("paused", None)
        return "observed"

    def pending_snapshot(self) -> str:
        for event in reversed(self.observer_events()):
            if event.get("snapshot"):
                return event["snapshot"]
        raise StepInconclusive("the operation snapshot is unknown; owner continuation is not attempted")

    def terminal(self, checks: dict) -> str:
        if self.cell.variant == "real-start":
            return self.terminal_real_start(checks)
        final = self.state.get("final_status")
        post_obs = self.guest_observation("post-recovery")
        post_work = self.workload_snapshot("post-recovery")
        pre_work = self.state["pre_workload"]
        # upd5: after a typed stop before any change the previous release runs and nothing was changed.
        stopped = classify_status(final) == "stopped"
        rollback = self.cell.variant in ROLLBACK_VARIANTS or stopped
        expected = self.artifacts["baseline"] if rollback else self.candidate
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
            # H10: both rendered from the served build's own rules and catalogues (systemUpdateOutcome.ts,
            # recoveryObservation.ts, RecoveryAccess.tsx), then judged only on a real mismatch.
            card["recovery_guidance"] = self.screen(card["recovery"]["body"])
            card["update_card"] = self.card(card["status"]["body"], card["recovery"]["body"])
            judged_card = judge_update_card(card["update_card"], ("unchanged",) if stopped
                                            else ("rolled_back",) if rollback else ("succeeded",))
            card["update_card_judged"] = judged_card
            self.state["card_judged"] = judged_card
            for finding in judged_card["findings"]:
                self.finding(f"update card: {finding}")
            if self.state["seed"]["mail"].get("listed"):
                accounts = self.api("GET", f"/api/v1/domains/{self.state['seed']['domain_id']}/mail/accounts",
                                    purpose="terminal mail accounts")
                card["mail_listed"] = self.state["seed"]["mail"]["address"] in accounts.text
            if self.cell.variant == "mgmt-off-reboot":
                self.state["truth_terminal"] = self.panel_truth("terminal")
        if self.cell.variant == "owner-continuation":
            last = (self.state.get("status_samples") or [{}])[-1]
            checks["texts_after_retry"] = {
                "cli": {lang: ((last.get("cli") or {}).get(lang) or {}).get("stdout") for lang in ("en", "tr")},
                "update_card": card.get("update_card"), "recovery_screen": card.get("recovery_guidance")}
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
        self.state["terminal"] = {"installed": installed_role(builds, self.artifacts, self.role),
                                  "database": database.get("verdict"),
                                  "recovery_body": (card.get("recovery") or {}).get("body"),
                                  "timers": timers, "login_ok": login_ok}
        self.state["post_workload"] = post_work
        self.inspect("after-terminal")
        failures = []
        if not identity_ok:
            failures.append("installed build identity is not the expected release")
        if not all(running.values()):
            failures.append("running executables differ from installed")
        if rollback and database["verdict"] not in ("equal", "equal-except-volatile"):
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

    def terminal_real_start(self, checks: dict) -> str:
        """real-start: the paused state is the terminal observation. The candidate is installed and its
        Panel is down; the owner's workloads must still be served. Nothing is repaired or retried."""
        post_obs = self.guest_observation("post-pause")
        post_work = self.workload_snapshot("post-pause")
        pre_work = self.state["pre_workload"]
        builds = post_work.get("build", {})
        role = installed_role(builds, self.artifacts, self.role)
        services = post_obs.get("services", {})
        panel_service = services.get("celikpanel-panel.service") or {}
        database = compare_databases(self.state["pre_observation"].get("database", {}), post_obs.get("database", {}),
                                     volatile_tables(bool(self.state.get("setup_waiting"))))
        timers = compare_states(pre_work.get("timers", {}), post_work.get("timers", {}))
        firewall_equal = pre_work.get("firewall", {}).get("sha256") == post_work.get("firewall", {}).get("sha256")
        self.client = None
        login = {"attempted": True}
        try:
            self.tunnel.ensure()
            self.panel_client().login(self.state["username"], self._password)
            login["ok"] = True
        except Exception as exc:  # noqa: BLE001 - an unreachable Panel is the expected observation here
            login.update(ok=False, error=type(exc).__name__)
        seed = self.state["seed"]
        checks.update(final=self.state.get("final_status"),
                      outcome=classify_outcome(self.cell.variant, self.state.get("final_status"), False),
                      installed=role, builds=builds, panel_service=panel_service,
                      transaction=post_work.get("transaction"), database_vs_pre_update=database,
                      database_note="recorded only: the candidate database was published before completion.pending",
                      timers=timers, firewall_equal=firewall_equal, login=login,
                      site_marker=post_work.get("web", {}).get("marker"), mailbox=post_work.get("mailbox"),
                      smtp=post_work.get("smtp", {}).get("ok"), cron=post_work.get("cron"),
                      views_at_pause=self.state.get("views_at_pause"))
        self.state["terminal"] = {"installed": role, "database": database.get("verdict"), "login": login}
        self.inspect("after-terminal")
        if login.get("ok"):
            self.finding("real-start: the owner could log in to the Panel after the pause; the fixture's panel "
                         "was expected never to listen")
        failures = []
        if not checks["site_marker"]:
            failures.append("site marker not served while the Panel is down")
        if seed["mail"].get("listed") and not ((post_work.get("mailbox") or {}).get("present") and checks["smtp"]):
            failures.append("mailbox or submission service missing while the Panel is down")
        if not timers["equal"]:
            failures.append(f"timers changed: {sorted(timers['changed'])}")
        if not firewall_equal:
            failures.append("firewall ruleset changed")
        if failures:
            raise StepFailed("; ".join(failures))
        return "passed"

    # -- upd12: the Panel's bounded retry of startup mail work -------------------------------------

    def panel_journal(self) -> str:
        return self.workload("journal", "--since=-12h", "--lines", "20000", "--unit", "celikpanel-panel.service",
                             timeout=120).get("stdout", "")

    def mail_file_facts(self, label: str) -> dict:
        try:
            raw = self.guest(mail_file_facts_script(), timeout=60).stdout
        except Exception as exc:  # noqa: BLE001 - recorded; a missing reading never stops the watch
            return {"label": label, "at": utc_now(), "unavailable": self.redactor.text(f"{type(exc).__name__}: {exc}")[:300]}
        return {"label": label, "at": utc_now(), "lines": raw.splitlines()}

    def deferred_mail_watch(self, checks: dict) -> str:
        """Read-only: follow the last Panel process's deferred startup mail retry until it resolved or the bound."""
        started = time.monotonic()
        facts = [self.mail_file_facts("watch-start")]
        polls: list[dict] = []
        text, view = "", {"processes": [], "last": None}
        while True:
            try:
                text = self.panel_journal()
                view = deferred_mail_view(text)
            except Exception as exc:  # noqa: BLE001 - one lost read is recorded; the next poll reads again
                polls.append({"at": utc_now(), "error": self.redactor.text(f"{type(exc).__name__}: {exc}")[:200]})
            last = view["last"] or {}
            polls.append({"at": utc_now(), "pid": last.get("pid"), "deferred": last.get("deferred"),
                          "attempts": len(last.get("attempts") or []), "finished": last.get("finished")})
            if last.get("finished") or time.monotonic() - started >= DEFERRED_MAIL_WATCH_S:
                break
            time.sleep(DEFERRED_MAIL_POLL_S)
        resolved_view = view
        settle = None
        last = view["last"] or {}
        if last.get("finished") and last.get("deferred"):
            time.sleep(DEFERRED_MAIL_SETTLE_S)
            try:
                text = self.panel_journal()
                after = deferred_mail_view(text)
                again = after["last"] or {}
                settle = {"at": utc_now(), "seconds_after_resolution": DEFERRED_MAIL_SETTLE_S,
                          "same_process": again.get("pid") == last.get("pid"),
                          "attempt_lines_before": len(last.get("attempts") or []),
                          "attempt_lines_after": len(again.get("attempts") or []),
                          "repeated_after": again.get("repeated")}
                view = after
            except Exception as exc:  # noqa: BLE001
                settle = {"at": utc_now(), "unavailable": self.redactor.text(f"{type(exc).__name__}: {exc}")[:200]}
        facts.append(self.mail_file_facts("watch-end"))
        self.ev.write_text(f"{self.step_dir}/journal-panel.txt", text)
        summary = {"processes": view["processes"], "polls": polls, "settle": settle, "mail_file_facts": facts,
                   "watch_seconds": round(time.monotonic() - started, 1),
                   "bound_seconds": DEFERRED_MAIL_WATCH_S}
        self.record_json("deferred-mail.json", summary)
        final = view["last"] or {}
        self.state["deferred_mail"] = {
            "panel_processes": [{k: p.get(k) for k in ("pid", "started_at", "ready_at", "deferred", "finished",
                                                        "gave_up", "repeated")} | {"attempt_lines": len(p["attempts"])}
                                for p in view["processes"]],
            "last": {k: final.get(k) for k in ("pid", "started_at", "ready_at", "deferred", "resolved", "finished",
                                               "gave_up", "repeated")},
            "settle": settle, "watch_seconds": summary["watch_seconds"]}
        checks.update(deferred_mail=self.state["deferred_mail"], polls=len(polls))
        if not resolved_view["last"]:
            raise StepInconclusive("no Panel start line in the Panel journal")
        if not (resolved_view["last"] or {}).get("finished"):
            raise StepInconclusive(f"the last Panel process's deferred startup mail work neither completed nor gave "
                                   f"up within {int(DEFERRED_MAIL_WATCH_S)} s of this watch")
        if final.get("repeated") or (settle and settle.get("attempt_lines_after", 0) > settle.get("attempt_lines_before", 0)):
            self.finding("deferred startup mail work: a resolved step ran again or another attempt line followed "
                         f"(repeated={final.get('repeated')}, settle={settle})")
        if final.get("gave_up"):
            self.finding("deferred startup mail work: the Panel gave up (the give-up line is in deferred-mail.json)")
        return "observed"

    # -- upd4: management off, one orderly reboot, management back ---------------------------------

    def panel_truth(self, label: str) -> dict:
        """The owner-visible Panel state (read-only GETs of the screens), without volatile fields."""
        seed, rid = self.state["seed"], self.state["request_id"]
        did = seed["domain_id"]
        truth: dict[str, Any] = {}
        with self.panel_client().polling() as view:
            def body(path: str, purpose: str) -> Any:
                response = self.api("GET", path, view=view, purpose=purpose)
                return response.status, response.json() if response.status == 200 else None, response
            _, version, _ = body("/api/v1/panel/version", "PanelVersion")
            # cmd/panel/version.go handleVersion; hostname/ipv4 are host facts, not owner state.
            truth["version"] = {k: (version or {}).get(k) for k in ("version", "commit", "schema_version",
                                                                    "agent_commit", "agent_matches")}
            _, domains, _ = body("/api/v1/domains", "Domains list")
            truth["domains"] = sorted((d.get("id"), d.get("domain_name") or d.get("name")) for d in (domains or [])
                                      if isinstance(d, dict))
            _, _, cron = body(f"/api/v1/domains/{did}/cron", "DomainCronManager list")
            truth["cron_listed"] = "upd1-cron-stamp.txt" in cron.text
            if seed["mail"].get("listed"):
                _, _, accounts = body(f"/api/v1/domains/{did}/mail/accounts", "DomainMailManager list")
                truth["mailbox_listed"] = seed["mail"]["address"] in accounts.text
            if self.owner_db_name():
                _, databases, _ = body(f"/api/v1/domains/{did}/databases", "DomainDatabaseManager list")
                truth["databases"] = sorted(d.get("name") for d in (databases or {}).get("databases") or []
                                            if isinstance(d, dict))
            _, status, _ = body(f"/api/v1/panel/update/status?request_id={rid}", "SystemUpdateOperation status")
            truth["update_status"] = (status or {}).get("status")
            _, recovery, _ = body(f"/api/v1/recovery/status?request_id={rid}", "RecoveryStatus")
            truth["recovery"] = {k: (recovery or {}).get(k) for k in ("phase", "terminal_proof", "previous_failure")}
        self.record_json(f"panel-truth-{label}.json", truth)
        return truth

    def management_off(self, checks: dict) -> str:
        # A volatile journal would lose the update's own lines at the reboot; keep them first.
        try:
            units = journal_groups(self.state.get("request_id"))["product"]
            text = self.workload("journal", "--since=-12h", "--lines", "20000",
                                 *sum((["--unit", u] for u in units), []), timeout=120).get("stdout", "")
            self.ev.write_text(f"{self.step_dir}/journal-product-before-reboot.txt", text)
        except Exception as exc:  # noqa: BLE001 - recorded; collect reads the journals again later
            checks["journal_before_reboot"] = f"unavailable: {type(exc).__name__}"
        self.state["truth_before_off"] = self.panel_truth("before-management-off")
        before = self.workload_snapshot("before-management-off")
        self.state["before_off_workload"] = before
        command = "systemctl disable --now celikpanel-panel.service celikpanel-agent.service"
        # H13 (upd4 d13-mgmt-off run-a): the owner's stop starts when the command is issued, so the update-only
        # verdict window ends at this instant, taken before the command; its completion is kept separately.
        self.state["management_off_at"] = time.time()
        done = self.guest(command, timeout=180)
        self.state["management_off_done_at"] = time.time()
        after = self.workload_snapshot("management-off")
        state = management_state(after.get("services"))
        checks.update(command=f"sudo {command}", returncode=done.returncode, management=state,
                      before=management_state(before.get("services")),
                      management_off_window={"requested_at": self.state["management_off_at"],
                                             "done_at": self.state["management_off_done_at"]})
        self.state["management_off"] = state
        if not state["disabled_and_stopped"]:
            raise StepFailed(f"the Panel/Agent are not disabled and stopped: {state['units']}")
        return "passed"

    def owner_reboot(self, checks: dict) -> str:
        before = self.guest("cat /proc/sys/kernel/random/boot_id", timeout=30).stdout.strip()
        # Orderly, as the owner would; the SSH session may end before it answers (never repeated).
        try:
            subprocess.run(self.lab.ssh(self.root, self.record, self.node) + ["sudo systemctl reboot"],
                           capture_output=True, timeout=30)
        except subprocess.TimeoutExpired:
            checks["reboot_command"] = "no answer within 30 s (the session ended with the reboot)"
        self.state["owner_reboot_at"] = time.time()
        self.tunnel.close()
        time.sleep(10)
        seconds = self.wait_for_ssh()
        after = self.guest("cat /proc/sys/kernel/random/boot_id", timeout=30).stdout.strip()
        record = {"before_boot_id": before, "after_boot_id": after, "new_boot": bool(after) and after != before,
                  "ssh_return_seconds": round(seconds, 1), "command": "sudo systemctl reboot"}
        self.state["owner_reboot"] = record
        checks.update(record)
        if not record["new_boot"]:
            raise StepFailed("the guest did not boot again")
        self.inspect("after-reboot")
        return "passed"

    def management_off_measure(self, checks: dict) -> str:
        boot_id = self.state["owner_reboot"]["after_boot_id"]
        deadline = time.monotonic() + 900
        window: list[dict] = []
        cron_seeded = bool(self.state["seed"]["cron"].get("seeded"))
        while time.monotonic() < deadline:
            window = boot_window(self.read_samples(), boot_id)
            if management_off_window_complete(window, cron_seeded):
                break
            time.sleep(10)
        snapshot = self.workload_snapshot("management-off-after-reboot")
        observed = self.management_off_observations(window, snapshot)
        # H20: how far the judged window ran past 180 s (a few seconds from polling alone; more when the window was
        # extended because cron had fewer than two in-boot stamps at 180 s).
        observed["h20_window_past_180_s"] = round(max(0.0, observed["window_seconds"] - MANAGEMENT_OFF_MEASURE_SECONDS),
                                                  1)
        observed["h20_cron_at_180_s"] = cron_after_boot(
            [s for s in window if float(s.get("monotonic", s["t"])) - float(window[0].get("monotonic", window[0]["t"]))
             <= MANAGEMENT_OFF_MEASURE_SECONDS + 5.0]) if cron_seeded and window else None
        self.state["management_off_measure"] = observed
        checks.update(observed)
        if observed["window_seconds"] < MANAGEMENT_OFF_MEASURE_SECONDS:
            raise StepInconclusive(f"only {observed['window_seconds']} s of samples in the new boot")
        needed = observed["needed_panel"]
        if needed or not observed["management_after_reboot"]["disabled_and_stopped"]:
            raise StepFailed("with management off: " + "; ".join(needed or ["the Panel/Agent were running"]))
        return "passed"

    def management_off_observations(self, window: list[dict], snapshot: dict) -> dict:
        seed = self.state["seed"]
        served = {"web": served_after_boot(window, "web"), "cron": (
            cron_after_boot(window) if seed["cron"].get("seeded") else {"verdict": "not-seeded"})}
        served["smtp"] = served_after_boot(window, "smtp") if seed["mail"].get("listed") else {"verdict": "not-seeded"}
        served["db"] = (served_after_boot(window, "db") if self.owner_db_name()
                        else {"verdict": "not-seeded", "reason": (seed.get("database") or {}).get("reason")})
        before = self.state.get("before_off_workload") or {}
        firewall = snapshot.get("firewall") or {}
        observed = {"boot_id": self.state["owner_reboot"]["after_boot_id"], "samples": len(window),
                    "window_seconds": window_seconds(window),
                    "first_sample_seconds_after_boot": window[0].get("monotonic") if window else None,
                    "management_after_reboot": management_state(snapshot.get("services")),
                    "served": served,
                    "renewal_timer": renewal_timer_verdict((self.state.get("pre_workload") or {}).get("timers"),
                                                           snapshot.get("timers")),
                    "firewall": {"present": bool(firewall.get("tables")), "tables": firewall.get("tables"),
                                 "equal_to_before": firewall.get("sha256") == (before.get("firewall") or {}).get("sha256")},
                    "services": snapshot.get("services")}
        needed = [f"{key}: {value.get('verdict')}" for key, value in served.items()
                  if value.get("verdict") not in ("served", "not-seeded")]
        if observed["renewal_timer"]["verdict"] == "changed":
            needed.append("certificate renewal timer changed state")
        if not observed["firewall"]["present"] or not observed["firewall"]["equal_to_before"]:
            needed.append("firewall ruleset absent or changed")
        observed["needed_panel"] = needed
        return observed

    def management_return(self, checks: dict) -> str:
        command = "systemctl enable --now celikpanel-agent.service celikpanel-panel.service"
        done = self.guest(command, timeout=180)
        checks.update(command=f"sudo {command}", returncode=done.returncode)
        deadline = time.monotonic() + 180
        leaf = None
        while time.monotonic() < deadline:
            try:
                leaf = self.refresh_pin()
                break
            except Exception:  # noqa: BLE001 - the Panel is still starting
                time.sleep(3)
        self.state["management_returned_at"] = time.time()
        back: dict[str, Any] = {"tls_leaf": leaf}
        if leaf is None:
            back["login_ok"] = False
        else:
            self.client = None
            try:
                self.tunnel.ensure()
                self.panel_client().login(self.state["username"], self._password)
                back["login_ok"] = True
            except Exception as exc:  # noqa: BLE001
                back.update(login_ok=False, error=type(exc).__name__)
        if back["login_ok"]:
            # H12 (upd4 d13-mgmt-off run-a): right after login the Panel still answered 503 PANEL_STARTING ("Panel
            # management is still starting") and every compared field read as empty. The owner's screens wait for
            # panel_state=ready; so does this read (read-only, bounded) before the owner state is compared.
            back["readiness_wait"] = self.wait_panel_ready()
            if back["readiness_wait"]["ready"]:
                after = self.panel_truth("after-management-return")
                back["differences"] = truth_differences(self.state.get("truth_before_off"), after)
            else:
                back["owner_state"] = (f"not compared: panel_state was not ready within "
                                       f"{MANAGEMENT_RETURN_READY_SECONDS:.0f} s")
        back["management"] = management_state(self.workload_snapshot("after-management-return").get("services"))
        self.state["management_return"] = back
        checks["management_return"] = back
        self.inspect("after-management-return")
        if not back["login_ok"] or back.get("differences") or not back["management"]["enabled_and_active"]:
            raise StepFailed(f"management did not return to the same owner state: {back}")
        if "owner_state" in back:
            raise StepInconclusive(f"management returned but the Panel never reported panel_state=ready within "
                                   f"{MANAGEMENT_RETURN_READY_SECONDS:.0f} s; the owner state was not compared")
        return "passed"

    def wait_panel_ready(self, bound: float | None = None) -> dict:
        """H12: read the exact request's recovery status (read-only) until it says ``panel_state=ready``, at most
        ``MANAGEMENT_RETURN_READY_SECONDS``; a 503 ``PANEL_STARTING``, ``starting`` or an error is read again."""
        bound = MANAGEMENT_RETURN_READY_SECONDS if bound is None else bound
        deadline = time.monotonic() + bound
        reads: list[dict] = []
        while True:
            entry: dict[str, Any] = {"at": utc_now()}
            try:
                probe = self.api("GET", f"/api/v1/recovery/status?request_id={self.state['request_id']}",
                                 purpose="RecoveryStatus (management return readiness, read-only)")
                body = probe.json()
                entry["http"] = probe.status
                if isinstance(body, dict):
                    entry["panel_state" if probe.status == 200 else "code"] = (
                        body.get("panel_state") if probe.status == 200 else body.get("code"))
            except Exception as exc:  # noqa: BLE001 - the Panel is still starting; read again
                entry["error"] = type(exc).__name__
            reads.append(entry)
            if entry.get("panel_state") == "ready" or time.monotonic() >= deadline:
                break
            time.sleep(MANAGEMENT_RETURN_READY_POLL_S)
        return {"ready": reads[-1].get("panel_state") == "ready", "reads": len(reads), "first": reads[0],
                "last": reads[-1], "bound_seconds": bound}

    def failure_lines(self) -> list[dict] | None:
        """The updater's failure lines from the collected product journal (None before collect)."""
        product = (self.state.get("journals") or {}).get("product")
        return None if product is None else parse_update_failure_lines(product)

    def outcome(self) -> str:
        return classify_outcome(self.cell.variant, self.state.get("final_status"),
                                bool(self.state.get("owner_continued")), self.failure_lines())

    def reach_observations(self) -> dict:
        """The run's records that say whether the cell's candidate kind was reached (``kind_not_reached``)."""
        rid = self.state.get("request_id") or ""
        product = (self.state.get("journals") or {}).get("product")
        attempts = self.state.get("attempts") or {}
        dispatches = [{k: a.get(k) for k in ("attempt", "operation", "phase", "direction")}
                      for a in attempts.get("automatic", [])]
        lines = self.failure_lines()
        return {"final": self.state.get("final_status"), "track_stop": self.state.get("track_stop"),
                "update_failure_codes": None if lines is None else [line["code"] for line in lines],
                "update_failure_lines": lines,
                "check_reasons": None if product is None else [r["code"] for r in parse_start_check_reasons(product)],
                "sidecar": sidecar_from_records(self.state.get("observation_records"), rid, self.candidate["commit"]),
                "renewal_sidecar": renewal_sidecar_from_records(self.state.get("observation_records"), rid,
                                                                self.candidate["commit"]),
                "completion_marker_seen": completion_marker_seen(self.state.get("observer_events")),
                "receipt_dispatches": dispatches or None,
                "installed": (self.state.get("terminal") or {}).get("installed")}

    def kind_observations(self) -> dict:
        """Everything the kind judges read, from this run's own records."""
        terminal = self.state.get("terminal") or {}
        attempts = self.state.get("attempts") or {}
        phases = [a.get("phase") for a in attempts.get("automatic", [])]
        texts = self.state.get("cli_texts")
        samples = self.state.get("status_samples", [])
        translator = self.translator_for(self.served_role())
        # H11: every screen the owner could have seen, rendered from the served build's own source.
        screens = [self.screen(s.get("observed")) for s in samples if s.get("observed")]
        if terminal.get("recovery_body"):
            screens.append(self.screen(terminal["recovery_body"]))
        unread = sorted({s["unavailable"] for s in screens if s.get("unavailable")})
        keys = sorted({key for s in screens for key in s["keys"]})
        per = (self.state.get("verdict_checks") or {}).get("workloads") or {}
        obs = self.reach_observations()
        obs.update({
            "receipt_phases": phases or None, "attempts": attempts, "database": terminal.get("database"),
            "cli": cli_text_observations(samples, texts) if texts else None,
            "web_keys": keys, "web_screen_unavailable": unread or None,
            # A key a rendered screen looked up and the catalogue lacks is a real mismatch; otherwise a screen that
            # could not be rendered leaves the rule unknown.
            "web_keys_missing": [k for k in keys if not translator.has(k)] or (None if unread or not keys else []),
            "owner_retry_run": bool(self.state.get("owner_continued")),
            "printed_retry_command": self.state.get("printed_retry_command"),
            "views": view_reachability(samples),
            "workloads": {k: v.get("verdict") for k, v in per.items() if k in WORKLOADS} or None,
            "panel_verdict": (per.get("panel") or {}).get("verdict"),
            "update_card": self.state.get("card_judged"),
            "outcome": self.outcome()})
        if self.cell.variant == "owner-continuation":
            obs.update(paused=self.state.get("paused_status"), hold=self.state.get("hold") or (
                           dict(self.state["hold_at_pause"], released_before_retry=False)
                           if self.state.get("hold_at_pause") else None),
                       panel_log=self.state.get("panel_log"), owner_retry=self.state.get("owner_retry"),
                       owner_receipts=attempts.get("owner_count") if attempts else None,
                       timers=terminal.get("timers"), login_ok=terminal.get("login_ok"))
        if self.cell.variant == "mgmt-off-reboot":
            measure = self.state.get("management_off_measure") or {}
            obs.update(management_after_reboot=measure.get("management_after_reboot"),
                       new_boot=(self.state.get("owner_reboot") or {}).get("new_boot"),
                       window_seconds=measure.get("window_seconds"), served=measure.get("served"),
                       renewal_timer=measure.get("renewal_timer"), firewall=measure.get("firewall"),
                       needed_panel=measure.get("needed_panel"),
                       management_return=self.state.get("management_return"))
        return obs

    def candidate_translator(self) -> Any:
        """The candidate build's own EN/TR catalogues (the screen a started candidate would serve)."""
        if getattr(self, "_candidate_translator", None) is None:
            guidance = self.p["guidance"]
            self._candidate_translator = guidance.Translator(guidance.load_catalog(
                Path(self.candidate["product_web_src"]) / "i18n"))
        return self._candidate_translator

    def load_cli_texts(self) -> dict:
        """The recovery CLI texts from the product source of the release that answers at the end
        (baseline after a rollback, the candidate otherwise), read from the fixture clone by commit."""
        role = "baseline" if self.cell.variant in ROLLBACK_VARIANTS else self.role
        commit = self.artifacts[role]["commit"]
        source = subprocess.run(["git", "-c", "safe.directory=*", "-C", self.artifacts["clone"], "show",
                                 f"{commit}:cmd/recovery/main.go"], capture_output=True, check=True, timeout=60).stdout
        texts = parse_cli_guidance(source.decode("utf-8"))
        texts["source"] = {"role": role, "commit": commit, "path": "cmd/recovery/main.go",
                           "sha256": hashlib.sha256(source).hexdigest()}
        return texts

    def kind_expectation(self, checks: dict) -> str:
        try:
            self.state["cli_texts"] = self.load_cli_texts()
            checks["cli_text_source"] = self.state["cli_texts"]["source"]
        except (subprocess.SubprocessError, OSError, ValueError, UnicodeError) as exc:
            checks["cli_text_source"] = f"unavailable: {type(exc).__name__}: {exc}"[:400]
        obs = self.kind_observations()
        judged = JUDGES[self.cell.variant](obs)
        checks.update(observations=obs, judged=judged)
        self.state["kind_judged"] = judged
        for finding in judged["findings"]:
            self.finding(f"{self.cell.variant}: {finding}")
        if judged["verdict"] == KIND_NOT_MEASURED:
            # The cell did not measure its kind; the product's own stop is recorded by the run, not judged here.
            raise StepInconclusive(f"{KIND_NOT_REACHED_RULE}: {judged['reason']}")
        return expectation_step_verdict(judged)

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
        # upd9 setuponce never installs the sampler (the cell ends after setup).
        samples = [] if self.scenario() == "setup-once" else attempt("guest-samples", self.read_samples) or []
        self.ev.write_text(f"{self.step_dir}/guest-samples.jsonl", "\n".join(json.dumps(s, sort_keys=True) for s in samples))
        self.ev.write_text(f"{self.step_dir}/host-samples.jsonl",
                           "\n".join(json.dumps(s, sort_keys=True) for s in self.host_samples))
        journals = {}
        groups = journal_groups(rid)
        if self.pk_on():
            # upd9: PackageKit's own journal (activation/exit times) and the owner's package task.
            groups["packagekit"] = ["packagekit.service", BUSY_OP_UNIT]
        for label, units in groups.items():
            value = attempt(f"journal-{label}", lambda units=units: self.workload(
                "journal", "--since=-12h", "--lines", "20000", *sum((["--unit", u] for u in units), []), timeout=120))
            if value is not None:
                journals[label] = value.get("stdout", "")
                attempt(f"journal-{label}-write",
                        lambda label=label: self.ev.write_text(f"{self.step_dir}/journal-{label}.txt", journals[label]))
        self.state["journals"] = journals
        observations = attempt("observation-records", lambda: json.loads(
            self.guest(observation_records_script(rid), timeout=30).stdout))
        if observations is not None:
            attempt("observation-records-write", lambda: self.record_json("observation-records.json", observations))
        self.state["observation_records"] = observations
        if self.state.get("paused_status") and observations is not None:
            # upd5: the <request>.renewal sidecar (read only) against what the status said at the pause.
            sidecar = renewal_sidecar_from_records(observations, rid, self.candidate["commit"])
            before = self.state.get("renewal_at_pause") or {}
            renewal = renewal_at_pause(dict(self.state["paused_status"],
                                            renewal_before_update=before.get("recorded")),
                                       (self.state.get("pre_workload") or {}).get("timers"), sidecar)
            checks["renewal_before_update"] = renewal
            for finding in renewal["findings"]:
                if finding not in before.get("findings", []):
                    self.finding(f"renewal at the pause: {finding}")
            self.state["renewal_at_pause"] = renewal
        observer =(attempt("observer-events", self.observer_events) or []) if rid else []
        self.state["observer_events"] = observer
        self.record_json("observer-events.json", observer)
        snapshot = next((e.get("snapshot") for e in reversed(observer) if e.get("snapshot")), None)
        budget = attempt("budget", lambda: self.workload("budget", *(["--snapshot-name", snapshot] if snapshot else [])))
        if budget is not None:
            self.record_json("budget.json", budget)
        attempts = attempts_from_receipts((budget or {}).get("receipts", []), snapshot)
        attempts["journal_admissions"] = parse_dispatch_journal(journals.get("product", ""))
        self.state["attempts"] = attempts
        if rid and self.cell.variant == "owner-continuation":
            # H15: the raw port-hold events file (guest private root) into the evidence, whatever stopped the cell.
            kept = attempt("port-hold-events", self.keep_port_hold_events)
            if kept is not None:
                checks["port_hold_events"] = kept
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
        if self.state.get("management_off_at"):
            # mgmt-off-reboot: this step judges the update part; the management-off window is judged by
            # management-off-measure (the Panel is down there by the owner's choice).
            # H13: management_off_at is taken before the owner's stop command is issued.
            # H14 (upd4 arch-mgmt-off run-a): a guest sample's "t" is the START of its sampler cycle and its Panel
            # probe runs last in the cycle (guest_upd1_workload.py sample(): site, DNS, SMTP, cron, then Panel),
            # about 2 s later on an external-DNS node. A cycle that started up to one interval before the stop
            # request may probe the Panel after it, so the update-only window ends one interval earlier.
            cut = self.state["management_off_at"] + skew - SAMPLE_INTERVAL_S
            samples = [s for s in samples if float(s.get("t", 0)) <= cut]
            checks["samples_until_management_off"] = len(samples)
            checks["management_off_cut"] = {"guest_clock": cut, "margin_seconds": SAMPLE_INTERVAL_S,
                                            "requested_at_host": self.state["management_off_at"]}
        # Host instants on the guest clock (the reset may also move the guest clock; recorded, not corrected).
        resets = [reset + skew for reset in self.state["resets"]]
        started = self.state["started_at"] + skew if self.state.get("started_at") else None
        terminal_at = self.state["terminal_at"] + skew if self.state.get("terminal_at") else None
        checks["clock_skew_seconds"] = skew
        per = workload_verdicts(samples, resets, mail_listed=bool(self.state.get("seed", {}).get("mail", {}).get("listed")),
                                cron=self.cron_scope(), dns_mode=self.dns_mode)
        panel_windows = classify_windows(outage_windows(samples, "panel"), resets)
        stopped_before = (kind_not_reached(self.cell.variant, self.reach_observations())
                          if self.cell.variant == "real-start" else None)
        if self.cell.variant == "real-start" and not stopped_before:
            # The fixture's Panel never listens again: down from the update to the end is expected.
            per["panel"] = dict(panel_verdict_until_end(panel_windows, started), windows=panel_windows)
            panel_expected = PANEL_UNTIL_END
        else:
            # A real-start update that stopped before the candidate ran (upd4 F4/F5) is judged as any other update:
            # the Panel may be down only inside the operation.
            per["panel"] = dict(panel_verdict(panel_windows, started, terminal_at), windows=panel_windows)
            panel_expected = "down-only-during-transaction"
            if stopped_before:
                per["panel"]["rule"] = f"{KIND_NOT_REACHED_RULE}: {stopped_before['shape']}"
                checks["kind_not_reached"] = stopped_before
        host_samples = self.host_samples
        if self.state.get("management_off_at"):
            # H14: the host series uses the same margin (its samples are taken the same way, start first).
            host_samples = [s for s in host_samples
                            if float(s.get("t", 0)) <= self.state["management_off_at"] - SAMPLE_INTERVAL_S]
        host_panel = outage_windows(host_samples, "panel")
        checks.update(workloads=per, host_panel_windows=host_panel, host_ssh_windows=outage_windows(host_samples, "ssh"),
                      agreement=agreement_verdict([s["agreement"] for s in self.state.get("status_samples", [])]),
                      outcome=self.outcome(),
                      attempts=self.state.get("attempts"))
        self.state["verdict_checks"] = {"workloads": per}
        interrupted = [k for k in WORKLOADS if per[k]["verdict"] == "interrupted"]
        if interrupted or per["panel"]["verdict"] != panel_expected:
            raise StepFailed("workload interruption outside the expected windows: "
                             + ", ".join(interrupted + ([] if per["panel"]["verdict"] == panel_expected
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
        if self.scenario() == "setup-once":
            return self.execute_setup_once()
        self.step("seed", self.seed, needs=("setup",))
        self.step("pre-state", self.pre_state, needs=("seed",))
        if self.scenario() == "busy-start":
            self.step("busy-start", self.busy_start, needs=("pre-state",))
            self.step("arm", self.arm, needs=("busy-start",))
        else:
            self.step("arm", self.arm, needs=("pre-state",))
        self.step("owner-start", self.owner_start, needs=("arm",))
        self.step("track", self.track, needs=("owner-start",))
        self.step("owner-continuation (required)", self.owner_continuation, needs=("track",))
        if self.state.get("owner_continued"):
            self.step("track-after-owner-continuation", lambda checks: self.track(checks, label="after-owner"),
                      needs=("owner-continuation (required)",))
        self.step("terminal", self.terminal, needs=("owner-start",))
        if deferred_mail_watched(self.cell):
            self.step("deferred-mail-watch", self.deferred_mail_watch, needs=("owner-start",))
        if self.cell.variant == "mgmt-off-reboot":
            # upd4: only after the verified good update (terminal passed); management returns even when the
            # measurement found something, as long as it was switched off.
            self.step("management-off", self.management_off, needs=("terminal",))
            self.step("owner-reboot", self.owner_reboot, needs=("management-off",))
            self.step("management-off-measure", self.management_off_measure, needs=("owner-reboot",))
            self.step("management-return", self.management_return, needs=("management-off",))
        # L3: collect is attempted whatever stopped the cell (it skips itself only when
        # the guest was never prepared), so a cell stopped at seed or earlier keeps its journals.
        self.step("collect", self.collect)
        self.step("verdicts", self.verdicts, needs=("owner-start",))
        if self.cell.variant in JUDGES:
            # upd3: judged after collect/verdicts, from this run's own records only.
            self.step("kind-expectation", self.kind_expectation, needs=("owner-start",))
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        judged_kind = self.state.get("kind_judged") or {}
        not_reached = judged_kind.get("verdict") == KIND_NOT_MEASURED
        result = {"schema": RESULT_SCHEMA, "native_evidence": False, "cell": dataclasses.asdict(self.cell),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": self.state.get("request_id"), "provenance": provenance_for(self.cell.variant),
                  "artifacts": {role: {k: self.artifacts[role][k] for k in ("version", "commit", "sha256")}
                                for role in cell_roles(self.cell)},
                  "outcome": {"classification": self.outcome(),
                              "final_status": self.state.get("final_status"),
                              "owner_continuation": bool(self.state.get("owner_continued")),
                              "attempts": self.state.get("attempts"), "reboot": self.state.get("reboot"),
                              "pin_changes": self.state.get("pin_changes", []),
                              "track_stop": self.state.get("track_stop"),
                              "port_hold": self.state.get("hold") or self.state.get("hold_at_pause"),
                              "owner_reboot": self.state.get("owner_reboot")},
                  "scope": result_scope(self.dns_mode, self.state.get("cron_availability"),
                                        self.state.get("setup_waiting"), self.state.get("origin_checks")),
                  "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")}
                            for s in self.steps],
                  "overall": overall(verdicts, kind_not_reached=not_reached),
                  "note": "Observations for the owner's review; the P0 rows are judged separately."}
        if self.cell.variant in JUDGES:
            result["kind"] = {"variant": self.cell.variant, "expected": KIND_EXPECTED[self.cell.variant],
                              "judged": self.state.get("kind_judged"),
                              "measured": None if not judged_kind else not not_reached,
                              "printed_retry_command": self.state.get("printed_retry_command")}
        if self.scenario() == "busy-start":
            busy = self.state.get("busy") or {}
            result["busy_start"] = {k: busy.get(k) for k in ("request_id", "start", "status", "expectations")}
            result["busy_start"]["idle_alive_attempt"] = {
                k: (self.state.get("idle_alive_attempt") or {}).get(k)
                for k in ("request_id", "packagekitd_alive_before", "admitted", "start", "readiness")}
        if self.pk_on():
            # upd11: every cell with the probe on summarises its observations (busy-start included, as before).
            result["packagekit"] = pk_summary(self.state.get("pk_observations", []))
        if deferred_mail_watched(self.cell):
            result["deferred_mail"] = self.state.get("deferred_mail")
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)

    def execute_setup_once(self) -> dict:
        """upd9 setuponce: the cell ends after setup (no seed, no update); PackageKit is read until it exits."""
        self.step("packagekit-after-setup", self.pk_after_setup, needs=("license",))
        self.step("collect", self.collect)
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        attempts = next((s["checks"].get("owner_attempts") for s in self.steps if s["name"] == "setup"), None) or []
        waiting = self.state.get("setup_waiting") or {}
        reached = waiting.get("phase") if waiting else None
        classification = (f"setup-reached-{reached}-wait-in-{len(attempts)}-attempt(s)" if reached
                          else "setup-succeeded" if (self.state.get("setup_execution") or {}).get("status") == "succeeded"
                          else "setup-stopped")
        result = {"schema": RESULT_SCHEMA, "native_evidence": False, "cell": dataclasses.asdict(self.cell),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": None, "provenance": provenance_for(self.cell.variant),
                  "artifacts": {role: {k: self.artifacts[role][k] for k in ("version", "commit", "sha256")}
                                for role in ("baseline",)},
                  "outcome": {"classification": classification, "final_status": None,
                              "setup_attempts": [{k: a.get(k) for k in ("attempt", "status", "phase", "error",
                                                                        "owner_texts", "component")}
                                                 for a in attempts]},
                  "scope": result_scope(self.dns_mode, self.state.get("cron_availability"),
                                        self.state.get("setup_waiting"), self.state.get("origin_checks")),
                  "packagekit": pk_summary(self.state.get("pk_observations", [])),
                  "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")}
                            for s in self.steps],
                  "overall": overall(verdicts),
                  "note": "upd9 setuponce: no update is started; observations for the owner's review."}
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
    fix.add_argument("--kind", required=True,
                     choices=("baseline", "baseline-ref", "good", "defective") + tuple(KIND_PATCHES))
    fix.add_argument("--previous-commit")
    fix.add_argument("--baseline-ref", choices=sorted(BASELINE_REFS),
                     help="upd7: the published baseline tag (kind baseline-ref; kind good labels the next release)")
    fix.add_argument("--source-commit", help="upd7: the exact source commit the acceptance-license seam is copied from")
    prove = sub.add_parser("prove", help="read-only host proof of every archive in the document (no guest)")
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
        print(json.dumps(fixture_source(args.repo, args.kind, args.previous_commit, baseline_ref=args.baseline_ref,
                                        source_commit=args.source_commit)))
        return 0
    if args.command == "prove":
        raw = json.loads(args.artifacts.read_text())
        configure_labels(raw)
        document = validate_artifacts(raw)
        roles = BASE_ROLES + tuple(role for role in EXTRA_ROLES if role in document)
        print(json.dumps({"schema": "celikpanel/upd1-artifact-proof/v1", "native_evidence": False,
                          "proofs": prove_artifacts(document, roles)},
                         indent=2, sort_keys=True))
        return 0
    cell = validate_cell(args.cell)
    validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    document = json.loads(args.artifacts.read_text())
    configure_labels(document)
    draft = json.loads(args.setup_draft_json.read_text()) if args.setup_draft_json else None
    if draft is not None and not isinstance(draft, dict):
        parser.error("--setup-draft-json must be a JSON object of draft fields")
    try:
        choices = setup_draft_choice(args.dns_mode, draft)
    except ValueError as exc:
        parser.error(str(exc))
    if args.command == "plan":
        validate_cell_artifacts(document, cell, check_files=not args.dry_run)
        plan = build_plan(cell, document, args.work_root, args.local_port, args.dns_mode)
        plan["draft_choices"] = choices
        print(json.dumps(plan, indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    validate_cell_artifacts(document, cell)
    result = Trial(cell, document, args.work_root, args.local_port, draft, args.dns_mode).execute()
    print(json.dumps({"overall": result["overall"], "outcome": result["outcome"]["classification"],
                      "request_id": result["request_id"],
                      "kind_measured": (result.get("kind") or {}).get("measured")}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
