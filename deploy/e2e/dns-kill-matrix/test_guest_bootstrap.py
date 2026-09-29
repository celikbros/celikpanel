#!/usr/bin/env python3

from __future__ import annotations

import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
import native_bind_peer
import run_cell


def cell(
    node: str,
    phase: str,
    *,
    source_fixture_policy: str | None = None,
    driver: str = "bind",
    role: str = "standalone",
) -> dict:
    if source_fixture_policy is None:
        if driver == "bind" and phase in bootstrap.CRITICAL_MANAGED_PDNS_PHASES:
            source_fixture_policy = "managed-pdns-required"
        elif (
            driver == "bind"
            and node == "arch"
            and phase in bootstrap.EARLY_UNINITIALIZED_PHASES
        ):
            source_fixture_policy = "uninitialized-permitted-noncritical"
        else:
            source_fixture_policy = "driver-specific"
    return {
        "driver": driver,
        "role": role,
        "status": "runnable",
        "applicability": "verified",
        "boundary": {"phase": phase},
        "placement": {
            "kill_host": "arch" if node == "arch" else "debian-13",
            "source_fixture_policy": source_fixture_policy,
        },
    }


class GuestBootstrapTest(unittest.TestCase):
    @staticmethod
    def shell_canonical_array(shell: str, name: str) -> tuple[str, ...]:
        marker = f"readonly -a {name}=(\n"
        body = shell.split(marker, 1)[1].split("\n)\n", 1)[0]
        return tuple(line.strip() for line in body.splitlines() if line.strip())

    @staticmethod
    def stale_socket_cleanup_program() -> str:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        return shell.split("# STALE_AGENT_SOCKET_CLEANUP\n", 1)[1].split(
            "\nPY\n", 1
        )[0]

    def run_stale_socket_cleanup(
        self, socket_path: Path, proc_net_unix: Path, proof_path: Path
    ) -> tuple[int, dict]:
        environment = {
            "STALE_AGENT_SOCKET_PATH": str(socket_path),
            "STALE_AGENT_SOCKET_PROC_NET_UNIX": str(proc_net_unix),
            "STALE_AGENT_SOCKET_EXPECTED_UID": str(os.getuid()),
            "STALE_AGENT_SOCKET_EXPECTED_GID": str(os.getgid()),
            "STALE_AGENT_SOCKET_EXPECTED_GROUP": "test-group",
            "STALE_AGENT_SOCKET_AGENT_UNIT": "inactive:dead:0:0",
            "STALE_AGENT_SOCKET_PANEL_UNIT": "inactive:dead:0:0",
        }
        with mock.patch.dict(os.environ, environment, clear=False), mock.patch.object(
            sys, "argv", ["stale-socket-cleanup", str(proof_path)]
        ), self.assertRaises(SystemExit) as stopped:
            exec(
                compile(
                    self.stale_socket_cleanup_program(),
                    "stale-agent-socket-cleanup",
                    "exec",
                ),
                {},
            )
        return int(stopped.exception.code), json.loads(
            proof_path.read_text(encoding="utf-8")
        )

    @unittest.skipUnless(sys.platform.startswith("linux"), "/proc/net/unix is Linux-only")
    def test_verified_stale_agent_socket_is_removed_with_canonical_proof(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            socket_path = root / "agent.sock"
            proc_net_unix = Path("/proc/net/unix")
            proof_path = root / "proof.json"
            listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            listener.bind(str(socket_path))
            listener.close()
            os.chmod(socket_path, 0o660)
            status, proof = self.run_stale_socket_cleanup(
                socket_path, proc_net_unix, proof_path
            )

            self.assertEqual(status, 0)
            self.assertFalse(socket_path.exists())
            self.assertEqual(proof["decision"], "removed-verified-stale-socket")
            self.assertEqual(proof["socket_before"]["type"], "socket")
            self.assertEqual(proof["socket_before"]["mode"], "0660")
            self.assertIsNone(proof["socket_after"])
            self.assertEqual(
                proof_path.read_bytes(),
                (json.dumps(proof, indent=2, sort_keys=True) + "\n").encode(),
            )

    @unittest.skipUnless(sys.platform.startswith("linux"), "/proc/net/unix is Linux-only")
    def test_active_agent_socket_is_preserved_and_refused(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            socket_path = root / "agent.sock"
            proc_net_unix = Path("/proc/net/unix")
            proof_path = root / "proof.json"
            listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                listener.bind(str(socket_path))
                listener.listen(1)
                os.chmod(socket_path, 0o660)

                status, proof = self.run_stale_socket_cleanup(
                    socket_path, proc_net_unix, proof_path
                )

                self.assertEqual(status, 1)
                self.assertTrue(socket_path.exists())
                self.assertEqual(proof["decision"], "refused")
                self.assertIn("active listener or process", proof["reason"])
                self.assertEqual(len(proof["active_kernel_entries_before"]), 1)
            finally:
                listener.close()

    @unittest.skipUnless(os.name == "posix", "Unix socket cleanup is POSIX-only")
    def test_unexpected_agent_socket_path_or_metadata_is_preserved(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            proc_net_unix = root / "proc-net-unix"
            proc_net_unix.write_text(
                "Num RefCount Protocol Flags Type St Inode Path\n",
                encoding="utf-8",
            )

            regular = root / "regular.sock"
            regular.write_bytes(b"keep")
            status, proof = self.run_stale_socket_cleanup(
                regular, proc_net_unix, root / "regular-proof.json"
            )
            self.assertEqual(status, 1)
            self.assertEqual(regular.read_bytes(), b"keep")
            self.assertIn("not a socket", proof["reason"])

            wrong_mode = root / "wrong-mode.sock"
            listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            listener.bind(str(wrong_mode))
            listener.close()
            os.chmod(wrong_mode, 0o600)
            status, proof = self.run_stale_socket_cleanup(
                wrong_mode, proc_net_unix, root / "mode-proof.json"
            )
            self.assertEqual(status, 1)
            self.assertTrue(wrong_mode.exists())
            self.assertIn("metadata differs", proof["reason"])

    def test_prepare_proves_coordinators_inactive_before_socket_cleanup(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        body = shell.split("prepare_bind() {\n", 1)[1].split("\n}\n\ncase ", 1)[0]
        ordered = [
            "systemctl stop celikpanel-panel.service celikpanel-agent.service",
            "agent_stop_evidence=$(inactive_unit_evidence celikpanel-agent.service)",
            "panel_stop_evidence=$(inactive_unit_evidence celikpanel-panel.service)",
            'remove_verified_stale_agent_socket "$agent_stop_evidence" "$panel_stop_evidence"',
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = body.index(fragment, cursor + 1)
        self.assertNotIn("agent socket remained after service stop", body)

    def test_guest_script_bundle_normalizes_crlf_before_hashing(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source.py"
            destination = root / "guest.py"
            source.write_bytes(b"#!/usr/bin/env python3\r\nprint('ready')\r\n")
            bootstrap.copy_guest_script(source, destination)
            self.assertEqual(destination.read_bytes(), b"#!/usr/bin/env python3\nprint('ready')\n")
            source.write_bytes(b"#!/usr/bin/env python3\rprint('bad')\n")
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.copy_guest_script(source, destination)

    def test_disposable_admin_uses_strict_inherited_stdin(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn('--create-admin --admin-credentials-file=-', shell)
        self.assertIn('{"username":"s1-admin","email":"s1-admin@fixture.invalid","password":"%s"}', shell)
        self.assertNotIn("printf 's1-admin\\ns1-admin@fixture.invalid\\n%s\\n'", shell)

    @unittest.skipUnless(
        sys.platform == "linux" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "release lock fixture metadata requires a root Linux fixture",
    )
    def test_release_transaction_lock_is_idempotent_and_rejects_unsafe_state(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        program = shell.split("# FIXTURE_RELEASE_TRANSACTION_LOCK\n", 1)[1].split(
            "\nPYRELEASELOCK\n", 1
        )[0]

        def invoke(directory: Path) -> subprocess.CompletedProcess[str]:
            scoped = program.replace(
                '"/var/lib/celikpanel-release-transaction"', repr(str(directory))
            )
            return subprocess.run(
                [sys.executable, "-c", scoped, str(directory)],
                check=False,
                capture_output=True,
                text=True,
            )

        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            directory = root / "release"
            self.assertEqual(invoke(directory).returncode, 0)
            lock = directory / "transaction.lock"
            initial = lock.stat()
            self.assertEqual(initial.st_uid, 0)
            self.assertEqual(initial.st_gid, 0)
            self.assertEqual(initial.st_mode & 0o777, 0o600)
            self.assertEqual(initial.st_size, 0)
            self.assertEqual(initial.st_nlink, 1)
            self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
            self.assertEqual(invoke(directory).returncode, 0)
            self.assertEqual(lock.stat().st_ino, initial.st_ino)

            lock.chmod(0o644)
            self.assertNotEqual(invoke(directory).returncode, 0)
            self.assertEqual(lock.stat().st_mode & 0o777, 0o644)
            lock.chmod(0o600)
            lock.write_bytes(b"owner-data")
            self.assertNotEqual(invoke(directory).returncode, 0)
            self.assertEqual(lock.read_bytes(), b"owner-data")
            lock.write_bytes(b"")

            outside = root / "hardlink"
            os.link(lock, outside)
            self.assertNotEqual(invoke(directory).returncode, 0)
            outside.unlink()
            lock.unlink()
            lock.symlink_to(outside)
            self.assertNotEqual(invoke(directory).returncode, 0)
            self.assertTrue(lock.is_symlink())
            lock.unlink()

            directory.chmod(0o755)
            self.assertNotEqual(invoke(directory).returncode, 0)
            self.assertEqual(directory.stat().st_mode & 0o777, 0o755)
            directory.rmdir()
            directory.symlink_to(root, target_is_directory=True)
            self.assertNotEqual(invoke(directory).returncode, 0)
            self.assertTrue(directory.is_symlink())

    def test_fresh_guest_creates_controller_required_dkim_directory(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn(
            "install -d -m 0750 -o root -g celikpanel /etc/celikpanel/dkim",
            shell,
        )

    def test_arch_uninitialized_scenario_has_exact_early_tuple(self) -> None:
        bootstrap.validate_bind_cell(cell("arch", "intent"), "arch", "uninitialized")
        scenario = bootstrap.bind_scenario("uninitialized")
        self.assertEqual(scenario["source_fixture"], "uninitialized")
        self.assertEqual(scenario["source_engine"], "")
        self.assertEqual(scenario["source_epoch"], 0)
        self.assertEqual(scenario["source_revision"], 0)
        self.assertEqual(scenario["target_epoch"], 1)

    def test_paired_primary_scenario_uses_distinct_guest_addresses(self) -> None:
        selected = cell("arch", "intent", role="paired-primary")
        bootstrap.validate_bind_cell(selected, "arch", "uninitialized")
        scenario = bootstrap.bind_scenario(
            "uninitialized", role="paired-primary", node="arch"
        )
        self.assertEqual(scenario["topology"], "paired")
        self.assertEqual(scenario["pair_role"], "primary")
        self.assertEqual(scenario["local_ip"], "192.0.2.11")
        self.assertEqual(scenario["peer_ip"], "192.0.2.10")
        records = scenario["zones"][0]["records"]
        tuples = {
            (record["name"], record["type"], record["content"]) for record in records
        }
        self.assertIn(("ns2.s1-kill.test", "A", "192.0.2.10"), tuples)
        self.assertIn(("s1-kill.test", "NS", "ns2.s1-kill.test"), tuples)

    def test_paired_primary_rejects_preexisting_source(self) -> None:
        selected = cell("arch", "intent", role="paired-primary")
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.validate_bind_cell(selected, "arch", "managed-pdns")
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.bind_scenario("managed-pdns", role="paired-primary", node="arch")
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.bind_scenario("uninitialized", role="paired-primary", node="debian13")

    def test_native_bind_peer_config_subscribes_to_exact_primary_catalog(self) -> None:
        config = native_bind_peer.secondary_config("192.0.2.11", "192.0.2.10")
        self.assertIn('zone "catalog-c000020b.celikpanel.invalid"', config)
        self.assertIn("default-primaries { 192.0.2.11; }", config)
        self.assertIn("listen-on { 127.0.0.1; 192.0.2.10; }", config)
        self.assertIn("recursion no;", config)
        with self.assertRaises(bootstrap.BootstrapError):
            native_bind_peer.secondary_config("192.0.2.11", "192.0.2.11")

    def test_shell_source_policy_arrays_match_python_constants(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        expected = {
            "SOURCE_FIXTURE_POLICIES": bootstrap.SOURCE_FIXTURE_POLICIES,
            "EARLY_UNINITIALIZED_PHASES": bootstrap.EARLY_UNINITIALIZED_PHASES,
            "CRITICAL_MANAGED_PDNS_PHASES": (
                bootstrap.CRITICAL_MANAGED_PDNS_PHASES
            ),
            "FRESH_BIND_STANDALONE_PHASES": bootstrap.FRESH_BIND_STANDALONE_PHASES,
            "FRESH_PDNS_STANDALONE_PHASES": bootstrap.FRESH_PDNS_STANDALONE_PHASES,
            "EARLY_MANAGED_PDNS_BIND_PHASES": (
                bootstrap.EARLY_MANAGED_PDNS_BIND_PHASES
            ),
        }
        for name, python_values in expected.items():
            with self.subTest(name=name):
                shell_values = self.shell_canonical_array(shell, name)
                self.assertEqual(len(shell_values), len(set(shell_values)))
                self.assertEqual(frozenset(shell_values), python_values)

    def test_shell_managed_pdns_bind_boundary_gate_is_exact(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        helper = shell.split("managed_pdns_bind_boundary_allowed() {\n", 1)[1].split(
            "\n}\n\nprepare_bind() {", 1
        )[0]
        array = shell.split("readonly -a CRITICAL_MANAGED_PDNS_PHASES=(\n", 1)[1].split(
            "\n)", 1
        )[0]
        early = shell.split("readonly -a EARLY_MANAGED_PDNS_BIND_PHASES=(\n", 1)[1].split(
            "\n)", 1
        )[0]
        contains = shell.split("array_contains() {\n", 1)[1].split("\n}", 1)[0]
        matches = shell.split("standalone_cell_matches_phase() {\n", 1)[1].split(
            "\n}\n", 1
        )[0]
        script = (
            "CRITICAL_MANAGED_PDNS_PHASES=(\n" + array + "\n)\n"
            + "EARLY_MANAGED_PDNS_BIND_PHASES=(\n" + early + "\n)\n"
            + "array_contains() {\n" + contains + "\n}\n"
            + "standalone_cell_matches_phase() {\n" + matches + "\n}\n"
            + "managed_pdns_bind_boundary_allowed() {\n" + helper + "\n}\n"
            + 'managed_pdns_bind_boundary_allowed "$1" "$2" "$3"\n'
        )
        bind_body = shell.split("prepare_bind() {\n", 1)[1].split(
            "\n}\n\nprepare_pdns_adopt() {", 1
        )[0]
        self.assertIn(
            'managed_pdns_bind_boundary_allowed "$cell_id" "$boundary_phase" "$source_fixture_policy"',
            bind_body,
        )
        bash = (
            Path("C:/Program Files/Git/bin/bash.exe")
            if os.name == "nt" else shutil.which("bash")
        )
        if not bash or not Path(bash).is_file():
            self.skipTest("bash is unavailable for the shell guard contract")
        cases = (
            (bootstrap.INDEPENDENT_BIND_HANDOFF_CELL, "rolling-back", "driver-specific", 0),
            (bootstrap.INDEPENDENT_BIND_HANDOFF_CELL, "rolling-back", "managed-pdns-required", 1),
            ("bind__rolling-back__before-write__standalone__peer-reachable",
             "rolling-back", "driver-specific", 1),
            ("bind__rolling-back__after-write__paired-primary__peer-reachable",
             "rolling-back", "driver-specific", 1),
            ("bind__source-stopped__before-write__standalone__peer-reachable",
             "source-stopped", "managed-pdns-required", 0),
            ("bind__source-stopped__before-write__standalone__peer-reachable",
             "source-stopped", "driver-specific", 1),
            ("bind__intent__after-write__standalone__peer-reachable",
             "intent", "driver-specific", 0),
            ("bind__intent__before-write__standalone__peer-unreachable",
             "intent", "driver-specific", 0),
            ("bind__target-staged__after-write__standalone__peer-reachable",
             "target-staged", "driver-specific", 0),
            ("bind__target-staged__before-write__standalone__peer-unreachable",
             "target-staged", "driver-specific", 0),
            ("bind__intent__after-write__standalone__peer-reachable",
             "intent", "managed-pdns-required", 1),
            ("bind__intent__after-write__standalone__peer-reachable",
             "intent", "uninitialized-permitted-noncritical", 1),
            ("bind__intent__after-write__paired-primary__peer-reachable",
             "intent", "driver-specific", 1),
            ("bind__intent__after-write__standalone__peer-reachable",
             "target-staged", "driver-specific", 1),
            ("bind__pre-intent__standalone__peer-reachable",
             "pre-intent", "driver-specific", 1),
            ("bind__target-verified__after-write__standalone__peer-reachable",
             "target-verified", "driver-specific", 1),
            ("bind__committed__after-write__standalone__peer-reachable",
             "committed", "driver-specific", 1),
        )
        for cell_id, phase, policy, expected in cases:
            with self.subTest(cell_id=cell_id, phase=phase, policy=policy):
                result = subprocess.run(
                    [str(bash), "-c", script, "test", cell_id, phase, policy],
                    check=False, capture_output=True, text=True,
                )
                self.assertEqual(result.returncode, expected, result.stderr)

    def test_shell_prepare_accepts_exact_manifest_policy_argument(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        body = shell.split("prepare_bind() {\n", 1)[1].split(
            "\n}\n\ncase ", 1
        )[0]
        self.assertIn("local source_fixture_policy=$5 stage=$6", body)
        self.assertIn(
            'array_contains "$source_fixture_policy" '
            '"${SOURCE_FIXTURE_POLICIES[@]}"',
            body,
        )
        self.assertNotIn("reserved for explicit early Arch cells", body)
        self.assertIn(
            "prepare-bind expects CELL_ID NODE PHASE SOURCE_FIXTURE "
            "SOURCE_FIXTURE_POLICY STAGE",
            shell,
        )

    def test_python_prepare_passes_exact_manifest_source_policy(self) -> None:
        source = Path(bootstrap.__file__).read_text(encoding="utf-8")
        body = source.split("def prepare(args: argparse.Namespace) -> None:\n", 1)[
            1
        ].split("\n\ndef common_parser(", 1)[0]
        self.assertIn(
            'source_policy = cell["placement"]["source_fixture_policy"]',
            body,
        )
        self.assertIn('f"{source_policy} {stage}"', body)
        self.assertIn('"source_fixture_policy": source_policy', body)

    def test_debian_uninitialized_is_allowed_by_driver_specific_policy(self) -> None:
        bootstrap.validate_bind_cell(
            cell("debian13", "intent"), "debian13", "uninitialized"
        )

    def test_uninitialized_rejects_wrong_fixture_policy(self) -> None:
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.validate_bind_cell(
                cell(
                    "debian13",
                    "intent",
                    source_fixture_policy="managed-pdns-required",
                ),
                "debian13",
                "uninitialized",
            )

    def test_rolled_back_bind_requires_managed_pdns_on_debian(self) -> None:
        bootstrap.validate_bind_cell(
            cell("debian13", "rolled-back",
                 source_fixture_policy="managed-pdns-required"),
            "debian13", "managed-pdns",
        )
        for node, source in (("debian13", "uninitialized"), ("arch", "managed-pdns")):
            with self.subTest(node=node, source=source), self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_bind_cell(
                    cell(node, "rolled-back",
                         source_fixture_policy="managed-pdns-required"),
                    node, source,
                )

    def test_managed_pdns_rejects_wrong_fixture_policy(self) -> None:
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.validate_bind_cell(
                cell(
                    "debian13",
                    "source-stopped",
                    source_fixture_policy="uninitialized-permitted-noncritical",
                ),
                "debian13",
                "managed-pdns",
            )

    def test_arch_uninitialized_cannot_claim_critical_boundary(self) -> None:
        for phase in ("source-stopped", "target-started"):
            with self.subTest(phase=phase), self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_bind_cell(
                    cell("arch", phase), "arch", "uninitialized"
                )

    def test_managed_pdns_is_debian_only(self) -> None:
        bootstrap.validate_bind_cell(
            cell("debian13", "source-stopped"), "debian13", "managed-pdns"
        )
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.validate_bind_cell(
                cell("arch", "intent"), "arch", "managed-pdns"
            )

    def test_managed_pdns_preinstall_is_critical_or_exact_early_cell_only(self) -> None:
        bootstrap.validate_bind_cell(
            cell("debian13", "target-started"), "debian13", "managed-pdns"
        )
        # The synthetic cell has no exact ID or fault selector, so the early
        # intent/target-staged admission (EarlyManagedPDNSBindCellTest) cannot
        # apply to it.
        for phase in ("pre-intent", "intent", "target-staged", "target-verified", "committed"):
            with self.subTest(phase=phase), self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_bind_cell(
                    cell("debian13", phase), "debian13", "managed-pdns"
                )

    def test_source_setup_uses_real_external_pdns_adoption(self) -> None:
        scenario = bootstrap.pdns_adoption_source_setup_scenario()
        self.assertEqual(scenario["driver"], "pdns-adopt")
        self.assertEqual(scenario["source_fixture"], "external-pdns-adoption")
        self.assertEqual(scenario["mode"], "adopt")
        self.assertEqual(scenario["source_engine"], "")
        self.assertEqual(scenario["target_engine"], "pdns")
        self.assertEqual((scenario["source_epoch"], scenario["target_epoch"]), (0, 1))
        self.assertEqual(scenario["source_revision"], 0)
        self.assertTrue(scenario["zones"])

    def test_deleted_child_is_opt_in_and_parent_remains_authoritative(self) -> None:
        ordinary = bootstrap.pdns_adoption_source_setup_scenario()
        with_deleted = bootstrap.pdns_adoption_source_setup_scenario(
            include_deleted_child=True
        )
        self.assertEqual(len(ordinary["zones"]), 1)
        self.assertEqual(with_deleted["zones"][0], ordinary["zones"][0])
        self.assertEqual(with_deleted["zones"][1], {
            "ordinal": 1,
            "domain": "old.s1-kill.test",
            "desired_generation": 1,
            "delete": True,
            "zone_type": "NATIVE",
            "records": [],
            "zone_qualifier": "",
        })
        args = bootstrap.parse_args([
            "prepare-pdns-adopt", "--work-root", "/tmp/fixture",
            "--cell-id", "pdns-adopt__intent__after-write__standalone__peer-reachable",
            "--node", "debian13", "--identity-file", "/tmp/key",
            "--source-fixture", "external-pdns-adoption",
            "--include-deleted-child",
        ])
        self.assertTrue(args.include_deleted_child)

    def test_pdns_adopt_cell_requires_exact_debian_external_preimage(self) -> None:
        selected = cell(
            "debian13", "intent", driver="pdns-adopt"
        )
        bootstrap.validate_pdns_adopt_cell(
            selected, "debian13", "external-pdns-adoption"
        )
        invalid = [
            (selected, "arch", "external-pdns-adoption"),
            (selected, "debian13", "managed-pdns"),
            (
                cell(
                    "debian13",
                    "intent",
                    driver="pdns-adopt",
                    role="paired-primary",
                ),
                "debian13",
                "external-pdns-adoption",
            ),
            (
                cell("debian13", "source-stopped", driver="pdns-adopt"),
                "debian13",
                "external-pdns-adoption",
            ),
            (
                cell(
                    "debian13",
                    "intent",
                    driver="pdns-adopt",
                    source_fixture_policy="managed-pdns-required",
                ),
                "debian13",
                "external-pdns-adoption",
            ),
        ]
        for candidate, node, source_fixture in invalid:
            with self.subTest(
                node=node,
                source_fixture=source_fixture,
                role=candidate["role"],
                phase=candidate["boundary"]["phase"],
            ), self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_pdns_adopt_cell(
                    candidate, node, source_fixture
                )

    def test_source_preinstall_proof_renderer_is_canonical_and_explicit(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        renderer = shell.split("# SOURCE_PREINSTALL_PROOF_RENDERER\n", 1)[1].split(
            "\nPY\n", 1
        )[0]
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "source-preinstall.json"
            environment = {
                "SOURCE_PREINSTALL_CELL_ID": (
                    "bind__source-stopped__before-write__standalone__peer-reachable"
                ),
                "PDNS_SERVER_VERSION": "4.9.2-1+deb13u1",
                "PDNS_SQLITE_VERSION": "4.9.2-1+deb13u1",
                "PDNS_UNIT_FILE_STATE": "enabled",
                "SOURCE_PREINSTALL_PURPOSE": "bind",
            }
            with mock.patch.dict(os.environ, environment, clear=False), mock.patch.object(
                sys, "argv", ["renderer", str(output)]
            ):
                exec(compile(renderer, "source-preinstall-renderer", "exec"), {})
            raw = output.read_bytes()
            value = json.loads(raw)
        self.assertEqual(
            raw,
            (json.dumps(value, indent=2, sort_keys=True) + "\n").encode(),
        )
        self.assertEqual(
            value["schema"], "celikpanel/dns-kill-source-preinstall/v1"
        )
        self.assertEqual(
            value["scope"], "managed-pdns-source-preparation-for-bind-only"
        )
        self.assertEqual(value["package_install_origin"], "harness-source-preinstall")
        self.assertEqual(
            [item["name"] for item in value["source_packages"]],
            ["pdns-backend-sqlite3", "pdns-server"],
        )
        self.assertEqual(
            value["measured_target_packages"],
            [{"name": "bind9", "status": "absent"}],
        )
        self.assertTrue(value["mask_removed_before_external_source_start"])
        self.assertEqual(
            value["source_unit_before_external_configuration"]["active_state"],
            "inactive",
        )

    def test_source_preinstall_renderer_marks_measured_adoption_packages_preexisting(
        self,
    ) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        renderer = shell.split("# SOURCE_PREINSTALL_PROOF_RENDERER\n", 1)[1].split(
            "\nPY\n", 1
        )[0]
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "source-preinstall.json"
            environment = {
                "SOURCE_PREINSTALL_CELL_ID": (
                    "pdns-adopt__intent__after-write__standalone__peer-reachable"
                ),
                "PDNS_SERVER_VERSION": "4.9.2-1+deb13u1",
                "PDNS_SQLITE_VERSION": "4.9.2-1+deb13u1",
                "PDNS_UNIT_FILE_STATE": "enabled",
                "SOURCE_PREINSTALL_PURPOSE": "pdns-adopt",
            }
            with mock.patch.dict(
                os.environ, environment, clear=False
            ), mock.patch.object(sys, "argv", ["renderer", str(output)]):
                exec(compile(renderer, "source-preinstall-renderer", "exec"), {})
            value = json.loads(output.read_bytes())
        self.assertEqual(
            value["scope"],
            "external-pdns-source-preparation-for-measured-adoption-only",
        )
        self.assertEqual(
            value["measured_target_packages"],
            [
                {
                    "name": "pdns-backend-sqlite3",
                    "status": "preexisting-required-by-adoption",
                },
                {
                    "name": "pdns-server",
                    "status": "preexisting-required-by-adoption",
                },
            ],
        )

    def test_prepare_pdns_adopt_seals_preimage_without_setup_adoption_rpc(
        self,
    ) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        body = shell.split("prepare_pdns_adopt() {\n", 1)[1].split(
            "\n}\n\ncase ", 1
        )[0]
        ordered = [
            'preinstall_pdns_source_packages "$cell_id" "$address" pdns-adopt',
            'create_external_pdns_source "$address" "$SCENARIO_FILE"',
            'write_external_pdns_preimage_proof "$cell_id" "$address"',
            'write_source_proof external-pdns-adoption "$cell_id"',
            'write_controller_argv "$cell_id" "$address"',
            "systemctl stop celikpanel-panel.service celikpanel-agent.service",
            'remove_verified_stale_agent_socket "$agent_stop_evidence"',
            "external PowerDNS preimage proof changed after coordinator stop",
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = body.index(fragment, cursor + 1)
        self.assertNotIn("SOURCE_SETUP_FILE", body)
        self.assertNotIn("SOURCE_SETUP_IDENTITY", body)
        self.assertNotIn("dns-kill-trigger rpc-switch", body)
        self.assertIn("prepare-pdns-adopt)", shell)

    def test_external_preimage_query_uses_live_wal_read_only_contract(
        self,
    ) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        renderer = shell.split(
            "# EXTERNAL_PDNS_PREIMAGE_PROOF_RENDERER\n", 1
        )[1].split("\nPY\n", 1)[0]
        query = renderer.split("connection = sqlite3.connect(", 1)[1].split(
            "\nreceipts = {", 1
        )[0]
        self.assertNotIn("immutable=1", query)
        self.assertEqual(query.count("?mode=ro"), 1)
        ordered = [
            '"file:/var/lib/powerdns/pdns.sqlite3?mode=ro"',
            "timeout=5.0",
            'connection.execute("PRAGMA query_only=ON")',
            'query_only = connection.execute("PRAGMA query_only").fetchone()',
            "query_only != (1,)",
            'journal_mode != ("wal",)',
            "external PowerDNS query created a rollback journal",
            'database_after = os.lstat(database_file["path"])',
            "wal_after = inspect_sidecar(",
            "shm_after = inspect_sidecar(",
            "identity(database_after) != identity(database_status)",
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = query.index(fragment, cursor + 1)
        self.assertEqual(renderer.count("os.lstat(journal_path)"), 2)

    def test_guest_shell_has_no_folded_apply_patch_artifacts(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        for fragment in ("]] +", '" +        ||', " in +", "sync -f +"):
            with self.subTest(fragment=fragment):
                self.assertNotIn(fragment, shell)

    def test_source_preinstall_guards_then_retires_mask_before_production(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        body = shell.split("preinstall_pdns_source_packages() {\n", 1)[1].split(
            "\n}\n\nwrite_source_proof() {", 1
        )[0]
        ordered = [
            "require_apt_package_absent bind9",
            "/usr/bin/apt-get update",
            "/usr/bin/systemctl mask pdns.service",
            "/usr/bin/apt-get install -y --no-install-recommends",
            "PowerDNS package hook escaped its start mask",
            "/usr/bin/systemctl unmask pdns.service",
            "source preinstall retained its temporary PowerDNS mask",
            'require_apt_package_absent bind9',
            'assert_no_source_engine "$address"',
            'write_source_preinstall_proof "$cell_id"',
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = body.index(fragment, cursor + 1)
        self.assertIn("local packages=(pdns-backend-sqlite3 pdns-server)", body)
        self.assertNotIn("bind9", body.split("apt-get install", 1)[1].split("\n", 1)[0])

    def test_external_pdns_is_unreceipted_and_uses_package_schema(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        body = shell.split("create_external_pdns_source() {\n", 1)[1].split(
            "\n}\n\nwrite_source_adoption_proof() {", 1
        )[0]
        ordered = [
            'assert_no_source_engine "$address"',
            "require_apt_package_absent bind9",
            "pdns-backend-sqlite3: $schema",
            "os.O_CREAT | os.O_EXCL | os.O_RDWR",
            "/usr/bin/systemctl enable --now pdns.service",
            "external PowerDNS source was created with a production state receipt",
            "external PowerDNS source was created with production ownership",
            "require_apt_package_absent bind9",
            'dns_probe "$address" www.s1-kill.test',
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = body.index(fragment, cursor + 1)
        self.assertIn(
            "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql", body
        )
        self.assertIn('"driver": "pdns-adopt"', body)
        self.assertIn('"source_fixture": "external-pdns-adoption"', body)

    def test_external_pdns_database_constructor_executes_create_new(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        marker = (
            "EXTERNAL_PDNS_SCENARIO=$scenario EXTERNAL_PDNS_SCHEMA=$schema "
            "EXTERNAL_PDNS_DATABASE=$database python3 - <<'PY'\n"
        )
        constructor = shell.split(marker, 1)[1].split("\nPY\n", 1)[0]
        schema = """
CREATE TABLE domains (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  type TEXT NOT NULL
);
CREATE TABLE records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id INTEGER,
  name TEXT,
  type TEXT,
  content TEXT,
  ttl INTEGER,
  prio INTEGER,
  disabled INTEGER,
  ordername TEXT,
  auth INTEGER
);
CREATE TABLE supermasters (ip TEXT, nameserver TEXT, account TEXT);
"""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scenario_path = root / "scenario.json"
            schema_path = root / "schema.sqlite3.sql"
            database_path = root / "pdns.sqlite3"
            scenario_path.write_bytes(
                bootstrap.json_bytes(bootstrap.pdns_adoption_source_setup_scenario())
            )
            schema_path.write_text(schema, encoding="utf-8")
            environment = {
                "EXTERNAL_PDNS_SCENARIO": str(scenario_path),
                "EXTERNAL_PDNS_SCHEMA": str(schema_path),
                "EXTERNAL_PDNS_DATABASE": str(database_path),
            }
            with mock.patch.dict(os.environ, environment, clear=False):
                exec(compile(constructor, "external-pdns-constructor", "exec"), {})
                with self.assertRaises(FileExistsError):
                    exec(
                        compile(constructor, "external-pdns-constructor", "exec"),
                        {},
                    )
            connection = sqlite3.connect(database_path)
            try:
                self.assertEqual(
                    connection.execute(
                        "SELECT name, type FROM domains ORDER BY id"
                    ).fetchall(),
                    [("s1-kill.test", "NATIVE")],
                )
                self.assertEqual(
                    connection.execute(
                        "SELECT name, type, content, ttl, prio, disabled, "
                        "ordername, auth FROM records ORDER BY id"
                    ).fetchall(),
                    [
                        (
                            record["name"], record["type"], record["content"],
                            record["ttl"], record["prio"],
                            int(record["disabled"]), None, 1,
                        )
                        for record in bootstrap.zone_snapshot()["records"]
                    ],
                )
                self.assertEqual(
                    connection.execute("PRAGMA quick_check").fetchone(), ("ok",)
                )
            finally:
                connection.close()
            for suffix in ("-journal", "-wal", "-shm"):
                self.assertFalse(Path(str(database_path) + suffix).exists())

    def test_source_adoption_sidecar_capture_is_fail_closed_and_metadata_only(
        self,
    ) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        capture = shell.split("# SOURCE_ADOPTION_SIDECAR_CAPTURE\n", 1)[1].split(
            "\nPY\n", 1
        )[0]
        compile(capture, "source-adoption-sidecar-capture", "exec")
        ordered = [
            'os.lstat(path)',
            'os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK | nofollow',
            'identity(before) != identity(opened)',
            'opened.st_nlink != 1',
            'require_empty and os.read(descriptor, 1) != b""',
            'after_path = os.lstat(path)',
            'identity(after_path) != identity(opened)',
            'journal_path = database_path + "-journal"',
            'required_size=0, require_empty=True',
            'required_size=32768',
            '"content_policy": content_policy',
            'metadata(shm_path, shm, "volatile-unhashed")',
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = capture.index(fragment, cursor + 1)
        self.assertNotIn("sha256", capture.lower())

    def test_prepare_adopts_then_normalizes_before_managed_source_proof(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        body = shell.split("prepare_bind() {\n", 1)[1].split(
            "\n}\n\ncase ", 1
        )[0]
        ordered = [
            'preinstall_pdns_source_packages "$cell_id" "$address"',
            'create_external_pdns_source "$address" "$SOURCE_SETUP_FILE"',
            "CELIKPANEL_S1_DRIVER=pdns-adopt",
            "require_apt_package_absent bind9",
            'write_source_adoption_proof "$cell_id"',
            "CELIKPANEL_S1_DRIVER=bind",
            "rpc-normalize-pdns",
            '--normalization-receipt "$SOURCE_NORMALIZATION_IDENTITY"',
            'validate_normalized_pdns_source "$cell_id" "$address" "$state_sha"',
            'write_source_proof managed-pdns "$cell_id"',
        ]
        cursor = -1
        for fragment in ordered:
            with self.subTest(fragment=fragment):
                cursor = body.index(fragment, cursor + 1)
        self.assertNotIn("CELIKPANEL_S1_DRIVER=pdns-switch", body)
        self.assertIn(
            'verify_dns_receipt_pair pdns',
            body,
        )
        normalization = shell.split("validate_normalized_pdns_source() {\n", 1)[1].split(
            "\n}\n\nwrite_source_proof() {", 1
        )[0]
        for table in (
            "celikpanel_dns_zone_sync_receipts",
            "celikpanel_dns_zone_sync_v3_receipts",
            "celikpanel_dns_engine_manifest_receipt",
        ):
            self.assertIn(table, normalization)
        self.assertIn('identity["source_engine"] != "pdns"', normalization)
        self.assertIn('operation.get("method") != "Agent.SyncDNSZoneV3"', normalization)

    def test_source_adoption_proof_renderer_is_canonical_and_target_clean(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        renderer = shell.split("# SOURCE_ADOPTION_PROOF_RENDERER\n", 1)[1].split(
            "\nPY\n", 1
        )[0]
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "source-adoption.json"
            environment = {
                "SOURCE_ADOPTION_CELL_ID": (
                    "bind__source-stopped__before-write__standalone__peer-reachable"
                ),
                "PDNS_SERVER_VERSION": "4.9.2-1+deb13u1",
                "PDNS_SQLITE_VERSION": "4.9.2-1+deb13u1",
                "SETUP_SCENARIO_SHA": "1" * 64,
                "SETUP_IDENTITY_SHA": "2" * 64,
                "PDNS_MAIN_SHA": "3" * 64,
                "PDNS_MANAGED_SHA": "4" * 64,
                "PDNS_SCHEMA_SHA": "5" * 64,
                "PDNS_DATABASE_SHA": "6" * 64,
                "PDNS_MAIN_OWNER": "root:pdns",
                "PDNS_STATE_SHA": "7" * 64,
                "PDNS_SIDECARS_JSON": json.dumps(
                    {
                        "rollback_journal": {
                            "path": "/var/lib/powerdns/pdns.sqlite3-journal",
                            "status": "absent",
                        },
                        "write_ahead_log": {
                            "path": "/var/lib/powerdns/pdns.sqlite3-wal",
                            "file_type": "regular",
                            "owner": "pdns:pdns",
                            "mode": "0640",
                            "link_count": 1,
                            "device": 65025,
                            "inode": 131534,
                            "size": 0,
                            "content_policy": "empty",
                        },
                        "shared_memory": {
                            "path": "/var/lib/powerdns/pdns.sqlite3-shm",
                            "file_type": "regular",
                            "owner": "pdns:pdns",
                            "mode": "0640",
                            "link_count": 1,
                            "device": 65025,
                            "inode": 131535,
                            "size": 32768,
                            "content_policy": "volatile-unhashed",
                        },
                    },
                    separators=(",", ":"),
                    sort_keys=True,
                ),
            }
            with mock.patch.dict(os.environ, environment, clear=False), mock.patch.object(
                sys, "argv", ["renderer", str(output)]
            ):
                exec(compile(renderer, "source-adoption-renderer", "exec"), {})
            raw = output.read_bytes()
            value = json.loads(raw)
        self.assertEqual(
            raw, (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        )
        self.assertEqual(
            value["schema"], "celikpanel/dns-kill-source-adoption/v2"
        )
        self.assertEqual(value["production_adoption_driver"], "pdns-adopt")
        self.assertEqual(
            value["measured_target_packages"],
            [{"name": "bind9", "status": "absent"}],
        )
        self.assertTrue(
            value["production_receipts"][
                "measured_target_install_ownership_absent"
            ]
        )
        self.assertEqual(
            value["production_receipts"]["state_sha256"],
            value["production_receipts"]["active_ownership_sha256"],
        )
        self.assertEqual(
            value["database"]["sidecars"]["write_ahead_log"]["size"], 0
        )
        self.assertEqual(
            value["database"]["sidecars"]["shared_memory"]["content_policy"],
            "volatile-unhashed",
        )

    @unittest.skipUnless(os.name == "posix", "run_cell validates paths with host OS rules")
    def test_emitted_trigger_and_retry_match_controller_contract(self) -> None:
        trigger, recovery, recovery_probe = bootstrap.controller_commands(
            "bind__intent__before-write__standalone__peer-reachable"
        )
        # These are guest paths. Offline host validation proves the argv shape
        # without pretending the not-yet-prepared guest directory is local.
        with mock.patch.object(run_cell, "require_real_directory"):
            contract = run_cell.socket_trigger_retry_contract(trigger, recovery)
        self.assertEqual(contract["scenario_path"], trigger[3])
        self.assertEqual(contract["identity_receipt_path"], trigger[5])
        self.assertEqual(contract["operation_timeout"], "45m")
        self.assertEqual(recovery_probe[0], "/opt/celikpanel/libexec/dns-kill-recovery-probe.py")
        self.assertEqual(len(recovery_probe), 13)

    def test_owner_bind_source_is_only_exact_debian_handoff(self) -> None:
        selected = cell("debian13", "rolling-back", source_fixture_policy="driver-specific")
        selected["id"] = bootstrap.INDEPENDENT_BIND_HANDOFF_CELL
        selected["boundary"]["edge"] = "after-write"
        selected["fault_selector"] = {"phase": "rolling-back", "point": "after_write"}
        selected["peer_reachability"] = "reachable"
        bootstrap.validate_bind_cell(selected, "debian13", "owner-bind")
        scenario = bootstrap.bind_scenario("owner-bind")
        self.assertEqual(
            (scenario["source_engine"], scenario["source_epoch"],
             scenario["target_epoch"], scenario["source_revision"]),
            ("", 0, 1, 0),
        )
        for changed in (
            {**selected, "id": "foreign"},
            {**selected, "role": "paired-primary"},
            {**selected, "placement": {**selected["placement"], "source_fixture_policy": "managed-pdns-required"}},
        ):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_bind_cell(changed, "debian13", "owner-bind")

    def test_managed_pdns_to_bind_preserves_source_revision(self) -> None:
        scenario = bootstrap.bind_scenario("managed-pdns")
        self.assertEqual(scenario["source_engine"], "pdns")
        self.assertEqual((scenario["source_epoch"], scenario["target_epoch"]), (1, 2))
        self.assertEqual(scenario["source_revision"], 0)

    def test_guest_renderer_emits_complete_controller_argv(self) -> None:
        shell = Path(bootstrap.__file__).with_name("guest_bootstrap.sh").read_text(
            encoding="utf-8"
        )
        marker = "RESULT_DIR=$result_dir python3 - \"$temporary\" <<'PY'\n"
        renderer = shell.split(marker, 1)[1].split("\nPY\n", 1)[0]
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "argv.json"
            environment = {
                "CELL_ID": "bind__intent__before-write__standalone__peer-reachable",
                "DNS_ADDRESS": "192.0.2.20",
                "REQUEST_ID": "1" * 32,
                "NONCE": "2" * 64,
                "RESULT_DIR": "/var/lib/celikpanel-dns-kill-matrix/results/cell",
            }
            with mock.patch.dict(os.environ, environment, clear=False), mock.patch.object(
                sys, "argv", ["renderer", str(output)]
            ):
                exec(compile(renderer, "guest-controller-renderer", "exec"), {})
            argv = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(
            argv[0], "/opt/celikpanel/libexec/dns-kill-run-cell.py"
        )
        parsed = run_cell.build_argument_parser().parse_args(argv[1:])
        self.assertEqual(parsed.source_proof, "/var/lib/celikpanel-dns-kill-matrix/source-proof.json")
        self.assertEqual(parsed.dns_address, "192.0.2.20")
        trigger = json.loads(parsed.trigger_command)
        recovery = json.loads(parsed.recovery_command)
        with mock.patch.object(run_cell, "require_real_directory"), mock.patch.object(
            run_cell, "require_clean_absolute", side_effect=lambda path, _label: path
        ):
            contract = run_cell.socket_trigger_retry_contract(trigger, recovery)
        self.assertEqual(
            contract["identity_receipt_path"],
            "/var/lib/celikpanel-dns-kill-matrix/measured/trigger-identity.json",
        )
        self.assertEqual(parsed.recovery_probe_command, json.dumps(
            bootstrap.controller_commands(environment["CELL_ID"])[2],
            separators=(",", ":"),
        ))

    def test_web_archive_is_byte_deterministic_and_has_no_root_prefix(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "dist"
            root.mkdir()
            (root / "assets").mkdir()
            (root / "index.html").write_text("index\n", encoding="utf-8")
            (root / "assets" / "app.js").write_text("app\n", encoding="utf-8")
            first, second = Path(temporary) / "first.tar", Path(temporary) / "second.tar"
            bootstrap.write_deterministic_web_tar(root, first)
            bootstrap.write_deterministic_web_tar(root, second)
            self.assertEqual(
                hashlib.sha256(first.read_bytes()).digest(),
                hashlib.sha256(second.read_bytes()).digest(),
            )
            with tarfile.open(first) as archive:
                self.assertEqual(
                    archive.getnames(), ["assets", "assets/app.js", "index.html"]
                )


class PreparedCellRunnerTest(unittest.TestCase):
    def test_runner_is_dry_by_default_and_preserves_controller_exit(self) -> None:
        cell_id = "bind__rolled-back__after-write__standalone__peer-reachable"
        args = mock.Mock(
            cell_id=cell_id,
            node="debian13",
            source_fixture="managed-pdns",
            identity_file=Path("/tmp/test-key"),
            execute=False,
            stop_after_kill_for_independent_recovery=False,
        )
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, {}, {})),
            mock.patch.object(bootstrap, "validate_supported_cell"),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch.object(bootstrap.subprocess, "run") as run,
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            self.assertEqual(bootstrap.run_prepared(args), 0)
            command = json.loads(output.getvalue())
            self.assertEqual(command[:2], ["ssh", "guest"])
            self.assertIn("runuser -u root -g celikpanel", command[-1])
            self.assertIn("env -i PATH=", command[-1])
            self.assertIn(cell_id, command[-1])
            run.assert_not_called()
            args.execute = True
            run.return_value = subprocess.CompletedProcess(command, 2)
            self.assertEqual(bootstrap.run_prepared(args), 2)
            run.assert_called_once_with(command, check=False)

    def test_bind_independent_handoff_requires_exact_managed_source_cell(self) -> None:
        cell_id = bootstrap.INDEPENDENT_BIND_HANDOFF_CELL
        selected = bootstrap.load_manifest_cell(
            Path(bootstrap.__file__).with_name("manifest.json"), cell_id
        )
        bootstrap.validate_bind_cell(selected, "debian13", "managed-pdns")
        args = mock.Mock(
            cell_id=cell_id, node="debian13", source_fixture="managed-pdns",
            identity_file=Path("/tmp/test-key"), execute=False,
            stop_after_kill_for_independent_recovery=True,
        )
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            self.assertEqual(bootstrap.run_prepared(args), 0)
        self.assertTrue(json.loads(output.getvalue())[-1].endswith(
            cell_id + " " + bootstrap.INDEPENDENT_PDNS_HANDOFF_FLAG
        ))
        for change in (
            {"fault_selector": {"phase": "rolling-back", "point": "before_write"}},
            {"role": "paired-primary"},
        ):
            wrong = dict(selected, **change)
            with mock.patch.object(bootstrap, "load_plan", return_value=({}, wrong, {})):
                with self.assertRaises(bootstrap.BootstrapError):
                    bootstrap.run_prepared(args)
        args.source_fixture = "external-pdns-adoption"
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.run_prepared(args)

    def test_later_bind_rollback_requires_exact_prepared_handoff(self) -> None:
        cell_id = bootstrap.INDEPENDENT_BIND_HANDOFF_CELL
        selected = bootstrap.load_manifest_cell(
            Path(bootstrap.__file__).with_name("manifest.json"), cell_id
        )
        args = mock.Mock(
            cell_id=cell_id, node="debian13", source_fixture="managed-pdns",
            identity_file=Path("/tmp/test-key"), execute=False,
            stop_after_kill_for_independent_recovery=True,
            bind_rollback_after_target_started=True,
        )
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            self.assertEqual(bootstrap.run_prepared(args), 0)
        self.assertTrue(json.loads(output.getvalue())[-1].endswith(
            cell_id + " " + bootstrap.INDEPENDENT_PDNS_HANDOFF_FLAG
            + " " + bootstrap.LATER_BIND_ROLLBACK_FLAG
        ))

        args.source_fixture = "owner-bind"
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            self.assertEqual(bootstrap.run_prepared(args), 0)
        self.assertTrue(json.loads(output.getvalue())[-1].endswith(
            cell_id + " " + bootstrap.INDEPENDENT_PDNS_HANDOFF_FLAG
            + " " + bootstrap.LATER_BIND_ROLLBACK_FLAG
        ))

        args.source_fixture = "managed-pdns"
        args.stop_after_kill_for_independent_recovery = False
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.run_prepared(args)
        args.stop_after_kill_for_independent_recovery = True
        args.source_fixture = "external-pdns-adoption"
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.run_prepared(args)
        self.assertIn(
            'sys.argv[3] != later_flag',
            bootstrap.RUN_PREPARED_CODE,
        )

    def test_independent_handoff_is_limited_to_canonical_adoption_rollback(self) -> None:
        cell_id = bootstrap.INDEPENDENT_PDNS_HANDOFF_CELL
        selected = bootstrap.load_manifest_cell(
            Path(bootstrap.__file__).with_name("manifest.json"), cell_id
        )
        args = mock.Mock(
            cell_id=cell_id,
            node="debian13",
            source_fixture="external-pdns-adoption",
            identity_file=Path("/tmp/test-key"),
            execute=False,
            stop_after_kill_for_independent_recovery=True,
        )
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            self.assertEqual(bootstrap.run_prepared(args), 0)
            command = json.loads(output.getvalue())
        self.assertTrue(
            command[-1].endswith(
                cell_id + " " + bootstrap.INDEPENDENT_PDNS_HANDOFF_FLAG
            )
        )

        wrong = dict(selected)
        wrong["fault_selector"] = {"phase": "rolling-back", "point": "before_write"}
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, wrong, {})):
            with self.assertRaisesRegex(bootstrap.BootstrapError, "exact Debian PowerDNS"):
                bootstrap.run_prepared(args)
        args.source_fixture = "managed-pdns"
        with mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {})):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.run_prepared(args)

    @unittest.skipUnless(
        sys.platform == "linux" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "prepared argv owner proof requires a root Linux fixture",
    )
    def test_guest_handoff_adds_only_scoped_flag_to_owned_controller_argv(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            prepared = root / "controller-argv.json"
            captured = root / "captured.json"
            executable = root / "run-cell.py"
            executable.write_text(
                "#!/usr/bin/env python3\n"
                "import json, sys\n"
                f"open({str(captured)!r}, 'w').write(json.dumps(sys.argv))\n",
                encoding="utf-8",
            )
            executable.chmod(0o700)
            code = bootstrap.RUN_PREPARED_CODE.replace(
                "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json",
                str(prepared),
            ).replace(
                "/opt/celikpanel/libexec/dns-kill-run-cell.py", str(executable)
            )
            cell_id = bootstrap.INDEPENDENT_PDNS_HANDOFF_CELL
            base = [str(executable), "--cell-id", cell_id, "--trigger-mode", "socket"]
            prepared.write_text(json.dumps(base), encoding="utf-8")
            prepared.chmod(0o600)
            command = [
                sys.executable,
                "-c",
                code,
                cell_id,
                bootstrap.INDEPENDENT_PDNS_HANDOFF_FLAG,
            ]
            self.assertEqual(subprocess.run(command, check=False).returncode, 0)
            self.assertEqual(
                json.loads(captured.read_text(encoding="utf-8")),
                base + [bootstrap.INDEPENDENT_PDNS_HANDOFF_FLAG],
            )
            self.assertEqual(json.loads(prepared.read_text(encoding="utf-8")), base)
            for wrong in (
                command[:-2]
                + [
                    "bind__rolling-back__after-write__standalone__peer-reachable",
                    bootstrap.INDEPENDENT_PDNS_HANDOFF_FLAG,
                ],
                command[:-1] + ["--unexpected"],
            ):
                self.assertNotEqual(
                    subprocess.run(wrong, check=False, capture_output=True).returncode,
                    0,
                )
            prepared.write_text(json.dumps(base[:-1] + ["subprocess"]), encoding="utf-8")
            self.assertNotEqual(
                subprocess.run(command, check=False, capture_output=True).returncode,
                0,
            )

    @unittest.skipUnless(
        sys.platform == "linux" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "prepared argv owner proof requires a root Linux fixture",
    )
    def test_guest_program_checks_file_and_cell_before_exec(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            prepared = root / "controller-argv.json"
            executable = root / "run-cell.py"
            executable.write_text(
                "#!/usr/bin/env python3\nraise SystemExit(17)\n", encoding="utf-8"
            )
            executable.chmod(0o700)
            code = bootstrap.RUN_PREPARED_CODE.replace(
                "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json",
                str(prepared),
            ).replace(
                "/opt/celikpanel/libexec/dns-kill-run-cell.py", str(executable)
            )
            prepared.write_text(
                json.dumps([str(executable), "--cell-id", "expected", "--result", "x"]),
                encoding="utf-8",
            )
            prepared.chmod(0o600)
            command = [sys.executable, "-c", code, "expected"]
            self.assertEqual(subprocess.run(command, check=False).returncode, 17)
            self.assertEqual(
                subprocess.run(command[:-1] + ["wrong"], check=False, capture_output=True).returncode,
                1,
            )
            prepared.chmod(0o644)
            self.assertEqual(
                subprocess.run(command, check=False, capture_output=True).returncode,
                1,
            )

    def test_paired_bind_to_powerdns_scenario_preserves_catalog_identity(self) -> None:
        source = bootstrap.bind_scenario(
            "uninitialized", role="paired-primary", node="debian13",
            allow_debian_paired_source=True,
        )
        measured = bootstrap.pdns_switch_scenario(role="paired-primary")
        self.assertEqual(source["topology"], "paired")
        self.assertEqual(measured["topology"], "paired")
        self.assertEqual(measured["zones"], source["zones"])
        for key, value in {
            "pair_role": "primary",
            "local_ip": "192.0.2.10",
            "peer_ip": "192.0.2.11",
            "local_ns": "ns1.s1-kill.test",
            "peer_ns": "ns2.s1-kill.test",
        }.items():
            self.assertEqual(source[key], value)
            self.assertEqual(measured[key], value)
        records = source["zones"][0]["records"]
        self.assertIn(
            {"name": "ns2.s1-kill.test", "type": "A", "content": "192.0.2.11",
             "ttl": 300, "prio": 0, "disabled": False},
            records,
        )
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.pdns_switch_scenario(role="paired-secondary")
        selected = cell("debian13", "intent", driver="pdns-switch", role="paired-primary")
        bootstrap.validate_pdns_switch_cell(selected, "debian13", "managed-bind")

    def test_fresh_paired_powerdns_primary_has_empty_source_and_master_member(self) -> None:
        selected = cell("debian13", "intent", driver="pdns-switch", role="paired-primary")
        bootstrap.validate_pdns_switch_cell(selected, "debian13", "uninitialized")
        scenario = bootstrap.pdns_switch_scenario(
            role="paired-primary", source_fixture="uninitialized"
        )
        self.assertEqual(
            (scenario["source_engine"], scenario["source_epoch"],
             scenario["target_engine"], scenario["target_epoch"]),
            ("", 0, "pdns", 1),
        )
        self.assertEqual(scenario["pair_role"], "primary")
        self.assertEqual(scenario["zones"][0]["zone_type"], "MASTER")
        self.assertEqual(
            scenario["zones"][0]["records"],
            bootstrap.pdns_switch_scenario(role="paired-primary")["zones"][0]["records"],
        )
        for candidate in (
            cell("debian13", "target-started", driver="pdns-switch", role="paired-primary"),
            cell("debian13", "intent", driver="pdns-switch", role="paired-secondary"),
            cell("arch", "intent", driver="pdns-switch", role="paired-primary"),
        ):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_pdns_switch_cell(candidate, "debian13", "uninitialized")
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.pdns_switch_scenario(
                role="paired-secondary", source_fixture="uninitialized"
            )

    def test_managed_bind_source_requires_standalone_debian_switch(self) -> None:
        selected = cell("debian13", "intent", driver="pdns-switch")
        bootstrap.validate_pdns_switch_cell(selected, "debian13", "managed-bind")
        measured = bootstrap.pdns_switch_scenario()
        source = bootstrap.bind_scenario("uninitialized", node="debian13")
        self.assertEqual(measured["zones"], source["zones"])
        self.assertEqual(
            (measured["source_engine"], measured["source_epoch"], measured["target_engine"]),
            ("bind", 1, "pdns"),
        )
        for candidate, node, fixture in (
            (cell("arch", "intent", driver="pdns-switch"), "arch", "managed-bind"),
            (cell("arch", "intent", driver="pdns-switch"), "arch", "uninitialized"),
            (selected, "debian13", "owner-bind"),
        ):
            with self.assertRaises(bootstrap.BootstrapError):
                bootstrap.validate_pdns_switch_cell(candidate, node, fixture)


class FreshInstallCellTest(unittest.TestCase):
    """D-026 first-install cells: the prior state is no DNS engine."""

    MANIFEST_PATH = Path(bootstrap.__file__).with_name("manifest.json")
    SHELL_PATH = Path(bootstrap.__file__).with_name("guest_bootstrap.sh")
    NEW_BIND_PHASES = frozenset({"target-verified"})

    @classmethod
    def setUpClass(cls) -> None:
        cls.manifest = json.loads(cls.MANIFEST_PATH.read_text(encoding="utf-8"))
        cls.runnable = [
            raw for raw in cls.manifest["cells"] if raw["status"] == "runnable"
        ]
        cls.shell = cls.SHELL_PATH.read_text(encoding="utf-8")

    @staticmethod
    def node(raw: dict) -> str:
        return bootstrap.NODE_FOR_PLACEMENT[raw["placement"]["kill_host"]]

    def fresh_admitted(self, raw: dict) -> bool:
        try:
            bootstrap.validate_supported_cell(raw, self.node(raw), "uninitialized")
        except bootstrap.BootstrapError:
            return False
        return True

    def newly_admitted(self) -> list[dict]:
        return [
            raw for raw in self.runnable
            if raw["role"] == "standalone" and (
                raw["driver"] == "pdns-switch"
                or (raw["driver"] == "bind"
                    and raw["boundary"]["phase"] in self.NEW_BIND_PHASES)
            )
        ]

    @staticmethod
    def fresh_scenario(raw: dict, node: str) -> dict:
        if raw["driver"] == "pdns-switch":
            return bootstrap.pdns_switch_scenario(
                role=raw["role"], source_fixture="uninitialized"
            )
        return bootstrap.bind_scenario("uninitialized", role=raw["role"], node=node)

    def bash(self) -> str:
        bash = (
            Path("C:/Program Files/Git/bin/bash.exe")
            if os.name == "nt" else shutil.which("bash")
        )
        if not bash or not Path(bash).is_file():
            self.skipTest("bash is unavailable for the shell guard contract")
        return str(bash)

    def shell_function(self, name: str) -> str:
        body = self.shell.split(f"\n{name}() {{\n", 1)[1].split("\n}\n", 1)[0]
        return f"{name}() {{\n{body}\n}}\n"

    def shell_array(self, name: str) -> str:
        body = self.shell.split(f"readonly -a {name}=(\n", 1)[1].split("\n)\n", 1)[0]
        return f"{name}=(\n{body}\n)\n"

    def test_every_new_cell_is_admitted_on_its_manifest_placement(self) -> None:
        cells = self.newly_admitted()
        self.assertEqual(
            sum(raw["driver"] == "pdns-switch" for raw in cells), 34
        )
        self.assertEqual(sum(raw["driver"] == "bind" for raw in cells), 4)
        for raw in cells:
            with self.subTest(cell_id=raw["id"]):
                self.assertEqual(raw["placement"]["source_fixture_policy"], "driver-specific")
                if raw["driver"] == "pdns-switch":
                    self.assertEqual(raw["placement"]["kill_host"], "debian-13")
                self.assertTrue(self.fresh_admitted(raw))

    def test_fresh_refusals_that_remain_are_exact(self) -> None:
        for raw in self.runnable:
            if raw["driver"] not in {"bind", "pdns-switch"} or not self.fresh_admitted(raw):
                continue
            phase = raw["boundary"]["phase"]
            with self.subTest(cell_id=raw["id"]):
                self.assertNotEqual(raw["role"], "paired-secondary")
                if raw["driver"] == "bind":
                    self.assertNotIn(phase, bootstrap.CRITICAL_MANAGED_PDNS_PHASES)
                    allowed = (
                        bootstrap.FRESH_BIND_STANDALONE_PHASES
                        if raw["role"] == "standalone"
                        else bootstrap.EARLY_UNINITIALIZED_PHASES
                    )
                    self.assertIn(phase, allowed)
                elif raw["role"] == "paired-primary":
                    self.assertEqual(phase, "intent")
        for cell_id in (
            "bind__target-started__after-write__standalone__peer-reachable",
            "bind__target-started__before-write__standalone__peer-unreachable",
            "bind__source-stopped__after-write__standalone__peer-reachable",
            "bind__rolled-back__after-write__standalone__peer-reachable",
            "bind__committed__after-write__standalone__peer-reachable",
            "bind__rolling-back__before-write__standalone__peer-reachable",
            "bind__target-verified__after-write__paired-secondary__peer-reachable",
            "bind__intent__after-write__paired-secondary__peer-reachable",
            "pdns-switch__intent__after-write__paired-secondary__peer-reachable",
            "pdns-switch__target-started__after-write__paired-primary__peer-reachable",
        ):
            raw = next(item for item in self.runnable if item["id"] == cell_id)
            with self.subTest(refused=cell_id):
                self.assertFalse(self.fresh_admitted(raw))
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.bind_scenario("uninitialized", role="paired-secondary")
        with self.assertRaises(bootstrap.BootstrapError):
            bootstrap.validate_pdns_switch_cell(
                cell("arch", "intent", driver="pdns-switch"), "arch", "uninitialized"
            )

    def test_fresh_standalone_powerdns_scenario_is_empty_native_source(self) -> None:
        scenario = bootstrap.pdns_switch_scenario(source_fixture="uninitialized")
        self.assertEqual(
            (scenario["driver"], scenario["source_fixture"], scenario["source_engine"],
             scenario["source_epoch"], scenario["target_engine"],
             scenario["target_epoch"], scenario["source_revision"],
             scenario["topology"]),
            ("pdns-switch", "uninitialized", "", 0, "pdns", 1, 0, "standalone"),
        )
        for key in ("pair_role", "local_ip", "local_ns", "peer_ip", "peer_ns"):
            self.assertNotIn(key, scenario)
        self.assertEqual([zone["zone_type"] for zone in scenario["zones"]], ["NATIVE"])
        self.assertEqual(
            scenario["zones"],
            bootstrap.bind_scenario("uninitialized", node="debian13")["zones"],
        )

    def test_controller_accepts_each_new_fresh_scenario_and_predecessor(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            for index, raw in enumerate(self.newly_admitted()):
                with self.subTest(cell_id=raw["id"]):
                    path = Path(temporary) / f"scenario-{index}.json"
                    path.write_bytes(bootstrap.json_bytes(
                        self.fresh_scenario(raw, self.node(raw))
                    ))
                    os.chmod(path, 0o600)
                    selected = run_cell.CellSpec.from_manifest(self.manifest, raw["id"])
                    value, _ = run_cell.validate_source_scenario(str(path), selected)
                    self.assertEqual(value["source_fixture"], "uninitialized")
                    run_cell.expected_journal_phase(selected)
            refused = "bind__target-started__after-write__standalone__peer-reachable"
            path = Path(temporary) / "refused.json"
            path.write_bytes(bootstrap.json_bytes(
                bootstrap.bind_scenario("uninitialized", node="debian13")
            ))
            os.chmod(path, 0o600)
            with self.assertRaisesRegex(run_cell.ControllerError, "managed PowerDNS source"):
                run_cell.validate_source_scenario(
                    str(path), run_cell.CellSpec.from_manifest(self.manifest, refused)
                )

    def dry_prepare(self, raw: dict, action: str) -> tuple[list, dict]:
        args = mock.Mock(
            action=action, cell_id=raw["id"], node=self.node(raw),
            source_fixture="uninitialized", identity_file=Path("/tmp/test-key"),
            execute=False, authority_acceptance=False,
        )
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch.object(bootstrap, "scp_base", return_value=["scp"]),
            mock.patch.object(bootstrap, "remote_destination",
                              side_effect=lambda _node, path: "guest:" + path),
            mock.patch.object(bootstrap.subprocess, "run") as run,
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            bootstrap.prepare(args)
            run.assert_not_called()
        lines = [json.loads(line) for line in output.getvalue().splitlines()]
        return lines[:-1], lines[-1]

    def test_dry_run_prepares_every_new_fresh_cell(self) -> None:
        for raw in self.newly_admitted():
            action = (
                "prepare-pdns-switch" if raw["driver"] == "pdns-switch" else "prepare-bind"
            )
            with self.subTest(cell_id=raw["id"]):
                commands, summary = self.dry_prepare(raw, action)
                self.assertEqual(len(commands), 3)
                self.assertEqual(commands[1][-1], "guest:" + bootstrap.stage_name(raw["id"]) + "/")
                self.assertEqual(commands[2][-1], (
                    f"sudo /bin/bash {bootstrap.stage_name(raw['id'])}/guest_bootstrap.sh "
                    f"{action} {raw['id']} {self.node(raw)} {raw['boundary']['phase']} "
                    f"uninitialized driver-specific {bootstrap.stage_name(raw['id'])}"
                ))
                self.assertEqual(summary["uploaded"], "scenario.json")
                self.assertIsNone(summary["source_preinstall_proof"])
                self.assertFalse(summary["setup_adoption_rpc_used"])
        for cell_id, action in (
            ("bind__target-started__after-write__standalone__peer-reachable", "prepare-bind"),
            ("bind__committed__after-write__standalone__peer-reachable", "prepare-bind"),
            ("pdns-switch__intent__after-write__paired-secondary__peer-reachable",
             "prepare-pdns-switch"),
        ):
            raw = next(item for item in self.runnable if item["id"] == cell_id)
            with self.subTest(refused=cell_id), self.assertRaises(bootstrap.BootstrapError):
                self.dry_prepare(raw, action)

    def test_shell_bind_fresh_gate_executes_exactly(self) -> None:
        snippet = self.shell.split("prepare_bind() {\n", 1)[1].split(
            "    if [[ $source_fixture == uninitialized ]]; then\n", 1
        )[1].split("\n        [[ $source_fixture_policy == driver-specific ||", 1)[0]
        script = (
            "set -euo pipefail\ndie() { exit 1; }\n"
            + self.shell_array("EARLY_UNINITIALIZED_PHASES")
            + self.shell_array("FRESH_BIND_STANDALONE_PHASES")
            + self.shell_function("array_contains")
            + self.shell_function("standalone_cell_matches_phase")
            + 'cell_id=$1\nboundary_phase=$2\n' + snippet + "\necho admitted\n"
        )
        cases = (
            ("bind__target-verified__after-write__standalone__peer-reachable", "target-verified", 0),
            ("bind__target-verified__before-write__standalone__peer-unreachable", "target-verified", 0),
            ("bind__intent__after-write__paired-primary__peer-reachable", "intent", 0),
            ("bind__pre-intent__standalone__peer-reachable", "pre-intent", 0),
            ("bind__target-verified__after-write__paired-secondary__peer-reachable", "target-verified", 1),
            ("bind__target-started__after-write__standalone__peer-reachable", "target-started", 1),
            ("bind__committed__after-write__standalone__peer-reachable", "committed", 1),
            ("bind__intent__after-write__standalone__peer-reachable", "target-verified", 1),
        )
        bash = self.bash()
        for cell_id, phase, expected in cases:
            with self.subTest(cell_id=cell_id, phase=phase):
                result = subprocess.run(
                    [bash, "-c", script, "test", cell_id, phase],
                    check=False, capture_output=True, text=True,
                )
                self.assertEqual(result.returncode, expected, result.stderr)

    def test_shell_pdns_switch_fresh_dispatch_executes_exactly(self) -> None:
        # Keep only the fresh-source dispatch; the managed-BIND remainder
        # carries a heredoc and is outside this contract.
        head = "\n        return\n    fi\n"
        dispatch = (
            "prepare_pdns_switch() {\n"
            + self.shell.split("\nprepare_pdns_switch() {\n", 1)[1].split(head, 1)[0]
            + head + "    exit 99\n}\n"
        )
        script = (
            "set -euo pipefail\ndie() { exit 1; }\n"
            + self.shell_array("FRESH_PDNS_STANDALONE_PHASES")
            + self.shell_function("array_contains")
            + self.shell_function("require_simple_value")
            + self.shell_function("standalone_cell_matches_phase")
            + self.shell_function("prepare_fresh_pdns_primary")
            + self.shell_function("prepare_fresh_pdns_standalone")
            + 'prepare_fresh_pdns_source() { echo "fresh:$4"; }\n'
            + dispatch
            + 'prepare_pdns_switch "$1" "$2" "$3" uninitialized "$4" /nonexistent\n'
        )
        cases = [
            (
                "pdns-switch__pre-intent__standalone__peer-unreachable"
                if phase == "pre-intent"
                else f"pdns-switch__{phase}__after-write__standalone__peer-reachable",
                "debian13", phase, "driver-specific", 0, "fresh:standalone",
            )
            for phase in sorted(bootstrap.FRESH_PDNS_STANDALONE_PHASES)
        ] + [
            ("pdns-switch__rolled-back__before-write__standalone__peer-unreachable",
             "debian13", "rolled-back", "driver-specific", 0, "fresh:standalone"),
            ("pdns-switch__intent__after-write__paired-primary__peer-reachable",
             "debian13", "intent", "driver-specific", 0, "fresh:primary"),
            ("pdns-switch__intent__after-write__standalone__peer-reachable",
             "arch", "intent", "driver-specific", 1, ""),
            ("pdns-switch__intent__after-write__standalone__peer-reachable",
             "debian13", "intent", "managed-pdns-required", 1, ""),
            ("pdns-switch__intent__after-write__standalone__peer-reachable",
             "debian13", "committed", "driver-specific", 1, ""),
            ("pdns-switch__intent__after-write__paired-secondary__peer-reachable",
             "debian13", "intent", "driver-specific", 1, ""),
            ("pdns-switch__target-started__after-write__paired-primary__peer-reachable",
             "debian13", "target-started", "driver-specific", 1, ""),
        ]
        bash = self.bash()
        for cell_id, node, phase, policy, expected, output in cases:
            with self.subTest(cell_id=cell_id, node=node, phase=phase, policy=policy):
                result = subprocess.run(
                    [bash, "-c", script, "test", cell_id, node, phase, policy],
                    check=False, capture_output=True, text=True,
                )
                self.assertEqual(result.returncode, expected, result.stderr)
                self.assertEqual(result.stdout.strip(), output)

    def test_guest_fresh_pdns_scenario_check_matches_python_renderer(self) -> None:
        program = self.shell.split("<<'PYFRESHPDNS'\n", 1)[1].split(
            "\nPYFRESHPDNS\n", 1
        )[0]
        scenarios = {
            "standalone": bootstrap.pdns_switch_scenario(source_fixture="uninitialized"),
            "primary": bootstrap.pdns_switch_scenario(
                role="paired-primary", source_fixture="uninitialized"
            ),
            "managed-bind": bootstrap.pdns_switch_scenario(),
        }
        cases = (
            ("standalone", "standalone", 0),
            ("primary", "primary", 0),
            ("standalone", "primary", 1),
            ("primary", "standalone", 1),
            ("standalone", "managed-bind", 1),
        )
        with tempfile.TemporaryDirectory() as temporary:
            for role, name, expected in cases:
                with self.subTest(role=role, scenario=name):
                    path = Path(temporary) / f"{name}.json"
                    path.write_bytes(bootstrap.json_bytes(scenarios[name]))
                    result = subprocess.run(
                        [sys.executable, "-c", program, str(path)],
                        check=False, capture_output=True, text=True,
                        env={**os.environ, "FRESH_PDNS_ROLE": role},
                    )
                    self.assertEqual(result.returncode, expected, result.stderr)


class EarlyManagedPDNSBindCellTest(unittest.TestCase):
    """Managed PowerDNS source at standalone Debian intent/target-staged BIND.

    The two after-write/peer-reachable cells run only through the explicit
    owner-inverse-after-restart controller mode; the two before-write cells and
    every run without that mode keep the earlier refusals. Nothing here is
    native evidence.
    """

    MANIFEST_PATH = Path(bootstrap.__file__).with_name("manifest.json")
    ADMITTED = frozenset({
        "bind__intent__before-write__standalone__peer-unreachable",
        "bind__intent__after-write__standalone__peer-reachable",
        "bind__target-staged__before-write__standalone__peer-unreachable",
        "bind__target-staged__after-write__standalone__peer-reachable",
    })

    @classmethod
    def setUpClass(cls) -> None:
        cls.manifest = json.loads(cls.MANIFEST_PATH.read_text(encoding="utf-8"))
        cls.runnable = [
            raw for raw in cls.manifest["cells"] if raw["status"] == "runnable"
        ]

    def raw(self, cell_id: str) -> dict:
        return next(item for item in self.runnable if item["id"] == cell_id)

    @staticmethod
    def node(raw: dict) -> str:
        return bootstrap.NODE_FOR_PLACEMENT[raw["placement"]["kill_host"]]

    @staticmethod
    def admitted(raw: dict, node: str) -> bool:
        try:
            bootstrap.validate_bind_cell(raw, node, "managed-pdns")
        except bootstrap.BootstrapError:
            return False
        return True

    def test_exactly_the_debian_standalone_early_cells_are_newly_admitted(self) -> None:
        early = [
            raw for raw in self.runnable
            if raw["driver"] == "bind"
            and raw["boundary"]["phase"] in bootstrap.EARLY_MANAGED_PDNS_BIND_PHASES
        ]
        admitted = {
            raw["id"] for raw in early
            if any(self.admitted(raw, node) for node in ("arch", "debian13"))
        }
        self.assertEqual(admitted, self.ADMITTED)
        for cell_id in self.ADMITTED:
            raw = self.raw(cell_id)
            with self.subTest(cell_id=cell_id):
                self.assertEqual(raw["placement"]["kill_host"], "debian-13")
                self.assertEqual(
                    raw["placement"]["source_fixture_policy"], "driver-specific"
                )
                self.assertFalse(self.admitted(raw, "arch"))
                # The empty source stays admitted on the same cells.
                bootstrap.validate_bind_cell(raw, "debian13", "uninitialized")

    def test_other_managed_pdns_refusals_remain(self) -> None:
        for cell_id in (
            "bind__intent__after-write__standalone__peer-unreachable",
            "bind__target-staged__before-write__standalone__peer-reachable",
            "bind__intent__after-write__paired-primary__peer-unreachable",
            "bind__target-staged__after-write__paired-secondary__peer-reachable",
            "bind__pre-intent__standalone__peer-reachable",
            "bind__target-verified__after-write__standalone__peer-reachable",
            "bind__committed__after-write__standalone__peer-reachable",
            "bind__rolling-back__before-write__standalone__peer-reachable",
        ):
            raw = self.raw(cell_id)
            with self.subTest(refused=cell_id):
                self.assertFalse(self.admitted(raw, self.node(raw)))
        selected = self.raw("bind__intent__after-write__standalone__peer-reachable")
        for change in (
            {"placement": {**selected["placement"],
                           "source_fixture_policy": "managed-pdns-required"}},
            {"placement": {**selected["placement"],
                           "source_fixture_policy": "uninitialized-permitted-noncritical"}},
            {"role": "paired-primary"},
            {"id": "bind__intent__after-write__standalone__peer-unreachable"},
            {"fault_selector": {"phase": "intent", "point": "before_write"}},
            {"boundary": {**selected["boundary"], "edge": "window"}},
        ):
            with self.subTest(change=sorted(change)), self.assertRaises(
                bootstrap.BootstrapError
            ):
                bootstrap.validate_bind_cell(
                    dict(selected, **change), "debian13", "managed-pdns"
                )
        # Critical placements keep their managed-pdns-required admission.
        bootstrap.validate_bind_cell(
            self.raw("bind__source-stopped__after-write__standalone__peer-reachable"),
            "debian13", "managed-pdns",
        )

    def dry_prepare(self, raw: dict) -> tuple[list, dict]:
        args = mock.Mock(
            action="prepare-bind", cell_id=raw["id"], node=self.node(raw),
            source_fixture="managed-pdns", identity_file=Path("/tmp/test-key"),
            execute=False, authority_acceptance=False,
        )
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch.object(bootstrap, "scp_base", return_value=["scp"]),
            mock.patch.object(bootstrap, "remote_destination",
                              side_effect=lambda _node, path: "guest:" + path),
            mock.patch.object(bootstrap.subprocess, "run") as run,
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            bootstrap.prepare(args)
            run.assert_not_called()
        lines = [json.loads(line) for line in output.getvalue().splitlines()]
        return lines[:-1], lines[-1]

    def test_dry_run_prepares_each_cell_on_the_unchanged_managed_path(self) -> None:
        for cell_id in sorted(self.ADMITTED):
            raw = self.raw(cell_id)
            stage = bootstrap.stage_name(cell_id)
            with self.subTest(cell_id=cell_id):
                commands, summary = self.dry_prepare(raw)
                self.assertEqual(len(commands), 3)
                self.assertEqual(commands[2][-1], (
                    f"sudo /bin/bash {stage}/guest_bootstrap.sh prepare-bind "
                    f"{cell_id} debian13 {raw['boundary']['phase']} "
                    f"managed-pdns driver-specific {stage}"
                ))
                self.assertEqual(summary["uploaded"], "scenario.json source-setup-pdns.json")
                self.assertTrue(summary["setup_adoption_rpc_used"])
                self.assertEqual(
                    summary["source_preinstall_proof"],
                    "/var/lib/celikpanel-dns-kill-matrix/source-preinstall-pdns.json",
                )
                self.assertEqual(
                    summary["source_adoption_proof"],
                    "/var/lib/celikpanel-dns-kill-matrix/source-adoption-pdns.json",
                )
        with self.assertRaises(bootstrap.BootstrapError):
            self.dry_prepare(
                self.raw("bind__intent__after-write__standalone__peer-unreachable")
            )

    def test_controller_scenario_and_predecessor_accept_managed_source(self) -> None:
        expected_phase = {
            "bind__intent__before-write__standalone__peer-unreachable": None,
            "bind__intent__after-write__standalone__peer-reachable": "intent",
            "bind__target-staged__before-write__standalone__peer-unreachable": "intent",
            "bind__target-staged__after-write__standalone__peer-reachable": "target-staged",
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "scenario.json"
            path.write_bytes(bootstrap.json_bytes(bootstrap.bind_scenario("managed-pdns")))
            os.chmod(path, 0o600)
            for cell_id in sorted(self.ADMITTED):
                with self.subTest(cell_id=cell_id):
                    selected = run_cell.CellSpec.from_manifest(self.manifest, cell_id)
                    value, _ = run_cell.validate_source_scenario(str(path), selected)
                    self.assertEqual(
                        (value["source_fixture"], value["source_engine"],
                         value["source_epoch"], value["target_epoch"]),
                        ("managed-pdns", "pdns", 1, 2),
                    )
                    self.assertEqual(
                        run_cell.expected_journal_phase(selected), expected_phase[cell_id]
                    )

    OWNER_INVERSE = frozenset({
        "bind__intent__after-write__standalone__peer-reachable",
        "bind__target-staged__after-write__standalone__peer-reachable",
    })

    @staticmethod
    def preinstall_document(cell_id: str) -> tuple[dict, bytes]:
        value = {
            "schema": run_cell.SOURCE_PREINSTALL_SCHEMA,
            "cell_id": cell_id,
            "scope": "managed-pdns-source-preparation-for-bind-only",
            "package_install_origin": "harness-source-preinstall",
            "source_packages": [
                {"name": "pdns-backend-sqlite3", "status": "install ok installed",
                 "version": "4.9.2-1+deb13u1"},
                {"name": "pdns-server", "status": "install ok installed",
                 "version": "4.9.2-1+deb13u1"},
            ],
            "measured_target_packages": [{"name": "bind9", "status": "absent"}],
            "install_guard": {
                "unit": "pdns.service",
                "persistent_mask_target": "/dev/null",
                "package_hooks_could_not_start": True,
            },
            "mask_removed_before_external_source_start": True,
            "source_unit_before_external_configuration": {
                "name": "pdns.service", "load_state": "loaded",
                "active_state": "inactive", "unit_file_state": "enabled",
            },
            "dns_state_absent": True,
            "dns_journal_absent": True,
            "dns_ownership_receipts_absent": True,
            "global_udp_tcp_53_bindable": True,
            "production_pdns_adoption_pending": True,
        }
        return value, (json.dumps(value, indent=2, sort_keys=True) + "\n").encode("utf-8")

    def test_controller_refuses_without_owner_inverse_mode(self) -> None:
        """Without the explicit mode the earlier refusals stay byte-identical."""

        for cell_id in sorted(self.ADMITTED):
            selected = run_cell.CellSpec.from_manifest(self.manifest, cell_id)
            value, raw = self.preinstall_document(cell_id)
            with self.subTest(cell_id=cell_id):
                self.assertEqual(
                    run_cell.expected_journal_schema(selected), run_cell.JOURNAL_SCHEMA
                )
                self.assertFalse(run_cell.is_bind_handoff_cell(selected))
                with self.assertRaisesRegex(run_cell.ControllerError, "escaped its exact"):
                    run_cell.validate_source_preinstall_document(value, raw, selected)

    def test_owner_inverse_mode_admits_exactly_two_cells_with_v2_journal(self) -> None:
        for cell_id in sorted(self.ADMITTED):
            selected = run_cell.CellSpec.from_manifest(self.manifest, cell_id)
            value, raw = self.preinstall_document(cell_id)
            owner = cell_id in self.OWNER_INVERSE
            with self.subTest(cell_id=cell_id):
                self.assertEqual(run_cell.is_owner_inverse_cell(selected), owner)
                self.assertEqual(
                    run_cell.expected_journal_schema(
                        selected, owner_inverse_after_restart=True
                    ),
                    run_cell.BIND_HANDOFF_JOURNAL_SCHEMA if owner else run_cell.JOURNAL_SCHEMA,
                )
                if owner:
                    self.assertEqual(
                        run_cell.validate_source_preinstall_document(
                            value, raw, selected, owner_inverse_after_restart=True
                        ),
                        value,
                    )
                else:
                    with self.assertRaisesRegex(run_cell.ControllerError, "escaped its exact"):
                        run_cell.validate_source_preinstall_document(
                            value, raw, selected, owner_inverse_after_restart=True
                        )
        self.assertEqual(run_cell.OWNER_INVERSE_CELLS, self.OWNER_INVERSE)
        self.assertEqual(bootstrap.OWNER_INVERSE_CELLS, self.OWNER_INVERSE)
        # Critical managed-pdns cells keep their unchanged V1 expectation.
        critical = run_cell.CellSpec.from_manifest(
            self.manifest, "bind__source-stopped__after-write__standalone__peer-reachable"
        )
        self.assertEqual(
            run_cell.expected_journal_schema(critical, owner_inverse_after_restart=True),
            run_cell.JOURNAL_SCHEMA,
        )

    def owner_args(self, raw: dict, **overrides) -> mock.Mock:
        values = dict(
            cell_id=raw["id"], node="debian13", source_fixture="managed-pdns",
            identity_file=Path("/tmp/test-key"), execute=False,
            stop_after_kill_for_independent_recovery=False,
            bind_rollback_after_target_started=False,
            owner_inverse_after_restart=True,
        )
        values.update(overrides)
        return mock.Mock(**values)

    def dry_run_prepared(self, raw: dict, args: mock.Mock) -> list:
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch.object(bootstrap.subprocess, "run") as run,
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            self.assertEqual(bootstrap.run_prepared(args), 0)
            run.assert_not_called()
        return json.loads(output.getvalue())

    def test_run_prepared_appends_owner_inverse_flag_only_for_exact_cells(self) -> None:
        for cell_id in sorted(self.OWNER_INVERSE):
            raw = self.raw(cell_id)
            with self.subTest(cell_id=cell_id):
                command = self.dry_run_prepared(raw, self.owner_args(raw))
                self.assertTrue(command[-1].endswith(
                    " " + cell_id + " " + bootstrap.OWNER_INVERSE_FLAG
                ))
                self.assertEqual(
                    command[-1].rsplit("'", 1)[1],
                    " " + cell_id + " " + bootstrap.OWNER_INVERSE_FLAG,
                )
        raw = self.raw("bind__target-staged__after-write__standalone__peer-reachable")
        refusals = (
            (raw, {"source_fixture": "uninitialized"}),
            (raw, {"node": "arch"}),
            (raw, {"stop_after_kill_for_independent_recovery": True}),
            (raw, {"bind_rollback_after_target_started": True}),
            (self.raw("bind__intent__before-write__standalone__peer-unreachable"), {}),
            (self.raw("bind__source-stopped__after-write__standalone__peer-reachable"), {}),
            (self.raw(bootstrap.INDEPENDENT_BIND_HANDOFF_CELL), {}),
            (dict(raw, fault_selector={"phase": "target-staged", "point": "before_write"}), {}),
            (dict(raw, role="paired-primary"), {}),
        )
        for selected, change in refusals:
            with self.subTest(cell=selected["id"], change=change), (
                mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {}))
            ), self.assertRaises(bootstrap.BootstrapError):
                bootstrap.run_prepared(self.owner_args(selected, **change))

    def test_run_prepared_has_no_handoff_for_early_cells(self) -> None:
        raw = self.raw("bind__target-staged__after-write__standalone__peer-reachable")
        args = mock.Mock(
            cell_id=raw["id"], node="debian13", source_fixture="managed-pdns",
            identity_file=Path("/tmp/test-key"), execute=False,
            stop_after_kill_for_independent_recovery=False,
            bind_rollback_after_target_started=False,
        )
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})),
            mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
            mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
            mock.patch.object(bootstrap.subprocess, "run") as run,
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            self.assertEqual(bootstrap.run_prepared(args), 0)
            run.assert_not_called()
        command = json.loads(output.getvalue())
        self.assertTrue(command[-1].endswith(" " + raw["id"]))
        args.stop_after_kill_for_independent_recovery = True
        with (
            mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})),
            self.assertRaises(bootstrap.BootstrapError),
        ):
            bootstrap.run_prepared(args)

    @unittest.skipUnless(
        sys.platform == "linux" and hasattr(os, "geteuid") and os.geteuid() == 0,
        "prepared argv owner proof requires a root Linux fixture",
    )
    def test_guest_program_appends_owner_inverse_flag_only_for_exact_cells(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            prepared = root / "controller-argv.json"
            captured = root / "captured.json"
            executable = root / "run-cell.py"
            executable.write_text(
                "#!/usr/bin/env python3\n"
                "import json, sys\n"
                f"open({str(captured)!r}, 'w').write(json.dumps(sys.argv))\n",
                encoding="utf-8",
            )
            executable.chmod(0o700)
            code = bootstrap.RUN_PREPARED_CODE.replace(
                "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json", str(prepared)
            ).replace("/opt/celikpanel/libexec/dns-kill-run-cell.py", str(executable))
            for cell_id in sorted(self.OWNER_INVERSE):
                base = [str(executable), "--cell-id", cell_id, "--trigger-mode", "socket"]
                prepared.write_text(json.dumps(base), encoding="utf-8")
                prepared.chmod(0o600)
                command = [sys.executable, "-c", code, cell_id, bootstrap.OWNER_INVERSE_FLAG]
                with self.subTest(cell_id=cell_id):
                    captured.unlink(missing_ok=True)
                    self.assertEqual(subprocess.run(command, check=False).returncode, 0)
                    self.assertEqual(
                        json.loads(captured.read_text(encoding="utf-8")),
                        base + [bootstrap.OWNER_INVERSE_FLAG],
                    )
                    for wrong in (
                        command + ["--extra"],
                        command[:3] + [bootstrap.INDEPENDENT_BIND_HANDOFF_CELL,
                                       bootstrap.OWNER_INVERSE_FLAG],
                    ):
                        captured.unlink(missing_ok=True)
                        self.assertNotEqual(
                            subprocess.run(wrong, check=False, capture_output=True).returncode,
                            0,
                        )
                        self.assertFalse(captured.exists())
            cell_id = "bind__intent__after-write__standalone__peer-reachable"
            prepared.write_text(json.dumps(
                [str(executable), "--cell-id", cell_id, "--trigger-mode", "startup"]
            ), encoding="utf-8")
            self.assertNotEqual(subprocess.run(
                [sys.executable, "-c", code, cell_id, bootstrap.OWNER_INVERSE_FLAG],
                check=False, capture_output=True,
            ).returncode, 0)

    def make_runtime(self, root: Path) -> Path:
        runtime = root / "recovery-runtime"
        (runtime / "bin").mkdir(parents=True)
        (runtime / "runtime.manifest").write_text("format=test\n", encoding="utf-8")
        binary = runtime / "bin" / "recovery"
        binary.write_text("#!/bin/sh\n", encoding="utf-8")
        binary.chmod(0o755)
        return runtime

    def test_enroll_recovery_runtime_dry_run_uses_product_enrollment(self) -> None:
        raw = self.raw("bind__intent__after-write__standalone__peer-reachable")
        stage = bootstrap.stage_name(raw["id"])
        with tempfile.TemporaryDirectory() as temporary:
            runtime = self.make_runtime(Path(temporary))
            args = self.owner_args(raw, recovery_runtime=runtime)
            with (
                mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {})),
                mock.patch.object(bootstrap, "identity_file", return_value=Path("/tmp/test-key")),
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh", "guest"]),
                mock.patch.object(bootstrap, "scp_base", return_value=["scp"]),
                mock.patch.object(bootstrap, "remote_destination",
                                  side_effect=lambda _node, path: "guest:" + path),
                mock.patch.object(bootstrap.subprocess, "run") as run,
                mock.patch("sys.stdout", new_callable=io.StringIO) as output,
            ):
                bootstrap.enroll_recovery_runtime(args)
                run.assert_not_called()
            lines = [json.loads(line) for line in output.getvalue().splitlines()]
        commands, summary = lines[:-1], lines[-1]
        self.assertEqual(len(commands), 3)
        self.assertEqual(
            commands[0][-1], f"test -d {stage} && test ! -e {stage}/recovery-kit.tar.gz"
        )
        self.assertEqual(commands[1][0], "scp")
        self.assertTrue(commands[1][1].endswith("/recovery-kit.tar.gz"))
        self.assertEqual(commands[1][2], "guest:" + stage + "/")
        remote = commands[2][-1]
        self.assertTrue(remote.startswith("sudo /bin/bash -c "))
        self.assertTrue(remote.endswith(
            f" enroll-recovery-runtime {stage} {summary['recovery_kit_sha256']}"
        ))
        script = bootstrap.RECOVERY_RUNTIME_ENROLL_SCRIPT
        for fragment in (
            "flock -x -w 60 9",
            'enroll-runtime --source "$root/recovery-runtime" --transaction-fd 9',
            "/usr/libexec/celikpanel/recovery check-bind-source-inverse-v1",
            'test "$marker" = celikpanel-bind-source-inverse/v1',
            'test ! -e "$root"',
        ):
            self.assertIn(fragment, script)
        self.assertLess(script.index("flock -x"), script.index("enroll-runtime"))
        self.assertEqual(summary["launcher"], "/usr/libexec/celikpanel/recovery")
        self.assertEqual(summary["required_capability"], bootstrap.BIND_SOURCE_INVERSE_MARKER)

    def test_enroll_recovery_runtime_refuses_other_cells_and_bad_kits(self) -> None:
        raw = self.raw("bind__target-staged__after-write__standalone__peer-reachable")
        with tempfile.TemporaryDirectory() as temporary:
            runtime = self.make_runtime(Path(temporary))
            for selected, change in (
                (self.raw(bootstrap.INDEPENDENT_BIND_HANDOFF_CELL), {}),
                (raw, {"source_fixture": "uninitialized"}),
            ):
                with self.subTest(cell=selected["id"], change=change), (
                    mock.patch.object(bootstrap, "load_plan", return_value=({}, selected, {}))
                ), self.assertRaises(bootstrap.BootstrapError):
                    bootstrap.enroll_recovery_runtime(
                        self.owner_args(selected, recovery_runtime=runtime, **change)
                    )
            renamed = Path(temporary) / "other"
            renamed.mkdir()
            (runtime / "runtime.manifest").unlink()
            for bad in (renamed, runtime):
                with self.subTest(bad=bad.name), (
                    mock.patch.object(bootstrap, "load_plan", return_value=({}, raw, {}))
                ), self.assertRaises(bootstrap.BootstrapError):
                    bootstrap.enroll_recovery_runtime(
                        self.owner_args(raw, recovery_runtime=bad)
                    )

    def test_parser_exposes_owner_inverse_and_enrollment(self) -> None:
        common = [
            "--work-root", "/tmp/root", "--cell-id",
            "bind__intent__after-write__standalone__peer-reachable",
            "--node", "debian13", "--identity-file", "/tmp/key",
            "--source-fixture", "managed-pdns",
        ]
        args = bootstrap.parse_args(["run-prepared", *common, bootstrap.OWNER_INVERSE_FLAG])
        self.assertIs(args.owner_inverse_after_restart, True)
        args = bootstrap.parse_args(
            ["enroll-recovery-runtime", *common, "--recovery-runtime", "/tmp/recovery-runtime"]
        )
        self.assertEqual(args.recovery_runtime, Path("/tmp/recovery-runtime"))
        self.assertFalse(args.execute)


if __name__ == "__main__":
    unittest.main()
