#!/usr/bin/env python3
"""Generate the deterministic S-1 DNS SIGKILL matrix manifest.

This module deliberately describes the matrix only.  It does not create VMs,
change networking, start an agent, or deliver a signal.
"""

from __future__ import annotations

import argparse
import json
import os
import tempfile
from itertools import product
from pathlib import Path
from typing import Any, Iterable


SCHEMA = "celikpanel/dns-kill-matrix/v1"

DRIVERS = (
    "bind",
    "pdns-switch",
    "pdns-adopt",
    "pdns-secondary-reconfigure",
    "signed-update-finalize",
)

PHASES = (
    "intent",
    "target-staged",
    "source-stopped",
    "target-started",
    "target-verified",
    "committed",
    "rolling-back",
    "rolled-back",
)

WRITE_EDGES = ("before-write", "after-write")
ROLES = ("standalone", "paired-primary", "paired-secondary")
PEER_STATES = ("reachable", "unreachable")

# The pre-intent window is one kill point, not a journal write with two edges.
# Therefore the exact boundary cardinality is 1 + (8 * 2) = 17.
BOUNDARIES = (
    ({"name": "pre-intent", "phase": "pre-intent", "edge": "window"},)
    + tuple(
        {
            "name": f"{phase}:{edge}",
            "phase": phase,
            "edge": edge,
        }
        for phase, edge in product(PHASES, WRITE_EDGES)
    )
)


def source_ref(path: str, lines: str, symbol: str, claim: str) -> dict[str, str]:
    """Return a report-ready source reference with an explicit file:line."""

    return {
        "file": path,
        "lines": lines,
        "file_line": f"{path}:{lines}",
        "symbol": symbol,
        "claim": claim,
    }


PHASE_ENUM_EVIDENCE = source_ref(
    "cmd/agent/dns_engine_transaction.go",
    "30-37",
    "dnsSwitchPhase*",
    "The journal schema defines the eight matrix phases.",
)

WRITE_HOOK_EVIDENCE = source_ref(
    "cmd/agent/dns_engine_transaction.go",
    "549-626",
    "writeDNSEngineSwitchJournalWithOps",
    "The shared writer exposes before-write and after-write fault boundaries.",
)

