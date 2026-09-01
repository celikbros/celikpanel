#!/usr/bin/env python3
"""Provision and control the disposable Debian 13 + Arch QEMU fixture.

The command is dry-run by default.  It uses only the Python standard library;
QEMU, qemu-img, curl, an ISO builder, and ssh are external host tools.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import socket
import stat
import subprocess
import sys
import tempfile
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable, Sequence
from urllib.parse import unquote, urlsplit


LOCK_SCHEMA = "celikpanel/dns-kill-image-lock/v1"
ROOT_MARKER_SCHEMA = "celikpanel/dns-kill-fixture-root/v1"
PLAN_SCHEMA = "celikpanel/dns-kill-fixture-plan/v1"
KILL_PROOF_SCHEMA = "celikpanel/dns-kill-proof/v1"
ROOT_MARKER = ".celikpanel-dns-kill-matrix-root"
EXPECTED_IMAGES = ("debian13", "arch")
DEFAULT_IMAGE_LOCK = Path(__file__).with_name("images.lock.json")
CELL_ID_RE = re.compile(r"[a-z0-9][a-z0-9_-]{1,239}")
DIGEST_LENGTHS = {"sha256": 64, "sha512": 128}
SSH_PUBLIC_KEY_RE = re.compile(
    r"(?:ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(?:256|384|521)) "
    r"[A-Za-z0-9+/]+={0,3}(?: [^\r\n]+)?"
)
DEBIAN_URL_RE = re.compile(
    r"/images/cloud/trixie/(?P<build>[0-9]{8}-[0-9]+)/"
    r"debian-13-genericcloud-amd64-(?P=build)\.qcow2"
)
ARCH_URL_RE = re.compile(
    r"/images/v(?P<build>[0-9]{8}\.[0-9]+)/"
    r"Arch-Linux-x86_64-cloudimg-(?P=build)\.qcow2"
)
NAMESPACE = uuid.UUID("718f806f-5f2f-4a43-8fa0-88b642d2d432")


class FixtureError(RuntimeError):
    pass


@dataclass(frozen=True)
class ImagePin:
    name: str
    distribution: str
    release: str
    architecture: str
    url: str
    filename: str
    digest_algorithm: str
    digest: str
    size: int


@dataclass(frozen=True)
class NodeSpec:
    name: str
    hostname: str
    image: str
    mgmt_mac: str
    peer_mac: str
    peer_address: str
    ssh_service: str
    admin_group: str


NODES = (
    NodeSpec(
        name="debian13",
        hostname="dns-debian13",
        image="debian13",
        mgmt_mac="52:54:00:13:00:10",
        peer_mac="52:54:00:53:00:10",
        peer_address="192.0.2.10/24",
        ssh_service="ssh.service",
        admin_group="sudo",
    ),
    NodeSpec(
        name="arch",
        hostname="dns-arch",
        image="arch",
        mgmt_mac="52:54:00:13:00:11",
        peer_mac="52:54:00:53:00:11",
        peer_address="192.0.2.11/24",
        ssh_service="sshd.service",
        admin_group="wheel",
    ),
)


def _reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise FixtureError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def read_json_regular(path: Path, purpose: str) -> Any:
    try:
        info = path.lstat()
    except FileNotFoundError as exc:
        raise FixtureError(f"{purpose} is missing: {path}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise FixtureError(f"{purpose} must be a regular, non-symlink file: {path}")
    try:
        return json.loads(
            path.read_text(encoding="utf-8"), object_pairs_hook=_reject_duplicate_keys
        )
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise FixtureError(f"cannot read canonical {purpose}: {path}: {exc}") from exc


def _validate_image_url(name: str, value: str, filename: str) -> None:
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or parsed.username is not None
        or parsed.password is not None
        or parsed.port is not None
        or parsed.query
        or parsed.fragment
        or unquote(parsed.path) != parsed.path
        or Path(parsed.path).name != filename
        or "/latest/" in parsed.path
    ):
        raise FixtureError(f"{name} image URL is not an immutable HTTPS pin")
    if name == "debian13":
        if parsed.hostname != "cloud.debian.org" or not DEBIAN_URL_RE.fullmatch(
            parsed.path
        ):
            raise FixtureError(
                "Debian image must be an immutable official Debian 13 genericcloud URL"
            )
    elif name == "arch":
        if parsed.hostname != "geo.mirror.pkgbuild.com" or not ARCH_URL_RE.fullmatch(
            parsed.path
        ):
            raise FixtureError(
                "Arch image must be an immutable official versioned pkgbuild URL"
            )


def load_image_lock(path: Path) -> dict[str, ImagePin]:
    raw = read_json_regular(path, "image lock")
    if not isinstance(raw, dict) or set(raw) != {"schema", "images"}:
        raise FixtureError("image lock must contain only schema and images")
    if raw["schema"] != LOCK_SCHEMA or not isinstance(raw["images"], dict):
        raise FixtureError("image lock schema is unsupported")
    if set(raw["images"]) != set(EXPECTED_IMAGES):
        raise FixtureError("image lock must pin exactly debian13 and arch")
    pins: dict[str, ImagePin] = {}
    for name in EXPECTED_IMAGES:
        item = raw["images"][name]
        required = {
            "distribution",
            "release",
            "architecture",
            "url",
            "filename",
            "digest",
            "bytes",
        }
        if not isinstance(item, dict) or set(item) != required:
            raise FixtureError(f"{name} image pin fields are incomplete or unexpected")
        strings = {key: item[key] for key in required - {"bytes", "digest"}}
        if any(not isinstance(value, str) or not value for value in strings.values()):
            raise FixtureError(f"{name} image pin contains an empty string")
        expected_distribution = "Debian" if name == "debian13" else "Arch Linux"
        expected_release = "13" if name == "debian13" else "rolling"
        if (
            item["distribution"] != expected_distribution
            or item["release"] != expected_release
            or item["architecture"] != "x86_64"
        ):
            raise FixtureError(f"{name} image identity is not the certified fixture")
        digest = item["digest"]
        expected_algorithm = "sha512" if name == "debian13" else "sha256"
        if not isinstance(digest, dict) or set(digest) != {"algorithm", "value"}:
            raise FixtureError(f"{name} digest must contain only algorithm and value")
        if digest["algorithm"] != expected_algorithm:
            raise FixtureError(
                f"{name} digest algorithm must be the official {expected_algorithm}"
            )
        digest_value = digest["value"]
        digest_length = DIGEST_LENGTHS[expected_algorithm]
        if (
            not isinstance(digest_value, str)
            or not re.fullmatch(rf"[0-9a-f]{{{digest_length}}}", digest_value)
        ):
            raise FixtureError(
                f"{name} {expected_algorithm} must be {digest_length} lowercase hexadecimal digits"
            )
        if len(set(digest_value)) == 1:
            raise FixtureError(f"{name} digest is a placeholder, not a pin")
        if (
            not isinstance(item["bytes"], int)
            or isinstance(item["bytes"], bool)
            or item["bytes"] <= 0
        ):
            raise FixtureError(f"{name} byte size must be a positive integer")
        if Path(item["filename"]).name != item["filename"] or not item[
            "filename"
        ].endswith(".qcow2"):
            raise FixtureError(f"{name} filename must be a plain qcow2 basename")
        _validate_image_url(name, item["url"], item["filename"])
        pins[name] = ImagePin(
            name=name,
            distribution=item["distribution"],
            release=item["release"],
            architecture=item["architecture"],
            url=item["url"],
            filename=item["filename"],
            digest_algorithm=expected_algorithm,
            digest=digest_value,
            size=item["bytes"],
        )
    return pins


def canonical_work_root(value: Path) -> Path:
    if not value.is_absolute():
        raise FixtureError("--work-root must be absolute")
    if value.exists() and value.is_symlink():
        raise FixtureError("work root must not be a symlink")
    root = value.resolve(strict=False)
    forbidden = {Path("/").resolve(), Path.home().resolve(), Path.cwd().resolve()}
    if root in forbidden or len(root.parts) < 3:
        raise FixtureError(f"refusing unsafe work root: {root}")
    return root


def marker_path(root: Path) -> Path:
    return root / ROOT_MARKER


def require_linux_qemu_host(platform: str | None = None) -> None:
    current = sys.platform if platform is None else platform
    if current != "linux":
        raise FixtureError(
            "QEMU fixture lifecycle commands require a Linux host; the generated "
            "commands deliberately use KVM/TCG, Unix QMP sockets, and -daemonize"
        )


def _validate_root_directory(root: Path, name: str) -> Path:
    target = root / name
    try:
        info = target.lstat()
    except FileNotFoundError as exc:
        raise FixtureError(f"validated harness directory is missing: {target}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        raise FixtureError(
            f"validated harness directory must be a real non-symlink directory: {target}"
        )
    return target


def validate_work_root(value: Path) -> Path:
    root = canonical_work_root(value)
    marker = marker_path(root)
    try:
        info = marker.lstat()
    except FileNotFoundError as exc:
        raise FixtureError(f"validated harness root marker is missing: {marker}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise FixtureError("harness root marker must be a regular non-symlink file")
    if marker.read_text(encoding="utf-8") != ROOT_MARKER_SCHEMA + "\n":
        raise FixtureError("harness root marker has unexpected contents")
    _validate_root_directory(root, "images")
    _validate_root_directory(root, "cells")
    return root


def initialize_work_root(value: Path) -> Path:
    root = canonical_work_root(value)
    if root.exists():
        entries = list(root.iterdir())
        if entries:
            marker = marker_path(root)
            if not marker.is_file() or marker.is_symlink():
                raise FixtureError(
                    "refusing to initialize a non-empty directory without its harness marker"
                )
            if marker.read_text(encoding="utf-8") != ROOT_MARKER_SCHEMA + "\n":
                raise FixtureError("harness root marker has unexpected contents")
    else:
        root.mkdir(parents=True, mode=0o700)
    marker = marker_path(root)
    if not marker.exists():
        marker.write_text(ROOT_MARKER_SCHEMA + "\n", encoding="utf-8")
        os.chmod(marker, 0o600)
    (root / "images").mkdir(mode=0o700, exist_ok=True)
    (root / "cells").mkdir(mode=0o700, exist_ok=True)
    return validate_work_root(root)


def validate_cell_id(cell_id: str) -> str:
    if (
        not CELL_ID_RE.fullmatch(cell_id)
        or "__" not in cell_id
        or ".." in cell_id
        or cell_id.endswith(("_", "-"))
    ):
        raise FixtureError(f"invalid matrix cell ID: {cell_id!r}")
    return cell_id


def cell_directory(root: Path, cell_id: str, *, must_exist: bool) -> Path:
    root = validate_work_root(root)
    validate_cell_id(cell_id)
    cells = (root / "cells").resolve(strict=False)
    basename = hashlib.sha256(cell_id.encode("utf-8")).hexdigest()[:24]
    target = (cells / basename).resolve(strict=False)
    if target.parent != cells:
        raise FixtureError("cell path escaped the validated cells directory")
    if target.exists() and target.is_symlink():
        raise FixtureError("cell directory must not be a symlink")
    if must_exist and not target.is_dir():
        raise FixtureError(f"cell directory is missing: {target}")
    return target


def digest_file(path: Path, algorithm: str) -> str:
    if algorithm not in DIGEST_LENGTHS:
        raise FixtureError(f"unsupported image digest algorithm: {algorithm}")
    digest = hashlib.new(algorithm)
    with path.open("rb") as handle:
        while True:
            chunk = handle.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
    return digest.hexdigest()


def image_path(root: Path, pin: ImagePin) -> Path:
    target = (root / "images" / pin.filename).resolve(strict=False)
    if target.parent != (root / "images").resolve(strict=False):
        raise FixtureError("image path escaped the validated image directory")
    return target


def verify_image(root: Path, pin: ImagePin, *, require_immutable: bool = True) -> Path:
    target = image_path(root, pin)
    try:
        info = target.lstat()
    except FileNotFoundError as exc:
        raise FixtureError(f"locked base image is missing: {target}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise FixtureError(f"locked base image is not a regular file: {target}")
    if info.st_size != pin.size:
        raise FixtureError(
            f"locked base image size mismatch for {pin.name}: "
            f"got {info.st_size}, want {pin.size}"
        )
    actual = digest_file(target, pin.digest_algorithm)
    if actual != pin.digest:
        raise FixtureError(
            f"locked base image {pin.digest_algorithm.upper()} mismatch for {pin.name}: "
            f"got {actual}, want {pin.digest}"
        )
    if require_immutable and info.st_mode & 0o222:
        raise FixtureError(f"locked base image is writable: {target}")
    return target


def verify_images(root: Path, pins: dict[str, ImagePin]) -> dict[str, Path]:
    root = validate_work_root(root)
    return {name: verify_image(root, pins[name]) for name in EXPECTED_IMAGES}


def shell_join(arguments: Sequence[str]) -> str:
    import shlex

    return shlex.join([str(value) for value in arguments])


def fetch_plan(root: Path, pins: dict[str, ImagePin]) -> dict[str, Any]:
    root = validate_work_root(root)
    downloads = []
    for name in EXPECTED_IMAGES:
        pin = pins[name]
        target = image_path(root, pin)
        temporary = target.with_name(f".{target.name}.download")
        downloads.append(
            {
                "image": name,
                "url": pin.url,
                "target": str(target),
                "temporary": str(temporary),
                "digest": {
                    "algorithm": pin.digest_algorithm,
                    "value": pin.digest,
                },
                "bytes": pin.size,
                "command": [
                    "curl",
                    "--fail",
                    "--location",
                    "--proto",
                    "=https",
                    "--proto-redir",
                    "=https",
                    "--output",
                    str(temporary),
                    pin.url,
                ],
            }
        )
    return {"action": "fetch", "downloads": downloads}


def execute_fetch(root: Path, pins: dict[str, ImagePin], plan: dict[str, Any]) -> None:
    for item in plan["downloads"]:
        pin = pins[item["image"]]
        target = Path(item["target"])
        temporary = Path(item["temporary"])
        if target.exists():
            verify_image(root, pin)
            continue
        if temporary.exists():
            if temporary.is_symlink() or not temporary.is_file():
                raise FixtureError(f"unsafe stale download path: {temporary}")
            temporary.unlink()
        try:
            subprocess.run(item["command"], check=True)
            info = temporary.lstat()
            if not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode):
                raise FixtureError("download did not produce a regular file")
            if (
                info.st_size != pin.size
                or digest_file(temporary, pin.digest_algorithm) != pin.digest
            ):
                raise FixtureError(f"downloaded image does not match lock: {pin.name}")
            os.chmod(temporary, 0o444)
            os.replace(temporary, target)
            verify_image(root, pin)
        except BaseException:
            if temporary.exists() and temporary.parent == (root / "images"):
                temporary.unlink()
            raise


def read_ssh_public_key(path: Path) -> str:
    try:
        info = path.lstat()
    except FileNotFoundError as exc:
        raise FixtureError(f"SSH public key is missing: {path}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise FixtureError("SSH public key must be a regular non-symlink file")
    value = path.read_text(encoding="utf-8").strip()
    if not SSH_PUBLIC_KEY_RE.fullmatch(value):
        raise FixtureError("SSH public key has an unsupported or non-canonical format")
    return value


def cloud_init_files(
    cell_id: str, node: NodeSpec, ssh_public_key: str
) -> dict[str, str]:
    identity = hashlib.sha256(f"{cell_id}\0{node.name}".encode()).hexdigest()[:20]
    quoted_key = json.dumps(ssh_public_key)
    user_data = f"""#cloud-config
