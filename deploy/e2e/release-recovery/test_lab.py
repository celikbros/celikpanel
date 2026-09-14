#!/usr/bin/env python3
"""Offline lab guard tests; no subprocess, SSH, QEMU or live guest is invoked.

Çevrimdışı laboratuvar koruma testleri; alt süreç, SSH, QEMU veya canlı konuk yok.
"""
import argparse
import ast
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path, PurePosixPath
import stat
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location("release_recovery_lab", Path(__file__).with_name("lab.py"))
lab = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = lab
SPEC.loader.exec_module(lab)
NONCE = "a" * 64
UUID = "f9c6016c-e418-4fdb-9b7c-7ce7c7a69ed8"
RECORD = {"schema": lab.SCHEMA, "nonce": NONCE, "cell_id": "release-recovery__" + NONCE[:16]}


def node():
    return {"paths": {"pid": "/fixture/debian13/qemu.pid", "qmp": "/fixture/debian13/qmp.sock", "directory": "/fixture/debian13"},
            "qemu_command": ["/usr/bin/qemu-system-x86_64", "-uuid", UUID, "-pidfile", "/fixture/debian13/qemu.pid", "-daemonize"],
            "management": {"ssh_host": "127.0.0.1", "ssh_port": 2261}}


class FakePosixPath(PurePosixPath):
    def absolute(self): return self
    def resolve(self, strict=False): return self
    def is_symlink(self): return False


class RootTests(unittest.TestCase):
    def test_only_dedicated_unaliased_roots(self):
        with patch.object(lab, "Path", FakePosixPath), patch.object(lab.fixture, "require_linux_qemu_host") as host:
            self.assertEqual(str(lab.checked_root("/var/tmp/cp-release-drill-one")), "/var/tmp/cp-release-drill-one")
            host.assert_called_once()
            for value in ("/", "/var/tmp", "/tmp/cp-release-drill-one", "/var/tmp/existing", "/var/tmp/cp-release-drill-one/child"):
                with self.subTest(value=value), self.assertRaises(ValueError): lab.checked_root(value)
            with patch.object(FakePosixPath, "resolve", return_value=FakePosixPath("/elsewhere")), self.assertRaises(ValueError):
                lab.checked_root("/var/tmp/cp-release-drill-one")

    def test_private_record_metadata(self):
        info = {"st_mode": stat.S_IFREG | 0o600, "st_uid": 1000, "st_nlink": 1}
        with patch.object(lab.os, "getuid", return_value=1000, create=True):
            safe = SimpleNamespace(lstat=lambda: SimpleNamespace(**info), read_text=lambda: json.dumps(RECORD))
            self.assertEqual(lab.read_private(safe), RECORD)
            for changed in ({"st_mode": stat.S_IFLNK | 0o600}, {"st_mode": stat.S_IFREG | 0o644}, {"st_uid": 1001}, {"st_nlink": 2}):
                bad = SimpleNamespace(lstat=lambda: SimpleNamespace(**dict(info, **changed)), read_text=Mock())
                with self.subTest(changed=changed), self.assertRaises(ValueError): lab.read_private(bad)
                bad.read_text.assert_not_called()

    def test_plan_bytes_are_sealed_and_identity_precedes_loading(self):
        raw = b'{"fixture":"reviewed"}\n'
        record = dict(RECORD, plan_sha256=hashlib.sha256(raw).hexdigest())
        plan = {"cell_directory": "/fixture/cell"}
        with patch.object(lab.fixture, "validate_work_root"), patch.object(lab, "read_private", return_value=record) as read, patch.object(lab.fixture, "load_cell_plan", return_value=plan) as load, patch.object(Path, "read_bytes", return_value=raw):
            self.assertEqual(lab.load(Path("/fixture")), (record, plan))
            with patch.object(Path, "read_bytes", return_value=raw + b"changed"), self.assertRaisesRegex(ValueError, "plan changed"):
                lab.load(Path("/fixture"))
            load.reset_mock()
            read.return_value = dict(RECORD, nonce="invalid")
            with self.assertRaises(ValueError): lab.load(Path("/fixture"))
            load.assert_not_called()


