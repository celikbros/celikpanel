#!/usr/bin/env python3
"""Atomically promote one pinned CelikPanel download-portal package.

The program is deliberately release-independent.  It is normally streamed to
``python3 -`` by ``publish-download-portal.ps1`` while the package and the
tracked bounded public verifier remain as immutable evidence in one unique
upload directory.

There is exactly one public verification pass: after the live/stage exchange
and before the old live inode is published as a backup.  Everything after that
pass is checked locally.  A failure rolls the exchange back and leaves the
candidate quarantined; the upload is never removed.
"""

from __future__ import annotations

import argparse
import ctypes
import hashlib
import json
import math
import os
import re
import secrets
import signal
import stat
import sys
import tarfile
import time
import types
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Iterable

try:
    import fcntl
except ImportError:  # pragma: no cover - the production mutator is Linux-only
    fcntl = None


SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
SEMVER_RE = re.compile(
    r"^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    r"(?:-(?:[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)
SAFE_SEGMENT_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]*$")
ARCHIVE_ROOT = "portal"
MAX_MEMBERS = 10_000
MAX_UNPACKED_BYTES = 4 * 1024 * 1024 * 1024
READ_CHUNK = 1024 * 1024
AT_FDCWD = -100
RENAME_NOREPLACE = 1
RENAME_EXCHANGE = 2
SUCCESS_MARKER = "CELIKPANEL_DOWNLOAD_PORTAL_PUBLISHED"
TRANSACTION_SIGNALS = (signal.SIGHUP, signal.SIGINT, signal.SIGTERM)


class PromotionError(RuntimeError):
    pass


@dataclass(frozen=True)
class PinnedFile:
    path: Path
    descriptor: int
    device: int
    inode: int
    size: int
    sha256: str


@dataclass(frozen=True)
class TreeIdentity:
    device: int
    inode: int
    inventory: tuple[tuple[object, ...], ...]


@dataclass
class TransactionState:
    stage: Path | None = None
    backup: Path | None = None
    exchanged: bool = False
    backup_published: bool = False


def fail(message: str) -> None:
    raise PromotionError(message)


def valid_version(value: str, label: str) -> str:
    if not SEMVER_RE.fullmatch(value):
        fail(f"invalid {label}: {value!r}")
    for identifier in value.partition("-")[2].split("."):
        if identifier.isdigit() and len(identifier) > 1 and identifier.startswith("0"):
            fail(f"invalid {label}: numeric prerelease identifiers cannot have leading zeroes")
    return value


def require_absolute(path: Path, label: str) -> Path:
    if not path.is_absolute():
        fail(f"{label} must be an absolute path")
    return Path(os.path.normpath(path))


def lstat_directory(path: Path, label: str) -> os.stat_result:
    try:
        value = os.lstat(path)
    except OSError as exc:
        fail(f"cannot inspect {label} {path}: {exc}")
    if stat.S_ISLNK(value.st_mode) or not stat.S_ISDIR(value.st_mode):
        fail(f"{label} must be a non-symlink directory: {path}")
    return value


def assert_no_symlink_components(path: Path, label: str) -> None:
    current = Path(path.anchor)
    for part in path.parts[1:]:
        current /= part
        try:
            value = os.lstat(current)
        except OSError as exc:
            fail(f"cannot inspect {label} path component {current}: {exc}")
        if stat.S_ISLNK(value.st_mode):
            fail(f"{label} contains a symlink component: {current}")


def validate_layout(args: argparse.Namespace) -> None:
    args.root = require_absolute(args.root, "root")
    args.live = require_absolute(args.live, "live")
    args.backups = require_absolute(args.backups, "backups")
    args.lock = require_absolute(args.lock, "lock")
    args.upload_dir = require_absolute(args.upload_dir, "upload directory")
    args.package = require_absolute(args.package, "package")
    args.verifier = require_absolute(args.verifier, "verifier")
    if args.live.parent != args.root or args.backups.parent != args.root:
        fail("live and backups must be direct children of root")
    if args.lock.parent != args.root or args.upload_dir.parent != args.root:
        fail("lock and upload directory must be direct children of root")
    if args.package.parent != args.upload_dir or args.verifier.parent != args.upload_dir:
        fail("package and verifier must be direct children of the pinned upload directory")
    if len({args.root, args.live, args.backups, args.lock, args.upload_dir, args.package, args.verifier}) != 7:
        fail("portal paths must be distinct")
    for path, label in (
        (args.root, "root"),
        (args.live, "live"),
        (args.backups, "backups"),
        (args.upload_dir, "upload directory"),
    ):
        assert_no_symlink_components(path, label)
    stats = [
        lstat_directory(args.root, "root"),
        lstat_directory(args.live, "live"),
        lstat_directory(args.backups, "backups"),
        lstat_directory(args.upload_dir, "upload directory"),
    ]
    if len({value.st_dev for value in stats}) != 1:
        fail("root, live, backups, and upload directory must be on one filesystem")


def open_pinned_regular(path: Path, expected_size: int, expected_sha: str, label: str) -> PinnedFile:
    if expected_size <= 0:
        fail(f"{label} size must be positive")
    if not SHA256_RE.fullmatch(expected_sha):
        fail(f"{label} SHA-256 is invalid")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as exc:
        fail(f"cannot open pinned {label} {path}: {exc}")
    try:
        value = os.fstat(descriptor)
        current = os.lstat(path)
        if not stat.S_ISREG(value.st_mode) or stat.S_ISLNK(current.st_mode):
            fail(f"{label} must be a regular non-symlink file")
        if value.st_nlink != 1:
            fail(f"{label} must have exactly one hard link")
        if (value.st_dev, value.st_ino) != (current.st_dev, current.st_ino):
            fail(f"{label} path changed while it was opened")
        if value.st_size != expected_size:
            fail(f"{label} size differs from its pin")
        digest = hashlib.sha256()
        while True:
            chunk = os.read(descriptor, READ_CHUNK)
            if not chunk:
                break
            digest.update(chunk)
        if digest.hexdigest() != expected_sha:
            fail(f"{label} SHA-256 differs from its pin")
        after = os.fstat(descriptor)
        if (after.st_dev, after.st_ino, after.st_size) != (value.st_dev, value.st_ino, value.st_size):
            fail(f"{label} changed while it was read")
        os.lseek(descriptor, 0, os.SEEK_SET)
        return PinnedFile(path, descriptor, value.st_dev, value.st_ino, value.st_size, expected_sha)
    except Exception:
        os.close(descriptor)
        raise


def reverify_pinned_path(pinned: PinnedFile, label: str) -> None:
    current = os.lstat(pinned.path)
    opened = os.fstat(pinned.descriptor)
    if stat.S_ISLNK(current.st_mode) or not stat.S_ISREG(current.st_mode):
        fail(f"pinned {label} path is no longer a regular file")
    if current.st_nlink != 1 or opened.st_nlink != 1:
        fail(f"pinned {label} acquired another hard link")
    identity = (pinned.device, pinned.inode, pinned.size)
    if (current.st_dev, current.st_ino, current.st_size) != identity:
        fail(f"pinned {label} path identity changed")
    if (opened.st_dev, opened.st_ino, opened.st_size) != identity:
        fail(f"opened {label} identity changed")


def acquire_lock(path: Path) -> int:
    if fcntl is None or os.name != "posix":
        fail("portal promotion requires Linux flock and renameat2")
    assert_no_symlink_components(path.parent, "lock parent")
    try:
        descriptor = os.open(path, os.O_RDWR | getattr(os, "O_NOFOLLOW", 0))
    except OSError as exc:
        fail(f"cannot open deployment lock: {exc}")
    try:
        value = os.fstat(descriptor)
        current = os.lstat(path)
        if not stat.S_ISREG(value.st_mode) or stat.S_ISLNK(current.st_mode):
            fail("deployment lock must be a regular non-symlink file")
        if value.st_nlink != 1 or value.st_size != 0:
            fail("deployment lock must have one link and zero bytes")
        if (value.st_dev, value.st_ino) != (current.st_dev, current.st_ino):
            fail("deployment lock path changed while it was opened")
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            fail("another portal transaction holds the deployment lock")
        return descriptor
    except Exception:
        os.close(descriptor)
        raise


def metadata_inventory(root: Path) -> tuple[tuple[object, ...], ...]:
    """Capture a bounded-I/O tree pin without rereading historical bodies.

    Link count and ctime are intentionally excluded because preserving history
    with hard links changes those values.  Path, inode, size, mtime, ownership,
    and mode still detect replacement or mutation under the deployment lock.
    """
    lstat_directory(root, "inventory root")
    result: list[tuple[object, ...]] = []
    pending = [root]
    while pending:
        directory = pending.pop()
        before = os.lstat(directory)
        for child in sorted(directory.iterdir(), key=lambda item: item.name):
            value = os.lstat(child)
            relative = child.relative_to(root).as_posix()
            if stat.S_ISDIR(value.st_mode) and not stat.S_ISLNK(value.st_mode):
                kind = "d"
                pending.append(child)
            elif stat.S_ISREG(value.st_mode) and not stat.S_ISLNK(value.st_mode):
                kind = "f"
            else:
                fail(f"unsupported live object: {child}")
            result.append((
                relative,
                kind,
                value.st_dev,
                value.st_ino,
                stat.S_IMODE(value.st_mode),
                value.st_uid,
                value.st_gid,
                value.st_size,
                value.st_mtime_ns,
            ))
        after = os.lstat(directory)
        if (before.st_dev, before.st_ino, before.st_mtime_ns) != (
            after.st_dev,
            after.st_ino,
            after.st_mtime_ns,
        ):
            fail(f"live directory changed during inventory: {directory}")
    return tuple(sorted(result))


def tree_identity(path: Path) -> TreeIdentity:
    value = lstat_directory(path, "tree")
    return TreeIdentity(value.st_dev, value.st_ino, metadata_inventory(path))


def assert_tree_identity(path: Path, expected: TreeIdentity, label: str) -> None:
    value = lstat_directory(path, label)
    if (value.st_dev, value.st_ino) != (expected.device, expected.inode):
        fail(f"{label} inode changed")
    if metadata_inventory(path) != expected.inventory:
        fail(f"{label} inventory changed")


def safe_archive_relative(name: str) -> str:
    if (
        "\x00" in name
        or "\n" in name
        or "\r" in name
        or "\\" in name
        or name.startswith("/")
    ):
        fail(f"unsafe archive member name: {name!r}")
    raw = name[:-1] if name.endswith("/") else name
    if raw == ARCHIVE_ROOT:
        return ""
    prefix = ARCHIVE_ROOT + "/"
    if not raw.startswith(prefix):
        fail(f"archive member is outside {ARCHIVE_ROOT}/: {name!r}")
    parts = raw[len(prefix):].split("/")
    if any(part in {"", ".", ".."} for part in parts):
        fail(f"unsafe archive member path: {name!r}")
    canonical = prefix + "/".join(parts)
    if raw != canonical:
        fail(f"archive member name is not canonical: {name!r}")
    return "/".join(parts)


def hash_path(path: Path, expected: os.stat_result) -> str:
    digest = hashlib.sha256()
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    try:
        opened = os.fstat(descriptor)
        identity = (expected.st_dev, expected.st_ino, expected.st_size, expected.st_mtime_ns)
        if not stat.S_ISREG(opened.st_mode) or (
            opened.st_dev,
            opened.st_ino,
            opened.st_size,
            opened.st_mtime_ns,
        ) != identity:
            fail(f"candidate file changed before hashing: {path}")
        while True:
            chunk = os.read(descriptor, READ_CHUNK)
            if not chunk:
                break
            digest.update(chunk)
        after = os.fstat(descriptor)
        current = os.lstat(path)
        for value in (after, current):
            if stat.S_ISLNK(value.st_mode) or (
                value.st_dev,
                value.st_ino,
                value.st_size,
                value.st_mtime_ns,
            ) != identity:
                fail(f"candidate file changed while hashing: {path}")
        return digest.hexdigest()
    finally:
        os.close(descriptor)


def content_inventory(root: Path) -> tuple[tuple[str, str, int, str], ...]:
    result: list[tuple[str, str, int, str]] = []
    pending = [root]
    while pending:
        directory = pending.pop()
        for child in sorted(directory.iterdir(), key=lambda item: item.name):
            value = os.lstat(child)
            relative = child.relative_to(root).as_posix()
            if stat.S_ISDIR(value.st_mode) and not stat.S_ISLNK(value.st_mode):
                result.append((relative, "d", 0, ""))
                pending.append(child)
            elif stat.S_ISREG(value.st_mode) and not stat.S_ISLNK(value.st_mode):
                result.append((relative, "f", value.st_size, hash_path(child, value)))
            else:
                fail(f"unsupported candidate object: {child}")
    return tuple(sorted(result))


def extract_candidate(package: PinnedFile, stage: Path) -> tuple[tuple[str, str, int, str], ...]:
    expected: list[tuple[str, str, int, str]] = []
    seen: set[str] = set()
    total = 0
    root_seen = False
    os.mkdir(stage, 0o700)
    with os.fdopen(os.dup(package.descriptor), "rb", closefd=True) as raw:
        os.lseek(raw.fileno(), 0, os.SEEK_SET)
        try:
            archive = tarfile.open(fileobj=raw, mode="r:gz")
        except (tarfile.TarError, OSError) as exc:
            fail(f"cannot open portal package: {exc}")
        with archive:
            members = archive.getmembers()
            if not members or len(members) > MAX_MEMBERS:
                fail("portal package member count is outside the accepted range")
            for member in members:
                relative = safe_archive_relative(member.name)
                if relative in seen:
                    fail(f"duplicate archive member: {member.name}")
                seen.add(relative)
                if relative == "":
                    if not member.isdir():
                        fail("archive root must be a directory")
                    root_seen = True
                    continue
                target = stage.joinpath(*PurePosixPath(relative).parts)
                if member.isdir():
                    try:
                        os.mkdir(target, 0o700)
                    except FileNotFoundError:
                        fail(f"archive directory parent was not declared first: {relative}")
                    except FileExistsError:
                        fail(f"archive directory already exists: {relative}")
                    expected.append((relative, "d", 0, ""))
                    continue
                if not member.isreg():
                    fail(f"archive contains a link or special object: {relative}")
                total += member.size
                if total > min(MAX_UNPACKED_BYTES, max(package.size * 8, 64 * 1024 * 1024)):
                    fail("portal package exceeds the unpacked byte limit")
                source = archive.extractfile(member)
                if source is None:
                    fail(f"cannot read archive member: {relative}")
                digest = hashlib.sha256()
                written = 0
                try:
                    descriptor = os.open(
                        target,
                        os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0),
                        0o600,
                    )
                except OSError as exc:
                    fail(f"cannot create candidate file {relative}: {exc}")
                try:
                    with os.fdopen(descriptor, "wb", closefd=True) as destination:
                        while True:
                            chunk = source.read(READ_CHUNK)
                            if not chunk:
                                break
                            written += len(chunk)
                            if written > member.size:
                                fail(f"archive member grew while extracting: {relative}")
                            digest.update(chunk)
                            destination.write(chunk)
                        destination.flush()
                        os.fsync(destination.fileno())
                finally:
                    source.close()
                if written != member.size:
                    fail(f"archive member size mismatch: {relative}")
                expected.append((relative, "f", written, digest.hexdigest()))
    if not root_seen:
        fail(f"portal package does not contain its exact {ARCHIVE_ROOT}/ root")
    actual = content_inventory(stage)
    if actual != tuple(sorted(expected)):
        fail("extracted candidate differs from the exact archive inventory")
    return actual


def normalize_candidate(root: Path, archive_inventory: tuple[tuple[str, str, int, str], ...]) -> None:
    for relative, kind, _size, _digest in archive_inventory:
        path = root.joinpath(*PurePosixPath(relative).parts)
        if kind == "d":
            os.chmod(path, 0o755, follow_symlinks=False)
        else:
            mode = 0o755 if relative == "get.sh" else 0o644
            os.chmod(path, mode, follow_symlinks=False)
    os.chmod(root, 0o755, follow_symlinks=False)


def copy_history_with_hardlinks(source: Path, destination: Path) -> None:
    source_value = lstat_directory(source, "historical release")
    source_fd = os.open(
        source, os.O_RDONLY | os.O_DIRECTORY | getattr(os, "O_NOFOLLOW", 0)
    )
    try:
        opened_source = os.fstat(source_fd)
        if (opened_source.st_dev, opened_source.st_ino) != (
            source_value.st_dev,
            source_value.st_ino,
        ):
            fail(f"historical release path changed while opening: {source}")
        os.mkdir(destination, stat.S_IMODE(source_value.st_mode))
        destination_fd = os.open(
            destination, os.O_RDONLY | os.O_DIRECTORY | getattr(os, "O_NOFOLLOW", 0)
        )
        try:
            _copy_history_directory(source_fd, destination_fd, source)
        finally:
            os.close(destination_fd)
        after_source = os.fstat(source_fd)
        if (
            after_source.st_dev,
            after_source.st_ino,
            after_source.st_mtime_ns,
        ) != (
            opened_source.st_dev,
            opened_source.st_ino,
            opened_source.st_mtime_ns,
        ):
            fail(f"historical release changed while preserving: {source}")
    finally:
        os.close(source_fd)


def _copy_history_directory(source_fd: int, destination_fd: int, display: Path) -> None:
    with os.scandir(source_fd) as iterator:
        names = sorted(entry.name for entry in iterator)
    for name in names:
        before = os.stat(name, dir_fd=source_fd, follow_symlinks=False)
        if stat.S_ISDIR(before.st_mode):
            child_source_fd = os.open(
                name,
                os.O_RDONLY | os.O_DIRECTORY | getattr(os, "O_NOFOLLOW", 0),
                dir_fd=source_fd,
            )
            try:
                opened = os.fstat(child_source_fd)
                if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
                    fail(f"historical directory changed while opening: {display / name}")
                os.mkdir(name, stat.S_IMODE(before.st_mode), dir_fd=destination_fd)
                child_destination_fd = os.open(
                    name,
                    os.O_RDONLY | os.O_DIRECTORY | getattr(os, "O_NOFOLLOW", 0),
                    dir_fd=destination_fd,
                )
                try:
                    _copy_history_directory(
                        child_source_fd, child_destination_fd, display / name
                    )
                finally:
                    os.close(child_destination_fd)
                after = os.fstat(child_source_fd)
                if (
                    after.st_dev,
                    after.st_ino,
                    after.st_mtime_ns,
                ) != (
                    opened.st_dev,
                    opened.st_ino,
                    opened.st_mtime_ns,
                ):
                    fail(f"historical directory changed while linking: {display / name}")
            finally:
                os.close(child_source_fd)
        elif stat.S_ISREG(before.st_mode):
            source_file_fd = os.open(
                name, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0), dir_fd=source_fd
            )
            try:
                opened = os.fstat(source_file_fd)
                identity = (
                    before.st_dev,
                    before.st_ino,
                    before.st_size,
                    before.st_mtime_ns,
                )
                if (
                    opened.st_dev,
                    opened.st_ino,
                    opened.st_size,
                    opened.st_mtime_ns,
                ) != identity:
                    fail(f"historical file changed while opening: {display / name}")
                os.link(
                    name,
                    name,
                    src_dir_fd=source_fd,
                    dst_dir_fd=destination_fd,
                    follow_symlinks=False,
                )
                linked = os.stat(name, dir_fd=destination_fd, follow_symlinks=False)
                after = os.fstat(source_file_fd)
                for value in (linked, after):
                    if not stat.S_ISREG(value.st_mode) or (
                        value.st_dev,
                        value.st_ino,
                        value.st_size,
                        value.st_mtime_ns,
                    ) != identity:
                        fail(f"historical hard-link identity mismatch: {display / name}")
            finally:
                os.close(source_file_fd)
        else:
            fail(f"historical release contains a link or special object: {display / name}")


