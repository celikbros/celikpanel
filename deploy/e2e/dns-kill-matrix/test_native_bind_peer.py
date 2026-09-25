#!/usr/bin/env python3

import argparse
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import native_bind_peer as peer
import guest_bootstrap as bootstrap


class NativeBindPeerTest(unittest.TestCase):
    def test_foreign_guest_marker_refuses_before_any_mutation(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = argparse.Namespace(
                work_root=Path(temporary),
                cell_id="bind__intent__after-write__paired-primary__peer-reachable",
                manifest=Path(temporary) / "manifest.json",
                identity_file=Path(temporary) / "key",
                execute=True,
            )
            plan = {
                "nodes": {
                    "arch": {"peer": {"address": "192.0.2.11/24"}},
                    "debian13": {"peer": {"address": "192.0.2.10/24"}},
                }
            }
            cell = {
                "driver": "bind",
                "role": "paired-primary",
                "placement": {"kill_host": "arch"},
            }
            result = subprocess.CompletedProcess(["ssh"], 0, stdout="foreign guest\n")
            with (
                mock.patch.object(peer.fixture, "load_cell_plan", return_value=plan),
                mock.patch.object(bootstrap, "load_manifest_cell", return_value=cell),
                mock.patch.object(bootstrap, "validate_bind_cell"),
                mock.patch.object(bootstrap, "identity_file", return_value=args.identity_file),
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                mock.patch.object(peer.subprocess, "run", return_value=result),
                mock.patch.object(bootstrap, "run") as mutating_run,
            ):
                with self.assertRaises(bootstrap.BootstrapError):
                    peer.prepare(args)
                mutating_run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