class ProcessTests(unittest.TestCase):
    def test_registered_process_and_loopback(self):
        vm = node()
        raw = b"\0".join(os.fsencode(x) for x in vm["qemu_command"]) + b"\0"
        with patch.object(Path, "read_text", return_value="4321\n"), patch.object(Path, "read_bytes", return_value=raw):
            self.assertTrue(lab.process_guard(vm))
            vm["management"]["ssh_host"] = "72.62.38.15"
            with self.assertRaisesRegex(ValueError, "non-loopback"): lab.process_guard(vm)

    def test_reused_pid_and_unknown_identity_refuse_even_for_stop(self):
        for raw in (b"/usr/bin/sshd\0-D\0", b"/usr/bin/qemu-system-x86_64\0-uuid\0different\0"):
            with patch.object(Path, "read_text", return_value="4321"), patch.object(Path, "read_bytes", return_value=raw):
                with self.assertRaises(ValueError): lab.process_guard(node(), allow_dead=True)
        with patch.object(Path, "read_text", return_value="1"), self.assertRaises(ValueError): lab.process_guard(node())

    def test_dead_node_only_allowed_for_cleanup(self):
        with patch.object(Path, "read_text", return_value="4321"), patch.object(Path, "read_bytes", side_effect=FileNotFoundError):
            with self.assertRaises(ValueError): lab.process_guard(node())
            self.assertFalse(lab.process_guard(node(), allow_dead=True))
        with patch.object(Path, "read_text", side_effect=FileNotFoundError), patch.object(Path, "exists", return_value=False):
            self.assertFalse(lab.process_guard(node(), allow_dead=True))
            with self.assertRaises(ValueError): lab.process_guard(node())
        with patch.object(Path, "read_text", side_effect=FileNotFoundError), patch.object(Path, "exists", return_value=True), self.assertRaises(ValueError):
            lab.process_guard(node(), allow_dead=True)

    def test_stop_filters_dead_nodes_without_targeting_their_stale_socket(self):
        first, second = node(), node()
        plan = {"nodes": {"debian13": first, "arch": second}, "start_order": ["debian13", "arch"]}
        with patch.object(sys, "argv", ["lab.py", "stop", "--work-root", "/fixture", "--execute"]), patch.object(lab, "checked_root", return_value=Path("/fixture")), patch.object(lab, "load", return_value=(RECORD, plan)), patch.object(lab, "process_guard", side_effect=[False, True]) as guard, patch.object(lab.fixture, "stop_vms") as stop, contextlib.redirect_stdout(io.StringIO()):
            lab.main()
        self.assertTrue(all(call.kwargs == {"allow_dead": True} for call in guard.call_args_list))
        self.assertEqual(stop.call_args.args[0]["nodes"], {"arch": second})
        self.assertEqual(stop.call_args.args[0]["start_order"], ["arch"])


