#!/usr/bin/env python3
"""Focused Linux tests for the generic atomic portal transaction."""

from __future__ import annotations

import contextlib
import hashlib
import importlib.util
import io
import json
import os
import shutil
import subprocess
import sys
import tarfile
import tempfile
import textwrap
import unittest
from pathlib import Path


DEPLOY = Path(__file__).resolve().parent
PROMOTER_PATH = DEPLOY / "promote-download-portal.py"
PREVIOUS = "v0.1.0-alpha.43"
TARGET = "v0.1.0-alpha.44"
FAKE_VERIFIER = textwrap.dedent(
    """
    import json
    import os
    from pathlib import Path

    HARD_REQUEST_LIMIT = 15

    class Target:
        def __init__(self, version):
            self.version = version

    def _version(site):
        return (Path(site) / "releases" / "latest.txt").read_text("ascii").strip()

    def _record(value):
        path = Path(os.environ["CELIKPANEL_TEST_PUBLIC_LOG"])
        with path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(value, sort_keys=True) + "\\n")

    def load_target(site):
        version = _version(site)
        _record({"kind": "local", "site": str(site), "version": version, "paths": []})
        return Target(version)

    def verify_public_portal(site_root, base_url, target_version, *, opener=None, timeout=30.0):
        version = _version(site_root)
        paths = [
            "/index.html",
            "/releases/latest.txt",
            "/releases/" + target_version + "/new.bin",
        ]
        _record({
            "kind": "public",
            "site": str(site_root),
            "base_url": base_url,
            "version": version,
            "target_version": target_version,
            "paths": paths,
        })
        if os.environ.get("CELIKPANEL_TEST_FAIL_PUBLIC") == "1":
            raise RuntimeError("injected first public-pass failure")
        if version != target_version:
            raise RuntimeError("public target mismatch")
        return {
            "status": "ok",
            "requests": len(paths),
            "request_limit": HARD_REQUEST_LIMIT,
            "archive_gets": 1,
            "paths": paths,
        }
    """
).lstrip()


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_promoter():
    name = f"test_portal_promoter_{os.getpid()}_{id(object())}"
    spec = importlib.util.spec_from_file_location(name, PROMOTER_PATH)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


class Fixture:
    def __init__(self, base: Path):
        self.base = base
        self.root = base / "portal-root"
        self.live = self.root / "httpdocs"
        self.backups = self.root / "portal-backups"
        self.lock = self.root / ".portal-deploy.lock"
        self.upload = self.root / ".upload-portal-test"
        self.package = self.upload / "portal.tar.gz"
        self.verifier = self.upload / "verify-download-portal-public.py"
        self.log = base / "verifier-calls.jsonl"

        (self.live / "releases" / PREVIOUS).mkdir(parents=True)
        self.backups.mkdir()
        self.upload.mkdir(mode=0o700)
        self.lock.touch()
        (self.live / "index.html").write_text("old root\n", encoding="utf-8")
        (self.live / "releases" / "latest.txt").write_text(PREVIOUS + "\n", encoding="ascii")
        (self.live / "releases" / "latest.json").write_text(
            json.dumps({"version": PREVIOUS}) + "\n", encoding="utf-8"
        )
        (self.live / "releases" / "index.json").write_text(
            json.dumps({"latest": PREVIOUS}) + "\n", encoding="utf-8"
        )
        (self.live / "releases" / PREVIOUS / "old.bin").write_bytes(b"historical-bytes")

        source = base / "package-source" / "portal"
        (source / "releases" / TARGET).mkdir(parents=True)
        (source / "index.html").write_text("new root\n", encoding="utf-8")
        (source / "releases" / "latest.txt").write_text(TARGET + "\n", encoding="ascii")
        (source / "releases" / "latest.json").write_text(
            json.dumps({"version": TARGET}) + "\n", encoding="utf-8"
        )
        (source / "releases" / "index.json").write_text(
            json.dumps({"latest": TARGET}) + "\n", encoding="utf-8"
        )
        (source / "releases" / TARGET / "new.bin").write_bytes(b"target-bytes")
        with tarfile.open(self.package, "w:gz", format=tarfile.PAX_FORMAT) as archive:
            archive.add(source, arcname="portal", recursive=True)
        self.verifier.write_text(FAKE_VERIFIER, encoding="utf-8", newline="\n")
        os.chmod(self.package, 0o600)
        os.chmod(self.verifier, 0o600)
        self.package_size = self.package.stat().st_size
        self.package_sha = sha256(self.package)
        self.verifier_size = self.verifier.stat().st_size
        self.verifier_sha = sha256(self.verifier)
        self.old_live_inode = self.live.stat().st_ino

    def argv(self) -> list[str]:
        return [
            "--root", str(self.root),
            "--live", str(self.live),
            "--backups", str(self.backups),
            "--lock", str(self.lock),
            "--upload-dir", str(self.upload),
            "--package", str(self.package),
            "--package-size", str(self.package_size),
            "--package-sha256", self.package_sha,
            "--verifier", str(self.verifier),
            "--verifier-size", str(self.verifier_size),
            "--verifier-sha256", self.verifier_sha,
            "--previous-version", PREVIOUS,
            "--target-version", TARGET,
            "--public-base-url", "https://portal.test",
            "--public-timeout", "2",
            "--public-total-timeout", "10",
        ]

    def environment(self, fail_public: bool = False) -> dict[str, str]:
        result = dict(os.environ)
        result["CELIKPANEL_TEST_PUBLIC_LOG"] = str(self.log)
        if fail_public:
            result["CELIKPANEL_TEST_FAIL_PUBLIC"] = "1"
        else:
            result.pop("CELIKPANEL_TEST_FAIL_PUBLIC", None)
        return result

    def calls(self) -> list[dict]:
        if not self.log.exists():
            return []
        return [
            json.loads(line)
            for line in self.log.read_text(encoding="utf-8").splitlines()
            if line
        ]


