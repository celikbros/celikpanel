#!/usr/bin/env python3
"""Bounded, fail-closed verification of the public download portal.

The production transaction runs this verifier exactly once, immediately after
the new portal tree is exchanged into place.  Backup publication is verified
locally; it must not trigger a second public pass.
"""

from __future__ import annotations

import argparse
import contextlib
import hashlib
import json
import os
import re
import stat
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import BinaryIO, Iterable


SMALL_RESPONSE_BUDGET = 1024 * 1024
HARD_REQUEST_LIMIT = 15
MAX_ARCHIVE_SIZE = 2_147_483_648
CHUNK_SIZE = 128 * 1024
SAFE_SEGMENT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]*$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")
COMMIT = re.compile(r"^[0-9a-f]{40}$")

ROOT_CRITICAL = (
    "index.html",
    "assets/site.css",
    "assets/site.js",
    "get.sh",
    "release-signing-ed25519.pem",
    ".well-known/security.txt",
)
SELECTORS = (
    "releases/latest.txt",
    "releases/latest.json",
    "releases/index.json",
)


class VerificationError(RuntimeError):
    pass


@dataclass(frozen=True)
class FileIdentity:
    device: int
    inode: int
    mode: int
    size: int
    modified_ns: int
    changed_ns: int


@dataclass(frozen=True)
class PlannedFile:
    relative_path: str
    local_path: Path
    size: int
    identity: FileIdentity
    is_archive: bool = False


@dataclass(frozen=True)
class Target:
    version: str
    os_name: str
    architecture: str
    archive_name: str
    archive_size: int
    archive_sha256: str
    files: tuple[PlannedFile, ...]


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: N802
        raise VerificationError(f"redirect refused for {req.full_url}: HTTP {code}")


def fail(message: str) -> None:
    raise VerificationError(message)


def file_identity(info: os.stat_result) -> FileIdentity:
    return FileIdentity(
        device=info.st_dev,
        inode=info.st_ino,
        mode=info.st_mode,
        size=info.st_size,
        modified_ns=info.st_mtime_ns,
        changed_ns=info.st_ctime_ns,
    )


def same_open_target(before: os.stat_result, after: os.stat_result) -> bool:
    return (
        before.st_dev == after.st_dev
        and before.st_ino == after.st_ino
        and stat.S_IFMT(before.st_mode) == stat.S_IFMT(after.st_mode)
        and before.st_size == after.st_size
    )


def open_no_symlink(path: Path) -> int:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_BINARY", 0)
    if os.name == "posix" and hasattr(os, "O_NOFOLLOW") and hasattr(os, "O_DIRECTORY"):
        absolute = path.absolute()
        parts = absolute.parts
        directory_fd = os.open(
            parts[0],
            flags | os.O_DIRECTORY | os.O_NOFOLLOW,
        )
        try:
            for component in parts[1:-1]:
                next_fd = os.open(
                    component,
                    flags | os.O_DIRECTORY | os.O_NOFOLLOW,
                    dir_fd=directory_fd,
                )
                os.close(directory_fd)
                directory_fd = next_fd
            return os.open(
                parts[-1],
                flags | os.O_NOFOLLOW,
                dir_fd=directory_fd,
            )
        except OSError:
            raise
        finally:
            os.close(directory_fd)

    # Development fallback for platforms without openat/O_NOFOLLOW. Production
    # promotion runs on Linux and always uses the descriptor-relative branch.
    current = Path(path.anchor)
    for component in path.absolute().parts[1:-1]:
        current /= component
        info = os.lstat(current)
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
            fail(f"local path has a symlink or non-directory ancestor: {path}")
    before = os.lstat(path)
    if stat.S_ISLNK(before.st_mode):
        fail(f"local path is a symlink: {path}")
    descriptor = os.open(path, flags)
    after = os.fstat(descriptor)
    if not same_open_target(before, after):
        os.close(descriptor)
        fail(f"local path changed while it was opened: {path}")
    return descriptor


