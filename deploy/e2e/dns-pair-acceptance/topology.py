"""Topology and OS placement for the DNS pair acceptance driver.

The kill-matrix fixture provisions exactly two disposable guests with fixed
peer-link addresses: ``debian13`` (192.0.2.10) and ``arch`` (192.0.2.11).
CelikPanel's PowerDNS target is certified only on Debian with APT
(``cmd/agent/dns_engine_pdns_unit.go``), so a PowerDNS role is always placed
on ``debian13``; BIND may run on either guest.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

# One run label names one fixture cell and prefixes the evidence run ID; both
# checkers (fixture CELL_ID_RE, evidence RUN_ID_RE) admit lowercase only.
RUN_LABEL_RE = re.compile(r"[a-z0-9][a-z0-9-]{0,31}")
NODE_ADDRESSES = {"debian13": "192.0.2.10", "arch": "192.0.2.11"}
NODE_OS = {"debian13": "debian-13", "arch": "arch"}
ENGINES = ("bind", "pdns")
PDNS_NODES = frozenset({"debian13"})

# Topology name -> (primary engine, secondary engine, is control)
TOPOLOGIES = {
    "bind-primary/pdns-secondary": ("bind", "pdns", False),
    "pdns-primary/bind-secondary": ("pdns", "bind", False),
    "bind/bind": ("bind", "bind", True),
}


class TopologyError(ValueError):
    pass


@dataclass(frozen=True)
class Role:
    role: str  # "primary" | "secondary"
    engine: str  # "bind" | "pdns"
    node: str  # fixture node name
    address: str
    peer_node: str
    peer_address: str
    nameserver: str  # this server's NS host name
    peer_nameserver: str


@dataclass(frozen=True)
class Topology:
    name: str
    primary: Role
    secondary: Role
    control: bool
    zone: str
    infra_zone: str

    @property
    def roles(self) -> tuple[Role, Role]:
        return (self.primary, self.secondary)

    def role_for_node(self, node: str) -> Role:
        for role in self.roles:
            if role.node == node:
                return role
        raise TopologyError(f"node {node!r} has no role in {self.name}")

    def cell_id(self, run_label: str) -> str:
        # fixture.validate_cell_id requires "__" and [a-z0-9_-]; one run = one cell.
        if RUN_LABEL_RE.fullmatch(run_label) is None:
            raise TopologyError(f"run label must be lowercase letters, digits and '-' (at most 32): {run_label!r}")
        engines = f"{self.primary.engine}-{self.primary.node}__{self.secondary.engine}-{self.secondary.node}"
        return f"pair-accept__{engines}__{run_label}"

    def as_dict(self) -> dict:
        def role(value: Role) -> dict:
            return {
                "role": value.role,
                "engine": value.engine,
                "node": value.node,
                "os": NODE_OS[value.node],
                "address": value.address,
                "nameserver": value.nameserver,
                "peer_node": value.peer_node,
                "peer_address": value.peer_address,
                "peer_nameserver": value.peer_nameserver,
            }

        return {
            "name": self.name,
            "control": self.control,
            "zone": self.zone,
            "infra_zone": self.infra_zone,
            "primary": role(self.primary),
            "secondary": role(self.secondary),
        }


def _other(node: str) -> str:
    return "arch" if node == "debian13" else "debian13"


def _label(value: str, what: str) -> str:
    text = value.strip().lower().rstrip(".")
    labels = text.split(".")
    if (
        len(labels) < 2
        or len(text) > 200
        or any(not part or len(part) > 63 or part.startswith("-") or part.endswith("-") for part in labels)
        or any(not all(ch.isalnum() or ch == "-" for ch in part) for part in labels)
    ):
        raise TopologyError(f"{what} is not a canonical DNS name: {value!r}")
    return text


def resolve(
    name: str,
    *,
    primary_node: str | None = None,
    zone: str = "pair-accept.test",
    infra_zone: str = "ns-accept.test",
) -> Topology:
    """Resolve and validate a topology with its OS placement.

    ``primary_node`` is optional for the PowerDNS topologies (their placement
    is forced by the Debian-only PowerDNS rule) and required for ``bind/bind``
    so the control's placement is always explicit in the evidence.
    """

    if name not in TOPOLOGIES:
        raise TopologyError(f"unknown topology {name!r}; choose one of {sorted(TOPOLOGIES)}")
    primary_engine, secondary_engine, control = TOPOLOGIES[name]
    if primary_node is not None and primary_node not in NODE_ADDRESSES:
        raise TopologyError(f"unknown node {primary_node!r}; choose debian13 or arch")
    if primary_engine == "pdns":
        forced = "debian13"
    elif secondary_engine == "pdns":
        forced = "arch"
    else:
        forced = None
    if forced is not None:
        if primary_node is not None and primary_node != forced:
            raise TopologyError(
                f"{name}: PowerDNS is certified only on Debian 13, so the primary must be "
                f"{forced!r} (got {primary_node!r})"
            )
        primary_node = forced
    elif primary_node is None:
        raise TopologyError(f"{name}: --primary-node is required (debian13 or arch)")
    secondary_node = _other(primary_node)
    for engine, node in ((primary_engine, primary_node), (secondary_engine, secondary_node)):
        if engine == "pdns" and node not in PDNS_NODES:
            raise TopologyError(f"PowerDNS cannot be placed on {node!r}; Debian 13 only")
    zone = _label(zone, "zone")
    infra_zone = _label(infra_zone, "infrastructure zone")
    if zone == infra_zone or zone.endswith("." + infra_zone) or infra_zone.endswith("." + zone):
        raise TopologyError("the test zone and the nameserver zone must be unrelated")
    ns1, ns2 = f"ns1.{infra_zone}", f"ns2.{infra_zone}"
    primary = Role(
        "primary", primary_engine, primary_node, NODE_ADDRESSES[primary_node],
        secondary_node, NODE_ADDRESSES[secondary_node], ns1, ns2,
    )
    secondary = Role(
        "secondary", secondary_engine, secondary_node, NODE_ADDRESSES[secondary_node],
        primary_node, NODE_ADDRESSES[primary_node], ns2, ns1,
    )
    return Topology(name, primary, secondary, control, zone, infra_zone)
