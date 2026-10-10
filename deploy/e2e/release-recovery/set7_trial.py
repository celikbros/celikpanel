#!/usr/bin/env python3
"""set7: what the CelikPanel interface shows in a real Chrome while the Panel restarts, on a disposable QEMU guest.

One cell kind over set6's good-update driver (``set6_trial.Set6UpdateTrial``), changing none of its steps:

  set6-name-pinning, preflight, origin, baseline-install, owner-login, license, setup, seed
      set6's steps unchanged (the five names pinned to the guest's loopback first and read back by two paths, the
      fixture release origin on the guest's loopback, the acceptance-test licence build, the owner's setup to the
      wait at ``access_dns``, the seeded site).
  set7-browser-window (new)
      The driver does NOT start the update. It starts a read-only probe on the guest (one HTTPS request to the
      Panel's own availability route on the guest's loopback every 0.5 s, its HTTP status and the guest clock
      written to a file), writes a handover file for the browser (the loopback port of the SSH forward, the owner's
      user name, the seeded domain, the Panel's own version answer; NOT the password) and the owner's password to a
      separate root-only file outside the evidence, and waits. A browser on the host (web/tools/browser-inspect/
      live-restart.mjs) signs in as the owner and does what the owner does: starts the update from Settings ->
      updates, or leaves a tab in the background. When the browser asks for it (a file ``restart-request-N``), the
      driver restarts the Panel's service on the guest (``systemctl restart celikpanel-panel.service``: the harness,
      on this disposable guest only) and records when. The window ends when the browser writes ``done.json`` or
      after ``--wait-seconds``. The password file is removed when the window ends.
  set7-guest-timeline (new)
      Read only: the probe's records, the journal of every celikpanel-*.service unit with microsecond timestamps,
      the Panel unit's own timestamps, the Panel's version answer, the guest-minus-host clock difference.
  collect, set6-name-pinning-at-the-end
      As set6.

  set7_trial.py plan --cell upd1-debian13-good --artifacts A.json --work-root /var/tmp/cp-release-drill-X --hand NAME [--dry-run]
  set7_trial.py run  --cell upd1-debian13-good --artifacts A.json --work-root /var/tmp/cp-release-drill-X --hand NAME --execute

Every result carries ``native_evidence: false``. No certificate authority and no licence service is contacted by the
driver; the browser opens only the loopback address of the SSH forward.
"""
from __future__ import annotations

import argparse
import base64
import json
import os
from pathlib import Path
import shlex
import sys
import time
from typing import Any

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set6_trial as s6  # noqa: E402

base = s6.base
CELL_KIND = "set7-browser-across-a-panel-restart"
RUN_ROOT = Path("/var/tmp/cp-set7-run")
HAND_ROOT = RUN_ROOT / "hand"
SECRET_ROOT = RUN_ROOT / "secret"
WINDOW_STEP, TIMELINE_STEP = "set7-browser-window", "set7-guest-timeline"
PANEL_UNIT = "celikpanel-panel.service"
PROBE_UNIT = "celikpanel-lab-set7-probe"
PROBE_FILE = "/root/celikpanel-release-recovery-lab/set7-probe.txt"
PROBE_SCRIPT = "/root/celikpanel-release-recovery-lab/set7-probe.py"
CELLS = ("upd1-debian13-good", "upd1-ubuntu-good", "upd1-arch-good")

# The guest probe: read only, the Panel's public availability route on the guest's own loopback; only the HTTP
# status (or the error class) and the guest clock are written. No session, no body is kept.
PROBE_BODY = r'''
import ssl, sys, time, urllib.request, urllib.error
out = sys.argv[1]
context = ssl.create_default_context(); context.check_hostname = False; context.verify_mode = ssl.CERT_NONE
while True:
    started = time.time()
    try:
        with urllib.request.urlopen(urllib.request.Request("https://127.0.0.1:2083/api/v1/panel/availability",
                                    headers={"Accept": "application/json"}), timeout=2, context=context) as r:
            status = str(r.status)
    except urllib.error.HTTPError as e:
        status = str(e.code)
    except Exception as e:
        status = "noanswer:" + type(e).__name__
    with open(out, "a") as f:
        f.write("%.3f %s %.3f\n" % (started, status, time.time() - started))
    time.sleep(max(0.0, 0.5 - (time.time() - started)))
'''


def utc_now() -> str:
    return base.utc_now()