@contextlib.contextmanager
def open_regular_file(path: Path, expected: FileIdentity | None = None):
    descriptor = -1
    try:
        descriptor = open_no_symlink(path)
        initial = file_identity(os.fstat(descriptor))
        if not stat.S_ISREG(initial.mode):
            fail(f"local path is not a regular file: {path}")
        if expected is not None and initial != expected:
            fail(f"local target identity changed before comparison: {path}")
        with os.fdopen(descriptor, "rb", closefd=True) as source:
            descriptor = -1
            yield source, initial
            final = file_identity(os.fstat(source.fileno()))
            if final != initial:
                fail(f"local target changed while it was read: {path}")
    except VerificationError:
        raise
    except OSError as exc:
        fail(f"cannot securely open local file {path}: {exc}")
    finally:
        if descriptor >= 0:
            os.close(descriptor)


def read_regular(path: Path, limit: int) -> bytes:
    with open_regular_file(path) as (source, identity):
        if identity.size > limit:
            fail(f"local file exceeds its safety limit: {path}")
        data = source.read(limit + 1)
    if len(data) != identity.size:
        fail(f"local file changed while it was read: {path}")
    return data


def parse_json_object(path: Path) -> tuple[bytes, dict]:
    raw = read_regular(path, SMALL_RESPONSE_BUDGET)
    try:
        value = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        fail(f"invalid local JSON {path}: {exc}")
    if not isinstance(value, dict):
        fail(f"local JSON is not an object: {path}")
    return raw, value


def required_string(value: dict, key: str) -> str:
    result = value.get(key)
    if not isinstance(result, str) or not result:
        fail(f"latest JSON field {key!r} must be a non-empty string")
    return result


def safe_segment(value: str, field: str) -> str:
    if not SAFE_SEGMENT.fullmatch(value) or value in {".", ".."}:
        fail(f"unsafe {field}: {value!r}")
    return value


def parse_manifest(path: Path) -> tuple[bytes, dict[str, str]]:
    raw = read_regular(path, 4096)
    try:
        text = raw.decode("ascii")
    except UnicodeDecodeError as exc:
        fail(f"signed manifest is not ASCII: {exc}")
    if not text.endswith("\n") or "\r" in text:
        fail("signed manifest is not canonical newline-delimited text")
    expected_keys = (
        "format",
        "sequence",
        "version",
        "commit",
        "published_at",
        "os",
        "arch",
        "archive",
        "archive_sha256",
        "archive_size",
    )
    values: dict[str, str] = {}
    lines = text[:-1].split("\n")
    if len(lines) != len(expected_keys):
        fail("signed manifest has an unexpected field count")
    for expected_key, line in zip(expected_keys, lines):
        key, separator, value = line.partition("=")
        if separator != "=" or key != expected_key or not value:
            fail(f"signed manifest field {expected_key!r} is not canonical")
        values[key] = value
    if values["format"] != "celikpanel-release-manifest-v2":
        fail("signed manifest format is unsupported")
    return raw, values


def regular_size(path: Path) -> int:
    with open_regular_file(path) as (_, identity):
        return identity.size


def regular_identity(path: Path) -> FileIdentity:
    with open_regular_file(path) as (_, identity):
        return identity


def hash_regular(path: Path) -> str:
    digest = hashlib.sha256()
    with open_regular_file(path) as (source, _):
        while True:
            chunk = source.read(CHUNK_SIZE)
            if not chunk:
                break
            digest.update(chunk)
    return digest.hexdigest()


def public_path(relative_path: str) -> str:
    return "/" + urllib.parse.quote(relative_path, safe="/._-+")


def local_file(site: Path, relative_path: str) -> Path:
    parts = relative_path.split("/")
    if not parts or any(not part or part in {".", ".."} for part in parts):
        fail(f"unsafe local relative path: {relative_path!r}")
    candidate = site.joinpath(*parts)
    current = site
    for index, part in enumerate(parts):
        current /= part
        try:
            info = current.lstat()
        except OSError as exc:
            fail(f"cannot stat local path component {current}: {exc}")
        if stat.S_ISLNK(info.st_mode):
            fail(f"local path contains a symlink: {relative_path!r}")
        if index < len(parts) - 1 and not stat.S_ISDIR(info.st_mode):
            fail(f"local path contains a non-directory ancestor: {relative_path!r}")
    try:
        resolved_site = site.resolve(strict=True)
        resolved_candidate = candidate.resolve(strict=True)
    except OSError as exc:
        fail(f"cannot resolve local path {candidate}: {exc}")
    try:
        resolved_candidate.relative_to(resolved_site)
    except ValueError:
        fail(f"local path escapes the site root: {relative_path!r}")
    if candidate.is_symlink():
        fail(f"local path is a symlink: {relative_path!r}")
    return candidate


