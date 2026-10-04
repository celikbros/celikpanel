#!/usr/bin/env python3

import argparse
import base64
import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import native_bind_inspector_channel as channel


class NativeBindInspectorChannelTest(unittest.TestCase):
    def test_exact_cell_and_pair_are_required(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = argparse.Namespace(
                work_root=Path(temporary), cell_id="another-cell",
                manifest=Path(temporary) / "manifest.json",
            )
            with self.assertRaisesRegex(ValueError, "exact paired BIND cell"):
                channel.selected_pair(args)

    def test_semantic_catalog_digest_is_framed(self) -> None:
        def frame(value: bytes) -> bytes:
            return len(value).to_bytes(4, "big") + value
        expected = hashlib.sha256(
            frame(b"celikpanel-bind-peer-catalog-members/v1")
            + frame(channel.CATALOG.encode())
            + (2).to_bytes(4, "big")
            + (0).to_bytes(4, "big")
        ).hexdigest()
        self.assertEqual(channel.member_digest(2, []), expected)
        with self.assertRaises(ValueError):
            channel.member_digest(2, ["s1-kill.test", "s1-kill.test"])

    def test_request_is_fresh_and_uses_reviewed_pair(self) -> None:
        args = argparse.Namespace(
            catalog_serial=2, request_id="a" * 32, owner_id="b" * 32,
            generation=5, qualifier="dns-zone-sync/v3:sha256:" + "c" * 64,
            catalog_member=[], deleted_zone="s1-kill.test", attempt=3,
        )
        first = channel.request(args, "d" * 64)
        second = channel.request(args, "d" * 64)
        self.assertNotEqual(first["nonce"], second["nonce"])
        self.assertEqual(first["primary_ip"], "192.0.2.11")
        self.assertEqual(first["peer_ip"], "192.0.2.10")
        self.assertEqual(first["catalog_members_sha256"], channel.member_digest(2, []))
        self.assertLessEqual(first["expires_at_unix"] - first["issued_at_unix"], 30)

    def test_enroll_requires_execute_before_guest_mutation(self) -> None:
        args = argparse.Namespace(execute=False)
        with mock.patch.object(channel, "selected_pair") as selected:
            with self.assertRaisesRegex(ValueError, "requires --execute"):
                channel.enroll(args)
            selected.assert_not_called()


    def test_forced_wrapper_checks_original_command_before_sudo(self) -> None:
        root = Path(channel.__file__).parent
        wrapper = (root / "native_bind_inspector_forced.sh").read_text(encoding="ascii")
        sshd = (root / "native_bind_inspector_sshd.conf").read_text(encoding="ascii")
        self.assertIn(
            '[ "$SSH_ORIGINAL_COMMAND" = "celikpanel-bind-peer-inspect-v1" ] || exit 126',
            wrapper,
        )
        self.assertIn(
            "exec /usr/bin/sudo -n -- /opt/celikpanel/bin/bind-peer-inspect",
            wrapper,
        )
        self.assertLess(wrapper.index("SSH_ORIGINAL_COMMAND"), wrapper.index("exec /usr/bin/sudo"))
        self.assertIn("ForceCommand " + channel.FORCED_WRAPPER, sshd)
        self.assertIn("DisableForwarding yes", sshd)
        self.assertIn("PermitTTY no", sshd)

    def test_agent_enrollment_record_binds_pinned_peer_and_client(self) -> None:
        public = (
            len(b"ssh-ed25519").to_bytes(4, "big") + b"ssh-ed25519"
            + (32).to_bytes(4, "big") + b"x" * 32
        )
        line = "ssh-ed25519 " + base64.b64encode(public).decode("ascii")
        client_digest = channel.public_key_digest(line)
        self.assertEqual(client_digest, hashlib.sha256(public).hexdigest())
        record = channel.agent_enrollment_record(
            "d" * 64, client_digest, "a" * 32, "b" * 32,
        )
        self.assertEqual(record["schema"], "celikpanel-dns-peer-inspection/v1")
        self.assertEqual(record["primary_ip"], "192.0.2.11")
        self.assertEqual(record["peer_ip"], "192.0.2.10")
        self.assertEqual(record["host_key_sha256"], "d" * 64)
        self.assertEqual(record["client_public_key_sha256"], client_digest)
        self.assertEqual(record["credential_id"], "b" * 32)

    def test_agent_enrollment_requires_execute_before_guest_mutation(self) -> None:
        args = argparse.Namespace(execute=False)
        with mock.patch.object(channel, "selected_pair") as selected:
            with self.assertRaisesRegex(ValueError, "requires --execute"):
                channel.agent_enroll(args)
            selected.assert_not_called()


if __name__ == "__main__":
    unittest.main()
