#!/usr/bin/env python3
"""Remove one member from a disposable primary using native BIND alone.

Run as root *inside* the paired Arch kill-matrix guest after the fault cell
has converged. This is an owner edit, not a CelikPanel deletion operation.
"""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess


CELL = "bind__intent__after-write__paired-primary__peer-reachable"
CATALOG = "catalog-c000020b.celikpanel.invalid"
MEMBER = "s1-kill.test."
MARKER = Path("/etc/celikpanel-dns-kill-matrix")
ROOT = Path("/var/named/celikpanel-native-delete")
NAMED_CONF = Path("/etc/named.conf")
MANAGED_INCLUDE = 'include "/var/named/celikpanel/current/zones.conf";'
OWNER_INCLUDE = f'include "{ROOT}/zones.conf";'


def run(*argv: str) -> None:
    subprocess.run(argv, check=True)


def remove(cell_id: str) -> None:
    expected = (
        "schema=celikpanel/dns-kill-fixture-plan/v1\n"
        f"cell_id={cell_id}\nnode=arch\n"
    )
    if cell_id != CELL or MARKER.read_text(encoding="ascii") != expected:
        raise ValueError("not the exact disposable Arch BIND primary")
    if ROOT.exists():
        raise ValueError("native owner edit already exists")

    generation = Path("/var/named/celikpanel/current").resolve(strict=True)
    config = (generation / "zones.conf").read_text(encoding="ascii")
    paths = re.findall(r'file "([^"]+)";', config)
    if len(paths) != 2:
        raise ValueError("unexpected managed zone count")
    catalog_path = next(
        (Path(p) for p in paths if f"$ORIGIN {CATALOG}." in Path(p).read_text(encoding="ascii")),
        None,
    )
    if catalog_path is None or catalog_path.parent != generation / "zones":
        raise ValueError("unexpected catalog source")
    text = catalog_path.read_text(encoding="ascii")
    lines = text.splitlines(keepends=True)
    members = [line for line in lines if ".zones IN PTR " in line]
    if len(members) != 1 or MEMBER not in members[0] or "@ IN SOA invalid. invalid. 1 " not in text:
        raise ValueError("unexpected catalog membership or serial")
    named = NAMED_CONF.read_text(encoding="ascii")
    if named.count(MANAGED_INCLUDE) != 1:
        raise ValueError("unexpected BIND include")
    run("systemctl", "disable", "--now", "celikpanel-agent.service", "celikpanel-panel.service")
    for service in ("celikpanel-agent.service", "celikpanel-panel.service"):
        if subprocess.run(["systemctl", "is-active", "--quiet", service]).returncode == 0:
            raise ValueError(f"management still active: {service}")
    ROOT.mkdir(mode=0o755)
    owner_zone = ROOT / "catalog.zone"
    owner_zone.write_text(
        text.replace("@ IN SOA invalid. invalid. 1 ", "@ IN SOA invalid. invalid. 2 ")
        .replace(members[0], ""),
        encoding="ascii",
    )
    owner_config = ROOT / "zones.conf"
    owner_config.write_text(config.replace(str(catalog_path), str(owner_zone)), encoding="ascii")
    shutil.copy2(NAMED_CONF, ROOT / "named.conf.before")
    candidate = ROOT / "named.conf.candidate"
    candidate.write_text(named.replace(MANAGED_INCLUDE, OWNER_INCLUDE), encoding="ascii")
    run("named-checkzone", CATALOG, str(owner_zone))
    run("named-checkconf", "-z", str(candidate))
    staged = NAMED_CONF.with_name(".named.conf.native-delete")
    shutil.copy2(candidate, staged)
    os.replace(staged, NAMED_CONF)
    run("systemctl", "reload", "named.service")
    run("systemctl", "is-active", "--quiet", "named.service")
    print(f"NATIVE_CATALOG_MEMBER_REMOVED cell={cell_id} catalog_serial=2")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cell-id", required=True)
    args = parser.parse_args()
    remove(args.cell_id)


if __name__ == "__main__":
    main()