def preserve_historical_releases(live: Path, stage: Path, target_version: str) -> None:
    source = live / "releases"
    destination = stage / "releases"
    lstat_directory(source, "live releases")
    lstat_directory(destination, "candidate releases")
    target = destination / target_version
    if target.is_symlink() or not target.is_dir():
        fail("candidate does not contain the target release directory")
    if (source / target_version).exists() or (source / target_version).is_symlink():
        fail("target release already exists in the live tree")
    for child in sorted(source.iterdir(), key=lambda item: item.name):
        value = os.lstat(child)
        if stat.S_ISDIR(value.st_mode) and not stat.S_ISLNK(value.st_mode):
            if not SAFE_SEGMENT_RE.fullmatch(child.name):
                fail(f"unsafe historical release directory name: {child.name!r}")
            target_child = destination / child.name
            if target_child.exists() or target_child.is_symlink():
                fail(f"candidate unexpectedly contains historical release {child.name}")
            copy_history_with_hardlinks(child, target_child)
        elif stat.S_ISREG(value.st_mode) and not stat.S_ISLNK(value.st_mode):
            if child.name not in {"index.json", "latest.json", "latest.txt"}:
                fail(f"unexpected file in live releases root: {child.name}")
        else:
            fail(f"unsupported object in live releases root: {child}")