class Set7BrowserTrial(s6.Set6UpdateTrial):
    def __init__(self, cell: Any, artifacts: dict, work_root: str, local_port: int, hand: str, wait_seconds: int) -> None:
        super().__init__(cell, artifacts, work_root, local_port)
        self.hand = HAND_ROOT / hand
        self.secret = SECRET_ROOT / f"{hand}.json"
        self.wait_seconds = wait_seconds

    # -- the browser window --------------------------------------------------------------------------------------

    def start_probe(self) -> dict:
        body = ("install -d -m 0700 /root/celikpanel-release-recovery-lab\n"
                f"cat > {PROBE_SCRIPT} <<'CP_SET7_PROBE'\n{PROBE_BODY}\nCP_SET7_PROBE\n"
                f"chmod 0600 {PROBE_SCRIPT}\n: > {PROBE_FILE}\n"
                f"systemd-run --unit={PROBE_UNIT} --no-block --property=RuntimeMaxSec=21600 "
                f"/usr/bin/python3 -I {PROBE_SCRIPT} {PROBE_FILE}\n"
                f"sleep 2; systemctl is-active {PROBE_UNIT}.service || true; wc -l < {PROBE_FILE}\n")
        out = self.guest(body, timeout=60).stdout
        return {"unit": PROBE_UNIT, "file": PROBE_FILE, "started_at": utc_now(), "answer": out.strip()[-300:]}

    def stop_probe(self) -> str:
        return self.guest(f"systemctl stop {PROBE_UNIT}.service || true; wc -l < {PROBE_FILE}", timeout=60).stdout.strip()

    def restart_panel(self, number: str) -> dict:
        before = time.time()
        script = (f"date -u +%FT%T.%NZ; systemctl show {PANEL_UNIT} -p InvocationID -p MainPID -p ActiveState\n"
                  f"systemctl restart {PANEL_UNIT}; echo rc=$?\n"
                  f"date -u +%FT%T.%NZ; systemctl show {PANEL_UNIT} -p InvocationID -p MainPID -p ActiveState "
                  f"-p ActiveEnterTimestamp -p ExecMainStartTimestamp\n")
        out = self.guest(script, timeout=180).stdout
        return {"request": number, "what": "systemctl restart celikpanel-panel.service on the disposable guest, by the harness",
                "host_before": before, "host_after": time.time(), "guest_output": out.strip()}

    def browser_window(self, checks: dict) -> str:
        if self.hand.exists():
            raise base.StepFailed(f"the handover directory {self.hand} exists; a window is never reused")
        self.hand.mkdir(parents=True, mode=0o755)
        SECRET_ROOT.mkdir(parents=True, mode=0o700, exist_ok=True)
        os.chmod(SECRET_ROOT, 0o700)
        checks["probe"] = self.start_probe()
        version = self.api("GET", "/api/v1/panel/version", purpose="set7: the Panel's version before the browser window")
        seed = self.state.get("seed") or {}
        leaf = self.refresh_pin()
        handover = {"schema": "celikpanel/set7-handover/v1", "local_port": self.local_port,
                    "base": f"https://127.0.0.1:{self.local_port}", "username": self.state.get("username"),
                    "domain": seed.get("domain"), "domain_id": seed.get("domain_id"), "panel_version": version.json(),
                    "tls_leaf_sha256": leaf, "baseline": {k: self.artifacts["baseline"][k] for k in ("version", "commit")},
                    "candidate": {k: self.candidate[k] for k in ("version", "commit")}, "at": utc_now(),
                    "password_file": str(self.secret)}
        descriptor = os.open(self.secret, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w") as handle:
            # `redact`: what the browser script removes from every text it writes (the acceptance fixture key).
            json.dump({"username": self.state.get("username"), "password": self._password,
                       "redact": [base.ACCEPTANCE_FIXTURE_KEY]}, handle)
        (self.hand / "ready.json").write_text(json.dumps(handover, indent=2, sort_keys=True) + "\n")
        checks["handover"] = handover
        print(json.dumps({"set7": "ready", "hand": str(self.hand), "port": self.local_port}), flush=True)
        deadline = time.monotonic() + self.wait_seconds
        restarts, ensured, done = [], time.monotonic(), None
        try:
            while time.monotonic() < deadline:
                for request in sorted(self.hand.glob("restart-request-*")):
                    number = request.name.rsplit("-", 1)[-1]
                    marker = self.hand / f"restart-done-{number}.json"
                    if marker.exists():
                        continue
                    record = self.restart_panel(number)
                    restarts.append(record)
                    marker.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
                    print(json.dumps({"set7": "restarted", "request": number}), flush=True)
                if (self.hand / "done.json").exists():
                    done = json.loads((self.hand / "done.json").read_text() or "{}")
                    break
                if time.monotonic() - ensured > 10:
                    self.tunnel.ensure()
                    ensured = time.monotonic()
                time.sleep(1)
        finally:
            try:
                self.secret.unlink()
            except FileNotFoundError:
                pass
            checks["password_file_removed"] = not self.secret.exists()
        checks.update(restarts=restarts, browser_done=done, ended_at=utc_now(),
                      probe_stopped=self.stop_probe())
        self.record_json("restarts.json", restarts)
        if done is None:
            raise base.StepInconclusive(f"the browser did not finish within {self.wait_seconds} s")
        return "observed"

    def guest_timeline(self, checks: dict) -> str:
        raw = self.guest(f"base64 -w0 {PROBE_FILE} 2>/dev/null || true", timeout=60).stdout.strip()
        probe = base64.b64decode(raw).decode() if raw else ""
        self.ev.write_text(f"{self.step_dir}/guest-probe.txt",
                           "# guest clock (epoch s), HTTP status of GET https://127.0.0.1:2083/api/v1/panel/availability "
                           "on the guest (or noanswer:<error class>), seconds the request took\n" + probe)
        journal = self.workload("journal", "--since=-12h", "--lines", "40000", "--unit", "celikpanel-*.service", timeout=120)
        self.ev.write_text(f"{self.step_dir}/journal-celikpanel-units.txt", journal.get("stdout", ""))
        unit = self.guest(f"systemctl show {PANEL_UNIT} -p InvocationID -p MainPID -p ActiveState -p ActiveEnterTimestamp "
                          "-p ExecMainStartTimestamp -p NRestarts; systemctl list-units --all --no-pager --plain "
                          "'celikpanel-self-update-*' 'celikpanel-*' | head -n 60", timeout=60).stdout
        self.ev.write_text(f"{self.step_dir}/units.txt", unit)
        version = self.api("GET", "/api/v1/panel/version", purpose="set7: the Panel's version after the browser window")
        skew = self.clock_skew()
        lines = [line.split() for line in probe.splitlines() if line.strip()]
        transitions, previous = [], None
        for parts in lines:
            ok = parts[1].isdigit()
            state = "answers" if ok else "no answer"
            if state != previous:
                transitions.append({"guest_epoch": float(parts[0]), "state": state, "status": parts[1]})
                previous = state
        checks.update(probe_lines=len(lines), probe_transitions=transitions, panel_version_after=version.json(),
                      guest_minus_host_seconds=skew, files=["guest-probe.txt", "journal-celikpanel-units.txt", "units.txt"])
        return "observed"

    # -- the cell ----------------------------------------------------------------------------------------------

    def execute(self) -> dict:
        plain = base.Trial.step
        plain(self, s6.PIN_STEP, self.pin_names6)
        plain(self, "preflight", self.preflight_after_the_pin(self.preflight), needs=(s6.PIN_STEP,))
        plain(self, "origin", self.origin, needs=("preflight",))
        plain(self, "baseline-install", self.baseline_install, needs=("origin",))
        plain(self, "owner-login", self.owner_login, needs=("baseline-install",))
        plain(self, "license", self.license, needs=("owner-login",))
        plain(self, "setup", self.setup, needs=("license",))
        plain(self, "seed", self.seed, needs=("setup",))
        plain(self, WINDOW_STEP, self.browser_window, needs=("seed",))
        plain(self, TIMELINE_STEP, self.guest_timeline, needs=("preflight",))
        plain(self, "collect", self.collect)
        plain(self, s6.PIN_END_STEP, self.pin_at_the_end6)
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        result = {"schema": base.RESULT_SCHEMA, "native_evidence": False, "cell": base.dataclasses.asdict(self.cell),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": None, "provenance": base.provenance_for(self.cell.variant),
                  "artifacts": {role: {k: self.artifacts[role][k] for k in ("version", "commit", "sha256")}
                                for role in base.cell_roles(self.cell)},
                  "outcome": {"classification": "set7: the update (if any) was started by the browser, not by the driver"},
                  "set7": {"cell_kind": CELL_KIND, "hand": str(self.hand)},
                  "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")}
                            for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "Observations for the owner's review; the browser's own records are kept beside this run."}
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("plan", "run"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--cell", required=True, choices=CELLS)
        cmd.add_argument("--artifacts", required=True, type=Path)
        cmd.add_argument("--work-root", required=True)
        cmd.add_argument("--local-port", type=int, default=18443)
        cmd.add_argument("--hand", required=True)
        cmd.add_argument("--wait-seconds", type=int, default=4 * 3600)
        if name == "plan":
            cmd.add_argument("--dry-run", action="store_true")
        else:
            cmd.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    base.validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    if not args.hand.replace("-", "").isalnum():
        parser.error("--hand must be a plain name")
    document = json.loads(args.artifacts.read_text())
    base.configure_labels(document)
    cell = base.validate_cell(args.cell)
    if args.command == "plan":
        base.validate_cell_artifacts(document, cell, check_files=not args.dry_run)
        print(json.dumps({"schema": "celikpanel/set7-plan/v1", "cell": args.cell, "cell_kind": CELL_KIND,
                          "work_root": args.work_root, "local_port": args.local_port, "hand": str(HAND_ROOT / args.hand),
                          "baseline": {k: document["baseline"][k] for k in ("version", "commit", "sha256")},
                          "candidate": {k: document[base.candidate_role(cell)][k] for k in ("version", "commit", "sha256")},
                          "steps": [s6.PIN_STEP, "preflight", "origin", "baseline-install", "owner-login", "license",
                                    "setup", "seed", WINDOW_STEP, TIMELINE_STEP, "collect", s6.PIN_END_STEP]},
                         indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    base.validate_cell_artifacts(document, cell)
    result = Set7BrowserTrial(cell, document, args.work_root, args.local_port, args.hand, args.wait_seconds).execute()
    print(json.dumps({"overall": result["overall"], "steps": {s["name"]: s["verdict"] for s in result["steps"]}},
                     sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
