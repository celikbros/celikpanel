#!/usr/bin/env python3
"""Fail-closed, atomic CelikPanel alpha.35 portal promotion.

Prepared for the immutable package that the operator uploads to ``UPLOAD``.
This program is intentionally self-contained and is meant to be run by the
operator on the portal host only after a fresh, read-only preflight confirms the
constants below. It never edits the live tree in place: a validated sibling is
exchanged with it atomically.
"""

from __future__ import annotations

import ctypes
import hashlib
import json
import os
import secrets
import shutil
import signal
import stat
import sys
import tarfile
import tempfile
import time
import urllib.request
from pathlib import Path, PurePosixPath

try:  # Allows offline parser tests on Windows; main() still requires Linux.
    import fcntl
except ImportError:  # pragma: no cover - Linux-only execution primitive
    fcntl = None


ROOT = Path("/var/www/vhosts/celikpanel.net")
LIVE = ROOT / "httpdocs"
BACKUPS = ROOT / "portal-backups"
UPLOAD_DIR = ROOT / ".upload-alpha35-user-20260821-31c28e9-a288f9a1be38"
UPLOAD = UPLOAD_DIR / "portal.tar.gz"
LOCK = ROOT / ".portal-deploy.lock"
ARCHIVE_ROOT = "portal"

VERSION = "v0.1.0-alpha.35"
PREVIOUS = "v0.1.0-alpha.34"
SEQUENCE = "35"
COMMIT = "31c28e941ddcde5cb0980ac471910bd98f6e1984"
PACKAGE_SIZE = 44_457_897
PACKAGE_SHA = "a288f9a1be381552f59a35a41c666d2e1aee989e72715c78259d10c9b6016256"
ARCHIVE_SIZE = 22_255_771
ARCHIVE_SHA = "b588254f58bb6ade0adee22595c0cde1fa8119cfd55db615332bbdb50bc01a70"

OLD_VERSIONS = frozenset(
    {
        PREVIOUS,
        "v0.1.0-alpha.33",
        "v0.1.0-alpha.32",
        "v0.1.0-alpha.31",
        "v0.1.0-alpha.30",
        *(f"v0.1.0-alpha.{number}" for number in range(9, 29)),
    }
)
RELEASE_SELECTORS = frozenset({"index.json", "latest.json", "latest.txt"})
PRESERVED_UPLOAD_DIRS = (
    ROOT / ".upload-alpha13-20260812T132500Z",
    ROOT / ".upload-alpha14-20260813T0825Z",
    ROOT / ".upload-alpha16-20260813T1919Z",
)