def fsync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | getattr(os, "O_NOFOLLOW", 0))
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def fsync_tree_directories(root: Path) -> None:
    directories = [root]
    for directory, names, files in os.walk(root, topdown=False, followlinks=False):
        current = Path(directory)
        for name in [*names, *files]:
            child = current / name
            value = os.lstat(child)
            if stat.S_ISLNK(value.st_mode) or not (
                stat.S_ISDIR(value.st_mode) or stat.S_ISREG(value.st_mode)
            ):
                fail(f"link or special object found while syncing candidate: {child}")
        directories.append(current)
    for path in dict.fromkeys(directories):
        fsync_directory(path)


def renameat2(source: Path, destination: Path, flags: int) -> None:
    libc = ctypes.CDLL(None, use_errno=True)
    function = getattr(libc, "renameat2", None)
    if function is None:
        fail("libc does not provide renameat2")
    function.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    function.restype = ctypes.c_int
    result = function(
        AT_FDCWD,
        os.fsencode(source),
        AT_FDCWD,
        os.fsencode(destination),
        flags,
    )
    if result != 0:
        error = ctypes.get_errno()
        fail(f"renameat2({source}, {destination}, {flags}) failed: {os.strerror(error)}")


def read_exact_verifier(pinned: PinnedFile) -> bytes:
    os.lseek(pinned.descriptor, 0, os.SEEK_SET)
    chunks = []
    remaining = pinned.size
    while remaining:
        chunk = os.read(pinned.descriptor, min(READ_CHUNK, remaining))
        if not chunk:
            fail("pinned verifier ended early")
        chunks.append(chunk)
        remaining -= len(chunk)
    if os.read(pinned.descriptor, 1):
        fail("pinned verifier grew while it was read")
    return b"".join(chunks)


