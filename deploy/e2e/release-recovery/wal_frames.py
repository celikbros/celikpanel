#!/usr/bin/env python3
"""Bounded, non-authorizing SQLite WAL byte evidence for disposable fixtures.

Format: https://www.sqlite.org/fileformat2.html#walformat (sections 4.1-4.5).
WAL header/frames store integers big-endian; the magic selects checksum input
endianness. Page bytes are hashed, never decoded or returned. This module never
opens SQLite, opens a pathname, writes a file, or changes a descriptor offset.

A checksum-valid noncommit suffix is physical evidence only. It can survive a
rollback. The caller must separately bind process, syscall, inode and accepted
operation before describing an interrupted live transaction. Invalid/truncated
or stale trailing bytes deliberately make the entire observation unknown.
"""
from __future__ import annotations

import hashlib
import os
import stat
import struct

SCHEMA = "celikpanel/lab-sqlite-wal-evidence/v1"
MAX_BYTES = 64 * 1024 * 1024
MAX_FRAMES = 16384
WAL_VERSION = 3007000
HEADER_BYTES = 32
FRAME_HEADER_BYTES = 24
MAX_PAGE_NUMBER = 0xFFFFFFFE
LIMITS = [
    "physical-WAL-bytes-only-not-write-or-recovery-authority",
    "valid-noncommit-suffix-may-be-remnants-of-a-rolled-back-transaction",
    "process-inode-syscall-and-operation-binding-required-separately",
    "no-SQLite-open-no-page-row-decoding-no-durability-claim",
]


def _unknown(reason: str, size: int | None = None, digest: str | None = None,
             *, diagnostic: dict | None = None) -> dict:
    answer = {"schema": SCHEMA, "status": "unknown", "classification": "unknown",
              "reason": reason, "byte_count": size, "sha256": digest,
              "uncommitted_transaction": "not-established-by-bytes",
              "growth_after_prior_commit": False, "limits": list(LIMITS)}
    if diagnostic is not None:
        answer["diagnostic_valid_prefix"] = diagnostic
    return answer


def _valid_limits(max_bytes: int, max_frames: int) -> bool:
    return (type(max_bytes) is int and HEADER_BYTES <= max_bytes <= MAX_BYTES
            and type(max_frames) is int and 1 <= max_frames <= MAX_FRAMES)


def _page_size(value: int) -> bool:
    # 1 is a main-DB-header sentinel, not a WAL-header page-size encoding.
    return type(value) is int and 512 <= value <= 65536 and value & (value - 1) == 0


def _checksum(data: memoryview, order: str, seed: tuple[int, int]) -> tuple[int, int]:
    first, second = seed
    for x, y in struct.iter_unpack(order + "II", data):
        first = (first + x + second) & 0xFFFFFFFF
        second = (second + y + first) & 0xFFFFFFFF
    return first, second


