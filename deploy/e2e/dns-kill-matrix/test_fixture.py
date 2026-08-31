#!/usr/bin/env python3

from __future__ import annotations

import hashlib
import io
import json
import os
import stat
import tempfile
import unittest
from unittest import mock
from pathlib import Path

import fixture


CELL_ID = "bind__intent__before-write__standalone__peer-reachable"
PUBLIC_KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmVLZXlGb3JUZXN0T25seQ fixture@test"


class FixtureGenerationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.parent = Path(self.temporary.name)
        self.root = fixture.initialize_work_root(self.parent / "harness" / "work")
        self.contents = {
            "debian13": b"synthetic Debian fixture image\n",
            "arch": b"synthetic Arch fixture image\n",
        }
        self.lock_path = self.parent / "images.lock.json"
        self.lock = self.make_lock()
        self.write_lock(self.lock)
        self.pins = fixture.load_image_lock(self.lock_path)

    def make_lock(self) -> dict[str, object]:
        images: dict[str, object] = {}
        definitions = {
            "debian13": {
                "distribution": "Debian",
                "release": "13",
                "url": (
                    "https://cloud.debian.org/images/cloud/trixie/20260826-2582/"
                    "debian-13-genericcloud-amd64-20260826-2582.qcow2"
                ),
                "filename": "debian-13-genericcloud-amd64-20260826-2582.qcow2",
            },
            "arch": {
                "distribution": "Arch Linux",
                "release": "rolling",
                "url": (
                    "https://geo.mirror.pkgbuild.com/images/v20260815.573966/"
                    "Arch-Linux-x86_64-cloudimg-20260815.573966.qcow2"
                ),
                "filename": "Arch-Linux-x86_64-cloudimg-20260815.573966.qcow2",
            },
        }
        for name, definition in definitions.items():
            data = self.contents[name]
            algorithm = "sha512" if name == "debian13" else "sha256"
            images[name] = {
                **definition,
                "architecture": "x86_64",
                "digest": {
                    "algorithm": algorithm,
                    "value": hashlib.new(algorithm, data).hexdigest(),
                },
                "bytes": len(data),
            }
        return {"schema": fixture.LOCK_SCHEMA, "images": images}

    def write_lock(self, value: object) -> None:
        self.lock_path.write_text(
            json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )

    def install_fake_bases(self) -> None:
        for name, pin in self.pins.items():
            path = fixture.image_path(self.root, pin)
            path.write_bytes(self.contents[name])
            os.chmod(path, 0o444)

    def test_lock_requires_exact_official_immutable_urls_and_real_hashes(self) -> None:
        self.assertEqual(set(self.pins), {"debian13", "arch"})

        invalid = self.make_lock()
        invalid["images"]["debian13"]["digest"]["value"] = "REPLACE_ME"
        self.write_lock(invalid)
        with self.assertRaisesRegex(fixture.FixtureError, "sha512"):
            fixture.load_image_lock(self.lock_path)

        invalid = self.make_lock()
        invalid["images"]["debian13"]["digest"]["algorithm"] = "sha256"
        self.write_lock(invalid)
        with self.assertRaisesRegex(fixture.FixtureError, "official sha512"):
            fixture.load_image_lock(self.lock_path)

        invalid = self.make_lock()
        invalid["images"]["arch"]["url"] = (
            "https://geo.mirror.pkgbuild.com/images/latest/"
            "Arch-Linux-x86_64-cloudimg.qcow2"
        )
        invalid["images"]["arch"]["filename"] = "Arch-Linux-x86_64-cloudimg.qcow2"
        self.write_lock(invalid)
        with self.assertRaisesRegex(fixture.FixtureError, "immutable"):
            fixture.load_image_lock(self.lock_path)

    def test_missing_mismatched_or_writable_base_fails_closed(self) -> None:
        with self.assertRaisesRegex(fixture.FixtureError, "missing"):
            fixture.verify_images(self.root, self.pins)

        self.install_fake_bases()
        verified = fixture.verify_images(self.root, self.pins)
        self.assertEqual(set(verified), {"debian13", "arch"})

        debian = fixture.image_path(self.root, self.pins["debian13"])
        os.chmod(debian, stat.S_IREAD | stat.S_IWRITE)
        with self.assertRaisesRegex(fixture.FixtureError, "writable"):
            fixture.verify_images(self.root, self.pins)
        os.chmod(debian, stat.S_IREAD | stat.S_IWRITE)
        debian.write_bytes(b"wrong")
        os.chmod(debian, stat.S_IREAD)
        with self.assertRaisesRegex(fixture.FixtureError, "size mismatch"):
            fixture.verify_images(self.root, self.pins)

    def test_plan_has_fresh_overlays_nocloud_nat_isolated_peer_and_qmp(self) -> None:
        self.install_fake_bases()
        plan = fixture.build_cell_plan(
            self.root,
            self.pins,
            CELL_ID,
            PUBLIC_KEY,
            debian_ssh_port=2221,
            arch_ssh_port=2222,
            peer_port=23053,
        )
        self.assertEqual(plan["start_order"], ["debian13", "arch"])
        self.assertEqual(plan["host_requirements"]["os"], "linux")
        self.assertEqual(plan["peer_link_policy"]["change_requires"]["exit_code"], 137)
        self.assertTrue(plan["peer_link_policy"]["change_requires"]["kill_proven"])

        debian = plan["nodes"]["debian13"]
        arch = plan["nodes"]["arch"]
        for node, address, ssh_port in (
            (debian, "192.0.2.10/24", 2221),
            (arch, "192.0.2.11/24", 2222),
        ):
            overlay = node["overlay_command"]
            self.assertEqual(overlay[:6], ["qemu-img", "create", "-f", "qcow2", "-F", "qcow2"])
            self.assertIn("-b", overlay)
            self.assertEqual(overlay[-1], "24G")
            self.assertNotEqual(node["paths"]["overlay"], node["base"]["path"])
            self.assertIn(address, node["cloud_init"]["network-config"])
            self.assertIn("dhcp4: true", node["cloud_init"]["network-config"])
            self.assertIn(PUBLIC_KEY, node["cloud_init"]["user-data"])
            self.assertIn("cidata", node["seed_command"])
            command = " ".join(node["qemu_command"])
            self.assertIn(f"hostfwd=tcp:127.0.0.1:{ssh_port}-:22", command)
            self.assertIn("id=peer-link", command)
            self.assertIn("-qmp", node["qemu_command"])
            self.assertIn("qmp.sock", command)
            if fixture.sys.platform == "linux":
                self.assertLessEqual(len(os.fsencode(node["paths"]["qmp"])), 100)
        self.assertIn("groups: [sudo]", debian["cloud_init"]["user-data"])
        self.assertIn("groups: [wheel]", arch["cloud_init"]["user-data"])
        self.assertIn("listen=127.0.0.1:23053", " ".join(debian["qemu_command"]))
        self.assertIn("connect=127.0.0.1:23053", " ".join(arch["qemu_command"]))

    def test_xorriso_seed_command_needs_no_python_cloud_init_package(self) -> None:
        plan = fixture.build_cell_plan(
            self.root,
            self.pins,
            CELL_ID,
            PUBLIC_KEY,
            iso_tool="xorriso",
        )
        command = plan["nodes"]["debian13"]["seed_command"]
        self.assertEqual(command[:3], ["xorriso", "-as", "mkisofs"])
        self.assertIn("user-data", " ".join(command))
        self.assertIn("meta-data", " ".join(command))
        self.assertIn("network-config", " ".join(command))

    def test_peer_link_requires_exact_atomic_exit_137_proof(self) -> None:
        proof_path = self.parent / "kill-proof.json"
        proof = {
            "schema": fixture.KILL_PROOF_SCHEMA,
            "cell_id": CELL_ID,
            "kill_proven": True,
            "exit_code": 137,
            "pid": 4242,
        }
        proof_path.write_text(json.dumps(proof) + "\n", encoding="utf-8")
        self.assertEqual(fixture.validate_kill_proof(proof_path, CELL_ID), proof)
        proof["exit_code"] = 1
        proof_path.write_text(json.dumps(proof) + "\n", encoding="utf-8")
        with self.assertRaisesRegex(fixture.FixtureError, "exit-137"):
            fixture.validate_kill_proof(proof_path, CELL_ID)

    def test_root_and_cell_guards_reject_broad_or_escaping_targets(self) -> None:
        with self.assertRaisesRegex(fixture.FixtureError, "absolute"):
            fixture.canonical_work_root(Path("relative/work"))
        with self.assertRaisesRegex(fixture.FixtureError, "unsafe"):
            fixture.canonical_work_root(Path(self.parent.anchor))
        with self.assertRaisesRegex(fixture.FixtureError, "invalid matrix cell"):
            fixture.cell_directory(self.root, "../../outside", must_exist=False)
        target = fixture.cell_directory(self.root, CELL_ID, must_exist=False)
        self.assertEqual(target.parent, (self.root / "cells").resolve())
        self.assertRegex(target.name, r"^[0-9a-f]{24}$")
        self.assertNotEqual(target.name, CELL_ID)

        (self.root / "cells").rmdir()
        with self.assertRaisesRegex(fixture.FixtureError, "directory is missing"):
            fixture.validate_work_root(self.root)

    def test_non_linux_qemu_lifecycle_fails_closed_even_for_dry_run(self) -> None:
        fixture.require_linux_qemu_host("linux")
        with self.assertRaisesRegex(fixture.FixtureError, "Linux host"):
            fixture.require_linux_qemu_host("win32")
        with mock.patch.object(fixture.sys, "platform", "win32"), mock.patch(
            "sys.stderr", new=io.StringIO()
        ):
            result = fixture.main(
                [
                    "start",
                    "--work-root",
                    str(self.root),
                    "--cell-id",
                    CELL_ID,
                ]
            )
        self.assertEqual(result, 2)

    def test_fetch_and_ssh_commands_are_reconstructible_dry_run_data(self) -> None:
        fetch = fixture.fetch_plan(self.root, self.pins)
        self.assertEqual(len(fetch["downloads"]), 2)
        for download in fetch["downloads"]:
            self.assertEqual(download["command"][0], "curl")
            self.assertIn("=https", download["command"])
            self.assertEqual(
                download["command"][download["command"].index("--proto-redir") + 1],
                "=https",
            )
            self.assertTrue(download["target"].startswith(str(self.root / "images")))
            self.assertEqual(set(download["digest"]), {"algorithm", "value"})

        plan = fixture.build_cell_plan(self.root, self.pins, CELL_ID, PUBLIC_KEY)
        identity = self.parent / "id_ed25519"
        command = fixture.ssh_command(plan["nodes"]["debian13"], identity)
        joined = " ".join(command)
        self.assertIn("StrictHostKeyChecking=accept-new", joined)
        self.assertIn("cloud-init status --wait", joined)
        self.assertIn("boot-finished", joined)


if __name__ == "__main__":
    unittest.main()
