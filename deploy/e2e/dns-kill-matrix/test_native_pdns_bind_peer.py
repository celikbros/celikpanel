#!/usr/bin/env python3

import argparse
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import guest_bootstrap as bootstrap
import native_pdns_bind_peer as peer


def catalog_axfr(serial: int = 1, member: str = peer.ZONE) -> str:
    catalog = peer.catalog_name("192.0.2.10") + "."
    soa = f"{catalog} 60 IN SOA invalid. invalid. {serial} 60 30 3600 30\n"
    label = hashlib.sha224(member.encode("ascii")).hexdigest()
    return (
        ";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n" + soa
        + f"{catalog} 60 IN NS invalid.\n"
        + f'version.{catalog} 60 IN TXT "2"\n'
        + f"{label}.zones.{catalog} 60 IN PTR {member}.\n" + soa
    )


def pdns_catalog_axfr(serial: int = 1, member: str = peer.ZONE) -> str:
    catalog = peer.catalog_name("192.0.2.10") + "."
    soa = f"{catalog} 60 IN SOA invalid. invalid. {serial} 60 30 3600 30\n"
    return (
        ";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n" + soa
        + f"{catalog} 60 IN NS invalid.\n"
        + f'version.{catalog} 60 IN TXT "2"\n'
        + f"tlhnkturltuku63bhersj40lbi0vkdvt.zones.{catalog} 60 IN PTR {member}.\n" + soa
    )


def member_answer(name: str, kind: str, value: str, *, authoritative: bool = True) -> str:
    flags = "qr aa" if authoritative else "qr"
    return (
        f";; ->>HEADER<<- opcode: QUERY, status: NOERROR\n;; flags: {flags};\n"
        f"{name}. 60 IN {kind} {value}\n"
    )