# path -> (kind, byte size, sha256). Directories use size zero and an empty hash.
# This is the complete normalized inventory of the exact portal package.
EXPECTED_ARCHIVE_OBJECTS = {
    ".htaccess": ("f", 851, "465bfcb300eee6ad9687fb450cc48f9c90c6fc6edeced6276821f4f944ef20d6"),
    ".well-known": ("d", 0, ""),
    ".well-known/security.txt": ("f", 283, "9e3262a7f9c084eb75e068a0ca83d86563d285bdbee395fdeeebf3dd4d661bee"),
    "assets": ("d", 0, ""),
    "assets/site.css": ("f", 38896, "dc1e751bca2529538926217ceaeeb99bbd93bdbb3dfaa218e52ae1db6e32210b"),
    "assets/site.js": ("f", 20724, "9d9f499e952a0febe9da565d4762afed09f09d370b2b0b556e307857f920a792"),
    "get.sh": ("f", 37513, "13044fedc5826ec7282802f998508e7080f86837e906ae1bd611a9e40f2c1251"),
    "index.html": ("f", 29042, "ee8de2395a4d11647a201369c33b6fad6cff07029b139642075ff0007aa6efaf"),
    "releases": ("d", 0, ""),
    "releases/index.json": ("f", 722, "161247e001f182326e229a9d8198d2c73fa9972d38a15a847b2d07d62dd0bdcf"),
    "releases/latest.json": ("f", 671, "0d69287f97ecea94062da89f71280c57e0b05b081321904c4e5c937eb826213f"),
    "releases/latest.txt": ("f", 16, "72d52943f676ec0566bbb5a144451160673c8d799dbe0d5f6442059e8274644a"),
    f"releases/{VERSION}": ("d", 0, ""),
    f"releases/{VERSION}/celikpanel-v0.1.0-alpha.35.tar.gz": ("f", 22255771, "b588254f58bb6ade0adee22595c0cde1fa8119cfd55db615332bbdb50bc01a70"),
    f"releases/{VERSION}/celikpanel-v0.1.0-alpha.35.tar.gz.sha256": ("f", 100, "da641e9dda17b8456f339dd0fc4735b69ecf87fc23da426bf9797b199ae4f3de"),
    f"releases/{VERSION}/linux": ("d", 0, ""),
    f"releases/{VERSION}/linux/amd64": ("d", 0, ""),
    f"releases/{VERSION}/linux/amd64/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz": ("f", 22255771, "b588254f58bb6ade0adee22595c0cde1fa8119cfd55db615332bbdb50bc01a70"),
    f"releases/{VERSION}/linux/amd64/celikpanel-v0.1.0-alpha.35-linux-amd64.tar.gz.sha256": ("f", 112, "3f798069eab3ecf49fc989ebd225e95924de26f2b76fe9d78705eae52bd1015a"),
    f"releases/{VERSION}/linux/amd64/release.json": ("f", 671, "0d69287f97ecea94062da89f71280c57e0b05b081321904c4e5c937eb826213f"),
    f"releases/{VERSION}/linux/amd64/release-manifest-v2": ("f", 332, "3240ffa25e0f34be323c74167bbe9022e565a0f7f4f7de55d164c3a1efc8db48"),
    f"releases/{VERSION}/linux/amd64/release-manifest-v2.sig": ("f", 64, "9755eaaf37e8944a07b8f98a4cc9f6ece9418e2c136e86728043f72128fecf2f"),
    f"releases/{VERSION}/release.json": ("f", 381, "7709b355a1fca7707c34e77e300051bf5159c081c79d6a69cabc82b94361f8e5"),
}

EXPECTED_RELEASE_DIGESTS = {
    name: value[2]
    for name, value in EXPECTED_ARCHIVE_OBJECTS.items()
    if value[0] == "f" and name.startswith(f"releases/{VERSION}/")
}

libc = ctypes.CDLL(None, use_errno=True)
RENAME_NOREPLACE = 1
RENAME_EXCHANGE = 2
AT_FDCWD = -100


def fail(message: str) -> None:
    raise RuntimeError(message)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def fsync_dir(path: Path) -> None:
    flags = os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_CLOEXEC", 0)
    fd = os.open(path, flags)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def identity(path: Path) -> tuple[int, int]:
    value = os.lstat(path)
    if not stat.S_ISDIR(value.st_mode):
        fail(f"not a directory: {path}")
    return value.st_dev, value.st_ino


def renameat2(source: Path, target: Path, flags: int) -> None:
    function = getattr(libc, "renameat2", None)
    if function is None:
        fail("renameat2 is unavailable")
    result = function(AT_FDCWD, os.fsencode(source), AT_FDCWD, os.fsencode(target), flags)
    if result != 0:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code), f"{source} -> {target}")
    fsync_dir(ROOT)
    fsync_dir(BACKUPS)


def safe_tree(path: Path) -> None:
    if path.is_symlink() or not path.is_dir():
        fail(f"unsafe tree root: {path}")
    for base, directories, files in os.walk(path, followlinks=False):
        for name in directories + files:
            child = Path(base) / name
            mode = os.lstat(child).st_mode
            if stat.S_ISLNK(mode) or not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                fail(f"unsafe tree member: {child}")