@unittest.skipUnless(os.name == "posix" and sys.platform.startswith("linux"), "Linux only")
class DownloadPortalPromotionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="celikpanel-portal-promotion-")
        self.fixture = Fixture(Path(self.temporary.name))

    def tearDown(self):
        self.temporary.cleanup()

    def run_subprocess(self, *, fail_public: bool = False) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(PROMOTER_PATH), *self.fixture.argv()],
            env=self.fixture.environment(fail_public),
            text=True,
            capture_output=True,
            timeout=30,
            check=False,
        )

    def public_calls(self) -> list[dict]:
        return [value for value in self.fixture.calls() if value["kind"] == "public"]

    def assert_no_historical_verifier_path(self):
        for call in self.public_calls():
            self.assertNotIn(PREVIOUS, "\n".join(call["paths"]))

    def test_success_has_one_public_pass_and_exact_old_inode_backup(self):
        result = self.run_subprocess()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines()[-1], "CELIKPANEL_DOWNLOAD_PORTAL_PUBLISHED")
        self.assertEqual(len(self.public_calls()), 1)
        self.assert_no_historical_verifier_path()
        self.assertEqual(
            (self.fixture.live / "releases" / "latest.txt").read_text("ascii").strip(),
            TARGET,
        )
        backups = list(self.fixture.backups.iterdir())
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].stat().st_ino, self.fixture.old_live_inode)
        self.assertEqual(
            (backups[0] / "releases" / PREVIOUS / "old.bin").read_bytes(),
            b"historical-bytes",
        )
        self.assertEqual(
            (self.fixture.live / "releases" / PREVIOUS / "old.bin").read_bytes(),
            b"historical-bytes",
        )
        self.assertTrue(self.fixture.package.exists(), "upload evidence must be retained")

    def test_first_public_failure_rolls_back_and_quarantines_stage(self):
        result = self.run_subprocess(fail_public=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(self.public_calls()), 1)
        self.assert_no_historical_verifier_path()
        self.assertEqual(self.fixture.live.stat().st_ino, self.fixture.old_live_inode)
        self.assertEqual(
            (self.fixture.live / "releases" / "latest.txt").read_text("ascii").strip(),
            PREVIOUS,
        )
        self.assertEqual(list(self.fixture.backups.iterdir()), [])
        stages = list(self.fixture.root.glob(".stage-portal-*"))
        self.assertEqual(len(stages), 1)
        self.assertEqual(
            (stages[0] / "releases" / "latest.txt").read_text("ascii").strip(),
            TARGET,
        )
        self.assertEqual(list(self.fixture.root.glob(".failed-portal-*")), [])
        self.assertTrue(self.fixture.package.exists())

    def test_after_backup_local_failure_rolls_back_to_failed_quarantine(self):
        promoter = load_promoter()
        original = promoter.verify_backup_local

        def injected_failure(_backup, _expected):
            raise promoter.PromotionError("injected after-backup local failure")

        promoter.verify_backup_local = injected_failure
        stdout = io.StringIO()
        stderr = io.StringIO()
        previous_environment = os.environ.copy()
        os.environ.update(self.fixture.environment())
        try:
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                result = promoter.main(self.fixture.argv())
        finally:
            promoter.verify_backup_local = original
            os.environ.clear()
            os.environ.update(previous_environment)
        self.assertEqual(result, 1)
        self.assertEqual(len(self.public_calls()), 1)
        self.assertEqual(self.fixture.live.stat().st_ino, self.fixture.old_live_inode)
        self.assertEqual(list(self.fixture.backups.iterdir()), [])
        self.assertEqual(list(self.fixture.root.glob(".stage-portal-*")), [])
        failed = list(self.fixture.root.glob(".failed-portal-*"))
        self.assertEqual(len(failed), 1)
        self.assertEqual(
            (failed[0] / "releases" / "latest.txt").read_text("ascii").strip(),
            TARGET,
        )
        self.assertNotIn("CELIKPANEL_DOWNLOAD_PORTAL_PUBLISHED", stdout.getvalue())
        self.assertTrue(self.fixture.package.exists())

    def test_source_contract_has_one_public_call_before_backup_and_none_after(self):
        source = PROMOTER_PATH.read_text(encoding="utf-8")
        call = "verifier.verify_public_portal("
        self.assertEqual(source.count(call), 1)
        public_offset = source.index(call)
        backup_offset = source.index("renameat2(state.stage, state.backup, RENAME_NOREPLACE)")
        self.assertLess(public_offset, backup_offset)
        self.assertNotIn(call, source[backup_offset:])
        publisher = (DEPLOY / "publish-download-portal.ps1").read_text(encoding="utf-8")
        self.assertIn("# The promoter is streamed exactly once.", publisher)
        self.assertIn(".BaseStream.WriteAsync(", publisher)
        self.assertNotIn(".BaseStream.Write(", publisher)
        self.assertNotIn("$Process.WaitForExit()", publisher)
        self.assertIn("$Process.WaitForExit(5000)", publisher)
        self.assertNotIn("for ($Retry", publisher)
        self.assertNotIn("while ($", publisher)


if __name__ == "__main__":
    unittest.main(verbosity=2)
