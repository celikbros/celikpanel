"""Pure verdict functions over ``guest_probe.py`` observations.

Pass definitions (also in README "Pass definitions"):

present   Every observer's query to every server, over UDP and TCP, returns
          NOERROR with the AA bit, exactly one SOA, one serial shared by both
          servers and both transports, the expected NS set and exactly the
          expected RRsets. ``min_serial`` requires the serial to advance.
absent    No server answers authoritatively for the zone over UDP or TCP. This
          is *never* sufficient alone: ``native_absent`` must also hold on
          both servers and the primary's catalog must no longer list the
          member.
native    BIND: ``rndc zonestatus`` is the exact "no matching zone ... in any
          view" text and no zone file names the zone. PowerDNS: no row in the
          SQLite ``domains`` table and ``pdns_control list-zones`` does not
          list it.
"""

from __future__ import annotations

from typing import Any, Iterable


def _norm(name: str) -> str:
    return name.rstrip(".").lower() + "."


def _answers(observations: Iterable[dict[str, Any]]) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    for observation in observations:
        observer = observation.get("observer")
        for answer in observation.get("answers") or []:
            items.append({**answer, "observer": observer})
    return items


def _rrset(answer: dict[str, Any], name: str, qtype: str) -> set[str]:
    wanted = _norm(name)
    return {
        record["data"].lower() if qtype in {"NS", "CNAME", "PTR"} else record["data"]
        for record in answer.get("answer") or []
        if record.get("type") == qtype and _norm(record.get("name", "")) == wanted
    }


def soa_serial(answer: dict[str, Any], zone: str) -> int | None:
    records = [r for r in answer.get("answer") or [] if r.get("type") == "SOA" and _norm(r.get("name", "")) == _norm(zone)]
    if len(records) != 1:
        return None
    try:
        return int(records[0]["data"].split()[2])
    except (IndexError, ValueError):
        return None


def evaluate_present(
    observations: list[dict[str, Any]],
    *,
    servers: list[str],
    zone: str,
    nameservers: list[str],
    expected: dict[tuple[str, str], set[str]],
    min_serial: int | None = None,
) -> dict[str, Any]:
    reasons: list[str] = []
    serials: dict[str, int | None] = {}
    answers = _answers(observations)
    seen_servers = {answer.get("server") for answer in answers}
    for server in servers:
        if server not in seen_servers:
            reasons.append(f"no observation of server {server}")
    expected_norm = {(_norm(n), t): set(values) for (n, t), values in expected.items()}
    for answer in answers:
        label = f"{answer.get('observer')}->{answer.get('server')}/{answer.get('transport')} {answer.get('name')} {answer.get('qtype')}"
        if "error" in answer:
            reasons.append(f"{label}: {answer['error']}")
            continue
        qtype = answer.get("qtype")
        name = answer.get("name", "")
        key = (_norm(name), qtype)
        if (
            key in expected_norm
            and not expected_norm[key]
            and answer.get("rcode") in {"NXDOMAIN", "NOERROR"}
            and answer.get("authoritative")
            and not answer.get("truncated")
        ):
            # An authoritative "no such name/data" is the expected answer here.
            if _rrset(answer, name, qtype):
                reasons.append(f"{label}: {sorted(_rrset(answer, name, qtype))} != expected []")
            continue
        if answer.get("rcode") != "NOERROR" or not answer.get("authoritative") or answer.get("truncated"):
            reasons.append(f"{label}: rcode={answer.get('rcode')} aa={answer.get('authoritative')}")
            continue
        if qtype == "SOA" and _norm(name) == _norm(zone):
            serial = soa_serial(answer, zone)
            serials[label] = serial
            if serial is None:
                reasons.append(f"{label}: not exactly one SOA")
        elif qtype == "NS" and _norm(name) == _norm(zone):
            got = _rrset(answer, zone, "NS")
            want = {_norm(ns) for ns in nameservers}
            if got != want:
                reasons.append(f"{label}: NS {sorted(got)} != {sorted(want)}")
        if key in expected_norm:
            got = _rrset(answer, name, qtype)
            if got != expected_norm[key]:
                reasons.append(f"{label}: {sorted(got)} != expected {sorted(expected_norm[key])}")
    values = {value for value in serials.values() if value is not None}
    if not serials:
        reasons.append("no SOA answer was observed")
    elif len(values) != 1:
        reasons.append(f"SOA serials differ: {sorted(values)}")
    serial = next(iter(values)) if len(values) == 1 else None
    if serial is not None and min_serial is not None and serial <= min_serial:
        reasons.append(f"SOA serial {serial} did not advance past {min_serial}")
    return {"passed": not reasons, "serial": serial, "reasons": reasons[:50]}


def evaluate_absent_dns(observations: list[dict[str, Any]], *, servers: list[str], zone: str) -> dict[str, Any]:
    reasons: list[str] = []
    rcodes: dict[str, str] = {}
    answers = [a for a in _answers(observations) if a.get("qtype") == "SOA" and _norm(a.get("name", "")) == _norm(zone)]
    for server in servers:
        if not any(a.get("server") == server for a in answers):
            reasons.append(f"no SOA observation of server {server}")
    for answer in answers:
        label = f"{answer.get('observer')}->{answer.get('server')}/{answer.get('transport')}"
        if "error" in answer:
            reasons.append(f"{label}: {answer['error']}")
            continue
        rcodes[label] = f"{answer.get('rcode')} aa={answer.get('authoritative')}"
        if answer.get("authoritative") and soa_serial(answer, zone) is not None:
            reasons.append(f"{label}: still authoritative for {zone}")
    return {"passed": not reasons, "rcodes": rcodes, "reasons": reasons,
            "note": "a non-authoritative DNS answer alone never proves removal"}


def evaluate_native(observation: dict[str, Any], *, expect_present: bool) -> dict[str, Any]:
    state = observation.get("native_state")
    if "error" in observation or state is None:
        return {"passed": False, "state": state, "reasons": [str(observation.get("error", "no native state"))]}
    if expect_present:
        passed = state == "present"
    else:
        passed = state == "absent"
    reasons = [] if passed else [f"native state is {state!r}, want {'present' if expect_present else 'absent'}"]
    if not expect_present and observation.get("engine") == "bind" and observation.get("zone_files"):
        reasons.append(f"zone files remain: {observation['zone_files'][:5]}")
        passed = False
    return {"passed": passed, "state": state, "reasons": reasons}


def evaluate_catalog(observation: dict[str, Any], *, zone: str, expect_member: bool) -> dict[str, Any]:
    if not observation.get("transferred"):
        return {"passed": False, "reasons": [f"catalog AXFR not transferred: {observation.get('rcode') or observation.get('error')}"]}
    members = {_norm(member) for member in observation.get("members") or []}
    present = _norm(zone) in members
    passed = present == expect_member
    return {
        "passed": passed,
        "serial": observation.get("serial"),
        "member_present": present,
        "reasons": [] if passed else [f"catalog member {zone} present={present}, want {expect_member}"],
    }


def catalog_name(primary_ip: str) -> str:
    """binddns.CatalogDomain: catalog-<hex(IPv4)>.celikpanel.invalid."""

    octets = [int(part) for part in primary_ip.split(".")]
    if len(octets) != 4 or any(not 0 <= value <= 255 for value in octets):
        raise ValueError("catalog primary must be an IPv4 address")
    return "catalog-" + "".join(f"{value:02x}" for value in octets) + ".celikpanel.invalid"
