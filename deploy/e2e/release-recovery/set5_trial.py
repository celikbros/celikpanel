#!/usr/bin/env python3
"""set5: the closing native measurement of the release candidate (commit 67b62cc0f): set3's Part 2 again, the
owner-started update from the published v0.1.0-alpha.81, cell by cell, on disposable QEMU guests (lab.py).

``Set5UpdateTrial`` is ``set4_trial.Set4UpdateTrial`` (itself ``owner_update_trial.Trial`` with two added steps) and
changes no step of either. Every one of the ten ``upd1-*`` cells of set3's Part 2 runs through it. What it adds:

  set5-name-pinning (the first step, before ``preflight``)
      Lab preparation, not an owner action: the directory names of the certificate authorities the product knows
      (``internal/core/acme_providers.go`` and certbot's default) are mapped to this guest's own loopback in its hosts
      file, before anything of the product is on the guest, and read back with ``getent -s files`` (the hosts file
      only; no DNS question is sent). ``preflight`` and everything after it run only if the reading shows it.
      ``celikpanel.net`` is mapped by the ``origin`` step, as in set3, before the baseline is installed.
  set4-php-site-before-the-update, set4-php-site-after-the-update (set4's item 9, unchanged)
      Only in ``upd1-debian13-good`` and ``upd1-ubuntu-good``: a PHP site created by the installed alpha.81, ten
      requests before the update, after it, and after the vhost was rendered again. Not on Arch (alpha.81 cannot
      create a PHP site there); set4's update-check step of the rollback cells is not run (not asked for here).
  M10-postfix-stop (set4's section, unchanged code; after ``verdicts``)
      Only in ``upd1-debian13-good`` and ``upd1-ubuntu-good``: one Stop of Postfix through the Panel while
      ``postfix check`` refuses ``main.cf``. It runs after the sample series was collected and judged, so that the
      cell's own outage windows are those of the update only. The section's code is ``Set4Trial.m10_postfix_stop`` and
      the recording methods of ``SettingsTrial``, borrowed as plain functions; the candidate was installed by the
      update, not fresh.
  set5-name-pinning-at-the-end (the last step)
      Read only: the same names again and ``celikpanel.net``, the boot id, whether a certbot log exists, and the
      kernel and the versions of the packaged services as they stand at the end of the cell.

Collection: ``set5_redact`` is put in front of the driver's redactor (digests under token-named keys, the directory
name under ``.release-db-migrations/``, and every such value wherever it appears again); before the result is written
every file of the run is passed through it once more and the counts are kept in ``set5-redaction-sweep.json``.

The driver calls only the Panel's HTTP API; every native fact is read over SSH by the guest helpers. No certificate
authority and no licence service is contacted. Every result carries ``native_evidence: false``.

  set5_trial.py plan --cell upd1-debian13-good --artifacts A.json --work-root /var/tmp/cp-release-drill-X [--dry-run]
  set5_trial.py run  --cell upd1-debian13-good --artifacts A.json --work-root /var/tmp/cp-release-drill-X --execute
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import shlex
import sys
import types
from typing import Any

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set4_trial as s4  # noqa: E402
import set5_redact  # noqa: E402

sw, base = s4.sw, s4.base
CELL_KIND = "set5-closing-update"
UPDATE_CELLS = ("upd1-arch-defective", "upd1-arch-good", "upd1-debian13-defective", "upd1-debian13-good",
                "upd1-debian13-mgmt-off-reboot", "upd1-debian13-owner-continuation", "upd1-debian13-startcheck",
                "upd1-ubuntu-defective", "upd1-ubuntu-good", "upd1-ubuntu-owner-continuation")
ITEM9_CELLS = ("upd1-debian13-good", "upd1-ubuntu-good")
M10_CELLS = ("upd1-debian13-good", "upd1-ubuntu-good")
PIN_STEP, PIN_END_STEP, M10_STEP = "set5-name-pinning", "set5-name-pinning-at-the-end", "M10-postfix-stop"
M10_TITLE = "Postfix stopped through the Panel while `postfix check` refuses main.cf (set4's section; the candidate installed by the update)"
ORIGIN_NAME = "celikpanel.net"
# certbot's default and its staging directory; the two other directories internal/core/acme_providers.go names.
CA_NAMES = ("acme-v02.api.letsencrypt.org", "acme-staging-v02.api.letsencrypt.org", "acme.zerossl.com",
            "dv.acme-v02.api.pki.goog")
PIN_MARK = "# set5 lab pin (disposable guest): certificate-authority directory names answer on this guest's own loopback"
LOOPBACK = ("127.0.0.1", "::1")

# One guest script for both readings; `apply` appends the lines first. It prints one JSON object and no secret.
PIN_SCRIPT = r'''
import hashlib, json, os, subprocess, sys, time
apply = sys.argv[1] == "apply"
names = json.loads(sys.argv[2]); mark = sys.argv[3]; origin = sys.argv[4]
hosts = "/etc/hosts"
def mapped(text, name):
    return [l.strip() for l in text.splitlines() if name in l.split("#", 1)[0].split()[1:]]
def ask(database, name):
    done = subprocess.run(["getent", "-s", "files", database, name], capture_output=True, text=True, timeout=20)
    return {"returncode": done.returncode, "stdout": done.stdout.strip()}
before = open(hosts, "rb").read()
out = {"mode": sys.argv[1], "hosts_sha256_before": hashlib.sha256(before).hexdigest(),
       "mapped_before": {n: mapped(before.decode("utf-8", "replace"), n) for n in names + [origin]}}
if apply:
    if any(out["mapped_before"][n] for n in names):
        out["refused"] = "a name is already mapped; the hosts file is not written twice"
    else:
        with open(hosts, "ab") as stream:
            stream.write(("\n" + mark + "\n" + "".join("127.0.0.1 %s\n::1 %s\n" % (n, n) for n in names)).encode())
            stream.flush(); os.fsync(stream.fileno())
        out["appended_lines"] = 1 + 2 * len(names)
text = open(hosts, "rb").read()
out["hosts_sha256_after"] = hashlib.sha256(text).hexdigest()
out["hosts_lines"] = [l for l in text.decode("utf-8", "replace").splitlines() if l.strip() and not l.lstrip().startswith("#")]
out["hosts_mode"] = oct(os.stat(hosts).st_mode & 0o7777)
out["asked_with"] = "getent -s files ahosts NAME and getent -s files hosts NAME (the hosts file only; no DNS question is sent)"
out["resolves"] = {n: {"ahosts": ask("ahosts", n), "hosts": ask("hosts", n)} for n in names + [origin]}
try:
    out["nsswitch_hosts"] = [l.strip() for l in open("/etc/nsswitch.conf") if l.startswith("hosts:")]
except OSError as exc:
    out["nsswitch_hosts"] = type(exc).__name__
out["product_paths_present"] = [p for p in ("/opt/celikpanel", "/etc/celikpanel", "/var/lib/celikpanel", "/usr/libexec/celikpanel") if os.path.exists(p)]
out["certbot_program"] = next((p for p in ("/usr/bin/certbot", "/usr/local/bin/certbot", "/snap/bin/certbot") if os.path.exists(p)), None)
logs = []
for directory in ("/var/log/letsencrypt", "/etc/letsencrypt/accounts", "/etc/letsencrypt/live", "/etc/letsencrypt/renewal"):
    try:
        logs.append({"path": directory, "entries": sorted(os.listdir(directory))[:20]})
    except OSError as exc:
        logs.append({"path": directory, "entries": None, "reason": type(exc).__name__})
out["certbot_directories"] = logs
wanted = ["nginx", "nginx-common", "nginx-core", "php-fpm", "php8.4-fpm", "php8.3-fpm", "php", "mariadb-server", "mariadb",
          "postgresql", "postgresql-17", "postgresql-16", "postfix", "dovecot-core", "dovecot", "rspamd", "roundcube",
          "roundcube-core", "roundcubemail", "certbot", "systemd", "openssl", "cronie", "cron"]
found = {}
for name in wanted:
    if os.path.exists("/usr/bin/dpkg-query"):
        done = subprocess.run(["dpkg-query", "-W", "-f", "${db:Status-Abbrev}|${Version}", name], capture_output=True, text=True, timeout=20)
        if done.returncode == 0 and done.stdout.startswith("ii"):
            found[name] = done.stdout.split("|", 1)[1]
    elif os.path.exists("/usr/bin/pacman"):
        done = subprocess.run(["pacman", "-Q", name], capture_output=True, text=True, timeout=20)
        if done.returncode == 0 and len(done.stdout.split()) == 2:
            found[name] = done.stdout.split()[1]
out["packages_installed"] = found
out["kernel"] = os.uname().release
try:
    out["os_release"] = {k: v.strip().strip('"') for k, v in (l.split("=", 1) for l in open("/etc/os-release") if "=" in l)
                         if k in ("PRETTY_NAME", "VERSION_ID", "ID", "BUILD_ID", "VERSION_CODENAME")}
except OSError as exc:
    out["os_release"] = type(exc).__name__
out["boot_id"] = open("/proc/sys/kernel/random/boot_id").read().strip()
out["uptime_seconds"] = float(open("/proc/uptime").read().split()[0])
out["at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
print(json.dumps(out, sort_keys=True))
'''


# ---------------------------------------------------------------------------
# Pure rules
# ---------------------------------------------------------------------------

def addresses_of(answer: dict | None) -> list:
    """The addresses of one ``getent`` answer (first field of every line)."""
    return [line.split()[0] for line in str((answer or {}).get("stdout") or "").splitlines() if line.split()]


def loopback_only(reading: dict | None) -> bool:
    """Both databases answered from the hosts file, and every address is this guest's own loopback."""
    reading = reading or {}
    found = addresses_of(reading.get("ahosts")) + addresses_of(reading.get("hosts"))
    return (bool(addresses_of(reading.get("ahosts"))) and bool(addresses_of(reading.get("hosts")))
            and all(address in LOOPBACK for address in found))


