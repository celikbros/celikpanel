"""The DNS engine card contract and the pair readiness pass rule (D1).

``read_engine_snapshot`` mirrors what the web client accepts from
``GET /api/v1/dns/engine`` (``web/src/lib/dnsEngineContract.ts``, the pair
rules around lines 370-384) plus the Panel's own refusal of a runtime that
claims both proofs (``cmd/panel/dns_engine.go`` ~315). A snapshot the web
client would refuse is never a pass.

``pair_readiness`` is the owner's pass rule D1, applied at ``pair-ready`` and
at ``management-return``. Corrected after pair2 (2026-09-30) to the product
contract (Decision D): ``secondary_ready`` is serialized exactly on an active
paired secondary and never on a primary (``cmd/panel/dns_engine.go`` ~108
``secondary_ready,omitempty`` and ~637-649; ``cmd/panel/dns_engine_test.go``
~2003 "the primary payload has no such key"), and the web client refuses a
primary snapshot that carries it (``dnsEngineContract.ts`` ~383):

* primary:   ``pair_role == "primary"``,   ``pair_ready is True``, ``secondary_ready`` ABSENT
* secondary: ``pair_role == "secondary"``, ``secondary_ready is True``, ``pair_ready is False``

plus the common facts (``active_engine`` is the role's engine, ``topology`` is
``paired``, ``state`` is ``ready``). ``True``/``False`` are compared exactly,
never by truthiness. A primary payload carrying ``secondary_ready`` at all
(any value) is a contract violation and fails.

A build that predates the field (the pair1 build ``aa6b9380``) serializes no
``secondary_ready`` on an active paired secondary. That is not a plain
failure: the verdict carries ``blocked`` = ``BLOCKED_NOT_REPORTED`` ("this
build does not report secondary readiness") and the step records
``blocked-product``. Because the product serializes the field on every active
paired secondary, its absence there is final for that build and the wait stops
at once.
"""

from __future__ import annotations

from typing import Any

PAIR_ROLE = "pair_role"
PAIR_READY = "pair_ready"
SECONDARY_READY = "secondary_ready"
ABSENT = "<absent>"
ENGINES = ("bind", "pdns")
ROLES = ("primary", "secondary")
ENGINE_STATES = frozenset({"unconfigured", "ready", "unmanaged", "conflict", "switching", "degraded"})
# dnsEngineContract.ts: an operation in one of these states belongs to state "switching".
IN_FLIGHT_OPERATION = frozenset({"running", "rolling_back", "recovery_required"})
# Still moving by the product's own definition; a stable payload in these states is not a settled failure.
ENGINE_PROGRESS = frozenset({"running", "rolling_back"})
OBSERVED_KEYS = ("active_engine", "state", "topology", PAIR_ROLE, PAIR_READY, SECONDARY_READY, "revision",
                 "engine_epoch")
RULE = {
    "primary": "pair_role == primary, pair_ready === true, secondary_ready absent",
    "secondary": "pair_role == secondary, secondary_ready === true, pair_ready === false",
}
# Exact values D1 requires; ABSENT means the key must not be in the payload at all.
EXPECTED = {
    "primary": {PAIR_READY: True, SECONDARY_READY: ABSENT},
    "secondary": {SECONDARY_READY: True, PAIR_READY: False},
}
BLOCKED_NOT_REPORTED = "secondary-readiness-not-reported"
NOT_REPORTED = (
    "secondary_ready is absent from GET /api/v1/dns/engine on an active paired secondary: this build does not "
    "report secondary readiness (the product serializes it on every active paired secondary, "
    "cmd/panel/dns_engine.go secondary_ready omitempty; this build predates it)"
)
PRIMARY_CARRIES_FIELD = (
    "secondary_ready is present on a primary ({value!r}): the product never serializes it there "
    "(cmd/panel/dns_engine_test.go: the primary payload has no such key) and the web client refuses such a "
    "snapshot (web/src/lib/dnsEngineContract.ts); contract violation"
)


class ContractError(ValueError):
    pass


