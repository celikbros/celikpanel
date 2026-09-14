#!/usr/bin/env python3
"""Offline contracts for the disposable signed-baseline controller."""
import ast
import os
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("install_baseline", Path(__file__).with_name("install_baseline.py"))
baseline = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = baseline
SPEC.loader.exec_module(baseline)


class BaselineTests(unittest.TestCase):
    def test_historical_bootstrap_is_exact_released_content(self):
        raw = baseline.historical_bootstrap(Path(__file__).resolve().parents[3])
        self.assertIn(b"bootstrap_release_sequence=75\n", raw)
        self.assertIn(b"bootstrap_release_version=v0.1.0-alpha.75\n", raw)

    def test_modified_bootstrap_is_refused(self):
        with mock.patch.object(baseline.subprocess, "run", return_value=types.SimpleNamespace(stdout=b"echo changed")):
            with self.assertRaises(ValueError):
                baseline.historical_bootstrap(Path("."))

    def test_guest_driver_is_valid_and_has_real_install_not_receipt_construction(self):
        record = {"nonce": "a" * 64, "cell_id": "release-recovery__0123456789abcdef"}
        node = {"qemu_command": ["qemu-system-x86_64", "-uuid", "12345678-1234-5678-1234-567812345678"]}
        script = baseline.guest_driver(record, "debian13", node)
        ast.parse(script)
        self.assertIn("/etc/celikpanel-release-recovery-lab", script)
        self.assertIn("/sys/class/dmi/id/product_uuid", script)
        self.assertIn('["/bin/bash",str(bootstrap),"--install","--version",VERSION]', script)
        self.assertIn("secrets.token_urlsafe(32)", script)
        self.assertIn("admin-login.json", script)
        self.assertNotIn("SKIP_ADMIN", script)
        self.assertNotIn("dns-engine-state.json", script)
        self.assertNotIn("--update", script)
        self.assertNotIn("print(credentials", script)

    def test_dry_run_never_contacts_guest(self):
        with mock.patch.object(baseline.lab, "guarded_script", side_effect=AssertionError("guest reached")):
            result = baseline.start(Path("/unused"), {}, {}, "debian13", False)
        self.assertFalse(result["execute"])

    @unittest.skipUnless(hasattr(os, "getuid"), "Linux fixture file ownership contract")
    def test_existing_private_artifact_is_never_silently_replaced(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "artifact"
            baseline.private_file(path, b"original")
            with self.assertRaises(ValueError):
                baseline.private_file(path, b"changed")
            self.assertEqual(path.read_bytes(), b"original")

    def test_every_existing_installation_root_is_checked(self):
        script = baseline.fresh_check()
        for path in baseline.FRESH_PATHS:
            self.assertIn(path, script)
        self.assertIn("guest already contains CelikPanel state", script)


if __name__ == "__main__":
    unittest.main()
