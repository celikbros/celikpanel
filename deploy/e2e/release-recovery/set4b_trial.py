#!/usr/bin/env python3
"""set4b: the one product failure of set4 (item 10: a Stop of Postfix on Ubuntu 24.04 that leaves ``postfix@-.service``
marked ``failed`` is answered without the note), measured before it is corrected, and measured again afterwards.

Built on ``set4_trial.Set4Trial`` and changing nothing of it. Cells:

``set4b-diag-ubuntu``
    The product as set4 measured it (no correction). After the owner's setup: M0 (the lab's isolation), then D10:
    what the Agent reads around the Stop. The exact reading commands of the Agent with their raw answers; the
    owner's own ``systemctl stop postfix`` with the unit's states sampled tightly afterwards; then the Panel's Stop,
    several times, each under a kernel trace of the programs the Agent starts (one clock with systemd's own state
    timestamps), and once more with ``strace`` on the Agent, which also shows what each command printed.
``set4b-ubuntu`` | ``set4b-debian13``
    The corrected candidate, installed fresh. M0, then set4's own M10 section unchanged (the Panel's Stop while
    ``postfix check`` refuses ``main.cf``; the note; no ``reset-failed``; Start after the correction), then R10: the
    same Stop three more times, each with the kernel trace, so that the answer is set beside when the unit settled;
    then set4's M2 (the cPanel import with DNS left to the owner's provider) with what the answer lists.

The driver calls only the Panel's HTTP API; every native fact is read over SSH by the guest helpers, and what the
owner does on the server by hand goes through ``owner-*`` modes and is recorded as such. No certificate authority and
no licence service is contacted. Every result carries ``native_evidence: false``.

  set4b_trial.py plan --cell set4b-ubuntu --artifacts A.json --work-root /var/tmp/cp-release-drill-X [--dry-run]
  set4b_trial.py run  --cell set4b-ubuntu --artifacts A.json --work-root /var/tmp/cp-release-drill-X --execute
"""
from __future__ import annotations

import argparse
import dataclasses
import json
from pathlib import Path
import sys
import time
from typing import Any

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set4_trial as s4  # noqa: E402