DRIVER_RULES: dict[str, dict[str, Any]] = {
    "bind": {
        "phases": frozenset(PHASES),
        "roles": frozenset(ROLES),
        "kill_hosts": ("debian-13", "arch"),
        "phase_evidence": (
            source_ref(
                "cmd/agent/dns_engine_host.go",
                "130-138",
                "runBINDRollbackWithJournal",
                "BIND rollback writes rolling-back and rolled-back.",
            ),
            source_ref(
                "cmd/agent/dns_engine_host.go",
                "1386-1591",
                "switchToBINDOnCertifiedProfile",
                "BIND exposes pre-intent and writes all six forward phases.",
            ),
        ),
        "role_evidence": (
            source_ref(
                "internal/mutationpayload/dns_engine.go",
                "198-219",
                "CanonicalDNSEngineSwitchManifestWithPairIdentity",
                "Switch manifests accept standalone and both paired roles.",
            ),
        ),
        "placement_evidence": (
            source_ref(
                "cmd/agent/dns_engine_host.go",
                "179-204",
                "bindLayoutForProfile",
                "BIND switching has APT and pacman layouts.",
            ),
        ),
    },
    "pdns-switch": {
        "phases": frozenset(PHASES),
        "roles": frozenset(ROLES),
        "kill_hosts": ("debian-13",),
        "phase_evidence": (
            source_ref(
                "cmd/agent/dns_engine_pdns_switch.go",
                "112-121",
                "finishDNSSwitchRollbackJournal",
                "PowerDNS rollback completion writes rolled-back.",
            ),
            source_ref(
                "cmd/agent/dns_engine_pdns_switch.go",
                "1575-1846",
                "switchToPDNSOnCertifiedProfile",
                "PowerDNS exposes pre-intent and writes all six forward phases plus rolling-back.",
            ),
        ),
        "role_evidence": (
            source_ref(
                "internal/mutationpayload/dns_engine.go",
                "198-219",
                "CanonicalDNSEngineSwitchManifestWithPairIdentity",
                "Switch manifests accept standalone and both paired roles.",
            ),
        ),
        "placement_evidence": (
            source_ref(
                "cmd/agent/dns_engine_pdns_unit.go",
                "63-70",
                "certifyAPTPDNSCapabilities",
                "Certified PowerDNS target mutation requires Debian-family APT and systemd.",
            ),
            source_ref(
                "cmd/agent/dns_engine_pdns_switch.go",
                "1406-1411",
                "switchToPDNS",
                "The switch runs through the certified PowerDNS target mutation guard.",
            ),
        ),
    },
    "pdns-adopt": {
        "phases": frozenset(
            (
                "intent",
                "target-verified",
                "committed",
                "rolling-back",
                "rolled-back",
            )
        ),
        # Adoption may use paired topology, but its production identity is
        # deliberately non-directional: pair_role is forbidden. Neither of
        # the matrix's directional paired roles can therefore exist.
        "roles": frozenset(("standalone",)),
        "kill_hosts": ("debian-13",),
        "phase_evidence": (
            source_ref(
                "cmd/agent/dns_engine_pdns_adopt.go",
                "473-499",
                "transitionPDNSAdoptionJournalToRollback",
                "Adoption can write rolling-back from its intent journal.",
            ),
            source_ref(
                "cmd/agent/dns_engine_pdns_adopt.go",
                "685-868",
                "adoptPDNSOnCertifiedProfile",
                "Adoption exposes pre-intent and writes only intent, rolled-back, target-verified, and committed on this path.",
            ),
        ),
        "role_evidence": (
            source_ref(
                "internal/mutationpayload/dns_engine.go",
                "203-219",
                "CanonicalDNSEngineSwitchManifestWithPairIdentity",
                "Adoption accepts paired topology but rejects pair_role and every local directional identity.",
            ),
            source_ref(
                "cmd/agent/dns_engine_pdns_adopt.go",
                "97-124",
                "verifyPDNSAdoptionDatabase",
                "Paired adoption accepts peer-owned SECONDARY/SLAVE zones and an exact autoprimary peer.",
            ),
            source_ref(
                "cmd/agent/dns_engine_host.go",
                "283-286",
                "validateDNSEngineState",
                "An adoption state receipt rejects every non-empty pair role.",
            ),
        ),
        "placement_evidence": (
            source_ref(
                "cmd/agent/dns_engine_pdns_unit.go",
                "63-70",
                "certifyAPTPDNSCapabilities",
                "Certified PowerDNS target mutation requires Debian-family APT and systemd.",
            ),
            source_ref(
                "cmd/agent/dns_engine_pdns_adopt.go",
                "676-681",
                "adoptPDNS",
                "Adoption runs through the certified PowerDNS target mutation guard.",
            ),
        ),
    },
    "pdns-secondary-reconfigure": {
        "phases": frozenset(PHASES),
        "roles": frozenset(("paired-secondary",)),
        "kill_hosts": ("debian-13",),
        "phase_evidence": (
            source_ref(
                "cmd/agent/dns_engine_pdns_switch.go",
                "112-121",
                "finishDNSSwitchRollbackJournal",
                "The reconfiguration uses the PowerDNS switch rollback writer.",
            ),
            source_ref(
                "cmd/agent/dns_engine_pdns_switch.go",
                "1575-1846",
                "switchToPDNSOnCertifiedProfile",
                "The reconfiguration uses the PowerDNS switch pre-intent and phase writers.",
            ),
        ),
        "role_evidence": (
            source_ref(
                "cmd/agent/dns_engine_pdns_switch.go",
                "27-36",
                "isPDNSPairSecondaryReconfigureManifest",
                "Reconfiguration requires paired topology and the secondary role exactly.",
            ),
        ),
        "placement_evidence": (
            source_ref(
                "cmd/agent/dns_engine_pdns_unit.go",
                "63-70",
                "certifyAPTPDNSCapabilities",
                "Certified PowerDNS target mutation requires Debian-family APT and systemd.",
            ),
        ),
    },
    "signed-update-finalize": {
        # Committed finalization reads and removes a journal; it never writes a
        # committed phase.  The only phase write in the signed-update recovery
        # walker is rolling-back -> rolled-back.
        "phases": frozenset(("rolled-back",)),
        "roles": frozenset(("standalone", "paired-primary")),
        "kill_hosts": ("debian-13",),
        "phase_evidence": (
            source_ref(
                "cmd/agent/dns_engine_bind_update.go",
                "486-654",
                "recoverCommittedDNSEngineSwitchJournalForSignedUpdate",
                "Committed signed-update recovery reads, proves, finalizes, and removes the journal without writing committed.",
            ),
            source_ref(
                "cmd/agent/dns_engine_bind_update.go",
                "657-762",
                "recoverDNSEngineSwitchJournalForSignedUpdate",
                "Signed-update recovery accepts committed or rollback journals and only writes the rolled-back recovery phase.",
            ),
        ),
        "role_evidence": (
            source_ref(
                "cmd/agent/dns_engine_rollback_evidence.go",
                "73-90",
                "initialBINDInstallRollbackEvidenceScope",
                "The recoverable initial BIND rollback scope is standalone or paired-primary, never paired-secondary.",
            ),
        ),
        "placement_evidence": (
            source_ref(
                "cmd/agent/dns_engine_bind_update.go",
                "349-350",
                "verifySignedUpdateBINDRestored",
                "Signed-update BIND rollback proof requires an APT host.",
            ),
        ),
    },
}