def _is_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def read_engine_snapshot(body: Any) -> dict[str, Any]:
    """Accept or refuse a DNS engine card snapshot as the web client does; return the pair facts."""

    if not isinstance(body, dict):
        raise ContractError("snapshot is not a JSON object")
    for key in ("revision", "engine_epoch", "zone_count", "pending_zone_count"):
        if not _is_int(body.get(key)):
            raise ContractError(f"{key} is not an integer")
    active = body.get("active_engine")
    if active is not None and active not in ENGINES:
        raise ContractError(f"active_engine {active!r} is not bind, pdns or null")
    if body.get("state") not in ENGINE_STATES:
        raise ContractError(f"state {body.get('state')!r} is not a DNS engine state")
    if not isinstance(body.get("topology"), str) or not isinstance(body.get("engines"), list):
        raise ContractError("topology or engines missing")
    for key in (PAIR_READY, SECONDARY_READY):
        if key in body and not isinstance(body[key], bool):
            raise ContractError(f"{key} is not a boolean")
    if PAIR_ROLE in body and body[PAIR_ROLE] not in ROLES:
        raise ContractError(f"pair_role {body[PAIR_ROLE]!r} is not primary or secondary")
    if (active is None) != (body["engine_epoch"] == 0):
        raise ContractError("active_engine and engine_epoch disagree")
    if body["state"] == "ready" and active is None:
        raise ContractError("state ready without an active engine")
    operation = body.get("operation") if isinstance(body.get("operation"), dict) else None
    in_flight = bool(operation) and operation.get("status") in IN_FLIGHT_OPERATION
    if body["state"] == "switching" and not in_flight:
        raise ContractError("state switching without an in-flight operation")
    if body["state"] != "switching" and in_flight:
        raise ContractError("an in-flight operation outside state switching")
    staged_pair = active is None and body["topology"] == "paired"
    active_pair = active is not None and body["topology"] == "paired"
    if staged_pair and (body.get(PAIR_ROLE) not in ROLES or PAIR_READY in body):
        raise ContractError("a staged pair needs a role and no pair_ready")
    if active_pair and not isinstance(body.get(PAIR_READY), bool):
        raise ContractError("an active pair must report pair_ready")
    if not staged_pair and not active_pair and (PAIR_ROLE in body or PAIR_READY in body):
        raise ContractError("pair fields outside a paired topology")
    # dnsEngineContract.ts:377-378 - publication readiness belongs to the primary only.
    if body.get(PAIR_READY) is True and body.get(PAIR_ROLE) != "primary":
        raise ContractError("pair_ready is true on a non-primary (web client refuses it)")
    if active_pair and body.get(PAIR_ROLE) == "secondary" and body.get(PAIR_READY) is not False:
        raise ContractError("a paired secondary must report pair_ready false (web client refuses it)")
    # cmd/panel/dns_engine.go ~315 - a runtime claiming both proofs is refused.
    if body.get(PAIR_READY) is True and body.get(SECONDARY_READY) is True:
        raise ContractError("pair_ready and secondary_ready are both true (Panel refuses such a runtime)")
    # dnsEngineContract.ts:383-384 - secondary_ready belongs to an active paired secondary only.
    if SECONDARY_READY in body and (not active_pair or body.get(PAIR_ROLE) != "secondary"
                                    or body.get(PAIR_READY) is True):
        raise ContractError("secondary_ready outside an active paired secondary (web client refuses it)")
    return {key: body.get(key, ABSENT) for key in OBSERVED_KEYS}


def observed(body: Any) -> dict[str, Any]:
    if not isinstance(body, dict):
        return {"snapshot": repr(body)[:200]}
    return {key: body.get(key, ABSENT) for key in OBSERVED_KEYS}


def pair_readiness(body: Any, *, role: str, engine: str) -> dict[str, Any]:
    """D1 verdict for one Panel's snapshot; ``passed`` only for the exact combination.

    ``blocked`` is set (to ``BLOCKED_NOT_REPORTED``) only when the single
    thing missing is ``secondary_ready`` on an otherwise ready active paired
    secondary: a build that predates the field, reported as ``blocked-product``.
    """

    if role not in ROLES:
        raise ValueError(f"unknown pair role {role!r}")
    reasons: list[str] = []
    absent: list[str] = []
    verdict: dict[str, Any] = {"passed": False, "role": role, "engine": engine, "rule": RULE[role],
                               "observed": observed(body), "reasons": reasons, "absent_fields": absent,
                               "blocked": None}
    if not isinstance(body, dict) or "poll_error" in body:
        reasons.append(f"no DNS engine snapshot: {body.get('poll_error') if isinstance(body, dict) else body!r}")
        return verdict
    try:
        read_engine_snapshot(body)
    except ContractError as exc:
        reasons.append(f"the web client would refuse this snapshot: {exc}")
    for key, want in (("active_engine", engine), ("topology", "paired"), ("state", "ready"), (PAIR_ROLE, role)):
        if body.get(key, ABSENT) != want:
            reasons.append(f"{key} is {body.get(key, ABSENT)!r}, expected {want!r}")
    not_reported = False
    for key, want in EXPECTED[role].items():
        if want is ABSENT:
            if key in body:
                reasons.append(PRIMARY_CARRIES_FIELD.format(value=body[key]))
        elif key not in body:
            absent.append(key)
            if key == SECONDARY_READY:
                not_reported = True
                reasons.append(NOT_REPORTED)
            else:
                reasons.append(f"{key} is absent; D1 requires {key} === {str(want).lower()}")
        elif body[key] is not want:
            reasons.append(f"{key} is {body[key]!r}, D1 requires {key} === {str(want).lower()}")
    verdict["passed"] = not reasons
    if not_reported and reasons == [NOT_REPORTED]:
        verdict["blocked"] = BLOCKED_NOT_REPORTED
    return verdict


def readiness_final(verdict: dict[str, Any]) -> bool:
    """True when waiting cannot change the verdict: passed, or this build does not report the field."""

    return bool(verdict.get("passed") or verdict.get("blocked"))


def engine_settled(body: Any) -> bool:
    """False while the product itself says the engine is moving (a switch in progress)."""

    if not isinstance(body, dict):
        return True
    operation = body.get("operation") if isinstance(body.get("operation"), dict) else {}
    return body.get("state") != "switching" and operation.get("status") not in ENGINE_PROGRESS


def setup_settled(execution: Any) -> bool:
    """A running setup step is progress, never a settled state (package installs can be slow)."""

    return not (isinstance(execution, dict) and execution.get("status") == "running")
