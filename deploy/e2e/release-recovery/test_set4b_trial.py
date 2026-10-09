"""Offline rules of the set4b driver and of its guest helper: no guest, no network, nothing started.

The kernel trace, the strace record and the sampler's rows are read here from texts in the shapes those tools print;
the timeline is built from a collected record as the helper returns it.
"""
from __future__ import annotations

import importlib.util
import inspect
import json
from pathlib import Path
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set4b_trial as s4b  # noqa: E402


def load_helper():
    spec = importlib.util.spec_from_file_location("set4b_guest_helper", HERE / "guest_set4b_native.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


TRACE_TEXT = """# tracer: nop
#
# entries-in-buffer/entries-written: 12/12   #P:2
#
#           TASK-PID     CPU#  |||||  TIMESTAMP  FUNCTION
#              | |         |   |||||     |         |
 celikpanel-agen-812     [000] ...1.   100.000100: sched_process_fork: comm=celikpanel-agen pid=812 child_comm=celikpanel-agen child_pid=9001
       systemctl-9001    [001] ...1.   100.000900: sched_process_exec: filename=/usr/bin/systemctl pid=9001 old_pid=9001
       systemctl-9001    [001] ...1.   100.010000: sched_process_exit: comm=systemctl pid=9001 prio=120
 celikpanel-agen-815     [000] ...1.   100.020000: sched_process_fork: comm=celikpanel-agen pid=815 child_comm=celikpanel-agen child_pid=9002
       systemctl-9002    [001] ...1.   100.021000: sched_process_exec: filename=/usr/bin/systemctl pid=9002 old_pid=9002
       systemctl-9002    [001] ...1.   100.050000: sched_process_exit: comm=systemctl pid=9002 prio=120
         systemd-1       [000] ...1.   100.045000: sched_process_fork: comm=systemd pid=1 child_comm=(sd-exec) child_pid=9100
       postmulti-9100    [001] ...1.   100.046000: sched_process_exec: filename=/usr/sbin/postmulti pid=9100 old_pid=9100
       postmulti-9100    [001] ...1.   101.060000: sched_process_exit: comm=postmulti pid=9100 prio=120
          master-700     [000] ...1.   101.063000: sched_process_exit: comm=master pid=700 prio=120
 celikpanel-agen-812     [000] ...1.   101.070000: sched_process_fork: comm=celikpanel-agen pid=812 child_comm=celikpanel-agen child_pid=9003
       systemctl-9003    [001] ...1.   101.071000: sched_process_exec: filename=/usr/bin/systemctl pid=9003 old_pid=9003
       systemctl-9003    [001] ...1.   101.080000: sched_process_exit: comm=systemctl pid=9003 prio=120
          python3-9500   [000] ...1.   101.090000: sched_process_exec: filename=/usr/bin/python3 pid=9500 old_pid=9500
 celikpanel-agen-813     [000] ...1.   101.095000: sched_process_exit: comm=celikpanel-agen pid=813 prio=120
"""

STRACE_TEXT = """812 1791000000.000100 openat(AT_FDCWD, "/var/spool/postfix/pid/master.pid", O_RDONLY|O_CLOEXEC) = 9 <0.000020>
812 1791000000.000200 read(9, "            700\\n", 512) = 16 <0.000010>
812 1791000000.000300 read(9, "", 496) = 0 <0.000010>
812 1791000000.000400 openat(AT_FDCWD, "/proc/700/comm", O_RDONLY|O_CLOEXEC) = 9 <0.000020>
812 1791000000.000500 read(9, "master\\n", 512) = 7 <0.000010>
812 1791000000.000550 read(7, "a body of the Agent's own that is never kept", 4096) = 44 <0.000010>
9001 1791000000.000900 execve("/usr/bin/systemctl", ["/usr/bin/systemctl", "show", "postfix@-.service", "--property=LoadState", "--property=ActiveState", "--property=Result"], 0xc000 /* 9 vars */) = 0 <0.000300>
9001 1791000000.009000 write(1, "LoadState=loaded\\nActiveState=deactivating\\nResult=success\\n", 57) = 57 <0.000010>
9001 1791000000.009500 exit_group(0) = ?
9001 1791000000.009600 +++ exited with 0 +++
9300 1791000000.100000 execve("/usr/bin/some-other-program", ["some-other-program", "--password=never-kept"], 0xc000 /* 9 vars */) = 0 <0.000300>
9300 1791000000.100500 write(1, "its output is never kept", 24) = 24 <0.000010>
"""


class Timeline(unittest.TestCase):
    def collected(self):
        return {"trace": {"events": [
            {"mono": 100.0009, "event": "exec", "pid": 9001, "program": "systemctl", "started_by": "agent"},
            {"mono": 100.0100, "event": "exit", "pid": 9001, "program": "systemctl", "started_by": "agent"},
            {"mono": 100.0210, "event": "exec", "pid": 9002, "program": "systemctl", "started_by": "agent"},
            {"mono": 100.0500, "event": "exit", "pid": 9002, "program": "systemctl", "started_by": "agent"},
            {"mono": 100.0460, "event": "exec", "pid": 9100, "program": "postmulti", "started_by": "systemd"},
            {"mono": 101.0630, "event": "exit", "pid": 700, "program": "master", "started_by": "master"},
            {"mono": 101.0710, "event": "exec", "pid": 9003, "program": "systemctl", "started_by": "agent"},
            {"mono": 101.0800, "event": "exit", "pid": 9003, "program": "systemctl", "started_by": "agent"}]},
            "units_at_collect": {"postfix@-.service": {"ActiveState": "failed", "SubState": "failed", "Result": "exit-code",
                                                       "ActiveExitTimestampMonotonic": "100045000",
                                                       "InactiveEnterTimestampMonotonic": "101067000"}}}

    def test_one_clock_for_the_agent_the_master_and_systemd(self):
        line = s4b.timeline(self.collected())
        self.assertEqual(line["left_active_mono"], 100.045)
        self.assertEqual(line["settled_mono"], 101.067)
        self.assertEqual(line["master_ended_mono"], 101.063)
        self.assertEqual([p["program"] for p in line["agent_programs"]], ["systemctl", "systemctl", "systemctl"])
        self.assertEqual(line["agent_programs"][1]["ended"], 100.05)
        # the unit settled 4 ms after the master ended; the Agent's last systemctl started 4 ms after that
        self.assertEqual(line["unit_settled_after_the_master_ended_ms"], 4.0)
        self.assertEqual(line["last_systemctl_started_after_the_unit_settled_ms"], 4.0)
        # the stop command is the systemctl whose run holds the moment the unit left `active`
        self.assertEqual(line["stop_command_returned_after_the_unit_left_active_ms"], 5.0)
        self.assertEqual(line["state_at_collect"], {"ActiveState": "failed", "SubState": "failed", "Result": "exit-code"})

    def test_nothing_is_derived_from_what_was_not_read(self):
        line = s4b.timeline({"trace": {"unavailable": "PermissionError"}, "units_at_collect": {}})
        self.assertIsNone(line["settled_mono"])
        self.assertIsNone(line["master_ended_mono"])
        self.assertEqual(line["agent_programs"], [])
        self.assertNotIn("last_systemctl_started_after_the_unit_settled_ms", line)
        self.assertIsNone(s4b.micros("0"))
        self.assertIsNone(s4b.micros("n/a"))
        self.assertEqual(s4b.micros("12"), 12)


class Driver(unittest.TestCase):
    def test_the_helpers_own_argument_named_label_reaches_the_helper(self):
        # run-a of the diagnostic cell stopped here: `label` was both the file label and the helper's argument
        for name in ("snap4b", "owner4b", "native4b"):
            parameters = list(inspect.signature(getattr(s4b.Set4bTrial, name)).parameters.values())
            self.assertTrue(all(p.kind is p.POSITIONAL_ONLY for p in parameters[:-1]), name)
            self.assertIs(parameters[-1].kind, parameters[-1].VAR_KEYWORD, name)
            self.assertNotIn("label", [p.name for p in parameters], name)

    def test_cells_and_their_sections(self):
        self.assertEqual([key for key, _ in s4b.sections_of("set4b-diag-ubuntu")], ["M0-prepare", s4b.DIAG])
        for cell in ("set4b-ubuntu", "set4b-debian13"):
            self.assertEqual([key for key, _ in s4b.sections_of(cell)],
                             ["M0-prepare", "M10-postfix-stop", "R10-postfix-stop-repeated", "M2-import"])
            self.assertTrue(s4b.CELLS[cell].mail)
        self.assertEqual(s4b.CELLS["set4b-ubuntu"].platform, "ubuntu")
        self.assertEqual(s4b.CELLS["set4b-debian13"].platform, "debian13-arch")
        artifacts = {"baseline": {"version": "v0.1.0-alpha.81", "commit": "a" * 40, "sha256": "b" * 64}}
        plan = s4b.build_plan("set4b-ubuntu", artifacts, "/var/tmp/cp-release-drill-x", 18443)
        self.assertEqual(plan["steps"][-5:], ["M0-prepare", "M10-postfix-stop", "R10-postfix-stop-repeated", "M2-import", "collect"])
        self.assertIs(plan["native_evidence"], False)


class GuestHelper(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.helper = load_helper()

    def test_the_kernel_trace_is_read_by_who_started_what(self):
        helper = self.helper
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "events" / "sched").mkdir(parents=True)
            for event in helper.EVENTS:
                (root / "events" / event).mkdir(parents=True, exist_ok=True)
            (root / "trace").write_text(TRACE_TEXT)
            previous, helper.TRACE = helper.TRACE, root
            try:
                value = helper.read_trace({"agent_pid": 812, "agent_tids_at_arm": [812, 813, 815], "master": {"pid": 700},
                                           "sampler": {"pid": 9400}})
            finally:
                helper.TRACE = previous
            self.assertEqual((root / "tracing_on").read_text(), "0")
        events = value["events"]
        agent = [e for e in events if e["started_by"] == "agent"]
        self.assertEqual([(e["event"], e["pid"], e["program"]) for e in agent],
                         [("exec", 9001, "systemctl"), ("exit", 9001, "systemctl"), ("exec", 9002, "systemctl"),
                          ("exit", 9002, "systemctl"), ("exec", 9003, "systemctl"), ("exit", 9003, "systemctl")])
        self.assertEqual([(e["event"], e["mono"]) for e in events if e["started_by"] == "master"], [("exit", 101.063)])
        self.assertEqual([(e["event"], e["program"]) for e in events if e["started_by"] == "systemd"],
                         [("exec", "postmulti"), ("exit", "postmulti")])
        # a program that is none of the fixed ones and was started by nobody of interest is not kept,
        # nor is the end of a thread of the Agent's own process
        self.assertFalse([e for e in events if e["program"] in ("python3", "celikpanel-agen")])

    def test_the_strace_record_keeps_only_the_fixed_programs_and_the_two_files(self):
        helper = self.helper
        with tempfile.TemporaryDirectory() as directory:
            previous, helper.WORK = helper.WORK, Path(directory)
            try:
                (helper.WORK / "strace-t1.txt").write_text(STRACE_TEXT)
                value = helper.read_strace("t1", {"strace": {"pid": 2 ** 22 + 12345}})
            finally:
                helper.WORK = previous
        text = "\n".join(value["lines"])
        self.assertIn('openat(AT_FDCWD, "/proc/700/comm"', text)
        self.assertIn('read(9, "master\\n"', text)
        self.assertIn('"show", "postfix@-.service", "--property=LoadState"', text)
        self.assertIn("ActiveState=deactivating", text)
        self.assertIn("+++ exited with 0 +++", text)
        self.assertIn('execve("/usr/bin/some-other-program", [argv not kept])', text)
        for never in ("never-kept", "its output is never kept", "a body of the Agent's own"):
            self.assertNotIn(never, text)

    def test_a_label_is_a_plain_word_and_the_modes_are_readers_or_owner_actions(self):
        helper = self.helper
        self.assertEqual(helper.label_of({"label": "p2a"}), "p2a")
        for bad in ("../x", "P1", "", None, "a b"):
            with self.assertRaises(helper.Refused):
                helper.label_of({"label": bad})
        self.assertEqual(sorted(helper.MODES), ["diag-arm", "diag-collect", "owner-stop-by-hand", "read-agent-view"])
        source = (HERE / "guest_set4b_native.py").read_text()
        for never in ("reset-failed", '"start"', '"restart"', '"reload"', "daemon-reload"):
            self.assertNotIn(never, source)
        # the one command that changes a unit is the owner's own stop
        self.assertEqual(source.count('["systemctl", "stop", "postfix"]'), 1)

    def test_the_sampler_is_a_program_of_its_own_that_only_reads(self):
        compile(self.helper.SAMPLER, "sampler", "exec")
        self.assertIn('"show", "postfix.service", "postfix@-.service"', self.helper.SAMPLER)
        self.assertNotIn("stop", self.helper.SAMPLER)
        rows = [[1, "master", 2, "master", {"postfix@-.service": ["loaded", "active", "running", "success"]}]] * 45 + \
               [[3, "!FileNotFoundError", 4, "!FileNotFoundError", {"postfix@-.service": ["loaded", "failed", "failed", "exit-code"]}]] * 3
        with tempfile.TemporaryDirectory() as directory:
            previous, self.helper.WORK = self.helper.WORK, Path(directory)
            try:
                (self.helper.WORK / "samples-t2.jsonl").write_text("\n".join(json.dumps(r) for r in rows) + "\n")
                value = self.helper.read_samples("t2", {"sampler": {"pid": 2 ** 22 + 12346, "seconds": 1, "pause_seconds": 0}})
            finally:
                self.helper.WORK = previous
        self.assertEqual(value["samples_total"], 48)
        kept = value["samples"]
        # the change is kept with the sample before it
        index = next(i for i, row in enumerate(kept) if row[1] == "!FileNotFoundError")
        self.assertEqual(kept[index - 1][1], "master")
        self.assertLess(len(kept), 12)


if __name__ == "__main__":
    unittest.main()