def inventory(path: Path) -> list[tuple[str, str, int, int, str]]:
    safe_tree(path)
    result = []
    for child in sorted(path.rglob("*"), key=lambda item: item.relative_to(path).as_posix()):
        relative = child.relative_to(path).as_posix()
        value = os.lstat(child)
        if stat.S_ISDIR(value.st_mode):
            result.append((relative, "d", stat.S_IMODE(value.st_mode), 0, ""))
        elif stat.S_ISREG(value.st_mode):
            result.append((relative, "f", stat.S_IMODE(value.st_mode), value.st_size, sha256(child)))
        else:
            fail(f"unsafe inventory member: {child}")
    return result


def expected_initial_inventory() -> list[tuple[str, str, int, int, str]]:
    result = []
    for name, (kind, size, digest) in sorted(EXPECTED_ARCHIVE_OBJECTS.items()):
        mode = 0o755 if kind == "d" or name == "get.sh" else 0o644
        result.append((name, kind, mode, size, digest))
    return result


def release_entries(path: Path) -> set[str]:
    return {child.name for child in path.iterdir()}


def preserved_upload_identities() -> dict[Path, tuple[int, ...]]:
    root_device = os.lstat(ROOT).st_dev
    result = {}
    for path in PRESERVED_UPLOAD_DIRS:
        if path.parent != ROOT:
            fail(f"unsafe preserved upload path: {path}")
        value = os.lstat(path)
        if (
            path.is_symlink()
            or not stat.S_ISDIR(value.st_mode)
            or value.st_dev != root_device
            or value.st_uid != os.geteuid()
            or stat.S_IMODE(value.st_mode) != 0o700
        ):
            fail(f"unsafe preserved upload metadata: {path}")
        result[path] = (
            value.st_dev,
            value.st_ino,
            value.st_mode,
            value.st_nlink,
            value.st_uid,
            value.st_gid,
            value.st_size,
            value.st_mtime_ns,
            value.st_ctime_ns,
        )
    return result


def verify_preserved_upload_identities(expected: dict[Path, tuple[int, ...]]) -> None:
    if preserved_upload_identities() != expected:
        fail("preserved upload sibling changed")


def validate_selectors(root: Path) -> None:
    if (root / "releases/latest.txt").read_text(encoding="utf-8").strip() != VERSION:
        fail("latest.txt mismatch")
    latest = json.loads((root / "releases/latest.json").read_text(encoding="utf-8"))
    if latest.get("version") != VERSION or latest.get("sequence") != SEQUENCE or latest.get("commit") != COMMIT:
        fail("latest.json identity mismatch")
    index = json.loads((root / "releases/index.json").read_text(encoding="utf-8"))
    if index != {"latest": VERSION, "releases": [latest]}:
        fail("index.json identity mismatch")
    if latest.get("sha256") != ARCHIVE_SHA:
        fail("latest.json archive digest mismatch")
    manifest = (root / f"releases/{VERSION}/linux/amd64/release-manifest-v2").read_text(encoding="ascii")
    required = {
        "format=celikpanel-release-manifest-v2",
        f"sequence={SEQUENCE}",
        f"version={VERSION}",
        f"commit={COMMIT}",
        "os=linux",
        "arch=amd64",
        f"archive_size={ARCHIVE_SIZE}",
        f"archive_sha256={ARCHIVE_SHA}",
    }
    if not required.issubset(set(manifest.splitlines())):
        fail("signed manifest mismatch")


def normalized_tar_name(member: tarfile.TarInfo) -> str | None:
    raw = member.name
    if "\x00" in raw or "\\" in raw or raw.startswith("/"):
        fail(f"unsafe tar path: {member.name!r}")
    while raw.startswith("./"):
        raw = raw[2:]
    raw = raw.rstrip("/")
    if raw == ARCHIVE_ROOT:
        if not member.isdir():
            fail("archive root marker is not a directory")
        return None
    prefix = ARCHIVE_ROOT + "/"
    if not raw.startswith(prefix):
        fail(f"unexpected archive root: {member.name!r}")
    raw = raw[len(prefix):]
    parts = PurePosixPath(raw).parts
    if not raw or any(part in ("", ".", "..") for part in parts):
        fail(f"noncanonical tar path: {member.name!r}")
    return raw