def import_pinned_verifier(pinned: PinnedFile):
    source = read_exact_verifier(pinned)
    if hashlib.sha256(source).hexdigest() != pinned.sha256:
        fail("pinned verifier bytes changed before import")
    name = f"celikpanel_portal_verifier_{pinned.sha256[:16]}_{os.getpid()}"
    module = types.ModuleType(name)
    module.__file__ = str(pinned.path)
    module.__package__ = ""
    sys.modules[name] = module
    try:
        code = compile(source, str(pinned.path), "exec", dont_inherit=True)
        exec(code, module.__dict__)
    except Exception:
        sys.modules.pop(name, None)
        raise
    if not callable(getattr(module, "load_target", None)) or not callable(
        getattr(module, "verify_public_portal", None)
    ):
        fail("pinned verifier does not expose the required interface")
    return module


class PublicDeadline:
    def __init__(self, seconds: float):
        self.seconds = seconds
        self.previous_handler = None

    def __enter__(self):
        if not hasattr(signal, "setitimer"):
            fail("platform cannot enforce the public verification deadline")
        self.previous_handler = signal.getsignal(signal.SIGALRM)

        def expired(_signum, _frame):
            raise TimeoutError("bounded public verification deadline expired")

        signal.signal(signal.SIGALRM, expired)
        signal.setitimer(signal.ITIMER_REAL, self.seconds)
        return self

    def __exit__(self, _kind, _value, _traceback):
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, self.previous_handler)