users:
  - default
  - name: celik
    gecos: CelikPanel kill matrix
    groups: [{node.admin_group}]
    sudo: [\"ALL=(ALL) NOPASSWD:ALL\"]
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - {quoted_key}
disable_root: true
ssh_pwauth: false
package_update: false
package_upgrade: false
write_files:
  - path: /etc/celikpanel-dns-kill-matrix
    owner: root:root
    permissions: '0444'
    content: |
      schema={PLAN_SCHEMA}
      cell_id={cell_id}
      node={node.name}
runcmd:
  - [systemctl, enable, --now, {node.ssh_service}]
  - [touch, /etc/celikpanel-dns-kill-matrix-ready]
final_message: \"CelikPanel DNS kill fixture ready\"
"""
    meta_data = f"""instance-id: dns-kill-{identity}
local-hostname: {node.hostname}
"""
    network_config = f"""version: 2
ethernets:
  mgmt:
    match:
      macaddress: \"{node.mgmt_mac}\"
    set-name: mgmt0
    dhcp4: true
    dhcp6: false
    optional: false
  peer:
    match:
      macaddress: \"{node.peer_mac}\"
    set-name: peer0
    dhcp4: false
    dhcp6: false
    addresses:
      - {node.peer_address}
    optional: false
"""
    return {
        "user-data": user_data,
        "meta-data": meta_data,
        "network-config": network_config,
    }


def _node_paths(cell: Path, node: NodeSpec) -> dict[str, Path]:
    node_dir = cell / node.name
    return {
        "directory": node_dir,
        "overlay": node_dir / "overlay.qcow2",
        "seed_directory": node_dir / "seed",
        "seed_iso": node_dir / "seed.iso",
        "qmp": node_dir / "qmp.sock",
        "pid": node_dir / "qemu.pid",
        "serial": node_dir / "serial.log",
    }


def _seed_command(tool: str, paths: dict[str, Path]) -> list[str]:
    seed = paths["seed_directory"]
    inputs = [str(seed / name) for name in ("user-data", "meta-data", "network-config")]
    if tool == "genisoimage":
        return [
            "genisoimage",
            "-quiet",
            "-output",
            str(paths["seed_iso"]),
            "-volid",
            "cidata",
            "-joliet",
            "-rock",
            *inputs,
        ]
    if tool == "xorriso":
        return [
            "xorriso",
            "-as",
            "mkisofs",
            "-quiet",
            "-output",
            str(paths["seed_iso"]),
            "-volid",
            "cidata",
            "-joliet",
            "-rock",
            *inputs,
        ]
    raise FixtureError(f"unsupported ISO tool: {tool}")


def _qemu_command(
    cell_id: str,
    node: NodeSpec,
    paths: dict[str, Path],
    ssh_port: int,
    peer_port: int,
    accel: str,
    memory_mb: int,
    cpus: int,
) -> list[str]:
    peer_transport = (
        f"socket,id=peer,listen=127.0.0.1:{peer_port}"
        if node.name == "debian13"
        else f"socket,id=peer,connect=127.0.0.1:{peer_port}"
    )
    vm_uuid = str(uuid.uuid5(NAMESPACE, f"{cell_id}\0{node.name}"))
    return [
        "qemu-system-x86_64",
        "-name",
        f"dns-kill-{node.name}",
        "-uuid",
        vm_uuid,
        "-accel",
        accel,
        "-cpu",
        "host" if accel == "kvm" else "max",
        "-smp",
        str(cpus),
        "-m",
        str(memory_mb),
        "-drive",
        f"file={paths['overlay']},if=virtio,format=qcow2,cache=none,discard=unmap",
        "-drive",
        f"file={paths['seed_iso']},if=virtio,format=raw,media=cdrom,readonly=on",
        "-netdev",
        f"user,id=mgmt,hostfwd=tcp:127.0.0.1:{ssh_port}-:22",
        "-device",
        f"virtio-net-pci,netdev=mgmt,id=mgmt-link,mac={node.mgmt_mac}",
        "-netdev",
        peer_transport,
        "-device",
        f"virtio-net-pci,netdev=peer,id=peer-link,mac={node.peer_mac}",
        "-qmp",
        f"unix:{paths['qmp']},server=on,wait=off",
        "-pidfile",
        str(paths["pid"]),
        "-serial",
        f"file:{paths['serial']}",
        "-display",
        "none",
        "-monitor",
        "none",
        "-daemonize",
    ]


def build_cell_plan(
    root: Path,
    pins: dict[str, ImagePin],
    cell_id: str,
    ssh_public_key: str,
    *,
    debian_ssh_port: int = 2201,
    arch_ssh_port: int = 2202,
    peer_port: int = 23053,
    iso_tool: str = "genisoimage",
    accel: str = "kvm",
    memory_mb: int = 4096,
    cpus: int = 2,
    disk_gb: int = 24,
) -> dict[str, Any]:
    validate_cell_id(cell_id)
    ports = (debian_ssh_port, arch_ssh_port, peer_port)
    if any(port < 1024 or port > 65535 for port in ports) or len(set(ports)) != 3:
        raise FixtureError("SSH and peer transport ports must be distinct unprivileged ports")
    if (
        accel not in {"kvm", "tcg"}
        or memory_mb < 1024
        or cpus < 1
        or disk_gb < 8
        or disk_gb > 256
    ):
        raise FixtureError("invalid QEMU resource or acceleration selection")
    cell = cell_directory(root, cell_id, must_exist=False)
    nodes: dict[str, Any] = {}
    ssh_ports = {"debian13": debian_ssh_port, "arch": arch_ssh_port}
    for node in NODES:
        paths = _node_paths(cell, node)
        if sys.platform == "linux" and len(os.fsencode(paths["qmp"])) > 100:
            raise FixtureError(
                "work root is too long for a portable Linux Unix QMP socket; "
                "choose a shorter --work-root"
            )
        base = image_path(root, pins[node.image])
        overlay = [
            "qemu-img",
            "create",
            "-f",
            "qcow2",
            "-F",
            "qcow2",
            "-b",
            str(base),
            str(paths["overlay"]),
            f"{disk_gb}G",
        ]
        nodes[node.name] = {
            "hostname": node.hostname,
            "management": {
                "ssh_host": "127.0.0.1",
                "ssh_port": ssh_ports[node.name],
                "mac": node.mgmt_mac,
                "mode": "qemu-user-nat",
            },
            "peer": {
                "address": node.peer_address,
                "mac": node.peer_mac,
                "device_id": "peer-link",
                "transport": "loopback-socket",
                "transport_port": peer_port,
            },
            "paths": {key: str(value) for key, value in paths.items()},
            "cloud_init": cloud_init_files(cell_id, node, ssh_public_key),
            "overlay_command": overlay,
            "seed_command": _seed_command(iso_tool, paths),
            "qemu_command": _qemu_command(
                cell_id,
                node,
                paths,
                ssh_ports[node.name],
                peer_port,
                accel,
                memory_mb,
                cpus,
            ),
            "base": {
                "path": str(base),
                "digest": {
                    "algorithm": pins[node.image].digest_algorithm,
                    "value": pins[node.image].digest,
                },
                "bytes": pins[node.image].size,
            },
        }
    return {
        "schema": PLAN_SCHEMA,
        "cell_id": cell_id,
        "work_root": str(root),
        "cell_directory": str(cell),
        "host_requirements": {
            "os": "linux",
            "qmp_transport": "unix",
            "daemonization": "qemu-daemonize",
            "accelerator": accel,
        },
        "start_order": ["debian13", "arch"],
        "peer_link_policy": {
            "initial": "up",
            "qmp_device": "peer-link",
            "change_requires": {
                "schema": KILL_PROOF_SCHEMA,
                "cell_id": cell_id,
                "kill_proven": True,
                "exit_code": 137,
            },
        },
        "nodes": nodes,
    }


def atomic_write_text(path: Path, data: str, mode: int) -> None:
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{path.name}.", suffix=".tmp", dir=path.parent
    )
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, mode)
        os.replace(temporary, path)
    except BaseException:
        temporary.unlink(missing_ok=True)
        raise


def execute_prepare(plan: dict[str, Any]) -> None:
    cell = Path(plan["cell_directory"])
    if cell.exists():
        raise FixtureError(f"fresh per-cell directory already exists: {cell}")
    cell.mkdir(parents=False, mode=0o700)
    atomic_write_text(
        cell / "fixture-plan.json",
        json.dumps(plan, indent=2, sort_keys=True) + "\n",
        0o600,
    )
    for node_name in plan["start_order"]:
        node = plan["nodes"][node_name]
        paths = {key: Path(value) for key, value in node["paths"].items()}
        paths["directory"].mkdir(mode=0o700)
        paths["seed_directory"].mkdir(mode=0o700)
        for filename, contents in node["cloud_init"].items():
            atomic_write_text(paths["seed_directory"] / filename, contents, 0o600)
        subprocess.run(node["overlay_command"], check=True)
        subprocess.run(node["seed_command"], check=True)


def load_cell_plan(root: Path, cell_id: str) -> dict[str, Any]:
    cell = cell_directory(root, cell_id, must_exist=True)
    plan = read_json_regular(cell / "fixture-plan.json", "fixture plan")
    if (
        not isinstance(plan, dict)
        or plan.get("schema") != PLAN_SCHEMA
        or plan.get("cell_id") != cell_id
        or plan.get("work_root") != str(root)
        or plan.get("cell_directory") != str(cell)
        or set(plan.get("nodes", {})) != set(EXPECTED_IMAGES)
    ):
        raise FixtureError("fixture plan identity is invalid")
    for node in NODES:
        expected = _node_paths(cell, node)
        actual = plan["nodes"][node.name].get("paths", {})
        if actual != {key: str(value) for key, value in expected.items()}:
            raise FixtureError("fixture plan contains paths outside its cell directory")
    return plan


def _read_qmp_response(stream: Any) -> dict[str, Any]:
    while True:
        line = stream.readline()
        if not line:
            raise FixtureError("QMP socket closed without a response")
        try:
            value = json.loads(line)
        except json.JSONDecodeError as exc:
            raise FixtureError("QMP returned invalid JSON") from exc
        if "return" in value or "error" in value or "QMP" in value:
            return value


def qmp_execute(
    socket_path: Path, command: str, arguments: dict[str, Any] | None = None
) -> Any:
    try:
        info = socket_path.lstat()
    except FileNotFoundError as exc:
        raise FixtureError(f"QMP socket is unavailable: {socket_path}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISSOCK(info.st_mode):
        raise FixtureError(f"QMP path must be a real Unix socket: {socket_path}")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(5)
        connection.connect(str(socket_path))
        stream = connection.makefile("rwb", buffering=0)
        greeting = _read_qmp_response(stream)
        if "QMP" not in greeting:
            raise FixtureError("QMP greeting is missing")
        for current, current_arguments in (
            ("qmp_capabilities", None),
            (command, arguments),
        ):
            request: dict[str, Any] = {"execute": current}
            if current_arguments is not None:
                request["arguments"] = current_arguments
            stream.write(json.dumps(request, separators=(",", ":")).encode() + b"\r\n")
            response = _read_qmp_response(stream)
            if "error" in response:
                raise FixtureError(f"QMP {current} failed: {response['error']}")
        return response.get("return")


def wait_for_qmp(socket_path: Path, timeout: float = 20.0) -> None:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            qmp_execute(socket_path, "query-status")
            return
        except (FixtureError, OSError) as exc:
            last_error = exc
            time.sleep(0.1)
    raise FixtureError(f"QMP did not become ready: {socket_path}: {last_error}")


def execute_start(plan: dict[str, Any]) -> None:
    for node_name in plan["start_order"]:
        node = plan["nodes"][node_name]
        paths = node["paths"]
        if Path(paths["pid"]).exists() or Path(paths["qmp"]).exists():
            raise FixtureError(f"{node_name} has stale or active runtime files")
        subprocess.run(node["qemu_command"], check=True)
        wait_for_qmp(Path(paths["qmp"]))


def ssh_command(node: dict[str, Any], identity_file: Path) -> list[str]:
    management = node["management"]
    known_hosts = Path(node["paths"]["directory"]).parent / "ssh-known-hosts"
    return [
        "ssh",
        "-o",
        "BatchMode=yes",
        "-o",
        "ConnectTimeout=5",
        "-o",
        "StrictHostKeyChecking=accept-new",
        "-o",
        "HashKnownHosts=no",
        "-o",
        f"UserKnownHostsFile={known_hosts}",
        "-i",
        str(identity_file),
        "-p",
        str(management["ssh_port"]),
        f"celik@{management['ssh_host']}",
        "cloud-init status --wait >/dev/null && "
        "test -e /var/lib/cloud/instance/boot-finished && "
        "test -e /etc/celikpanel-dns-kill-matrix-ready",
    ]


def wait_for_ssh(plan: dict[str, Any], identity_file: Path, timeout: int) -> None:
    if timeout < 10:
        raise FixtureError("SSH readiness timeout must be at least 10 seconds")
    try:
        info = identity_file.lstat()
    except FileNotFoundError as exc:
        raise FixtureError(f"SSH identity is missing: {identity_file}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise FixtureError("SSH identity must be a regular non-symlink file")
    pending = set(plan["start_order"])
    deadline = time.monotonic() + timeout
    while pending and time.monotonic() < deadline:
        for node_name in tuple(pending):
            result = subprocess.run(
                ssh_command(plan["nodes"][node_name], identity_file),
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                check=False,
            )
            if result.returncode == 0:
                pending.remove(node_name)
        if pending:
            time.sleep(2)
    if pending:
        raise FixtureError(f"SSH readiness timed out for: {', '.join(sorted(pending))}")


def validate_kill_proof(path: Path, cell_id: str) -> dict[str, Any]:
    proof = read_json_regular(path, "kill proof")
    if not isinstance(proof, dict):
        raise FixtureError("kill proof must be a JSON object")
    if (
        proof.get("schema") != KILL_PROOF_SCHEMA
        or proof.get("cell_id") != cell_id
        or proof.get("kill_proven") is not True
        or proof.get("exit_code") != 137
        or not isinstance(proof.get("pid"), int)
        or isinstance(proof.get("pid"), bool)
        or proof["pid"] <= 0
    ):
        raise FixtureError("peer link changes require an exact exit-137 kill proof")
    return proof


def set_peer_link(plan: dict[str, Any], up: bool) -> None:
    for node_name in plan["start_order"]:
        qmp_execute(
            Path(plan["nodes"][node_name]["paths"]["qmp"]),
            "set_link",
            {"name": "peer-link", "up": up},
        )


def _pid_alive(path: Path) -> bool:
    try:
        info = path.lstat()
    except FileNotFoundError:
        return False
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise FixtureError(f"QEMU pidfile must be a regular non-symlink file: {path}")
    try:
        value = int(path.read_text(encoding="ascii").strip())
    except (OSError, ValueError):
        raise FixtureError(f"invalid QEMU pidfile: {path}")
    if value <= 0:
        raise FixtureError(f"invalid QEMU pid: {value}")
    try:
        os.kill(value, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True


def stop_vms(plan: dict[str, Any], timeout: float = 20.0) -> None:
    for node_name in reversed(plan["start_order"]):
        paths = plan["nodes"][node_name]["paths"]
        qmp = Path(paths["qmp"])
        pid = Path(paths["pid"])
        alive = _pid_alive(pid)
        try:
            qmp_info = qmp.lstat()
        except FileNotFoundError:
            qmp_info = None
        if qmp_info is not None:
            if stat.S_ISLNK(qmp_info.st_mode) or not stat.S_ISSOCK(qmp_info.st_mode):
                raise FixtureError(f"QMP path must be a real Unix socket: {qmp}")
            try:
                qmp_execute(qmp, "quit")
            except (ConnectionRefusedError, FileNotFoundError):
                if alive:
                    raise FixtureError(
                        f"{node_name} is alive but its QMP socket is unreachable"
                    )
        elif alive:
            raise FixtureError(f"{node_name} is alive but its QMP socket is missing")
        deadline = time.monotonic() + timeout
        while _pid_alive(pid) and time.monotonic() < deadline:
            time.sleep(0.1)
        if _pid_alive(pid):
            raise FixtureError(
                f"{node_name} did not stop through QMP; refusing to signal or delete"
            )


def teardown_cell(root: Path, cell_id: str) -> None:
    root = validate_work_root(root)
    cell = cell_directory(root, cell_id, must_exist=True)
    plan = load_cell_plan(root, cell_id)
    stop_vms(plan)
    for node_name in plan["start_order"]:
        if _pid_alive(Path(plan["nodes"][node_name]["paths"]["pid"])):
            raise FixtureError("refusing teardown while QEMU is still alive")
    expected_parent = (root / "cells").resolve(strict=True)
    resolved = cell.resolve(strict=True)
    if resolved.parent != expected_parent or resolved == root:
        raise FixtureError("recursive teardown target escaped the validated work root")
    shutil.rmtree(resolved)


def emit(value: Any) -> None:
    print(json.dumps(value, indent=2, sort_keys=True))


def add_common(subparser: argparse.ArgumentParser, *, lock: bool = False) -> None:
    subparser.add_argument("--work-root", required=True, type=Path)
    if lock:
        subparser.add_argument("--lock", type=Path, default=DEFAULT_IMAGE_LOCK)


def add_execute(subparser: argparse.ArgumentParser) -> None:
    subparser.add_argument(
        "--execute",
        action="store_true",
        help="perform the action; without this flag the command is a dry-run",
    )


def parse_args(arguments: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="action", required=True)

    current = subparsers.add_parser("init-root")
    add_common(current)
    add_execute(current)

    current = subparsers.add_parser("fetch")
    add_common(current, lock=True)
    add_execute(current)

    current = subparsers.add_parser("verify-images")
    add_common(current, lock=True)

    current = subparsers.add_parser("prepare")
    add_common(current, lock=True)
    current.add_argument("--cell-id", required=True)
    current.add_argument("--ssh-public-key", required=True, type=Path)
    current.add_argument("--debian-ssh-port", type=int, default=2201)
    current.add_argument("--arch-ssh-port", type=int, default=2202)
    current.add_argument("--peer-port", type=int, default=23053)
    current.add_argument("--iso-tool", choices=("genisoimage", "xorriso"), default="genisoimage")
    current.add_argument("--accel", choices=("kvm", "tcg"), default="kvm")
    current.add_argument("--memory-mb", type=int, default=4096)
    current.add_argument("--cpus", type=int, default=2)
    current.add_argument("--disk-gb", type=int, default=24)
    add_execute(current)

    current = subparsers.add_parser("start")
    add_common(current)
    current.add_argument("--cell-id", required=True)
    add_execute(current)

    current = subparsers.add_parser("wait-ssh")
    add_common(current)
    current.add_argument("--cell-id", required=True)
    current.add_argument("--identity-file", required=True, type=Path)
    current.add_argument("--timeout", type=int, default=300)
    add_execute(current)

    current = subparsers.add_parser("peer-link")
    add_common(current)
    current.add_argument("--cell-id", required=True)
    current.add_argument("--state", required=True, choices=("up", "down"))
    current.add_argument("--kill-proof", required=True, type=Path)
    add_execute(current)

    current = subparsers.add_parser("stop")
    add_common(current)
    current.add_argument("--cell-id", required=True)
    add_execute(current)

    current = subparsers.add_parser("teardown")
    add_common(current)
    current.add_argument("--cell-id", required=True)
    add_execute(current)
    return parser.parse_args(arguments)


def main(arguments: Sequence[str] | None = None) -> int:
    args = parse_args(arguments)
    try:
        if args.action in {
            "prepare",
            "start",
            "wait-ssh",
            "peer-link",
            "stop",
            "teardown",
        }:
            require_linux_qemu_host()
        if args.action == "init-root":
            root = canonical_work_root(args.work_root)
            plan = {
                "action": "init-root",
                "work_root": str(root),
                "marker": str(marker_path(root)),
                "dry_run": not args.execute,
            }
            if args.execute:
                initialize_work_root(root)
            emit(plan)
            return 0

        root = validate_work_root(args.work_root)
        if args.action == "fetch":
            pins = load_image_lock(args.lock)
            plan = fetch_plan(root, pins)
            plan["dry_run"] = not args.execute
            if args.execute:
                execute_fetch(root, pins, plan)
            emit(plan)
            return 0
        if args.action == "verify-images":
            pins = load_image_lock(args.lock)
            paths = verify_images(root, pins)
            emit({"action": "verify-images", "verified": {key: str(value) for key, value in paths.items()}})
            return 0
        if args.action == "prepare":
            pins = load_image_lock(args.lock)
            verify_images(root, pins)
            key = read_ssh_public_key(args.ssh_public_key)
            plan = build_cell_plan(
                root,
                pins,
                args.cell_id,
                key,
                debian_ssh_port=args.debian_ssh_port,
                arch_ssh_port=args.arch_ssh_port,
                peer_port=args.peer_port,
                iso_tool=args.iso_tool,
                accel=args.accel,
                memory_mb=args.memory_mb,
                cpus=args.cpus,
                disk_gb=args.disk_gb,
            )
            if Path(plan["cell_directory"]).exists():
                raise FixtureError("fresh per-cell overlay directory already exists")
            output = dict(plan)
            output["dry_run"] = not args.execute
            if args.execute:
                execute_prepare(plan)
            emit(output)
            return 0
        if args.action == "start":
            plan = load_cell_plan(root, args.cell_id)
            output = {
                "action": "start",
                "cell_id": args.cell_id,
                "dry_run": not args.execute,
                "commands": [
                    plan["nodes"][name]["qemu_command"] for name in plan["start_order"]
                ],
            }
            if args.execute:
                execute_start(plan)
            emit(output)
            return 0
        if args.action == "wait-ssh":
            plan = load_cell_plan(root, args.cell_id)
            commands = {
                name: ssh_command(plan["nodes"][name], args.identity_file)
                for name in plan["start_order"]
            }
            if args.execute:
                wait_for_ssh(plan, args.identity_file, args.timeout)
            emit(
                {
                    "action": "wait-ssh",
                    "cell_id": args.cell_id,
                    "dry_run": not args.execute,
                    "commands": commands,
                }
            )
            return 0
        if args.action == "peer-link":
            plan = load_cell_plan(root, args.cell_id)
            proof = validate_kill_proof(args.kill_proof, args.cell_id)
            if args.execute:
                set_peer_link(plan, args.state == "up")
            emit(
                {
                    "action": "peer-link",
                    "cell_id": args.cell_id,
                    "state": args.state,
                    "dry_run": not args.execute,
                    "proof": {
                        "path": str(args.kill_proof),
                        "pid": proof["pid"],
                        "exit_code": proof["exit_code"],
                    },
                    "qmp_sockets": [
                        plan["nodes"][name]["paths"]["qmp"]
                        for name in plan["start_order"]
                    ],
                }
            )
            return 0
        if args.action == "stop":
            plan = load_cell_plan(root, args.cell_id)
            if args.execute:
                stop_vms(plan)
            emit(
                {
                    "action": "stop",
                    "cell_id": args.cell_id,
                    "dry_run": not args.execute,
                    "method": "QMP quit; no fallback signal",
                }
            )
            return 0
        if args.action == "teardown":
            cell = cell_directory(root, args.cell_id, must_exist=True)
            output = {
                "action": "teardown",
                "cell_id": args.cell_id,
                "target": str(cell),
                "dry_run": not args.execute,
                "warning": "recursive deletion is confined to this validated cell directory",
            }
            if args.execute:
                teardown_cell(root, args.cell_id)
            emit(output)
            return 0
        raise FixtureError(f"unsupported action: {args.action}")
    except (FixtureError, OSError, subprocess.SubprocessError) as exc:
        print(f"fixture: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
