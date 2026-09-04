#!/usr/bin/env python3

from __future__ import annotations

import hashlib
import json
import unittest
from itertools import product

import generate_manifest as matrix
import render_n_a as appendix


class DNSKillMatrixManifestTest(unittest.TestCase):
    def setUp(self) -> None:
        self.manifest = matrix.build_manifest()
        self.cells = self.manifest["cells"]

    def test_raw_cross_product_is_510_and_ids_are_unique(self) -> None:
        expected_coordinates = {
            (driver, boundary["name"], role, peer)
            for driver, boundary, role, peer in product(
                matrix.DRIVERS,
                matrix.BOUNDARIES,
                matrix.ROLES,
                matrix.PEER_STATES,
            )
        }
        actual_coordinates = {
            (
                cell["driver"],
                cell["boundary"]["name"],
                cell["role"],
                cell["peer_reachability"],
            )
            for cell in self.cells
        }
        self.assertEqual(len(expected_coordinates), 510)
        self.assertEqual(actual_coordinates, expected_coordinates)
        ids = [cell["id"] for cell in self.cells]
        self.assertEqual(len(ids), 510)
        self.assertEqual(len(set(ids)), 510)

    def test_pre_intent_is_singleton_and_phase_writes_have_both_edges(self) -> None:
        self.assertEqual(matrix.BOUNDARIES[0]["name"], "pre-intent")
        self.assertEqual(matrix.BOUNDARIES[0]["edge"], "window")
        for phase in matrix.PHASES:
            edges = {
                boundary["edge"]
                for boundary in matrix.BOUNDARIES
                if boundary["phase"] == phase
            }
            self.assertEqual(edges, set(matrix.WRITE_EDGES), phase)

    def test_n_a_cells_have_reason_and_file_line_evidence(self) -> None:
        for cell in self.cells:
            if cell["status"] != "n/a":
                continue
            reasons = cell["n_a"]["reasons"]
            self.assertTrue(reasons, cell["id"])
            for reason in reasons:
                self.assertTrue(reason["detail"], cell["id"])
                self.assertTrue(reason["evidence"], cell["id"])
                for evidence in reason["evidence"]:
                    self.assertRegex(
                        evidence["file_line"], r"^[^:]+:\d+(?:-\d+)?$"
                    )
                    self.assertTrue(evidence["claim"], cell["id"])

    def test_audited_applicability_count_is_stable(self) -> None:
        # 102 BIND + 102 PDNS switch + 22 adoption + 34 secondary
        # reconfigure + 8 signed-update rolled-back recovery cells.
        self.assertEqual(
            self.manifest["summary"],
            {
                "raw": 510,
                "runnable": 268,
                "n_a": 242,
                "unverified_applicability": 0,
            },
        )

    def test_adoption_has_no_directional_paired_role(self) -> None:
        adoption = [cell for cell in self.cells if cell["driver"] == "pdns-adopt"]
        runnable = [cell for cell in adoption if cell["status"] == "runnable"]
        self.assertEqual(len(runnable), 22)
        self.assertEqual({cell["role"] for cell in runnable}, {"standalone"})

        for cell in adoption:
            if cell["role"] == "standalone":
                continue
            self.assertEqual(cell["status"], "n/a", cell["id"])
            reason_codes = {reason["code"] for reason in cell["n_a"]["reasons"]}
            self.assertIn("driver-role-cannot-exist", reason_codes, cell["id"])

    def test_sparse_path_evidence_uses_complete_current_ranges(self) -> None:
        adoption_refs = {
            ref["file_line"]
            for ref in matrix.DRIVER_RULES["pdns-adopt"]["phase_evidence"]
        }
        self.assertIn(
            "cmd/agent/dns_engine_pdns_adopt.go:685-868",
            adoption_refs,
        )
        signed_refs = {
            ref["file_line"]
            for ref in matrix.DRIVER_RULES["signed-update-finalize"]["phase_evidence"]
        }
        self.assertIn(
            "cmd/agent/dns_engine_bind_update.go:486-654",
            signed_refs,
        )

    def test_sparse_phase_rules_do_not_hide_rollback_writers(self) -> None:
        adopt = [cell for cell in self.cells if cell["driver"] == "pdns-adopt"]
        adopt_runnable_phases = {
            cell["boundary"]["phase"]
            for cell in adopt
            if cell["status"] == "runnable"
        }
        self.assertEqual(
            adopt_runnable_phases,
            {
                "pre-intent",
                "intent",
                "target-verified",
                "committed",
                "rolling-back",
                "rolled-back",
            },
        )
        signed = [
            cell
            for cell in self.cells
            if cell["driver"] == "signed-update-finalize"
            and cell["status"] == "runnable"
        ]
        self.assertEqual(
            {cell["boundary"]["name"] for cell in signed},
            {"rolled-back:before-write", "rolled-back:after-write"},
        )

    def test_critical_bind_source_boundaries_require_debian(self) -> None:
        critical = [
            cell
            for cell in self.cells
            if cell["driver"] == "bind"
            and cell["status"] == "runnable"
            and cell["boundary"]["phase"] in {"source-stopped", "target-started"}
        ]
        self.assertEqual(len(critical), 24)
        for cell in critical:
            self.assertEqual(cell["placement"]["kill_host"], "debian-13", cell["id"])
            self.assertEqual(
                cell["placement"]["source_fixture_policy"],
                "managed-pdns-required",
                cell["id"],
            )

    def test_early_arch_bind_source_is_explicitly_noncritical(self) -> None:
        early_arch = [
            cell
            for cell in self.cells
            if cell["driver"] == "bind"
            and cell["status"] == "runnable"
            and cell["placement"]["kill_host"] == "arch"
            and cell["boundary"]["phase"] in {"pre-intent", "intent", "target-staged"}
        ]
        self.assertTrue(early_arch)
        for cell in early_arch:
            self.assertEqual(
                cell["placement"]["source_fixture_policy"],
                "uninitialized-permitted-noncritical",
                cell["id"],
            )

    def test_generation_is_byte_deterministic(self) -> None:
        first = matrix.encoded_manifest(matrix.build_manifest())
        second = matrix.encoded_manifest(matrix.build_manifest())
        self.assertEqual(first, second)
        self.assertEqual(json.loads(first), self.manifest)

    def test_n_a_appendix_is_complete_ordered_and_deterministic(self) -> None:
        manifest_bytes = matrix.encoded_manifest(self.manifest).encode("utf-8")
        digest = hashlib.sha256(manifest_bytes).hexdigest()
        first = appendix.encoded_n_a_appendix(self.manifest, digest)
        second = appendix.encoded_n_a_appendix(matrix.build_manifest(), digest)
        self.assertEqual(first, second)
        expected_ids = [
            cell["id"] for cell in self.cells if cell["status"] == "n/a"
        ]
        headings = [
            line.removeprefix("## ")
            for line in first.splitlines()
            if line.startswith("## ")
        ]
        self.assertEqual(headings, expected_ids)
        self.assertEqual(len(headings), 242)
        self.assertIn(f"Manifest SHA-256: `{digest}`", first)


if __name__ == "__main__":
    unittest.main()