def unique_path(parent: Path, prefix: str) -> Path:
    for _ in range(32):
        stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
        candidate = parent / f"{prefix}.{stamp}.{os.getpid()}.{secrets.token_hex(8)}"
        if not candidate.exists() and not candidate.is_symlink():
            return candidate
    fail(f"cannot allocate a unique path below {parent}")


def read_latest_version(live: Path) -> str:
    path = live / "releases" / "latest.txt"
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    try:
        value = os.fstat(descriptor)
        current = os.lstat(path)
        if (
            not stat.S_ISREG(value.st_mode)
            or stat.S_ISLNK(current.st_mode)
            or value.st_size > 256
            or (value.st_dev, value.st_ino, value.st_size)
            != (current.st_dev, current.st_ino, current.st_size)
        ):
            fail("live latest.txt is unsafe")
        raw = os.read(descriptor, 257)
        after = os.fstat(descriptor)
        if (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns) != (
            value.st_dev,
            value.st_ino,
            value.st_size,
            value.st_mtime_ns,
        ):
            fail("live latest.txt changed while it was read")
    finally:
        os.close(descriptor)
    try:
        text = raw.decode("ascii")
    except UnicodeError as exc:
        fail(f"cannot decode live latest.txt: {exc}")
    if not text.endswith("\n") or text.count("\n") != 1:
        fail("live latest.txt is not canonical")
    return text[:-1]


