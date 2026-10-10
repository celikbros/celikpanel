#!/usr/bin/env python3
"""set4b: what the Agent reads around a Stop of Postfix, measured inside a marked disposable QEMU guest.

The question (set4, item 10): on Ubuntu 24.04 a Stop of Postfix with a ``main.cf`` that ``postfix check`` refuses
leaves ``postfix@-.service`` marked ``failed``, and the answer carries no note. Nothing here corrects anything; the
modes only observe, with three instruments whose clocks can be laid over each other:

``read-agent-view``
    The exact commands the Agent sends to read a unit before and after a stop (``systemctl show <unit>
    --property=LoadState --property=ActiveState --property=Result``), as root, with their raw answers, and the
    master's process as the Agent looks for it (``postconf -h queue_directory``, ``<queue>/pid/master.pid``,
    ``/proc/<pid>/comm``).
``diag-arm`` / ``diag-collect``
    A kernel trace instance (tracefs, clock ``mono``; the events ``sched_process_fork``, ``sched_process_exec`` and
    ``sched_process_exit``): which programs the Agent's process started and when each ended, and when the Postfix
    master ended. The trace adds no stop to any process. Optionally ``strace`` attached to the Agent
    (``execve``, ``openat``, ``read``, ``write``, ``exit_group``): the argv of each command and what it printed.
    Optionally a sampler: the state systemd shows for both units and whether the master's process exists, read in a
    tight loop with CLOCK_MONOTONIC stamps. ``diag-collect`` returns a filtered view: only lines of the fixed
    programs (systemctl, postconf, postfix, postmulti, master, postfix-script) and of the two files named above are
    kept; nothing else the Agent reads or writes leaves the guest.
``owner-stop-by-hand``
    The server owner's own ``systemctl stop postfix`` with the moment it was sent and the moment it returned.

systemd's own ``...TimestampMonotonic`` properties of the units are CLOCK_MONOTONIC microseconds, the trace's clock
is ``mono``, and the sampler stamps with ``time.monotonic_ns()``: one clock for all three.

No mode prints a password, a private key or a hash, and no mode contacts another host.

set4b: Postfix durdurulurken Agent'ın ne okuduğunun işaretli geçici QEMU konuğunda ölçümü. Kipler yalnızca gözler;
hiçbiri bir şeyi düzeltmez, parola, özel anahtar ya da özet yazdırmaz, başka bir sunucuya bağlanmaz.
"""
from __future__ import annotations

import argparse
import base64
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
SCHEMA = "celikpanel/set4b-native/v1"
WORK = Path("/run/set4b-diag")
TRACE = Path("/sys/kernel/tracing/instances/set4b")
EVENTS = ("sched/sched_process_fork", "sched/sched_process_exec", "sched/sched_process_exit")
UNITS = ("postfix.service", "postfix@-.service")
AGENT_UNIT = "celikpanel-agent.service"
LABEL_RE = re.compile(r"[a-z0-9-]{1,40}\Z")
PROGRAMS = ("systemctl", "postconf", "postfix", "postmulti", "master", "postfix-script", "postsuper", "postlog")
PROGRAM_RE = re.compile(r"(?:^|[/=\s\"])(" + "|".join(re.escape(p) for p in PROGRAMS) + r")(?:[\s\",\]]|$)")
TIMESTAMPS = ("StateChangeTimestampMonotonic", "InactiveEnterTimestampMonotonic", "InactiveExitTimestampMonotonic",
              "ActiveEnterTimestampMonotonic", "ActiveExitTimestampMonotonic")
