#!/usr/bin/env python3
"""Offline Git/blob and archive proof for the generated independent recovery kit.

These fixtures neither build real binaries nor claim native runtime acceptance.
Geçici Git/arşiv kanıtı; gerçek binary veya yerel çalışma ortamı kabul testi değil.
"""
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location(
    "recovery_candidate_archive", Path(__file__).with_name("candidate_archive.py")
)
archive = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(archive)

KIT_STATIC = {
    "update.sh": "update.sh",
    "rollback.sh": "rollback.sh",
    "deploy/release-transaction-guard.sh": "deploy/release-transaction-guard.sh",
    "deploy/release-unit-transition.sh": "deploy/release-unit-transition.sh",
    "deploy/release-recovery-foundation.sh": "deploy/release-recovery-foundation.sh",
    "deploy/panel-tls-snapshot.sh": "deploy/panel-tls-snapshot.sh",
    "deploy/release-recovery-observation.sh": "deploy/release-recovery-observation.sh",
    "deploy/recovery/runtime-entry.sh": "deploy/release-recovery-runner.sh",
}
KIT_BINARIES = ("recovery", "agent-checker", "panel-checker", "schema17-bridge")


@unittest.skipUnless(shutil.which("git"), "real Git is required for source proof")
class RecoveryCandidateArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.hooks = self.root / "empty-hooks"
        self.hooks.mkdir()
        self.git("init")
        self.sources = {}
        self.files = {}
        self.static_count = 0
        for name in archive.REQUIRED:
            if name.startswith(("bin/", "web/dist/")):
                self.files[name] = ("generated fixture " + name + "\n").encode()
            else:
                source = "download-portal/get.sh" if name == "libexec/get.sh" else name
                self.files[name] = self.source_bytes(source)
                self.static_count += 1
        for destination, source in KIT_STATIC.items():
            self.files["recovery-runtime/" + destination] = self.source_bytes(source)
            self.static_count += 1
        for name in KIT_BINARIES:
            self.files["recovery-runtime/bin/" + name] = ("generated checker fixture " + name + "\n").encode()
        self.files["recovery-runtime/runtime.manifest"] = (
            b"format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n"
            + b"".join(
                (hashlib.sha256(data).hexdigest() + "  " + name.removeprefix("recovery-runtime/") + "\n").encode()
                for name, data in sorted(self.files.items()) if name.startswith("recovery-runtime/")
            )
        )
        self.git("add", "--", *self.sources)
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "isolated source fixture")
        self.commit = self.git("rev-parse", "HEAD").strip()
        self.tree = self.git("rev-parse", "HEAD^{tree}").strip()
        self.files.update({"release.version": b"1\n", "release.commit": (self.commit + "\n").encode(), "release.tree": (self.tree + "\n").encode()})

    def git(self, *args):
        environment = {name: value for name, value in os.environ.items() if not name.startswith("GIT_")}
        result = subprocess.run(
            ["git", "-C", str(self.repository), "-c", "core.autocrlf=false", "-c", "commit.gpgsign=false",
             "-c", "core.hooksPath=" + str(self.hooks), *args],
            check=True, capture_output=True, text=True, env=environment,
        )
        return result.stdout

    def source_bytes(self, name):
        if name not in self.sources:
            data = ("actual committed source: " + name + "\n").encode()
            self.sources[name] = data
            target = self.repository / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        return self.sources[name]

    def manifest(self, files):
        return b"".join((hashlib.sha256(files[name]).hexdigest() + "  ./" + name + "\n").encode() for name in sorted(files))

    def candidate(self, changed=None, manifest=None):
        files = dict(self.files)
        files.update(changed or {})
        files["SHA256SUMS"] = self.manifest(files) if manifest is None else manifest
        path = self.root / "candidate.tar.gz"
        root_name = "celikpanel-v0.1.0-lab.recovery"
        with tarfile.open(path, "w:gz") as bundle:
            directory = tarfile.TarInfo(root_name)
            directory.type = tarfile.DIRTYPE
            bundle.addfile(directory)
            for name, data in files.items():
                entry = tarfile.TarInfo(root_name + "/" + name)
                entry.size, entry.mode = len(data), 0o755
                bundle.addfile(entry, io.BytesIO(data))
        return archive.inspect_archive(path, hashlib.sha256(path.read_bytes()).hexdigest())

    def test_committed_kit_static_sources_and_runner_mapping_are_accepted(self):
        # No fictitious source blob exists for the generated kit or its binaries.
        self.assertFalse((self.repository / "recovery-runtime").exists())
        self.assertFalse((self.repository / "deploy/recovery/runtime-entry.sh").exists())
        candidate = self.candidate()
        proof = archive.verify_committed_source(candidate, self.repository)
        self.assertEqual(proof["commit"], self.commit)
        self.assertEqual(proof["tree"], self.tree)
        self.assertEqual(proof["verified_static_files"], self.static_count)
        self.assertEqual(candidate["provenance"], "unpublished-local-build-not-signed-agent-admission")

    def test_each_modified_kit_static_file_is_rejected_after_valid_archive_proof(self):
        for relative in KIT_STATIC:
            with self.subTest(relative=relative):
                name = "recovery-runtime/" + relative
                candidate = self.candidate({name: b"uncommitted replacement\n"})
                with self.assertRaisesRegex(ValueError, "candidate source bytes differ from commit"):
                    archive.verify_committed_source(candidate, self.repository)

    def test_uncommitted_extra_kit_script_is_not_exempted_as_generated(self):
        candidate = self.candidate({"recovery-runtime/deploy/uncommitted.sh": b"extra script\n"})
        with self.assertRaisesRegex(ValueError, "candidate contains uncommitted source"):
            archive.verify_committed_source(candidate, self.repository)

    def test_source_proof_reads_commit_instead_of_dirty_worktree(self):
        candidate = self.candidate()
        for name in self.sources:
            (self.repository / name).write_bytes(b"dirty working tree\n")
        self.assertEqual(archive.verify_committed_source(candidate, self.repository)["verified_static_files"], self.static_count)

    def test_generated_kit_bytes_still_require_exact_archive_inventory(self):
        manifest = self.manifest(self.files)
        generated = ["recovery-runtime/bin/" + name for name in KIT_BINARIES] + ["recovery-runtime/runtime.manifest"]
        for name in generated:
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "candidate full checksum inventory differs"):
                self.candidate({name: b"changed generated payload\n"}, manifest=manifest)


if __name__ == "__main__":
    unittest.main()
