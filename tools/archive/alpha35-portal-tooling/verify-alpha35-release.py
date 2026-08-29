#!/usr/bin/env python3
"""Offline, fail-closed verification for the six alpha.35 release assets."""

from __future__ import annotations

import hashlib
import os
import stat
import tarfile
from pathlib import Path, PurePosixPath


ASSETS = Path(r"C:\tmp\celikpanel-alpha35-release-assets-31c28e9-run32518010435")
VERSION = "v0.1.0-alpha.35"
COMMIT = "31c28e941ddcde5cb0980ac471910bd98f6e1984"
TREE = "4a63878b9500a9db28c7581484be1788d03f692d"
ARCHIVE_SHA = "b588254f58bb6ade0adee22595c0cde1fa8119cfd55db615332bbdb50bc01a70"
ARCHIVE_SIZE = 22_255_771
ROOT = f"celikpanel-{VERSION}"

EXPECTED = {
    f"celikpanel-{VERSION}.tar.gz": (ARCHIVE_SIZE, ARCHIVE_SHA),
    f"celikpanel-{VERSION}.tar.gz.sha256": (
        100,
        "da641e9dda17b8456f339dd0fc4735b69ecf87fc23da426bf9797b199ae4f3de",
    ),
    f"celikpanel-{VERSION}-linux-amd64.release-manifest-v2": (
        332,
        "3240ffa25e0f34be323c74167bbe9022e565a0f7f4f7de55d164c3a1efc8db48",
    ),
    f"celikpanel-{VERSION}-linux-amd64.release-manifest-v2.sig": (
        64,
        "9755eaaf37e8944a07b8f98a4cc9f6ece9418e2c136e86728043f72128fecf2f",
    ),
    f"celikpanel-{VERSION}-linux-amd64.tar.gz": (ARCHIVE_SIZE, ARCHIVE_SHA),
    f"celikpanel-{VERSION}-linux-amd64.tar.gz.sha256": (
        112,
        "3f798069eab3ecf49fc989ebd225e95924de26f2b76fe9d78705eae52bd1015a",
    ),
}

MANIFEST_BYTES = (
    "format=celikpanel-release-manifest-v2\n"
    "sequence=35\n"
    f"version={VERSION}\n"
    f"commit={COMMIT}\n"
    "published_at=2026-08-21T19:05:46Z\n"
    "os=linux\n"
    "arch=amd64\n"
    f"archive=celikpanel-{VERSION}-linux-amd64.tar.gz\n"
    f"archive_sha256={ARCHIVE_SHA}\n"
    f"archive_size={ARCHIVE_SIZE}\n"
).encode("ascii")


def fail(message: str) -> None:
    raise RuntimeError(message)


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def exact_member_bytes(archive: tarfile.TarFile, name: str, maximum: int) -> bytes:
    member = archive.getmember(name)
    if not member.isreg() or member.size > maximum:
        fail(f"invalid required archive member: {name}")
    handle = archive.extractfile(member)
    if handle is None:
        fail(f"unreadable required archive member: {name}")
    data = handle.read(maximum + 1)
    if len(data) != member.size or len(data) > maximum:
        fail(f"bounded archive read failed: {name}")
    return data


def member_digest(archive: tarfile.TarFile, member: tarfile.TarInfo) -> str:
    handle = archive.extractfile(member)
    if handle is None:
        fail(f"unreadable archive member: {member.name}")
    value = hashlib.sha256()
    remaining = member.size
    while remaining:
        chunk = handle.read(min(1024 * 1024, remaining))
        if not chunk:
            fail(f"short archive member read: {member.name}")
        value.update(chunk)
        remaining -= len(chunk)
    if handle.read(1):
        fail(f"overlong archive member read: {member.name}")
    return value.hexdigest()


def verify_assets() -> Path:
    if not ASSETS.is_dir() or ASSETS.is_symlink():
        fail("asset root is not a regular directory")
    entries = list(ASSETS.iterdir())
    if {entry.name for entry in entries} != set(EXPECTED):
        fail("asset inventory is not exactly the expected six files")
    for entry in entries:
        info = entry.lstat()
        if entry.is_symlink() or not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            fail(f"unsafe asset object: {entry.name}")
        expected_size, expected_sha = EXPECTED[entry.name]
        if info.st_size != expected_size or digest(entry) != expected_sha:
            fail(f"asset metadata mismatch: {entry.name}")

    generic = ASSETS / f"celikpanel-{VERSION}.tar.gz"
    platform = ASSETS / f"celikpanel-{VERSION}-linux-amd64.tar.gz"
    if generic.read_bytes() != platform.read_bytes():
        fail("generic and platform archive bytes differ")

    generic_sum = ASSETS / f"celikpanel-{VERSION}.tar.gz.sha256"
    platform_sum = ASSETS / f"celikpanel-{VERSION}-linux-amd64.tar.gz.sha256"
    if generic_sum.read_bytes() != f"{ARCHIVE_SHA}  {generic.name}\n".encode("ascii"):
        fail("generic checksum is not canonical")
    if platform_sum.read_bytes() != f"{ARCHIVE_SHA}  {platform.name}\n".encode("ascii"):
        fail("platform checksum is not canonical")

    manifest = ASSETS / f"celikpanel-{VERSION}-linux-amd64.release-manifest-v2"
    if manifest.read_bytes() != MANIFEST_BYTES:
        fail("signed manifest is not the exact canonical alpha.35 manifest")
    return platform