SAMPLER = r'''
import json, os, subprocess, sys, time
out, seconds, pid, pause = sys.argv[1], float(sys.argv[2]), int(sys.argv[3]), float(sys.argv[4])
argv = ["systemctl", "show", "postfix.service", "postfix@-.service", "--property=Id", "--property=LoadState",
        "--property=ActiveState", "--property=SubState", "--property=Result"]
env = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"}
def comm():
    try:
        with open("/proc/%d/comm" % pid) as handle:
            return handle.read().strip()
    except OSError as exc:
        return "!" + type(exc).__name__
end = time.monotonic() + seconds
with open(out, "w") as handle:
    while time.monotonic() < end:
        a = time.monotonic_ns(); c0 = comm()
        done = subprocess.run(argv, capture_output=True, env=env)
        b = time.monotonic_ns(); c1 = comm()
        units = {}
        for block in done.stdout.decode("utf-8", "replace").split("\n\n"):
            values = dict(line.split("=", 1) for line in block.splitlines() if "=" in line)
            if values.get("Id"):
                units[values["Id"]] = [values.get("LoadState"), values.get("ActiveState"), values.get("SubState"), values.get("Result")]
        handle.write(json.dumps([a, c0, b, c1, units]) + "\n"); handle.flush()
        if pause > 0:
            time.sleep(pause)
'''


def _load(name: str):
    spec = importlib.util.spec_from_file_location("set4b_" + name.replace(".", "_"), HERE / name)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


rid = _load("guest_request_identity_native.py")
run, utc, Refused = rid.run, rid.utc, rid.Refused


def label_of(args: dict) -> str:
    label = args.get("label")
    if not isinstance(label, str) or not LABEL_RE.fullmatch(label):
        raise Refused("not a label")
    return label


def clocks() -> dict:
    return {"monotonic_ns": time.monotonic_ns(), "realtime_ns": time.time_ns(), "utc": utc()}


def show(unit: str, properties) -> dict:
    argv = ["systemctl", "show", unit] + ["--property=" + name for name in properties]
    before = time.monotonic_ns()
    done = run(argv, timeout=30)
    return {"argv": argv, "returncode": done.get("returncode"), "stdout": done.get("stdout"), "stderr": done.get("stderr"),
            "sent_monotonic_ns": before, "answered_monotonic_ns": time.monotonic_ns()}


def master() -> dict:
    """The master's process, looked for the way the Agent does (cmd/agent/mail_service_verify.go, postfixMasterProcess)."""
    queue = run(["postconf", "-h", "queue_directory"], timeout=30)
    value = {"postconf": {k: queue.get(k) for k in ("argv", "returncode", "stdout", "stderr", "seconds")}}
    directory = (queue.get("stdout") or "").strip()
    if not directory.startswith("/"):
        return value
    path = os.path.join(directory, "pid", "master.pid")
    try:
        text = Path(path).read_text()
    except OSError as exc:
        value["pid_file"] = {"path": path, "error": type(exc).__name__}
        return value
    value["pid_file"] = {"path": path, "text": text}
    try:
        pid = int(text.strip())
    except ValueError:
        return value
    value["pid"] = pid
    try:
        value["comm"] = Path(f"/proc/{pid}/comm").read_text()
    except OSError as exc:
        value["comm_error"] = type(exc).__name__
    return value


def unit_times() -> dict:
    result = {}
    for unit in UNITS:
        done = run(["systemctl", "show", unit, "--property=LoadState", "--property=ActiveState", "--property=SubState",
                    "--property=Result", "--property=Type", "--property=ExecStop", "--property=PartOf", "--property=Before",
                    "--property=After", "--property=ConsistsOf", "--property=GuessMainPID", "--property=MainPID",
                    "--property=ControlPID", "--property=KillMode", "--property=FragmentPath"]
                   + ["--property=" + name for name in TIMESTAMPS], timeout=30)
        result[unit] = dict(line.split("=", 1) for line in (done.get("stdout") or "").splitlines() if "=" in line)
    return result


def read_agent_view(args: dict) -> dict:
    """What the Agent's two readings of a unit answer now, and the master as the Agent looks for it."""
    return {"clocks": clocks(),
            "readUnitFailure": {unit: show(unit, ("LoadState", "ActiveState", "Result")) for unit in UNITS},
            "postfixMasterProcess": master(), "units": unit_times(), "at": utc()}


def agent_pid() -> int:
    done = run(["systemctl", "show", AGENT_UNIT, "--property=MainPID"], timeout=30)
    text = (done.get("stdout") or "").strip().partition("=")[2]
    return int(text) if text.isdigit() else 0


def tids(pid: int) -> list:
    try:
        return sorted(int(name) for name in os.listdir(f"/proc/{pid}/task") if name.isdigit())
    except OSError:
        return []


def trace_write(name: str, text: str) -> None:
    (TRACE / name).write_text(text)