@contextmanager
def transaction_signal_handlers():
    previous = {}

    def interrupted(signum, _frame):
        raise PromotionError(f"transaction interrupted by signal {signum}")

    for signum in TRANSACTION_SIGNALS:
        previous[signum] = signal.getsignal(signum)
        signal.signal(signum, interrupted)
    try:
        yield
    finally:
        for signum, handler in previous.items():
            signal.signal(signum, handler)


@contextmanager
def block_transaction_signals():
    if not hasattr(signal, "pthread_sigmask"):
        fail("platform cannot protect atomic transaction state from signals")
    previous = signal.pthread_sigmask(signal.SIG_BLOCK, TRANSACTION_SIGNALS)
    try:
        yield
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous)


def verify_backup_local(backup: Path, expected: TreeIdentity) -> None:
    assert_tree_identity(backup, expected, "published old-live backup")


def assert_live_restored(live: Path, expected: TreeIdentity) -> None:
    assert_tree_identity(live, expected, "rolled-back live tree")


def rollback_transaction(args: argparse.Namespace, state: TransactionState, old_live: TreeIdentity) -> None:
    try:
        if state.backup_published:
            assert state.backup is not None
            with block_transaction_signals():
                renameat2(args.live, state.backup, RENAME_EXCHANGE)
            assert_live_restored(args.live, old_live)
            failed = unique_path(args.root, f".failed-portal-{args.target_version}")
            with block_transaction_signals():
                renameat2(state.backup, failed, RENAME_NOREPLACE)
            state.backup = failed
            state.backup_published = False
        elif state.exchanged:
            assert state.stage is not None
            with block_transaction_signals():
                renameat2(args.live, state.stage, RENAME_EXCHANGE)
            assert_live_restored(args.live, old_live)
        fsync_directory(args.root)
        fsync_directory(args.backups)
        state.exchanged = False
    except Exception as rollback_error:
        raise PromotionError(f"ROLLBACK FAILED; live outcome is unknown: {rollback_error}") from rollback_error


