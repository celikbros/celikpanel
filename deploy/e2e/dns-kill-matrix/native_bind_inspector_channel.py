#!/usr/bin/env python3
"""Exercise the optional native BIND peer proof channel in one disposable pair.

This is a fixture, not a production enrollment or deletion tool. The primary
guest runs the SSH client over the private peer network; the secondary has no
CelikPanel service. No proof is treated as an accepted mutation by this script.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
from pathlib import Path
import secrets
import subprocess
import tempfile
import time

import fixture
import guest_bootstrap as bootstrap
import native_bind_peer


CELL = "bind__intent__after-write__paired-primary__peer-reachable"
COMMAND = "celikpanel-bind-peer-inspect-v1"
POLICY_SCHEMA = "celikpanel-bind-peer-inspector-policy/v1"
REQUEST_SCHEMA = "celikpanel-bind-peer-deletion-request/v1"
RESPONSE_SCHEMA = "celikpanel-bind-peer-deletion-observation/v1"
CATALOG = "catalog-c000020b.celikpanel.invalid"
STAGE = "/home/celik/celikpanel-native-inspector"
PRIMARY_ROOT = "/etc/celikpanel-native-peer-proof"
INSPECTOR = "/opt/celikpanel/bin/bind-peer-inspect"
FORCED_WRAPPER = "/opt/celikpanel/libexec/bind-peer-inspect-forced"


def canonical(value: dict) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("ascii")


def member_digest(serial: int, members: list[str]) -> str:
    if not 0 < serial < 2**32 or members != sorted(set(members)):
        raise ValueError("catalog members or serial are invalid")
    out = bytearray()

    def frame(value: bytes) -> None:
        out.extend(len(value).to_bytes(4, "big"))
        out.extend(value)

    frame(b"celikpanel-bind-peer-catalog-members/v1")
    frame(CATALOG.encode("ascii"))
    out.extend(serial.to_bytes(4, "big"))
    out.extend(len(members).to_bytes(4, "big"))
    for member in members:
        if member.endswith(".") or member == CATALOG:
            raise ValueError("invalid member name")
        frame(member.encode("ascii"))
    return hashlib.sha256(out).hexdigest()


def selected_pair(args: argparse.Namespace) -> tuple[dict, dict]:
    if args.cell_id != CELL:
        raise ValueError("inspector fixture is restricted to the exact paired BIND cell")
    root = args.work_root.resolve(strict=True)
    plan = fixture.load_cell_plan(root, args.cell_id)
    cell = bootstrap.load_manifest_cell(args.manifest, args.cell_id)
    bootstrap.validate_bind_cell(cell, "arch", "uninitialized")
    primary_ip, peer_ip, _ = native_bind_peer.pair_addresses(plan, cell)
    if (primary_ip, peer_ip) != ("192.0.2.11", "192.0.2.10"):
        raise ValueError("unexpected peer network")
    return plan["nodes"]["arch"], plan["nodes"]["debian13"]


def management(node: dict, identity: Path, command: str, *, capture: bool = False) -> str:
    ssh = bootstrap.ssh_base(node, identity)
    if not capture:
        bootstrap.run(ssh + [command], execute=True)
        return ""
    result = subprocess.run(ssh + [command], check=True, capture_output=True, text=True)
    return result.stdout


def marker(node: dict, identity: Path, cell_id: str, name: str) -> None:
    expected = f"schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id={cell_id}\nnode={name}\n"
    if management(node, identity, "cat /etc/celikpanel-dns-kill-matrix", capture=True) != expected:
        raise ValueError("guest marker mismatch")


def transfer(node: dict, identity: Path, local: Path, remote: str) -> None:
    bootstrap.run(
        bootstrap.scp_base(node, identity) + [str(local), bootstrap.remote_destination(node, remote)],
        execute=True,
    )


def host_key(node: dict, identity: Path) -> tuple[str, str]:
    line = management(node, identity, "sudo cat /etc/ssh/ssh_host_ed25519_key.pub", capture=True).strip()
    parts = line.split()
    if len(parts) not in (2, 3) or parts[0] != "ssh-ed25519":
        raise ValueError("unexpected secondary SSH host key")
    wire = base64.b64decode(parts[1], validate=True)
    if not wire.startswith(b"\x00\x00\x00\x0bssh-ed25519"):
        raise ValueError("invalid secondary SSH host key wire format")
    return " ".join(parts[:2]), hashlib.sha256(wire).hexdigest()


def agent_enrollment_record(host_digest: str, client_digest: str,
                            enrollment_id: str, credential_id: str) -> dict:
    return {
        "schema": "celikpanel-dns-peer-inspection/v1",
        "enrollment_id": enrollment_id, "revision": 1,
        "primary_ip": "192.0.2.11", "peer_ip": "192.0.2.10",
        "catalog_name": CATALOG, "view": "_default",
        "ssh_username": "celikpeer", "host_key_sha256": host_digest,
        "credential_id": credential_id, "client_public_key_sha256": client_digest,
    }


def public_key_digest(line: str) -> str:
    parts = line.split()
    if len(parts) not in (2, 3) or parts[0] != "ssh-ed25519":
        raise ValueError("unexpected SSH public key")
    wire = base64.b64decode(parts[1], validate=True)
    if not wire.startswith(b"\x00\x00\x00\x0bssh-ed25519"):
        raise ValueError("invalid SSH public key wire format")
    return hashlib.sha256(wire).hexdigest()


def fixture_key_dir(root: Path, cell_id: str) -> Path:
    target = root / cell_id / "native-peer-inspector"
    target.mkdir(mode=0o700, parents=True, exist_ok=True)
    if target.is_symlink() or target.stat().st_mode & 0o077:
        raise ValueError("unsafe fixture credential directory")
    return target


def enroll(args: argparse.Namespace) -> None:
    if not args.execute:
        raise ValueError("enroll requires --execute; dry-run does not establish peer identity")
    primary, secondary = selected_pair(args)
    identity = bootstrap.identity_file(args.identity_file)
    marker(primary, identity, args.cell_id, "arch")
    marker(secondary, identity, args.cell_id, "debian13")
    management(secondary, identity, "test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel")
    management(secondary, identity, "systemctl is-active --quiet named.service && sudo named-checkconf /etc/bind/named.conf")
    management(secondary, identity, "sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq bind9-dnsutils")
    if args.inspector is None or not args.inspector.is_file():
        raise ValueError("a built Linux bind-peer-inspect binary is required")
    target = fixture_key_dir(args.work_root.resolve(strict=True), args.cell_id)
    key = target / "id_ed25519"
    if key.exists() or key.with_suffix(".pub").exists():
        raise ValueError("fixture key already exists; refuse to overwrite enrollment")
    subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(key)], check=True)
    public_key = key.with_suffix(".pub").read_text(encoding="ascii").strip()
    if not public_key.startswith("ssh-ed25519 "):
        raise ValueError("unexpected fixture client key")
    known_host, host_digest = host_key(secondary, identity)
    policy = {
        "schema": POLICY_SCHEMA, "primary_ip": "192.0.2.11", "peer_ip": "192.0.2.10",
        "catalog_name": CATALOG, "view": "_default",
    }
    policy_file = target / "policy.json"
    policy_file.write_bytes(canonical(policy))
    authorization = target / "authorized_key"
    authorization.write_text(
        'restrict,from="192.0.2.11" ' + " ".join(public_key.split()[:2]) + "\n",
        encoding="ascii", newline="\n",
    )
    sudoers = target / "sudoers"
    sudoers.write_text('celikpeer ALL=(root) NOPASSWD: ' + INSPECTOR + ' ""\n', encoding="ascii", newline="\n")
    wrapper = Path(__file__).with_name("native_bind_inspector_forced.sh")
    sshd = Path(__file__).with_name("native_bind_inspector_sshd.conf")
    known = target / "known_hosts"
    known.write_text("192.0.2.10 " + known_host + "\n", encoding="ascii", newline="\n")
    for file, destination in (
        (args.inspector, STAGE + ".bin"), (authorization, STAGE + ".authorized_key"),
        (policy_file, STAGE + ".policy.json"), (sudoers, STAGE + ".sudoers"),
        (sshd, STAGE + ".sshd.conf"), (wrapper, STAGE + ".forced-wrapper"),
    ):
        transfer(secondary, identity, file, destination)
    management(secondary, identity, " && ".join((
        "sudo install -d -m 0755 -o root -g root /opt/celikpanel /opt/celikpanel/bin",
        f"sudo install -m 0755 -o root -g root {STAGE}.bin {INSPECTOR}",
        "sudo install -d -m 0755 -o root -g root /opt/celikpanel/libexec",
        f"sudo install -m 0755 -o root -g root {STAGE}.forced-wrapper {FORCED_WRAPPER}",
        "sudo install -d -m 0750 -o root -g root /etc/bind-peer-inspector",
        f"sudo install -m 0600 -o root -g root {STAGE}.policy.json /etc/bind-peer-inspector/policy.json",
        "sudo install -d -m 0755 -o root -g root /etc/ssh/authorized_keys",
        f"sudo install -m 0644 -o root -g root {STAGE}.authorized_key /etc/ssh/authorized_keys/celikpeer",
        "(id -u celikpeer >/dev/null 2>&1 || sudo useradd -m -s /bin/sh celikpeer)",
        "sudo passwd -l celikpeer",
        f"sudo install -m 0440 -o root -g root {STAGE}.sudoers /etc/sudoers.d/celikpanel-native-peer",
        "sudo visudo -cf /etc/sudoers.d/celikpanel-native-peer",
        f"sudo install -m 0644 -o root -g root {STAGE}.sshd.conf /etc/ssh/sshd_config.d/99-celikpanel-native-peer.conf",
        "sudo /usr/sbin/sshd -t", "sudo systemctl reload ssh.service",
    )))
    transfer(primary, identity, key, STAGE + ".key")
    transfer(primary, identity, known, STAGE + ".known_hosts")
    management(primary, identity, " && ".join((
        f"sudo install -d -m 0700 -o root -g root {PRIMARY_ROOT}",
        f"sudo install -m 0600 -o root -g root {STAGE}.key {PRIMARY_ROOT}/id_ed25519",
        f"sudo install -m 0600 -o root -g root {STAGE}.known_hosts {PRIMARY_ROOT}/known_hosts",
        f"rm -- {STAGE}.key {STAGE}.known_hosts",
    )))
    print(json.dumps({"enrolled": True, "cell_id": args.cell_id, "peer_host_key_sha256": host_digest,
                      "primary": "192.0.2.11", "peer": "192.0.2.10", "scope": "disposable fixture"}, sort_keys=True))


def agent_enroll(args: argparse.Namespace) -> None:
    if not args.execute:
        raise ValueError("agent-enroll requires --execute")
    primary, secondary = selected_pair(args)
    identity = bootstrap.identity_file(args.identity_file)
    marker(primary, identity, args.cell_id, "arch")
    marker(secondary, identity, args.cell_id, "debian13")
    management(secondary, identity,
        "test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel")
    local = fixture_key_dir(args.work_root.resolve(strict=True), args.cell_id)
    pub_path = local / "id_ed25519.pub"
    if not pub_path.is_file():
        raise ValueError("first enroll the dedicated native inspector SSH account")
    _, host_digest = host_key(secondary, identity)
    client_digest = public_key_digest(pub_path.read_text(encoding="ascii"))
    enrollment_id = secrets.token_hex(16)
    credential_id = secrets.token_hex(16)
    record = agent_enrollment_record(
        host_digest, client_digest, enrollment_id, credential_id,
    )
    with tempfile.TemporaryDirectory(prefix="native-agent-enrollment-") as temporary:
        source = Path(temporary) / "record.json"
        source.write_bytes(canonical(record))
        transfer(primary, identity, source, STAGE + ".agent-enrollment")
    state = "/var/lib/celikpanel-agent-private"
    key_dir = state + "/dns-peer-inspection-keys"
    record_path = state + "/dns-peer-inspection-v1.json"
    key_path = key_dir + "/" + credential_id + ".key"
    management(primary, identity, " && ".join((
        f"sudo test -d {state}",
        f"sudo test ! -e {record_path}",
        f"sudo test ! -e {key_path}",
        f"sudo install -d -m 0700 -o root -g root {key_dir}",
        f"sudo install -m 0600 -o root -g root {PRIMARY_ROOT}/id_ed25519 {key_path}",
        f"sudo install -m 0600 -o root -g root {STAGE}.agent-enrollment {record_path}",
        f"rm -- {STAGE}.agent-enrollment",
        f"sudo test -s {record_path}", f"sudo test -s {key_path}",
    )))
    print(json.dumps({
        "agent_enrollment": "fixture-only", "cell_id": args.cell_id,
        "record_sha256": hashlib.sha256(canonical(record)).hexdigest(),
        "host_key_sha256": host_digest, "client_public_key_sha256": client_digest,
        "credential_id": credential_id, "enrollment_id": enrollment_id,
        "record_path": record_path,
    }, sort_keys=True))


def request(args: argparse.Namespace, identity_sha: str) -> dict:
    now = int(time.time())
    if args.catalog_serial is None or args.request_id is None or args.owner_id is None or args.qualifier is None:
        raise ValueError("probe requires exact operation IDs, qualifier and catalog serial")
    members = sorted(args.catalog_member or [])
    return {
        "schema": REQUEST_SCHEMA, "mutation_request_id": args.request_id,
        "mutation_owner_id": args.owner_id, "deletion_generation": args.generation,
        "deletion_qualifier": args.qualifier, "primary_ip": "192.0.2.11",
        "peer_ip": "192.0.2.10", "peer_identity_sha256": identity_sha,
        "catalog_name": CATALOG, "catalog_serial": args.catalog_serial,
        "catalog_members_sha256": member_digest(args.catalog_serial, members),
        "deleted_zone": args.deleted_zone, "view": "_default",
        "nonce": secrets.token_hex(32), "attempt": args.attempt,
        "issued_at_unix": now, "expires_at_unix": now + 30,
    }


def probe(args: argparse.Namespace) -> None:
    if not args.execute:
        raise ValueError("probe requires --execute")
    primary, secondary = selected_pair(args)
    identity = bootstrap.identity_file(args.identity_file)
    marker(primary, identity, args.cell_id, "arch")
    marker(secondary, identity, args.cell_id, "debian13")
    _, host_digest = host_key(secondary, identity)
    body = request(args, host_digest)
    raw = canonical(body)
    with tempfile.TemporaryDirectory(prefix="native-bind-inspector-") as temp:
        local = Path(temp) / "request.json"
        local.write_bytes(raw + b"\n")
        transfer(primary, identity, local, STAGE + ".request")
    command = (
        f"sudo ssh -F /dev/null -i {PRIMARY_ROOT}/id_ed25519"
        " -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=5"
        " -o StrictHostKeyChecking=yes"
        f" -o UserKnownHostsFile={PRIMARY_ROOT}/known_hosts"
        " -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no"
        " -o ClearAllForwardings=yes -T celikpeer@192.0.2.10"
        f" {COMMAND} < {STAGE}.request"
    )
    raw_response = management(primary, identity, command, capture=True).encode("ascii")
    if len(raw_response) > 4097 or not raw_response.endswith(b"\n"):
        raise ValueError("inspector response is missing or over bound")
    response = json.loads(raw_response)
    if canonical(response) + b"\n" != raw_response or response.get("schema") != RESPONSE_SCHEMA:
        raise ValueError("inspector response is not canonical v1")
    expected_hash = hashlib.sha256(raw).hexdigest()
    expected = {"request_sha256": expected_hash, "nonce": body["nonce"],
                "attempt": body["attempt"], "catalog_serial": body["catalog_serial"],
                "catalog_members_sha256": body["catalog_members_sha256"],
                "deleted_zone": body["deleted_zone"]}
    if any(response.get(key) != value for key, value in expected.items()):
        raise ValueError("inspector response does not bind the request")
    denied = {}
    for label, altered in (
        ("untrusted_host_key", command.replace(
            f"UserKnownHostsFile={PRIMARY_ROOT}/known_hosts",
            "UserKnownHostsFile=/dev/null",
        )),
        ("extra_original_command", command.replace(
            f" {COMMAND} <", f" {COMMAND}-unexpected <",
        )),
        ("wrong_client_credential", command.replace(
            f"-i {PRIMARY_ROOT}/id_ed25519", "-i /dev/null",
        )),
    ):
        try:
            management(primary, identity, altered, capture=True)
        except subprocess.CalledProcessError:
            denied[label] = True
        else:
            raise ValueError(f"peer SSH restriction admitted {label}")
    positive = all(response.get(k) == v for k, v in (
        ("catalog_state", "transferred"), ("member_state", "absent"),
        ("native_state", "unloaded")))
    print(json.dumps({"cell_id": args.cell_id, "request_sha256": expected_hash,
                      "peer_host_key_sha256": host_digest, "response": response,
                      "native_absence_observed": positive, "negative_channels_denied": denied,
                      "accepted_deletion_proven": False}, sort_keys=True))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("enroll", "agent-enroll", "probe"))
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    parser.add_argument("--inspector", type=Path)
    parser.add_argument("--request-id")
    parser.add_argument("--owner-id")
    parser.add_argument("--generation", type=int, default=0)
    parser.add_argument("--qualifier")
    parser.add_argument("--catalog-serial", type=int)
    parser.add_argument("--catalog-member", action="append")
    parser.add_argument("--deleted-zone", default="s1-kill.test")
    parser.add_argument("--attempt", type=int, default=1)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    try:
        {"enroll": enroll, "agent-enroll": agent_enroll, "probe": probe}[args.action](args)
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError,
            bootstrap.BootstrapError, fixture.FixtureError) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