def diag_arm(args: dict) -> dict:
    label = label_of(args)
    WORK.mkdir(mode=0o700, exist_ok=True)
    state = {"label": label, "armed": clocks(), "agent_pid": agent_pid(), "master": master(), "units_at_arm": unit_times()}
    state["agent_tids_at_arm"] = tids(state["agent_pid"])
    # the kernel trace
    try:
        TRACE.mkdir(exist_ok=True)
        trace_write("tracing_on", "0")
        trace_write("trace_clock", "mono")
        trace_write("trace", "")
        for event in EVENTS:
            trace_write(f"events/{event}/enable", "1")
        trace_write("tracing_on", "1")
        state["trace"] = {"instance": str(TRACE), "clock": (TRACE / "trace_clock").read_text().strip(), "events": list(EVENTS)}
    except OSError as exc:
        state["trace"] = {"unavailable": f"{type(exc).__name__}: {exc}"[:200]}
    # strace on the Agent
    if args.get("strace"):
        program = shutil.which("strace", path="/usr/sbin:/usr/bin:/sbin:/bin")
        if not program or state["agent_pid"] <= 1:
            state["strace"] = {"unavailable": "no strace program on this guest" if not program else "no Agent main process"}
        else:
            out = WORK / f"strace-{label}.txt"
            process = subprocess.Popen([program, "-f", "-ttt", "-T", "-s", "400", "-e", "trace=execve,openat,read,write,exit_group",
                                        "-p", str(state["agent_pid"]), "-o", str(out)], stdin=subprocess.DEVNULL,
                                       stdout=subprocess.DEVNULL, stderr=open(WORK / f"strace-{label}.err", "wb"),
                                       start_new_session=True)
            time.sleep(1.0)
            state["strace"] = {"pid": process.pid, "program": program, "running": process.poll() is None,
                               "argv": "strace -f -ttt -T -s 400 -e trace=execve,openat,read,write,exit_group -p <Agent main PID>"}
    # the sampler
    if args.get("sampler"):
        pid = state["master"].get("pid") or 0
        script = WORK / "sampler.py"
        script.write_text(SAMPLER)
        out = WORK / f"samples-{label}.jsonl"
        seconds, pause = float(args.get("seconds", 14)), float(args.get("pause", 0.004))
        process = subprocess.Popen([sys.executable, "-I", str(script), str(out), str(seconds), str(pid), str(pause)],
                                   stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   start_new_session=True)
        state["sampler"] = {"pid": process.pid, "seconds": seconds, "pause_seconds": pause, "master_pid": pid}
    (WORK / f"state-{label}.json").write_text(json.dumps(state))
    state["at"] = utc()
    return state


def owner_stop_by_hand(args: dict) -> dict:
    """The server owner's own `systemctl stop postfix`: when it was sent and when it returned (CLOCK_MONOTONIC)."""
    sent = clocks()
    done = run(["systemctl", "stop", "postfix"], timeout=120)
    returned = clocks()
    return {"action": "owner-stop-by-hand", "argv": done.get("argv"), "returncode": done.get("returncode"),
            "stdout": done.get("stdout"), "stderr": done.get("stderr"), "sent": sent, "returned": returned,
            "seconds": round((returned["monotonic_ns"] - sent["monotonic_ns"]) / 1e9, 6), "at": utc()}


TRACE_LINE = re.compile(r"^\s*(?P<task>.+)-(?P<tid>\d+)\s+\[\d+\]\s+\S+\s+(?P<time>\d+\.\d+): (?P<event>sched_process_\w+): (?P<rest>.*)$")