def validate_archive_and_extract(stage: Path) -> None:
    if sha256(UPLOAD) != PACKAGE_SHA or UPLOAD.stat().st_size != PACKAGE_SIZE:
        fail("uploaded package changed")
    seen: set[str] = set()
    root_seen = False
    total_size = 0
    with tarfile.open(UPLOAD, "r:gz") as archive:
        members = archive.getmembers()
        for member in members:
            name = normalized_tar_name(member)
            if name is None:
                if root_seen:
                    fail("duplicate tar root marker")
                root_seen = True
                continue
            if name in seen:
                fail(f"duplicate tar path: {name}")
            seen.add(name)
            if not (member.isdir() or member.isreg()):
                fail(f"unsafe tar type: {name}")
            if member.pax_headers or getattr(member, "sparse", None):
                fail(f"extended tar member refused: {name}")
            if member.size < 0 or member.size > 2 * 1024 * 1024 * 1024:
                fail(f"unsafe tar size: {name}")
            total_size += member.size
            if total_size > 2 * 1024 * 1024 * 1024:
                fail("tar expansion limit exceeded")
            expected = EXPECTED_ARCHIVE_OBJECTS.get(name)
            kind = "d" if member.isdir() else "f"
            if expected is None or expected[0] != kind or expected[1] != member.size:
                fail(f"unexpected archive object: {name}")
        if seen != set(EXPECTED_ARCHIVE_OBJECTS):
            fail("archive inventory mismatch")

        for member in members:
            name = normalized_tar_name(member)
            if name is None:
                continue
            target = stage.joinpath(*PurePosixPath(name).parts)
            if member.isdir():
                target.mkdir(mode=0o755)
                os.chmod(target, 0o755)
                continue
            if not target.parent.is_dir() or target.parent.is_symlink():
                fail(f"unsafe extraction parent: {name}")
            source = archive.extractfile(member)
            if source is None:
                fail(f"cannot read tar member: {name}")
            flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0)
            fd = os.open(target, flags, 0o600)
            try:
                with os.fdopen(fd, "wb", closefd=False) as output:
                    shutil.copyfileobj(source, output, 1024 * 1024)
                    output.flush()
                    os.fsync(output.fileno())
            finally:
                os.close(fd)
                source.close()
            os.chmod(target, 0o755 if name == "get.sh" else 0o644)
    safe_tree(stage)
    if inventory(stage) != expected_initial_inventory():
        fail("exact extracted package inventory mismatch")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        return None


def fetch(relative: str) -> bytes:
    target_url = "https://celikpanel.net/" + relative
    request = urllib.request.Request(
        target_url,
        headers={"Cache-Control": "no-cache", "User-Agent": "celikpanel-release-verifier/1"},
    )
    opener = urllib.request.build_opener(NoRedirect)
    with opener.open(request, timeout=30) as response:
        if response.status != 200 or response.geturl() != request.full_url:
            fail(f"unexpected HTTP response for {relative}")
        return response.read()


def regular_inventory_paths(root: Path) -> list[str]:
    return [relative for relative, kind, _mode, _size, _digest in inventory(root) if kind == "f"]


def post_publish_verify(old_public_paths: list[str]) -> None:
    paths = [
        "index.html",
        "get.sh",
        "assets/site.js",
        "assets/site.css",
        ".well-known/security.txt",
        "releases/latest.txt",
        "releases/latest.json",
        "releases/index.json",
        *EXPECTED_RELEASE_DIGESTS.keys(),
        *old_public_paths,
    ]
    for relative in dict.fromkeys(paths):
        remote = fetch(relative)
        local = (LIVE / relative).read_bytes()
        if remote != local:
            fail(f"public byte mismatch: {relative}")