def verify_archive(path: Path) -> None:
    seen: set[str] = set()
    total_size = 0
    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
        if not members or len(members) > 20_000:
            fail("archive member count is invalid")
        for member in members:
            name = member.name
            pure = PurePosixPath(name)
            if (
                name in seen
                or not name
                or name.startswith("/")
                or chr(92) in name
                or any(part in {"", ".", ".."} for part in pure.parts)
                or pure.parts[0] != ROOT
                or any(ord(char) < 32 or ord(char) == 127 for char in name)
            ):
                fail(f"unsafe archive path: {name!r}")
            seen.add(name)
            if member.pax_headers or getattr(member, "sparse", None):
                fail(f"extended/sparse archive member rejected: {name}")
            if not (member.isdir() or member.isreg()):
                fail(f"non-file archive member rejected: {name}")
            if member.uid != 0 or member.gid != 0:
                fail(f"non-root archive ownership: {name}")
            mode = stat.S_IMODE(member.mode)
            if member.isdir() and mode != 0o755:
                fail(f"invalid directory mode: {name}")
            if member.isreg() and mode not in {0o644, 0o755}:
                fail(f"invalid file mode: {name}")
            if member.size < 0 or member.size > 512 * 1024 * 1024:
                fail(f"invalid member size: {name}")
            total_size += member.size
            if total_size > 1024 * 1024 * 1024:
                fail("expanded archive size exceeds bound")

        required = {
            f"{ROOT}/release.version",
            f"{ROOT}/release.commit",
            f"{ROOT}/release.tree",
            f"{ROOT}/SHA256SUMS",
            f"{ROOT}/bin/panel",
            f"{ROOT}/bin/agent",
            f"{ROOT}/deploy/build-download-portal.sh",
            f"{ROOT}/libexec/get.sh",
        }
        if not required.issubset(seen):
            fail("required provenance members are missing")
        if exact_member_bytes(archive, f"{ROOT}/release.version", 16) != b"1\n":
            fail("release.version mismatch")
        if exact_member_bytes(archive, f"{ROOT}/release.commit", 128) != f"{COMMIT}\n".encode():
            fail("release.commit mismatch")
        if exact_member_bytes(archive, f"{ROOT}/release.tree", 128) != f"{TREE}\n".encode():
            fail("release.tree mismatch")

        sums_name = f"{ROOT}/SHA256SUMS"
        sums_bytes = exact_member_bytes(archive, sums_name, 1024 * 1024)
        try:
            sums_text = sums_bytes.decode("ascii")
        except UnicodeDecodeError as exc:
            raise RuntimeError("SHA256SUMS is not ASCII") from exc
        if not sums_text.endswith("\n"):
            fail("SHA256SUMS lacks a final newline")
        listed: dict[str, str] = {}
        for line in sums_text.splitlines():
            if len(line) < 68 or line[64:68] != "  ./":
                fail("SHA256SUMS line is not canonical")
            expected_sha, relative = line[:64], line[68:]
            if (
                any(char not in "0123456789abcdef" for char in expected_sha)
                or not relative
                or relative in listed
                or PurePosixPath(relative).is_absolute()
                or any(part in {"", ".", ".."} for part in PurePosixPath(relative).parts)
            ):
                fail("SHA256SUMS entry is unsafe or duplicated")
            listed[relative] = expected_sha

        regular = {
            member.name[len(ROOT) + 1 :]: member
            for member in members
            if member.isreg() and member.name != sums_name
        }
        if set(listed) != set(regular):
            fail("SHA256SUMS inventory does not exactly cover regular archive members")
        for relative, expected_sha in listed.items():
            if member_digest(archive, regular[relative]) != expected_sha:
                fail(f"SHA256SUMS digest mismatch: {relative}")

        expected_embedded = {
            f"{ROOT}/deploy/build-download-portal.sh": "ed576fc470d1b9263e9d92931332fcce7ce9a840c08bb74cd67081caea5d25f5",
            f"{ROOT}/libexec/get.sh": "13044fedc5826ec7282802f998508e7080f86837e906ae1bd611a9e40f2c1251",
        }
        for name, expected_sha in expected_embedded.items():
            if hashlib.sha256(exact_member_bytes(archive, name, 128 * 1024)).hexdigest() != expected_sha:
                fail(f"embedded source mismatch: {name}")

        for binary in ("panel", "agent"):
            data = exact_member_bytes(archive, f"{ROOT}/bin/{binary}", 64 * 1024 * 1024)
            if (
                len(data) < 64
                or data[:4] != b"\x7fELF"
                or data[4] != 2
                or data[5] != 1
                or int.from_bytes(data[18:20], "little") != 62
            ):
                fail(f"{binary} is not ELF64 little-endian x86-64")


def main() -> None:
    archive = verify_assets()
    verify_archive(archive)
    print("ALPHA35_SIX_ASSETS_AND_TAR_PROVENANCE_OK")


if __name__ == "__main__":
    main()