class TransportTests(unittest.TestCase):
    def test_ssh_pins_keys_and_exact_loopback_destination(self):
        with patch.object(lab, "process_guard"):
            argv = lab.ssh(Path("/fixture"), RECORD, node())
            ready = lab.ssh(Path("/fixture"), RECORD, node(), readiness=True)
        self.assertIn("StrictHostKeyChecking=yes", argv)
        self.assertNotIn("StrictHostKeyChecking=accept-new", argv)
        self.assertIn("StrictHostKeyChecking=accept-new", ready)
        self.assertIn("celik@127.0.0.1", argv)
        self.assertEqual(argv[argv.index("-p") + 1], "2261")

    def test_guest_guard_survives_python_optimization_and_precedes_body(self):
        guard = lab.guest_guard(RECORD, "debian13", node())
        self.assertFalse(any(isinstance(item, ast.Assert) for item in ast.walk(ast.parse(guard))))
        for value in (NONCE, UUID, RECORD["cell_id"], lab.MARKER, "systemd"): self.assertIn(value, guard)
        with patch.object(lab, "ssh", return_value=["ssh", "celik@127.0.0.1"]), patch.object(lab, "run") as run:
            lab.guarded_script(Path("/fixture"), RECORD, {"nodes": {"debian13": node()}}, "debian13", "printf 'body'\n")
        program = run.call_args.kwargs["input"]
        self.assertTrue(program.startswith("set -eu\npython3 -I -"))
        self.assertLess(program.index(lab.MARKER), program.index("printf 'body'"))

    @unittest.skipUnless(sys.platform.startswith("linux"), "native O_NOFOLLOW/O_NONBLOCK upload boundary requires Linux")
    def test_upload_only_private_basenames_and_digest_confirmed_payloads(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "payload"
            data = b"public fixture program\n"
            source.write_bytes(data)
            digest = hashlib.sha256(data).hexdigest()
            plan = {"nodes": {"debian13": node()}}
            with patch.object(lab, "ssh", return_value=["ssh", "celik@127.0.0.1"]), patch.object(lab, "run", return_value=subprocess.CompletedProcess([], 0, (digest + "\n").encode(), b"")) as run:
                self.assertEqual(lab.put_file(Path("/fixture"), RECORD, plan, "debian13", source, "guest-probe.py"), ("/root/celikpanel-release-recovery-lab/guest-probe.py", digest))
                self.assertEqual(run.call_args.kwargs["input"], data)
                remote = run.call_args.args[0][-1]
                self.assertIn("sudo python3 -I -c ", remote)
                self.assertIn(NONCE, remote)
                self.assertIn("os.fsync", remote)
                self.assertIn("os.replace", remote)
                for name, mode in (("../escape", 0o600), ("/etc/passwd", 0o600), ("file", 0o777)):
                    run.reset_mock()
                    with self.assertRaises(ValueError): lab.put_file(Path("/fixture"), RECORD, plan, "debian13", source, name, mode=mode)
                    run.assert_not_called()
                run.return_value = subprocess.CompletedProcess([], 0, b"wrong digest\n", b"")
                with self.assertRaisesRegex(ValueError, "digest"): lab.put_file(Path("/fixture"), RECORD, plan, "debian13", source, "file")


class PrepareTests(unittest.TestCase):
    def test_dry_run_and_bad_ports_never_create_root_key_images_or_vm(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "new-lab"
            args = argparse.Namespace(work_root=str(root), image_cache=directory, ssh_port=2261, execute=False)
            with patch.object(lab, "checked_root", return_value=root), patch.object(lab.fixture, "load_image_lock", return_value={}), patch.object(lab.fixture, "initialize_work_root") as initialize, patch.object(lab.fixture, "execute_prepare") as execute, patch.object(lab, "run") as run, contextlib.redirect_stdout(io.StringIO()):
                lab.prepare(args)
                for port in (22, 0, 65534, -1):
                    args.ssh_port, args.execute = port, True
                    with self.subTest(port=port), self.assertRaises(ValueError): lab.prepare(args)
            initialize.assert_not_called()
            execute.assert_not_called()
            run.assert_not_called()
            self.assertFalse(root.exists())

    def test_existing_root_refused_before_image_work(self):
        with tempfile.TemporaryDirectory() as directory:
            args = argparse.Namespace(work_root=directory, image_cache=directory, ssh_port=2261, execute=False)
            with patch.object(lab, "checked_root", return_value=Path(directory)), patch.object(lab.fixture, "load_image_lock") as load, self.assertRaisesRegex(ValueError, "already exists"):
                lab.prepare(args)
            load.assert_not_called()


if __name__ == "__main__":
    unittest.main()