def locate_old_peer(old_id: tuple[int, int], stage: Path, backup: Path) -> Path:
    matches = []
    for candidate in (stage, backup):
        try:
            if identity(candidate) == old_id:
                matches.append(candidate)
        except FileNotFoundError:
            pass
    if len(matches) != 1:
        fail("cannot uniquely identify old live peer")
    return matches[0]


def validate_unique_candidate(path: Path, prefix: str, expected_id: tuple[int, int]) -> None:
    if path.parent != ROOT or not path.name.startswith(prefix) or path.is_symlink():
        fail(f"unsafe cleanup target: {path}")
    if identity(path) != expected_id:
        fail(f"cleanup identity mismatch: {path}")


def remove_failed_candidate(path: Path, prefix: str, expected_id: tuple[int, int]) -> None:
    validate_unique_candidate(path, prefix, expected_id)
    shutil.rmtree(path)
    fsync_dir(ROOT)


def cleanup_upload(upload_id: tuple[int, int]) -> None:
    if UPLOAD.parent != UPLOAD_DIR or UPLOAD_DIR.parent != ROOT:
        fail("unsafe upload cleanup path")
    current = os.lstat(UPLOAD)
    if (current.st_dev, current.st_ino) != upload_id or not stat.S_ISREG(current.st_mode):
        fail("upload identity changed before cleanup")
    UPLOAD.unlink()
    UPLOAD_DIR.rmdir()  # Refuses to remove an unexpected non-empty upload directory.
    fsync_dir(ROOT)


def cleanup_retry_state(
    stage: Path | None,
    stage_id: tuple[int, int] | None,
    old_id: tuple[int, int] | None,
    upload_id: tuple[int, int],
) -> None:
    if stage is not None and (stage.exists() or stage.is_symlink()):
        if stage_id is None or old_id is None:
            fail("missing stage cleanup identity")
        if identity(LIVE) != old_id:
            fail("live identity changed before stage cleanup")
        remove_failed_candidate(stage, ".stage-alpha35-", stage_id)
    cleanup_upload(upload_id)