def boundary_is_supported(driver: str, boundary: dict[str, str]) -> bool:
    if boundary["phase"] == "pre-intent":
        return driver != "signed-update-finalize"
    return boundary["phase"] in DRIVER_RULES[driver]["phases"]


def deterministic_bind_host(
    boundary_index: int, role_index: int, peer_index: int
) -> str:
    """Spread BIND cells across both hosts without adding an OS dimension."""

    return ("debian-13", "arch")[(boundary_index + role_index + peer_index) % 2]


def host_placement(
    driver: str,
    boundary: dict[str, str],
    role: str,
    boundary_index: int,
    role_index: int,
    peer_index: int,
) -> dict[str, Any]:
    eligible = DRIVER_RULES[driver]["kill_hosts"]
    critical_bind_source = driver == "bind" and boundary["phase"] in {
        "source-stopped",
        "target-started",
    }
    if critical_bind_source:
        kill_host = "debian-13"
        selection = "genuine-managed-pdns-source-required"
    elif len(eligible) == 1:
        kill_host = eligible[0]
        selection = "required-by-production-capability"
    else:
        kill_host = deterministic_bind_host(boundary_index, role_index, peer_index)
        selection = "deterministic-parity-across-both-hosts"
    other_host = "arch" if kill_host == "debian-13" else "debian-13"
    source_fixture_policy = "driver-specific"
    if critical_bind_source:
        source_fixture_policy = "managed-pdns-required"
    elif driver == "bind" and kill_host == "arch" and boundary["phase"] in {
        "pre-intent",
        "intent",
        "target-staged",
    }:
        source_fixture_policy = "uninitialized-permitted-noncritical"
    return {
        "kill_host": kill_host,
        "other_host": other_host,
        "dns_peer_host": other_host if role != "standalone" else None,
        "eligible_kill_hosts": list(eligible),
        "selection": selection,
        "source_fixture_policy": source_fixture_policy,
    }


def n_a_reasons(
    driver: str, boundary: dict[str, str], role: str
) -> list[dict[str, Any]]:
    rules = DRIVER_RULES[driver]
    reasons: list[dict[str, Any]] = []
    if not boundary_is_supported(driver, boundary):
        if driver == "pdns-adopt":
            detail = (
                f"PowerDNS adoption has no {boundary['phase']} journal writer; "
                "its sparse forward path and rollback writers are enumerated in the cited ranges."
            )
        elif driver == "signed-update-finalize":
            detail = (
                f"Signed-update recovery has no {boundary['name']} boundary: committed "
                "finalization does not write a phase, and its only phase write is rolled-back."
            )
        else:  # Defensive: currently every other driver supports every point.
            detail = f"{driver} has no production writer for {boundary['name']}."
        reasons.append(
            {
                "code": "phase-writer-does-not-exist",
                "detail": detail,
                "evidence": list(rules["phase_evidence"]),
            }
        )
    if role not in rules["roles"]:
        if driver == "pdns-adopt":
            detail = (
                "PowerDNS adoption may use paired topology, but its identity is "
                "non-directional and forbids pair_role; neither paired-primary nor "
                "paired-secondary is a production adoption role."
            )
        elif driver == "pdns-secondary-reconfigure":
            detail = "PowerDNS secondary reconfiguration requires paired-secondary exactly."
        elif driver == "signed-update-finalize":
            detail = (
                "The signed-update rolled-back writer is reachable only for the supported "
                "initial BIND rollback evidence scope, which excludes paired-secondary."
            )
        else:  # Defensive: currently every other driver supports every role.
            detail = f"{driver} cannot execute with role {role}."
        reasons.append(
            {
                "code": "driver-role-cannot-exist",
                "detail": detail,
                "evidence": list(rules["role_evidence"]),
            }
        )
    return reasons