def run_transaction(args: argparse.Namespace) -> dict:
    valid_version(args.previous_version, "previous version")
    valid_version(args.target_version, "target version")
    if args.previous_version == args.target_version:
        fail("previous and target versions must differ")
    validate_layout(args)
    lock_descriptor = acquire_lock(args.lock)
    package: PinnedFile | None = None
    verifier_file: PinnedFile | None = None
    state = TransactionState()
    old_live: TreeIdentity | None = None
    candidate: TreeIdentity | None = None
    signal_context = transaction_signal_handlers()
    signal_context.__enter__()
    try:
        # All mutable state below is observed while holding the deployment lock.
        validate_layout(args)
        if read_latest_version(args.live) != args.previous_version:
            fail("live portal does not publish the pinned previous version")
        old_live = tree_identity(args.live)
        package = open_pinned_regular(args.package, args.package_size, args.package_sha256, "package")
        verifier_file = open_pinned_regular(
            args.verifier, args.verifier_size, args.verifier_sha256, "public verifier"
        )
        if package.device != old_live.device or verifier_file.device != old_live.device:
            fail("pinned upload files and live portal must be on one filesystem")
        verifier = import_pinned_verifier(verifier_file)

        state.stage = unique_path(args.root, f".stage-portal-{args.target_version}")
        archive_inventory = extract_candidate(package, state.stage)
        normalize_candidate(state.stage, archive_inventory)
        target = verifier.load_target(state.stage)
        if getattr(target, "version", None) != args.target_version:
            fail("candidate selectors do not publish the pinned target version")
        preserve_historical_releases(args.live, state.stage, args.target_version)

        # Hard-link preservation changes nlink/ctime only, which the inventory
        # deliberately excludes.  Any path/inode/size/mtime change fails here.
        assert_tree_identity(args.live, old_live, "old live tree before exchange")
        reverify_pinned_path(package, "package")
        reverify_pinned_path(verifier_file, "public verifier")
        fsync_tree_directories(state.stage)
        fsync_directory(args.root)
        candidate = tree_identity(state.stage)

        with block_transaction_signals():
            renameat2(args.live, state.stage, RENAME_EXCHANGE)
            state.exchanged = True
        assert_tree_identity(args.live, candidate, "exchanged candidate live tree")
        assert_tree_identity(state.stage, old_live, "exchanged old live tree")

        # THE ONLY PUBLIC PASS IN THE TRANSACTION.
        with PublicDeadline(args.public_total_timeout):
            public_result = verifier.verify_public_portal(
                args.live,
                args.public_base_url,
                args.target_version,
                timeout=args.public_timeout,
            )
        request_limit = getattr(verifier, "HARD_REQUEST_LIMIT", None)
        if (
            not isinstance(public_result, dict)
            or public_result.get("status") != "ok"
            or not isinstance(request_limit, int)
            or request_limit <= 0
            or request_limit > 15
            or not isinstance(public_result.get("requests"), int)
            or not 0 < public_result["requests"] <= request_limit
            or public_result.get("request_limit") != request_limit
            or public_result.get("archive_gets") != 1
        ):
            fail("bounded public verifier did not return an explicit ok result")

        state.backup = unique_path(args.backups, f"httpdocs.before-{args.target_version}")
        with block_transaction_signals():
            renameat2(state.stage, state.backup, RENAME_NOREPLACE)
            state.backup_published = True
        fsync_directory(args.backups)
        fsync_directory(args.root)

        # No verifier call and no network I/O is allowed after backup publish.
        verify_backup_local(state.backup, old_live)
        if read_latest_version(args.live) != args.target_version:
            fail("live target selector changed after backup publication")
        reverify_pinned_path(package, "package")
        reverify_pinned_path(verifier_file, "public verifier")
        result = {
            "status": "committed",
            "previous_version": args.previous_version,
            "target_version": args.target_version,
            "backup": str(state.backup),
            "old_live_device": old_live.device,
            "old_live_inode": old_live.inode,
            "public_verification": public_result,
            "upload_retained": str(args.upload_dir),
        }
        return result
    except Exception:
        if old_live is not None and state.exchanged:
            rollback_transaction(args, state, old_live)
        raise
    finally:
        # A pending termination signal cannot raise between a committed result
        # and handler restoration.  It is delivered only after the old handler
        # is restored and all transaction descriptors/locks are released.
        with block_transaction_signals():
            if package is not None:
                os.close(package.descriptor)
            if verifier_file is not None:
                os.close(verifier_file.descriptor)
            signal_context.__exit__(None, None, None)
            try:
                fcntl.flock(lock_descriptor, fcntl.LOCK_UN)
            finally:
                os.close(lock_descriptor)