def read_trace(state: dict) -> dict:
    try:
        trace_write("tracing_on", "0")
        text = (TRACE / "trace").read_text()
        for event in EVENTS:
            trace_write(f"events/{event}/enable", "0")
    except OSError as exc:
        return {"unavailable": f"{type(exc).__name__}: {exc}"[:200]}
    agent = set(state.get("agent_tids_at_arm") or []) | {state.get("agent_pid") or -1}
    master_pid = (state.get("master") or {}).get("pid") or -1
    sampler_pid = (state.get("sampler") or {}).get("pid") or -1
    strace_pid = (state.get("strace") or {}).get("pid") or -1
    parent, names, events = {}, {}, []
    lost = [line.strip() for line in text.splitlines() if "LOST" in line or "entries-in-buffer" in line]
    for line in text.splitlines():
        found = TRACE_LINE.match(line)
        if not found:
            continue
        tid, when, event, rest = int(found["tid"]), float(found["time"]), found["event"], found["rest"]
        fields = dict(re.findall(r"(\w+)=(\S+)", rest))
        if event == "sched_process_fork":
            child = int(fields.get("child_pid", "0"))
            parent[child] = int(fields.get("pid", "0"))
            continue
        if event == "sched_process_exec":
            names[int(fields.get("pid", tid))] = fields.get("filename", "")
        events.append({"mono": when, "event": event.replace("sched_process_", ""), "pid": int(fields.get("pid", tid)),
                       "comm": fields.get("comm") or found["task"].strip(), "filename": fields.get("filename")})

    def ancestry(pid: int) -> str:
        seen = 0
        while pid in parent and seen < 64:
            pid, seen = parent[pid], seen + 1
            if pid in agent:
                return "agent"
            if pid == sampler_pid:
                return "sampler"
            if pid == strace_pid:
                return "strace"
            if pid == 1:
                return "systemd"
        return "agent" if pid in agent else "other"

    kept = []
    for item in events:
        whose = "master" if item["pid"] == master_pid else ancestry(item["pid"])
        if whose in ("sampler", "strace"):
            continue
        name = os.path.basename(item["filename"] or names.get(item["pid"], "") or item["comm"])
        if whose == "other" and name not in PROGRAMS and item["comm"] not in PROGRAMS:
            continue
        if whose == "systemd" and name not in PROGRAMS and item["comm"] not in PROGRAMS and not item["comm"].startswith("(sd-"):
            continue
        if whose == "agent" and item["event"] == "exit" and item["pid"] not in names:
            continue    # a thread of the Agent's own process ending; it started no program
        kept.append({"mono": item["mono"], "event": item["event"], "pid": item["pid"], "program": name, "started_by": whose})
    return {"events": kept[:4000], "events_total": len(events), "buffer_notes": lost[:6]}


STRACE_LINE = re.compile(r"^(?P<pid>\d+)\s+(?P<time>\d+\.\d+)\s+(?P<body>.*)$")


def read_strace(label: str, state: dict) -> dict:
    info = state.get("strace") or {}
    if "pid" not in info:
        return info
    try:
        os.kill(info["pid"], signal.SIGINT)
    except OSError:
        pass
    for _ in range(50):
        try:
            os.kill(info["pid"], 0)
        except OSError:
            break
        time.sleep(0.1)
    path = WORK / f"strace-{label}.txt"
    try:
        lines = path.read_text(errors="replace").splitlines()
    except OSError as exc:
        return {"unavailable": type(exc).__name__}
    programs, kept, opened = {}, [], {}
    for line in lines:
        found = STRACE_LINE.match(line)
        if not found:
            continue
        pid, when, body = int(found["pid"]), found["time"], found["body"]
        if body.startswith("execve("):
            target = re.match(r'execve\("([^"]*)"', body)
            name = os.path.basename(target.group(1)) if target else ""
            if name in PROGRAMS:
                programs[pid] = name
                kept.append(f"{pid} {when} {body[:900]}")
            else:
                kept.append(f"{pid} {when} execve(\"{target.group(1) if target else '?'}\", [argv not kept]) {body.rsplit(' = ', 1)[-1][:60]}")
            continue
        if body.startswith("openat(") and ("master.pid" in body or re.search(r'"/proc/\d+/comm"', body)):
            kept.append(f"{pid} {when} {body[:300]}")
            result = body.rsplit(" = ", 1)[-1].split()[0]
            if result.isdigit():
                opened[pid] = result
            continue
        if body.startswith("read(") and pid in opened and body.startswith(f"read({opened[pid]},"):
            kept.append(f"{pid} {when} {body[:300]}")
            if '""' in body or " = 0 " in body + " ":
                opened.pop(pid, None)
            continue
        if pid in programs and (body.startswith("write(1,") or body.startswith("write(2,") or body.startswith("exit_group(")
                                 or body.startswith("+++ exited") or "+++ killed" in body):
            kept.append(f"{pid} {when} {body[:900]}")
            continue
        if "<... execve resumed>" in body and pid in programs:
            kept.append(f"{pid} {when} {body[:200]}")
    error = ""
    try:
        error = (WORK / f"strace-{label}.err").read_text(errors="replace")[:600]
    except OSError:
        pass
    return {"lines": kept[:1500], "lines_total": len(lines), "kept": len(kept), "stderr": error,
            "kept_rule": "execve of the fixed programs with argv, any other execve by path only, openat and the following "
                         "read of master.pid and /proc/<pid>/comm, write to stdout or stderr and exit of the fixed programs"}


