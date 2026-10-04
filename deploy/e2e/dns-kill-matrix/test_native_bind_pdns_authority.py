#!/usr/bin/env python3
"""Focused checks for the isolated Frankfurt/Boston authority witness."""

from __future__ import annotations

import argparse
from pathlib import Path
import subprocess
import unittest
from unittest import mock

import guest_bootstrap
import native_bind_pdns_authority as pair
import native_bind_pdns_authority_probe as probe


def reply(owner: str, kind: str, values: tuple[str, ...], *, aa: bool = True) -> str:
    flags = "qr aa" if aa else "qr"
    return (";; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 1\n"
            f";; flags: {flags} rd; QUERY: 1, ANSWER: 1\n"
            f"{owner} 300 IN {kind} {' '.join(values)}\n")


class AuthorityWitnessTest(unittest.TestCase):
    def test_opt_in_adds_exact_zone_without_changing_matrix_zone(self) -> None:
        baseline = guest_bootstrap.pdns_switch_scenario(role="paired-primary")
        scenario = guest_bootstrap.pdns_switch_scenario(
            role="paired-primary", authority_acceptance=True)
        self.assertEqual(scenario["zones"][0], baseline["zones"][0])
        self.assertEqual([zone["domain"] for zone in scenario["zones"]],
                         ["s1-kill.test", "celikhost.com"])
        records = {(r["name"], r["type"], r["content"])
                   for r in scenario["zones"][1]["records"]}
        self.assertIn(("frankfurt.celikhost.com", "A", "192.0.2.10"), records)
        self.assertIn(("boston.celikhost.com", "A", "192.0.2.11"), records)
        self.assertEqual({content for name, kind, content in records
                          if name == "celikhost.com" and kind == "NS"},
                         {"ns1.celikhost.com", "ns2.celikhost.com"})
        with self.assertRaises(guest_bootstrap.BootstrapError):
            guest_bootstrap.bind_scenario(
                "uninitialized", authority_acceptance=True)

    def test_direct_answer_requires_authority_and_exact_records(self) -> None:
        owner = "frankfurt.celikhost.com."
        good = reply(owner, "A", ("192.0.2.10",))
        with mock.patch.object(probe.subprocess, "run",
                               return_value=subprocess.CompletedProcess([], 0, good)) as run:
            self.assertEqual(probe.answer("192.0.2.11", owner, "A", True),
                             {("192.0.2.10",)})
            self.assertIn("+tcp", run.call_args.args[0])
            self.assertIn("@192.0.2.11", run.call_args.args[0])
        with mock.patch.object(probe.subprocess, "run",
                               return_value=subprocess.CompletedProcess(
                                   [], 0, reply(owner, "A", ("192.0.2.10",), aa=False))):
            with self.assertRaisesRegex(ValueError, "authoritative"):
                probe.answer("192.0.2.11", owner, "A", False)
        with mock.patch.object(probe.subprocess, "run",
                               return_value=subprocess.CompletedProcess(
                                   [], 0, reply(owner, "A", ("192.0.2.12",)))):
            self.assertNotEqual(probe.answer("192.0.2.11", owner, "A", False),
                                probe.EXPECTED[(owner, "A")])
        apex = "celikhost.com."
        ns_reply = (reply(apex, "NS", ("ns1.celikhost.com.",)) +
                    "celikhost.com. 300 IN NS ns2.celikhost.com.\n")
        with mock.patch.object(probe.subprocess, "run",
                               return_value=subprocess.CompletedProcess([], 0, ns_reply)):
            self.assertEqual(probe.answer("192.0.2.10", apex, "NS", False),
                             probe.EXPECTED[(apex, "NS")])
        with mock.patch.object(probe.subprocess, "run",
                               return_value=subprocess.CompletedProcess(
                                   [], 0, reply(apex, "NS", ("ns1.celikhost.com.",)))):
            self.assertNotEqual(probe.answer("192.0.2.10", apex, "NS", False),
                                probe.EXPECTED[(apex, "NS")])

    def test_post_reboot_requires_both_new_boot_ids(self) -> None:
        args = argparse.Namespace(
            work_root=Path("."), cell_id=pair.CELL, manifest=Path("manifest.json"),
            identity_file=Path("identity"), previous_primary_boot_id="old",
            previous_secondary_boot_id="same", execute=True)
        plan = {"nodes": {"debian13": {}, "arch": {}}}
        cell = {"id": pair.CELL}
        marker_primary = ("schema=celikpanel/dns-kill-fixture-plan/v1\n"
                          f"cell_id={pair.CELL}\nnode=debian13")
        marker_secondary = marker_primary.replace("node=debian13", "node=arch")
        with (mock.patch.object(pair.fixture, "load_cell_plan", return_value=plan),
              mock.patch.object(pair.bootstrap, "load_manifest_cell", return_value=cell),
              mock.patch.object(pair.bootstrap, "validate_pdns_switch_cell"),
              mock.patch.object(pair.native_pdns_peer, "pair_addresses",
                                return_value=("192.0.2.10", "192.0.2.11", plan["nodes"]["arch"])),
              mock.patch.object(pair.bootstrap, "identity_file", return_value=Path("identity")),
              mock.patch.object(pair, "remote", side_effect=[
                  marker_primary, marker_secondary, "new", "same",
              ])):
            with self.assertRaisesRegex(ValueError, "new boot IDs"):
                pair.observe(args)


if __name__ == "__main__":
    unittest.main()