def pin_verdict(reading: dict, names: tuple) -> dict:
    resolves = reading.get("resolves") or {}
    return {name: loopback_only(resolves.get(name)) for name in names}


def cell_additions(name: str) -> dict:
    return {"item9": name in ITEM9_CELLS, "m10": name in M10_CELLS}


# ---------------------------------------------------------------------------
# The trial
# ---------------------------------------------------------------------------

class Set5UpdateTrial(s4.Set4UpdateTrial):
    # set1..set4's recording and service-action methods and set4's M10 section, borrowed as plain functions. None of
    # these names exists in owner_update_trial.Trial or Set4UpdateTrial, so no method of the update driver is replaced.
    native = sw.SettingsTrial.native
    snap = sw.SettingsTrial.snap
    owner = sw.SettingsTrial.owner
    keep_text = sw.SettingsTrial.keep_text
    check = sw.SettingsTrial.check
    note = sw.SettingsTrial.note
    catalogue = sw.SettingsTrial.catalogue
    call = sw.SettingsTrial.call
    refused = sw.SettingsTrial.refused
    section = sw.SettingsTrial.section
    postfix = sw.SettingsTrial.postfix
    smtp = sw.SettingsTrial.smtp
    journal = sw.SettingsTrial.journal
    guest_clock = sw.SettingsTrial.guest_clock
    service_units = sw.SettingsTrial.service_units
    service_state = sw.SettingsTrial.service_state
    daemon = sw.SettingsTrial.daemon
    service_action = sw.SettingsTrial.service_action
    snap4 = s4.Set4Trial.snap4
    m10_postfix_stop = s4.Set4Trial.m10_postfix_stop

    def __init__(self, cell: Any, artifacts: dict, work_root: str, local_port: int) -> None:
        super().__init__(cell, artifacts, work_root, local_port, None, "external")
        self.additions = cell_additions(cell.name)
        self.redactor, self.token_digests = set5_redact.wrap(self.redactor)
        # what the borrowed methods read of a fresh-install trial
        self.settings = types.SimpleNamespace(name=cell.name, mail=bool(cell.mail_required), purpose=None, components=())
        self.sections: dict[str, dict] = {}
        self.current: dict[str, Any] = {}
        self.native_sequence = 0
        finalize = self.ev.finalize_upd1

        def finalize_swept(result: dict) -> dict:
            result["set5"] = {"cell_kind": CELL_KIND, "additions": self.additions, "sections": self.sections,
                              "pinned_names": list(CA_NAMES)}
            report = set5_redact.sweep([self.ev.directory], self.token_digests)
            self.ev.write_json("set5-redaction-sweep.json", report)
            return finalize(result)
        self.ev.finalize_upd1 = finalize_swept

    def upload_helpers(self) -> dict:
        # set3's helpers always; set4's two only where item 9 or M10 runs, and set1's settings helper for M10.
        helpers = base.Trial.upload_helpers(self)
        extra = []
        if self.additions["item9"] or self.additions["m10"]:
            extra += [s4.rid.HELPER, s4.HELPER4]
        if self.additions["m10"]:
            extra.append(sw.HELPER)
        for name in extra:
            helpers[name] = self.lab.put_file(self.root, self.record, self.plan, self.node_name, HERE / name, name)[1]
        return helpers

    # -- the added steps -------------------------------------------------------------------------------------

    def step(self, name: str, function, *, needs: tuple = ()) -> str:
        plain = base.Trial.step
        if name == "preflight":
            plain(self, PIN_STEP, self.pin_names)
            needs = tuple(needs) + (PIN_STEP,)
        if name == "pre-state" and self.additions["item9"]:
            plain(self, "set4-php-site-before-the-update", self.php_before, needs=("seed",))
        if name == "collect" and self.additions["item9"]:
            plain(self, "set4-php-site-after-the-update", self.php_after,
                  needs=("terminal", "set4-php-site-before-the-update"))
        verdict = plain(self, name, function, needs=needs)
        if name == "verdicts":
            if self.additions["m10"]:
                self.m10_after_the_update()
            plain(self, PIN_END_STEP, self.pin_reading_at_the_end)
        return verdict

    def pin_reading(self, mode: str) -> dict:
        script = ("python3 -I - " + shlex.join([mode, json.dumps(list(CA_NAMES)), PIN_MARK, ORIGIN_NAME])
                  + " <<'CP_SET5_PIN'\n" + PIN_SCRIPT + "\nCP_SET5_PIN\n")
        return json.loads(self.guest(script, timeout=120).stdout)

    def pin_names(self, checks: dict) -> str:
        """Before anything else is done on the guest: the certificate authorities' names answer on its loopback."""
        self.lab.process_guard(self.node)
        reading = self.pin_reading("apply")
        self.record_json("name-pinning.json", reading)
        verdict = pin_verdict(reading, CA_NAMES)
        checks.update(what="lab preparation, not an owner action", names=list(CA_NAMES), loopback_only=verdict,
                      appended_lines=reading.get("appended_lines"), refused=reading.get("refused"),
                      product_paths_present=reading.get("product_paths_present"),
                      certbot_program=reading.get("certbot_program"), boot_id=reading.get("boot_id"),
                      uptime_seconds=reading.get("uptime_seconds"), at=reading.get("at"),
                      origin_name_mapped_yet=bool((reading.get("mapped_before") or {}).get(ORIGIN_NAME)),
                      origin_name_note=f"{ORIGIN_NAME} is mapped by the origin step, before the baseline is installed",
                      asked_with=reading.get("asked_with"), file="name-pinning.json")
        if reading.get("refused") or not all(verdict.values()):
            raise base.StepFailed("the certificate authorities' names do not answer on this guest's loopback only: "
                                  + json.dumps(verdict, sort_keys=True))
        if reading.get("product_paths_present"):
            raise base.StepFailed("the product was on the guest before the names were pinned")
        return "passed"

    def pin_reading_at_the_end(self, checks: dict) -> str:
        if self.verdict_of(PIN_STEP) != "passed":
            checks["reason"] = "the names were not pinned at the start; there is nothing to read again"
            return "skipped"
        reading = self.pin_reading("read")
        self.record_json("name-pinning-at-the-end.json", reading)
        names = CA_NAMES + (ORIGIN_NAME,)
        verdict = pin_verdict(reading, names)
        first = next((s for s in self.steps if s["name"] == PIN_STEP), {})
        checks.update(names=list(names), loopback_only=verdict, boot_id=reading.get("boot_id"),
                      boot_id_at_the_pinning=(first.get("checks") or {}).get("boot_id"),
                      certbot_program=reading.get("certbot_program"),
                      certbot_directories=reading.get("certbot_directories"), at=reading.get("at"),
                      packages_installed=reading.get("packages_installed"), kernel=reading.get("kernel"),
                      os_release=reading.get("os_release"),
                      asked_with=reading.get("asked_with"), file="name-pinning-at-the-end.json")
        if not all(verdict.values()):
            raise base.StepFailed("a pinned name no longer answers on this guest's loopback only: "
                                  + json.dumps(verdict, sort_keys=True))
        return "passed"

    def m10_after_the_update(self) -> str:
        """set4's M10 section on the candidate the update installed. The fresh-install trial's site domain and
        mailbox are those of this cell's seed."""
        seed = self.state.get("seed") or {}
        mail = seed.get("mail") or {}
        self.state["mailbox"] = mail.get("address") if mail.get("listed") else None
        if self.verdict_of("terminal") != "passed" or not seed.get("domain") or not self.state["mailbox"]:
            return base.Trial.step(self, M10_STEP, lambda checks: checks.update(
                reason="not run: the update did not end verified, or the seeded mailbox is missing") or "not-run")
        with base.patched(sw, "SITE_DOMAIN", seed["domain"]):
            return self.section(M10_STEP, M10_TITLE, self.m10_postfix_stop, needs=("terminal",))


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def build_plan(name: str, artifacts: dict, work_root: str, local_port: int) -> dict:
    cell = base.validate_cell(name)
    additions = cell_additions(name)
    added = [PIN_STEP]
    if additions["item9"]:
        added += ["set4-php-site-before-the-update", "set4-php-site-after-the-update"]
    if additions["m10"]:
        added.append(M10_STEP)
    added.append(PIN_END_STEP)
    return {"schema": "celikpanel/set5-plan/v1", "cell_kind": CELL_KIND, "native_evidence": False, "cell": name,
            "work_root": work_root, "local_port": local_port,
            "rule": "the driver calls only the Panel's HTTP API as the logged-in owner; every native fact is a read-only SSH "
                    "inspection; owner actions and lab preparation on the guest are recorded as such; no certificate "
                    "authority and no licence service is contacted",
            "pinned_names": list(CA_NAMES),
            "update": base.build_plan(cell, artifacts, work_root, local_port, "external").get("steps"),
            "added_steps": added}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("plan", "run"):
        cmd = sub.add_parser(name)
        cmd.add_argument("--cell", required=True, choices=sorted(UPDATE_CELLS))
        cmd.add_argument("--artifacts", required=True, type=Path)
        cmd.add_argument("--work-root", required=True)
        cmd.add_argument("--local-port", type=int, default=18443)
        if name == "plan":
            cmd.add_argument("--dry-run", action="store_true", help="validate the plan without any guest")
        else:
            cmd.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    base.validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    document = json.loads(args.artifacts.read_text())
    base.configure_labels(document)
    cell = base.validate_cell(args.cell)
    if args.command == "plan":
        base.validate_cell_artifacts(document, cell, check_files=not args.dry_run)
        print(json.dumps(build_plan(args.cell, document, args.work_root, args.local_port), indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    base.validate_cell_artifacts(document, cell)
    result = Set5UpdateTrial(cell, document, args.work_root, args.local_port).execute()
    print(json.dumps({"overall": result["overall"], "outcome": result["outcome"]["classification"],
                      "request_id": result["request_id"],
                      "steps": {s["name"]: s["verdict"] for s in result["steps"]}}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