def read_samples(label: str, state: dict) -> dict:
    info = state.get("sampler") or {}
    if "pid" not in info:
        return info
    for _ in range(200):
        try:
            os.kill(info["pid"], 0)
        except OSError:
            break
        time.sleep(0.1)
    rows = []
    try:
        for line in (WORK / f"samples-{label}.jsonl").read_text().splitlines():
            rows.append(json.loads(line))
    except (OSError, ValueError) as exc:
        return {"unavailable": type(exc).__name__}
    # every sample around a change, and one in twenty elsewhere
    kept, previous = [], None
    for index, row in enumerate(rows):
        shape = (row[1], row[3], json.dumps(row[4], sort_keys=True))
        if shape != previous or index % 20 == 0 or index == len(rows) - 1:
            if shape != previous and index > 0 and (not kept or kept[-1] is not rows[index - 1]):
                kept.append(rows[index - 1])
            kept.append(row)
        previous = shape
    return {"samples_total": len(rows), "columns": ["read_started_monotonic_ns", "master_comm_before", "read_answered_monotonic_ns",
                                                    "master_comm_after", "units: [LoadState, ActiveState, SubState, Result]"],
            "kept_rule": "every sample at which anything differs from the one before, the sample before it, and one in twenty",
            "samples": kept[:1200], "seconds": info.get("seconds"), "pause_seconds": info.get("pause_seconds"),
            "master_pid": info.get("master_pid"),
            "median_read_ms": (sorted((r[2] - r[0]) / 1e6 for r in rows)[len(rows) // 2] if rows else None)}


def diag_collect(args: dict) -> dict:
    label = label_of(args)
    try:
        state = json.loads((WORK / f"state-{label}.json").read_text())
    except (OSError, ValueError):
        raise Refused("nothing was armed under this label")
    result = {"label": label, "armed": state.get("armed"), "collected": clocks(), "agent_pid": state.get("agent_pid"),
              "master_at_arm": state.get("master"), "units_at_arm": state.get("units_at_arm")}
    result["strace"] = read_strace(label, state)
    result["samples"] = read_samples(label, state)
    result["trace"] = read_trace(state)
    result["units_at_collect"] = unit_times()
    result["master_at_collect"] = master()
    result["agent_pid_at_collect"] = agent_pid()
    result["at"] = utc()
    return result


MODES = {"read-agent-view": read_agent_view, "diag-arm": diag_arm, "diag-collect": diag_collect,
         "owner-stop-by-hand": owner_stop_by_hand}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("mode", choices=sorted(MODES))
    for name in ("lab-nonce", "vm-uuid", "cell-id", "node"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--args-b64", default="e30=")
    args = parser.parse_args(argv)
    rid._load_probe().guard_guest(args)
    request = json.loads(base64.b64decode(args.args_b64, validate=True))
    if not isinstance(request, dict):
        parser.error("--args-b64 must hold a JSON object")
    value = MODES[args.mode](request)
    value.update(schema=SCHEMA, mode=args.mode, owner_action=args.mode.startswith("owner-"))
    print(json.dumps(value, sort_keys=True, default=str))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Refused as exc:
        print(json.dumps({"refused": "Refused", "reason": str(exc)[:300]}), file=sys.stderr)
        raise SystemExit(3)
    except Exception as exc:  # noqa: BLE001 - reported as data; the driver decides
        print(json.dumps({"refused": type(exc).__name__, "reason": str(exc)[:300]}), file=sys.stderr)
        raise SystemExit(2)