def cell_id(driver: str, boundary: str, role: str, peer: str) -> str:
    return "__".join((driver, boundary.replace(":", "__"), role, f"peer-{peer}"))


def make_cell(
    driver: str,
    boundary: dict[str, str],
    role: str,
    peer: str,
    boundary_index: int,
    role_index: int,
    peer_index: int,
) -> dict[str, Any]:
    reasons = n_a_reasons(driver, boundary, role)
    rules = DRIVER_RULES[driver]
    placement = host_placement(
        driver, boundary, role, boundary_index, role_index, peer_index
    )
    paired = role != "standalone"
    result: dict[str, Any] = {
        "id": cell_id(driver, boundary["name"], role, peer),
        "driver": driver,
        "boundary": dict(boundary),
        "role": role,
        "peer_reachability": peer,
        "status": "n/a" if reasons else "runnable",
        "applicability": "verified",
        "placement": placement,
        "peer_control": {
            "semantic": "dns-peer" if paired else "unused-peer-invariance-control",
            # A paired-secondary preflight contacts the peer before it can reach
            # the hook.  Apply the matrix peer state only after exit 137 has
            # proved delivery, and before restart/recovery assertions.
            "apply": "after-proven-kill-before-restart" if paired else "control-only",
        },
        "fault_selector": {
            "point": "pre_intent"
            if boundary["phase"] == "pre-intent"
            else boundary["edge"].replace("-", "_"),
            "phase": boundary["phase"],
        },
        "assertions": [
            "kill-exit-137",
            "dns-engine-serving-after-restart",
            "panel-starts",
            "agent-stays-running",
        ],
    }
    if reasons:
        result["n_a"] = {"reasons": reasons}
    else:
        result["evidence"] = {
            "phase": list(rules["phase_evidence"]),
            "role": list(rules["role_evidence"]),
            "placement": list(rules["placement_evidence"]),
            "shared_write_hook": [WRITE_HOOK_EVIDENCE]
            if boundary["phase"] != "pre-intent"
            else [],
        }
    return result


def iter_cells() -> Iterable[dict[str, Any]]:
    for driver in DRIVERS:
        for boundary_index, boundary in enumerate(BOUNDARIES):
            for role_index, role in enumerate(ROLES):
                for peer_index, peer in enumerate(PEER_STATES):
                    yield make_cell(
                        driver,
                        boundary,
                        role,
                        peer,
                        boundary_index,
                        role_index,
                        peer_index,
                    )


def validate_manifest(manifest: dict[str, Any]) -> None:
    cells = manifest.get("cells")
    if not isinstance(cells, list):
        raise ValueError("manifest cells must be a list")
    expected_raw = len(DRIVERS) * len(BOUNDARIES) * len(ROLES) * len(PEER_STATES)
    if expected_raw != 510:
        raise ValueError(f"matrix dimensions changed: {expected_raw}, want 510")
    if len(cells) != expected_raw:
        raise ValueError(f"manifest has {len(cells)} cells, want {expected_raw}")
    ids = [cell.get("id") for cell in cells]
    if len(set(ids)) != len(ids):
        raise ValueError("manifest cell IDs are not unique")
    if any(not isinstance(value, str) or not value for value in ids):
        raise ValueError("manifest contains an empty or non-string cell ID")
    allowed_statuses = {"runnable", "n/a"}
    invalid_statuses = sorted(
        {str(cell.get("status")) for cell in cells} - allowed_statuses
    )
    if invalid_statuses:
        raise ValueError(f"invalid cell statuses: {invalid_statuses}")
    for cell in cells:
        if (
            cell.get("driver") == "bind"
            and cell.get("status") == "runnable"
            and cell.get("boundary", {}).get("phase")
            in {"source-stopped", "target-started"}
        ):
            placement = cell.get("placement", {})
            if (
                placement.get("kill_host") != "debian-13"
                or placement.get("source_fixture_policy") != "managed-pdns-required"
                or placement.get("selection")
                != "genuine-managed-pdns-source-required"
            ):
                raise ValueError(
                    f"critical BIND cell {cell['id']} lacks its genuine Debian "
                    "managed-PowerDNS source placement"
                )
        if cell["status"] == "n/a":
            reasons = cell.get("n_a", {}).get("reasons", [])
            if not reasons:
                raise ValueError(f"N/A cell {cell['id']} has no reason")
            for reason in reasons:
                evidence = reason.get("evidence", [])
                if not reason.get("detail") or not evidence:
                    raise ValueError(
                        f"N/A cell {cell['id']} has a reason without evidence"
                    )
                for ref in evidence:
                    if not ref.get("file_line") or not ref.get("claim"):
                        raise ValueError(
                            f"N/A cell {cell['id']} has incomplete file:line evidence"
                        )
    summary = manifest.get("summary", {})
    actual_runnable = sum(cell["status"] == "runnable" for cell in cells)
    actual_n_a = sum(cell["status"] == "n/a" for cell in cells)
    expected_summary = {
        "raw": expected_raw,
        "runnable": actual_runnable,
        "n_a": actual_n_a,
        "unverified_applicability": sum(
            cell.get("applicability") == "unverified" for cell in cells
        ),
    }
    if summary != expected_summary:
        raise ValueError(f"summary {summary!r} does not match {expected_summary!r}")