def main() -> None:
    if fcntl is None or os.name != "posix":
        fail("this promotion program requires Linux")
    os.umask(0o022)
    if ROOT.resolve() != ROOT or LIVE.is_symlink() or BACKUPS.is_symlink() or UPLOAD_DIR.is_symlink():
        fail("unsafe portal root")
    live_stat = os.lstat(LIVE)
    backup_stat = os.lstat(BACKUPS)
    upload_dir_stat = os.lstat(UPLOAD_DIR)
    upload_stat = os.lstat(UPLOAD)
    if not all(stat.S_ISDIR(value.st_mode) for value in (live_stat, backup_stat, upload_dir_stat)):
        fail("live, backups, or upload root is not a directory")
    if len({live_stat.st_dev, backup_stat.st_dev, upload_dir_stat.st_dev, upload_stat.st_dev}) != 1:
        fail("portal paths are not on one filesystem")
    if stat.S_IMODE(live_stat.st_mode) != 0o755:
        fail("unexpected live mode")
    if upload_dir_stat.st_uid != os.geteuid() or stat.S_IMODE(upload_dir_stat.st_mode) & 0o077:
        fail("unsafe upload directory metadata")
    if (
        not stat.S_ISREG(upload_stat.st_mode)
        or upload_stat.st_nlink != 1
        or upload_stat.st_uid != os.geteuid()
        or stat.S_IMODE(upload_stat.st_mode) & 0o077
    ):
        fail("unsafe upload metadata")
    upload_id = (upload_stat.st_dev, upload_stat.st_ino)
    preserved_uploads = preserved_upload_identities()

    lock_fd = None
    success = False
    rollback_complete = False
    exchange_attempted = False
    old_id = None
    stage = None
    stage_id = None
    failed = None
    backup = None
    try:
        lock_fd = os.open(LOCK, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
        lock_stat = os.fstat(lock_fd)
        lock_path_stat = os.lstat(LOCK)
        if (
            not stat.S_ISREG(lock_stat.st_mode)
            or lock_stat.st_nlink != 1
            or lock_stat.st_uid != os.geteuid()
            or stat.S_IMODE(lock_stat.st_mode) & 0o022
        ):
            fail("unsafe deployment lock")
        if (lock_stat.st_dev, lock_stat.st_ino) != (lock_path_stat.st_dev, lock_path_stat.st_ino):
            fail("deployment lock changed")
        fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        after_lock = os.lstat(LOCK)
        if (after_lock.st_dev, after_lock.st_ino) != (lock_stat.st_dev, lock_stat.st_ino):
            fail("deployment lock changed after acquisition")
        verify_preserved_upload_identities(preserved_uploads)

        if (LIVE / "releases/latest.txt").read_text(encoding="utf-8").strip() != PREVIOUS:
            fail("live portal is no longer alpha.34")
        initial_entries = release_entries(LIVE / "releases")
        if initial_entries != OLD_VERSIONS | RELEASE_SELECTORS:
            fail(f"unexpected live release inventory: {sorted(initial_entries)}")
        if (LIVE / f"releases/{VERSION}").exists():
            fail("alpha.35 already exists")
        old_id = identity(LIVE)
        old_live_inventory = inventory(LIVE)
        old_inventory = {version: inventory(LIVE / "releases" / version) for version in OLD_VERSIONS}
        old_public_paths = []
        for version in sorted(OLD_VERSIONS):
            for relative in regular_inventory_paths(LIVE / "releases" / version):
                old_public_paths.append(f"releases/{version}/{relative}")

        stage = Path(tempfile.mkdtemp(prefix=".stage-alpha35-", dir=ROOT))
        stage_id = identity(stage)
        os.chmod(stage, 0o755)
        if stage_id == old_id:
            fail("stage/live inode collision")
        token = secrets.token_hex(8)
        stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
        backup = BACKUPS / f"httpdocs.before-{VERSION}.{stamp}.{os.getpid()}.{token}"
        failed = ROOT / f".failed-alpha35-{os.getpid()}-{token}"
        if backup.exists() or backup.is_symlink():
            fail("backup path collision")
        if failed.exists() or failed.is_symlink():
            fail("failed-candidate path collision")

        validate_archive_and_extract(stage)
        if release_entries(stage / "releases") != {VERSION} | RELEASE_SELECTORS:
            fail("initial stage release inventory mismatch")
        validate_selectors(stage)
        for relative, expected in EXPECTED_RELEASE_DIGESTS.items():
            if sha256(stage / relative) != expected:
                fail(f"release digest mismatch: {relative}")

        for version in sorted(OLD_VERSIONS):
            source = LIVE / "releases" / version
            target = stage / "releases" / version
            if target.exists() or target.is_symlink():
                fail(f"old release collision: {version}")
            shutil.copytree(source, target, symlinks=False, copy_function=shutil.copy2)
            if inventory(target) != old_inventory[version] or inventory(source) != old_inventory[version]:
                fail(f"old release changed: {version}")
        if release_entries(stage / "releases") != OLD_VERSIONS | {VERSION} | RELEASE_SELECTORS:
            fail("final stage release inventory mismatch")

        expected_final = expected_initial_inventory()
        for version in sorted(OLD_VERSIONS):
            expected_final.append((f"releases/{version}", "d", 0o755, 0, ""))
            expected_final.extend(
                (f"releases/{version}/{relative}", kind, mode, size, digest)
                for relative, kind, mode, size, digest in old_inventory[version]
            )
        expected_final.sort(key=lambda item: item[0])
        if inventory(stage) != expected_final:
            fail("exact final stage inventory mismatch")
        if inventory(LIVE) != old_live_inventory:
            fail("live portal changed during staging")
        os.chmod(stage, 0o755)
        fsync_dir(stage)
        fsync_dir(ROOT)

        def interrupted(signum, _frame):
            raise RuntimeError(f"signal {signum}")

        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(signum, interrupted)

        verify_preserved_upload_identities(preserved_uploads)
        try:
            exchange_attempted = True
            renameat2(LIVE, stage, RENAME_EXCHANGE)
            if identity(LIVE) != stage_id or identity(stage) != old_id:
                fail("post-exchange inode mismatch")
            for version in OLD_VERSIONS:
                if inventory(LIVE / "releases" / version) != old_inventory[version]:
                    fail(f"published old release drift: {version}")
            validate_selectors(LIVE)
            post_publish_verify(old_public_paths)
            verify_preserved_upload_identities(preserved_uploads)

            renameat2(stage, backup, RENAME_NOREPLACE)
            if identity(backup) != old_id or identity(LIVE) != stage_id:
                fail("backup publication identity mismatch")
            os.chmod(backup, 0o750)
            if identity(backup) != old_id or stat.S_IMODE(os.lstat(backup).st_mode) != 0o750:
                fail("backup permission verification failed")
            fsync_dir(BACKUPS)
            post_publish_verify(old_public_paths)
            verify_preserved_upload_identities(preserved_uploads)
            for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
                signal.signal(signum, signal.SIG_IGN)
            success = True
        finally:
            if not success:
                for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
                    signal.signal(signum, signal.SIG_IGN)
                current = identity(LIVE)
                if current == stage_id:
                    peer = locate_old_peer(old_id, stage, backup)
                    peer_is_backup = peer == backup
                    if peer_is_backup:
                        if peer != backup or identity(peer) != old_id:
                            fail("published backup rollback identity mismatch")
                        os.chmod(peer, 0o755)
                        if identity(peer) != old_id or stat.S_IMODE(os.lstat(peer).st_mode) != 0o755:
                            fail("rollback peer permission verification failed")
                        fsync_dir(BACKUPS)
                    renameat2(LIVE, peer, RENAME_EXCHANGE)
                    if identity(LIVE) != old_id:
                        fail("automatic rollback identity mismatch")
                    candidate = peer
                    if peer_is_backup:
                        if peer != backup or identity(backup) != stage_id:
                            fail("published backup rollback identity mismatch")
                        renameat2(backup, failed, RENAME_NOREPLACE)
                        if identity(failed) != stage_id:
                            fail("failed candidate quarantine identity mismatch")
                        candidate = failed
                    if inventory(LIVE) != old_live_inventory:
                        fail("rolled-back live inventory mismatch")
                    verify_preserved_upload_identities(preserved_uploads)
                    validate_unique_candidate(
                        candidate,
                        ".failed-alpha35-" if candidate == failed else ".stage-alpha35-",
                        stage_id,
                    )
                    remove_failed_candidate(
                        candidate,
                        ".failed-alpha35-" if candidate == failed else ".stage-alpha35-",
                        stage_id,
                    )
                    rollback_complete = True
                elif current == old_id:
                    rollback_complete = True
                else:
                    fail("live inode is neither old nor new")
    except BaseException:
        if not success and (not exchange_attempted or rollback_complete):
            verify_preserved_upload_identities(preserved_uploads)
            cleanup_retry_state(stage, stage_id, old_id, upload_id)
        raise
    finally:
        if lock_fd is not None:
            try:
                if fcntl is not None:
                    fcntl.flock(lock_fd, fcntl.LOCK_UN)
            finally:
                os.close(lock_fd)

    verify_preserved_upload_identities(preserved_uploads)
    cleanup_upload(upload_id)
    print(f"PROMOTED={VERSION}")
    print(f"BACKUP={backup}")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"ERROR: {error}", file=sys.stderr)
        raise SystemExit(1)
