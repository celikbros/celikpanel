#!/usr/bin/env python3
"""Offline tests: resuming the fresh primary's pending zone deletion.

Batch 9 z04/z05 (zero-zone fresh paired PowerDNS primary): the lifecycle's
delete of the child s2.s1-kill.test stayed pending with
dns_peer_enrollment_required; after the owner's enrollment the trigger's
recover command refused because it matched the job against the parent's
name. These tests cover the corrected resume path:

1. the trigger's recover matches the lifecycle's own zone (source check; the
   Go tests replay the exact batch 9 phase);
2. zone-lifecycle --recover-delete resumes the delete, then requires both
   catalogs back at zero members and the child REFUSED on both servers, then
   re-adds and requires the child in both catalogs with its re-add serial;
3. run-prepared --zone-lifecycle --reboot-after-recovery keeps the guest run
   suspended on a pending delete (no "not-passed" continuation), and
   --resume-held-zone-lifecycle recovers, re-adds, continues the run and
   reboots both guests (z05).

Nothing here starts a guest, runs --execute, talks to an Agent or a peer, or
reboots anything. Nothing here is native evidence.
"""

from __future__ import annotations

import argparse
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
import native_pdns_bind_peer as bind_peer
import test_fresh_primary_serial_zero_zone as zero
import test_fresh_primary_v3 as base

HERE = Path(__file__).parent
README = (HERE / "README.md").read_text(encoding="utf-8")
TRIGGER_SOURCE = HERE.parents[2] / "cmd" / "dns-kill-matrix-trigger" / "pdns_fresh_primary_v3.go"
CHILD = "s2.s1-kill.test"
ZONE_SCHEMA = "celikpanel/dns-kill-matrix-pdns-primary-zone-v3/v1"
Z05 = "pdns-switch__target-started__after-write__paired-primary__peer-reachable"
Z04 = "pdns-switch__committed__after-write__paired-primary__peer-reachable"


def trigger_result(step: str, outcome: str, **extra: object) -> str:
    value = {"schema": ZONE_SCHEMA, "step": step, "domain": CHILD, "outcome": outcome, **extra}
    return json.dumps(value) + "\n"


class TriggerSourceTest(unittest.TestCase):
    def test_recover_matches_the_lifecycle_zone_not_the_trial_parent(self) -> None:
        source = TRIGGER_SOURCE.read_text(encoding="utf-8")
        recover = source.split("func recoverFreshPrimaryZoneDeletion(", 1)[1].split("\nfunc ", 1)[0]
        self.assertNotIn("exactPendingDeletionJob(", recover)
        self.assertEqual(recover.count(
            "exactPendingZoneDeletionJob(before.Job, begin, freshPrimaryZoneDomain)"), 1)
        self.assertEqual(recover.count(
            "exactPendingZoneDeletionJob(job, begin, freshPrimaryZoneDomain)"), 1)
        self.assertRegex(source, r'freshPrimaryZoneDomain\s+= "s2\.s1-kill\.test"')
        self.assertEqual(bootstrap.ZONE_LIFECYCLE_CHILD, CHILD)
        self.assertEqual(bind_peer.CHILD, CHILD)