sw, base = s4.sw, s4.base
CELL_KIND = "set4b-postfix-stop"
HELPER4B = "guest_set4b_native.py"
UNITS = ["postfix.service", "postfix@-.service"]
SITUATION = "main.cf holds a line Postfix refuses"
DIAG = "D10-what-the-agent-reads"
SECTIONS = {
    "set4b-diag-ubuntu": (("M0-prepare", "the platform and the lab's isolation"),
                          (DIAG, "what the Agent reads around a Stop of Postfix whose unit ends failed (measurement only)")),
    "remeasure": (("M0-prepare", "the platform and the lab's isolation"),
                  ("M10-postfix-stop", "Postfix stopped through the Panel while `postfix check` refuses main.cf (set4's section)"),
                  ("R10-postfix-stop-repeated", "the same Stop three more times, each beside when the unit settled"),
                  ("M2-import", "the cPanel-archive import of set3's fixture, DNS left to the owner's provider: what the answer lists")),
}
CELLS = {
    "set4b-diag-ubuntu": sw.SettingsCell("set4b-diag-ubuntu", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "set4b-ubuntu": sw.SettingsCell("set4b-ubuntu", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "set4b-debian13": sw.SettingsCell("set4b-debian13", "debian13", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
}


def sections_of(cell: str) -> tuple:
    return SECTIONS.get(cell, SECTIONS["remeasure"])


# ---------------------------------------------------------------------------
# Pure rules
# ---------------------------------------------------------------------------

def micros(value: Any) -> int | None:
    text = str(value or "")
    return int(text) if text.isdigit() and int(text) > 0 else None


def timeline(collected: dict, unit: str = "postfix@-.service") -> dict:
    """One clock (CLOCK_MONOTONIC seconds): the programs the Agent started, the master's end, and systemd's own
    moments for the unit. ``left_active`` is ActiveExitTimestampMonotonic (the stop job began), ``settled`` is
    InactiveEnterTimestampMonotonic (the unit became inactive or failed)."""
    events = (collected.get("trace") or {}).get("events") or []
    after = (collected.get("units_at_collect") or {}).get(unit) or {}
    left, settled = micros(after.get("ActiveExitTimestampMonotonic")), micros(after.get("InactiveEnterTimestampMonotonic"))
    master = [e["mono"] for e in events if e.get("started_by") == "master" and e.get("event") == "exit"]
    agent = [e for e in events if e.get("started_by") == "agent"]
    started = [{"mono": e["mono"], "program": e["program"], "pid": e["pid"]} for e in agent if e.get("event") == "exec"]
    ended = {e["pid"]: e["mono"] for e in agent if e.get("event") == "exit"}
    for item in started:
        item["ended"] = ended.get(item["pid"])
    value = {"unit": unit, "left_active_mono": left / 1e6 if left else None, "settled_mono": settled / 1e6 if settled else None,
             "state_at_collect": {k: after.get(k) for k in ("ActiveState", "SubState", "Result")},
             "master_ended_mono": master[-1] if master else None, "agent_programs": started}
    # the stop command is the last systemctl the Agent started before the unit left `active`: the readings of the
    # units come before it, and the stop job of the unit begins while it runs
    earlier = [i for i in started if i["program"] == "systemctl" and left and i["mono"] <= left / 1e6]
    stop = earlier[-1] if earlier else None
    value["agent_stop_command"] = stop
    # the last systemctl the Agent started is its reading of a unit after the stop (when it took one at all)
    shows = [i for i in started if i["program"] == "systemctl"]
    value["agent_last_systemctl"] = shows[-1] if shows else None
    if settled and shows:
        value["last_systemctl_started_after_the_unit_settled_ms"] = round((shows[-1]["mono"] - settled / 1e6) * 1000, 3)
    if settled and master:
        value["unit_settled_after_the_master_ended_ms"] = round((settled / 1e6 - master[-1]) * 1000, 3)
    if left and stop and stop["ended"]:
        value["stop_command_returned_after_the_unit_left_active_ms"] = round((stop["ended"] - left / 1e6) * 1000, 3)
    return value


class Set4bTrial(s4.Set4Trial):
    def upload_helpers(self) -> dict:
        helpers = super().upload_helpers()
        helpers[HELPER4B] = self.lab.put_file(self.root, self.record, self.plan, self.node_name, HERE / HELPER4B, HELPER4B)[1]
        return helpers

    def native4b(self, mode: str, /, **arguments: Any) -> dict:
        payload = s4.base64.b64encode(json.dumps(arguments).encode()).decode()
        return self.helper(HELPER4B, mode, "--args-b64", payload, timeout=300)

    def snap4b(self, file_label: str, mode: str, /, **arguments: Any) -> dict:
        # positional-only: the helper's own argument is named `label` too
        value = self.native4b(mode, **arguments)
        self.native_sequence += 1
        name = f"native/{self.native_sequence:03d}-{file_label}.json"
        self.record_json(name, value)
        self.current.setdefault("natives", []).append({"label": file_label, "mode": mode, "file": name,
                                                       "owner_action": mode.startswith("owner-")})
        value["_file"] = name
        return value

    # -- the owner's line and its removal ---------------------------------------------------------------------

    def typo(self, label: str, base_text: str) -> None:
        self.owner(f"{label}-typo-main-cf", "owner-edit", path=sw.MAIN_CF,
                   content_b64=sw.b64(sw.add_line(base_text, sw.MAIN_CF_TYPO)))

    def restore(self, label: str, base_text: str) -> dict:
        self.owner(f"{label}-typo-removed", "owner-edit", path=sw.MAIN_CF, content_b64=sw.b64(base_text))
        started = self.service_action("postfix", "start", f"after the owner's correction ({label})")
        return {"status": started["status"], "answer": started["answer"], "master_running": started["daemon_after"]["running"]}

    def panel_stop(self, label: str, *, sampler: bool, strace: bool) -> dict:
        """One Stop through the Panel under the instruments; the answer, the note, and the timeline."""
        armed = self.snap4b(f"{label}-armed", "diag-arm", label=label, sampler=sampler, strace=strace, seconds=16)
        before = self.snap4b(f"{label}-agent-view-before", "read-agent-view")
        stopped = self.service_action("postfix", "stop", f"{SITUATION} ({label})")
        entry = [c for c in self.current["calls"] if c["label"].startswith("S8 postfix stop")][-1]
        body = entry.get("_parsed") if isinstance(entry.get("_parsed"), dict) else (entry.get("json") or {})
        note = body.get("note") if isinstance(body, dict) else None
        time.sleep(6 if not sampler else 12)
        collected = self.snap4b(f"{label}-collected", "diag-collect", label=label)
        after = self.snap4b(f"{label}-agent-view-after", "read-agent-view")
        line = timeline(collected)
        record = {"label": label, "instruments": {"kernel_trace": "unavailable" not in (armed.get("trace") or {}),
                                                  "sampler": sampler, "strace": strace and "pid" in (armed.get("strace") or {})},
                  "status": stopped["status"], "answer": body, "note": note,
                  "note_given": isinstance(note, dict) and note.get("code") == "SERVICE_ACTION_NOTE",
                  "units_after": {u: {k: ((after.get("units") or {}).get(u) or {}).get(k) for k in ("ActiveState", "SubState", "Result")}
                                  for u in UNITS},
                  "master_running_after": stopped["daemon_after"]["running"], "timeline": line,
                  "files": {"armed": armed["_file"], "agent_view_before": before["_file"], "collected": collected["_file"],
                            "agent_view_after": after["_file"]}}
        return record

    # -- D10 --------------------------------------------------------------------------------------------------

    def d10_what_the_agent_reads(self) -> None:
        state = self.service_state("postfix-units-with-unit-files", "postfix", cat=True)
        self.current["unit_files"] = {name: unit.get("cat") for name, unit in state["units"].items()}
        first = self.snap4b("agent-view-at-start", "read-agent-view")
        self.current["agent_view_at_start"] = {
            "readUnitFailure": {u: {k: v.get(k) for k in ("argv", "returncode", "stdout", "stderr")}
                                for u, v in first["readUnitFailure"].items()},
            "postfixMasterProcess": first["postfixMasterProcess"]}
        for unit in UNITS:
            answer = first["readUnitFailure"][unit]
            self.check(f"measured: the Agent's reading command for {unit} was run and answered (exit status recorded)",
                       answer.get("returncode") is not None, {k: answer.get(k) for k in ("argv", "returncode", "stdout", "stderr")})
        base_text = self.snap("postfix-main.cf-before-typo", "read-file", path=sw.MAIN_CF)["file"]["text"]
        runs: list = []
        self.current["runs"] = runs
        try:
            # P1: the owner's own `systemctl stop postfix`, the unit sampled tightly
            self.typo("p1", base_text)
            refused = self.postfix("p1-postfix-with-typo")
            self.check("`postfix check` refuses main.cf (the owner's line)",
                       True if refused["check"].get("returncode") not in (0, None) else None, refused["check"])
            with_typo = self.snap4b("p1-agent-view-with-the-refused-main-cf", "read-agent-view")
            self.current["agent_view_with_the_refused_main_cf"] = {
                "readUnitFailure": {u: {k: v.get(k) for k in ("argv", "returncode", "stdout", "stderr")}
                                    for u, v in with_typo["readUnitFailure"].items()},
                "postfixMasterProcess": with_typo["postfixMasterProcess"]}
            armed = self.snap4b("p1-armed", "diag-arm", label="p1", sampler=True, strace=False, seconds=10, pause=0.002)
            by_hand = self.owner4b("p1-stop-by-hand", "owner-stop-by-hand")
            time.sleep(11)
            collected = self.snap4b("p1-collected", "diag-collect", label="p1")
            samples = (collected.get("samples") or {}).get("samples") or []
            self.current["by_hand"] = {
                "sent_monotonic_ns": by_hand["sent"]["monotonic_ns"], "returned_monotonic_ns": by_hand["returned"]["monotonic_ns"],
                "seconds": by_hand["seconds"], "returncode": by_hand["returncode"], "timeline": timeline(collected),
                "samples_total": (collected.get("samples") or {}).get("samples_total"),
                "files": {"armed": armed["_file"], "stop": by_hand["_file"], "collected": collected["_file"]}}
            self.check("measured: the owner's own stop was sampled (more than 50 samples of the two units)",
                       ((collected.get("samples") or {}).get("samples_total") or 0) > 50, {"samples_kept": len(samples)})
            self.current["by_hand"]["restored"] = self.restore("p1", base_text)
            # P2: the Panel's Stop under the kernel trace only, three times
            for name in ("p2a", "p2b", "p2c"):
                self.typo(name, base_text)
                runs.append(self.panel_stop(name, sampler=False, strace=False))
                runs[-1]["restored"] = self.restore(name, base_text)
            # P3: once with strace on the Agent and the sampler; P4: strace alone
            for name, sampler in (("p3", True), ("p4", False)):
                self.typo(name, base_text)
                runs.append(self.panel_stop(name, sampler=sampler, strace=True))
                runs[-1]["restored"] = self.restore(name, base_text)
        finally:
            self.owner("typo-removed-at-the-end", "owner-edit", path=sw.MAIN_CF, content_b64=sw.b64(base_text))
        traced = [r for r in runs if r["timeline"].get("agent_programs")]
        self.check("measured: the kernel trace shows the programs the Agent started in every Stop through the Panel",
                   len(traced) == len(runs) and bool(runs), [{"label": r["label"], "programs": len(r["timeline"]["agent_programs"])} for r in runs])
        self.check("measured: systemd's own moment for the unit's end was read in every Stop through the Panel",
                   bool(runs) and all(r["timeline"].get("settled_mono") for r in runs),
                   [{"label": r["label"], "settled": r["timeline"].get("settled_mono")} for r in runs])
        self.current["summary"] = [{"label": r["label"], "instruments": r["instruments"], "status": r["status"],
                                    "note_given": r["note_given"], "units_after": r["units_after"],
                                    "last_systemctl_started_after_the_unit_settled_ms":
                                        r["timeline"].get("last_systemctl_started_after_the_unit_settled_ms"),
                                    "unit_settled_after_the_master_ended_ms": r["timeline"].get("unit_settled_after_the_master_ended_ms"),
                                    "stop_command_returned_after_the_unit_left_active_ms":
                                        r["timeline"].get("stop_command_returned_after_the_unit_left_active_ms")} for r in runs]

    def owner4b(self, file_label: str, mode: str, /, **arguments: Any) -> dict:
        value = self.snap4b("owner-" + file_label, mode, **arguments)
        self.current.setdefault("owner_actions", []).append({"label": file_label, "mode": mode, "at": value.get("at")})
        return value

    # -- R10 --------------------------------------------------------------------------------------------------

    def r10_postfix_stop_repeated(self) -> None:
        base_text = self.snap("postfix-main.cf-before-typo", "read-file", path=sw.MAIN_CF)["file"]["text"]
        runs: list = []
        self.current["runs"] = runs
        try:
            for name in ("r1", "r2", "r3"):
                self.typo(name, base_text)
                runs.append(self.panel_stop(name, sampler=False, strace=False))
                later = self.snap4("%s-postfix-units-later" % name, "read-units", units=UNITS)
                runs[-1]["failed_later"] = [u for u in UNITS if (later.get("is_active") or {}).get(u) == "failed"]
                runs[-1]["restored"] = self.restore(name, base_text)
        finally:
            self.owner("typo-removed-at-the-end", "owner-edit", path=sw.MAIN_CF, content_b64=sw.b64(base_text))
        for run in runs:
            note = run["note"] if isinstance(run["note"], dict) else {}
            variables = note.get("vars") or {}
            failed = [u for u in UNITS if (run["units_after"].get(u) or {}).get("ActiveState") == "failed"]
            self.check(f"{run['label']}: Stop answers 200 success with the note, and the note names the unit systemd shows as failed",
                       run["status"] == 200 and run["note_given"] and bool(failed) and variables.get("failed_unit") in failed
                       and note.get("reason") in ("unit_marked_failed", "unit_marked_failed_config")
                       and variables.get("command") == "sudo systemctl reset-failed " + str(variables.get("failed_unit")),
                       {"status": run["status"], "note": run["note"], "units_after": run["units_after"]})
            self.check(f"{run['label']}: the unit is still `failed` afterwards (no reset-failed), and Start works after the correction",
                       run["failed_later"] == failed and bool(failed) and run["restored"]["status"] == 200
                       and run["restored"]["master_running"], {"failed_later": run["failed_later"], "restored": run["restored"]})
        self.current["summary"] = [{"label": r["label"], "status": r["status"], "note_reason": (r["note"] or {}).get("reason")
                                    if isinstance(r["note"], dict) else None, "units_after": r["units_after"],
                                    "last_systemctl_started_after_the_unit_settled_ms":
                                        r["timeline"].get("last_systemctl_started_after_the_unit_settled_ms"),
                                    "unit_settled_after_the_master_ended_ms": r["timeline"].get("unit_settled_after_the_master_ended_ms"),
                                    "agent_programs": [p["program"] for p in r["timeline"].get("agent_programs") or []]} for r in runs]

    # -- M2 with what the answer lists ------------------------------------------------------------------------

    def m2_import_lists(self) -> None:
        self.m2_import()
        record = self.current.get("import") or {}
        steps = {s.get("step"): s for s in record.get("steps") or [] if isinstance(s, dict)}
        applied = [c for c in self.current["calls"] if (c.get("request") or {}).get("path") == "/api/v1/import/cpanel/apply"]
        answer = (applied[-1].get("json") if applied else None) or {}
        sent = (applied[-1].get("request") or {}).get("body") if applied else None
        self.current["lists"] = {"imported": record.get("imported"), "not_imported": record.get("not_imported"),
                                 "left_out": answer.get("left_out"), "dns_step": steps.get("dns"),
                                 "forwarders_step": steps.get("forwarders"),
                                 "request_do_dns": (sent or {}).get("do_dns") if isinstance(sent, dict) else None,
                                 "dns_mode_of_the_server": "external (the setup's choice in this cell)"}
        self.check("item 3: the import asked for no DNS (`do_dns: false`) and `imported` does not name `dns`",
                   isinstance(record.get("imported"), list) and "dns" not in record["imported"]
                   and self.current["lists"]["request_do_dns"] is False, self.current["lists"])
        self.check("item 3: the `dns` step ended without an error, carries `state: left_to_owner` and is listed under `left_out`",
                   isinstance(steps.get("dns"), dict) and steps["dns"].get("ok") is True and steps["dns"].get("state") == "left_to_owner"
                   and isinstance(answer.get("left_out"), list) and "dns" in answer["left_out"],
                   {"dns_step": steps.get("dns"), "left_out": answer.get("left_out")})
        parts = [s.get("step") for s in record.get("steps") or [] if isinstance(s, dict) and s.get("step") != "finalize"]
        lists = [record.get("imported") or [], record.get("not_imported") or [], answer.get("left_out") or []]
        self.check("item 3: every step of the answer is in exactly one of `imported`, `not_imported` and `left_out`",
                   bool(parts) and all(sum(part in one for one in lists) == 1 for part in parts),
                   {"steps": parts, "imported": lists[0], "not_imported": lists[1], "left_out": lists[2]})
        self.check("item 3: the import is still `active` (a part left to the owner on purpose does not make it partial)",
                   record.get("import_status") == "active" and record.get("code") in (None, ""),
                   {k: record.get(k) for k in ("import_status", "code", "not_imported")})

    # -- run --------------------------------------------------------------------------------------------------

    def execute(self) -> dict:
        self.step("preflight", self.preflight)
        self.step("origin", self.origin, needs=("preflight",))
        self.step("baseline-install", self.baseline_install, needs=("origin",))
        self.step("owner-login", self.owner_login, needs=("baseline-install",))
        self.step("license", self.license, needs=("owner-login",))
        self.step("setup", self.setup, needs=("license",))
        self.step("site", self.site, needs=("setup",))
        functions = {"M0-prepare": self.m0_prepare, "M10-postfix-stop": self.m10_postfix_stop, DIAG: self.d10_what_the_agent_reads,
                     "R10-postfix-stop-repeated": self.r10_postfix_stop_repeated, "M2-import": self.m2_import_lists}
        for key, title in sections_of(self.settings.name):
            if key != "M0-prepare" and self.state.get("subscription_id") is None:
                self.step(key, lambda checks: checks.update(reason="M0 did not establish the subscription") or "not-run")
            else:
                self.section(key, title, functions[key], needs=("site",))
            self.sections.setdefault(key, {"title": title, "verdict": "not-run", "reason": "an earlier step did not pass"})
        self.step("collect", self.collect)
        self.tunnel.close()
        verdicts = [s["verdict"] for s in self.steps]
        result = {"schema": base.RESULT_SCHEMA, "cell_kind": CELL_KIND, "native_evidence": False,
                  "cell": dataclasses.asdict(self.settings),
                  "identity": {k: self.identity[k] for k in ("cell_id", "node", "vm_uuid")},
                  "request_id": None, "provenance": base.provenance_for("good"),
                  "artifacts": {"baseline": {k: self.artifacts["baseline"][k] for k in ("version", "commit", "sha256")}},
                  "outcome": {"classification": "set4b-measured", "final_status": None},
                  "setup": {"purpose": self.settings.purpose, "components": sorted(self.settings.components),
                            "waiting": self.state.get("setup_waiting")},
                  "site": {k: self.state.get(k) for k in ("domain_id", "site_user", "mailbox", "subscription_id")},
                  "sections": self.sections, "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")} for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "set4b: observations for the owner's review; no update is started and no P0 row is judged."}
        self.step_dir = "result"
        return self.ev.finalize_upd1(result)


def build_plan(name: str, artifacts: dict, work_root: str, local_port: int) -> dict:
    settings = CELLS[name]
    return {"schema": "celikpanel/set4b-plan/v1", "cell_kind": CELL_KIND, "native_evidence": False, "cell": name,
            "work_root": work_root, "local_port": local_port,
            "rule": "the driver calls only the Panel's HTTP API as the logged-in owner; every native fact is a read-only SSH "
                    "inspection (the kernel trace and strace observe, they send nothing to a service); owner actions on the "
                    "guest are recorded as such; no certificate authority and no licence service is contacted",
            "candidate": {k: artifacts["baseline"][k] for k in ("version", "commit", "sha256")},
            "setup": {"purpose": settings.purpose, "components": sorted(settings.components), "dns_mode": "external"},
            "steps": ["preflight", "origin", "baseline-install", "owner-login", "license", "setup", "site"]
                     + [key for key, _title in sections_of(name)] + ["collect"]}


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
    base.validate_work_root(args.work_root)
    if not 1024 < args.local_port < 65536:
        parser.error("--local-port must be an unprivileged loopback port")
    document = json.loads(args.artifacts.read_text())
    base.configure_labels(document)
    cell = CELLS[args.cell].cell
    if args.command == "plan":
        base.validate_cell_artifacts(document, cell, check_files=not args.dry_run)
        print(json.dumps(build_plan(args.cell, document, args.work_root, args.local_port), indent=2, sort_keys=True))
        return 0
    if not args.execute:
        parser.error("run mutates one registered disposable guest and requires --execute")
    base.validate_cell_artifacts(document, cell)
    result = Set4bTrial(CELLS[args.cell], document, args.work_root, args.local_port).execute()
    print(json.dumps({"overall": result["overall"], "cell_kind": CELL_KIND,
                      "sections": {k: v.get("verdict") for k, v in result["sections"].items()}}, sort_keys=True))
    return 0 if result["overall"] != "failed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