def load_target(site: Path) -> Target:
    try:
        original_site_info = site.lstat()
    except OSError as exc:
        fail(f"cannot stat local site root: {exc}")
    if stat.S_ISLNK(original_site_info.st_mode):
        fail("local site root must not be a symlink")
    try:
        site = site.resolve(strict=True)
    except OSError as exc:
        fail(f"cannot resolve local site root: {exc}")
    if not site.is_dir() or site.is_symlink():
        fail("local site root must be a non-symlink directory")

    latest_text_path = local_file(site, "releases/latest.txt")
    latest_text_raw = read_regular(latest_text_path, 256)
    try:
        latest_text = latest_text_raw.decode("ascii")
    except UnicodeDecodeError as exc:
        fail(f"latest.txt is not ASCII: {exc}")
    if not latest_text.endswith("\n") or latest_text.count("\n") != 1:
        fail("latest.txt is not one canonical newline-terminated version")
    version = safe_segment(latest_text[:-1], "version")

    latest_json_path = local_file(site, "releases/latest.json")
    latest_raw, latest = parse_json_object(latest_json_path)
    if required_string(latest, "version") != version:
        fail("latest.txt and latest.json target different versions")
    os_name = safe_segment(required_string(latest, "os"), "operating system")
    architecture = safe_segment(required_string(latest, "arch"), "architecture")
    archive_sha256 = required_string(latest, "sha256")
    if not SHA256.fullmatch(archive_sha256):
        fail("latest JSON archive SHA-256 is invalid")
    if not COMMIT.fullmatch(required_string(latest, "commit")):
        fail("latest JSON commit is invalid")
    required_string(latest, "sequence")
    required_string(latest, "published_at")

    platform_prefix = f"releases/{version}/{os_name}/{architecture}"
    manifest_path = local_file(site, f"{platform_prefix}/release-manifest-v2")
    _, manifest = parse_manifest(manifest_path)
    for key, expected in (
        ("version", version),
        ("os", os_name),
        ("arch", architecture),
        ("commit", latest["commit"]),
        ("sequence", latest["sequence"]),
        ("published_at", latest["published_at"]),
        ("archive_sha256", archive_sha256),
    ):
        if manifest[key] != expected:
            fail(f"latest JSON and signed manifest disagree on {key}")
    archive_name = safe_segment(manifest["archive"], "archive filename")
    try:
        archive_size = int(manifest["archive_size"], 10)
    except ValueError:
        fail("signed manifest archive size is invalid")
    if str(archive_size) != manifest["archive_size"] or not 0 < archive_size <= MAX_ARCHIVE_SIZE:
        fail("signed manifest archive size is outside the accepted range")

    expected_archive_url = public_path(f"{platform_prefix}/{archive_name}")
    expected_checksum_url = expected_archive_url + ".sha256"
    expected_manifest_url = public_path(f"{platform_prefix}/release-manifest-v2")
    expected_signature_url = expected_manifest_url + ".sig"
    for key, expected in (
        ("archive_url", expected_archive_url),
        ("checksum_url", expected_checksum_url),
        ("signed_manifest_url", expected_manifest_url),
        ("signed_manifest_signature_url", expected_signature_url),
    ):
        if required_string(latest, key) != expected:
            fail(f"latest JSON field {key!r} does not name the exact target path")

    index_path = local_file(site, "releases/index.json")
    _, index = parse_json_object(index_path)
    if index.get("latest") != version or index.get("releases") != [latest]:
        fail("release index does not contain exactly the local latest release")

    platform_release_json_path = local_file(site, f"{platform_prefix}/release.json")
    platform_release_raw, platform_release = parse_json_object(platform_release_json_path)
    if platform_release != latest or platform_release_raw != latest_raw:
        fail("platform release JSON is not byte-identical to latest.json")

    archive_path = local_file(site, f"{platform_prefix}/{archive_name}")
    if regular_size(archive_path) != archive_size:
        fail("local target archive size does not match the signed manifest")
    if hash_regular(archive_path) != archive_sha256:
        fail("local target archive digest does not match the signed manifest")

    checksum_path = local_file(site, f"{platform_prefix}/{archive_name}.sha256")
    checksum_raw = read_regular(checksum_path, 4096)
    expected_checksum = f"{archive_sha256}  {archive_name}\n".encode("ascii")
    if checksum_raw != expected_checksum:
        fail("platform checksum file is not canonical")

    signature_path = local_file(site, f"{platform_prefix}/release-manifest-v2.sig")
    if regular_size(signature_path) != 64:
        fail("signed manifest signature is not exactly 64 bytes")

    legacy_archive_name = f"celikpanel-{version}.tar.gz"
    legacy_release_json_path = local_file(site, f"releases/{version}/release.json")
    _, legacy_release = parse_json_object(legacy_release_json_path)
    if legacy_release.get("version") != version or legacy_release.get("sha256") != archive_sha256:
        fail("legacy target release JSON disagrees with the platform release")
    if legacy_release.get("archive_url") != f"/releases/{version}/{legacy_archive_name}":
        fail("legacy target release JSON has an unexpected archive URL")
    if legacy_release.get("checksum_url") != f"/releases/{version}/{legacy_archive_name}.sha256":
        fail("legacy target release JSON has an unexpected checksum URL")
    legacy_checksum_path = local_file(site, f"releases/{version}/{legacy_archive_name}.sha256")
    legacy_checksum_raw = read_regular(legacy_checksum_path, 4096)
    if legacy_checksum_raw != f"{archive_sha256}  {legacy_archive_name}\n".encode("ascii"):
        fail("legacy target checksum file is not canonical")

    relative_paths = list(ROOT_CRITICAL) + list(SELECTORS) + [
        f"{platform_prefix}/release.json",
        f"{platform_prefix}/{archive_name}.sha256",
        f"{platform_prefix}/release-manifest-v2",
        f"{platform_prefix}/release-manifest-v2.sig",
        f"releases/{version}/release.json",
    ]
    planned = []
    for path in relative_paths:
        pinned_path = local_file(site, path)
        identity = regular_identity(pinned_path)
        planned.append(
            PlannedFile(path, pinned_path, identity.size, identity)
        )
    archive_identity = regular_identity(archive_path)
    if archive_identity.size != archive_size:
        fail("local target archive changed before the public plan was pinned")
    planned.append(PlannedFile(
        f"{platform_prefix}/{archive_name}",
        archive_path,
        archive_size,
        archive_identity,
        True,
    ))
    if len(planned) > HARD_REQUEST_LIMIT:
        fail("public verification plan exceeds the hard request limit")
    small_size = sum(item.size for item in planned if not item.is_archive)
    if small_size > SMALL_RESPONSE_BUDGET:
        fail("critical non-archive files exceed the fixed one-MiB network budget")
    return Target(
        version=version,
        os_name=os_name,
        architecture=architecture,
        archive_name=archive_name,
        archive_size=archive_size,
        archive_sha256=archive_sha256,
        files=tuple(planned),
    )