def parse_args(argv: Iterable[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="atomically promote a pinned download portal package")
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--live", required=True, type=Path)
    parser.add_argument("--backups", required=True, type=Path)
    parser.add_argument("--lock", required=True, type=Path)
    parser.add_argument("--upload-dir", required=True, type=Path)
    parser.add_argument("--package", required=True, type=Path)
    parser.add_argument("--package-size", required=True, type=int)
    parser.add_argument("--package-sha256", required=True)
    parser.add_argument("--verifier", required=True, type=Path)
    parser.add_argument("--verifier-size", required=True, type=int)
    parser.add_argument("--verifier-sha256", required=True)
    parser.add_argument("--previous-version", required=True)
    parser.add_argument("--target-version", required=True)
    parser.add_argument("--public-base-url", required=True)
    parser.add_argument("--public-timeout", type=float, default=30.0)
    parser.add_argument("--public-total-timeout", type=float, default=180.0)
    args = parser.parse_args(argv)
    if not 0 < args.public_timeout <= 60:
        parser.error("--public-timeout must be greater than zero and no more than 60 seconds")
    if not 0 < args.public_total_timeout <= 600:
        parser.error("--public-total-timeout must be greater than zero and no more than 600 seconds")
    return args


def main(argv: Iterable[str] | None = None) -> int:
    try:
        result = run_transaction(parse_args(argv))
    except Exception as exc:
        print(f"portal promotion failed: {exc}", file=sys.stderr)
        return 1
    # The transaction has returned only after signal handlers were restored,
    # descriptors were closed, and the deployment lock was released.
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    print(SUCCESS_MARKER)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