class RecoverLifecycleTest(unittest.TestCase):
    """zone-lifecycle --recover-delete against a replayed pair."""

    def args(self, **changes: object) -> argparse.Namespace:
        values: dict = dict(
            work_root=Path("/w"), cell_id=Z04, manifest=Path("/m"), node="debian13",
            identity_file=Path("/k"), source_fixture="uninitialized", execute=True,
            zero_zones=True, recover_delete=True)
        values.update(changes)
        return argparse.Namespace(**values)

    def run_lifecycle(self, root: Path, outcomes: dict[str, str], *, served_after: dict,
                      recover_delete: bool = True, args: argparse.Namespace | None = None,
                      job_error_codes: dict[str, str] | None = None):
        """served_after[step]: (catalog members, child SOA answer) after that step.

        job_error_codes[step], when given, is the trigger's job_error_code for
        that step (as the Agent's ledger reports it), so a pending outcome can
        be judged against a real code instead of leaving it absent.
        """

        remotes: list[str] = []
        observed: list[tuple[str, bool]] = []
        written: dict[str, dict] = {}
        state = {"step": None}
        catalog = bind_peer.catalog_name("192.0.2.10")

        def run(command, **kwargs):
            remote = command[-1]
            remotes.append(remote)
            step = remote.split("--step ", 1)[1].split()[0]
            state["step"] = step
            extra = {}
            if job_error_codes and step in job_error_codes:
                extra["job_error_code"] = job_error_codes[step]
            return subprocess.CompletedProcess(command, 0, stdout=trigger_result(
                step, outcomes[step], **extra), stderr="")

        def read(ssh, command, execute):
            members, child = served_after[state["step"]]
            if command.startswith("systemctl"):
                return ""
            if f" {catalog} AXFR" in command:
                return zero.empty_catalog_axfr(1790800002, members)
            header = ";; ->>HEADER<<- opcode: QUERY, status: "
            if f" {CHILD} SOA" in command:
                if child == "REFUSED":
                    return header + "REFUSED\n;; flags: qr;\n"
                if child == "NXDOMAIN":
                    return header + "NXDOMAIN\n;; flags: qr aa;\n"
                return (header + "NOERROR\n;; flags: qr aa;\n"
                        f"{CHILD}. 300 IN SOA ns1.s1-kill.test. hostmaster.s1-kill.test. "
                        f"{child} 10800 3600 604800 3600\n")
            if f" www.{CHILD} A" in command:
                return header + f"NOERROR\n;; flags: qr aa;\nwww.{CHILD}. 300 IN A 192.0.2.10\n"
            return header + "NXDOMAIN\n;; flags: qr aa;\n"

        real_observe_child = bind_peer.observe_child

        def observe_child(namespace):
            observed.append((namespace.step, namespace.zero_zones))
            return real_observe_child(namespace)

        real_converge = bootstrap.observe_until_converged

        def write(plan, name, value, **kwargs):
            written[name] = value
            return name

        with mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch.object(bootstrap.subprocess, "run", side_effect=run), \
                mock.patch.object(bootstrap, "write_peer_evidence", side_effect=write), \
                mock.patch.object(bootstrap, "observe_until_converged",
                                  side_effect=lambda observe: real_converge(observe, attempts=1)), \
                mock.patch.object(bind_peer, "observe_child", side_effect=observe_child), \
                mock.patch.object(bind_peer, "selected",
                                  return_value=("192.0.2.10", "192.0.2.11", {}, Path("/k"))), \
                mock.patch.object(bind_peer, "verify_guest"), \
                mock.patch.object(bind_peer, "remote_read", side_effect=read), \
                mock.patch("sys.stdout", new_callable=io.StringIO):
            plan = {"nodes": {"debian13": {}}, "cell_directory": str(root)}
            result = bootstrap.run_zone_lifecycle_status(
                args or self.args(), plan, recover_delete=recover_delete)
        return result, remotes, observed, written

    def test_resumed_delete_is_refused_on_both_then_the_child_is_re_added(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            (code, status, pending), remotes, observed, written = self.run_lifecycle(
                Path(root), {"delete": "verified_published", "re-add": "verified_published"},
                served_after={"delete": ((), "REFUSED"), "re-add": ((CHILD,), "2026092803")})
        self.assertEqual((code, status, pending), (0, "passed", None))
        self.assertEqual(len(remotes), 2)
        self.assertIn("dns-kill-trigger rpc-pdns-primary-zone-v3-recover ", remotes[0])
        self.assertIn("--step delete", remotes[0])
        self.assertIn("dns-kill-trigger rpc-pdns-primary-zone-v3 ", remotes[1])
        self.assertIn("--step re-add", remotes[1])
        self.assertEqual(observed, [("delete", True), ("re-add", True)])
        record = written["zone-lifecycle-recover.json"]
        self.assertEqual(record["status"], "passed")
        delete, re_add = record["steps"]
        self.assertEqual((delete["step"], delete["recover"]), ("delete", True))
        self.assertEqual(delete["observation"]["catalog_members_expected"], [])
        self.assertEqual({value["soa"] for value in delete["observation"]["answers"].values()},
                         {"REFUSED"})
        self.assertEqual(len(delete["observation"]["answers"]), 4)
        self.assertEqual(re_add["observation"]["catalog_members_expected"], [CHILD])
        self.assertEqual({value["soa_serial"] for value in re_add["observation"]["answers"].values()},
                         {"2026092803"})

    def test_a_child_still_listed_or_denied_by_a_parent_fails_before_the_re_add(self) -> None:
        for served, name in ((((CHILD,), "REFUSED"), "still a catalog member"),
                             (((), "NXDOMAIN"), "answered as if a parent were served")):
            with self.subTest(name), tempfile.TemporaryDirectory() as root:
                (code, status, pending), remotes, observed, written = self.run_lifecycle(
                    Path(root), {"delete": "verified_published", "re-add": "verified_published"},
                    served_after={"delete": served})
                self.assertEqual((code, status, pending), (1, "failed", None))
                self.assertEqual(len(remotes), 1, "the re-add ran after a failed delete")
                self.assertTrue(written["zone-lifecycle-recover.json"]["steps"][0]
                                ["observation_error"].startswith("mismatch:"))

    def test_a_refused_or_still_pending_recovery_does_not_re_add(self) -> None:
        for outcome, status in (("refused_not_pending", "unverified"),
                                ("pending_exact_operation", "pending")):
            with self.subTest(outcome), tempfile.TemporaryDirectory() as root:
                (code, got, pending), remotes, observed, written = self.run_lifecycle(
                    Path(root), {"delete": outcome}, served_after={},
                    job_error_codes={"delete": "dns_peer_enrollment_required"}
                    if outcome == "pending_exact_operation" else None)
                self.assertEqual((code, got), (2, status))
                self.assertEqual((len(remotes), observed), (1, []))
                entry = written["zone-lifecycle-recover.json"]["steps"][0]
                if status == "pending":
                    self.assertEqual(pending["job_error_code"], "dns_peer_enrollment_required")
                    self.assertIn("--zero-zones --recover-delete --execute", entry["next_step"])
                    self.assertIn(CHILD, entry["next_step"])
                    self.assertIn("dns_peer_enrollment_required", entry["next_step"])
                else:
                    self.assertIsNone(pending)

    def test_pending_next_step_depends_on_the_agents_own_code(self) -> None:
        """dns_peer_inspection_unknown (batch 10) and an unnamed code each get
        their own guidance, never the enrollment text for the wrong code."""

        cases = {
            "dns_peer_inspection_unknown": (
                "inspector exchange ran and did not produce an observation",
                "rndc key",
            ),
            "some_future_code": (
                "the harness has no owner step for some_future_code",
            ),
        }
        for code, expect in cases.items():
            with self.subTest(code), tempfile.TemporaryDirectory() as root:
                (code_exit, status, pending), remotes, observed, written = self.run_lifecycle(
                    Path(root), {"delete": "pending_exact_operation"}, served_after={},
                    job_error_codes={"delete": code})
                self.assertEqual((code_exit, status), (2, "pending"))
                self.assertEqual(pending["job_error_code"], code)
                entry = written["zone-lifecycle-recover.json"]["steps"][0]
                for text in expect:
                    self.assertIn(text, entry["next_step"])
                self.assertNotIn("dns-peer-enroll --engine bind", entry["next_step"])

    def test_each_recovery_attempt_has_its_own_evidence_and_never_replaces_one(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root) / bootstrap.FRESH_PRIMARY_EVIDENCE_DIRECTORY
            directory.mkdir()
            plan = {"cell_directory": root}
            self.assertEqual(bootstrap.zone_lifecycle_evidence_name(plan, False), "zone-lifecycle.json")
            self.assertEqual(bootstrap.zone_lifecycle_evidence_name(plan, True),
                             "zone-lifecycle-recover.json")
            (directory / "zone-lifecycle.json").write_text("{}", encoding="utf-8")
            (directory / "zone-lifecycle-recover.json").write_text("{}", encoding="utf-8")
            self.assertEqual(bootstrap.zone_lifecycle_evidence_name(plan, True),
                             "zone-lifecycle-recover-2.json")
            with self.assertRaisesRegex(bootstrap.BootstrapError, "already ran"):
                bootstrap.zone_lifecycle_evidence_name(plan, False)
            for attempt in range(2, bootstrap.ZONE_LIFECYCLE_RECOVER_ATTEMPTS + 1):
                (directory / f"zone-lifecycle-recover-{attempt}.json").write_text(
                    "{}", encoding="utf-8")
            # Refused before any RPC.
            with mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                    mock.patch.object(bootstrap.subprocess, "run") as run, \
                    self.assertRaisesRegex(bootstrap.BootstrapError, "already ran"):
                bootstrap.run_zone_lifecycle_status(
                    self.args(), {"nodes": {"debian13": {}}, "cell_directory": root},
                    recover_delete=True)
            run.assert_not_called()


class HeldLifecycleHostTest(unittest.TestCase):
    """z05: pending delete -> guest kept suspended -> enrollment -> resume + reboot."""

    RAW = base.raw_cell(Z05)

    def args(self, **changes: object) -> argparse.Namespace:
        values = dict(
            work_root=Path("/w"), cell_id=self.RAW["id"], manifest=Path("/m"), node="debian13",
            identity_file=Path("/k"), source_fixture="uninitialized", execute=True,
            owner_edit=None, owner_release_recovery=False, zone_lifecycle=True,
            owner_directives=False, reboot_after_recovery=True,
            disable_management_before_reboot=True, stop_after_kill_for_independent_recovery=False,
            zero_zones=True, resume_held_zone_lifecycle=False,
        )
        values.update(changes)
        return argparse.Namespace(**values)

    def run_host(self, root: str, codes: list[int], lifecycle: list[tuple[int, str]],
                 **changes: object):
        events: list[str] = []
        runs: list[str] = []
        output = io.StringIO()

        def run(command, check=False):
            runs.append(command[-1])
            events.append("guest")
            return subprocess.CompletedProcess(command, codes[len(runs) - 1])

        def peer_verdict(args, plan, guest_returncode, **kwargs):
            events.append("peer-after-reboot" if kwargs.get("child_step") else "peer")
            return bootstrap.worst_exit(guest_returncode, 0)

        def reboot(plan, node, identity, timeout):
            events.append("reboot " + node)
            return {"node": node}

        def zone_lifecycle(args, plan, *, recover_delete=False):
            events.append("recover" if recover_delete else "lifecycle")
            return lifecycle.pop(0)

        plan = {"cell_directory": root}
        with mock.patch.object(bootstrap, "load_plan", return_value=(plan, self.RAW, {"n": 1})), \
                mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch.object(bootstrap.subprocess, "run", side_effect=run), \
                mock.patch.object(bootstrap, "finish_fresh_primary_peer_verdict",
                                  side_effect=peer_verdict), \
                mock.patch.object(bootstrap, "run_zone_lifecycle_status",
                                  side_effect=zone_lifecycle), \
                mock.patch.object(bootstrap, "verify_guest_zone_set"), \
                mock.patch.object(bootstrap.fixture, "reboot_guest", side_effect=reboot), \
                mock.patch("sys.stdout", output):
            code = bootstrap.run_prepared(self.args(**changes))
        return code, events, runs, output.getvalue()

    def held_path(self, root: str) -> Path:
        return (Path(root) / bootstrap.FRESH_PRIMARY_EVIDENCE_DIRECTORY
                / bootstrap.ZONE_LIFECYCLE_HELD_EVIDENCE)

    def test_pending_delete_keeps_the_guest_suspended_then_resume_reboots(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            code, events, runs, _ = self.run_host(
                root, [5],
                [(2, "pending", {"domain": CHILD, "job_error_code": "dns_peer_enrollment_required"})])
            self.assertEqual(code, 2)
            # No continuation: the guest run stays suspended on this boot.
            self.assertEqual(events, ["guest", "peer", "lifecycle"])
            self.assertEqual(len(runs), 1)
            held = json.loads(self.held_path(root).read_text(encoding="utf-8"))
            self.assertEqual((held["cell_id"], held["zero_zones"], held["peer_verdict_exit"],
                              held["lifecycle_status"]), (Z05, True, 0, "pending"))
            self.assertIn(bootstrap.RESUME_HELD_LIFECYCLE_FLAG, held["next_step"])
            self.assertIn("--zero-zones", held["next_step"])
            self.assertIn(CHILD, held["next_step"])
            self.assertIn("dns_peer_enrollment_required", held["next_step"])

            # After the owner's enrollment: recover + re-add, continue, reboot.
            code, events, runs, _ = self.run_host(
                root, [3, 0], [(0, "passed", None)], resume_held_zone_lifecycle=True)
            self.assertEqual(code, 0)
            self.assertEqual(events, ["recover", "guest", "reboot arch", "reboot debian13",
                                      "guest", "peer-after-reboot"])
            self.assertTrue(runs[0].endswith(bootstrap.ZONE_LIFECYCLE_OUTCOME_PREFIX + "passed"))
            self.assertIn(bootstrap.DISABLE_MANAGEMENT_FLAG, runs[0])
            self.assertNotIn(bootstrap.RESUME_HELD_LIFECYCLE_FLAG, runs[0])
            self.assertTrue(runs[1].endswith(bootstrap.RESUME_FLAG))

    def test_resume_that_is_still_pending_stays_suspended_and_a_failure_continues(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            self.run_host(
                root, [5],
                [(2, "pending", {"domain": CHILD, "job_error_code": "dns_peer_enrollment_required"})])
            before = self.held_path(root).read_bytes()
            # Batch 10: after enrollment the same job stayed pending with a
            # different code. The held record is not rewritten on a resume
            # (still the same bytes), but the printed record for this attempt
            # must carry the new code, not the old one or an empty string.
            code, events, runs, output = self.run_host(
                root, [],
                [(2, "pending", {"domain": CHILD, "job_error_code": "dns_peer_inspection_unknown"})],
                resume_held_zone_lifecycle=True)
            self.assertEqual((code, events, runs), (2, ["recover"], []))
            self.assertEqual(self.held_path(root).read_bytes(), before)
            printed = json.loads(output.splitlines()[-1])
            self.assertIn("dns_peer_inspection_unknown", printed["held"]["next_step"])
            self.assertIn("rndc key", printed["held"]["next_step"])
            self.assertNotIn("dns-peer-enroll --engine bind", printed["held"]["next_step"])
            code, events, runs, _ = self.run_host(
                root, [0], [(1, "failed", None)], resume_held_zone_lifecycle=True)
            self.assertEqual((code, events), (1, ["recover", "guest"]))
            self.assertTrue(runs[0].endswith(bootstrap.ZONE_LIFECYCLE_OUTCOME_PREFIX + "not-passed"))

    def test_resume_is_refused_without_an_exact_hold_before_anything_runs(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaisesRegex(bootstrap.BootstrapError, "did not hold"):
                self.run_host(root, [], [], resume_held_zone_lifecycle=True)
            self.run_host(
                root, [5],
                [(2, "pending", {"domain": CHILD, "job_error_code": "dns_peer_enrollment_required"})])
            for changes in ({"zero_zones": False}, {"cell_id": Z04}):
                with self.subTest(changes), \
                        self.assertRaisesRegex(bootstrap.BootstrapError, "not this cell"):
                    bootstrap.read_zone_lifecycle_hold(self.args(**changes), {"cell_directory": root})
        with self.assertRaisesRegex(bootstrap.BootstrapError, "same run flags"):
            self.run_host("/nonexistent", [], [], resume_held_zone_lifecycle=True,
                          reboot_after_recovery=False, disable_management_before_reboot=False)

    def test_zone_lifecycle_without_reboot_names_the_standalone_recover(self) -> None:
        text = bootstrap.zone_lifecycle_pending_next_step(
            self.args(reboot_after_recovery=False), {"domain": CHILD,
                                                     "job_error_code": "dns_peer_enrollment_required"})
        self.assertIn("zone-lifecycle <same arguments> --zero-zones --recover-delete --execute", text)
        self.assertIn("dns_peer_enrollment_required", text)
        self.assertEqual(bootstrap.finish_fresh_primary_run(
            self.args(), {}, 5, before_reboot={"outcome": "held", "peer_verdict_exit": 0,
                                               "lifecycle_exit": 2}), 2)

    def test_pending_next_step_names_the_inspection_and_unknown_codes_before_reboot(self) -> None:
        """Batch 10's two harness guidance defects, in the run-prepared (before-reboot)
        phrasing: the code, not just the domain, selects the action, and an
        enrolled-but-unobserved secondary is told to check its rndc key and
        loopback transfer rather than to enroll again."""

        args = self.args(reboot_after_recovery=True)
        inspection = bootstrap.zone_lifecycle_pending_next_step(
            args, {"domain": CHILD, "job_error_code": "dns_peer_inspection_unknown"})
        self.assertIn(bootstrap.RESUME_HELD_LIFECYCLE_FLAG, inspection)
        self.assertIn("rndc key", inspection)
        self.assertIn("loopback catalog transfer", inspection)
        self.assertIn("dns_peer_inspection_unknown", inspection)
        self.assertNotIn("dns-peer-enroll --engine bind", inspection)

        unknown = bootstrap.zone_lifecycle_pending_next_step(
            args, {"domain": CHILD, "job_error_code": "some_other_code"})
        self.assertIn("the harness has no owner step for some_other_code", unknown)
        self.assertNotIn("dns-peer-enroll --engine bind", unknown)
        self.assertNotIn(bootstrap.RESUME_HELD_LIFECYCLE_FLAG, unknown)

        missing = bootstrap.zone_lifecycle_pending_next_step(args, {"domain": CHILD})
        self.assertIn("pending ():", missing)
        self.assertIn("the harness has no owner step for", missing)

    def test_cli_and_dry_run(self) -> None:
        common = ["--work-root", "/w", "--cell-id", Z05, "--node", "debian13",
                  "--identity-file", "/k", "--source-fixture", "uninitialized"]
        self.assertTrue(bootstrap.parse_args(
            ["run-prepared", *common, bootstrap.RESUME_HELD_LIFECYCLE_FLAG]).resume_held_zone_lifecycle)
        with self.assertRaises(SystemExit), mock.patch("sys.stderr", new_callable=io.StringIO):
            bootstrap.parse_args(["zone-lifecycle", *common, bootstrap.RESUME_HELD_LIFECYCLE_FLAG])
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, self.RAW, {"n": 1})), \
                mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/k")), \
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]), \
                mock.patch.object(bootstrap.subprocess, "run") as run, \
                mock.patch("sys.stdout", new_callable=io.StringIO) as output:
            self.assertEqual(bootstrap.run_prepared(
                self.args(execute=False, resume_held_zone_lifecycle=True)), 0)
        run.assert_not_called()
        first = json.loads(output.getvalue().splitlines()[0])
        self.assertIn("rpc-pdns-primary-zone-v3-recover", first["primary"][0][-1])
        self.assertIn("--step re-add", first["primary"][1][-1])


class ReadmeTest(unittest.TestCase):
    def test_zero_zone_section_names_the_exact_resume_commands(self) -> None:
        section = README[README.index("#### Zero-zone fresh paired PowerDNS primary"):]
        section = section[:section.index("\n## ")]
        for text in (
            'python3 "$BOOTSTRAP" zone-lifecycle "${COMMON[@]}" --zero-zones --recover-delete --execute',
            'python3 "$BOOTSTRAP" run-prepared "${COMMON[@]}" --zero-zones --zone-lifecycle \\\n'
            '  --reboot-after-recovery --disable-management-before-reboot \\\n'
            '  --resume-held-zone-lifecycle --execute',
            "REFUSED", "zone-lifecycle-held.json", "zone-lifecycle-recover.json",
        ):
            self.assertIn(text, section)


if __name__ == "__main__":
    unittest.main()