def build_manifest() -> dict[str, Any]:
    cells = list(iter_cells())
    manifest = {
        "schema": SCHEMA,
        "dimensions": {
            "drivers": list(DRIVERS),
            "boundaries": [dict(boundary) for boundary in BOUNDARIES],
            "roles": list(ROLES),
            "peer_reachability": list(PEER_STATES),
            "raw_formula": "5 * (1 + 8 * 2) * 3 * 2",
        },
        "shared_evidence": {
            "phase_enum": PHASE_ENUM_EVIDENCE,
            "journal_write_hook": WRITE_HOOK_EVIDENCE,
        },
        "policy": {
            "n_a": (
                "Only source-proven impossible phase/role combinations are excluded. "
                "An uncertain combination must remain runnable with applicability=unverified."
            ),
            "standalone_peer_dimension": (
                "Both peer labels remain runnable as invariance controls even though a "
                "standalone manifest carries no DNS peer."
            ),
            "unreachable_timing": (
                "For paired cells, make the peer unreachable only after exit 137 proves "
                "the boundary kill and before restarting the agent."
            ),
            "platform_dimension": (
                "Operating system is a placement constraint, not an extra matrix dimension. "
                "Certified PowerDNS and signed-update rollback cells run on Debian 13; "
                "BIND cells are otherwise spread across Debian 13 and Arch, but every "
                "source-stopped and target-started BIND cell runs on Debian with a genuine "
                "managed PowerDNS source. Early Arch BIND cells may use an explicitly proven "
                "uninitialized source and do not claim stopped-source coverage."
            ),
            "gate_denominator": (
                "The D-021 execution denominator is runnable cells. An unproven kill is "
                "recorded unverified at execution time and must never be counted passed."
            ),
        },
        "cells": cells,
        "summary": {
            "raw": len(cells),
            "runnable": sum(cell["status"] == "runnable" for cell in cells),
            "n_a": sum(cell["status"] == "n/a" for cell in cells),
            "unverified_applicability": sum(
                cell["applicability"] == "unverified" for cell in cells
            ),
        },
    }
    validate_manifest(manifest)
    return manifest


def encoded_manifest(manifest: dict[str, Any], *, compact: bool = False) -> str:
    if compact:
        return json.dumps(
            manifest, ensure_ascii=False, sort_keys=True, separators=(",", ":")
        ) + "\n"
    return json.dumps(manifest, ensure_ascii=False, sort_keys=True, indent=2) + "\n"


def write_atomic(path: Path, data: str) -> None:
    path = path.resolve()
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(
        prefix=f".{path.name}.", suffix=".tmp", dir=path.parent
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    except BaseException:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--output",
        type=Path,
        help="write the manifest atomically to this path instead of stdout",
    )
    parser.add_argument(
        "--compact", action="store_true", help="emit compact deterministic JSON"
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="validate the generated manifest and print only its counts",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    manifest = build_manifest()
    if args.check:
        summary = manifest["summary"]
        print(
            "dns-kill-matrix: "
            f"raw={summary['raw']} runnable={summary['runnable']} "
            f"n/a={summary['n_a']} unique_ids={len(manifest['cells'])}"
        )
        return 0
    data = encoded_manifest(manifest, compact=args.compact)
    if args.output is None:
        print(data, end="")
    else:
        write_atomic(args.output, data)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