def validate_base_url(value: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        fail("base URL must be an absolute HTTP(S) URL")
    if parsed.username is not None or parsed.password is not None:
        fail("base URL must not contain credentials")
    if parsed.query or parsed.fragment or parsed.path not in {"", "/"}:
        fail("base URL must not contain a path, query, or fragment")
    if parsed.scheme == "http" and parsed.hostname not in {"127.0.0.1", "localhost", "::1"}:
        fail("plain HTTP is accepted only for a loopback test server")
    return value.rstrip("/")


def compare_response(response: BinaryIO, item: PlannedFile, budget: int, downloaded: int) -> int:
    try:
        with open_regular_file(item.local_path, item.identity) as (expected, _):
            remaining = item.size
            while remaining:
                wanted = min(CHUNK_SIZE, remaining)
                actual_chunk = response.read(wanted)
                expected_chunk = expected.read(wanted)
                if len(actual_chunk) != len(expected_chunk):
                    fail(f"short public response for /{item.relative_path}")
                downloaded += len(actual_chunk)
                if downloaded > budget:
                    fail("public verification exceeded its byte budget")
                if actual_chunk != expected_chunk:
                    fail(f"public bytes differ from local target: /{item.relative_path}")
                remaining -= len(actual_chunk)
            if expected.read(1):
                fail(f"local target changed during verification: {item.local_path}")
            extra = response.read(1)
            downloaded += len(extra)
            if downloaded > budget:
                fail("public verification exceeded its byte budget")
            if extra:
                fail(f"public response is longer than local target: /{item.relative_path}")
    except OSError as exc:
        fail(f"cannot compare local target {item.local_path}: {exc}")
    return downloaded


def verify_public_portal(
    site_root: os.PathLike[str] | str,
    base_url: str,
    target_version: str,
    *,
    opener=None,
    timeout: float = 30.0,
) -> dict:
    """Verify one exact target with the transaction's single public GET plan."""
    target_version = safe_segment(target_version, "target version")
    target = load_target(Path(site_root))
    if target.version != target_version:
        fail(
            f"local portal targets {target.version!r}, not the pinned version "
            f"{target_version!r}"
        )
    base_url = validate_base_url(base_url)
    byte_limit = target.archive_size + SMALL_RESPONSE_BUDGET
    if opener is None:
        opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            NoRedirect(),
        )
    downloaded = 0
    requests = 0
    archive_gets = 0
    requested_paths: list[str] = []
    for item in target.files:
        if requests >= HARD_REQUEST_LIMIT:
            fail("public verification exceeded its request budget")
        path = public_path(item.relative_path)
        url = base_url + path
        request = urllib.request.Request(
            url,
            method="GET",
            headers={
                "Accept": "application/octet-stream, application/json, text/plain, text/html",
                "Accept-Encoding": "identity",
                "Cache-Control": "no-cache",
                "User-Agent": "CelikPanel-Portal-Verifier/1",
            },
        )
        requests += 1
        requested_paths.append(path)
        if item.is_archive:
            archive_gets += 1
        try:
            with opener.open(request, timeout=timeout) as response:
                status = getattr(response, "status", None)
                if status != 200:
                    fail(f"unexpected HTTP status for {path}: {status}")
                if response.geturl() != url:
                    fail(f"response URL changed for {path}")
                encodings = response.headers.get_all("Content-Encoding", [])
                if encodings:
                    fail(f"encoded response refused for {path}")
                if response.headers.get_all("Transfer-Encoding", []):
                    fail(f"transfer-encoded response refused for {path}")
                lengths = response.headers.get_all("Content-Length", [])
                if lengths != [str(item.size)]:
                    fail(f"Content-Length does not match the local target for {path}")
                downloaded = compare_response(response, item, byte_limit, downloaded)
        except VerificationError:
            raise
        except urllib.error.HTTPError as exc:
            fail(f"HTTP {exc.code} for {path}; redirects are not accepted")
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            fail(f"request failed for {path}: {exc}")

    if requests != len(target.files) or requests > HARD_REQUEST_LIMIT:
        fail("public verification did not use its exact request plan")
    if archive_gets != 1:
        fail("target platform archive body was not fetched exactly once")
    if downloaded > byte_limit:
        fail("public verification exceeded its byte budget")
    return {
        "status": "ok",
        "policy": "single-full-pass-after-exchange",
        "phase": "full",
        "version": target.version,
        "requests": requests,
        "request_limit": HARD_REQUEST_LIMIT,
        "downloaded_bytes": downloaded,
        "download_byte_limit": byte_limit,
        "fixed_overhead_bytes": SMALL_RESPONSE_BUDGET,
        "archive_gets": archive_gets,
        "paths": requested_paths,
    }


def parse_args(argv: Iterable[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Run the transaction's single bounded public portal pass. "
            "Do not run another public pass after backup publication."
        )
    )
    parser.add_argument("--site", required=True, type=Path, help="built local portal tree")
    parser.add_argument("--base-url", required=True, help="public portal origin")
    parser.add_argument(
        "--target-version",
        required=True,
        help="exact release version approved by the promotion transaction",
    )
    parser.add_argument(
        "--phase",
        default="full",
        choices=("full",),
        help="the only transaction public phase (default: full)",
    )
    parser.add_argument("--timeout", type=float, default=30.0, help="per-request timeout seconds")
    args = parser.parse_args(argv)
    if not 0 < args.timeout <= 300:
        parser.error("--timeout must be greater than zero and no more than 300 seconds")
    return args


def main(argv: Iterable[str] | None = None) -> int:
    try:
        args = parse_args(argv)
        result = verify_public_portal(
            args.site,
            args.base_url,
            args.target_version,
            timeout=args.timeout,
        )
    except VerificationError as exc:
        print(f"portal public verification failed: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