def _parse(raw: bytes, expected_page_size: int | None, max_bytes: int,
           max_frames: int) -> dict:
    size = len(raw)
    if size > max_bytes:
        return _unknown("byte-limit", size)
    digest = hashlib.sha256(raw).hexdigest()
    if size < HEADER_BYTES:
        return _unknown("empty-or-truncated-header", size, digest)
    view = memoryview(raw)
    magic, version, page_size, sequence, salt1, salt2, c0, c1 = struct.unpack_from(">8I", view)
    if magic not in (0x377F0682, 0x377F0683):
        return _unknown("unsupported-magic", size, digest)
    if version != WAL_VERSION:
        return _unknown("unsupported-version", size, digest)
    if not _page_size(page_size):
        return _unknown("unsupported-page-size", size, digest)
    if expected_page_size is not None and page_size != expected_page_size:
        return _unknown("database-page-size-mismatch", size, digest)
    order = "<" if magic == 0x377F0682 else ">"
    checksum = _checksum(view[:24], order, (0, 0))
    if checksum != (c0, c1):
        return _unknown("header-checksum-mismatch", size, digest)
    frame_size = FRAME_HEADER_BYTES + page_size
    count, remainder = divmod(size - HEADER_BYTES, frame_size)
    if count > max_frames:
        return _unknown("frame-limit", size, digest)
    header = {"magic": magic, "version": version, "page_size": page_size,
              "checkpoint_sequence": sequence, "salts": [salt1, salt2],
              "checksum_input_byteorder": "little" if order == "<" else "big",
              "stored_integer_byteorder": "big", "checksum": [c0, c1],
              "sha256": hashlib.sha256(view[:HEADER_BYTES]).hexdigest()}
    frames = []
    latest_commit = 0

    def invalid(reason: str, offset: int) -> dict:
        end = HEADER_BYTES + len(frames) * frame_size
        prefix = {"header": header, "frame_count": len(frames),
                  "latest_commit_frame": latest_commit, "valid_bytes": end,
                  "sha256": hashlib.sha256(view[:end]).hexdigest(),
                  "first_unverified_offset": offset,
                  "remaining_unverified_bytes": size - offset,
                  "scope": "diagnostic-only-do-not-use-as-positive-suffix-proof"}
        return _unknown(reason, size, digest, diagnostic=prefix)

    for index in range(1, count + 1):
        offset = HEADER_BYTES + (index - 1) * frame_size
        page, db_size, s1, s2, f0, f1 = struct.unpack_from(">6I", view, offset)
        if page == 0 or page > MAX_PAGE_NUMBER or db_size > MAX_PAGE_NUMBER:
            return invalid("invalid-frame-page-number", offset)
        if (s1, s2) != (salt1, salt2):
            return invalid("stale-or-mismatched-frame-salts", offset)
        candidate_sum = _checksum(view[offset:offset + 8], order, checksum)
        candidate_sum = _checksum(view[offset + FRAME_HEADER_BYTES:offset + frame_size],
                                  order, candidate_sum)
        if candidate_sum != (f0, f1):
            return invalid("frame-checksum-mismatch", offset)
        checksum = candidate_sum
        if db_size:
            latest_commit = index
        frames.append({"index": index, "offset": offset, "end_offset": offset + frame_size,
                       "page_number": page, "database_size_pages": db_size,
                       "is_commit": db_size != 0, "checksum": [f0, f1],
                       "frame_sha256": hashlib.sha256(view[offset:offset + frame_size]).hexdigest()})
    if remainder:
        return invalid("trailing-incomplete-frame", HEADER_BYTES + count * frame_size)
    commit_end = HEADER_BYTES + latest_commit * frame_size
    suffix_count = count - latest_commit
    classification = ("header-only" if not count else
                      "valid-noncommit-suffix" if suffix_count else "committed-prefix-only")
    return {"schema": SCHEMA, "status": "verified", "classification": classification,
            "reason": "complete-checksum-valid-byte-image", "byte_count": size, "sha256": digest,
            "header": header, "frame_size": frame_size, "frame_count": count, "frames": frames,
            "latest_commit_frame": latest_commit,
            "latest_commit_database_size_pages": frames[latest_commit - 1]["database_size_pages"] if latest_commit else None,
            "committed_prefix_bytes": commit_end,
            "committed_prefix_sha256": hashlib.sha256(view[:commit_end]).hexdigest(),
            "committed_prefix_has_transaction": latest_commit != 0,
            "noncommit_suffix_frames": suffix_count, "noncommit_suffix_offset": commit_end,
            "noncommit_suffix_sha256": hashlib.sha256(view[commit_end:]).hexdigest(),
            "uncommitted_transaction": "not-established-by-bytes",
            "growth_after_prior_commit": False, "prior_prefix": {"status": "not-supplied"},
            "limits": list(LIMITS)}