class NativePDNSBINDPeerTest(unittest.TestCase):
    def args(self, root: str, action: str = "prepare", execute: bool = False) -> argparse.Namespace:
        return argparse.Namespace(
            action=action, work_root=Path(root), cell_id=peer.CELL,
            manifest=Path(root) / "manifest.json", identity_file=Path(root) / "key",
            source_fixture="managed-bind",
            address="192.0.2.10" if action == "observe" else None, execute=execute,
        )

    def plan(self) -> dict:
        return {"nodes": {
            "debian13": {"peer": {"address": "192.0.2.10/24"}},
            "arch": {"peer": {"address": "192.0.2.11/24"}},
        }}

    def cell(self) -> dict:
        return {"id": peer.CELL, "driver": "pdns-switch", "role": "paired-primary",
                "placement": {"kill_host": "debian-13", "source_fixture_policy": "driver-specific"},
            "boundary": {"phase": "intent"}}

    def test_cross_engine_config_and_exact_cell(self) -> None:
        config = peer.secondary_config("192.0.2.10", "192.0.2.11")
        self.assertIn('zone "catalog-c000020a.celikpanel.invalid" default-primaries { 192.0.2.10; } in-memory yes;', config)
        self.assertIn('zone "catalog-c000020a.celikpanel.invalid" {\n    type secondary;', config)
        self.assertIn("allow-transfer { 192.0.2.10; 127.0.0.1; };", config)
        self.assertIn('file "celikpanel-fixture-catalog.zone";', config)
        with self.assertRaises(bootstrap.BootstrapError):
            peer.secondary_config("192.0.2.10", "192.0.2.10")
        with self.assertRaises(bootstrap.BootstrapError):
            peer.pair_addresses(self.plan(), {**self.cell(), "id": "another-cell"})

    def test_fresh_peer_selection_uses_exact_empty_source_policy(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = self.args(temporary)
            args.source_fixture = "uninitialized"
            with (
                mock.patch.object(peer.fixture, "load_cell_plan", return_value=self.plan()),
                mock.patch.object(bootstrap, "load_manifest_cell", return_value=self.cell()),
                mock.patch.object(bootstrap, "identity_file", return_value=args.identity_file),
            ):
                self.assertEqual(peer.selected(args)[:2], ("192.0.2.10", "192.0.2.11"))

    def test_foreign_guest_marker_refuses_before_any_mutation(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = self.args(temporary, execute=True)
            with (
                mock.patch.object(peer.fixture, "load_cell_plan", return_value=self.plan()),
                mock.patch.object(bootstrap, "load_manifest_cell", return_value=self.cell()),
                mock.patch.object(bootstrap, "validate_pdns_switch_cell"),
                mock.patch.object(bootstrap, "identity_file", return_value=args.identity_file),
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                mock.patch.object(peer.subprocess, "run", return_value=subprocess.CompletedProcess(
                    ["ssh"], 0, stdout="foreign guest\n")),
                mock.patch.object(bootstrap, "run") as mutating_run,
            ):
                with self.assertRaisesRegex(bootstrap.BootstrapError, "marker"):
                    peer.prepare(args)
                mutating_run.assert_not_called()

    def test_prepare_stages_catalog_consumer_before_primary_exists(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = self.args(temporary)
            with (
                mock.patch.object(peer, "selected", return_value=(
                    "192.0.2.10", "192.0.2.11", {}, args.identity_file)),
                mock.patch.object(peer, "verify_guest"),
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                mock.patch.object(bootstrap, "scp_base", return_value=["scp"]),
                mock.patch.object(bootstrap, "remote_destination", return_value="celik@peer:fixture"),
                mock.patch.object(bootstrap, "run") as run,
            ):
                result = peer.prepare(args)
            commands = [" ".join(call.args[0]) for call in run.call_args_list]
            self.assertTrue(any("pacman -Syu --noconfirm bind bind-tools" in command for command in commands))
            self.assertTrue(any("named-checkconf /etc/named.conf" in command for command in commands))
            self.assertTrue(any("systemctl enable --now named.service" in command for command in commands))
            self.assertFalse(any("dig " in command for command in commands))
            self.assertEqual(result["management_installed_on_secondary"], False)

    def test_observe_requires_source_bound_catalog_and_authoritative_member(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            args = self.args(temporary, "observe", True)
            soa = member_answer(peer.ZONE, "SOA", "ns.test. hostmaster.test. 1 60 30 3600 60")
            address = member_answer(peer.QUERY, "A", "192.0.2.10")
            replies = ["", pdns_catalog_axfr(), pdns_catalog_axfr()] + [soa] * 4 + [address] * 4
            with (
                mock.patch.object(peer, "selected", return_value=(
                    "192.0.2.10", "192.0.2.11", {}, args.identity_file)),
                mock.patch.object(peer, "verify_guest"),
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                mock.patch.object(peer, "remote_read", side_effect=replies) as read,
            ):
                result = peer.observe(args)
            self.assertEqual(result["catalog_serial"], 1)
            self.assertEqual(result["catalog_members"], [peer.ZONE])
            self.assertTrue(result["authoritative_udp_tcp"])
            self.assertEqual(read.call_count, 11)
            self.assertIn("@192.0.2.10", read.call_args_list[1].args[1])
            self.assertIn("@127.0.0.1", read.call_args_list[2].args[1])

            with (
                mock.patch.object(peer, "selected", return_value=(
                    "192.0.2.10", "192.0.2.11", {}, args.identity_file)),
                mock.patch.object(peer, "verify_guest"),
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                mock.patch.object(peer, "remote_read", side_effect=[
                    "", pdns_catalog_axfr(), pdns_catalog_axfr(2)]),
            ):
                with self.assertRaisesRegex(ValueError, "catalog differs"):
                    peer.observe(args)

            with (
                mock.patch.object(peer, "selected", return_value=(
                    "192.0.2.10", "192.0.2.11", {}, args.identity_file)),
                mock.patch.object(peer, "verify_guest"),
                mock.patch.object(bootstrap, "ssh_base", return_value=["ssh"]),
                mock.patch.object(peer, "remote_read", side_effect=[
                    "", pdns_catalog_axfr(), pdns_catalog_axfr(),
                    member_answer(peer.ZONE, "SOA", "ns.test. hostmaster.test. 1 60 30 3600 60", authoritative=False),
                ]),
            ):
                with self.assertRaisesRegex(ValueError, "not authoritative"):
                    peer.observe(args)


if __name__ == "__main__":
    unittest.main()
