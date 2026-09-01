#!/usr/bin/env python3

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import socket
import sqlite3
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
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
        }
        for name, python_values in expected.items():
            with self.subTest(name=name):
                shell_values = self.shell_canonical_array(shell, name)
                self.assertEqual(len(shell_values), len(set(shell_values)))
                self.assertEqual(frozenset(shell_values), python_values)

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

    def test_managed_pdns_preinstall_is_critical_boundary_only(self) -> None:
        bootstrap.validate_bind_cell(
            cell("debian13", "target-started"), "debian13", "managed-pdns"
        )
        for phase in ("pre-intent", "intent", "target-staged", "committed"):
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
            'cmp -s "$STATE_DIR/dns-engine-state.json" '
            '"$STATE_DIR/dns-engine-ownership-pdns.json"',
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


if __name__ == "__main__":
    unittest.main()