def inspect_wal(raw: bytes, *, expected_page_size: int | None = None,
                prior_prefix: bytes | None = None, max_bytes: int = MAX_BYTES,
                max_frames: int = MAX_FRAMES) -> dict:
    """Inspect a complete captured byte image, optionally bound to an older prefix.

    prior_prefix must itself be a full valid header or end at a commit frame.
    A reset, shortened image, rewritten prefix or earlier noncommit suffix is
    unknown rather than evidence of one continuing transaction. Empty prior
    bytes are not an established WAL generation. Inputs must be immutable bytes.
    """
    if (type(raw) is not bytes or not _valid_limits(max_bytes, max_frames)
            or expected_page_size is not None and not _page_size(expected_page_size)
            or prior_prefix is not None and type(prior_prefix) is not bytes):
        return _unknown("invalid-input")
    result = _parse(raw, expected_page_size, max_bytes, max_frames)
    if result["status"] != "verified" or prior_prefix is None:
        return result
    prior = _parse(prior_prefix, expected_page_size, max_bytes, max_frames)
    if prior["status"] != "verified" or prior["noncommit_suffix_frames"]:
        return _unknown("prior-prefix-not-committed-or-header-only", len(raw), result["sha256"])
    if len(prior_prefix) > len(raw):
        return _unknown("prior-prefix-truncated", len(raw), result["sha256"])
    if prior_prefix[:HEADER_BYTES] != raw[:HEADER_BYTES]:
        return _unknown("prior-header-or-generation-changed", len(raw), result["sha256"])
    if not raw.startswith(prior_prefix):
        return _unknown("prior-prefix-rewritten", len(raw), result["sha256"])
    added = result["frame_count"] - prior["frame_count"]
    commits = sum(frame["is_commit"] for frame in result["frames"][prior["frame_count"]:])
    result["prior_prefix"] = {"status": "verified-exact-prefix", "byte_count": len(prior_prefix),
                              "sha256": prior["sha256"], "frame_count": prior["frame_count"],
                              "latest_commit_frame": prior["latest_commit_frame"],
                              "appended_frames": added, "appended_commit_frames": commits}
    result["growth_after_prior_commit"] = added > 0 and commits == 0
    return result


def _fd_identity(info: os.stat_result) -> dict:
    return {"dev": info.st_dev, "ino": info.st_ino, "mode": info.st_mode,
            "uid": info.st_uid, "gid": info.st_gid, "links": info.st_nlink,
            "size": info.st_size, "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns}


def inspect_wal_fd(fd: int, *, expected_page_size: int | None = None,
                   prior_prefix: bytes | None = None, max_bytes: int = MAX_BYTES,
                   max_frames: int = MAX_FRAMES) -> dict:
    """pread an already-open read-only regular FD without taking ownership of it.

    Descriptor mode/link/size and exact before/after metadata must match. This is
    a bounded copy observation, not a freeze, root/path authority check or a
    guarantee against an external writer. The caller retains/owns the FD and
    supplies all native identity/quiescence proof. atime is not compared.
    """
    if type(fd) is not int or fd < 0 or not _valid_limits(max_bytes, max_frames):
        return _unknown("invalid-fd-input")
    try:
        import fcntl
        before = os.fstat(fd)
        if (fcntl.fcntl(fd, fcntl.F_GETFL) & os.O_ACCMODE != os.O_RDONLY
                or not stat.S_ISREG(before.st_mode) or before.st_nlink != 1):
            return _unknown("fd-not-readonly-single-link-regular")
        if before.st_size < 0 or before.st_size > max_bytes:
            return _unknown("fd-byte-limit", before.st_size)
        chunks = []
        offset = 0
        while offset < before.st_size:
            chunk = os.pread(fd, min(65536, before.st_size - offset), offset)
            if not chunk:
                return _unknown("fd-short-read", before.st_size)
            chunks.append(chunk)
            offset += len(chunk)
        after = os.fstat(fd)
        if _fd_identity(before) != _fd_identity(after):
            return _unknown("fd-changed-during-copy", before.st_size)
        result = inspect_wal(b"".join(chunks), expected_page_size=expected_page_size,
                             prior_prefix=prior_prefix, max_bytes=max_bytes, max_frames=max_frames)
        result["fd_copy"] = {"status": "metadata-stable-readonly-copy", "identity": _fd_identity(after),
                             "offset_preserved": True, "path_authority": "not-established"}
        return result
    except (ImportError, AttributeError):
        return _unknown("fd-copy-platform-unsupported")
    except (OSError, OverflowError, ValueError):
        return _unknown("fd-copy-unavailable")
